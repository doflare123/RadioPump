import 'dart:async';

import 'package:flutter/material.dart';
import 'package:window_manager/window_manager.dart';

import '../domain/radio_models.dart';
import '../platform/desktop_shell.dart';
import '../playback/radio_audio.dart';
import '../radio_controller.dart';

const _background = Color(0xff242526);
const _panel = Color(0xff1c1d1e);
const _gold = Color(0xffcbb679);
const _muted = Color(0xffa0a3a6);

// Адаптивный экран использует системный шрифт и статические поверхности.
// Отдельных движков, пакета навигации и анимаций постоянного вращения нет.
class RadioPumpApp extends StatelessWidget {
  const RadioPumpApp({super.key, required this.controller, this.desktop});
  final RadioController controller;
  final DesktopShell? desktop;

  @override
  Widget build(BuildContext context) => MaterialApp(
    title: 'RadioPump',
    debugShowCheckedModeBanner: false,
    theme: ThemeData(
      brightness: Brightness.dark,
      scaffoldBackgroundColor: _background,
      colorScheme: ColorScheme.fromSeed(
        seedColor: _gold,
        brightness: Brightness.dark,
      ).copyWith(primary: _gold, surface: _panel),
      fontFamily: null,
      useMaterial3: true,
      splashFactory: NoSplash.splashFactory,
      tooltipTheme: const TooltipThemeData(
        waitDuration: Duration(milliseconds: 500),
      ),
    ),
    home: _Lifecycle(controller: controller, desktop: desktop),
  );
}

// На телефоне жизненный цикл отключает UI-таймер при уходе в фон. Управление
// аудио остаётся у системного media session и не зависит от страницы Flutter.
class _Lifecycle extends StatefulWidget {
  const _Lifecycle({required this.controller, this.desktop});
  final RadioController controller;
  final DesktopShell? desktop;
  @override
  State<_Lifecycle> createState() => _LifecycleState();
}

class _LifecycleState extends State<_Lifecycle> with WidgetsBindingObserver {
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (widget.desktop == null) {
      widget.controller.setVisible(state == AppLifecycleState.resumed);
    }
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final shell = widget.desktop;
    if (shell == null) return _FullPlayer(controller: widget.controller);
    return ValueListenableBuilder<bool>(
      valueListenable: shell.visible,
      builder: (context, visible, _) => TickerMode(
        enabled: visible,
        child: ValueListenableBuilder<bool>(
          valueListenable: shell.compact,
          builder: (context, compact, _) => compact
              ? _CompactPlayer(controller: widget.controller, desktop: shell)
              : _FullPlayer(controller: widget.controller, desktop: shell),
        ),
      ),
    );
  }
}

class _FullPlayer extends StatefulWidget {
  const _FullPlayer({required this.controller, this.desktop});
  final RadioController controller;
  final DesktopShell? desktop;
  @override
  State<_FullPlayer> createState() => _FullPlayerState();
}

class _FullPlayerState extends State<_FullPlayer> {
  bool _showStations = true;

  @override
  Widget build(BuildContext context) => Scaffold(
    body: SafeArea(
      child: AnimatedBuilder(
        animation: widget.controller,
        builder: (context, _) => LayoutBuilder(
          builder: (context, constraints) {
            final narrow = constraints.maxWidth < 700;
            return Column(
              children: [
                _Header(controller: widget.controller, desktop: widget.desktop),
                if (widget.desktop?.trayError != null)
                  _Notice(widget.desktop!.trayError!),
                if (widget.controller.error != null)
                  _Notice(
                    widget.controller.error!,
                    onRetry: () => unawaited(widget.controller.refresh()),
                  ),
                Expanded(
                  child: widget.controller.server == null
                      ? _Welcome(controller: widget.controller)
                      : narrow
                      ? _mobile(context)
                      : Row(
                          children: [
                            if (_showStations)
                              SizedBox(
                                width: 260,
                                child: _StationList(
                                  controller: widget.controller,
                                ),
                              ),
                            IconButton(
                              tooltip: _showStations
                                  ? 'Скрыть список волн'
                                  : 'Показать список волн',
                              onPressed: () => setState(
                                () => _showStations = !_showStations,
                              ),
                              icon: Icon(
                                _showStations
                                    ? Icons.chevron_left
                                    : Icons.chevron_right,
                              ),
                            ),
                            Expanded(
                              child: _PlayerBody(controller: widget.controller),
                            ),
                          ],
                        ),
                ),
              ],
            );
          },
        ),
      ),
    ),
  );

