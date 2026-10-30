import AppKit
import SwiftUI
import AirReplyCore
import AirReplySystem
import AirReplyVoice

/// Settings in a standard macOS preferences window with toolbar tabs.
@MainActor
final class SettingsWindowController: NSWindowController {
    init(coordinator: AppCoordinator) {
        let tabs = NSTabViewController()
        tabs.tabStyle = .toolbar
        tabs.canPropagateSelectedChildViewControllerTitle = true
        func pane<V: View>(_ view: V, _ title: String, _ symbol: String) -> NSTabViewItem {
            let controller = NSHostingController(rootView: view.frame(width: 520).fixedSize())
            controller.sizingOptions = .preferredContentSize
            let item = NSTabViewItem(viewController: controller)
            item.label = title
            item.image = NSImage(systemSymbolName: symbol, accessibilityDescription: title)
            return item
        }
        tabs.addTabViewItem(pane(GeneralPane(coordinator: coordinator), String(localized: "settings.general"), "gearshape"))
        tabs.addTabViewItem(pane(VoicePane(coordinator: coordinator), String(localized: "settings.voice"), "mic"))
        tabs.addTabViewItem(pane(AccountPane(coordinator: coordinator), String(localized: "settings.account"), "person.crop.circle"))
        tabs.addTabViewItem(pane(AboutPane(coordinator: coordinator), String(localized: "settings.about"), "info.circle"))
        let window = NSWindow(contentViewController: tabs)
        window.styleMask = [.titled, .closable]
        window.isReleasedWhenClosed = false
        window.setFrameAutosaveName("AIReplySettings")
        super.init(window: window)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) { fatalError("init(coder:) is not supported") }

    func present() {
        // An agent app must activate itself to bring a regular window forward.
        NSApp.activate()
        window?.center()
        showWindow(nil)
        window?.makeKeyAndOrderFront(nil)
    }
}

/// The root of the settings, also usable from SwiftUI previews.
public struct SettingsRootView: View {
    let coordinator: AppCoordinator

    public init(coordinator: AppCoordinator) {
        self.coordinator = coordinator
    }

    public var body: some View {
        TabView {
            GeneralPane(coordinator: coordinator).tabItem { Label("settings.general", systemImage: "gearshape") }
            VoicePane(coordinator: coordinator).tabItem { Label("settings.voice", systemImage: "mic") }
            AccountPane(coordinator: coordinator).tabItem { Label("settings.account", systemImage: "person.crop.circle") }
            AboutPane(coordinator: coordinator).tabItem { Label("settings.about", systemImage: "info.circle") }
        }
        .frame(width: 520)
    }
}

private struct GeneralPane: View {
    let coordinator: AppCoordinator
    @State private var loginItem = LoginItem.state

    var body: some View {
        @Bindable var settings = coordinator.settings
        Form {
            Section {
                LabeledContent("settings.shortcut") {
                    ShortcutRecorder(combo: settings.shortcut) { coordinator.changeShortcut(to: $0) }
                }
                if let note = coordinator.shortcutNote {
                    Text(note).font(.caption).foregroundStyle(.orange)
                }
                Toggle("settings.login_item", isOn: Binding(
                    get: { loginItem != .disabled },
                    set: { enabled in
                        try? LoginItem.setEnabled(enabled)
                        loginItem = LoginItem.state
                    }))
                if loginItem == .requiresApproval {
                    HStack {
                        Text("settings.login_item.approval").font(.caption).foregroundStyle(.secondary)
                        Button("settings.open_system_settings") { LoginItem.openLoginItemsSettings() }.buttonStyle(.link)
                    }
                }
            }
            Section {
                Picker("settings.placement", selection: $settings.placement) {
                    Text("settings.placement.top").tag(PanelPlacement.top)
                    Text("settings.placement.bottom").tag(PanelPlacement.bottom)
                    Text("settings.placement.remembered").tag(PanelPlacement.remembered)
                }
                Picker("settings.appearance", selection: $settings.appearance) {
                    Text("settings.appearance.system").tag(AppearancePreference.system)
                    Text("settings.appearance.light").tag(AppearancePreference.light)
                    Text("settings.appearance.dark").tag(AppearancePreference.dark)
                }
                .onChange(of: settings.appearance) { coordinator.applyAppearance() }
            }
            Section {
                Toggle("settings.offer_copied", isOn: $settings.offerCopiedText)
                Toggle("settings.close_after_copy", isOn: $settings.closeAfterCopy)
                Toggle("settings.paste_after_copy", isOn: Binding(
                    get: { settings.pasteAfterCopy },
                    set: { enabled in
                        settings.pasteAfterCopy = enabled
                        if enabled { coordinator.requestPastePermission() }
                    }))
                Text("settings.paste_after_copy.footer").font(.caption).foregroundStyle(.secondary)
            }
        }
        .formStyle(.grouped)
        .onAppear { loginItem = LoginItem.state }
    }
}

