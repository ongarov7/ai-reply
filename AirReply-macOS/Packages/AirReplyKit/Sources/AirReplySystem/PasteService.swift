import AppKit
import Carbon.HIToolbox
import ServiceManagement

/// Remembers which app was in front when the quick panel opened, so focus and an
/// optional automatic paste go back to it.
@MainActor
public final class FrontmostAppTracker {
    public private(set) var previousApp: NSRunningApplication?
    private let frontmostApplication: @MainActor () -> NSRunningApplication?
    private let ownProcessID: pid_t
    private let ownBundleIdentifier: String?

    public init() {
        frontmostApplication = { NSWorkspace.shared.frontmostApplication }
        ownProcessID = ProcessInfo.processInfo.processIdentifier
        ownBundleIdentifier = Bundle.main.bundleIdentifier
    }

    init(frontmostApplication: @escaping @MainActor () -> NSRunningApplication?, ownProcessID: pid_t, ownBundleIdentifier: String?) {
        self.frontmostApplication = frontmostApplication
        self.ownProcessID = ownProcessID
        self.ownBundleIdentifier = ownBundleIdentifier
    }

    /// Called right before the panel shows. Ignores this app itself.
    public func rememberFrontmostApp() {
        // With our own window in front (settings) there is nothing to go back to, and a paste
        // must not land in an app the user left earlier.
        previousApp = frontmostApplication().flatMap { isThisApp($0) ? nil : $0 }
    }

    private func isThisApp(_ app: NSRunningApplication) -> Bool {
        app.processIdentifier == ownProcessID || (ownBundleIdentifier != nil && app.bundleIdentifier == ownBundleIdentifier)
    }
}

/// Optional "paste for me": posts ⌘V to the previous app. Off by default; needs
/// the user's permission to post events (System Settings → Privacy & Security →
/// Accessibility). Copy always works without it.
@MainActor
public final class PasteService {
    public enum Availability: Equatable, Sendable {
        case available
        case needsPermission
        /// A password field (or another app) holds Secure Event Input: keystrokes would be dropped.
        case secureInputActive
    }

    /// 12 × 50 ms: how long the previous app gets to come back to the front.
    static let activationChecks = 12
    private let system: any PasteSystem

    public init() {
        system = LivePasteSystem()
    }

    init(system: any PasteSystem) {
        self.system = system
    }

    public func availability() -> Availability {
        if system.isSecureInputEnabled { return .secureInputActive }
        return system.canPostEvents ? .available : .needsPermission
    }

    /// Shows the system prompt for event posting (once; then the user goes to Settings).
    public func requestPermission() {
        system.requestPostEventAccess()
    }

    /// Opens Privacy & Security → Accessibility.
    public func openPrivacySettings() {
        guard let url = URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility") else { return }
        system.open(url)
    }

    /// Activates `app` and posts ⌘V. Returns false when it did not try.
    /// Close the panel first: a key panel would receive the keystroke itself.
    public func paste(into app: NSRunningApplication?) async -> Bool {
        guard let app, !app.isTerminated, availability() == .available else { return false }
        system.activate(app)
        if !system.isFrontmost(app) {
            var checks = 0
            repeat {
                // Posting anyway could paste the reply into whatever app is in front instead.
                guard checks < Self.activationChecks else { return false }
                await system.pause()
                checks += 1
            } while !system.isFrontmost(app)
            // Its key window takes focus just after the app becomes active.
            await system.pause()
        }
        guard !Task.isCancelled else { return false }
        return system.postCommandV()
    }
}

/// The system calls behind `PasteService`, faked in tests so no keystroke is ever posted.
@MainActor
protocol PasteSystem: AnyObject {
    var isSecureInputEnabled: Bool { get }
    var canPostEvents: Bool { get }
    func requestPostEventAccess()
    func open(_ url: URL)
    func activate(_ app: NSRunningApplication)
    func isFrontmost(_ app: NSRunningApplication) -> Bool
    /// One step of waiting for activation (50 ms).
    func pause() async
    /// Posts ⌘V key down and up; false when the events could not be created.
    func postCommandV() -> Bool
}

@MainActor
final class LivePasteSystem: PasteSystem {
    var isSecureInputEnabled: Bool { IsSecureEventInputEnabled() }
    var canPostEvents: Bool { CGPreflightPostEventAccess() }

    func requestPostEventAccess() {
        _ = CGRequestPostEventAccess()
    }

    func open(_ url: URL) {
        NSWorkspace.shared.open(url)
    }

    func activate(_ app: NSRunningApplication) {
        // Cooperative activation (macOS 14): while our settings window is active we hand over first.
        NSApp?.yieldActivation(to: app)
        app.activate()
    }

    func isFrontmost(_ app: NSRunningApplication) -> Bool {
        NSWorkspace.shared.frontmostApplication?.processIdentifier == app.processIdentifier
    }

    func pause() async {
        try? await Task.sleep(for: .milliseconds(50))
    }

    func postCommandV() -> Bool {
        let source = CGEventSource(stateID: .combinedSessionState)
        let fallback = CGKeyCode(kVK_ANSI_V)
        let key = KeyboardLayout.currentASCIICapable()?.keyCode(typingWithCommand: "v", preferring: fallback) ?? fallback
        guard let down = CGEvent(keyboardEventSource: source, virtualKey: key, keyDown: true),
              let up = CGEvent(keyboardEventSource: source, virtualKey: key, keyDown: false) else { return false }
        down.flags = .maskCommand
        up.flags = .maskCommand
        down.post(tap: .cghidEventTap)
        up.post(tap: .cghidEventTap)
        return true
    }
}

/// Start at login through `SMAppService.mainApp` (macOS 13+). Off until the user turns it on.
@MainActor
public enum LoginItem {
    public enum State: Equatable, Sendable {
        case enabled
        case disabled
        /// Registered, but the user must allow it in System Settings → Login Items.
        case requiresApproval
    }

    public static var state: State {
        switch SMAppService.mainApp.status {
        case .enabled: .enabled
        case .requiresApproval: .requiresApproval
        default: .disabled
        }
    }

    /// Registering twice or removing what is not there are errors in ServiceManagement,
    /// so the current status decides whether there is anything to do.
    public static func setEnabled(_ enabled: Bool) throws {
        let service = SMAppService.mainApp
        switch (enabled, service.status) {
        case (true, .enabled), (true, .requiresApproval), (false, .notRegistered), (false, .notFound):
            return
        case (true, _):
            try service.register()
        case (false, _):
            try service.unregister()
        }
    }

    public static func openLoginItemsSettings() {
        SMAppService.openSystemSettingsLoginItems()
    }
}
