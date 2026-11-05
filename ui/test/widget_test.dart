import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:marshrut/main.dart';
import 'package:marshrut/backend.dart';

class FixtureBackend extends Backend {
  String selected = '';
  bool pinging = false;
  bool autoStart = true, autoWarp = true;
  bool connectOnStartup = true;
  final actions = <String>[];
  @override
  Future<Map<String, dynamic>> call(String action,
      [Map<String, dynamic> args = const {}]) async {
    actions.add(action);
    if (action == 'select_server') selected = args['id'] as String;
    if (action == 'ping_servers') pinging = true;
    if (action == 'cancel_server_ping') pinging = false;
    if (action == 'autostart') autoStart = args['value'] as bool;
    if (action == 'auto_warp') autoWarp = args['value'] as bool;
    if (action == 'connect_on_startup') {
      connectOnStartup = args['value'] as bool;
    }
    return {
      'phase': 'disconnected',
      'groups': [
        {
          'id': 'ai',
          'name': 'AI-сервисы',
          'domains': ['chatgpt.com']
        }
      ],
      'servers': [
        {
          'id': 'fixture',
          'route': 'vless-fixture',
          'name':
              'Личный сервер с очень длинным именем для проверки компоновки интерфейса'
        }
      ],
      'components': [
        {'id': 'vless', 'state': 'configured'},
        {'id': 'warp', 'state': 'unconfigured'},
        {'id': 'dpi', 'state': 'available'}
      ],
      'pins': {'ai': 'vless-removed'},
      'direct_fallback': true,
      'dpi_enabled': true,
      'platform': 'windows',
      'autostart_supported': true,
      'autostart': autoStart,
      'auto_warp': autoWarp,
      'connect_on_startup': connectOnStartup,
      'selected_server': selected,
      'server_pings': {
        'fixture': {
          'reachable': true,
          'latency_ms': 15,
          'checked_at': DateTime.now().toUtc().toIso8601String()
        }
      },
      if (pinging)
        'server_ping': {
          'running': true,
          'completed': 0,
          'total': 1,
          'started_at': DateTime.now().toUtc().toIso8601String()
        },
    };
  }
}

class HundredBackend extends FixtureBackend {
  @override
  Future<Map<String, dynamic>> call(String action,
      [Map<String, dynamic> args = const {}]) async {
    final data = await super.call(action, args);
    data['servers'] = List.generate(
        100,
        (i) => {
              'id': 'server-$i',
              'route': 'vless-server-$i',
              'name': 'Сервер ${i.toString().padLeft(3, '0')}'
            });
    return data;
  }
}

class ConnectedBackend extends FixtureBackend {
  @override
  Future<Map<String, dynamic>> call(String action,
      [Map<String, dynamic> args = const {}]) async {
    final data = await super.call(action, args);
    data['phase'] = 'connected';
    return data;
  }
}

class AndroidDiagnosticsBackend extends FixtureBackend {
  @override
  Future<Map<String, dynamic>> call(String action,
      [Map<String, dynamic> args = const {}]) async {
    final data = await super.call(action, args);
    data['platform'] = 'android';
    data['android'] = {
      'stage': 'core_ready',
      'previous_stage': 'tun_ready',
      'exit_reason': 5,
      'exit_status': 6,
      'native_failure': 'go_stack_split',
      'exception': '',
    };
    return data;
  }
}

