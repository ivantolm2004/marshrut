import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:marshrut/backend.dart';
import 'package:marshrut/main.dart';

class RoutingFixture extends Backend {
  int revision = 0;
  bool pending = false, connected = false, failSave = false;
  final actions = <String>[];
  List<Map<String, dynamic>> rules = [
    for (final e in {
      'dota': 'Dota 2',
      'cs2': 'Counter-Strike 2',
      'pubg': 'PUBG: Battlegrounds'
    }.entries)
      {
        'id': e.key,
        'name': e.value,
        'category': 'games',
        'route': 'direct',
        'enabled': true,
        'auto_group': 'default',
        'network': '',
        'domains': <String>[],
        'processes': [
          e.key == 'pubg'
              ? 'TslGame.exe'
              : e.key == 'dota'
                  ? 'dota2.exe'
                  : 'cs2.exe'
        ],
        'process_paths': <String>[],
        'ip_cidr': <String>[]
      },
    {
      'id': 'youtube',
      'name': 'YouTube',
      'category': 'video',
      'route': 'auto',
      'enabled': true,
      'auto_group': 'youtube',
      'network': '',
      'domains': ['youtube.com', 'googlevideo.com'],
      'processes': <String>[],
      'process_paths': <String>[],
      'ip_cidr': <String>[]
    },
  ];
  @override
  Future<Map<String, dynamic>> call(String action,
      [Map<String, dynamic> args = const {}]) async {
    actions.add(action);
    if (action == 'routing_save') {
      if (failSave) throw CoreException('Тестовая ошибка сохранения');
      if (args['rules_revision'] != revision) {
        throw CoreException('Список уже изменился');
      }
      final next = (jsonDecode(jsonEncode(args['rules'])) as List)
          .map((r) => Map<String, dynamic>.from(r as Map))
          .toList();
      if (next.any((r) =>
          (r['domains'] as List).isEmpty &&
          (r['processes'] as List).isEmpty &&
          (r['process_paths'] as List).isEmpty &&
          (r['ip_cidr'] as List).isEmpty)) {
        throw CoreException('Добавьте хотя бы одно условие');
      }
      for (final rule in next) {
        if (rule['id'] == '') rule['id'] = 'custom-$revision';
      }
      rules = next;
      revision++;
      pending = connected;
    }
    if (action == 'routing_apply') pending = false;
    return {
      'phase': connected ? 'connected' : 'disconnected',
      'platform': 'windows',
      'routing_supported': true,
      'routing_rules': rules,
      'routing_revision': revision,
      'routing_pending': pending,
      'groups': <dynamic>[],
      'pins': <String, dynamic>{},
      'components': <dynamic>[],
      'servers': [
        {'id': 'test', 'name': 'Мой сервер', 'route': 'vless-test'}
      ]
    };
  }
}

