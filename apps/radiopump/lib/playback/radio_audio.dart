import 'dart:async';
import 'dart:math';

import 'package:audio_service/audio_service.dart';
import 'package:audio_session/audio_session.dart';
import 'package:flutter/foundation.dart';
import 'package:just_audio/just_audio.dart';

// Узкий интерфейс позволяет проверять переключения и lifecycle без настоящего
// аудиоустройства. Во всём приложении существует только одна его реализация.
abstract interface class LivePlayer {
  ValueNotifier<PlayerView> get view;
  double get bufferedSeconds;
  Future<void> open(Uri source);
  Future<void> stop();
  Future<void> play();
  Future<void> setVolume(double value);
  void metadata(MediaItem? item);
  void setStationActions({
    required Future<void> Function() next,
    required Future<void> Function() previous,
    required Future<void> Function() resume,
    required Future<void> Function() pause,
  });
  Future<void> shutdown();
}

enum PlayerPhase { stopped, connecting, playing, buffering, reconnecting }

@immutable
class PlayerView {
  const PlayerView(this.phase, {this.error});
  final PlayerPhase phase;
  final String? error;
  bool get active => phase != PlayerPhase.stopped;
}

// Системный media session управляет тем же плеером, что и окно. Пауза live-радио
// закрывает подписку; следующее включение подключается к текущему эфиру.
class RadioAudio extends BaseAudioHandler implements LivePlayer {
  RadioAudio({this.diagnostic})
    : _player = AudioPlayer(
        useProxyForRequestHeaders: false,
        audioLoadConfiguration: const AudioLoadConfiguration(
          androidLoadControl: AndroidLoadControl(
            minBufferDuration: Duration(seconds: 2),
            maxBufferDuration: Duration(seconds: 8),
            bufferForPlaybackDuration: Duration(milliseconds: 500),
            bufferForPlaybackAfterRebufferDuration: Duration(seconds: 1),
            targetBufferBytes: 256 * 1024,
          ),
          darwinLoadControl: DarwinLoadControl(
            preferredForwardBufferDuration: Duration(seconds: 5),
          ),
        ),
      ) {
    _subscriptions.add(_player.playerStateStream.listen(_onState));
    _subscriptions.add(_player.errorStream.listen((_) => _recover()));
    _subscriptions.add(
      _player
          .createPositionStream(
            minPeriod: const Duration(seconds: 2),
            maxPeriod: const Duration(seconds: 2),
          )
          .listen((position) {
            if (position > _lastPosition) {
              _lastPosition = position;
              _lastProgress = _clock.elapsed;
              if (position > const Duration(seconds: 15)) _failures = 0;
            }
          }),
    );
    _publish(PlayerPhase.stopped);
  }

  final AudioPlayer _player;
  // Не используется обычным приложением; отдельная release-проверка может
  // записывать переходы нативного проигрывателя без логирования URL и секретов.
  final void Function(String)? diagnostic;
  int get positionMilliseconds => _player.position.inMilliseconds;
  final List<StreamSubscription<dynamic>> _subscriptions = [];
  final Stopwatch _clock = Stopwatch()..start();
  final Random _random = Random();
  @override
  final ValueNotifier<PlayerView> view = ValueNotifier(
    const PlayerView(PlayerPhase.stopped),
  );
  Uri? _source;
  Timer? _retry;
  Timer? _watchdog;
  Duration _lastPosition = Duration.zero;
  Duration _lastProgress = Duration.zero;
  int _generation = 0;
  int _failures = 0;
  bool _wanted = false;
  bool _closed = false;
  bool _interrupted = false;
  Future<void> Function()? _next;
  Future<void> Function()? _previous;
  Future<void> Function()? _resume;
  Future<void> Function()? _pause;

  @override
  double get bufferedSeconds => max(
    0,
    (_player.bufferedPosition - _player.position).inMilliseconds / 1000,
  ).clamp(0, 30).toDouble();

  // Watchdog не должен включать музыку во время звонка или после отключения
  // наушников. Сам плеер обрабатывает аудиофокус, этот флаг защищает наш retry.
  void attachAudioSession(AudioSession session) {
    _subscriptions.add(
      session.interruptionEventStream.listen((event) {
        _interrupted = event.begin;
        _lastProgress = _clock.elapsed;
        if (!event.begin && event.type == AudioInterruptionType.unknown) {
          unawaited(stop());
        }
      }),
    );
    _subscriptions.add(
      session.becomingNoisyEventStream.listen((_) => unawaited(stop())),
    );
  }

  @override
  void setStationActions({
    required Future<void> Function() next,
    required Future<void> Function() previous,
    required Future<void> Function() resume,
    required Future<void> Function() pause,
  }) {
    _next = next;
    _previous = previous;
    _resume = resume;
    _pause = pause;
  }

  @override
  void metadata(MediaItem? item) => mediaItem.add(item);

