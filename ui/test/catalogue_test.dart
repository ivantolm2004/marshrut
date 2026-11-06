import 'dart:io';
import 'dart:ui' as ui;
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:marshrut/backend.dart';
import 'package:marshrut/main.dart';
import 'routing_editor_test.dart' show RoutingFixture;

class CatalogueFixture extends RoutingFixture {
  final records = <Map<String, dynamic>>[
    {
      'id': 'chrome',
      'kind': 'application',
      'origin': 'discovered',
      'route': 'inherit',
      'definition': {
        'id': 'chrome',
        'name': 'Google Chrome',
        'category': 'apps',
        'process_paths': [r'C:\Apps\Chrome\chrome.exe']
      },
      'application': {'running': true, 'rule_id': 'shared'}
    },
    {
      'id': 'site',
      'kind': 'site',
      'origin': 'custom',
      'route': 'direct',
      'definition': {
        'id': 'site',
        'name': 'Мой сайт',
        'category': 'other',
        'domains': ['example.org']
      }
    },
    {
      'id': 'ip',
      'kind': 'network',
      'origin': 'builtin',
      'route': 'inherit',
      'definition': {
        'id': 'ip',
        'name': 'Тестовая сеть',
        'category': 'other',
        'ip_cidr': ['203.0.113.0/24']
      }
    },
  ];
  Map<String, dynamic>? lastSave;
  Map<String, dynamic>? lastImport;
  @override
  Future<Map<String, dynamic>> call(String action,
      [Map<String, dynamic> args = const {}]) async {
    if (action == 'catalogue_save') {
      if (failSave) throw CoreException('Не удалось сохранить каталог');
      if (args['rules_revision'] != revision) {
        throw CoreException('Список уже изменился');
      }
      lastSave = Map.from(args);
      if (args['id'] != null) {
        records.firstWhere((e) => e['id'] == args['id'])['route'] =
            args['route'];
      }
      revision++;
      pending = connected;
    }
    if (action == 'catalogue_import') {
      if (failSave) throw CoreException('Список уже изменился');
      lastImport = Map.from(args);
      revision++;
    }
    if (action == 'routing_preview') {
      final current = {
        'route': 'vpn',
        'rule_name': 'Индивидуальный VPN',
        'reason': 'Совпал путь приложения',
        'shadowed': 1
      };
      final active = {
        'route': 'direct',
        'rule_name': 'Общее правило',
        'reason': 'Совпало приложение',
        'shadowed': 0
      };
      return {
        'notice': 'Предпросмотр правил. Это не измерение трафика.',
        'results': [
          {
            'target': 'example.org',
            'tcp': current,
            'udp': current,
            'active_tcp': active,
            'active_udp': active
          }
        ]
      };
    }
    final result = await super.call(action, args);
    return {
      ...result,
      'catalogue_supported': true,
      'catalogue_transfer_supported': true,
      'catalogue': records,
      'application_discovery_supported': true
    };
  }
}

