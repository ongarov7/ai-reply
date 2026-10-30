import AppKit
import AirReplyCore
import AirReplyUI

/// A menu bar utility: AppKit lifecycle, no main window and no Dock icon
/// (`LSUIElement`). Everything lives in `AppCoordinator`.
@main
@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    private let coordinator = AppCoordinator(configuration: .fromMainBundle())

    static func main() {
        let app = NSApplication.shared
        let delegate = AppDelegate()
        app.delegate = delegate
        app.setActivationPolicy(.accessory)
        app.run()
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        coordinator.start()
    }

    /// Opening the app again from Finder, Spotlight or Launchpad shows the bar.
    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        coordinator.showPanel()
        return false
    }

    func applicationWillTerminate(_ notification: Notification) {
        coordinator.stop()
    }
}
