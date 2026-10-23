import SwiftUI
import UIKit

/// «Настройка клавиатуры и разрешений»: the keyboard, Full Access, paste
/// from other apps, notifications and the last check, each with what is
/// honestly known and, while unresolved, the one thing the user can do.
///
/// Пернетақта мен рұқсаттар: әр жолда белгілі күй және әлі шешілмесе, бір әрекет.
///
/// iOS tells an app nothing about its own keyboard. Every keyboard line is
/// what the keyboard itself last reported (`KeyboardStatusMonitor`) and
/// when: verified with its date, seen off, or not verified yet - never a
/// guess. Notifications come from `UNUserNotificationCenter`, which does
/// know. Settings shows the same section; `aireply://permissions` and
/// `aireply://keyboard` open this screen.
struct PermissionsView: View {

    /// Focuses the keyboard check when the screen opens (Settings ▸
    /// «Проверить клавиатуру»).
    var startsCheck = false

    @Environment(KeyboardStatusMonitor.self) private var keyboard
    @Environment(PushNotificationsModel.self) private var notifications
    @Environment(\.scenePhase) private var scenePhase
    @FocusState private var isChecking: Bool
    @State private var isShowingPasteHelp = false

    var body: some View {
        Form {
            PermissionStatusSection(onPasteHelp: { isShowingPasteHelp = true },
                                    onCheck: { isChecking = true })

            Section {
                KeyboardCheckField(focus: $isChecking)
                    .padding(.vertical, DS.Spacing.xxs)
            } header: {
                Text("keyboardCheck.title")
            }

            Section {
                KeyboardConnectSteps()
                    .padding(.vertical, DS.Spacing.xxs)
                Button("permissions.openSettings", action: OpenKeyboardSettingsButton.openSystemSettings)
            } header: {
                Text("permissions.steps.title")
            } footer: {
                Text("setup.steps.footer")
            }

            Section {
                Text("permissions.fullAccess.why")
                Text("permissions.fullAccess.onlyYou")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            } header: {
                Text("permissions.fullAccess")
            }

            Section("tutorial.videos") {
                NavigationLink { PasteTutorialView() } label: {
                    Label("tutorial.paste.title", systemImage: "play.rectangle")
                }
            }

            Section {
                PrivacyPoint("setup.privacy.clipboard")
                PrivacyPoint("setup.privacy.explicit")
                PrivacyPoint("setup.privacy.noKeyInKeyboard")
                PrivacyPoint("setup.privacy.secureFields")
            } header: {
                Text("setup.privacy.title")
            }
        }
        .navigationTitle("permissions.title")
        .navigationBarTitleDisplayMode(.inline)
        .navigationDestination(isPresented: $isShowingPasteHelp) { PasteHelpView() }
        // Back from iOS Settings, or from an app where the keyboard just ran.
        .onChange(of: scenePhase) { _, phase in
            guard phase == .active else { return }
            keyboard.refresh()
            Task { await notifications.refreshPermission() }
        }
        .onAppear { keyboard.refresh() }
        .task {
            await notifications.refreshPermission()
            guard startsCheck else { return }
            // After the push, so the field exists to take the focus.
            try? await Task.sleep(for: .milliseconds(450))
            isChecking = true
        }
    }
}

/// The five lines, as a `Form` section: Settings and `PermissionsView`.
///
/// The screens it opens are pushed by the `Form` that holds it: a
/// navigation destination inside a list section would be ignored.
struct PermissionStatusSection: View {

    /// «Как разрешить» on the paste line.
    let onPasteHelp: () -> Void
    /// «Проверить клавиатуру»: the check field below, or the screen with it.
    let onCheck: () -> Void

    @Environment(KeyboardStatusMonitor.self) private var keyboard
    @Environment(PushNotificationsModel.self) private var notifications
    @Environment(\.locale) private var locale

    var body: some View {
        Section {
            keyboardRow
            fullAccessRow
            pasteRow
            notificationsRow
            lastCheckRow
        } header: {
            Text("permissions.section")
        } footer: {
            Text("permissions.section.footer")
        }
    }

    private var dateStyle: Date.FormatStyle { KeyboardSightingDate.style.locale(locale) }

    // MARK: Rows

    private var keyboardRow: some View {
        let sighting = keyboard.keyboardSighting
        let state: LocalizedStringKey
        switch sighting {
        case .confirmed(let date): state = "permissions.keyboard.on \(date, format: dateStyle)"
        case .outdated(let date):  state = "permissions.keyboard.outdated \(date, format: dateStyle)"
        case .denied, .unknown:    state = "permissions.keyboard.unknown"
        }
        return PermissionRow(symbol: "keyboard", title: "permissions.keyboard", state: Text(state),
                             evidence: PermissionMark(sighting)) {
            if !sighting.isConfirmed { openSettingsButton }
        }
    }

    private var fullAccessRow: some View {
        let sighting = keyboard.fullAccessSighting
        let state: LocalizedStringKey
        switch sighting {
        case .confirmed(let date): state = "permissions.fullAccess.on \(date, format: dateStyle)"
        case .outdated(let date):  state = "permissions.fullAccess.outdated \(date, format: dateStyle)"
        case .denied(let date):    state = "permissions.fullAccess.off \(date, format: dateStyle)"
        case .unknown:             state = "permissions.fullAccess.unknown"
        }
        return PermissionRow(symbol: "lock.open", title: "permissions.fullAccess", state: Text(state),
                             evidence: PermissionMark(sighting)) {
            if !sighting.isConfirmed { openSettingsButton }
        }
    }

