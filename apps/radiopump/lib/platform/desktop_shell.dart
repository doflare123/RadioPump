import 'dart:async';
import 'dart:io';
import 'dart:math';

import 'package:flutter/material.dart';
import 'package:screen_retriever/screen_retriever.dart';
import 'package:tray_manager/tray_manager.dart';
import 'package:window_manager/window_manager.dart';

import '../radio_controller.dart';

bool get isDesktop =>
    Platform.isWindows || Platform.isLinux || Platform.isMacOS;

// Трей меняет размер единственного окна, не создавая второй Flutter engine.
// Полный режим сохраняет свои координаты; мини-плеер привязывается к рабочей
// области монитора со значком трея и скрывается при потере фокуса.
class DesktopShell with WindowListener, TrayListener {
  DesktopShell(this.controller);
  final RadioController controller;
  final ValueNotifier<bool> compact = ValueNotifier(false);
  final ValueNotifier<bool> visible = ValueNotifier(true);
  Rect? _fullBounds;
  bool _trayReady = false;
  bool _switching = false;
  bool _quitting = false;
  String? trayError;

  static const fullSize = Size(1000, 640);
  static const compactSize = Size(400, 210);

  Future<void> initialize() async {
    await windowManager.ensureInitialized();
    windowManager.addListener(this);
    await windowManager.waitUntilReadyToShow(
      const WindowOptions(
        size: fullSize,
        minimumSize: Size(720, 500),
        center: true,
        title: 'RadioPump',
        backgroundColor: Color(0xff242526),
        titleBarStyle: TitleBarStyle.hidden,
      ),
      () async {
        await windowManager.show();
        await windowManager.focus();
      },
    );
    trayManager.addListener(this);
    try {
      await trayManager.setIcon(
        Platform.isWindows ? 'assets/tray.ico' : 'assets/tray.png',
        isTemplate: Platform.isMacOS,
      );
      await trayManager.setToolTip('RadioPump');
      await trayManager.setContextMenu(
        Menu(
          items: [
            MenuItem(key: 'compact', label: 'Мини-плеер'),
            MenuItem(key: 'expand', label: 'Развернуть RadioPump'),
            MenuItem.separator(),
            MenuItem(key: 'toggle', label: 'Включить / остановить'),
            MenuItem.separator(),
            MenuItem(key: 'quit', label: 'Выход'),
          ],
        ),
      );
      _trayReady = true;
      await windowManager.setPreventClose(true);
    } catch (_) {
      trayError =
          'Системный трей недоступен. Закрытие окна завершит приложение.';
      await windowManager.setPreventClose(false);
    }
  }

  Future<void> hide() async {
    if (!_trayReady) {
      await windowManager.minimize();
      return;
    }
    controller.setVisible(false);
    visible.value = false;
    await windowManager.hide();
  }

  Future<void> showCompact() async {
    if (_switching || _quitting) return;
    _switching = true;
    try {
      if (!compact.value) _fullBounds = await windowManager.getBounds();
      await windowManager.setMinimumSize(compactSize);
      await windowManager.setResizable(false);
      await windowManager.setAlwaysOnTop(true);
      await windowManager.setSkipTaskbar(true);
      compact.value = true;
      final position = await _popupPosition();
      await windowManager.setBounds(position & compactSize);
      await windowManager.show();
      await windowManager.focus();
      visible.value = true;
      controller.setVisible(true);
    } finally {
      _switching = false;
    }
  }

  Future<Offset> _popupPosition() async {
    final tray = await trayManager.getBounds();
    final anchor = tray?.center ?? await screenRetriever.getCursorScreenPoint();
    final displays = await screenRetriever.getAllDisplays();
    if (displays.isEmpty) return const Offset(20, 20);
    Rect areaOf(Display display) => Rect.fromLTWH(
      display.visiblePosition?.dx ?? 0,
      display.visiblePosition?.dy ?? 0,
      display.visibleSize?.width ?? display.size.width,
      display.visibleSize?.height ?? display.size.height,
    );
    var area = areaOf(displays.first);
    for (final display in displays) {
      final candidate = areaOf(display);
      if ((candidate.center - anchor).distanceSquared <
          (area.center - anchor).distanceSquared) {
        area = candidate;
      }
    }
    final x = (anchor.dx - compactSize.width / 2).clamp(
      area.left + 8,
      max(area.left + 8, area.right - compactSize.width - 8),
    );
    final above = anchor.dy - compactSize.height - 12;
    final y = (above >= area.top ? above : anchor.dy + 12).clamp(
      area.top + 8,
      max(area.top + 8, area.bottom - compactSize.height - 8),
    );
    return Offset(x.toDouble(), y.toDouble());
  }

  Future<void> expand() async {
    if (_switching || _quitting) return;
    _switching = true;
    try {
      await windowManager.setAlwaysOnTop(false);
      await windowManager.setSkipTaskbar(false);
      await windowManager.setResizable(true);
      compact.value = false;
      await windowManager.setMinimumSize(const Size(720, 500));
      if (_fullBounds != null) {
        await windowManager.setBounds(_fullBounds!);
      } else {
        await windowManager.setSize(fullSize);
        await windowManager.center();
      }
      await windowManager.show();
      await windowManager.focus();
      visible.value = true;
      controller.setVisible(true);
    } finally {
      _switching = false;
    }
  }

  Future<void> quit() async {
    if (_quitting) return;
    _quitting = true;
    await controller.shutdown();
    if (_trayReady) await trayManager.destroy();
    trayManager.removeListener(this);
    windowManager.removeListener(this);
    await windowManager.setPreventClose(false);
    await windowManager.destroy();
  }

  @override
  void onTrayIconMouseDown() => unawaited(showCompact());
  @override
  void onTrayIconRightMouseDown() => unawaited(trayManager.popUpContextMenu());
  @override
  void onTrayMenuItemClick(MenuItem menuItem) {
    switch (menuItem.key) {
      case 'compact':
        unawaited(showCompact());
      case 'expand':
        unawaited(expand());
      case 'toggle':
        unawaited(controller.toggle());
      case 'quit':
        unawaited(quit());
    }
  }

  @override
  void onWindowClose() {
    if (_trayReady) {
      unawaited(hide());
    } else {
      unawaited(quit());
    }
  }

  @override
  void onWindowBlur() {
    if (compact.value && !_switching && !_quitting) unawaited(hide());
  }

  @override
  void onWindowMinimize() {
    visible.value = false;
    controller.setVisible(false);
  }

  @override
  void onWindowRestore() {
    visible.value = true;
    controller.setVisible(true);
  }

  @override
  void onWindowFocus() {
    visible.value = true;
    controller.setVisible(true);
  }
}
