import 'dart:async';
import 'dart:math';

import 'package:audio_service/audio_service.dart';
import 'package:flutter/foundation.dart';

import 'data/preferences.dart';
import 'data/radio_api.dart';
import 'domain/radio_models.dart';
import 'playback/radio_audio.dart';

// Общий контроллер принадлежит приложению, а не окнам или карточкам. Переключение
// полного/компактного режима сохраняет плеер, профиль и HTTP-клиент.
class RadioController extends ChangeNotifier {
  RadioController({
    required this.audio,
    required ClientPreferences preferences,
    RadioApi? api,
    Future<void> Function(ClientPreferences)? persist,
  }) : _api = api ?? RadioApi(),
       _persist = persist ?? ((value) => value.save()),
       servers = List.of(preferences.servers),
       volume = preferences.volume,
       _savedAddress = preferences.selectedAddress,
       _savedStation = preferences.selectedStation {
    audio.view.addListener(_onPlayer);
    audio.setStationActions(
      next: () => stepStation(1),
      previous: () => stepStation(-1),
      resume: () async {
        if (!wantsPlayback && selectedStation != null) {
          await selectStation(selectedStation!);
        }
      },
      pause: () async {
        _resumeAfterRefresh = false;
        await audio.stop();
      },
    );
  }

  final LivePlayer audio;
  final RadioApi _api;
  final Future<void> Function(ClientPreferences) _persist;
  final String? _savedAddress;
  final String? _savedStation;
  final List<RadioServer> servers;
  final ValueNotifier<int> clockTick = ValueNotifier(0);
  final Stopwatch _clock = Stopwatch()..start();
  Future<void> _saveTail = Future.value();
  Timer? _poll;
  Timer? _ticker;
  Timer? _volumeSave;
  RadioSnapshot? _snapshot;
  int _receivedMs = 0;
  int _generation = 0;
  int? _fetchingGeneration;
  int _failures = 0;
  bool _closed = false;
  bool _visible = true;
  bool _resumeAfterRefresh = false;
  String? _mediaKey;
  RadioServer? server;
  String? selectedStation;
  String? error;
  bool loading = false;
  double volume;

  List<RadioStation> get stations => _snapshot?.stations ?? const [];
  bool get wantsPlayback => _resumeAfterRefresh || audio.view.value.active;
  RadioStation? get station {
    for (final item in stations) {
      if (item.id == selectedStation) return item;
    }
    return null;
  }

  // Буфер MP3 не имеет точных серверных меток. Используем ту же ограниченную
  // оценку задержки, что и веб; точную синхронизацию протокол пока не обещает.
  int get audibleNowMs =>
      (_snapshot?.serverTime.millisecondsSinceEpoch ?? 0) +
      _clock.elapsedMilliseconds -
      _receivedMs -
      (audio.view.value.active ? audio.bufferedSeconds * 1000 : 0).round();

  RadioTrack? get track {
    final current = station;
    if (current == null) return null;
    if (audio.view.value.active) {
      final now = audibleNowMs;
      for (final item in [
        if (current.current != null) current.current!,
        ...current.history,
      ]) {
        if (item.startedMs <= now &&
            (item.endedMs == 0 || now < item.endedMs)) {
          return item;
        }
      }
    }
    return current.current;
  }

  Duration get elapsed {
    final item = track;
    if (item == null) return Duration.zero;
    final seconds = max(0, (audibleNowMs - item.startedMs) ~/ 1000);
    return Duration(
      seconds: item.duration > 0 ? min(item.duration, seconds) : seconds,
    );
  }

  Future<void> initialize() async {
    await audio.setVolume(volume);
    _startTicker();
    if (servers.isEmpty) return;
    final initial = servers.firstWhere(
      (item) => item.address.toString() == _savedAddress,
      orElse: () => servers.first,
    );
    await selectServer(initial);
  }

  Future<void> selectServer(RadioServer value) async {
    final generation = ++_generation;
    _resumeAfterRefresh = wantsPlayback;
    _poll?.cancel();
    _api.cancel();
    server = value;
    selectedStation = value.address.toString() == _savedAddress
        ? _savedStation
        : null;
    _snapshot = null;
    _mediaKey = null;
    _failures = 0;
    error = null;
    loading = true;
    audio.metadata(null);
    notifyListeners();
    await audio.stop();
    if (_closed || generation != _generation) return;
    _save();
    await refresh();
  }

  Future<void> addServer(RadioServer value) async {
    final index = servers.indexWhere((item) => item.address == value.address);
    if (index < 0) {
      if (servers.length >= 50) {
        throw const FormatException('Можно сохранить не больше 50 серверов');
      }
      servers.add(value);
    } else {
      servers[index] = value;
    }
    await selectServer(value);
  }

  Future<void> removeServer(RadioServer value) async {
    servers.removeWhere((item) => item.address == value.address);
    if (server?.address == value.address) {
      if (servers.isNotEmpty) {
        await selectServer(servers.first);
      } else {
        ++_generation;
        _poll?.cancel();
        _api.cancel();
        server = null;
        selectedStation = null;
        _snapshot = null;
        loading = false;
        error = null;
        _resumeAfterRefresh = false;
        await audio.stop();
        audio.metadata(null);
      }
    }
    _save();
    notifyListeners();
  }

