import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'backend.dart';

const routingCategories = <String, String>{
  'apps': 'Приложения',
  'games': 'Игры',
  'launchers': 'Лаунчеры',
  'video': 'Видео',
  'social': 'Общение',
  'ai': 'AI',
  'work': 'Работа',
  'downloads': 'Загрузки',
  'other': 'Другое',
};

Future<void> showRoutingJsonImport({
  required BuildContext context,
  required String title,
  required String description,
  required Key inputKey,
  required String label,
  required Future<String?> Function(String) submit,
}) async {
  final input = TextEditingController();
  String? error;
  bool working = false;
  final dialogRoute = DialogRoute<void>(
    context: context,
    builder: (dialogContext) => StatefulBuilder(
      builder: (context, update) => AlertDialog(
        title: Text(title),
        content: SizedBox(
          width: 600,
          child: SingleChildScrollView(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(description),
                const SizedBox(height: 14),
                TextField(
                  key: inputKey,
                  controller: input,
                  minLines: 7,
                  maxLines: 12,
                  decoration: InputDecoration(labelText: label),
                ),
                if (error != null)
                  Padding(
                    padding: const EdgeInsets.only(top: 12),
                    child: Text(
                      error!,
                      style: TextStyle(
                        color: Theme.of(context).colorScheme.error,
                      ),
                    ),
                  ),
              ],
            ),
          ),
        ),
        actions: [
          TextButton(
            onPressed: working ? null : () => Navigator.pop(dialogContext),
            child: const Text('Отмена'),
          ),
          FilledButton(
            onPressed: working
                ? null
                : () async {
                    update(() {
                      working = true;
                      error = null;
                    });
                    final problem = await submit(input.text);
                    if (!dialogContext.mounted) return;
                    if (problem == null) {
                      Navigator.pop(dialogContext);
                    } else {
                      update(() {
                        working = false;
                        error = problem;
                      });
                    }
                  },
            child: Text(working ? 'Сохраняем…' : 'Импортировать'),
          ),
        ],
      ),
    ),
  );
  await Navigator.of(context, rootNavigator: true).push(dialogRoute);
  await dialogRoute.completed;
  input.dispose();
}

class RoutingRulesPanel extends StatefulWidget {
  const RoutingRulesPanel({
    super.key,
    required this.backend,
    required this.state,
    required this.onChanged,
    this.busy = false,
    this.connected = false,
  });
  final Backend backend;
  final Map<String, dynamic> state;
  final Future<void> Function() onChanged;
  final bool busy, connected;
  @override
  State<RoutingRulesPanel> createState() => _RoutingRulesPanelState();
}

class _RoutingRulesPanelState extends State<RoutingRulesPanel> {
  String query = '', category = 'all';
  bool saving = false;
  bool runningOnly = true, showSystem = true;
  @override
  void initState() {
    super.initState();
    if (widget.state['application_discovery_supported'] == true) {
      category = 'apps';
    }
  }

  List<Map<String, dynamic>> get applications =>
      ((widget.state['applications'] as List?) ?? [])
          .map((a) => Map<String, dynamic>.from(a as Map))
          .toList();

  Future<void> refreshApplications() async {
    setState(() => saving = true);
    try {
      await widget.backend.call('applications_refresh');
      await widget.onChanged();
    } catch (e) {
      if (mounted) message(e.toString());
    } finally {
      if (mounted) setState(() => saving = false);
    }
  }

  Map<String, dynamic>? applicationRule(Map<String, dynamic> app) {
    for (final rule in entries) {
      if (rule['id'] == app['rule_id']) return rule;
    }
    return null;
  }

  bool dedicatedRule(Map<String, dynamic> app, Map<String, dynamic> rule) {
    final paths = (rule['process_paths'] as List?) ?? [];
    return paths.length == 1 &&
        paths.first.toString().toLowerCase() ==
            app['path'].toString().toLowerCase() &&
        ((rule['processes'] as List?) ?? []).isEmpty &&
        ((rule['domains'] as List?) ?? []).isEmpty &&
        ((rule['ip_cidr'] as List?) ?? []).isEmpty;
  }

