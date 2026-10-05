import SwiftUI

/// Turning the keyboard on. Only the user can, in iOS Settings; this step says
/// where, shows what the app can honestly tell about it, and offers a field
/// to open the keyboard in, which is what makes the status turn green.
struct OnboardingKeyboardStep: View {

    @Environment(KeyboardStatusMonitor.self) private var keyboard

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.l) {
            OnboardingStepTitle("onboarding.keyboard.title", "onboarding.keyboard.prompt")

            OnboardingStatusCard(kind: .keyboard, footer: "onboarding.keyboard.statusFooter")

            DSSection(title: "setup.steps.title") {
                VStack(alignment: .leading, spacing: DS.Spacing.m) {
                    KeyboardSetupSteps()
                    OpenKeyboardSettingsButton()
                    DisclosureGroup {
                        Text("setup.steps.footer")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(.top, DS.Spacing.xs)
                    } label: {
                        Text("onboarding.keyboard.otherWay").font(.subheadline)
                    }
                    .dsCard()
                }
            }

            KeyboardTestField()
        }
        // While this step is on screen the status follows the user's trip to
        // iOS Settings and back without them having to tap anything.
        .task { await keyboard.followLiveUpdates() }
        .onAppear { ProductEvents.track(.onboardingKeyboardStepViewed) }
    }
}

/// Full Access: what it is for, what it is not, and the iOS warning the user
/// is about to see - in that order, without scaring anyone.
struct OnboardingFullAccessStep: View {

    @Environment(KeyboardStatusMonitor.self) private var keyboard
    /// Set once, while Full Access is not confirmed yet, and kept: the field
    /// must not vanish under the keyboard the user just opened in it.
    @State private var offersTestField = false

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.l) {
            OnboardingStepTitle("onboarding.fullAccess.title", "onboarding.fullAccess.prompt")

            OnboardingStatusCard(kind: .fullAccess, footer: "onboarding.fullAccess.statusFooter")

            VStack(alignment: .leading, spacing: DS.Spacing.s) {
                ExplanationRow(symbol: "keyboard", text: "onboarding.fullAccess.without")
                ExplanationRow(symbol: "exclamationmark.bubble", text: "onboarding.fullAccess.warning")
                ExplanationRow(symbol: "hand.tap", text: "onboarding.fullAccess.onlyOnTap")
            }
            .dsCard()

            if keyboard.status.fullAccess != .on {
                VStack(alignment: .leading, spacing: DS.Spacing.s) {
                    Text("setup.fullAccess.manual")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                    OpenKeyboardSettingsButton()
                }
            }

            // iOS tells no app when its keyboard got Full Access; the
            // keyboard says so itself when it opens. This field is where to
            // open it after turning the switch on, so the status can turn
            // green on this step.
            if offersTestField {
                KeyboardTestField()
            }
        }
        .onAppear {
            if keyboard.status.fullAccess != .on { offersTestField = true }
        }
        .task { await keyboard.followLiveUpdates() }
    }
}

/// One status line, and until it is green, the honest note about why it may
/// still be grey and "Check again".
private struct OnboardingStatusCard: View {

    @Environment(KeyboardStatusMonitor.self) private var keyboard
    let kind: KeyboardStatusRow.Kind
    let footer: LocalizedStringKey

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            KeyboardStatusRow(kind: kind, status: keyboard.status)
            if !isConfirmed {
                Text(footer)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                Button("onboarding.status.checkAgain") { keyboard.refresh() }
                    .buttonStyle(.dsSecondary)
            }
        }
        .dsCard()
    }

    private var isConfirmed: Bool {
        switch kind {
        case .keyboard:   return keyboard.status.isEnabled
        case .fullAccess: return keyboard.status.fullAccess == .on
        }
    }
}

private struct ExplanationRow: View {
    let symbol: String
    let text: LocalizedStringKey

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: DS.Spacing.s) {
            Image(systemName: symbol)
                .foregroundStyle(Color.accentColor)
                .frame(width: 22)
                .accessibilityHidden(true)
            Text(text).font(.subheadline)
        }
    }
}
