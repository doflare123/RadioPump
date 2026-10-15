import 'dart:io';
import 'dart:ui' show ImageByteFormat;

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:radiopump/platform/desktop_shell.dart';
import 'package:radiopump/ui/radio_app.dart';

import 'support/fakes.dart';

// Предпросмотры — реальные кадры Flutter widget test с тестовым эфиром.
// Шрифт можно взять с машины проверяющего, не добавляя font-пакет в приложение.
Future<void> _preview(WidgetTester tester, GlobalKey key, String name) async {
  if (Platform.environment['RADIOPUMP_PREVIEWS'] != '1') return;
  await tester.runAsync(() async {
    final boundary =
        key.currentContext!.findRenderObject()! as RenderRepaintBoundary;
    final image = await boundary.toImage();
    final bytes = await image.toByteData(format: ImageByteFormat.png);
    final directory = Directory('build/previews')..createSync(recursive: true);
    File(
      '${directory.path}/$name.png',
    ).writeAsBytesSync(bytes!.buffer.asUint8List());
    image.dispose();
  });
}

void main() {
  setUpAll(() async {
    final iconPath = Platform.environment['RADIOPUMP_MATERIAL_ICONS'];
    if (iconPath != null) {
      final iconBytes = await File(iconPath).readAsBytes();
      await (FontLoader(
        'MaterialIcons',
      )..addFont(Future.value(ByteData.sublistView(iconBytes)))).load();
    }
    final fontPath = Platform.environment['RADIOPUMP_PREVIEW_FONT'];
    if (fontPath == null) return;
    final bytes = await File(fontPath).readAsBytes();
    final loader = FontLoader('Roboto')
      ..addFont(Future.value(ByteData.sublistView(bytes)));
    await loader.load();
  });

  testWidgets('полный вид показывает станции и переключает эфир', (
    tester,
  ) async {
    tester.view.devicePixelRatio = 1;
    tester.view.physicalSize = const Size(1000, 640);
    addTearDown(tester.view.reset);
    final controller = exampleController();
    await controller.initialize();
    final key = GlobalKey();
    await tester.pumpWidget(
      RepaintBoundary(
        key: key,
        child: RadioPumpApp(
          controller: controller,
          desktop: DesktopShell(controller),
        ),
      ),
    );
    await tester.pump();
    expect(find.text('Список станций'), findsOneWidget);
    expect(find.text('Ночной город'), findsNWidgets(2));
    await tester.tap(find.byTooltip('Слушать эфир'));
    await tester.pump();
    expect(find.byTooltip('Остановить эфир'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await _preview(tester, key, 'desktop');
    await tester.pumpWidget(const SizedBox());
    await controller.shutdown();
  });

  testWidgets('мобильный вид помещается на 390 px и открывает выбор волны', (
    tester,
  ) async {
    tester.view.devicePixelRatio = 1;
    tester.view.physicalSize = const Size(390, 844);
    addTearDown(tester.view.reset);
    final controller = exampleController();
    await controller.initialize();
    final key = GlobalKey();
    await tester.pumpWidget(
      RepaintBoundary(
        key: key,
        child: RadioPumpApp(controller: controller),
      ),
    );
    await tester.pump();
    expect(find.text('Список станций'), findsNothing);
    expect(tester.takeException(), isNull);
    await _preview(tester, key, 'mobile');
    await tester.tap(find.text('Общая волна'));
    await tester.pumpAndSettle();
    expect(find.text('Список станций'), findsOneWidget);
    await tester.tap(find.text('Спокойная'));
    await tester.pumpAndSettle();
    expect(controller.selectedStation, 'Спокойная');
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
    await controller.shutdown();
  });

  testWidgets('мини-плеер не создаёт обложку и второй аудиоконтроллер', (
    tester,
  ) async {
    tester.view.devicePixelRatio = 1;
    tester.view.physicalSize = const Size(400, 210);
    addTearDown(tester.view.reset);
    final player = FakePlayer();
    final controller = exampleController(player: player);
    await controller.initialize();
    await controller.toggle();
    final desktop = DesktopShell(controller)..compact.value = true;
    final key = GlobalKey();
    await tester.pumpWidget(
      RepaintBoundary(
        key: key,
        child: RadioPumpApp(controller: controller, desktop: desktop),
      ),
    );
    await tester.pump();
    expect(find.text('Развернуть'), findsOneWidget);
    expect(find.byType(Image), findsNothing);
    expect(player.opened.length, 1);
    expect(tester.takeException(), isNull);
    await _preview(tester, key, 'compact');
    await tester.pumpWidget(const SizedBox());
    await controller.shutdown();
  });

  testWidgets('форма отвергает адрес без протокола', (tester) async {
    final controller = exampleController();
    await controller.initialize();
    await tester.pumpWidget(RadioPumpApp(controller: controller));
    await tester.tap(find.byTooltip('Мои серверы'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextFormField).first, 'Друг');
    await tester.enterText(find.byType(TextFormField).last, 'example.org');
    await tester.tap(find.text('Добавить'));
    await tester.pumpAndSettle();
    expect(
      find.text('Нужен полный адрес http:// или https://'),
      findsOneWidget,
    );
    expect(controller.servers.length, 2);
    await tester.pumpWidget(const SizedBox());
    await controller.shutdown();
  });
}