  Widget _mobile(BuildContext context) => Column(
    children: [
      Padding(
        padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 6),
        child: OutlinedButton.icon(
          onPressed: () => showModalBottomSheet<void>(
            context: context,
            showDragHandle: true,
            isScrollControlled: true,
            useSafeArea: true,
            builder: (context) => SizedBox(
              height: MediaQuery.sizeOf(context).height * 0.65,
              child: AnimatedBuilder(
                animation: widget.controller,
                builder: (context, _) => _StationList(
                  controller: widget.controller,
                  onSelected: () => Navigator.pop(context),
                ),
              ),
            ),
          ),
          icon: const Icon(Icons.radio_outlined, size: 18),
          label: Text(
            widget.controller.selectedStation ?? 'Выбрать волну',
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
          ),
        ),
      ),
      Expanded(child: _PlayerBody(controller: widget.controller)),
    ],
  );
}

// Переключатель владельца отделён от списка его волн. Никакой общей учётной
// записи или выдуманного каталога пользователей сервер пока не предоставляет.
class _Header extends StatelessWidget {
  const _Header({required this.controller, this.desktop});
  final RadioController controller;
  final DesktopShell? desktop;

  @override
  Widget build(BuildContext context) => SizedBox(
    height: 64,
    child: Row(
      children: [
        const SizedBox(width: 20),
        if (desktop != null)
          Expanded(
            child: GestureDetector(
              behavior: HitTestBehavior.opaque,
              onPanStart: (_) => unawaited(windowManager.startDragging()),
              child: const Row(
                children: [
                  Icon(Icons.graphic_eq, color: _gold),
                  SizedBox(width: 10),
                  Text(
                    'RadioPump',
                    style: TextStyle(fontWeight: FontWeight.w600, fontSize: 18),
                  ),
                ],
              ),
            ),
          )
        else ...[
          const Icon(Icons.graphic_eq, color: _gold),
          const SizedBox(width: 10),
        ],
        Flexible(
          child: DropdownButtonHideUnderline(
            child: DropdownButton<String>(
              isExpanded: true,
              value: controller.server?.address.toString(),
              hint: const Text('Моё радио'),
              items: controller.servers
                  .map(
                    (server) => DropdownMenuItem(
                      value: server.address.toString(),
                      child: Text(server.name, overflow: TextOverflow.ellipsis),
                    ),
                  )
                  .toList(growable: false),
              onChanged: (value) {
                if (value == null) return;
                final server = controller.servers.firstWhere(
                  (item) => item.address.toString() == value,
                );
                if (server.address != controller.server?.address) {
                  unawaited(controller.selectServer(server));
                }
              },
            ),
          ),
        ),
        IconButton(
          tooltip: 'Мои серверы',
          onPressed: () => _showServers(context, controller),
          icon: const Icon(Icons.tune, size: 20),
        ),
        if (desktop != null) ...[
          IconButton(
            tooltip: 'Свернуть в трей',
            onPressed: () => unawaited(desktop!.hide()),
            icon: const Icon(Icons.remove),
          ),
          IconButton(
            tooltip: 'Скрыть в трей',
            onPressed: () => unawaited(desktop!.hide()),
            icon: const Icon(Icons.close, size: 20),
          ),
        ],
        const SizedBox(width: 8),
      ],
    ),
  );
}