Future<void> openCatalogue(WidgetTester tester, CatalogueFixture backend,
    {Size size = const Size(1200, 900)}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  await tester.pumpWidget(RepaintBoundary(
      key: const ValueKey('catalogue_preview'),
      child: MarshrutApp(backend: backend)));
  await tester.pumpAndSettle();
  await tester.tap(find.text('Правила').last);
  await tester.pumpAndSettle();
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUpAll(() async {
    await (FontLoader('PT Sans')
          ..addFont(rootBundle.load('assets/fonts/PT_Sans-Web-Regular.ttf'))
          ..addFont(rootBundle.load('assets/fonts/PT_Sans-Web-Bold.ttf')))
        .load();
    await (FontLoader('MaterialIcons')
          ..addFont(rootBundle.load('fonts/MaterialIcons-Regular.otf')))
        .load();
  });
  testWidgets('Catalogue search and source/type filters', (tester) async {
    await openCatalogue(tester, CatalogueFixture());
    expect(find.text('Google Chrome'), findsOneWidget);
    await tester.enterText(
        find.byKey(const ValueKey('catalogue_search')), 'example.org');
    await tester.pumpAndSettle();
    expect(find.text('Google Chrome'), findsNothing);
    expect(find.text('Мой сайт'), findsOneWidget);
    await tester.enterText(find.byKey(const ValueKey('catalogue_search')), '');
    await tester.tap(find.text('Обнаруженные'));
    await tester.pumpAndSettle();
    expect(find.text('Мой сайт'), findsNothing);
    expect(find.text('Google Chrome'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets('Catalogue import keeps the document on a revision conflict',
      (tester) async {
    final backend = CatalogueFixture()..failSave = true;
    await openCatalogue(tester, backend);
    await tester.tap(find.text('Перенос каталога'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Импортировать JSON каталога'));
    await tester.pumpAndSettle();
    final input = find.byKey(const ValueKey('catalogue_import_json'));
    const document =
        '{"format":"marshrut-routing-catalogue","schema":1,"rules":[],"entries":[]}';
    await tester.enterText(input, document);
    await tester.tap(find.text('Импортировать'));
    await tester.pumpAndSettle();
    expect(find.text('Список уже изменился'), findsOneWidget);
    expect(tester.widget<TextField>(input).controller!.text, document);
    backend.failSave = false;
    await tester.tap(find.text('Импортировать'));
    await tester.pumpAndSettle();
    expect(backend.lastImport!['rules_revision'], 0);
    expect(backend.lastImport!['catalogue']['entries'], isEmpty);
    expect(input, findsNothing);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets('Add URL keeps failed form and sends optimistic revision',
      (tester) async {
    final backend = CatalogueFixture()..failSave = true;
    await openCatalogue(tester, backend);
    await tester.tap(find.byKey(const ValueKey('catalogue_add')));
    await tester.pumpAndSettle();
    await tester.enterText(find.byKey(const ValueKey('catalogue_address')),
        'https://example.net/path');
    await tester.enterText(
        find.byKey(const ValueKey('catalogue_name')), 'Новый сайт');
    await tester.tap(find.text('Сохранить'));
    await tester.pumpAndSettle();
    expect(find.text('Не удалось сохранить каталог'), findsOneWidget);
    expect(backend.revision, 0);
    backend.failSave = false;
    await tester.tap(find.text('Сохранить'));
    await tester.pumpAndSettle();
    expect(backend.lastSave?['text'], 'https://example.net/path');
    expect(backend.lastSave?['route'], 'inherit');
    expect(backend.lastSave?['rules_revision'], 0);
    expect(find.byType(AlertDialog), findsNothing);
    expect(tester.takeException(), isNull);
    await tester.tap(find.byKey(const ValueKey('catalogue_add')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Отмена'));
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets('Quick app route offers searchable servers and preserves app ID',
      (tester) async {
    final backend = CatalogueFixture()..connected = true;
    await openCatalogue(tester, backend);
    final button = find.byKey(const ValueKey('catalogue_route_chrome'));
    await tester.ensureVisible(button);
    await tester.pumpAndSettle();
    await tester.tap(button);
    await tester.pumpAndSettle();
    await tester.enterText(
        find.byKey(const ValueKey('catalogue_route_search')), 'Мой сервер');
    await tester.pumpAndSettle();
    expect(find.byKey(const ValueKey('target_vless-test')), findsOneWidget);
    await tester.tap(find.byKey(const ValueKey('target_vless-test')));
    await tester.pumpAndSettle();
    expect(backend.lastSave?['id'], 'chrome');
    expect(backend.lastSave?['route'], 'vless-test');
    expect(backend.pending, true);
    await tester.ensureVisible(find.text('Применить'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Применить'));
    await tester.pumpAndSettle();
    expect(backend.pending, false);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets('Preview distinguishes saved and applied protocol routes',
      (tester) async {
    await openCatalogue(tester, CatalogueFixture());
    final why = find.descendant(
        of: find.byKey(const ValueKey('catalogue_chrome')),
        matching: find.text('Почему этот маршрут?'));
    await tester.ensureVisible(why);
    await tester.pumpAndSettle();
    await tester.tap(why);
    await tester.pumpAndSettle();
    expect(find.textContaining('Сохранено · TCP'), findsOneWidget);
    expect(find.textContaining('Применено · UDP'), findsOneWidget);
    await tester.enterText(
        find.byKey(const ValueKey('catalogue_preview_address')),
        'https://example.org');
    await tester.tap(find.text('Проверить'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Закрыть'));
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });
  for (final width in [360.0, 1200.0]) {
    testWidgets('Catalogue fits $width and renders preview', (tester) async {
      await openCatalogue(tester, CatalogueFixture(), size: Size(width, 1000));
      expect(tester.takeException(), isNull);
      final output = Platform.environment['MARSHRUT_UI_PREVIEW'];
      if (output != null) {
        await tester.runAsync(() async {
          final boundary = tester.renderObject<RenderRepaintBoundary>(
              find.byKey(const ValueKey('catalogue_preview')));
          final image = await boundary.toImage();
          final data = await image.toByteData(format: ui.ImageByteFormat.png);
          await File('$output/catalogue-${width.toInt()}.png')
              .writeAsBytes(data!.buffer.asUint8List());
          image.dispose();
        });
      }
      await tester.tap(find.byKey(const ValueKey('catalogue_add')));
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      await tester.tap(find.text('Отмена'));
      await tester.pumpAndSettle();
      await tester.pumpWidget(const SizedBox());
    });
  }
}
