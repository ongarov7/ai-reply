import AVFoundation
import Speech
import SwiftUI
import UIKit

/// The keyboard setup guide.
///
/// One screen, reachable from onboarding AND from Settings ▸ Keyboard setup, so
/// a user who skipped it or forgot the steps never has to reinstall the app to
/// see them again.
///
/// HONESTY IS THE DESIGN CONSTRAINT HERE. iOS gives a containing app no public
/// API that reports whether its own keyboard extension has been added, enabled
/// or granted Full Access. `UITextInputMode.activeInputModes` lists languages,
/// not extension identifiers. So this screen does not guess: the keyboard
/// itself writes a timestamp and a Full Access flag into the App Group the
/// first time it runs, and the checklist reports exactly that, including
/// "we cannot tell yet" as a real state.
struct KeyboardSetupView: View {

    /// Onboarding presents this inside its own step, where the navigation bar
    /// is not showing the screen's name. Pushed from Settings the bar already
    /// carries it, so the lead paragraph stands alone.
    var showsTitle = true

    @State private var status = KeyboardStatus.current()
    @State private var microphoneStatus = AVCaptureDevice.authorizationStatus(for: .audio)
    @State private var speechStatus = SFSpeechRecognizer.authorizationStatus()
    @Environment(\.scenePhase) private var scenePhase

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: DS.Spacing.xl) {
                Text("setup.subtitle")
                    .font(.body)
                    .foregroundStyle(.secondary)

                checklist
                steps
                fullAccess
                paste
                privacy
            }
            .padding(.horizontal, DS.Spacing.l)
            .padding(.vertical, DS.Spacing.l)
            .frame(maxWidth: DS.Layout.readableWidth, alignment: .leading)
            .frame(maxWidth: .infinity)
        }
        .background(Color.dsBackground)
        .navigationTitle(showsTitle ? "setup.title" : "")
        .navigationBarTitleDisplayMode(.inline)
        // Re-read on return from Settings: the user has usually just enabled
        // something, and the keyboard may have reported in since.
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { status = .current() }
        }
        .onAppear { status = .current() }
    }

    // MARK: Checklist

    private var checklist: some View {
        DSSection(title: "setup.checklist.title") {
            VStack(alignment: .leading, spacing: DS.Spacing.s) {
                ChecklistRow(
                    title: "setup.checklist.added",
                    state: status.isConfigured ? .done : .unknown
                )
                ChecklistRow(
                    title: "setup.checklist.fullAccess",
                    state: fullAccessState
                )
                ChecklistRow(
                    title: "setup.checklist.microphone",
                    state: permissionState(microphoneStatus)
                )
                ChecklistRow(
                    title: "setup.checklist.speech",
                    state: permissionState(speechStatus)
                )
                if !status.isConfigured {
                    Text("setup.checklist.unknown.footer")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                if microphoneStatus != .authorized || speechStatus != .authorized {
                    Button("setup.voice.enable") { Task { await requestVoicePermissions() } }
                        .buttonStyle(.dsSecondary)
                }
            }
            .dsCard()
        }
    }

    /// Full Access is only knowable once the keyboard has run at least once.
    /// Before that the honest answer is "not known yet", not "off".
    private var fullAccessState: ChecklistRow.State {
        guard status.isConfigured else { return .unknown }
        return status.hasFullAccess ? .done : .missing
    }

    private func permissionState(_ status: AVAuthorizationStatus) -> ChecklistRow.State {
        switch status {
        case .authorized: return .done
        case .notDetermined: return .unknown
        case .denied, .restricted: return .missing
        @unknown default: return .unknown
        }
    }

    private func permissionState(_ status: SFSpeechRecognizerAuthorizationStatus) -> ChecklistRow.State {
        switch status {
        case .authorized: return .done
        case .notDetermined: return .unknown
        case .denied, .restricted: return .missing
        @unknown default: return .unknown
        }
    }

    @MainActor
    private func requestVoicePermissions() async {
        if microphoneStatus == .notDetermined {
            _ = await AVCaptureDevice.requestAccess(for: .audio)
        }
        if speechStatus == .notDetermined {
            _ = await withCheckedContinuation { continuation in
                SFSpeechRecognizer.requestAuthorization { status in
                    continuation.resume(returning: status)
                }
            }
        }
        microphoneStatus = AVCaptureDevice.authorizationStatus(for: .audio)
        speechStatus = SFSpeechRecognizer.authorizationStatus()
        if microphoneStatus == .denied || speechStatus == .denied {
            openSystemSettings()
        }
    }

    // MARK: Steps

    private var steps: some View {
        DSSection(title: "setup.steps.title") {
            VStack(alignment: .leading, spacing: DS.Spacing.m) {
                KeyboardSetupSteps()
                // iOS lets an app open its OWN settings page and nothing
                // deeper - but for a keyboard app that page lists its
                // keyboards, with the Full Access switch. That is the short
                // path the steps describe; the footer keeps the long one
                // (General ▸ Keyboard) for anyone who does not see it.
                Button(action: openSystemSettings) {
                    Label("home.keyboard.openSettings", systemImage: "keyboard")
                }
                .buttonStyle(.dsPrimary)
                Text("setup.steps.footer")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
    }

    // MARK: Explanations

    private var fullAccess: some View {
        DSSection(title: "setup.fullAccess.title") {
            VStack(alignment: .leading, spacing: DS.Spacing.xs) {
                Text("setup.fullAccess.why").font(.body)
                Text("setup.fullAccess.manual").font(.footnote).foregroundStyle(.secondary)
            }
            .dsCard()
        }
    }

    private var paste: some View {
        DSSection(title: "setup.paste.title") {
            VStack(alignment: .leading, spacing: DS.Spacing.xs) {
                Text("setup.paste.body").font(.body)
                Text("setup.paste.footer").font(.footnote).foregroundStyle(.secondary)
            }
            .dsCard()
        }
    }

    private var privacy: some View {
        DSSection(title: "setup.privacy.title") {
            VStack(alignment: .leading, spacing: DS.Spacing.s) {
                PrivacyPoint("setup.privacy.clipboard")
                PrivacyPoint("setup.privacy.explicit")
                PrivacyPoint("setup.privacy.noKeyInKeyboard")
                PrivacyPoint("setup.privacy.secureFields")
            }
            .dsCard()
        }
    }

    private func openSystemSettings() {
        guard let url = URL(string: UIApplication.openSettingsURLString) else { return }
        UIApplication.shared.open(url)
    }
}

// MARK: - Rows

/// One line of the setup checklist.
struct ChecklistRow: View {

    enum State {
        case done
        case missing
        /// iOS has not told us, and we are not going to pretend it has.
        case unknown
    }

    let title: LocalizedStringKey
    let state: State

    var body: some View {
        HStack(spacing: DS.Spacing.s) {
            Image(systemName: symbol)
                .foregroundStyle(tint)
                .frame(width: 22)
            Text(title).foregroundStyle(.primary)
            Spacer()
            Text(statusKey)
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
        .accessibilityElement(children: .combine)
    }

    private var symbol: String {
        switch state {
        case .done:    return "checkmark.circle.fill"
        case .missing: return "exclamationmark.circle"
        case .unknown: return "questionmark.circle"
        }
    }

    private var tint: Color {
        switch state {
        case .done:    return .green
        case .missing: return .orange
        case .unknown: return .secondary
        }
    }

    private var statusKey: LocalizedStringKey {
        switch state {
        case .done:    return "setup.state.done"
        case .missing: return "setup.state.missing"
        case .unknown: return "setup.state.unknown"
        }
    }
}

private struct PrivacyPoint: View {
    let key: LocalizedStringKey

    init(_ key: LocalizedStringKey) { self.key = key }

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: DS.Spacing.s) {
            Image(systemName: "lock.shield")
                .font(.footnote)
                .foregroundStyle(Color.accentColor)
                .frame(width: 18)
            Text(key).font(.footnote).foregroundStyle(.secondary)
        }
    }
}
