import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';

import 'package:radiopump/data/preferences.dart';
import 'package:radiopump/domain/radio_models.dart';
import 'package:radiopump/platform/desktop_shell.dart';
import 'package:radiopump/playback/radio_audio.dart';
import 'package:radiopump/radio_controller.dart';
import 'package:radiopump/ui/radio_app.dart';

// Отдельная точка входа для измерений release-сборки Windows. Не читает и не
// меняет пользовательские настройки. Сервер должен содержать тестовую музыку.
Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  const address = String.fromEnvironment('RADIOPUMP_TEST_SERVER');
  const reportPath = String.fromEnvironment('RADIOPUMP_REPORT');
  if (!Platform.isWindows || address.isEmpty || reportPath.isEmpty) exit(2);
  PaintingBinding.instance.imageCache
    ..maximumSize = 12
    ..maximumSizeBytes = 8 * 1024 * 1024;
  final trace = <String>[];
  final audio = RadioAudio(
    diagnostic: (event) {
      if (trace.length < 200) trace.add(event);
    },
  );
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
  final key = GlobalKey();
  final samples = <Map<String, Object?>>[];
  final report = File(reportPath);
  var succeeded = false;

  void sample(String stage) {
    samples.add({
      'stage': stage,
      'time': DateTime.now().toUtc().toIso8601String(),
      'rss_bytes': ProcessInfo.currentRss,
      'phase': audio.view.value.phase.name,
      'position_ms': audio.positionMilliseconds,
      'image_cache_bytes': PaintingBinding.instance.imageCache.currentSizeBytes,
    });
  }

  Future<void> capture(String name) async {
    await Future<void>.delayed(const Duration(milliseconds: 500));
    final boundary =
        key.currentContext!.findRenderObject()! as RenderRepaintBoundary;
    final image = await boundary.toImage();
    final data = await image.toByteData(format: ui.ImageByteFormat.png);
    await File(
      '${report.parent.path}/$name.png',
    ).writeAsBytes(data!.buffer.asUint8List());
    image.dispose();
  }

  runApp(
    RepaintBoundary(
      key: key,
      child: RadioPumpApp(controller: controller, desktop: desktop),
    ),
  );
  try {
    await desktop.initialize();
    await controller.initialize();
    await Future<void>.delayed(const Duration(seconds: 3));
    sample('idle');
    if (controller.error != null) throw StateError(controller.error!);
    if (desktop.trayError != null) throw StateError(desktop.trayError!);
    await controller.toggle();
    await Future<void>.delayed(const Duration(seconds: 20));
    if (audio.view.value.phase != PlayerPhase.playing) {
      throw StateError('Не начался эфир');
    }
    sample('playing');
    await capture('native-desktop');
    await desktop.showCompact();
    await Future<void>.delayed(const Duration(seconds: 3));
    sample('compact');
    await capture('native-compact');
    await desktop.hide();
    for (var index = 0; index < 6; index++) {
      await Future<void>.delayed(const Duration(seconds: 5));
      sample('hidden-$index');
    }
    if (audio.view.value.phase != PlayerPhase.playing) {
      throw StateError('Эфир остановился в трее');
    }
    await desktop.expand();
    for (var index = 0; index < 12; index++) {
      await controller.stepStation(1);
      await Future<void>.delayed(const Duration(milliseconds: 900));
    }
    await Future<void>.delayed(const Duration(seconds: 12));
    for (
      var attempt = 0;
      attempt < 20 && audio.view.value.phase != PlayerPhase.playing;
      attempt++
    ) {
      await Future<void>.delayed(const Duration(seconds: 1));
    }
    sample('after-switching');
    if (audio.view.value.phase != PlayerPhase.playing) {
      throw StateError('Переключение сломало эфир');
    }
    await controller.toggle();
    await desktop.hide();
    await Future<void>.delayed(const Duration(seconds: 5));
    sample('stopped-hidden');
    succeeded = true;
  } catch (error, stack) {
    sample('failure');
    await report.writeAsString(
      jsonEncode({
        'success': false,
        'trace': trace,
        'error': error.toString(),
        'stack': stack.toString(),
        'samples': samples,
      }),
    );
  } finally {
    if (succeeded) {
      await report.writeAsString(
        const JsonEncoder.withIndent(
          '  ',
        ).convert({'success': true, 'samples': samples}),
      );
    }
    await desktop.quit();
    exit(succeeded ? 0 : 1);
  }
}
