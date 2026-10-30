import AppKit
import Carbon.HIToolbox
import SwiftUI
import AirReplyCore
import AirReplySystem
import AirReplyVoice

/// Builds the app's objects once and connects them: menu bar item, global
/// shortcut, quick panel, account and settings.
@MainActor
public final class AppCoordinator {
    public let configuration: AppConfiguration
    let settings: AppSettings
    let auth: AuthSession
    let account: AccountStore
    let model: QuickBarModel
    private let client: APIClient
    private let voice: VoiceController
    private let clipboard: ClipboardMonitor
    private let hotKeys = HotKeyCenter()
    private let frontmost = FrontmostAppTracker()
    private let paste = PasteService()
    private let panel: QuickPanelController
    private var statusItem: StatusItemController?
    private var settingsWindow: SettingsWindowController?
    private var observers: [NSObjectProtocol] = []
    /// Shown in Settings when the chosen shortcut could not be registered.
    private(set) var shortcutNote: String?

    public init(configuration: AppConfiguration) {
        self.configuration = configuration
        let settings = AppSettings()
        self.settings = settings
        let sessions = SessionStore(store: KeychainStore(service: configuration.bundleIdentifier))
        client = APIClient(configuration: configuration, transport: URLSessionTransport(), sessions: sessions)
        let restored = sessions.load()
        auth = AuthSession(client: client, restoredEmail: restored?.email, isSignedIn: restored != nil)
        account = AccountStore(client: client, configuration: configuration)
        clipboard = ClipboardMonitor()
        let transcriber = ServerTranscriber(client: client) {
            ServerTranscriber.hints(setting: settings.dictationLanguage, keyboard: KeyboardLanguage.current())
        }
        voice = VoiceController(transcriber: transcriber)
        model = QuickBarModel(auth: auth, account: account, voice: voice,
                              generator: ServerGenerator(client: client, configuration: configuration),
                              clipboard: clipboard, settings: settings)
        panel = QuickPanelController(settings: settings)
    }

    public func start() {
        applyAppearance()
        wirePanel()
        wireAccount()
        statusItem = StatusItemController(snapshot: { [unowned self] in statusSnapshot() }, actions: .init(
            open: { [unowned self] in showPanel() },
            dictate: { [unowned self] in startDictation() },
            signIn: { [unowned self] in showPanel() },
            plans: { [unowned self] in openPlans() },
            settings: { [unowned self] in openSettings() },
            checkForUpdates: { [unowned self] in openURL(configuration.websiteURL("app")) },
            quit: { NSApp.terminate(nil) }))
        hotKeys.onPress = { [unowned self] in togglePanel() }
        registerShortcut(settings.shortcut)
        clipboard.start()
        observers.append(NSWorkspace.shared.notificationCenter.addObserver(
            forName: NSWorkspace.didWakeNotification, object: nil, queue: .main) { [weak self] _ in
                Task { @MainActor in await self?.refreshAccount() }
            })
        Task {
            await account.refreshConfig()
            if auth.isSignedIn { try? await account.refreshMe() }
        }
    }

    public func stop() {
        hotKeys.unregister()
        clipboard.stop()
        voice.cancel()
        observers.forEach { NSWorkspace.shared.notificationCenter.removeObserver($0) }
        observers.removeAll()
    }

    // MARK: Panel

    public func showPanel() {
        guard !panel.isVisible else { return }
        frontmost.rememberFrontmostApp()
        let copied = settings.offerCopiedText ? clipboard.takeFreshCopiedText(maxAge: QuickBarModel.keepAlive) : .none
        model.panelDidOpen(copied: copied)
        panel.show()
    }

    public func togglePanel() {
        panel.isVisible ? panel.hide() : showPanel()
    }

    public func startDictation() {
        showPanel()
        if voice.phase == .idle { model.toggleVoice() }
    }

    private func wirePanel() {
        panel.setContent(QuickBarView(
            model: model,
            pricingURL: configuration.websiteURL("payment"),
            legal: { [unowned self] in account.config.legal },
            onHeightChange: { [unowned self] in panel.contentHeightChanged($0) }))
        panel.holdsOpen = { [unowned self] in model.holdsPanelOpen }
        panel.willShow = { [unowned self] in model.growsUpward = $0 }
        panel.didHide = { [unowned self] in model.panelDidClose() }
        panel.onEscape = { [unowned self] in model.escape() }
        model.requestClose = { [unowned self] in panel.hide() }
        model.didCopy = { [unowned self] in afterCopy() }
        model.openURL = { [unowned self] in openURL($0) }
        model.openPrivacySettings = { [unowned self] in openMicrophoneSettings() }
    }

