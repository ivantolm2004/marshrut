import 'dart:async';
import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'backend.dart';
import 'routing_editor.dart';
import 'routing_catalogue.dart';

void main(List<String> args) =>
    runApp(MarshrutApp(startupLaunch: args.contains('--startup')));

const background = Color(0xFF101215);
const surface = Color(0xFF1A1D21);
const muted = Color(0xFF90999F);
const accent = Color(0xFFB9F277);
const border = Color(0xFF2D3338);
const appVersion =
    String.fromEnvironment('FLUTTER_BUILD_NAME', defaultValue: '0.1.11');

class MarshrutApp extends StatelessWidget {
  const MarshrutApp({super.key, this.backend, this.startupLaunch = false});
  final Backend? backend;
  final bool startupLaunch;
  @override
  Widget build(BuildContext context) => MaterialApp(
        title: 'Маршрут',
        debugShowCheckedModeBanner: false,
        theme: ThemeData(
          brightness: Brightness.dark,
          useMaterial3: true,
          scaffoldBackgroundColor: background,
          colorScheme: const ColorScheme.dark(
              primary: accent,
              surface: surface,
              onPrimary: background,
              outline: border),
          fontFamily: 'PT Sans',
          cardTheme: const CardThemeData(
              color: surface, elevation: 0, margin: EdgeInsets.zero),
          inputDecorationTheme: InputDecorationTheme(
              filled: true,
              fillColor: background,
              border: OutlineInputBorder(
                  borderRadius: BorderRadius.circular(14),
                  borderSide: const BorderSide(color: border))),
          snackBarTheme:
              const SnackBarThemeData(behavior: SnackBarBehavior.floating),
        ),
        home: Shell(backend: backend, startupLaunch: startupLaunch),
      );
}

class Shell extends StatefulWidget {
  const Shell({super.key, this.backend, this.startupLaunch = false});
  final Backend? backend;
  final bool startupLaunch;
  @override
  State<Shell> createState() => _ShellState();
}

class _ShellState extends State<Shell> {
  late final Backend core;
  int settingsRevision = 0;
  Map<String, dynamic> state = {};
  int page = 0;
  bool busy = false, polling = false;
  bool startupRequested = false;
  String? error;
  Timer? timer;
  final labels = [
    'Подключение',
    'Серверы',
    'Правила',
    'Диагностика',
    'Настройки'
  ];
  final icons = [
    Icons.route_rounded,
    Icons.dns_outlined,
    Icons.tune_rounded,
    Icons.monitor_heart_outlined,
    Icons.settings_outlined
  ];

  @override
  void initState() {
    super.initState();
    core = widget.backend ?? Backend();
    refresh();
    var ticks = 0;
    timer = Timer.periodic(const Duration(seconds: 1), (_) {
      ticks++;
      if (ticks % 5 == 0 || map('server_ping')['running'] == true) refresh();
    });
  }

  @override
  void dispose() {
    timer?.cancel();
    super.dispose();
  }

  Future<void> refresh() async {
    if (polling) return;
    polling = true;
    try {
      final next = await core.call('status');
      if (mounted) {
        setState(() {
          state = next;
          error = null;
        });
        // Wait until the service answers. Consume the launch once, so polling
        // never reconnects after a user's later manual disconnect.
        if (widget.startupLaunch && !startupRequested) {
          startupRequested = true;
          if (next['platform'] == 'windows' &&
              next['autostart'] == true &&
              next['connect_on_startup'] == true) {
            unawaited(act('startup_connect'));
          }
        }
      }
    } catch (e) {
      if (mounted) setState(() => error = e.toString());
    } finally {
      polling = false;
    }
  }

  Future<void> act(String action,
      [Map<String, dynamic> args = const {}]) async {
    if (busy && action != 'disconnect') return;
    setState(() => busy = true);
    try {
      final result = await core.call(action, args);
      if (mounted && action == 'speed' && result['mbps'] is num) {
        message(
            'Скорость: ${(result['mbps'] as num).toStringAsFixed(1)} Мбит/с · тест 2 МБ');
      }
      if (mounted && result['added'] != null) {
        message('Добавлено подключений: ${result['added']}');
      }
      if (mounted && result['notice'] is String) {
        message(result['notice'] as String);
      }
      if (mounted &&
          result['warnings'] is List &&
          (result['warnings'] as List).isNotEmpty) {
        message((result['warnings'] as List).join('\n'));
      }
      await refresh();
    } catch (e) {
      if (mounted) message(e.toString());
    } finally {
      if (mounted) {
        setState(() {
          busy = false;
          settingsRevision++;
        });
      }
    }
  }