private struct VoicePane: View {
    let coordinator: AppCoordinator

    var body: some View {
        @Bindable var settings = coordinator.settings
        Form {
            Section {
                Picker("settings.dictation_language", selection: $settings.dictationLanguage) {
                    Text("language.auto").tag(DictationLanguage.auto)
                    Text(verbatim: "Русский").tag(DictationLanguage.ru)
                    Text(verbatim: "Қазақша").tag(DictationLanguage.kk)
                    Text(verbatim: "English").tag(DictationLanguage.en)
                }
                Text("settings.voice.footer").font(.caption).foregroundStyle(.secondary)
            }
            Section {
                LabeledContent("settings.microphone") {
                    Button("settings.open_system_settings") { coordinator.openMicrophoneSettings() }
                }
            }
        }
        .formStyle(.grouped)
    }
}

private struct AccountPane: View {
    let coordinator: AppCoordinator

    var body: some View {
        Form {
            if coordinator.auth.isSignedIn {
                Section {
                    LabeledContent("settings.account.email", value: coordinator.signedInEmail ?? "—")
                    if let balance = coordinator.account.balance {
                        LabeledContent("settings.account.plan", value: balance.planName)
                        LabeledContent("settings.account.balance") { BalanceCaption(balance: balance) }
                    }
                }
                Section {
                    Button("settings.account.plans") { coordinator.openPlans() }
                    Button("settings.account.sign_out", role: .destructive) { coordinator.signOut() }
                    Button("settings.account.delete") { coordinator.openAccountDeletion() }
                }
            } else {
                Section {
                    Text("settings.account.signed_out").foregroundStyle(.secondary)
                    Button("menu.sign_in") { coordinator.showPanel() }
                }
            }
        }
        .formStyle(.grouped)
        .task { await coordinator.refreshAccount() }
    }
}

private struct AboutPane: View {
    let coordinator: AppCoordinator
    @State private var copied = false

    var body: some View {
        Form {
            Section {
                HStack(spacing: 14) {
                    Image(nsImage: NSApp.applicationIconImage).resizable().frame(width: 56, height: 56)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: "AI Reply").font(.title3.weight(.semibold))
                        Text("settings.about.version \(coordinator.configuration.appVersion) \(coordinator.configuration.build)")
                            .foregroundStyle(.secondary)
                    }
                }
            }
            Section {
                if let legal = coordinator.account.config.legal {
                    if let url = legal.privacyURL { Link("consent.privacy", destination: url) }
                    if let url = legal.termsURL { Link("consent.terms", destination: url) }
                    if let url = legal.supportURL { Link("settings.about.support", destination: url) }
                }
                Button(copied ? "settings.about.copied" : "settings.about.copy_diagnostics") {
                    coordinator.copyDiagnostics()
                    copied = true
                }
            }
        }
        .formStyle(.grouped)
        .task { await coordinator.account.refreshConfig() }
    }
}

/// Click, then press the new combination. Esc cancels.
struct ShortcutRecorder: NSViewRepresentable {
    let combo: KeyCombo
    let onChange: (KeyCombo) -> Void

    func makeNSView(context: Context) -> RecorderButton {
        let button = RecorderButton()
        button.onRecord = onChange
        button.combo = combo
        return button
    }

    func updateNSView(_ button: RecorderButton, context: Context) {
        button.onRecord = onChange
        button.combo = combo
    }

    final class RecorderButton: NSButton {
        var onRecord: ((KeyCombo) -> Void)?
        var combo: KeyCombo? { didSet { refresh() } }
        private var recording = false { didSet { refresh() } }

        init() {
            super.init(frame: .zero)
            bezelStyle = .push
            target = self
            action = #selector(toggleRecording)
            refresh()
        }

        @available(*, unavailable)
        required init?(coder: NSCoder) { fatalError("init(coder:) is not supported") }

        override var acceptsFirstResponder: Bool { true }

        @objc private func toggleRecording() {
            recording.toggle()
            if recording { window?.makeFirstResponder(self) }
        }

        override func keyDown(with event: NSEvent) {
            guard recording else { return super.keyDown(with: event) }
            if event.keyCode == 53 { // Esc
                recording = false
                return
            }
            guard let candidate = KeyCombo(event: event), candidate.isAllowedAsGlobalShortcut else {
                NSSound.beep()
                return
            }
            recording = false
            onRecord?(candidate)
        }

        override func resignFirstResponder() -> Bool {
            recording = false
            return super.resignFirstResponder()
        }

        private func refresh() {
            title = recording ? String(localized: "settings.shortcut.recording") : (combo?.displayString ?? "—")
        }
    }
}