    private var pasteRow: some View {
        let sighting = keyboard.pasteSighting
        let state: LocalizedStringKey
        switch sighting {
        case .confirmed(let date): state = "permissions.paste.on \(date, format: dateStyle)"
        case .outdated(let date):  state = "permissions.paste.outdated \(date, format: dateStyle)"
        case .denied(let date):    state = "permissions.paste.off \(date, format: dateStyle)"
        case .unknown:             state = "permissions.paste.unknown"
        }
        return PermissionRow(symbol: "doc.on.clipboard", title: "permissions.paste", state: Text(state),
                             evidence: PermissionMark(sighting)) {
            if !sighting.isConfirmed {
                Button("permissions.paste.howTo", action: onPasteHelp)
            }
        }
    }

    private var notificationsRow: some View {
        let permission = notifications.permission
        let state: LocalizedStringKey
        let evidence: PermissionMark
        switch permission {
        case .authorized, .provisional, .ephemeral:
            state = "permissions.notifications.allowed"
            evidence = .verified
        case .denied:
            state = "permissions.notifications.denied"
            evidence = .off
        case .notDetermined:
            state = "permissions.notifications.notDetermined"
            evidence = .unverified
        case .unknown:
            state = "permissions.notifications.unknown"
            evidence = .unverified
        }
        return PermissionRow(symbol: "bell", title: "permissions.notifications", state: Text(state),
                             evidence: evidence) {
            switch permission {
            case .notDetermined:
                Button("settings.notifications.allow") {
                    Task { await notifications.requestPermission() }
                }
                .disabled(notifications.isRequestingPermission)
            case .denied:
                Button("permissions.openSettings", action: Self.openNotificationSettings)
            default:
                EmptyView()
            }
        }
    }

    private var lastCheckRow: some View {
        let state = keyboard.status.lastKeyboardCheckAt.map { Text($0, format: dateStyle) }
            ?? Text("permissions.lastCheck.never")
        return PermissionRow(symbol: "clock", title: "permissions.lastCheck", state: state, evidence: .none) {
            Button("permissions.check", action: onCheck)
        }
    }

    private var openSettingsButton: some View {
        Button("permissions.openSettings", action: OpenKeyboardSettingsButton.openSystemSettings)
    }

    /// This app's page in iOS Settings, notifications included.
    private static func openNotificationSettings() {
        guard let url = URL(string: UIApplication.openNotificationSettingsURLString) else { return }
        UIApplication.shared.open(url)
    }
}

/// How the state reads at a glance.
enum PermissionMark {
    /// Seen working recently: a green tick.
    case verified
    /// Seen off: an orange mark. Never red: nothing failed.
    case off
    /// Not seen, or only long ago: grey, no mark of success.
    case unverified
    /// Information only.
    case none

    init(_ sighting: KeyboardSighting) {
        switch sighting {
        case .confirmed:           self = .verified
        case .denied:              self = .off
        case .outdated, .unknown:  self = .unverified
        }
    }
}

/// One line: what it is, what is known about it, and - while it is not done -
/// the action, as a button of its own inside the row.
struct PermissionRow<Action: View>: View {

    let symbol: String
    let title: LocalizedStringKey
    let state: Text
    let evidence: PermissionMark
    @ViewBuilder var action: Action

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: DS.Spacing.s) {
            Image(systemName: symbol)
                .foregroundStyle(Color.accentColor)
                .frame(width: 24)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: DS.Spacing.xxs) {
                HStack(alignment: .firstTextBaseline) {
                    Text(title).foregroundStyle(.primary)
                    Spacer(minLength: DS.Spacing.xs)
                    mark
                }
                state
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                action
                    .font(.subheadline.weight(.medium))
                    // Only the button is the target, not the whole row.
                    .buttonStyle(.borderless)
                    .padding(.top, DS.Spacing.xxs)
            }
        }
        .padding(.vertical, DS.Spacing.xxs)
    }

    @ViewBuilder
    private var mark: some View {
        switch evidence {
        case .verified:
            Image(systemName: "checkmark.circle.fill").foregroundStyle(.green).accessibilityHidden(true)
        case .off:
            Image(systemName: "exclamationmark.circle.fill").foregroundStyle(.orange).accessibilityHidden(true)
        case .unverified, .none:
            EmptyView()
        }
    }
}

/// The four taps from AI Reply's own page in iOS Settings, the one page an
/// app may open. On iOS 17 and 18 a keyboard app's page lists «Клавиатуры»,
/// with the keyboard's switch and Full Access inside.
struct KeyboardConnectSteps: View {
    /// Ticks the steps the keyboard has been seen to have done.
    var showsProgress = false

    @Environment(KeyboardStatusMonitor.self) private var keyboard

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.m) {
            step(1, "setup.connect.open", detail: "setup.connect.open.detail", done: false)
            step(2, "setup.connect.keyboards", done: false)
            step(3, "setup.connect.enable", done: showsProgress && keyboard.keyboardSighting.isConfirmed)
            step(4, "setup.connect.fullAccess", done: showsProgress && keyboard.fullAccessSighting.isConfirmed)
        }
    }

    private func step(_ index: Int, _ text: LocalizedStringKey, detail: LocalizedStringKey? = nil,
                      done: Bool) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: DS.Spacing.xs) {
            DSStepRow(index: index, text: text, detail: detail)
            if done {
                Image(systemName: "checkmark.circle.fill")
                    .foregroundStyle(.green)
                    .accessibilityLabel("permissions.done")
            }
        }
    }
}

struct PrivacyPoint: View {
    let key: LocalizedStringKey

    init(_ key: LocalizedStringKey) { self.key = key }

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: DS.Spacing.s) {
            Image(systemName: "lock.shield")
                .font(.footnote)
                .foregroundStyle(Color.accentColor)
                .frame(width: 18)
                .accessibilityHidden(true)
            Text(key).font(.footnote).foregroundStyle(.secondary)
        }
    }
}
