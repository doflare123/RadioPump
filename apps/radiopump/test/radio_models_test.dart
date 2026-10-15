import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:radiopump/domain/radio_models.dart';

void main() {
  // Адреса чужих self-hosted установок могут иметь префикс и имена волн с
  // Unicode/слешами. Проверяем формирование реального URL, а не только строки UI.
  test('адреса API, обложек и волн сохраняют префикс установки', () {
    final server = RadioServer(
      name: 'Друг',
      address: Uri.parse('https://example.org/radio///'),
    );
    expect(
      server.endpoint('api/radio').toString(),
      'https://example.org/radio/api/radio',
    );
    expect(server.stream('Рок / джаз').pathSegments.last, 'Рок / джаз');
    expect(
      server.cover('/api/covers/12').toString(),
      'https://example.org/radio/api/covers/12',
    );
    expect(server.cover('https://outside.example/cover'), isNull);
    expect(server.cover('//outside.example/cover'), isNull);
  });

  test(
    'не принимаются локальные файлы, пароли и параметры вместо адреса сервера',
    () {
      for (final value in [
        'file:///tmp/music',
        'https://a:b@example.org',
        'https://example.org?token=x',
        'example.org',
      ]) {
        expect(
          () => RadioServer(name: 'N', address: Uri.parse(value)),
          throwsFormatException,
        );
      }
    },
  );

  test('контракт читает null текущего трека и отбрасывает будущую очередь', () {
    final state = RadioSnapshot.decode(
      jsonEncode({
        'server_time': '2026-10-15T12:00:00Z',
        'stations': [
          {
            'id': 'Рок',
            'status': 'empty',
            'tags': null,
            'history': null,
            'current': null,
            'queue': [],
          },
        ],
      }),
    );
    expect(state.stations.single.current, isNull);
    expect(state.stations.single.tags, isEmpty);
    expect(state.stations.single.description, 'Пока нет музыки');
  });

  test('повторяющиеся ID волн отвергаются до построения интерфейса', () {
    expect(
      () => RadioSnapshot.decode(
        jsonEncode({
          'server_time': '2026-10-15T12:00:00Z',
          'stations': [
            {'id': 'Рок'},
            {'id': 'Рок'},
          ],
        }),
      ),
      throwsFormatException,
    );
  });
}