class _StationList extends StatelessWidget {
  const _StationList({required this.controller, this.onSelected});
  final RadioController controller;
  final VoidCallback? onSelected;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.fromLTRB(20, 16, 8, 20),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Padding(
          padding: EdgeInsets.only(left: 8, bottom: 18),
          child: Text(
            'Список станций',
            style: TextStyle(fontSize: 20, fontWeight: FontWeight.w500),
          ),
        ),
        Expanded(
          child: Container(
            decoration: BoxDecoration(
              border: Border.all(color: const Color(0xff454748)),
              borderRadius: BorderRadius.circular(16),
            ),
            child: controller.loading && controller.stations.isEmpty
                ? const Center(
                    child: SizedBox.square(
                      dimension: 22,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    ),
                  )
                : controller.stations.isEmpty
                ? const Center(
                    child: Padding(
                      padding: EdgeInsets.all(20),
                      child: Text(
                        'Пока нет волн',
                        style: TextStyle(color: _muted),
                      ),
                    ),
                  )
                : ListView.builder(
                    padding: const EdgeInsets.all(10),
                    itemCount: controller.stations.length,
                    itemBuilder: (context, index) {
                      final station = controller.stations[index];
                      final selected = station.id == controller.selectedStation;
                      return Padding(
                        padding: const EdgeInsets.only(bottom: 8),
                        child: Material(
                          color: selected ? const Color(0xff243e55) : _panel,
                          borderRadius: BorderRadius.circular(12),
                          child: InkWell(
                            borderRadius: BorderRadius.circular(12),
                            onTap: () {
                              unawaited(controller.selectStation(station.id));
                              onSelected?.call();
                            },
                            child: Padding(
                              padding: const EdgeInsets.all(14),
                              child: Column(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                children: [
                                  Row(
                                    children: [
                                      Expanded(
                                        child: Text(
                                          station.id,
                                          maxLines: 1,
                                          overflow: TextOverflow.ellipsis,
                                          style: const TextStyle(
                                            fontSize: 16,
                                            fontWeight: FontWeight.w600,
                                          ),
                                        ),
                                      ),
                                      if (selected)
                                        const Icon(
                                          Icons.graphic_eq,
                                          color: _gold,
                                          size: 18,
                                        ),
                                    ],
                                  ),
                                  const SizedBox(height: 7),
                                  Text(
                                    station.current?.title ??
                                        station.description,
                                    maxLines: 1,
                                    overflow: TextOverflow.ellipsis,
                                    style: const TextStyle(
                                      fontSize: 12,
                                      color: Color(0xffc4c8cc),
                                    ),
                                  ),
                                  if (station.tags.isNotEmpty) ...[
                                    const SizedBox(height: 7),
                                    Text(
                                      station.tags.join(' · '),
                                      maxLines: 1,
                                      overflow: TextOverflow.ellipsis,
                                      style: const TextStyle(
                                        fontSize: 11,
                                        color: _muted,
                                      ),
                                    ),
                                  ],
                                ],
                              ),
                            ),
                          ),
                        ),
                      );
                    },
                  ),
          ),
        ),
      ],
    ),
  );
}

class _PlayerBody extends StatelessWidget {
  const _PlayerBody({required this.controller});
  final RadioController controller;