Future<void> showRules(WidgetTester tester, RoutingFixture backend,
    {Size size = const Size(1200, 850)}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  await tester.pumpWidget(RepaintBoundary(
      key: const ValueKey('routing_preview'),
      child: MarshrutApp(backend: backend)));
  await tester.pumpAndSettle();
  await tester.tap(find.text('Правила').last);
  await tester.pumpAndSettle();
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUpAll(() async {
    final fonts = FontLoader('PT Sans')
      ..addFont(rootBundle.load('assets/fonts/PT_Sans-Web-Regular.ttf'))
      ..addFont(rootBundle.load('assets/fonts/PT_Sans-Web-Bold.ttf'));
    await fonts.load();
    final icons = FontLoader('MaterialIcons')
      ..addFont(rootBundle.load('fonts/MaterialIcons-Regular.otf'));
    await icons.load();
  });
  testWidgets('Search filters requested games and route edit persists',
      (tester) async {
    final backend = RoutingFixture();
    await showRules(tester, backend);
    await tester.enterText(
        find.byKey(const ValueKey('routing_search')), 'PUBG');
    await tester.pumpAndSettle();
    expect(find.text('3. PUBG: Battlegrounds'), findsOneWidget);
    expect(find.text('1. Dota 2'), findsNothing);
    await tester.tap(find.text('3. PUBG: Battlegrounds'));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('rule_route')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('VPN автоматически').last);
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('rule_save')));
    await tester.pumpAndSettle();
    expect(backend.rules[2]['route'], 'vpn');
    expect(backend.revision, 1);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets('Add domain rule and keep dialog open on save failure',
      (tester) async {
    final backend = RoutingFixture();
    await showRules(tester, backend);
    await tester.tap(find.byKey(const ValueKey('routing_add')));
    await tester.pumpAndSettle();
    await tester.enterText(find.byKey(const ValueKey('rule_name')), 'Мой сайт');
    await tester.ensureVisible(find.byKey(const ValueKey('rule_domains')));
    await tester.enterText(
        find.byKey(const ValueKey('rule_domains')), 'example.com');
    backend.failSave = true;
    await tester.tap(find.byKey(const ValueKey('rule_save')));
    await tester.pumpAndSettle();
    expect(find.textContaining('Тестовая ошибка сохранения'), findsOneWidget);
    expect(backend.rules.length, 4);
    backend.failSave = false;
    await tester.tap(find.byKey(const ValueKey('rule_save')));
    await tester.pumpAndSettle();
    expect(backend.rules.first['name'], 'Мой сайт');
    expect(backend.rules.first['domains'], ['example.com']);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets('Toggle, reorder and apply pending rules explicitly',
      (tester) async {
    final backend = RoutingFixture()..connected = true;
    await showRules(tester, backend);
    final dota = find.byKey(const ValueKey('routing_rule_dota'));
    await tester.tap(find.descendant(of: dota, matching: find.byType(Switch)));
    await tester.pumpAndSettle();
    expect(backend.rules.first['enabled'], false);
    expect(backend.pending, true);
    await tester.ensureVisible(dota);
    await tester.tap(find.descendant(
        of: dota, matching: find.byTooltip('Действия с правилом')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Ниже по приоритету'));
    await tester.pumpAndSettle();
    expect(backend.rules.first['id'], 'cs2');
    await tester.ensureVisible(find.text('Применить'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Применить'));
    await tester.pumpAndSettle();
    expect(backend.pending, false);
    expect(backend.actions, contains('routing_apply'));
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets('Import exported JSON and reject malformed imports',
      (tester) async {
    final backend = RoutingFixture();
    await showRules(tester, backend);
    await tester.tap(find.text('Импорт JSON'));
    await tester.pumpAndSettle();
    await tester.enterText(
        find.byKey(const ValueKey('routing_import_json')), 'bad json');
    await tester.tap(find.text('Импортировать'));
    await tester.pumpAndSettle();
    expect(find.textContaining('Нужен JSON'), findsOneWidget);
    await tester.enterText(
        find.byKey(const ValueKey('routing_import_json')),
        jsonEncode({
          'schema': 1,
          'rules': [backend.rules.last]
        }));
    await tester.tap(find.text('Импортировать'));
    await tester.pumpAndSettle();
    expect(backend.rules.length, 1);
    expect(backend.rules.first['id'], 'youtube');
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });
  for (final width in [360.0, 1200.0]) {
    testWidgets('Rules and editor fit at width $width', (tester) async {
      final backend = RoutingFixture();
      final output = Platform.environment['MARSHRUT_UI_PREVIEW'];
      if (output != null) {
        final catalogue = File('$output/Marshrut-routing-defaults.json');
        if (catalogue.existsSync()) {
          backend.rules = ((jsonDecode(catalogue.readAsStringSync())
                  as Map)['rules'] as List)
              .map((r) => Map<String, dynamic>.from(r as Map))
              .toList();
        }
      }
      await showRules(tester, backend, size: Size(width, 850));
      expect(tester.takeException(), isNull);
      if (output != null && width == 1200) {
        await tester.runAsync(() async {
          final boundary = tester.renderObject<RenderRepaintBoundary>(
              find.byKey(const ValueKey('routing_preview')));
          final image = await boundary.toImage();
          final bytes = await image.toByteData(format: ui.ImageByteFormat.png);
          await File('$output/routing-list.png')
              .writeAsBytes(bytes!.buffer.asUint8List());
          image.dispose();
        });
      }
      await tester.tap(find.byKey(const ValueKey('routing_add')));
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      await tester.tap(find.text('Отмена').last);
      await tester.pumpAndSettle();
      await tester.pumpWidget(const SizedBox());
    });
  }
}