  // Следующий запрос назначается после завершения предыдущего. Скрытый плеер
  // обновляет метаданные реже, а скрытое остановленное приложение не опрашивает API.
  Future<void> refresh() async {
    final current = server;
    final generation = _generation;
    if (_closed || current == null || _fetchingGeneration == generation) return;
    _poll?.cancel();
    _fetchingGeneration = generation;
    try {
      final snapshot = await _api.fetch(current);
      if (_closed || generation != _generation) return;
      _snapshot = snapshot;
      _receivedMs = _clock.elapsedMilliseconds;
      error = null;
      _failures = 0;
      final previous = selectedStation;
      if (!stations.any((item) => item.id == selectedStation)) {
        selectedStation = stations.isEmpty ? null : stations.first.id;
      }
      if (previous != selectedStation) {
        if (audio.view.value.active) {
          _resumeAfterRefresh = true;
        }
        await audio.stop();
        if (_closed || generation != _generation) return;
        _save();
      }
      _updateMedia();
      if (_resumeAfterRefresh && selectedStation != null) {
        _resumeAfterRefresh = false;
        unawaited(audio.open(current.stream(selectedStation!)));
      }
    } catch (_) {
      if (_closed || generation != _generation) return;
      _failures++;
      error =
          'Не удалось связаться с ${current.name}. Проверьте адрес и подключение.';
    } finally {
      if (_fetchingGeneration == generation) _fetchingGeneration = null;
      if (!_closed && generation == _generation) {
        loading = false;
        notifyListeners();
        _schedulePoll();
      }
    }
  }

  void _schedulePoll() {
    _poll?.cancel();
    if (_closed ||
        server == null ||
        (!_visible && !audio.view.value.active && !_resumeAfterRefresh)) {
      return;
    }
    final seconds = _failures > 0
        ? min(30, 5 * (1 << min(_failures - 1, 3)))
        : (_visible ? 5 : 20);
    _poll = Timer(Duration(seconds: seconds), () => unawaited(refresh()));
  }

  Future<void> selectStation(String id) async {
    final current = server;
    if (current == null || !stations.any((item) => item.id == id)) return;
    selectedStation = id;
    _resumeAfterRefresh = false;
    _mediaKey = null;
    _save();
    _updateMedia();
    notifyListeners();
    await audio.open(current.stream(id));
  }

  Future<void> stepStation(int delta) async {
    if (stations.isEmpty) return;
    final index = stations.indexWhere((item) => item.id == selectedStation);
    await selectStation(stations[(max(0, index) + delta) % stations.length].id);
  }

  Future<void> toggle() async {
    if (wantsPlayback) {
      _resumeAfterRefresh = false;
      await audio.stop();
    } else if (selectedStation != null) {
      await selectStation(selectedStation!);
    }
  }

  Future<void> setVolume(double value) async {
    volume = value.clamp(0, 1);
    await audio.setVolume(volume);
    _volumeSave?.cancel();
    _volumeSave = Timer(const Duration(milliseconds: 350), _save);
    notifyListeners();
  }

  // Метаданные уходят в media session только при изменении трека/станции.
  // Графический artwork ограничен настройками AudioService при инициализации.
  void _updateMedia() {
    final item = track;
    final current = station;
    if (server == null || current == null) return;
    final key =
        '${server!.address}|${current.id}|${item?.id}|${item?.startedMs}|${item?.title}|${item?.artist}|${item?.coverPath}';
    if (key == _mediaKey) return;
    _mediaKey = key;
    audio.metadata(
      MediaItem(
        id: server!.stream(current.id).toString(),
        title: item?.title.isNotEmpty == true ? item!.title : current.id,
        artist: item?.artist,
        album: '${server!.name} · ${current.id}',
        artUri: server!.cover(item?.coverPath),
        isLive: true,
      ),
    );
  }

  void _onPlayer() {
    if (_closed) return;
    _updateMedia();
    _schedulePoll();
    notifyListeners();
  }

  // Секундный таймер обновляет только маленькую полосу времени. Скрытый UI
  // не держит таймеров отрисовки, а метаданные продолжают обновляться отдельно.
  void setVisible(bool value) {
    if (_closed || value == _visible) return;
    _visible = value;
    _ticker?.cancel();
    _ticker = null;
    if (value) {
      _startTicker();
      unawaited(refresh());
    }
    _schedulePoll();
  }

  void _startTicker() {
    if (!_visible) return;
    _ticker ??= Timer.periodic(const Duration(seconds: 1), (_) {
      if (station?.current == null) return;
      clockTick.value++;
      _updateMedia();
    });
  }

  // Последовательная запись не даёт медленному сохранению старой громкости
  // затереть более новый профиль. Данные малы и не содержат музыкального кеша.
  void _save() {
    final value = ClientPreferences(
      servers: List.of(servers),
      selectedAddress: server?.address.toString(),
      selectedStation: selectedStation,
      volume: volume,
    );
    _saveTail = _saveTail.then((_) => _persist(value)).catchError((Object _) {
      if (!_closed) {
        error = 'Не удалось сохранить настройки приложения';
        notifyListeners();
      }
    });
  }

  Future<void> shutdown() async {
    if (_closed) return;
    _closed = true;
    ++_generation;
    _poll?.cancel();
    _ticker?.cancel();
    if (_volumeSave?.isActive == true) {
      _volumeSave!.cancel();
      _save();
    }
    _api.close();
    audio.view.removeListener(_onPlayer);
    await audio.shutdown();
    await _saveTail;
    _clock.stop();
    clockTick.dispose();
    super.dispose();
  }
}