  Future<void> configureApplication(Map<String, dynamic> app) async {
    final existing = applicationRule(app);
    if (existing != null && dedicatedRule(app, existing)) {
      await edit(existing);
      return;
    }
    await edit({
      'id': '',
      'name': app['name'],
      'category': 'apps',
      'enabled': true,
      'route': existing?['route'] ?? 'auto',
      'auto_group': existing?['auto_group'] ?? 'default',
      'network': existing?['network'] ?? '',
      'domains': <String>[],
      'processes': <String>[],
      'process_paths': [app['path']],
      'ip_cidr': <String>[]
    }, true);
  }

  Widget applicationCard(Map<String, dynamic> app) {
    final existing = applicationRule(app);
    final running = app['running'] == true;
    final dedicated = existing != null && dedicatedRule(app, existing);
    final route = existing == null
        ? 'Общий маршрут'
        : '${existing['enabled'] == true ? '' : 'Правило выключено · '}${routeLabel(existing['route'] as String)}';
    return Container(
        key: ValueKey('application_${app['id']}'),
        margin: const EdgeInsets.only(bottom: 10),
        padding: const EdgeInsets.all(14),
        decoration: BoxDecoration(
            color: Theme.of(context).colorScheme.surface,
            border: Border.all(color: Theme.of(context).colorScheme.outline),
            borderRadius: BorderRadius.circular(16)),
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Row(children: [
            Icon(Icons.apps_outlined,
                color: running
                    ? Theme.of(context).colorScheme.primary
                    : Theme.of(context).colorScheme.onSurfaceVariant),
            const SizedBox(width: 10),
            Expanded(
                child: Text(app['name'] as String,
                    style: const TextStyle(
                        fontWeight: FontWeight.w600, fontSize: 16))),
            Text(running ? 'Запущено' : 'Не запущено',
                style: Theme.of(context).textTheme.bodySmall)
          ]),
          const SizedBox(height: 8),
          Text(route,
              style: TextStyle(
                  color: existing == null
                      ? Theme.of(context).colorScheme.onSurfaceVariant
                      : Theme.of(context).colorScheme.primary)),
          const SizedBox(height: 5),
          Text(app['path'] as String,
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: Theme.of(context).textTheme.bodySmall),
          const SizedBox(height: 10),
          Wrap(spacing: 10, runSpacing: 6, children: [
            OutlinedButton.icon(
                onPressed: locked ? null : () => configureApplication(app),
                icon: const Icon(Icons.tune),
                label:
                    Text(dedicated ? 'Изменить маршрут' : 'Настроить маршрут')),
            if (existing != null && !dedicated)
              TextButton(
                  onPressed: locked ? null : () => edit(existing),
                  child: Text('Правило: ${existing['name']}')),
            if (running && (app['process_count'] as num? ?? 0) > 1)
              Padding(
                  padding: const EdgeInsets.symmetric(vertical: 12),
                  child: Text('Процессов: ${app['process_count']}',
                      style: Theme.of(context).textTheme.bodySmall)),
          ]),
        ]));
  }

  List<Map<String, dynamic>> get entries =>
      ((widget.state['routing_rules'] as List?) ?? [])
          .map((e) => Map<String, dynamic>.from(e as Map))
          .toList();
  int get revision => (widget.state['routing_revision'] as num?)?.toInt() ?? 0;
  bool get locked => saving || widget.busy;
  void message(String value) => ScaffoldMessenger.of(context)
      .showSnackBar(SnackBar(content: Text(value)));

  Future<String?> save(
    List<Map<String, dynamic>> rules,
    int baseRevision, {
    String action = 'routing_save',
  }) async {
    setState(() => saving = true);
    try {
      await widget.backend.call(action, {
        'rules': rules,
        'rules_revision': baseRevision,
      });
      await widget.onChanged();
      return null;
    } catch (e) {
      return e.toString();
    } finally {
      if (mounted) setState(() => saving = false);
    }
  }

  String routeLabel(String value) {
    const labels = {
      'auto': 'Автоматически',
      'vpn': 'VPN автоматически',
      'direct': 'Напрямую',
      'warp': 'WARP',
      'dpi': 'DPI · TCP',
      'block': 'Блокировать',
    };
    if (labels.containsKey(value)) return labels[value]!;
    for (final server in (widget.state['servers'] as List?) ?? []) {
      if (server['route'] == value) return server['name'] as String;
    }
    return 'Недоступный сервер · блокировка';
  }

  List<DropdownMenuItem<String>> routeItems(String selected) {
    final items = <DropdownMenuItem<String>>[
      for (final value in ['direct', 'auto', 'vpn', 'warp', 'dpi', 'block'])
        DropdownMenuItem(value: value, child: Text(routeLabel(value))),
      for (final server in (widget.state['servers'] as List?) ?? [])
        DropdownMenuItem(
          value: server['route'] as String,
          child: Text(
            server['name'] as String,
            overflow: TextOverflow.ellipsis,
          ),
        ),
    ];
    if (!items.any((item) => item.value == selected)) {
      items.add(
        DropdownMenuItem(value: selected, child: Text(routeLabel(selected))),
      );
    }
    return items;
  }

  Future<void> persistAndNotify(
    List<Map<String, dynamic>> rules,
    int baseRevision,
  ) async {
    final error = await save(rules, baseRevision);
    if (!mounted) return;
    message(
      error ??
          (widget.connected
              ? 'Сохранено. Нажмите «Применить» для переподключения.'
              : 'Правила сохранены'),
    );
  }

  Future<bool> confirm(String title, String description) async =>
      await showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
          title: Text(title),
          content: Text(description),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context, false),
              child: const Text('Отмена'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(context, true),
              child: const Text('Подтвердить'),
            ),
          ],
        ),
      ) ??
      false;

  Future<void> reset() async {
    final baseRevision = revision;
    if (!await confirm(
      'Восстановить стартовый список?',
      'Ваши изменения и добавленные правила будут заменены стартовыми. При работающем VPN потребуется нажать «Применить».',
    )) {
      return;
    }
    if (!mounted) return;
    final error = await save([], baseRevision, action: 'routing_reset');
    if (mounted) message(error ?? 'Стартовые правила восстановлены');
  }

  Future<void> exportRules() async {
    final text = const JsonEncoder.withIndent('  ')
        .convert({'schema': 1, 'rules': entries});
    await Clipboard.setData(ClipboardData(text: text));
    if (mounted) {
      message('Список правил скопирован в JSON. Ключей VPN в нём нет.');
    }
  }

  Future<void> importRules() async {
    final baseRevision = revision;
    await showRoutingJsonImport(
      context: context,
      title: 'Импорт правил',
      description:
          'Вставьте JSON, скопированный из редактора. Импорт заменит весь сохранённый список.',
      inputKey: const ValueKey('routing_import_json'),
      label: 'JSON со списком правил',
      submit: (text) async {
        try {
          if (text.length > 2 * 1024 * 1024) {
            throw const FormatException();
          }
          final document = jsonDecode(text);
          if (document is! Map ||
              document['schema'] != 1 ||
              document['rules'] is! List) {
            throw const FormatException();
          }
          final rules = (document['rules'] as List)
              .map((e) => Map<String, dynamic>.from(e as Map))
              .toList();
          return await save(rules, baseRevision);
        } catch (_) {
          return 'Нужен JSON со schema: 1 и массивом rules';
        }
      },
    );
  }

  Future<void> edit([
    Map<String, dynamic>? source,
    bool duplicate = false,
  ]) async {
    final snapshot = entries;
    final baseRevision = revision;
    final form = GlobalKey<FormState>();
    final name = TextEditingController(text: source?['name'] as String? ?? '');
    final domains = TextEditingController(
      text: ((source?['domains'] as List?) ?? []).join('\n'),
    );
    final processes = TextEditingController(
      text: ((source?['processes'] as List?) ?? []).join('\n'),
    );
    final paths = TextEditingController(
      text: ((source?['process_paths'] as List?) ?? []).join('\n'),
    );
    final ips = TextEditingController(
      text: ((source?['ip_cidr'] as List?) ?? []).join('\n'),
    );
    var target = source?['route'] as String? ?? 'direct';
    var group = source?['auto_group'] as String? ?? 'default';
    var kind = source?['category'] as String? ?? 'other';
    var network = source?['network'] as String? ?? '';
    var enabled = source?['enabled'] as bool? ?? true;
    bool working = false;
    String? error;
    List<String> split(String value, {bool linesOnly = false}) => value
        .split(linesOnly ? RegExp(r'[\r\n]+') : RegExp(r'[\s,;]+'))
        .map((s) => s.trim())
        .where((s) => s.isNotEmpty)
        .toList();
    final dialogRoute = DialogRoute<void>(
      context: context,
      barrierDismissible: false,
      builder: (dialogContext) => StatefulBuilder(
        builder: (context, update) {
          Widget multiline(
            String key,
            TextEditingController controller,
            String label,
            String hint,
          ) =>
              Padding(
                padding: const EdgeInsets.only(top: 14),
                child: TextFormField(
                  key: ValueKey(key),
                  controller: controller,
                  minLines: 2,
                  maxLines: 4,
                  decoration: InputDecoration(
                    labelText: label,
                    hintText: hint,
                    alignLabelWithHint: true,
                  ),
                ),
              );
          return AlertDialog(
            title: Text(
              source == null || duplicate
                  ? 'Добавить правило'
                  : 'Редактировать правило',
            ),
            content: SizedBox(
              width: 640,
              child: SingleChildScrollView(
                child: Form(
                  key: form,
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      TextFormField(
                        key: const ValueKey('rule_name'),
                        controller: name,
                        autofocus: true,
                        maxLength: 100,
                        decoration: const InputDecoration(
                          labelText: 'Название',
                        ),
                        validator: (s) => s == null || s.trim().isEmpty
                            ? 'Укажите название'
                            : null,
                      ),
                      const SizedBox(height: 8),
                      DropdownButtonFormField<String>(
                        initialValue: kind,
                        isExpanded: true,
                        decoration: const InputDecoration(
                          labelText: 'Категория',
                        ),
                        items: [
                          for (final e in routingCategories.entries)
                            DropdownMenuItem(
                              value: e.key,
                              child: Text(e.value),
                            ),
                        ],
                        onChanged:
                            working ? null : (v) => update(() => kind = v!),
                      ),
                      const SizedBox(height: 14),
                      DropdownButtonFormField<String>(
                        key: const ValueKey('rule_route'),
                        initialValue: target,
                        isExpanded: true,
                        decoration: const InputDecoration(labelText: 'Маршрут'),
                        items: routeItems(target),
                        onChanged: working
                            ? null
                            : (v) => update(() {
                                  target = v!;
                                  if (target == 'dpi') network = 'tcp';
                                }),
                      ),
                      if (target == 'vpn')
                        const Padding(
                          padding: EdgeInsets.only(top: 8),
                          child: Text(
                            'Рабочий VLESS, затем WARP. Без прямого резерва: если туннеля нет, соединение блокируется.',
                          ),
                        ),
                      if (target == 'direct')
                        const Padding(
                          padding: EdgeInsets.only(top: 8),
                          child: Text(
                            'Соединение идёт через вашего интернет-провайдера.',
                          ),
                        ),
                      if (target == 'dpi')
                        const Padding(
                          padding: EdgeInsets.only(top: 8),
                          child: Text(
                            'DPI-обход обрабатывает TCP на портах 80/443. Игровой UDP через него не идёт.',
                          ),
                        ),
                      if (target == 'warp' &&
                          !((widget.state['components'] as List?) ?? []).any(
                            (c) =>
                                c['id'] == 'warp' &&
                                c['state'] != 'unconfigured',
                          ))
                        const Padding(
                          padding: EdgeInsets.only(top: 8),
                          child: Text(
                            'WARP ещё не настроен. До его настройки этот маршрут будет блокироваться.',
                          ),
                        ),
                      if (target == 'auto')
                        Padding(
                          padding: const EdgeInsets.only(top: 14),
                          child: DropdownButtonFormField<String>(
                            initialValue: group,
                            isExpanded: true,
                            decoration: const InputDecoration(
                              labelText: 'Профиль автовыбора',
                            ),
                            items: [
                              for (final e in const {
                                'default': 'Остальной интернет',
                                'youtube': 'YouTube',
                                'discord': 'Discord',
                                'telegram': 'Telegram',
                                'ai': 'AI-сервисы',
                              }.entries)
                                DropdownMenuItem(
                                  value: e.key,
                                  child: Text(e.value),
                                ),
                            ],
                            onChanged: working
                                ? null
                                : (v) => update(() => group = v!),
                          ),
                        ),
                      const SizedBox(height: 14),
                      DropdownButtonFormField<String>(
                        key: ValueKey('rule_network_$network'),
                        initialValue: network,
                        isExpanded: true,
                        decoration: const InputDecoration(
                          labelText: 'Протокол',
                        ),
                        items: [
                          for (final e in const {
                            '': 'TCP + UDP',
                            'tcp': 'Только TCP',
                            'udp': 'Только UDP',
                          }.entries)
                            DropdownMenuItem(
                              value: e.key,
                              child: Text(e.value),
                            ),
                        ],
                        onChanged: working || target == 'dpi'
                            ? null
                            : (v) => update(() => network = v!),
                      ),
                      const SizedBox(height: 18),
                      const Text(
                        'Условия совпадения · достаточно любого',
                        style: TextStyle(fontWeight: FontWeight.w600),
                      ),
                      multiline(
                        'rule_processes',
                        processes,
                        'Имена процессов · по одному на строку',
                        'dota2.exe\ncs2.exe',
                      ),
                      multiline(
                        'rule_domains',
                        domains,
                        'Домены и поддомены',
                        'example.com\n*.example.org',
                      ),
                      multiline(
                        'rule_paths',
                        paths,
                        'Полные пути программ · по одному на строку',
                        r'C:\Games\game.exe',
                      ),
                      multiline(
                        'rule_ips',
                        ips,
                        'IP-адреса и подсети CIDR',
                        '203.0.113.5\n203.0.113.0/24',
                      ),
                      SwitchListTile(
                        contentPadding: EdgeInsets.zero,
                        title: const Text('Правило включено'),
                        value: enabled,
                        onChanged:
                            working ? null : (v) => update(() => enabled = v),
                      ),
                      const Text(
                        'Сверху списка — больший приоритет. Локальная сеть остаётся напрямую.',
                      ),
                      if (widget.connected)
                        const Padding(
                          padding: EdgeInsets.only(top: 8),
                          child: Text(
                            'После сохранения нажмите «Применить» в списке правил. VPN переподключится.',
                          ),
                        ),
                      if (error != null)
                        Padding(
                          padding: const EdgeInsets.only(top: 12),
                          child: Text(
                            error!,
                            style: TextStyle(
                              color: Theme.of(context).colorScheme.error,
                            ),
                          ),
                        ),
                    ],
                  ),
                ),
              ),
            ),
            actions: [
              TextButton(
                onPressed: working ? null : () => Navigator.pop(dialogContext),
                child: const Text('Отмена'),
              ),
              FilledButton(
                key: const ValueKey('rule_save'),
                onPressed: working
                    ? null
                    : () async {
                        if (!form.currentState!.validate()) return;
                        final rule = <String, dynamic>{
                          'id': duplicate ? '' : source?['id'] ?? '',
                          'name': name.text.trim(),
                          'category': kind,
                          'enabled': enabled,
                          'route': target,
                          'auto_group': group,
                          'network': network,
                          'domains': split(domains.text),
                          'processes': split(processes.text, linesOnly: true),
                          'process_paths': split(paths.text, linesOnly: true),
                          'ip_cidr': split(ips.text),
                        };
                        final next = List<Map<String, dynamic>>.from(snapshot);
                        final index = source == null || duplicate
                            ? -1
                            : next.indexWhere((r) => r['id'] == source['id']);
                        if (index < 0) {
                          next.insert(0, rule);
                        } else {
                          next[index] = rule;
                        }
                        update(() {
                          working = true;
                          error = null;
                        });
                        final problem = await save(next, baseRevision);
                        if (!dialogContext.mounted) return;
                        if (problem == null) {
                          Navigator.pop(dialogContext);
                        } else {
                          update(() {
                            working = false;
                            error = problem;
                          });
                        }
                      },
                child: Text(working ? 'Сохраняем…' : 'Сохранить'),
              ),
            ],
          );
        },
      ),
    );
    await Navigator.of(context, rootNavigator: true).push(dialogRoute);
    await dialogRoute.completed;
    for (final controller in [name, domains, processes, paths, ips]) {
      controller.dispose();
    }
  }

  Future<void> ruleAction(String action, Map<String, dynamic> rule) async {
    if (action == 'edit') {
      await edit(rule);
      return;
    }
    if (action == 'copy') {
      await edit(rule, true);
      return;
    }
    final next = entries;
    final baseRevision = revision;
    final index = next.indexWhere((r) => r['id'] == rule['id']);
    if (index < 0) return;
    if (action == 'delete') {
      if (!await confirm('Удалить правило?', rule['name'] as String)) return;
      next.removeAt(index);
    } else {
      final destination = action == 'up' ? index - 1 : index + 1;
      if (destination < 0 || destination >= next.length) return;
      final item = next.removeAt(index);
      next.insert(destination, item);
    }
    if (mounted) await persistAndNotify(next, baseRevision);
  }

  @override
  Widget build(BuildContext context) {
    final all = entries;
    final needle = query.toLowerCase();
    final filtered = all
        .where(
          (r) =>
              (category == 'all' || r['category'] == category) &&
              [
                r['name'],
                ...(r['domains'] as List? ?? []),
                ...(r['processes'] as List? ?? []),
                ...(r['process_paths'] as List? ?? []),
                ...(r['ip_cidr'] as List? ?? []),
              ].join(' ').toLowerCase().contains(needle),
        )
        .toList();
    final discovery = widget.state['application_discovery_supported'] == true;
    final showApplications =
        discovery && (category == 'all' || category == 'apps');
    final detected = applications
        .where((a) =>
            (!runningOnly || a['running'] == true) &&
            (showSystem || a['system'] != true) &&
            '${a['name']} ${a['executable']} ${a['path']}'
                .toLowerCase()
                .contains(needle))
        .toList();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const Text(
          'Маршруты для игр, сайтов и программ',
          style: TextStyle(fontSize: 23, fontWeight: FontWeight.w600),
        ),
        const SizedBox(height: 8),
        const Text(
          'Первое включённое совпадение побеждает.',
        ),
        const SizedBox(height: 18),
        Wrap(
          spacing: 10,
          runSpacing: 10,
          children: [
            FilledButton.icon(
              key: const ValueKey('routing_add'),
              onPressed: locked ? null : () => edit(),
              icon: const Icon(Icons.add),
              label: const Text('Добавить правило'),
            ),
            if (discovery)
              OutlinedButton.icon(
                  onPressed: locked ? null : refreshApplications,
                  icon: const Icon(Icons.refresh),
                  label: const Text('Обновить приложения')),
            OutlinedButton.icon(
              onPressed: locked ? null : importRules,
              icon: const Icon(Icons.download_outlined),
              label: const Text('Импорт JSON'),
            ),
            OutlinedButton.icon(
              onPressed: locked ? null : exportRules,
              icon: const Icon(Icons.copy_outlined),
              label: const Text('Копировать JSON'),
            ),
            TextButton(
              onPressed: locked ? null : reset,
              child: const Text('Стартовый список'),
            ),
          ],
        ),
        if (widget.state['routing_pending'] == true)
          Container(
            margin: const EdgeInsets.only(top: 16),
            padding: const EdgeInsets.all(16),
            decoration: BoxDecoration(
              border: Border.all(color: Theme.of(context).colorScheme.primary),
              borderRadius: BorderRadius.circular(14),
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const Text(
                  'Есть сохранённые изменения. Для применения VPN переподключится.',
                ),
                const SizedBox(height: 10),
                FilledButton.icon(
                  onPressed: locked
                      ? null
                      : () async {
                          setState(() => saving = true);
                          try {
                            await widget.backend.call('routing_apply');
                            await widget.onChanged();
                          } catch (e) {
                            if (mounted) message(e.toString());
                          } finally {
                            if (mounted) setState(() => saving = false);
                          }
                        },
                  icon: const Icon(Icons.refresh),
                  label: const Text('Применить'),
                ),
              ],
            ),
          ),
        const SizedBox(height: 20),
        TextField(
          key: const ValueKey('routing_search'),
          decoration: const InputDecoration(
            prefixIcon: Icon(Icons.search),
            labelText: 'Найти игру, программу, домен или IP',
          ),
          onChanged: (value) => setState(() => query = value),
        ),
        const SizedBox(height: 12),
        Wrap(
          spacing: 8,
          runSpacing: 6,
          children: [
            for (final e in {'all': 'Все', ...routingCategories}.entries)
              ChoiceChip(
                label: Text(e.value),
                selected: category == e.key,
                selectedColor: Theme.of(context)
                    .colorScheme
                    .primary
                    .withValues(alpha: .16),
                checkmarkColor: Theme.of(context).colorScheme.primary,
                labelStyle:
                    TextStyle(color: Theme.of(context).colorScheme.onSurface),
                onSelected: (_) => setState(() => category = e.key),
              ),
          ],
        ),
        const SizedBox(height: 12),
        if (showApplications) ...[
          Wrap(spacing: 8, runSpacing: 6, children: [
            FilterChip(
                label: const Text('Только запущенные'),
                selected: runningOnly,
                onSelected: (v) => setState(() => runningOnly = v)),
            FilterChip(
                label: const Text('Системные'),
                selected: showSystem,
                onSelected: (v) => setState(() => showSystem = v)),
          ]),
          const SizedBox(height: 12),
          Text('Приложений: ${detected.length}',
              style: Theme.of(context).textTheme.bodySmall),
          const SizedBox(height: 10),
          if ((widget.state['applications_error'] as String? ?? '').isNotEmpty)
            Text(widget.state['applications_error'] as String),
          if (detected.isEmpty)
            const Padding(
                padding: EdgeInsets.symmetric(vertical: 16),
                child: Text(
                    'Подходящих приложений пока нет. Список обновляется автоматически.')),
          for (final app in detected) applicationCard(app),
          const SizedBox(height: 14),
        ],
        if (category != 'apps' || filtered.isNotEmpty)
          Text(
            'Правил: ${filtered.length} из ${all.length} · включено ${all.where((r) => r['enabled'] == true).length}',
            style: Theme.of(context).textTheme.bodySmall,
          ),
        const SizedBox(height: 12),
        if (saving) const LinearProgressIndicator(),
        if (filtered.isEmpty && category != 'apps')
          const Padding(
            padding: EdgeInsets.all(24),
            child: Text(
              'Правила не найдены. Добавьте новое или измените фильтр.',
            ),
          ),
        for (final rule in filtered)
          Container(
            key: ValueKey('routing_rule_${rule['id']}'),
            margin: const EdgeInsets.only(bottom: 10),
            padding: const EdgeInsets.all(14),
            decoration: BoxDecoration(
              color: Theme.of(context).colorScheme.surface,
              border: Border.all(color: Theme.of(context).colorScheme.outline),
              borderRadius: BorderRadius.circular(16),
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Switch(
                      value: rule['enabled'] == true,
                      onChanged: locked
                          ? null
                          : (value) {
                              final next = entries;
                              next.firstWhere(
                                (r) => r['id'] == rule['id'],
                              )['enabled'] = value;
                              persistAndNotify(next, revision);
                            },
                    ),
                    const SizedBox(width: 8),
                    Expanded(
                      child: InkWell(
                        onTap: locked ? null : () => edit(rule),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              '${all.indexWhere((r) => r['id'] == rule['id']) + 1}. ${rule['name']}',
                              style: const TextStyle(
                                fontWeight: FontWeight.w600,
                                fontSize: 16,
                              ),
                            ),
                            const SizedBox(height: 4),
                            Text(
                              '${routingCategories[rule['category']] ?? 'Другое'} · ${rule['network'] == 'tcp' ? 'TCP' : rule['network'] == 'udp' ? 'UDP' : 'TCP + UDP'}',
                              style: Theme.of(context).textTheme.bodySmall,
                            ),
                          ],
                        ),
                      ),
                    ),
                    PopupMenuButton<String>(
                      tooltip: 'Действия с правилом',
                      enabled: !locked,
                      onSelected: (value) => ruleAction(value, rule),
                      itemBuilder: (context) => [
                        const PopupMenuItem(
                          value: 'edit',
                          child: Text('Редактировать'),
                        ),
                        const PopupMenuItem(
                          value: 'copy',
                          child: Text('Дублировать'),
                        ),
                        PopupMenuItem(
                          value: 'up',
                          enabled: all.first['id'] != rule['id'],
                          child: const Text('Выше по приоритету'),
                        ),
                        PopupMenuItem(
                          value: 'down',
                          enabled: all.last['id'] != rule['id'],
                          child: const Text('Ниже по приоритету'),
                        ),
                        const PopupMenuItem(
                          value: 'delete',
                          child: Text('Удалить'),
                        ),
                      ],
                    ),
                  ],
                ),
                Padding(
                  padding: const EdgeInsets.only(left: 8, top: 10),
                  child: Text(
                    routeLabel(rule['route'] as String),
                    style: TextStyle(
                      color: Theme.of(context).colorScheme.primary,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                ),
                Padding(
                  padding: const EdgeInsets.only(left: 8, top: 6),
                  child: Text(
                    [
                      ...(rule['processes'] as List? ?? []),
                      ...(rule['domains'] as List? ?? []),
                      ...(rule['process_paths'] as List? ?? []),
                      ...(rule['ip_cidr'] as List? ?? []),
                    ].join(' · '),
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                ),
              ],
            ),
          ),
        const SizedBox(height: 8),
        const Text(
          'Условия внутри правила работают как «ИЛИ». Домены включают поддомены. Локальная сеть всегда остаётся напрямую. Недоступный явно выбранный маршрут блокируется.',
        ),
      ],
    );
  }
}
