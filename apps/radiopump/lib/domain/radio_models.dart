import 'dart:convert';

// Профиль хранит адрес конкретного владельца радио, включая префикс reverse proxy.
// Пароли не нужны: текущее API прослушивания публично.
class RadioServer {
  RadioServer({required this.name, required Uri address})
    : address = normalizeAddress(address);

  final String name;
  final Uri address;

  static Uri normalizeAddress(Uri value) {
    if (!['http', 'https'].contains(value.scheme) ||
        value.host.isEmpty ||
        value.userInfo.isNotEmpty ||
        value.hasQuery ||
        value.hasFragment) {
      throw const FormatException(
        'Укажите адрес http:// или https:// без пароля и параметров',
      );
    }
    return value.replace(path: '${value.path.replaceAll(RegExp(r'/+$'), '')}/');
  }

  Uri endpoint(String path) => address.resolve(path);
  Uri stream(String station) =>
      endpoint('stream/${Uri.encodeComponent(station)}');

  // Обложки принадлежат тому же серверу. Внешние URL и переход на другой origin
  // не допускаются; относительные серверные пути учитывают префикс установки.
  Uri? cover(String? path) {
    if (path == null || path.isEmpty) return null;
    final parsed = Uri.tryParse(path);
    if (parsed == null || parsed.hasScheme || parsed.hasAuthority) return null;
    final result = endpoint(path.replaceFirst(RegExp(r'^/+'), ''));
    return result.origin == address.origin ? result : null;
  }

  Map<String, String> toJson() => {'name': name, 'address': address.toString()};

  static RadioServer fromJson(Map<String, dynamic> json) => RadioServer(
    name: json['name'] as String,
    address: Uri.parse(json['address'] as String),
  );
}

// Клиент оставляет только текущие метаданные и короткую историю для учёта
// задержки. Будущая очередь и серверная библиотека в память не загружаются.
class RadioTrack {
  const RadioTrack({
    required this.id,
    required this.title,
    required this.artist,
    required this.album,
    required this.duration,
    required this.startedMs,
    required this.endedMs,
    this.coverPath,
  });

  final int id;
  final String title;
  final String artist;
  final String album;
  final int duration;
  final int startedMs;
  final int endedMs;
  final String? coverPath;

  factory RadioTrack.fromJson(Map<String, dynamic> json) => RadioTrack(
    id: (json['id'] as num?)?.toInt() ?? 0,
    title: json['title'] as String? ?? '',
    artist: json['artist'] as String? ?? '',
    album: json['album'] as String? ?? '',
    duration: (json['duration'] as num?)?.toInt() ?? 0,
    startedMs: (json['started_ms'] as num?)?.toInt() ?? 0,
    endedMs: (json['ended_ms'] as num?)?.toInt() ?? 0,
    coverPath: json['cover_url'] as String?,
  );
}

class RadioStation {
  const RadioStation({
    required this.id,
    required this.status,
    required this.tags,
    required this.history,
    this.current,
  });

  final String id;
  final String status;
  final List<String> tags;
  final RadioTrack? current;
  final List<RadioTrack> history;

  factory RadioStation.fromJson(Map<String, dynamic> json) => RadioStation(
    id: json['id'] as String,
    status: json['status'] as String? ?? 'starting',
    tags: (json['tags'] as List? ?? const []).whereType<String>().toList(
      growable: false,
    ),
    current: json['current'] == null
        ? null
        : RadioTrack.fromJson(json['current'] as Map<String, dynamic>),
    history: (json['history'] as List? ?? const [])
        .take(5)
        .map((item) => RadioTrack.fromJson(item as Map<String, dynamic>))
        .toList(growable: false),
  );

  String get description => switch (status) {
    'empty' => 'Пока нет музыки',
    'decode_error' => 'Ошибка аудио на сервере',
    'error' => 'Эфир временно недоступен',
    'stopped' => 'Эфир остановлен',
    _ => 'Станция готовится к эфиру',
  };
}

class RadioSnapshot {
  const RadioSnapshot({required this.serverTime, required this.stations});
  final DateTime serverTime;
  final List<RadioStation> stations;

  // Размер HTTP-ответа ограничен транспортом; дополнительный предел защищает
  // интерфейс от неожиданного числа волн в чужой установке.
  factory RadioSnapshot.decode(String body) {
    final json = jsonDecode(body) as Map<String, dynamic>;
    final entries = json['stations'] as List;
    if (entries.length > 500) throw const FormatException('Слишком много волн');
    final stations = entries
        .map((item) => RadioStation.fromJson(item as Map<String, dynamic>))
        .toList(growable: false);
    if (stations.map((station) => station.id).toSet().length !=
        stations.length) {
      throw const FormatException('Сервер вернул повторяющиеся волны');
    }
    return RadioSnapshot(
      serverTime: DateTime.parse(json['server_time'] as String),
      stations: stations,
    );
  }
}
