import 'dart:async';
import 'dart:io';

import 'package:audio_service/audio_service.dart';
import 'package:audio_session/audio_session.dart';
import 'package:flutter/material.dart';
import 'package:flutter_cache_manager/flutter_cache_manager.dart';
import 'package:just_audio_media_kit/just_audio_media_kit.dart';

import 'data/preferences.dart';
import 'platform/desktop_shell.dart';
import 'playback/radio_audio.dart';
import 'radio_controller.dart';
import 'ui/radio_app.dart';

// Плеер и media session создаются один раз. На Windows используется WinRT,
// на Android/Apple — платформенные декодеры, а libmpv загружается только Linux.
Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  PaintingBinding.instance.imageCache
    ..maximumSize = 12
    ..maximumSizeBytes = 8 * 1024 * 1024;

  if (Platform.isLinux) {
    JustAudioMediaKit.bufferSize = 1024 * 1024;
    JustAudioMediaKit.prefetchPlaylist = false;
    JustAudioMediaKit.ensureInitialized(linux: true, windows: false);
  }
  final RadioAudio audio;
  if (Platform.isAndroid || Platform.isIOS || Platform.isMacOS) {
    audio = await AudioService.init<RadioAudio>(
      builder: RadioAudio.new,
      // Для системной обложки нужен файл. Короткий кеш хранит восемь записей
      // с JSON-индексом и не открывает SQLite ради картинок уведомления.
      cacheManager: CacheManager(
        Config(
          'radiopump_art',
          stalePeriod: const Duration(days: 1),
          maxNrOfCacheObjects: 8,
          repo: JsonCacheInfoRepository(databaseName: 'radiopump_art'),
        ),
      ),
      config: const AudioServiceConfig(
        androidNotificationChannelId: 'dev.radiopump.playback',
        androidNotificationChannelName: 'Эфир RadioPump',
        androidNotificationIcon: 'drawable/ic_stat_radio',
        androidNotificationOngoing: true,
        artDownscaleWidth: 256,
        artDownscaleHeight: 256,
      ),
    );
    final session = await AudioSession.instance;
    await session.configure(const AudioSessionConfiguration.music());
    audio.attachAudioSession(session);
  } else {
    audio = RadioAudio();
  }
  final controller = RadioController(
    audio: audio,
    preferences: await ClientPreferences.load(),
  );
  final desktop = isDesktop ? DesktopShell(controller) : null;
  runApp(RadioPumpApp(controller: controller, desktop: desktop));
  if (desktop != null) await desktop.initialize();
  unawaited(controller.initialize());
}