  @override
  Widget build(BuildContext context) {
    final track = controller.track;
    final cover = controller.server?.cover(track?.coverPath);
    return LayoutBuilder(
      builder: (context, constraints) => SingleChildScrollView(
        padding: const EdgeInsets.fromLTRB(24, 12, 24, 24),
        child: ConstrainedBox(
          constraints: BoxConstraints(
            minHeight: (constraints.maxHeight - 36).clamp(0, double.infinity),
          ),
          child: Center(
            child: SizedBox(
              width: 360,
              child: Column(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  RepaintBoundary(
                    child: ClipRRect(
                      borderRadius: BorderRadius.circular(6),
                      child: SizedBox.square(
                        dimension: constraints.maxHeight < 550 ? 210 : 260,
                        child: cover == null
                            ? const _ArtworkPlaceholder()
                            : Image.network(
                                cover.toString(),
                                cacheWidth: 512,
                                cacheHeight: 512,
                                fit: BoxFit.cover,
                                gaplessPlayback: false,
                                errorBuilder: (context, error, stack) =>
                                    const _ArtworkPlaceholder(),
                              ),
                      ),
                    ),
                  ),
                  const SizedBox(height: 24),
                  Text(
                    track?.title.isNotEmpty == true
                        ? track!.title
                        : controller.station?.description ?? 'Выберите волну',
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    textAlign: TextAlign.center,
                    style: const TextStyle(
                      fontSize: 22,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                  const SizedBox(height: 7),
                  Text(
                    [track?.artist, track?.album]
                        .whereType<String>()
                        .where((part) => part.isNotEmpty)
                        .join(' · '),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    textAlign: TextAlign.center,
                    style: const TextStyle(color: _muted, fontSize: 14),
                  ),
                  const SizedBox(height: 18),
                  _Transport(controller: controller),
                  const SizedBox(height: 18),
                  _TrackProgress(controller: controller),
                  const SizedBox(height: 12),
                  Text(
                    _phaseLabel(controller),
                    style: const TextStyle(fontSize: 12, color: _muted),
                  ),
                  const SizedBox(height: 12),
                  SizedBox(
                    width: 180,
                    child: Row(
                      children: [
                        const Icon(
                          Icons.volume_down_outlined,
                          size: 20,
                          color: _muted,
                        ),
                        Expanded(
                          child: Slider(
                            value: controller.volume,
                            label: '${(controller.volume * 100).round()}%',
                            onChanged: (value) =>
                                unawaited(controller.setVolume(value)),
                          ),
                        ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

// Нейтральная заглушка рисуется векторными примитивами и не требует bitmap,
// стороннего шрифта или постоянно работающего animation controller.
class _ArtworkPlaceholder extends StatelessWidget {
  const _ArtworkPlaceholder();
  @override
  Widget build(BuildContext context) => ColoredBox(
    color: const Color(0xff181a1c),
    child: Center(
      child: Container(
        width: 142,
        height: 142,
        decoration: BoxDecoration(
          shape: BoxShape.circle,
          border: Border.all(color: const Color(0xff424444)),
        ),
        child: Center(
          child: Container(
            width: 96,
            height: 96,
            decoration: BoxDecoration(
              shape: BoxShape.circle,
              border: Border.all(color: const Color(0xff333536)),
            ),
            child: const Icon(Icons.graphic_eq, size: 42, color: _gold),
          ),
        ),
      ),
    ),
  );
}

class _Transport extends StatelessWidget {
  const _Transport({required this.controller, this.small = false});
  final RadioController controller;
  final bool small;

  @override
  Widget build(BuildContext context) {
    final active = controller.wantsPlayback;
    final enabled = controller.selectedStation != null;
    return Row(
      mainAxisAlignment: MainAxisAlignment.center,
      children: [
        IconButton(
          tooltip: 'Предыдущая волна',
          onPressed: controller.stations.length > 1
              ? () => unawaited(controller.stepStation(-1))
              : null,
          icon: Icon(Icons.chevron_left, size: small ? 34 : 38),
        ),
        const SizedBox(width: 28),
        Container(
          decoration: BoxDecoration(
            shape: BoxShape.circle,
            border: Border.all(
              color: enabled ? _gold : const Color(0xff555555),
              width: 1.5,
            ),
          ),
          child: IconButton(
            tooltip: active ? 'Остановить эфир' : 'Слушать эфир',
            onPressed: enabled ? () => unawaited(controller.toggle()) : null,
            iconSize: small ? 28 : 32,
            padding: EdgeInsets.all(small ? 10 : 14),
            icon: Icon(active ? Icons.pause : Icons.play_arrow),
          ),
        ),
        const SizedBox(width: 28),
        IconButton(
          tooltip: 'Следующая волна',
          onPressed: controller.stations.length > 1
              ? () => unawaited(controller.stepStation(1))
              : null,
          icon: Icon(Icons.chevron_right, size: small ? 34 : 38),
        ),
      ],
    );
  }
}

class _TrackProgress extends StatelessWidget {
  const _TrackProgress({required this.controller});
  final RadioController controller;

  @override
  Widget build(BuildContext context) => RepaintBoundary(
    child: ValueListenableBuilder<int>(
      valueListenable: controller.clockTick,
      builder: (context, tick, _) {
        final elapsed = controller.elapsed;
        final duration = controller.track?.duration ?? 0;
        return Tooltip(
          message: 'Прямой эфир · без перемотки',
          child: Column(
            children: [
              Row(
                mainAxisAlignment: MainAxisAlignment.spaceBetween,
                children: [
                  Text(
                    _duration(elapsed.inSeconds),
                    style: const TextStyle(fontSize: 10, color: _muted),
                  ),
                  Text(
                    duration > 0 ? _duration(duration) : 'LIVE',
                    style: const TextStyle(fontSize: 10, color: _muted),
                  ),
                ],
              ),
              const SizedBox(height: 6),
              LinearProgressIndicator(
                value: duration > 0
                    ? (elapsed.inSeconds / duration).clamp(0, 1)
                    : 0,
                minHeight: 4,
                color: _gold,
                backgroundColor: const Color(0xff414344),
                borderRadius: BorderRadius.circular(2),
              ),
            ],
          ),
        );
      },
    ),
  );
}

String _duration(int seconds) =>
    '${seconds ~/ 60}:${(seconds % 60).toString().padLeft(2, '0')}';
String _phaseLabel(RadioController controller) =>
    switch (controller.audio.view.value.phase) {
      PlayerPhase.stopped => 'Прямой эфир · подключение при нажатии',
      PlayerPhase.connecting => 'Подключаемся…',
      PlayerPhase.playing => 'В ПРЯМОМ ЭФИРЕ',
      PlayerPhase.buffering => 'Буферизация…',
      PlayerPhase.reconnecting => 'Восстанавливаем связь…',
    };

// Мини-плеер намеренно не строит обложку и список станций. Используется то же
// состояние, а «Развернуть» возвращает полное окно без повторного подключения.
class _CompactPlayer extends StatelessWidget {
  const _CompactPlayer({required this.controller, required this.desktop});
  final RadioController controller;
  final DesktopShell desktop;

  @override
  Widget build(BuildContext context) => Scaffold(
    backgroundColor: const Color(0xff151617),
    body: AnimatedBuilder(
      animation: controller,
      builder: (context, _) => Padding(
        padding: const EdgeInsets.fromLTRB(22, 4, 14, 16),
        child: Column(
          children: [
            Row(
              children: [
                const Icon(Icons.graphic_eq, color: _gold, size: 17),
                const Spacer(),
                TextButton.icon(
                  onPressed: () => unawaited(desktop.expand()),
                  icon: const Icon(Icons.open_in_full, size: 13),
                  label: const Text(
                    'Развернуть',
                    style: TextStyle(fontSize: 11),
                  ),
                ),
                IconButton(
                  tooltip: 'Скрыть',
                  visualDensity: VisualDensity.compact,
                  onPressed: () => unawaited(desktop.hide()),
                  icon: const Icon(Icons.close, size: 16),
                ),
              ],
            ),
            Text(
              '${controller.track?.title ?? 'RadioPump'} · ${controller.selectedStation ?? 'Выберите волну'}',
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: const TextStyle(fontSize: 13),
            ),
            const SizedBox(height: 8),
            _Transport(controller: controller, small: true),
            const Spacer(),
            _TrackProgress(controller: controller),
            const SizedBox(height: 6),
            Text(
              _phaseLabel(controller),
              style: const TextStyle(fontSize: 10, color: _muted),
            ),
          ],
        ),
      ),
    ),
  );
}

class _Notice extends StatelessWidget {
  const _Notice(this.message, {this.onRetry});
  final String message;
  final VoidCallback? onRetry;
  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 4),
    child: Row(
      children: [
        const Icon(Icons.info_outline, size: 16, color: _gold),
        const SizedBox(width: 8),
        Expanded(child: Text(message, style: const TextStyle(fontSize: 12))),
        if (onRetry != null)
          TextButton(onPressed: onRetry, child: const Text('Повторить')),
      ],
    ),
  );
}

class _Welcome extends StatelessWidget {
  const _Welcome({required this.controller});
  final RadioController controller;
  @override
  Widget build(BuildContext context) => Center(
    child: Padding(
      padding: const EdgeInsets.all(28),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Icon(Icons.radio_outlined, size: 64, color: _gold),
          const SizedBox(height: 24),
          const Text(
            'Ваше радио. Просто слушать.',
            textAlign: TextAlign.center,
            style: TextStyle(fontSize: 24),
          ),
          const SizedBox(height: 12),
          const Text(
            'Добавьте адрес своего RadioPump\nили радио другого пользователя.',
            textAlign: TextAlign.center,
            style: TextStyle(color: _muted, height: 1.6),
          ),
          const SizedBox(height: 28),
          FilledButton.icon(
            onPressed: () => _showServers(context, controller),
            icon: const Icon(Icons.add),
            label: const Text('Добавить радио'),
          ),
        ],
      ),
    ),
  );
}

Future<void> _showServers(BuildContext context, RadioController controller) =>
    showDialog<void>(
      context: context,
      builder: (context) => _ServerDialog(controller: controller),
    );

class _ServerDialog extends StatefulWidget {
  const _ServerDialog({required this.controller});
  final RadioController controller;
  @override
  State<_ServerDialog> createState() => _ServerDialogState();
}

class _ServerDialogState extends State<_ServerDialog> {
  final _form = GlobalKey<FormState>();
  final _name = TextEditingController();
  final _address = TextEditingController();
  bool _saving = false;
  String? _error;

  @override
  void dispose() {
    _name.dispose();
    _address.dispose();
    super.dispose();
  }

  Future<void> _add() async {
    if (!_form.currentState!.validate() || _saving) return;
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      final value = RadioServer(
        name: _name.text.trim(),
        address: Uri.parse(_address.text.trim()),
      );
      // На время первого запроса исключаем повторное добавление. Ошибку
      // подключения показывает основной экран; сохранённый адрес остаётся доступен.
      final operation = widget.controller.addServer(value);
      await operation;
      if (mounted) Navigator.pop(context);
    } catch (error) {
      if (mounted) {
        setState(() {
          _error = error is FormatException
              ? error.message
              : 'Не удалось сохранить адрес';
        });
      }
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('Мои серверы'),
    content: SizedBox(
      width: 420,
      child: SingleChildScrollView(
        child: Form(
          key: _form,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              AnimatedBuilder(
                animation: widget.controller,
                builder: (context, _) => Column(
                  children: [
                    for (final server in widget.controller.servers)
                      ListTile(
                        contentPadding: EdgeInsets.zero,
                        title: Text(
                          server.name,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                        ),
                        subtitle: Text(
                          server.address.toString(),
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                        ),
                        onTap: _saving
                            ? null
                            : () {
                                unawaited(
                                  widget.controller.selectServer(server),
                                );
                                Navigator.pop(context);
                              },
                        trailing: IconButton(
                          tooltip: 'Убрать сохранённый сервер',
                          onPressed: _saving
                              ? null
                              : () => unawaited(
                                  widget.controller.removeServer(server),
                                ),
                          icon: const Icon(Icons.delete_outline, size: 20),
                        ),
                      ),
                  ],
                ),
              ),
              const SizedBox(height: 12),
              TextFormField(
                controller: _name,
                maxLength: 64,
                decoration: const InputDecoration(
                  labelText: 'Название',
                  hintText: 'Радио друга',
                ),
                textInputAction: TextInputAction.next,
                validator: (value) => value == null || value.trim().isEmpty
                    ? 'Укажите название'
                    : null,
              ),
              const SizedBox(height: 8),
              TextFormField(
                controller: _address,
                maxLength: 2048,
                decoration: const InputDecoration(
                  labelText: 'Адрес RadioPump',
                  hintText: 'https://radio.example.org',
                  counterText: '',
                ),
                keyboardType: TextInputType.url,
                autocorrect: false,
                textInputAction: TextInputAction.done,
                onFieldSubmitted: (_) => unawaited(_add()),
                validator: (value) {
                  try {
                    RadioServer.normalizeAddress(
                      Uri.parse(value?.trim() ?? ''),
                    );
                    return null;
                  } on FormatException {
                    return 'Нужен полный адрес http:// или https://';
                  }
                },
              ),
              const SizedBox(height: 12),
              const Text(
                'Для прослушивания логин не нужен.',
                style: TextStyle(fontSize: 12, color: _muted),
              ),
              if (_error != null)
                Padding(
                  padding: const EdgeInsets.only(top: 12),
                  child: Text(_error!, style: const TextStyle(color: _gold)),
                ),
            ],
          ),
        ),
      ),
    ),
    actions: [
      TextButton(
        onPressed: () => showLicensePage(
          context: context,
          applicationName: 'RadioPump',
          applicationVersion: '0.1.0',
          applicationLegalese: 'CC BY-NC 4.0',
        ),
        child: const Text('Лицензии'),
      ),
      TextButton(
        onPressed: _saving ? null : () => Navigator.pop(context),
        child: const Text('Закрыть'),
      ),
      FilledButton(
        onPressed: _saving ? null : () => unawaited(_add()),
        child: Text(_saving ? 'Подключаемся…' : 'Добавить'),
      ),
    ],
  );
}
