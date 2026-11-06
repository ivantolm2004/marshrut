import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:marshrut/main.dart';
import 'routing_editor_test.dart' show RoutingFixture;

class ApplicationFixture extends RoutingFixture {
  @override
  Future<Map<String, dynamic>> call(String action,
      [Map<String, dynamic> args = const {}]) async {
    final result = await super.call(action, args);
    result['application_discovery_supported'] = true;
    result['applications'] = [
      {
        'id': 'chrome',
        'name': 'Google Chrome',
        'executable': 'chrome.exe',
        'path': r'C:\Apps\Chrome\chrome.exe',
        'running': true,
        'process_count': 4,
        'system': false
      },
      {
        'id': 'dota-app',
        'name': 'dota2',
        'executable': 'dota2.exe',
        'path': r'C:\Games\dota2.exe',
        'running': true,
        'process_count': 1,
        'system': false,
        'rule_id': 'dota'
      },
      {
        'id': 'closed',
        'name': 'Closed app',
        'executable': 'closed.exe',
        'path': r'C:\Apps\closed.exe',
        'running': false,
        'process_count': 0,
        'system': false
      },
    ];
    return result;
  }
}

Future<void> openApps(WidgetTester tester, ApplicationFixture backend,
    {Size size = const Size(1200, 900)}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  await tester.pumpWidget(MarshrutApp(backend: backend));
  await tester.pumpAndSettle();
  await tester.tap(find.text('Правила').last);
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('Application list fits a narrow window', (tester) async {
    await openApps(tester, ApplicationFixture(), size: const Size(360, 850));
    expect(tester.takeException(), isNull);
    await tester
        .ensureVisible(find.byKey(const ValueKey('application_chrome')));
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets('Running apps appear automatically and closed apps can be shown',
      (tester) async {
    final backend = ApplicationFixture();
    await openApps(tester, backend);
    expect(find.text('Google Chrome'), findsOneWidget);
    expect(find.text('Closed app'), findsNothing);
    expect(
        find.textContaining('Dota 2, CS2 и PUBG по умолчанию'), findsNothing);
    await tester.tap(find.text('Только запущенные'));
    await tester.pumpAndSettle();
    expect(find.text('Closed app'), findsOneWidget);
    await tester.ensureVisible(find.text('Обновить приложения'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Обновить приложения'));
    await tester.pumpAndSettle();
    expect(backend.actions, contains('applications_refresh'));
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets('Application configuration creates an exact-path rule',
      (tester) async {
    final backend = ApplicationFixture();
    await openApps(tester, backend);
    final chrome = find.byKey(const ValueKey('application_chrome'));
    await tester.ensureVisible(chrome);
    await tester.pumpAndSettle();
    await tester.tap(
        find.descendant(of: chrome, matching: find.text('Настроить маршрут')));
    await tester.pumpAndSettle();
    expect(find.widgetWithText(TextFormField, 'Google Chrome'), findsOneWidget);
    await tester.tap(find.byKey(const ValueKey('rule_route')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('VPN автоматически').last);
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('rule_save')));
    await tester.pumpAndSettle();
    expect(backend.rules.first['route'], 'vpn');
    expect(
        backend.rules.first['process_paths'], [r'C:\Apps\Chrome\chrome.exe']);
    expect(backend.rules.first['processes'], isEmpty);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });
}