class DelayedServiceBackend extends FixtureBackend {
  bool available = false;
  @override
  Future<Map<String, dynamic>> call(String action,
      [Map<String, dynamic> args = const {}]) async {
    if (action == 'status' && !available) {
      throw CoreException('Служба ещё запускается');
    }
    return super.call(action, args);
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUpAll(() async {
    final fonts = FontLoader('PT Sans')
      ..addFont(rootBundle.load('assets/fonts/PT_Sans-Web-Regular.ttf'))
      ..addFont(rootBundle.load('assets/fonts/PT_Sans-Web-Bold.ttf'));
    await fonts.load();
  });
  testWidgets('Startup launch connects once after service becomes available',
      (tester) async {
    final backend = DelayedServiceBackend();
    await tester.pumpWidget(MarshrutApp(backend: backend, startupLaunch: true));
    await tester.pumpAndSettle();
    expect(backend.actions, isNot(contains('startup_connect')));
    backend.available = true;
    await tester.pump(const Duration(seconds: 5));
    await tester.pumpAndSettle();
    expect(backend.actions.where((a) => a == 'startup_connect').length, 1);
    await tester.pump(const Duration(seconds: 10));
    await tester.pumpAndSettle();
    expect(backend.actions.where((a) => a == 'startup_connect').length, 1);
    await tester.pumpWidget(const SizedBox());
  });
  for (final scenario in ['manual', 'autorun_off', 'connect_off', 'android']) {
    testWidgets('No automatic connection for $scenario', (tester) async {
      final backend = scenario == 'android'
          ? AndroidDiagnosticsBackend()
          : FixtureBackend();
      if (scenario == 'autorun_off') backend.autoStart = false;
      if (scenario == 'connect_off') backend.connectOnStartup = false;
      await tester.pumpWidget(
          MarshrutApp(backend: backend, startupLaunch: scenario != 'manual'));
      await tester.pumpAndSettle();
      await tester.pump(const Duration(seconds: 5));
      await tester.pumpAndSettle();
      expect(backend.actions, isNot(contains('startup_connect')));
      await tester.pumpWidget(const SizedBox());
    });
  }
  testWidgets('Connection on startup has an independent settings switch',
      (tester) async {
    final backend = FixtureBackend();
    await tester.pumpWidget(MarshrutApp(backend: backend));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Настройки').last);
    await tester.pumpAndSettle();
    final toggle =
        find.widgetWithText(SwitchListTile, 'Подключаться при автозапуске');
    await tester.ensureVisible(toggle);
    await tester.tap(toggle);
    await tester.pumpAndSettle();
    expect(backend.connectOnStartup, false);
    expect(backend.autoStart, true);
    expect(backend.actions, contains('connect_on_startup'));
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets('Android connection failure details appear before connection',
      (tester) async {
    tester.view.physicalSize = const Size(360, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    await tester.pumpWidget(MarshrutApp(backend: AndroidDiagnosticsBackend()));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Диагностика').last);
    await tester.pumpAndSettle();
    expect(find.text('Запуск VPN на Android'), findsOneWidget);
    expect(find.textContaining('Предыдущий этап: tun_ready'), findsOneWidget);
    expect(find.textContaining('go_stack_split'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets('Autostart and automatic WARP switches can be disabled',
      (tester) async {
    final backend = FixtureBackend();
    await tester.pumpWidget(MarshrutApp(backend: backend));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Настройки').last);
    await tester.pumpAndSettle();
    final startup = find.widgetWithText(SwitchListTile, 'Автозапуск');
    await tester.ensureVisible(startup);
    await tester.pumpAndSettle();
    await tester.tap(startup);
    await tester.pumpAndSettle();
    expect(backend.autoStart, false);
    final warp = find.widgetWithText(SwitchListTile, 'Автонастройка WARP');
    await tester.ensureVisible(warp);
    await tester.pumpAndSettle();
    await tester.tap(warp);
    await tester.pumpAndSettle();
    expect(backend.autoWarp, false);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets(
      'WARP registration is available while connected and requires consent',
      (tester) async {
    final backend = ConnectedBackend();
    await tester.pumpWidget(MarshrutApp(backend: backend));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Серверы').last);
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.text('Повторить настройку'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Повторить настройку'));
    await tester.pumpAndSettle();
    expect(backend.actions, isNot(contains('warp_register')));
    await tester.tap(find.text('Отмена').last);
    await tester.pumpAndSettle();
    expect(backend.actions, isNot(contains('warp_register')));
    await tester.tap(find.text('Повторить настройку'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Принять и настроить'));
    await tester.pumpAndSettle();
    expect(backend.actions.where((s) => s == 'warp_register').length, 1);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets('Search selects one of a hundred servers', (tester) async {
    final backend = HundredBackend();
    await tester.pumpWidget(MarshrutApp(backend: backend));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Серверы').last);
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.text('Выбрать сервер'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Выбрать сервер'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), 'Сервер 099');
    await tester.pumpAndSettle();
    await tester.tap(find.text('Сервер 099').last);
    await tester.pumpAndSettle();
    expect(backend.selected, 'server-99');
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets('Unconfigured UI never claims connection', (tester) async {
    await tester.pumpWidget(const MarshrutApp());
    await tester.pump();
    expect(find.text('Подключено'), findsNothing);
    expect(find.text('Интернет своим путём'), findsOneWidget);
    expect(find.text('Добавить VLESS или подписку'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
  });
  for (final size in [const Size(360, 800), const Size(1280, 900)]) {
    testWidgets('Server ping and manual/auto selection at $size',
        (tester) async {
      tester.view.physicalSize = size;
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final backend = FixtureBackend();
      await tester.pumpWidget(MarshrutApp(backend: backend));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Серверы').last);
      await tester.pumpAndSettle();
      await tester.ensureVisible(find.text('Пинг серверов'));
      await tester.tap(find.text('Пинг серверов'));
      await tester.pump(const Duration(milliseconds: 200));
      expect(backend.actions, contains('ping_servers'));
      expect(find.text('Проверено 0 из 1'), findsOneWidget);
      await tester.tap(find.text('Остановить пинг'));
      await tester.pumpAndSettle();
      await tester.ensureVisible(find.text('Выбрать сервер'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Выбрать сервер'));
      await tester.pumpAndSettle();
      expect(find.text('Выбрать VLESS-сервер'), findsOneWidget);
      await tester.enterText(find.byType(TextField), 'очень');
      await tester.pumpAndSettle();
      await tester.tap(find.textContaining('Личный сервер с очень').last);
      await tester.pumpAndSettle();
      expect(backend.selected, 'fixture');
      await tester.ensureVisible(find.text('Автоматически').first);
      await tester.pumpAndSettle();
      await tester.tap(find.text('Автоматически').first);
      await tester.pumpAndSettle();
      expect(backend.selected, '');
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    });
    testWidgets('Navigation and stale manual pin at $size', (tester) async {
      tester.view.physicalSize = size;
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      await tester.pumpWidget(MarshrutApp(backend: FixtureBackend()));
      await tester.pumpAndSettle();
      expect(find.text('Подключено'), findsNothing);
      expect(tester.takeException(), isNull);
      for (final page in ['Серверы', 'Правила', 'Диагностика', 'Настройки']) {
        await tester.tap(find.text(page).last);
        await tester.pumpAndSettle();
        expect(tester.takeException(), isNull, reason: 'page $page at $size');
      }
      await tester.pumpWidget(const SizedBox());
    });
  }
}
