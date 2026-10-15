import 'dart:convert';

import 'package:shared_preferences/shared_preferences.dart';

import '../domain/radio_models.dart';

// Хранятся только адреса, выбранная волна и громкость. Нет кеша библиотеки
// или автоматического запуска музыки после открытия приложения.
class ClientPreferences {
  ClientPreferences({
    this.servers = const [],
    this.selectedAddress,
    this.selectedStation,
    this.volume = 0.8,
  });

  final List<RadioServer> servers;
  final String? selectedAddress;
  final String? selectedStation;
  final double volume;

  static Future<ClientPreferences> load() async {
    final store = SharedPreferencesAsync();
    final raw = await store.getString('radiopump.client');
    if (raw == null || raw.length > 64 * 1024) return ClientPreferences();
    try {
      final json = jsonDecode(raw) as Map<String, dynamic>;
      return ClientPreferences(
        servers: (json['servers'] as List)
            .take(50)
            .map((item) => RadioServer.fromJson(item as Map<String, dynamic>))
            .toList(growable: false),
        selectedAddress: json['selectedAddress'] as String?,
        selectedStation: json['selectedStation'] as String?,
        volume: ((json['volume'] as num?)?.toDouble() ?? 0.8).clamp(0, 1),
      );
    } on FormatException {
      return ClientPreferences();
    } on TypeError {
      return ClientPreferences();
    }
  }

  Future<void> save() => SharedPreferencesAsync().setString(
    'radiopump.client',
    jsonEncode({
      'servers': servers.map((server) => server.toJson()).toList(),
      'selectedAddress': selectedAddress,
      'selectedStation': selectedStation,
      'volume': volume,
    }),
  );
}
