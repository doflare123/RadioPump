import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import '../domain/radio_models.dart';

// Один HTTP-клиент переиспользует соединение. Общий таймаут включает чтение
// тела; потоковые и слишком большие ответы не могут навсегда занять polling.
class RadioApi {
  HttpClient _client = _newClient();

  static HttpClient _newClient() => HttpClient()
    ..connectionTimeout = const Duration(seconds: 8)
    ..idleTimeout = const Duration(seconds: 20)
    ..maxConnectionsPerHost = 2
    ..userAgent = 'RadioPump/0.1';

  Future<RadioSnapshot> fetch(RadioServer server) async {
    final client = _client;
    try {
      return await _fetch(client, server).timeout(const Duration(seconds: 10));
    } on TimeoutException {
      if (identical(client, _client)) cancel();
      rethrow;
    }
  }

  Future<RadioSnapshot> _fetch(HttpClient client, RadioServer server) async {
    final request = await client.getUrl(server.endpoint('api/radio'));
    request.followRedirects = false;
    request.headers.set(HttpHeaders.acceptHeader, 'application/json');
    final response = await request.close();
    if (response.statusCode != HttpStatus.ok) {
      request.abort();
      throw HttpException('Сервер ответил ${response.statusCode}');
    }
    const limit = 2 * 1024 * 1024;
    if (response.contentLength > limit) {
      request.abort();
      throw const FormatException('Ответ сервера слишком большой');
    }
    final bytes = BytesBuilder(copy: false);
    await for (final chunk in response) {
      if (bytes.length + chunk.length > limit) {
        request.abort();
        throw const FormatException('Ответ сервера слишком большой');
      }
      bytes.add(chunk);
    }
    return RadioSnapshot.decode(utf8.decode(bytes.takeBytes()));
  }

  // При смене владельца старый запрос прерывается сразу, а не после таймаута.
  void cancel() {
    _client.close(force: true);
    _client = _newClient();
  }

  void close() => _client.close(force: true);
}
