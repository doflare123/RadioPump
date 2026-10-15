import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:radiopump/data/preferences.dart';
import 'package:radiopump/domain/radio_models.dart';
import 'package:radiopump/platform/desktop_shell.dart';
import 'package:radiopump/playback/radio_audio.dart';
import 'package:radiopump/radio_controller.dart';
import 'package:radiopump/ui/radio_app.dart';
import 'package:tray_manager/tray_manager.dart';
import 'package:window_manager/window_manager.dart';

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  // Запускается отдельно от unit tests против настоящего сервера с синтетической
  // библиотекой. Пользовательские настройки и музыкальные файлы не используются.
  testWidgets('реальный Windows-плеер, общий эфир и переключение окна', (
    tester,
  ) async {
    const address = String.fromEnvironment('RADIOPUMP_TEST_SERVER');
    expect(
      address,
      isNotEmpty,
      reason:
          'Передайте --dart-define=RADIOPUMP_TEST_SERVER=http://127.0.0.1:PORT',
    );
    expect(Platform.isWindows, isTrue);
    final audio = RadioAudio();
    final controller = RadioController(
      audio: audio,
      preferences: ClientPreferences(
        servers: [
          RadioServer(name: 'Тестовый эфир', address: Uri.parse(address)),
        ],
      ),
      persist: (_) async {},
    );
    final desktop = DesktopShell(controller);
    await tester.pumpWidget(
      RadioPumpApp(controller: controller, desktop: desktop),
    );
    await tester.runAsync(() async {
      await desktop.initialize();
      await controller.initialize();
      await controller.toggle();
      final deadline = DateTime.now().add(const Duration(seconds: 30));
      while (audio.view.value.phase != PlayerPhase.playing &&
          DateTime.now().isBefore(deadline)) {
        await Future<void>.delayed(const Duration(milliseconds: 250));
      }
    });
    await tester.pump();
    expect(audio.view.value.phase, PlayerPhase.playing);
    expect(controller.station, isNotNull);
    expect(desktop.trayError, isNull);
    await tester.runAsync(() async {
      await desktop.showCompact();
      await Future<void>.delayed(const Duration(seconds: 2));
    });
    await tester.pumpAndSettle();
    expect(desktop.compact.value, isTrue);
    expect(find.text('Развернуть'), findsOneWidget);
    expect(audio.view.value.active, isTrue);
    await tester.runAsync(() async {
      await desktop.hide();
      expect(await windowManager.isVisible(), isFalse);
      final before = audio.positionMilliseconds;
      await Future<void>.delayed(const Duration(seconds: 5));
      expect(audio.view.value.active, isTrue);
      expect(audio.positionMilliseconds, greaterThan(before));
      await desktop.expand();
    });
    await tester.pumpAndSettle();
    expect(desktop.compact.value, isFalse);
    expect(audio.view.value.active, isTrue);
    await tester.runAsync(() async {
      if (controller.stations.length > 1) await controller.stepStation(1);
      await Future<void>.delayed(const Duration(seconds: 3));
      await controller.toggle();
    });
    await tester.pump();
    expect(audio.view.value.phase, PlayerPhase.stopped);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
    await tester.runAsync(() async {
      await controller.shutdown();
      await trayManager.destroy();
      trayManager.removeListener(desktop);
      windowManager.removeListener(desktop);
      await windowManager.setPreventClose(false);
    });
  });
}
