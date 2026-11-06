import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'backend.dart';
import 'routing_editor.dart';

class RoutingCatalogue extends StatefulWidget {
  const RoutingCatalogue(
      {super.key,
      required this.backend,
      required this.state,
      required this.onChanged,
      this.busy = false,
      this.connected = false});
  final Backend backend;
  final Map<String, dynamic> state;
  final Future<void> Function() onChanged;
  final bool busy, connected;
  @override
  State<RoutingCatalogue> createState() => _RoutingCatalogueState();
}

class _RoutingCatalogueState extends State<RoutingCatalogue> {
  String query = '', kind = 'all', origin = 'all';
  bool working = false, runningOnly = false, advanced = false;
  bool get locked => working || widget.busy;
  int get revision => (widget.state['routing_revision'] as num?)?.toInt() ?? 0;
  List<Map<String, dynamic>> get rows =>
      ((widget.state['catalogue'] as List?) ?? [])
          .map((e) => Map<String, dynamic>.from(e as Map))
          .toList();
  Map<String, dynamic> definition(Map<String, dynamic> e) =>
      Map<String, dynamic>.from(e['definition'] as Map);
  String label(String value) {
    const labels = {
      'inherit': 'По общим правилам',
      'auto': 'Автоматически',
      'vpn': 'VPN автоматически',
      'direct': 'Напрямую',
      'warp': 'WARP',
      'dpi': 'DPI · только TCP',
      'block': 'Блокировать'
    };
    if (labels.containsKey(value)) return labels[value]!;
    for (final s in widget.state['servers'] as List? ?? []) {
      if (s['route'] == value) return s['name'] as String;
    }
    return 'Недоступный сервер · блокировка';
  }

