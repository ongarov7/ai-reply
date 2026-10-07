import SwiftUI

/// What AI Reply does, in one screen: a chat, the keyboard under it with the
/// persona row, and the three things the user will actually do.
struct OnboardingWelcomeStep: View {

    @Environment(AppSettings.self) private var settings
    let samples: OnboardingSamples

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.l) {
            VStack(alignment: .leading, spacing: DS.Spacing.s) {
                AppMarkView(size: 56)
                Text("onboarding.welcome.title")
                    .font(.largeTitle.weight(.semibold))
                    .accessibilityAddTraits(.isHeader)
                Text("onboarding.welcome.body")
                    .font(.body)
                    .foregroundStyle(.secondary)
            }

            MockDevice {
                VStack(alignment: .leading, spacing: 0) {
                    MockIncomingBubble(text: samples.practiceIncoming)
                    MockMessageField()
                }
            } keyboard: {
                VStack(spacing: DS.Spacing.xs) {
                    MockPersonaRow(language: settings.effectiveLanguage, highlighted: .friend)
                    MockKeys()
                }
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel("onboarding.welcome.mock")

            VStack(alignment: .leading, spacing: DS.Spacing.s) {
                FeatureRow(symbol: "doc.on.clipboard", text: "onboarding.welcome.point.copy")
                FeatureRow(symbol: "person.2", text: "onboarding.welcome.point.templates")
                FeatureRow(symbol: "square.and.pencil", text: "onboarding.welcome.point.edit")
            }
            .dsCard()
        }
    }
}

private struct FeatureRow: View {
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