  void _publish(PlayerPhase phase, {String? error}) {
    if (_closed) return;
    view.value = PlayerView(phase, error: error);
    playbackState.add(
      PlaybackState(
        controls: [
          MediaControl.skipToPrevious,
          if (_wanted) MediaControl.pause else MediaControl.play,
          MediaControl.skipToNext,
        ],
        androidCompactActionIndices: const [0, 1, 2],
        processingState: switch (phase) {
          PlayerPhase.connecting => AudioProcessingState.loading,
          PlayerPhase.buffering ||
          PlayerPhase.reconnecting => AudioProcessingState.buffering,
          PlayerPhase.playing => AudioProcessingState.ready,
          PlayerPhase.stopped => AudioProcessingState.idle,
        },
        playing: _wanted,
        // Поток не поддерживает seek. Системе не передаётся фиктивная позиция файла.
        systemActions: const {},
      ),
    );
  }

  void _onState(PlayerState state) {
    diagnostic?.call(
      'native ${state.processingState.name}, playing=${state.playing}, wanted=$_wanted, generation=$_generation',
    );
    if (!_wanted || _closed || _retry != null) return;
    switch (state.processingState) {
      case ProcessingState.completed:
        _recover();
      case ProcessingState.buffering:
        _publish(PlayerPhase.buffering);
      case ProcessingState.ready:
        _publish(state.playing ? PlayerPhase.playing : PlayerPhase.connecting);
      case ProcessingState.loading:
        _publish(PlayerPhase.connecting);
      case ProcessingState.idle:
        break;
    }
  }

  @override
  Future<void> open(Uri source) async {
    if (_closed) return;
    _source = source;
    _wanted = true;
    _failures = 0;
    _retry?.cancel();
    _retry = null;
    _watchdog ??= Timer.periodic(const Duration(seconds: 5), (_) {
      if (_wanted &&
          !_interrupted &&
          _retry == null &&
          _clock.elapsed - _lastProgress > const Duration(seconds: 25)) {
        _recover();
      }
    });
    await _connect();
  }

  // Поколение проверяется после каждого await. Позднее завершение старого URL
  // не может включить прежнюю станцию после быстрого переключения владельца.
  Future<void> _connect() async {
    if (!_wanted || _closed || _source == null) return;
    final generation = ++_generation;
    _lastPosition = Duration.zero;
    _lastProgress = _clock.elapsed;
    _publish(PlayerPhase.connecting);
    try {
      await _player.stop();
      diagnostic?.call(
        'stopped before load, generation=$generation/$_generation',
      );
      if (generation != _generation || !_wanted) return;
      await _player.setUrl(_source.toString());
      diagnostic?.call(
        'loaded, generation=$generation/$_generation, wanted=$_wanted',
      );
      if (generation != _generation || !_wanted) return;
      unawaited(
        _player.play().catchError((Object _) {
          if (generation == _generation) _recover();
        }),
      );
    } catch (error) {
      diagnostic?.call('load failed: $error');
      if (generation == _generation) _recover();
    }
  }

  // Backoff ограничивает расход сети и CPU при недоступном сервере. Остановка
  // отменяет как таймер, так и незавершённый запуск нативного проигрывателя.
  void _recover() {
    if (!_wanted || _closed || _retry != null) return;
    ++_generation;
    final seconds = min(30, 1 << min(_failures++, 5));
    _publish(PlayerPhase.reconnecting, error: 'Восстанавливаем связь…');
    _retry = Timer(
      Duration(milliseconds: seconds * 1000 + _random.nextInt(400)),
      () {
        _retry = null;
        unawaited(_connect());
      },
    );
    unawaited(_player.stop().catchError((Object _) {}));
  }

  @override
  Future<void> play() async {
    // Системная кнопка использует текущий профиль контроллера, а не сохранённый
    // URL: удалённый или сменённый сервер не должен сам включиться из гарнитуры.
    if (_resume != null) {
      await _resume!();
    } else if (_source != null && !_wanted) {
      await open(_source!);
    }
  }

  @override
  Future<void> pause() => _pause?.call() ?? stop();

  @override
  Future<void> stop() async {
    _wanted = false;
    ++_generation;
    _retry?.cancel();
    _retry = null;
    _watchdog?.cancel();
    _watchdog = null;
    _publish(PlayerPhase.stopped);
    await _player.stop();
  }

  @override
  Future<void> skipToNext() async => await _next?.call();
  @override
  Future<void> skipToPrevious() async => await _previous?.call();
  @override
  Future<void> onTaskRemoved() => pause();
  @override
  Future<void> setVolume(double value) => _player.setVolume(value.clamp(0, 1));

  @override
  Future<void> shutdown() async {
    await stop();
    _closed = true;
    for (final subscription in _subscriptions) {
      await subscription.cancel();
    }
    await _player.dispose();
    view.dispose();
    _clock.stop();
  }
}
