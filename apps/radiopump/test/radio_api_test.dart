import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:radiopump/data/radio_api.dart';
import 'package:radiopump/domain/radio_models.dart';

void main() {
  late HttpServer server;
  late RadioApi api;
  late RadioServer profile;

  setUp(() async {
    server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    profile = RadioServer(
      name: 'Тест',
      address: Uri.parse('http://127.0.0.1:${server.port}/radio'),
    );
    api = RadioApi();
  });
  tearDown(() async {
    api.close();
    await server.close(force: true);
  });

  // Реальный HTTP подтверждает префикс reverse proxy и отсутствие привязки
  // протокола к браузерным cookies или localStorage.
  test('читает состояние через HTTP без авторизации', () async {
    server.listen((request) async {
      expect(request.uri.path, '/radio/api/radio');
      expect(request.headers.value(HttpHeaders.authorizationHeader), isNull);
      request.response.headers.contentType = ContentType.json;
      request.response.write(
        jsonEncode({'server_time': '2026-10-15T12:00:00Z', 'stations': []}),
      );
      await request.response.close();
    });
    expect((await api.fetch(profile)).stations, isEmpty);
  });

  test('не следует редиректу чужого API', () async {
    server.listen((request) async {
      request.response.statusCode = 302;
      request.response.headers.set(
        HttpHeaders.locationHeader,
        'https://outside.example',
      );
      await request.response.close();
    });
    await expectLater(api.fetch(profile), throwsA(isA<HttpException>()));
  });

  test('размер chunked-ответа ограничен до JSON decode', () async {
    server.listen((request) async {
      try {
        final chunk = List.filled(64 * 1024, 32);
        for (var index = 0; index < 34; index++) {
          request.response.add(chunk);
          await request.response.flush();
        }
        await request.response.close();
      } catch (_) {
        // Клиент обязан оборвать слишком большой ответ раньше сервера.
      }
    });
    await expectLater(api.fetch(profile), throwsFormatException);
  });

  test('смена сервера отменяет незавершённый запрос', () async {
    final arrived = Completer<void>();
    server.listen((request) {
      arrived.complete();
    });
    final pending = api.fetch(profile);
    final cancelled = expectLater(pending, throwsA(isA<Exception>()));
    await arrived.future;
    api.cancel();
    await cancelled;
  });
}
