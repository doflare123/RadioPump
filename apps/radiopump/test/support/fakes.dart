import 'dart:async';

import 'package:audio_service/audio_service.dart';
import 'package:flutter/foundation.dart';
import 'package:radiopump/data/preferences.dart';
import 'package:radiopump/data/radio_api.dart';
import 'package:radiopump/domain/radio_models.dart';
import 'package:radiopump/playback/radio_audio.dart';
import 'package:radiopump/radio_controller.dart';

// Подмена только границы аудиоустройства: тесты упражняют настоящий контроллер,
// HTTP-модели и UI, не запуская нативные плагины и сеть.
class FakePlayer implements LivePlayer {
  @override
  final view = ValueNotifier(const PlayerView(PlayerPhase.stopped));
  Uri? source;
  final opened = <Uri>[];
  MediaItem? item;
  int stops = 0;
  double volume = 0;
  @override
  double bufferedSeconds = 0;
  Future<void> Function()? next;
  Future<void> Function()? previous;
  Future<void> Function()? resume;
  Future<void> Function()? pause;

  @override
  Future<void> open(Uri source) async {
    this.source = source;
    opened.add(source);
    view.value = const PlayerView(PlayerPhase.playing);
  }

  @override
  Future<void> play() async {
    if (source != null) await open(source!);
  }

  @override
  Future<void> stop() async {
    stops++;
    view.value = const PlayerView(PlayerPhase.stopped);
  }

  @override
  Future<void> setVolume(double value) async => volume = value;
  @override
  void metadata(MediaItem? item) => this.item = item;
  @override
  void setStationActions({
    required Future<void> Function() next,
    required Future<void> Function() previous,
    required Future<void> Function() resume,
    required Future<void> Function() pause,
  }) {
    this.next = next;
    this.previous = previous;
    this.resume = resume;
    this.pause = pause;
  }

  @override
  Future<void> shutdown() async => view.dispose();
}

class FakeApi extends RadioApi {
  Future<RadioSnapshot> Function(RadioServer)? handler;
  int requests = 0;
  @override
  Future<RadioSnapshot> fetch(RadioServer server) {
    requests++;
    return handler?.call(server) ?? Future.value(exampleSnapshot());
  }

  @override
  void cancel() {}
}

RadioSnapshot exampleSnapshot({String firstStation = 'Общая волна'}) {
  final now = DateTime.now().toUtc();
  return RadioSnapshot(
    serverTime: now,
    stations: [
      RadioStation(
        id: firstStation,
        status: 'playing',
        tags: const ['rock', 'indie'],
        history: const [],
        current: RadioTrack(
          id: 1,
          title: 'Ночной город',
          artist: 'Тихие улицы',
          album: 'После полуночи',
          duration: 223,
          startedMs: now.millisecondsSinceEpoch - 83000,
          endedMs: 0,
        ),
      ),
      const RadioStation(
        id: 'Спокойная',
        status: 'empty',
        tags: ['ambient'],
        history: [],
      ),
    ],
  );
}

RadioController exampleController({FakeApi? api, FakePlayer? player}) =>
    RadioController(
      audio: player ?? FakePlayer(),
      api: api ?? FakeApi(),
      preferences: ClientPreferences(
        servers: [
          RadioServer(
            name: 'Радио Александра',
            address: Uri.parse('https://radio.example.org'),
          ),
          RadioServer(
            name: 'Радио друга',
            address: Uri.parse('https://friend.example.org'),
          ),
        ],
      ),
      persist: (_) async {},
    );