  void notify(String text) =>
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(text)));
  Future<String?> call(String action,
      [Map<String, dynamic> args = const {}]) async {
    setState(() => working = true);
    try {
      await widget.backend.call(action, args);
      await widget.onChanged();
      return null;
    } catch (e) {
      return e.toString();
    } finally {
      if (mounted) setState(() => working = false);
    }
  }

  Future<void> exportCatalogue() async {
    setState(() => working = true);
    try {
      final document = await widget.backend.call('catalogue_export');
      await Clipboard.setData(ClipboardData(text: jsonEncode(document)));
      if (mounted) notify('Каталог скопирован. В JSON могут быть пути к EXE.');
    } catch (e) {
      if (mounted) notify(e.toString());
    } finally {
      if (mounted) setState(() => working = false);
    }
  }

  Future<void> importCatalogue() async {
    final baseRevision = revision;
    await showRoutingJsonImport(
      context: context,
      title: 'Импорт каталога',
      description:
          'Вставьте JSON из меню «Перенос каталога». Импорт заменит сохранённые записи и правила.',
      inputKey: const ValueKey('catalogue_import_json'),
      label: 'JSON каталога',
      submit: (text) async {
        if (utf8.encode(text).length > 4 * 1024 * 1024) {
          return 'Размер JSON превышает 4 МБ';
        }
        dynamic document;
        try {
          document = jsonDecode(text);
        } on FormatException {
          return 'Не удалось прочитать JSON каталога';
        }
        if (document is! Map<String, dynamic> ||
            document['format'] != 'marshrut-routing-catalogue') {
          return 'Нужен JSON, скопированный через «Перенос каталога»';
        }
        return call('catalogue_import', {
          'catalogue': document,
          'rules_revision': baseRevision,
        });
      },
    );
  }

  // Searching a server list keeps even large subscriptions usable on a small window.
  Future<String?> pickRoute(String current) async {
    String search = '';
    return showDialog<String>(
        context: context,
        builder: (dialogContext) => StatefulBuilder(builder: (context, update) {
              final targets = <Map<String, String>>[
                for (final value in [
                  'inherit',
                  'direct',
                  'auto',
                  'vpn',
                  'warp',
                  'dpi',
                  'block'
                ])
                  {'route': value, 'name': label(value)},
                for (final s in widget.state['servers'] as List? ?? [])
                  {'route': s['route'] as String, 'name': s['name'] as String},
              ];
              final matches = targets
                  .where((s) =>
                      s['name']!.toLowerCase().contains(search.toLowerCase()))
                  .toList();
              return AlertDialog(
                  title: const Text('Выбрать маршрут'),
                  content: SizedBox(
                      width: 540,
                      height: 430,
                      child: Column(children: [
                        TextField(
                            key: const ValueKey('catalogue_route_search'),
                            autofocus: true,
                            decoration: const InputDecoration(
                                prefixIcon: Icon(Icons.search),
                                labelText: 'Маршрут или сервер'),
                            onChanged: (v) => update(() => search = v)),
                        const SizedBox(height: 12),
                        Expanded(
                            child: ListView.builder(
                                itemCount: matches.length,
                                itemBuilder: (context, i) {
                                  final s = matches[i];
                                  return ListTile(
                                      key: ValueKey('target_${s['route']}'),
                                      title: Text(s['name']!),
                                      trailing: s['route'] == current
                                          ? const Icon(Icons.check)
                                          : null,
                                      onTap: () => Navigator.pop(
                                          dialogContext, s['route']));
                                })),
                      ])),
                  actions: [
                    TextButton(
                        onPressed: () => Navigator.pop(dialogContext),
                        child: const Text('Отмена'))
                  ]);
            }));
  }

  Future<void> changeRoute(Map<String, dynamic> row) async {
    final base = revision;
    final target = await pickRoute(row['route'] as String);
    if (target == null || !mounted || target == row['route']) return;
    final error = await call('catalogue_save',
        {'id': row['id'], 'route': target, 'rules_revision': base});
    if (mounted) {
      notify(error ??
          (widget.connected
              ? 'Сохранено. Примените изменения для переподключения.'
              : 'Маршрут сохранён'));
    }
  }

  Future<void> edit([Map<String, dynamic>? row]) async {
    final d = row == null ? <String, dynamic>{} : definition(row);
    final name = TextEditingController(text: d['name'] as String? ?? '');
    final values = [
      ...(d['domains'] as List? ?? []),
      ...(d['ip_cidr'] as List? ?? [])
    ];
    final address = TextEditingController(
        text: values.isEmpty ? '' : values.first as String);
    final simpleAddress = row == null ||
        (row['origin'] == 'custom' &&
            values.length == 1 &&
            (d['processes'] as List? ?? []).isEmpty &&
            (d['process_paths'] as List? ?? []).isEmpty);
    String target = row?['route'] as String? ?? 'inherit';
    final base = revision;
    bool saving = false;
    String? error;
    final dialog = DialogRoute<void>(
        context: context,
        barrierDismissible: false,
        builder: (dialogContext) => StatefulBuilder(
            builder: (context, update) => AlertDialog(
                    title: Text(row == null
                        ? 'Добавить сайт или IP'
                        : 'Изменить запись'),
                    content: SizedBox(
                        width: 540,
                        child: SingleChildScrollView(
                            child: Column(
                                mainAxisSize: MainAxisSize.min,
                                crossAxisAlignment: CrossAxisAlignment.stretch,
                                children: [
                              if (simpleAddress) ...[
                                TextField(
                                    key: const ValueKey('catalogue_address'),
                                    controller: address,
                                    autofocus: true,
                                    decoration: const InputDecoration(
                                        labelText:
                                            'Ссылка, домен, IP или подсеть',
                                        hintText:
                                            'https://example.com или 203.0.113.0/24')),
                                const SizedBox(height: 8),
                                const Text(
                                    'Для ссылки применяется весь домен и его поддомены, без учёта пути страницы.'),
                                const SizedBox(height: 16),
                              ],
                              TextField(
                                  key: const ValueKey('catalogue_name'),
                                  controller: name,
                                  maxLength: 100,
                                  decoration: const InputDecoration(
                                      labelText: 'Название',
                                      hintText:
                                          'Можно оставить пустым для нового адреса')),
                              const SizedBox(height: 12),
                              OutlinedButton.icon(
                                  onPressed: saving
                                      ? null
                                      : () async {
                                          final selected =
                                              await pickRoute(target);
                                          if (selected != null &&
                                              dialogContext.mounted) {
                                            update(() => target = selected);
                                          }
                                        },
                                  icon: const Icon(Icons.alt_route),
                                  label: Text(label(target))),
                              const SizedBox(height: 8),
                              const Text(
                                  '«По общим правилам» сохраняет запись без отдельного маршрута. VPN автоматически не использует прямой резерв.'),
                              if (error != null)
                                Padding(
                                    padding: const EdgeInsets.only(top: 12),
                                    child: Text(error!,
                                        style: TextStyle(
                                            color: Theme.of(context)
                                                .colorScheme
                                                .error))),
                            ]))),
                    actions: [
                      TextButton(
                          onPressed: saving
                              ? null
                              : () => Navigator.pop(dialogContext),
                          child: const Text('Отмена')),
                      FilledButton(
                          onPressed: saving
                              ? null
                              : () async {
                                  if (simpleAddress &&
                                      address.text.trim().isEmpty) {
                                    update(() => error = 'Укажите адрес');
                                    return;
                                  }
                                  update(() {
                                    saving = true;
                                    error = null;
                                  });
                                  final problem = await call('catalogue_save', {
                                    if (row != null) 'id': row['id'],
                                    if (simpleAddress)
                                      'text': address.text.trim(),
                                    'name': name.text.trim(),
                                    'route': target,
                                    'rules_revision': base,
                                  });
                                  if (!dialogContext.mounted) return;
                                  if (problem == null) {
                                    Navigator.pop(dialogContext);
                                  } else {
                                    update(() {
                                      saving = false;
                                      error = problem;
                                    });
                                  }
                                },
                          child: Text(saving ? 'Сохраняем…' : 'Сохранить')),
                    ])));
    await Navigator.of(context, rootNavigator: true).push(dialog);
    await dialog.completed;
    name.dispose();
    address.dispose();
  }

  Future<void> remove(Map<String, dynamic> row) async {
    final base = revision;
    final accepted = await showDialog<bool>(
        context: context,
        builder: (ctx) => AlertDialog(
                title: const Text('Удалить запись?'),
                content: Text(
                    '«${definition(row)['name']}» и её отдельное правило будут удалены. После этого действуют оставшиеся общие правила.'),
                actions: [
                  TextButton(
                      onPressed: () => Navigator.pop(ctx, false),
                      child: const Text('Отмена')),
                  FilledButton(
                      onPressed: () => Navigator.pop(ctx, true),
                      child: const Text('Удалить'))
                ]));
    if (accepted != true || !mounted) return;
    final error = await call(
        'catalogue_delete', {'id': row['id'], 'rules_revision': base});
    if (mounted) notify(error ?? 'Запись удалена');
  }

  Future<void> explain(Map<String, dynamic> row) async {
    final input = TextEditingController();
    Map<String, dynamic>? result;
    String? error;
    bool loading = true;
    try {
      result = await widget.backend.call('routing_preview', {'id': row['id']});
    } catch (e) {
      error = e.toString();
    }
    loading = false;
    if (!mounted) {
      input.dispose();
      return;
    }
    final dialog = DialogRoute<void>(
        context: context,
        builder: (ctx) => StatefulBuilder(builder: (context, update) {
              Widget decision(Map value, String heading) => Padding(
                  padding: const EdgeInsets.only(top: 8),
                  child: Text(
                      '$heading: ${label(value['route'] as String)}\nПравило: ${value['rule_name']}\n${value['reason']}${(value['shadowed'] as num? ?? 0) > 0 ? '\nСовпадений ниже по приоритету: ${value['shadowed']}' : ''}'));
              return AlertDialog(
                  title: const Text('Почему этот маршрут?'),
                  content: SizedBox(
                      width: 620,
                      child: SingleChildScrollView(
                          child: Column(
                              mainAxisSize: MainAxisSize.min,
                              crossAxisAlignment: CrossAxisAlignment.stretch,
                              children: [
                            Text(definition(row)['name'] as String,
                                style: const TextStyle(
                                    fontWeight: FontWeight.w600)),
                            const SizedBox(height: 12),
                            TextField(
                                key:
                                    const ValueKey('catalogue_preview_address'),
                                controller: input,
                                decoration: InputDecoration(
                                    labelText: row['application'] == null
                                        ? 'Проверить другой сайт или IP'
                                        : 'Сайт или IP для этого приложения')),
                            TextButton(
                                onPressed: loading
                                    ? null
                                    : () async {
                                        update(() {
                                          loading = true;
                                          error = null;
                                        });
                                        try {
                                          final response = await widget.backend
                                              .call('routing_preview', {
                                            'id': row['id'],
                                            'text': input.text.trim()
                                          });
                                          if (ctx.mounted) {
                                            update(() => result = response);
                                          }
                                        } catch (e) {
                                          if (ctx.mounted) {
                                            update(() => error = e.toString());
                                          }
                                        } finally {
                                          if (ctx.mounted) {
                                            update(() => loading = false);
                                          }
                                        }
                                      },
                                child: const Text('Проверить')),
                            if (loading) const LinearProgressIndicator(),
                            if (error != null)
                              Text(error!,
                                  style: TextStyle(
                                      color:
                                          Theme.of(context).colorScheme.error)),
                            for (final sample
                                in result?['results'] as List? ?? []) ...[
                              const Divider(),
                              Text(sample['target'] as String,
                                  style: const TextStyle(
                                      fontWeight: FontWeight.w600)),
                              for (final network in ['tcp', 'udp']) ...[
                                decision(sample[network] as Map,
                                    'Сохранено · ${network.toUpperCase()}'),
                                if (sample['active_$network'] != null)
                                  decision(sample['active_$network'] as Map,
                                      'Применено · ${network.toUpperCase()}'),
                              ],
                            ],
                            const SizedBox(height: 14),
                            Text(
                                result?['notice'] as String? ??
                                    'Предпросмотр правил, без отправки сетевых запросов.',
                                style: Theme.of(context).textTheme.bodySmall),
                          ]))),
                  actions: [
                    TextButton(
                        onPressed: () => Navigator.pop(ctx),
                        child: const Text('Закрыть'))
                  ]);
            }));
    await Navigator.of(context, rootNavigator: true).push(dialog);
    await dialog.completed;
    input.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final android = widget.state['platform'] == 'android';
    final all = rows;
    final filtered = all.where((e) {
      final d = definition(e);
      return (kind == 'all' ||
              e['kind'] == kind ||
              (kind == 'application' &&
                  ((d['processes'] as List? ?? []).isNotEmpty ||
                      (d['process_paths'] as List? ?? []).isNotEmpty)) ||
              (kind == 'site' && (d['domains'] as List? ?? []).isNotEmpty) ||
              (kind == 'network' &&
                  (d['ip_cidr'] as List? ?? []).isNotEmpty)) &&
          (origin == 'all' || e['origin'] == origin) &&
          (!runningOnly || (e['application'] as Map?)?['running'] == true) &&
          [
            d['name'],
            ...(d['domains'] as List? ?? []),
            ...(d['processes'] as List? ?? []),
            ...(d['process_paths'] as List? ?? []),
            ...(d['ip_cidr'] as List? ?? [])
          ].join(' ').toLowerCase().contains(query.toLowerCase());
    }).toList();
    filtered.sort((a, b) => definition(a)['name']
        .toString()
        .toLowerCase()
        .compareTo(definition(b)['name'].toString().toLowerCase()));
    return Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
      const Text('Каталог маршрутов',
          style: TextStyle(fontSize: 23, fontWeight: FontWeight.w600)),
      const SizedBox(height: 8),
      Text(android
          ? 'Сайты и сети. Выберите маршрут рядом с нужной записью.'
          : 'Приложения, сайты и сети. Выберите маршрут рядом с нужной записью.'),
      const SizedBox(height: 16),
      Wrap(spacing: 10, runSpacing: 8, children: [
        FilledButton.icon(
            key: const ValueKey('catalogue_add'),
            onPressed: locked ? null : () => edit(),
            icon: const Icon(Icons.add),
            label: const Text('Добавить сайт или IP')),
        if (widget.state['application_discovery_supported'] == true)
          OutlinedButton.icon(
              onPressed: locked
                  ? null
                  : () async {
                      final error = await call('applications_refresh');
                      if (error != null && mounted) notify(error);
                    },
              icon: const Icon(Icons.refresh),
              label: const Text('Обновить приложения')),
        if (!android)
          TextButton(
              onPressed: () => setState(() => advanced = !advanced),
              child: Text(advanced ? 'Вернуться к каталогу' : 'Редактор правил')),
        if (widget.state['catalogue_transfer_supported'] == true)
          PopupMenuButton<String>(
            enabled: !locked,
            tooltip: 'Перенос каталога',
            onSelected: (action) {
              if (action == 'export') {
                exportCatalogue();
              } else {
                importCatalogue();
              }
            },
            itemBuilder: (context) => const [
              PopupMenuItem(
                  value: 'export', child: Text('Скопировать JSON каталога')),
              PopupMenuItem(
                  value: 'import', child: Text('Импортировать JSON каталога')),
            ],
            child: const Padding(
              padding: EdgeInsets.symmetric(horizontal: 12, vertical: 10),
              child: Text('Перенос каталога'),
            ),
          ),
      ]),
      if (widget.state['routing_pending'] == true)
        Container(
            margin: const EdgeInsets.symmetric(vertical: 14),
            padding: const EdgeInsets.all(14),
            decoration: BoxDecoration(
                border:
                    Border.all(color: Theme.of(context).colorScheme.primary),
                borderRadius: BorderRadius.circular(14)),
            child:
                Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              const Text(
                  'Маршруты сохранены. Чтобы они заработали, нужно переподключить VPN.'),
              if (!android) ...[
                const SizedBox(height: 8),
                FilledButton(
                    onPressed: locked
                        ? null
                        : () async {
                            final error = await call('routing_apply');
                            if (error != null && mounted) notify(error);
                          },
                    child: const Text('Применить')),
              ],
            ])),
      if ((widget.state['catalogue_error'] as String? ?? '').isNotEmpty)
        Text(widget.state['catalogue_error'] as String),
      if ((widget.state['applications_error'] as String? ?? '').isNotEmpty)
        Text(widget.state['applications_error'] as String),
      const SizedBox(height: 16),
      if (advanced)
        RoutingRulesPanel(
            backend: widget.backend,
            state: widget.state,
            onChanged: widget.onChanged,
            busy: locked,
            connected: widget.connected)
      else ...[
        TextField(
            key: const ValueKey('catalogue_search'),
            decoration: InputDecoration(
                prefixIcon: const Icon(Icons.search),
                labelText: android
                    ? 'Название, сайт или IP'
                    : 'Название, сайт, IP или программа'),
            onChanged: (v) => setState(() => query = v)),
        const SizedBox(height: 12),
        Wrap(spacing: 8, runSpacing: 6, children: [
          for (final e in {
            'all': 'Все',
            if (!android) 'application': 'Приложения',
            'site': 'Сайты и сервисы',
            'network': 'IP и подсети'
          }.entries)
            ChoiceChip(
                label: Text(e.value),
                selected: kind == e.key,
                selectedColor: Theme.of(context)
                    .colorScheme
                    .primary
                    .withValues(alpha: .16),
                checkmarkColor: Theme.of(context).colorScheme.primary,
                labelStyle:
                    TextStyle(color: Theme.of(context).colorScheme.onSurface),
                onSelected: (_) => setState(() {
                      kind = e.key;
                      runningOnly = false;
                    })),
        ]),
        const SizedBox(height: 8),
        Wrap(spacing: 8, runSpacing: 6, children: [
          for (final e in {
            'all': 'Любой источник',
            'builtin': 'Встроенные',
            if (!android) 'discovered': 'Обнаруженные',
            'custom': 'Мои записи'
          }.entries)
            ChoiceChip(
                label: Text(e.value),
                selected: origin == e.key,
                selectedColor: Theme.of(context)
                    .colorScheme
                    .primary
                    .withValues(alpha: .16),
                checkmarkColor: Theme.of(context).colorScheme.primary,
                labelStyle:
                    TextStyle(color: Theme.of(context).colorScheme.onSurface),
                onSelected: (_) => setState(() => origin = e.key)),
          if (kind == 'application')
            FilterChip(
                label: const Text('Только запущенные'),
                selected: runningOnly,
                onSelected: (v) => setState(() => runningOnly = v)),
        ]),
        const SizedBox(height: 12),
        Text('Найдено: ${filtered.length} из ${all.length}',
            style: Theme.of(context).textTheme.bodySmall),
        const SizedBox(height: 10),
        if (working) const LinearProgressIndicator(),
        if (filtered.isEmpty)
          const Padding(
              padding: EdgeInsets.all(20),
              child: Text(
                  'Записи не найдены. Измените фильтр или добавьте адрес.')),
        for (final e in filtered) _card(e),
      ],
    ]);
  }

  Widget _card(Map<String, dynamic> row) {
    final d = definition(row);
    final app = row['application'] as Map?;
    const origins = {
      'builtin': 'Встроенная',
      'custom': 'Моя запись',
      'discovered': 'Обнаружено'
    };
    final details = [
      ...(d['domains'] as List? ?? []),
      ...(d['ip_cidr'] as List? ?? []),
      ...(d['processes'] as List? ?? []),
      ...(d['process_paths'] as List? ?? [])
    ].join(' · ');
    return Container(
        key: ValueKey('catalogue_${row['id']}'),
        margin: const EdgeInsets.only(bottom: 10),
        padding: const EdgeInsets.all(14),
        decoration: BoxDecoration(
            color: Theme.of(context).colorScheme.surface,
            border: Border.all(color: Theme.of(context).colorScheme.outline),
            borderRadius: BorderRadius.circular(14)),
        child:
            Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
          Row(children: [
            Icon(row['kind'] == 'application'
                ? Icons.apps
                : row['kind'] == 'network'
                    ? Icons.lan_outlined
                    : Icons.language),
            const SizedBox(width: 10),
            Expanded(
                child: Text(d['name'] as String,
                    style: const TextStyle(
                        fontSize: 16, fontWeight: FontWeight.w600))),
            PopupMenuButton<String>(
                enabled: !locked,
                tooltip: 'Действия с записью',
                onSelected: (value) =>
                    value == 'edit' ? edit(row) : remove(row),
                itemBuilder: (_) => [
                      const PopupMenuItem(
                          value: 'edit', child: Text('Изменить запись')),
                      if (row['origin'] != 'discovered')
                        const PopupMenuItem(
                            value: 'delete', child: Text('Удалить'))
                    ]),
          ]),
          Text(
              '${origins[row['origin']]}${app == null ? '' : app['running'] == true ? ' · Запущено' : ' · Не запущено'}',
              style: Theme.of(context).textTheme.bodySmall),
          const SizedBox(height: 6),
          Text(details,
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: Theme.of(context).textTheme.bodySmall),
          const SizedBox(height: 10),
          OutlinedButton.icon(
              key: ValueKey('catalogue_route_${row['id']}'),
              onPressed: locked ? null : () => changeRoute(row),
              icon: const Icon(Icons.alt_route),
              label: Text(label(row['route'] as String),
                  overflow: TextOverflow.ellipsis)),
          TextButton(
              onPressed: locked ? null : () => explain(row),
              child: const Text('Почему этот маршрут?')),
          if (app != null &&
              row['route'] == 'inherit' &&
              row['preview_tcp'] is Map) ...[
            Text(
                'TCP: ${label((row['preview_tcp'] as Map)['route'] as String)} · UDP: ${label((row['preview_udp'] as Map)['route'] as String)}',
                style: Theme.of(context).textTheme.bodySmall),
          ],
          if ((d['network'] as String? ?? '').isNotEmpty)
            Text(
                d['network'] == 'tcp'
                    ? 'Отдельное правило только для TCP'
                    : 'Отдельное правило только для UDP',
                style: Theme.of(context).textTheme.bodySmall),
          if (app != null &&
              row['route'] == 'inherit' &&
              app['rule_id'] != null)
            Text(
                'Есть общее правило для этого приложения. Отдельный маршрут имеет приоритет.',
                style: Theme.of(context).textTheme.bodySmall),
        ]));
  }
}
