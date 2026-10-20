import SwiftUI

/// The keyboard setup guide.
///
/// One screen, reachable from onboarding AND from Settings ▸ Keyboard setup, so
/// a user who skipped it or forgot the steps never has to reinstall the app to
/// see them again.
///
/// HONESTY IS THE DESIGN CONSTRAINT HERE. iOS gives a containing app no public
/// API that reports whether its own keyboard extension has been added, enabled
/// or granted Full Access. `UITextInputMode.activeInputModes` lists languages,
/// not extension identifiers. So this screen does not guess: it reports what
/// the keyboard itself said when it last ran (`KeyboardStatusMonitor`),
/// including "we cannot tell yet" as a real state.
///
/// The keyboard has no microphone, so there is no microphone or speech
/// permission to set up here: dictation in the app asks for both the first
/// time its microphone button is tapped.
struct KeyboardSetupView: View {

    @Environment(KeyboardStatusMonitor.self) private var keyboard
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
        .navigationTitle("setup.title")
        .navigationBarTitleDisplayMode(.inline)
        // Re-read on return from Settings: the user has usually just enabled
        // something, and the keyboard may have reported in since.
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { keyboard.refresh() }
        }
        .onAppear { keyboard.refresh() }
    }

    // MARK: Checklist

    /// The same two lines as Settings and onboarding: what the keyboard was
    /// seen doing, and when.
    private var checklist: some View {
        DSSection(title: "setup.checklist.title") {
            VStack(alignment: .leading, spacing: DS.Spacing.s) {
                KeyboardStatusRow(kind: .keyboard, sighting: keyboard.keyboardSighting)
                KeyboardStatusRow(kind: .fullAccess, sighting: keyboard.fullAccessSighting)
                if keyboard.keyboardSighting == .unknown {
                    Text("setup.checklist.unknown.footer")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
            .dsCard()
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
                OpenKeyboardSettingsButton()
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
}

// MARK: - Rows

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
