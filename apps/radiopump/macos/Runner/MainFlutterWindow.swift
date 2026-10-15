// Платформенная оболочка Flutter. Прикладной плеер и состояние находятся в lib/.
import Cocoa
import FlutterMacOS

class MainFlutterWindow: NSWindow {
  override func awakeFromNib() {
    let flutterViewController = FlutterViewController()
    let windowFrame = self.frame
    self.contentViewController = flutterViewController
    self.setFrame(windowFrame, display: true)

    RegisterGeneratedPlugins(registry: flutterViewController)

    // Короткоживущие дочерние панели не нужны; плагин меняет единственное окно.
    self.isReleasedWhenClosed = false

    super.awakeFromNib()
  }
}
