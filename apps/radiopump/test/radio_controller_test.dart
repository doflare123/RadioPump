import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:radiopump/domain/radio_models.dart';
import 'package:radiopump/playback/radio_audio.dart';

import 'support/fakes.dart';

void main() {
  test(
    'системная пауза отменяет отложенный запуск при смене владельца',
    () async {
      final player = FakePlayer();
      final api = FakeApi();
      final controller = exampleController(api: api, player: player);
      await controller.initialize();
      await controller.toggle();
      final pending = Completer<RadioSnapshot>();
      api.handler = (_) => pending.future;
      final changing = controller.selectServer(controller.servers.last);
      await Future<void>.delayed(Duration.zero);
      expect(controller.wantsPlayback, isTrue);
      await player.pause!();
      pending.complete(exampleSnapshot());
      await changing;
      expect(player.opened.length, 1);
      expect(controller.wantsPlayback, isFalse);
      await controller.shutdown();
    },
  );

  test(
    'системный play после удаления профилей не включает старый URL',
    () async {
      final player = FakePlayer();
      final controller = exampleController(player: player);
      await controller.initialize();
      await controller.toggle();
      await controller.removeServer(controller.servers.last);
      await controller.removeServer(controller.servers.single);
      await player.resume!();
      expect(player.opened.length, 1);
      expect(player.view.value.active, isFalse);
      await controller.shutdown();
    },
  );

  test('поздний ответ прежнего сервера не меняет выбранное радио', () async {
    final pending = Completer<RadioSnapshot>();
    final api = FakeApi()
      ..handler = (server) => server.name == 'Радио Александра'
          ? pending.future
          : Future.value(exampleSnapshot(firstStation: 'Волна друга'));
    final player = FakePlayer();
    final controller = exampleController(api: api, player: player);
    final initial = controller.initialize();
    await Future<void>.delayed(Duration.zero);
    await controller.selectServer(controller.servers.last);
    pending.complete(exampleSnapshot(firstStation: 'Старый ответ'));
    await initial;
    expect(controller.station?.id, 'Волна друга');
    expect(controller.server?.name, 'Радио друга');
    expect(player.opened, isEmpty);
    await controller.shutdown();
  });

  test(
    'переключение станции и системные стрелки используют один плеер',
    () async {
      final player = FakePlayer();
      final controller = exampleController(player: player);
      await controller.initialize();
      expect(player.opened, isEmpty);
      await controller.toggle();
      final first = player.source;
      await controller.stepStation(1);
      expect(player.opened.length, 2);
      expect(player.source, isNot(first));
      await controller.toggle();
      expect(player.view.value.phase, PlayerPhase.stopped);
      await controller.toggle();
      expect(player.source?.pathSegments.last, 'Спокойная');
      await controller.shutdown();
    },
  );

  test('смена владельца во время эфира включает только его волну', () async {
    final player = FakePlayer();
    final controller = exampleController(player: player);
    await controller.initialize();
    await controller.toggle();
    await controller.selectServer(controller.servers.last);
    await Future<void>.delayed(Duration.zero);
    expect(player.source?.host, 'friend.example.org');
    expect(player.opened.length, 2);
    await controller.shutdown();
  });

  test(
    'удаление последнего профиля освобождает эфир и текущие метаданные',
    () async {
      final player = FakePlayer();
      final controller = exampleController(player: player);
      await controller.initialize();
      await controller.toggle();
      await controller.removeServer(controller.servers.last);
      await controller.removeServer(controller.servers.single);
      expect(controller.server, isNull);
      expect(player.item, isNull);
      expect(player.view.value.active, isFalse);
      await controller.shutdown();
    },
  );

  testWidgets('скрытый остановленный клиент прекращает polling и UI-таймер', (
    tester,
  ) async {
    final api = FakeApi();
    final controller = exampleController(api: api);
    await controller.initialize();
    controller.setVisible(false);
    final count = api.requests;
    final tick = controller.clockTick.value;
    await tester.pump(const Duration(minutes: 2));
    expect(api.requests, count);
    expect(controller.clockTick.value, tick);
    controller.setVisible(true);
    await tester.pump();
    expect(api.requests, count + 1);
    await controller.shutdown();
  });

  testWidgets('незавершённый запрос не размножается по таймеру', (
    tester,
  ) async {
    final api = FakeApi();
    final controller = exampleController(api: api);
    await controller.initialize();
    final pending = Completer<RadioSnapshot>();
    api.handler = (_) => pending.future;
    final fetch = controller.refresh();
    final count = api.requests;
    await tester.pump(const Duration(minutes: 2));
    expect(api.requests, count);
    pending.complete(exampleSnapshot());
    await fetch;
    await controller.shutdown();
  });
}