    /// After "Copy": close so ⌘V goes to the messenger, or paste there for the user.
    private func afterCopy() {
        guard settings.closeAfterCopy || settings.pasteAfterCopy else { return }
        panel.hide()
        guard settings.pasteAfterCopy else { return }
        if paste.availability() == .available {
            let target = frontmost.previousApp
            Task { _ = await paste.paste(into: target) }
        }
    }

    // MARK: Account

    private func wireAccount() {
        auth.onSignedIn = { [unowned self] payload in
            account.apply(payload.me)
            Task { await account.refreshConfig() }
        }
        auth.onSignedOut = { [unowned self] in account.clear() }
        Task { [client] in
            await client.setEventHandler { [weak self] event in
                Task { @MainActor in self?.handle(event) }
            }
        }
    }

    private func handle(_ event: SessionEvent) {
        switch event {
        case let .refreshed(payload): account.apply(payload.me)
        case .signedOut: auth.sessionEnded()
        }
    }

    var signedInEmail: String? {
        if case let .signedIn(email) = auth.state, let email { return email }
        return account.me?.user.email
    }

    func refreshAccount() async {
        guard auth.isSignedIn else { return }
        try? await account.refreshMe(maxAge: 10)
    }

    func signOut() {
        Task { await auth.signOut() }
    }

    func openPlans() {
        openURL(configuration.websiteURL("payment"))
    }

    func openAccountDeletion() {
        openURL(account.config.legal?.accountDeletionURL ?? configuration.websiteURL("account/delete"))
    }

    // MARK: Settings and system

    func openSettings() {
        panel.hide()
        if settingsWindow == nil { settingsWindow = SettingsWindowController(coordinator: self) }
        settingsWindow?.present()
    }

    func changeShortcut(to combo: KeyCombo) {
        let previous = settings.shortcut
        // A combination macOS itself uses (for example switching the keyboard layout) would never reach us.
        if combo.isUsedBySystem {
            shortcutNote = String(localized: "settings.shortcut.system \(combo.displayString)")
            return
        }
        switch hotKeys.register(combo) {
        case .success:
            settings.shortcut = combo
            shortcutNote = nil
        case .failure:
            shortcutNote = String(localized: "settings.shortcut.taken \(combo.displayString)")
            _ = hotKeys.register(previous)
        }
    }

    /// ⌃⌥⌘Space: the last resort when ⌥Space and ⌃⌥Space are both unavailable.
    private static let controlOptionCommandSpace = KeyCombo(keyCode: UInt32(kVK_Space),
                                                            modifiers: UInt32(controlKey | optionKey | cmdKey))

    /// The saved shortcut; for the default ⌥Space, the first free fallback. ⌃⌥Space is
    /// skipped where macOS uses it to switch input sources (common with ru/kk layouts).
    private func registerShortcut(_ combo: KeyCombo) {
        let candidates = combo == .optionSpace ? [combo, .controlOptionSpace, Self.controlOptionCommandSpace] : [combo]
        for candidate in candidates where !candidate.isUsedBySystem {
            guard case .success = hotKeys.register(candidate) else { continue }
            if candidate != combo {
                settings.shortcut = candidate
                shortcutNote = String(localized: "settings.shortcut.fallback \(combo.displayString) \(candidate.displayString)")
            } else {
                shortcutNote = nil
            }
            return
        }
        shortcutNote = String(localized: "settings.shortcut.taken \(combo.displayString)")
    }

    func applyAppearance() {
        NSApp.appearance = switch settings.appearance {
        case .system: nil
        case .light: NSAppearance(named: .aqua)
        case .dark: NSAppearance(named: .darkAqua)
        }
    }

    func requestPastePermission() {
        if paste.availability() == .needsPermission { paste.requestPermission() }
    }

    func openMicrophoneSettings() {
        openURL(URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_Microphone")!)
    }

    /// Versions and state for support. Never the user's text.
    func copyDiagnostics() {
        let lines = [
            "AI Reply \(configuration.appVersion) (\(configuration.build))",
            "macOS \(configuration.osVersion), \(DeviceDescriptor.hardwareModel())",
            "Server: \(configuration.backendBaseURL.host ?? "-")\(configuration.backendBaseURL.path)",
            "Signed in: \(auth.isSignedIn ? "yes" : "no"), consent: \(account.hasConsent.map { $0 ? "yes" : "no" } ?? "-")",
            "Voice: \(account.config.voiceTranscription ? "on" : "off"), shortcut: \(settings.shortcut.displayString)",
            "Last problem: \(model.issue.map { String(describing: $0) } ?? "none")",
        ]
        clipboard.copy(lines.joined(separator: "\n"))
    }

    private func openURL(_ url: URL) {
        NSWorkspace.shared.open(url)
    }

    private func statusSnapshot() -> StatusItemController.Snapshot {
        .init(shortcut: settings.shortcut.displayString, email: signedInEmail, signedIn: auth.isSignedIn,
              balance: account.balance, voiceAvailable: account.config.voiceTranscription)
    }
}