  void message(String text) => ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text(text), duration: const Duration(seconds: 6)));
  String get phase => state['phase'] as String? ?? 'unavailable';
  bool get active => ['connected', 'degraded', 'connecting'].contains(phase);
  String get phaseLabel =>
      {
        'connected': 'Подключено',
        'connecting': 'Проверяем маршруты',
        'degraded': 'Нет рабочего маршрута',
        'disconnected': 'Готов к подключению',
        'disconnecting': 'Отключение',
        'error': 'Ошибка подключения',
        'unavailable': 'Ядро недоступно',
      }[phase] ??
      phase;
  List<dynamic> list(String key) =>
      state[key] is List ? state[key] as List : [];
  List<dynamic> get sortedServers {
    final values = List<dynamic>.from(list('servers'));
    final pings = map('server_pings');
    values.sort((a, b) {
      final ap = pings[a['id']] as Map?;
      final bp = pings[b['id']] as Map?;
      final av = ap?['reachable'] == true;
      final bv = bp?['reachable'] == true;
      if (av != bv) return av ? -1 : 1;
      if (av) {
        final latency =
            (ap!['latency_ms'] as num).compareTo(bp!['latency_ms'] as num);
        if (latency != 0) return latency;
      }
      return (a['name'] as String).compareTo(b['name'] as String);
    });
    return values;
  }

  String serverPingLabel(String id) {
    final ping = map('server_pings')[id] as Map?;
    final scan = map('server_ping');
    if (scan['running'] == true) {
      final started = DateTime.tryParse(scan['started_at']?.toString() ?? '');
      final checked = DateTime.tryParse(ping?['checked_at']?.toString() ?? '');
      if (checked == null || (started != null && checked.isBefore(started))) {
        return '…';
      }
    }
    if (ping == null) return '—';
    return ping['reachable'] == true
        ? '${ping['latency_ms']} мс'
        : 'Недоступен';
  }

  Future<void> chooseServer() async {
    final servers = sortedServers;
    var query = '';
    final selected = await showDialog<String>(
        context: context,
        builder: (context) => StatefulBuilder(builder: (context, update) {
              final filtered = servers
                  .where((s) => (s['name'] as String)
                      .toLowerCase()
                      .contains(query.toLowerCase()))
                  .toList();
              return AlertDialog(
                title: const Text('Выбрать VLESS-сервер'),
                content: SizedBox(
                    width: 540,
                    height: 420,
                    child: Column(children: [
                      TextField(
                          autofocus: true,
                          decoration: const InputDecoration(
                              labelText: 'Поиск сервера',
                              prefixIcon: Icon(Icons.search)),
                          onChanged: (value) => update(() => query = value)),
                      const SizedBox(height: 12),
                      Expanded(
                          child: filtered.isEmpty
                              ? const Center(child: Text('Серверы не найдены'))
                              : ListView.builder(
                                  itemCount: filtered.length,
                                  itemBuilder: (context, index) {
                                    final server = filtered[index];
                                    return ListTile(
                                      selected: state['selected_server'] ==
                                          server['id'],
                                      title: Text(server['name'] as String,
                                          maxLines: 2,
                                          overflow: TextOverflow.ellipsis),
                                      subtitle: Text(
                                          'TCP: ${serverPingLabel(server['id'] as String)}'),
                                      onTap: () => Navigator.pop(
                                          context, server['id'] as String),
                                    );
                                  }))
                    ])),
                actions: [
                  TextButton(
                      onPressed: () => Navigator.pop(context),
                      child: const Text('Отмена'))
                ],
              );
            }));
    if (selected != null && mounted) {
      await act('select_server', {'id': selected});
    }
  }

  Map<String, dynamic> map(String key) =>
      state[key] is Map ? Map<String, dynamic>.from(state[key] as Map) : {};
  String routeName(String id) {
    if (id == 'direct') return 'Напрямую';
    if (id == 'dpi') return 'DPI-обход';
    if (id == 'warp') return 'WARP';
    if (id == 'block') return 'Нет маршрута';
    for (final server in list('servers')) {
      if (server['route'] == id) return server['name'] as String;
    }
    return 'Недоступный сервер';
  }

  List<DropdownMenuItem<String>> routeOptions(String pin) {
    final items = <DropdownMenuItem<String>>[
      const DropdownMenuItem(value: '', child: Text('Автоматически')),
      const DropdownMenuItem(value: 'direct', child: Text('Напрямую')),
      const DropdownMenuItem(value: 'dpi', child: Text('DPI-обход · TCP')),
      if (list('components')
          .any((c) => c['id'] == 'warp' && c['state'] != 'unconfigured'))
        const DropdownMenuItem(value: 'warp', child: Text('WARP')),
      for (final server in list('servers'))
        DropdownMenuItem(
            value: server['route'] as String,
            child: Text(server['name'] as String,
                overflow: TextOverflow.ellipsis)),
    ];
    if (!items.any((item) => item.value == pin)) {
      items.add(DropdownMenuItem(
          value: pin, child: const Text('Недоступный закреплённый маршрут')));
    }
    return items;
  }

  @override
  Widget build(BuildContext context) =>
      LayoutBuilder(builder: (context, constraints) {
        final wide = constraints.maxWidth >= 850;
        return Scaffold(
          body: SafeArea(
              child: Row(children: [
            if (wide)
              Container(
                  width: 220,
                  decoration: const BoxDecoration(
                      border: Border(right: BorderSide(color: border))),
                  padding: const EdgeInsets.all(20),
                  child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        const SizedBox(height: 16),
                        brand(),
                        const SizedBox(height: 48),
                        for (var i = 0; i < labels.length; i++)
                          Padding(
                              padding: const EdgeInsets.only(bottom: 8),
                              child: Material(
                                  color: page == i
                                      ? accent.withValues(alpha: .10)
                                      : Colors.transparent,
                                  borderRadius: BorderRadius.circular(12),
                                  child: ListTile(
                                      dense: true,
                                      shape: RoundedRectangleBorder(
                                          borderRadius:
                                              BorderRadius.circular(12)),
                                      leading: Icon(icons[i],
                                          color: page == i ? accent : muted,
                                          size: 22),
                                      title: Text(labels[i],
                                          style: TextStyle(
                                              color:
                                                  page == i ? accent : muted)),
                                      onTap: () => setState(() => page = i)))),
                        const Spacer(),
                        const Text('ЛИЧНОЕ ПОДКЛЮЧЕНИЕ',
                            style: TextStyle(
                                color: muted,
                                fontSize: 10,
                                letterSpacing: 1.5)),
                        const SizedBox(height: 10),
                        const Text('Windows + Android',
                            style: TextStyle(fontSize: 12)),
                        const SizedBox(height: 4),
                        const Text('Версия $appVersion',
                            style: TextStyle(fontSize: 11, color: muted)),
                      ])),
            Expanded(
                child: Center(
                    child: ConstrainedBox(
                        constraints: const BoxConstraints(maxWidth: 1100),
                        child: Column(children: [
                          Padding(
                              padding: EdgeInsets.fromLTRB(
                                  wide ? 40 : 22, 26, wide ? 40 : 22, 20),
                              child: Row(children: [
                                if (!wide) brand(),
                                if (wide)
                                  Text(labels[page],
                                      style: const TextStyle(
                                          fontSize: 21,
                                          fontWeight: FontWeight.w600)),
                                const Spacer(),
                                Container(
                                    padding: const EdgeInsets.symmetric(
                                        horizontal: 12, vertical: 8),
                                    decoration: BoxDecoration(
                                        border: Border.all(color: border),
                                        borderRadius:
                                            BorderRadius.circular(30)),
                                    child: Row(
                                        mainAxisSize: MainAxisSize.min,
                                        children: [
                                          Icon(Icons.circle,
                                              size: 7,
                                              color: phase == 'connected'
                                                  ? accent
                                                  : muted),
                                          const SizedBox(width: 8),
                                          Text(
                                              phase == 'connected'
                                                  ? 'В сети'
                                                  : 'Локально',
                                              style:
                                                  const TextStyle(fontSize: 12))
                                        ]))
                              ])),
                          if (busy) const LinearProgressIndicator(minHeight: 2),
                          Expanded(
                              child: ListView(
                                  padding: EdgeInsets.fromLTRB(
                                      wide ? 40 : 22, 10, wide ? 40 : 22, 32),
                                  children: [
                                if (error != null)
                                  Padding(
                                      padding:
                                          const EdgeInsets.only(bottom: 18),
                                      child:
                                          notice(error!, Icons.info_outline)),
                                if ((state['error'] as String? ?? '')
                                    .isNotEmpty)
                                  Padding(
                                      padding:
                                          const EdgeInsets.only(bottom: 18),
                                      child: notice(state['error'] as String,
                                          Icons.error_outline)),
                                switch (page) {
                                  0 => connection(wide),
                                  1 => servers(),
                                  2 => rules(),
                                  3 => diagnostics(),
                                  _ => settings()
                                },
                              ])),
                        ])))),
          ])),
          bottomNavigationBar: wide
              ? null
              : NavigationBar(
                  selectedIndex: page,
                  onDestinationSelected: (i) => setState(() => page = i),
                  destinations: [
                      for (var i = 0; i < labels.length; i++)
                        NavigationDestination(
                            icon: Icon(icons[i]), label: labels[i])
                    ]),
        );
      });

  Widget brand() => const SizedBox(
      width: 160,
      child: Row(children: [
        Icon(Icons.alt_route_rounded, color: accent, size: 31),
        SizedBox(width: 9),
        Expanded(
            child: Text('маршрут',
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(
                    fontSize: 23,
                    fontWeight: FontWeight.w700,
                    letterSpacing: -.8)))
      ]));
  Widget panel(Widget child, {EdgeInsets padding = const EdgeInsets.all(24)}) =>
      Material(
          color: surface,
          shape: RoundedRectangleBorder(
              side: const BorderSide(color: border),
              borderRadius: BorderRadius.circular(20)),
          child: Padding(padding: padding, child: child));
  Widget notice(String text, IconData icon) => panel(
      Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Icon(icon, color: const Color(0xFFF3BE77), size: 20),
        const SizedBox(width: 12),
        Expanded(
            child: Text(text,
                style: const TextStyle(color: Color(0xFFE6D3BA), height: 1.5)))
      ]),
      padding: const EdgeInsets.all(18));
  Widget section(String title, String subtitle) => Padding(
      padding: const EdgeInsets.only(bottom: 22),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Text(title,
            style: const TextStyle(
                fontSize: 27, fontWeight: FontWeight.w600, letterSpacing: -.7)),
        const SizedBox(height: 7),
        Text(subtitle, style: const TextStyle(color: muted, height: 1.5))
      ]));
  Widget connection(bool wide) =>
      Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        section('Интернет своим путём',
            'Автоматический выбор рабочего соединения для каждого сервиса.'),
        panel(Column(children: [
          const SizedBox(height: 8),
          Container(
              width: 136,
              height: 136,
              decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  color: accent.withValues(
                      alpha: phase == 'connected' ? .14 : .05),
                  border: Border.all(color: accent.withValues(alpha: .3))),
              child: Icon(
                  phase == 'connected'
                      ? Icons.check_rounded
                      : Icons.power_settings_new_rounded,
                  color: accent,
                  size: 58)),
          const SizedBox(height: 24),
          Text(phaseLabel,
              style:
                  const TextStyle(fontSize: 25, fontWeight: FontWeight.w600)),
          const SizedBox(height: 8),
          Text(
              phase == 'connected'
                  ? 'Доступность маршрутов проверена. Смотрите путь каждого сервиса ниже.'
                  : 'Добавьте свою подписку и включите подключение.',
              textAlign: TextAlign.center,
              style: const TextStyle(color: muted, height: 1.5)),
          const SizedBox(height: 25),
          SizedBox(
              width: 250,
              height: 48,
              child: FilledButton.icon(
                  onPressed: (busy && phase != 'connecting') || error != null
                      ? null
                      : () => act(active ? 'disconnect' : 'connect'),
                  icon: Icon(
                      active ? Icons.stop_rounded : Icons.power_settings_new),
                  label: Text(active ? 'Отключить' : 'Подключить'))),
          const SizedBox(height: 12),
          TextButton.icon(
              onPressed: busy || !active ? null : () => act('probe'),
              icon: const Icon(Icons.refresh, size: 16),
              label: const Text('Проверить маршруты')),
        ])),
        const SizedBox(height: 22),
        LayoutBuilder(
            builder: (context, c) =>
                Wrap(spacing: 12, runSpacing: 12, children: [
                  for (final component in list('components'))
                    SizedBox(
                        width: c.maxWidth >= 650
                            ? (c.maxWidth - 24) / 3
                            : c.maxWidth,
                        child: componentCard(component as Map))
                ])),
        const SizedBox(height: 28),
        const Text('КУДА ИДЁТ ТРАФИК',
            style: TextStyle(color: muted, fontSize: 11, letterSpacing: 1.8)),
        const SizedBox(height: 12),
        for (final group in list('groups'))
          Padding(
              padding: const EdgeInsets.only(bottom: 10),
              child: routeCard(group as Map)),
        if (list('servers').isEmpty)
          Padding(
              padding: const EdgeInsets.only(top: 16),
              child: OutlinedButton.icon(
                  onPressed: busy ? null : () => importDialog(false),
                  icon: const Icon(Icons.add),
                  label: const Text('Добавить VLESS или подписку'))),
      ]);
  Widget componentCard(Map c) {
    final status = c['state'] as String? ?? 'unavailable';
    final label = {
          'verified': 'Проверен',
          'configured': 'Настроен',
          'configuring': 'Настраивается',
          'unconfigured': 'Не настроен',
          'unavailable': 'Не установлен',
          'available': 'Доступен',
          'disabled': 'Выключен',
          'error': 'Недоступен'
        }[status] ??
        status;
    return panel(
        Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Row(children: [
            Text(
                {
                      'vless': 'VLESS',
                      'warp': 'WARP',
                      'dpi': 'DPI-обход'
                    }[c['id']] ??
                    '',
                style: const TextStyle(fontWeight: FontWeight.w600)),
            const Spacer(),
            Icon(Icons.circle,
                size: 7, color: status == 'verified' ? accent : muted)
          ]),
          const SizedBox(height: 15),
          Text(label,
              style: TextStyle(
                  color: status == 'verified' ? accent : Colors.white)),
          const SizedBox(height: 6),
          Text(c['detail'] as String? ?? '',
              style: const TextStyle(color: muted, fontSize: 11, height: 1.5))
        ]),
        padding: const EdgeInsets.all(18));
  }

  Widget routeCard(Map group) {
    final id = group['id'] as String;
    final decision = map('decisions')[id] as Map?;
    final udp = map('decisions')['$id-udp'] as Map?;
    final route = decision?['route'] as String?;
    final samples = map('samples')[id] as Map?;
    final latency =
        route == null ? null : (samples?[route] as Map?)?['latency_ms'];
    return panel(
        Row(children: [
          Container(
              width: 38,
              height: 38,
              decoration: BoxDecoration(
                  color: background, borderRadius: BorderRadius.circular(11)),
              child: Icon(
                  {
                        'youtube': Icons.play_arrow_rounded,
                        'discord': Icons.headset_mic_outlined,
                        'telegram': Icons.send_outlined,
                        'ai': Icons.auto_awesome_outlined
                      }[id] ??
                      Icons.public,
                  color: muted,
                  size: 21)),
          const SizedBox(width: 14),
          Expanded(
              child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                Text(group['name'] as String,
                    style: const TextStyle(fontWeight: FontWeight.w600)),
                const SizedBox(height: 4),
                Text(
                    decision?['reason'] as String? ??
                        'Проверка ещё не выполнена',
                    style: const TextStyle(color: muted, fontSize: 11)),
                if (udp != null)
                  Text('UDP: ${routeName(udp['route'] as String)}',
                      style: const TextStyle(color: muted, fontSize: 11))
              ])),
          const SizedBox(width: 10),
          SizedBox(
              width: 140,
              child:
                  Column(crossAxisAlignment: CrossAxisAlignment.end, children: [
                Text(route == null ? '—' : routeName(route),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                        color: route == 'block' ? muted : accent,
                        fontSize: 12)),
                if (latency is num && latency > 0)
                  Padding(
                      padding: const EdgeInsets.only(top: 4),
                      child: Text('$latency мс',
                          style: const TextStyle(color: muted, fontSize: 11)))
              ]))
        ]),
        padding: const EdgeInsets.all(16));
  }

  Widget servers() =>
      Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        section('Ваши подключения',
            'Ключи хранятся на устройстве в защищённом хранилище.'),
        Wrap(spacing: 12, runSpacing: 12, children: [
          FilledButton.icon(
              onPressed: busy ? null : () => importDialog(false),
              icon: const Icon(Icons.add),
              label: const Text('Добавить VLESS / подписку')),
          OutlinedButton.icon(
              onPressed: busy || list('subscriptions').isEmpty
                  ? null
                  : () => act('refresh'),
              icon: const Icon(Icons.refresh),
              label: const Text('Обновить подписки'))
        ]),
        const SizedBox(height: 18),
        panel(Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
          const Text('Выбор VLESS-сервера',
              style: TextStyle(fontSize: 18, fontWeight: FontWeight.w600)),
          const SizedBox(height: 14),
          SegmentedButton<bool>(
            segments: const [
              ButtonSegment(value: true, label: Text('Автоматически')),
              ButtonSegment(value: false, label: Text('Выбрать сервер')),
            ],
            selected: {(state['selected_server'] as String? ?? '').isEmpty},
            showSelectedIcon: false,
            onSelectionChanged: busy || list('servers').isEmpty
                ? null
                : (values) {
                    if (values.first) {
                      act('select_server', {'id': ''});
                    } else {
                      chooseServer();
                    }
                  },
          ),
          if ((state['selected_server'] as String? ?? '').isNotEmpty) ...[
            const SizedBox(height: 12),
            OutlinedButton.icon(
                onPressed: busy ? null : chooseServer,
                icon: const Icon(Icons.dns_outlined),
                label: Text(routeName('vless-${state['selected_server']}'),
                    maxLines: 2, overflow: TextOverflow.ellipsis)),
          ],
          const SizedBox(height: 12),
          Text(
              (state['selected_server'] as String? ?? '').isEmpty
                  ? 'При подключении проверяем до трёх кандидатов. Учитываем пинг и последний рабочий сервер.'
                  : 'Для VLESS используется выбранный сервер. Закрепления сервисов в «Правилах» имеют приоритет.',
              style: const TextStyle(color: muted, fontSize: 12, height: 1.5)),
          const SizedBox(height: 18),
          Wrap(spacing: 12, runSpacing: 12, children: [
            OutlinedButton.icon(
              onPressed: busy || list('servers').isEmpty
                  ? null
                  : () => act(map('server_ping')['running'] == true
                      ? 'cancel_server_ping'
                      : 'ping_servers'),
              icon: Icon(map('server_ping')['running'] == true
                  ? Icons.stop_rounded
                  : Icons.network_ping),
              label: Text(map('server_ping')['running'] == true
                  ? 'Остановить пинг'
                  : 'Пинг серверов'),
            ),
            if (map('server_ping')['total'] is num)
              Padding(
                  padding: const EdgeInsets.symmetric(vertical: 12),
                  child: Text(
                      'Проверено ${map('server_ping')['completed']} из ${map('server_ping')['total']}',
                      style: const TextStyle(color: muted))),
          ]),
          if (map('server_ping')['running'] == true)
            Padding(
                padding: const EdgeInsets.only(top: 10),
                child: LinearProgressIndicator(
                    value: (map('server_ping')['completed'] as num? ?? 0) /
                        ((map('server_ping')['total'] as num? ?? 1) == 0
                            ? 1
                            : map('server_ping')['total'] as num))),
          const SizedBox(height: 10),
          const Text(
              'TCP-пинг проверяет доступность адреса сервера. Работу VLESS проверяем при подключении.',
              style: TextStyle(color: muted, fontSize: 11, height: 1.5)),
          if ((map('server_ping')['error'] as String? ?? '').isNotEmpty)
            Text(map('server_ping')['error'] as String),
        ])),
        const SizedBox(height: 24),
        if (list('servers').isEmpty)
          panel(const Column(children: [
            Icon(Icons.dns_outlined, size: 40, color: muted),
            SizedBox(height: 14),
            Text('Серверов пока нет'),
            SizedBox(height: 7),
            Text('Вставьте vless://, ссылку подписки или JSON sing-box.',
                textAlign: TextAlign.center, style: TextStyle(color: muted))
          ])),
        for (final server in sortedServers)
          Padding(
              padding: const EdgeInsets.only(bottom: 10),
              child: panel(
                  InkWell(
                      onTap: busy
                          ? null
                          : () => act('select_server', {'id': server['id']}),
                      child: Row(children: [
                        Icon(
                            state['selected_server'] == server['id']
                                ? Icons.check_circle
                                : Icons.radio_button_unchecked,
                            color: state['selected_server'] == server['id']
                                ? accent
                                : muted),
                        const SizedBox(width: 12),
                        Expanded(
                            child: Column(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                children: [
                              Text(server['name'] as String,
                                  maxLines: 2, overflow: TextOverflow.ellipsis),
                              const SizedBox(height: 4),
                              Text(
                                  'TCP: ${serverPingLabel(server['id'] as String)}',
                                  style: const TextStyle(
                                      color: muted, fontSize: 11)),
                            ])),
                        IconButton(
                            tooltip: 'Удалить сервер',
                            onPressed: busy || active
                                ? null
                                : () =>
                                    act('remove_server', {'id': server['id']}),
                            icon: const Icon(Icons.delete_outline, size: 18))
                      ])),
                  padding: const EdgeInsets.all(16))),
        if (list('subscriptions').isNotEmpty) ...[
          const SizedBox(height: 18),
          const Text('Подписки', style: TextStyle(fontSize: 18)),
          const SizedBox(height: 12),
          for (final sub in list('subscriptions'))
            Padding(
                padding: const EdgeInsets.only(bottom: 10),
                child: panel(
                    Row(children: [
                      Expanded(
                          child: Text(
                              '${sub['servers']} серверов · обновлено ${formatTime(sub['updated'])}',
                              style: const TextStyle(color: muted))),
                      IconButton(
                          tooltip: 'Обновить',
                          onPressed: busy
                              ? null
                              : () => act('refresh', {'id': sub['id']}),
                          icon: const Icon(Icons.refresh)),
                      IconButton(
                          tooltip: 'Удалить подписку',
                          onPressed: busy || active
                              ? null
                              : () =>
                                  act('remove_subscription', {'id': sub['id']}),
                          icon: const Icon(Icons.link_off))
                    ]),
                    padding: const EdgeInsets.all(12)))
        ],
        const SizedBox(height: 28),
        panel(Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          const Row(children: [
            Icon(Icons.cloud_outlined, color: accent),
            SizedBox(width: 10),
            Expanded(
                child: Text('Cloudflare WARP',
                    style:
                        TextStyle(fontSize: 18, fontWeight: FontWeight.w600)))
          ]),
          const SizedBox(height: 12),
          const Text(
              'Резерв при отказе VLESS. Профиль готовится автоматически в фоне; работа туннеля проверяется отдельно. Регистрация через wgcf неофициальная.',
              style: TextStyle(color: muted, height: 1.6)),
          const SizedBox(height: 18),
          if (map('warp_setup')['running'] == true) ...[
            const LinearProgressIndicator(),
            const SizedBox(height: 10),
            const Text('Подготавливаем профиль WARP…',
                style: TextStyle(color: muted)),
          ],
          if ((map('warp_setup')['notice'] as String? ?? '').isNotEmpty)
            Text(map('warp_setup')['notice'] as String,
                style: const TextStyle(color: muted)),
          Wrap(spacing: 12, runSpacing: 12, children: [
            OutlinedButton(
                onPressed: busy || map('warp_setup')['running'] == true
                    ? null
                    : registerWarp,
                child: const Text('Повторить настройку')),
            TextButton(
                onPressed: busy || active ? null : () => importDialog(true),
                child: const Text('Импорт WireGuard-профиля')),
            TextButton(
                onPressed: busy || active ? null : () => act('warp_remove'),
                child: const Text('Удалить профиль'))
          ])
        ])),
      ]);
  String formatTime(dynamic value) {
    final time = DateTime.tryParse(value?.toString() ?? '')?.toLocal();
    return time == null
        ? '—'
        : '${time.day}.${time.month.toString().padLeft(2, '0')}.${time.year} ${time.hour}:${time.minute.toString().padLeft(2, '0')}';
  }

  Future<void> importDialog(bool warp) async {
    final field = TextEditingController();
    final text = await showDialog<String>(
        context: context,
        builder: (context) => AlertDialog(
                title: Text(warp ? 'Импорт WARP' : 'Добавить подключение'),
                content: SizedBox(
                    width: 540,
                    child: Column(
                        mainAxisSize: MainAxisSize.min,
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                              warp
                                  ? 'Вставьте содержимое WireGuard-профиля.'
                                  : 'Вставьте свой ключ VLESS, HTTPS-ссылку подписки, Base64 или JSON sing-box.',
                              style:
                                  const TextStyle(color: muted, height: 1.5)),
                          const SizedBox(height: 18),
                          TextField(
                              controller: field,
                              minLines: 4,
                              maxLines: 8,
                              enableSuggestions: false,
                              autocorrect: false,
                              decoration: InputDecoration(
                                  hintText: warp
                                      ? '[Interface]\nPrivateKey = …\n[Peer]\n…'
                                      : 'vless://… или https://…'),
                              keyboardType: TextInputType.multiline)
                        ])),
                actions: [
                  TextButton(
                      onPressed: () => Navigator.pop(context),
                      child: const Text('Отмена')),
                  FilledButton(
                      onPressed: () =>
                          Navigator.pop(context, field.text.trim()),
                      child: const Text('Добавить'))
                ]));
    // Dialog text is not retained in application state or diagnostics.
    field.clear();
    if (text != null && text.isNotEmpty) {
      await act(warp ? 'warp_import' : 'import', {'text': text});
    }
    field.dispose();
  }

  Future<void> registerWarp() async {
    final accepted = await showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
                title: const Text('Настроить WARP'),
                content: const SizedBox(
                    width: 500,
                    child: Column(
                        mainAxisSize: MainAxisSize.min,
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                              'Приложение создаст устройство WARP через неофициальный API wgcf. Если прямой доступ к API недоступен, включите VLESS и повторите автонастройку. Полученный профиль применяется после переподключения.'),
                          SizedBox(height: 18),
                          Text(
                              'Нажимая «Принять и настроить», вы принимаете условия Cloudflare:'),
                          SizedBox(height: 8),
                          SelectableText(
                              'https://www.cloudflare.com/application/terms/',
                              style: TextStyle(color: accent))
                        ])),
                actions: [
                  TextButton(
                      onPressed: () => Navigator.pop(context, false),
                      child: const Text('Отмена')),
                  FilledButton(
                      onPressed: () => Navigator.pop(context, true),
                      child: const Text('Принять и настроить'))
                ]));
    if (accepted == true) await act('warp_register', {'accept_terms': true});
  }

  Widget rules() {
    if (state['routing_supported'] != true) return legacyRules();
    return Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
      if (state['catalogue_supported'] == true) RoutingCatalogue(
          backend: core, state: state, onChanged: refresh,
          busy: busy, connected: active)
      else RoutingRulesPanel(
          backend: core, state: state, onChanged: refresh,
          busy: busy, connected: active),
      const SizedBox(height: 24),
      ExpansionTile(
          title: const Text('Общие маршруты автовыбора'),
          children: [legacyRules()]),
    ]);
  }

  Widget legacyRules() =>
      Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        section('Правила сервисов',
            'Автовыбор учитывает доступность, стабильность и задержку. Можно закрепить конкретный путь.'),
        for (final group in list('groups'))
          Padding(
              padding: const EdgeInsets.only(bottom: 12),
              child: panel(Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(group['name'] as String,
                        style: const TextStyle(
                            fontWeight: FontWeight.w600, fontSize: 17)),
                    const SizedBox(height: 12),
                    DropdownButtonFormField<String>(
                        key: ValueKey('${group['id']}-$settingsRevision'),
                        initialValue: map('pins')[group['id']] as String? ?? '',
                        isExpanded: true,
                        decoration: const InputDecoration(labelText: 'Маршрут'),
                        items: routeOptions(
                            map('pins')[group['id']] as String? ?? ''),
                        onChanged: busy
                            ? null
                            : (v) => act('pin',
                                {'group': group['id'], 'route': v ?? ''})),
                    const SizedBox(height: 10),
                    Text(
                        (group['domains'] as List?)?.join(', ') ??
                            'Назначения без отдельного правила',
                        style: const TextStyle(
                            color: muted, fontSize: 11, height: 1.5))
                  ]))),
        notice(
            'Закреплённый вручную маршрут не заменяется автоматически. DPI-обход на Android работает только с TCP; для UDP нужен отдельный доступный путь.',
            Icons.info_outline),
      ]);
  Widget diagnostics() =>
      Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        section('Проверка соединения',
            'TLS, UDP/STUN и подтверждение выхода WARP проверяются независимо.'),
        Wrap(spacing: 12, runSpacing: 12, children: [
          FilledButton.icon(
              onPressed: busy || !active ? null : () => act('probe'),
              icon: const Icon(Icons.network_check),
              label: const Text('Проверить все маршруты')),
          OutlinedButton.icon(
              onPressed: busy || !active
                  ? null
                  : () => act('speed', {'group': 'default'}),
              icon: const Icon(Icons.speed),
              label: const Text('Скорость · 2 МБ')),
          OutlinedButton.icon(
              onPressed: busy ? null : copyDiagnostics,
              icon: const Icon(Icons.copy),
              label: const Text('Скопировать отчёт'))
        ]),
        const SizedBox(height: 22),
        if (map('android').isNotEmpty)
          Padding(
              padding: const EdgeInsets.only(bottom: 14),
              child: panel(Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Text('Запуск VPN на Android',
                        style: TextStyle(
                            fontSize: 17, fontWeight: FontWeight.w600)),
                    const SizedBox(height: 12),
                    SelectableText(
                        'Этап: ${map('android')['stage'] ?? 'unknown'}\n'
                        'Предыдущий этап: ${map('android')['previous_stage'] ?? 'unknown'}\n'
                        'Завершение Android: ${map('android')['exit_reason'] ?? '—'}; '
                        'код: ${map('android')['exit_status'] ?? '—'}\n'
                        'Нативный сбой: ${map('android')['native_failure'] ?? '—'}\n'
                        'Исключение: ${map('android')['exception'] ?? ''}',
                        style: const TextStyle(color: muted, height: 1.5)),
                  ]))),
        for (final group in list('groups'))
          if (map('samples')[group['id']] is Map)
            Padding(
                padding: const EdgeInsets.only(bottom: 14),
                child: panel(Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(group['name'] as String,
                          style: const TextStyle(
                              fontSize: 17, fontWeight: FontWeight.w600)),
                      const SizedBox(height: 12),
                      for (final entry in Map<String, dynamic>.from(
                              map('samples')[group['id']] as Map)
                          .entries)
                        Padding(
                            padding: const EdgeInsets.symmetric(vertical: 8),
                            child: Row(children: [
                              Expanded(
                                  child: Text(routeName(entry.key),
                                      overflow: TextOverflow.ellipsis)),
                              const SizedBox(width: 8),
                              Text(
                                  entry.value['reachable'] == true
                                      ? '${entry.value['latency_ms']} мс'
                                      : 'Нет TLS',
                                  style: TextStyle(
                                      color: entry.value['reachable'] == true
                                          ? accent
                                          : muted,
                                      fontSize: 12)),
                              const SizedBox(width: 18),
                              Text(
                                  entry.value['udp'] == true
                                      ? 'UDP ✓'
                                      : 'UDP —',
                                  style: const TextStyle(
                                      color: muted, fontSize: 12))
                            ]))
                    ]))),
        if (map('samples').isEmpty)
          panel(const Text('Результаты появятся после подключения.',
              style: TextStyle(color: muted))),
        const SizedBox(height: 18),
        notice(
            'Сетевая проверка не подтверждает воспроизведение видео, вход в аккаунт или реальный звонок. Эти сценарии проверяются в соответствующем приложении.',
            Icons.info_outline),
      ]);
  Future<void> copyDiagnostics() async {
    try {
      final data = await core.call('diagnostics');
      await Clipboard.setData(ClipboardData(
          text: const JsonEncoder.withIndent('  ').convert(data)));
      if (mounted) message('Обезличенный отчёт скопирован');
    } catch (e) {
      if (mounted) message(e.toString());
    }
  }

  Widget settings() =>
      Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        section('Настройки',
            'Управление соединением без облачного аккаунта приложения.'),
        panel(Column(children: [
          SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('Автозапуск'),
              subtitle: Text(
                  state['platform'] == 'android'
                      ? 'Восстанавливать ранее подключённый VPN после перезагрузки. Требуется разрешение VPN от Android.'
                      : 'Открывать приложение при входе в Windows. Выключение сохраняется при обновлении.',
                  style: const TextStyle(color: muted, height: 1.5)),
              value: state['autostart'] == true,
              onChanged: busy || state['autostart_supported'] != true
                  ? null
                  : (v) => act('autostart', {'value': v})),
          if (state['platform'] == 'windows') ...[
            const Divider(color: border, height: 32),
            SwitchListTile(
                contentPadding: EdgeInsets.zero,
                title: const Text('Подключаться при автозапуске'),
                subtitle: const Text(
                    'Включать подключение при входе в Windows. Нужны включённый автозапуск и добавленный сервер или профиль WARP.',
                    style: TextStyle(color: muted, height: 1.5)),
                value: state['connect_on_startup'] == true,
                onChanged: busy
                    ? null
                    : (v) => act('connect_on_startup', {'value': v})),
          ],
          const Divider(color: border, height: 32),
          SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('Автонастройка WARP'),
              subtitle: const Text(
                  'Подготавливать резервный профиль в фоне и повторять попытку при появлении рабочего VLESS.',
                  style: TextStyle(color: muted, height: 1.5)),
              value: state['auto_warp'] == true,
              onChanged: busy ? null : (v) => act('auto_warp', {'value': v})),
          const Padding(
              padding: EdgeInsets.only(top: 8),
              child: SelectableText(
                  'Условия WARP: https://www.cloudflare.com/application/terms/',
                  style: TextStyle(color: muted, fontSize: 11))),
          const Divider(color: border, height: 32),
          SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('Прямой резерв'),
              subtitle: const Text(
                  'При отказе туннелей пробовать прямой доступ. Внешний IP провайдера будет виден сервису.',
                  style: TextStyle(color: muted, height: 1.5)),
              value: state['direct_fallback'] == true,
              onChanged:
                  busy ? null : (v) => act('direct_fallback', {'value': v})),
          const Divider(color: border, height: 32),
          SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('DPI-обход'),
              subtitle: const Text(
                  'zapret2 на Windows, ограниченный TCP-режим tpws на Android. Изменяется при отключённом соединении.',
                  style: TextStyle(color: muted, height: 1.5)),
              value: state['dpi_enabled'] == true,
              onChanged: busy || active
                  ? null
                  : (v) => act('dpi_enabled', {'value': v}))
        ])),
        const SizedBox(height: 18),
        panel(const Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('Локальное хранение',
                  style: TextStyle(fontSize: 17, fontWeight: FontWeight.w600)),
              SizedBox(height: 12),
              Text(
                  'Windows DPAPI / Android Keystore\nКлючи исключены из диагностики. Подписки проверяются на обновление при подключении, если прошло 24 часа.',
                  style: TextStyle(color: muted, height: 1.7))
            ])),
        for (final warning in list('warnings'))
          Padding(
              padding: const EdgeInsets.only(top: 12),
              child: notice(warning.toString(), Icons.info_outline)),
      ]);
}
