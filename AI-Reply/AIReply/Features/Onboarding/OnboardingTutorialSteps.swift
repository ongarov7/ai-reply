import SwiftUI
import UIKit

/// «Как пользоваться» in three steps: copy a message, choose who it is for,
/// get the reply.
///
/// Үш қадамдық оқыту: көшіру, әңгімелесушіні таңдау, жауап алу.
///
/// Everything here is local: the messages and replies are fixed examples
/// from the string catalog (`OnboardingSamples`), so nothing is sent and no
/// reply is spent. The Copy buttons really write to the clipboard - writing
/// never makes iOS ask anything - so the user can paste the example
/// somewhere and see it work.

/// The footer of a tutorial step: dots, the main button, «Пропустить».
private struct TutorialButtons: View {
    let position: Int
    let primary: LocalizedStringKey
    let onPrimary: () -> Void
    /// nil on the last step, whose main button already moves on.
    let onSkip: (() -> Void)?

    var body: some View {
        OnboardingPageDots(position: position, count: OnboardingFlow.Step.tutorial.count)
        Button(primary, action: onPrimary)
            .buttonStyle(.dsPrimary)
        if let onSkip {
            Button("onboarding.skip", action: onSkip)
                .buttonStyle(.dsText)
        }
    }
}

/// Copies a text and says so with a checkmark, in place.
private struct CopyButton: View {
    let text: String
    var onCopy: () -> Void = {}

    @State private var didCopy = false

    var body: some View {
        Button {
            // A write: iOS never asks about it.
            UIPasteboard.general.string = text
            didCopy = true
            onCopy()
        } label: {
            Label(didCopy ? "compose.copied" : "common.copy",
                  systemImage: didCopy ? "checkmark" : "doc.on.doc")
        }
        .buttonStyle(.dsOutline)
        .onChange(of: text) { didCopy = false }
    }
}

// MARK: - 1 of 3

struct TutorialCopyStep: View {
    let samples: OnboardingSamples
    /// The keyboard step is still ahead: the footnote promises it.
    let mentionsKeyboard: Bool
    let onNext: () -> Void
    let onSkip: () -> Void

    var body: some View {
        OnboardingPage {
            VStack(alignment: .leading, spacing: DS.Spacing.l) {
                OnboardingStepTitle("onboarding.tutorial.copy.title", "onboarding.tutorial.copy.body")

                VStack(alignment: .leading, spacing: DS.Spacing.m) {
                    OnboardingBubble(text: samples.demoMessage, side: .incoming)
                    CopyButton(text: samples.demoMessage)
                }
                .dsCard()

                if mentionsKeyboard {
                    Text("onboarding.tutorial.copy.next")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
        } buttons: {
            TutorialButtons(position: 1, primary: "onboarding.next", onPrimary: onNext, onSkip: onSkip)
        }
        .onboardingTutorialHeader(position: 1, count: OnboardingFlow.Step.tutorial.count)
    }
}

// MARK: - 2 of 3

struct TutorialRecipientStep: View {
    @Binding var selection: RelationshipKind?
    let onNext: () -> Void
    let onSkip: () -> Void

    var body: some View {
        OnboardingPage {
            VStack(alignment: .leading, spacing: DS.Spacing.l) {
                OnboardingStepTitle("onboarding.tutorial.recipient.title", "onboarding.tutorial.recipient.body")

                VStack(alignment: .leading, spacing: DS.Spacing.s) {
                    Text("compose.recipients")
                        .font(.headline)
                        .accessibilityAddTraits(.isHeader)
                    TutorialRecipientGrid(selection: $selection)
                }

                Label {
                    Text("onboarding.tutorial.recipient.hint")
                } icon: {
                    Image(systemName: "slider.horizontal.3").foregroundStyle(.secondary)
                }
                .font(.subheadline)
            }
        } buttons: {
            TutorialButtons(position: 2, primary: "onboarding.next", onPrimary: onNext, onSkip: onSkip)
        }
        .onboardingTutorialHeader(position: 2, count: OnboardingFlow.Step.tutorial.count)
    }
}

/// The four built-ins in this user's keyboard order, with their own names.
private struct TutorialRecipientGrid: View {
    @Binding var selection: RelationshipKind?

    @Environment(AppSettings.self) private var settings
    @Environment(ReplyConfigurationModel.self) private var model

    var body: some View {
        RecipientGrid(
            items: RecipientGrid.items(kinds: model.configuration.builtInOrder, language: settings.effectiveLanguage),
            selection: selection?.rawValue,
            onSelect: { id in selection = RelationshipKind(rawValue: id) }
        )
    }
}

// MARK: - 3 of 3

struct TutorialReplyStep: View {
    let samples: OnboardingSamples
    @Binding var selection: RelationshipKind?
    /// «Начать пользоваться»: on to the keyboard, or into the app.
    let onDone: () -> Void

    @State private var didCopy = false

    var body: some View {
        OnboardingPage {
            VStack(alignment: .leading, spacing: DS.Spacing.l) {
                OnboardingStepTitle("onboarding.tutorial.reply.title", "onboarding.tutorial.reply.body")

                OnboardingBubble(text: samples.demoMessage, side: .incoming)
                    .dsCard()

                VStack(alignment: .leading, spacing: DS.Spacing.s) {
                    Text("compose.recipients")
                        .font(.headline)
                        .accessibilityAddTraits(.isHeader)
                    TutorialRecipientGrid(selection: recipient)
                }

                replyCard
            }
        } buttons: {
            TutorialButtons(position: 3, primary: "onboarding.tutorial.start", onPrimary: onDone, onSkip: nil)
        }
        .onboardingTutorialHeader(position: 3, count: OnboardingFlow.Step.tutorial.count)
    }

    /// The choice made on step 2; Client until there is one, the example
    /// being a client's question.
    private var kind: RelationshipKind { selection ?? .client }

    private var recipient: Binding<RelationshipKind?> {
        Binding(get: { kind }, set: { selection = $0 })
    }

    private var replyCard: some View {
        let reply = samples.demoReply(for: kind)
        return VStack(alignment: .leading, spacing: DS.Spacing.s) {
            HStack(alignment: .firstTextBaseline) {
                Text("onboarding.replyOption")
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(.secondary)
                Spacer(minLength: DS.Spacing.s)
                Label("onboarding.tutorial.reply.ready", systemImage: "checkmark")
                    .font(.caption.weight(.medium))
                    .foregroundStyle(Color.accentColor)
            }
            OnboardingBubble(text: reply, side: .reply)
            CopyButton(text: reply) {
                if !didCopy { ProductEvents.track(.onboardingPracticeCompleted) }
                didCopy = true
            }
            .frame(maxWidth: .infinity, alignment: .trailing)

            // Right where the user tapped, so it is seen without scrolling.
            if didCopy {
                Divider().padding(.vertical, DS.Spacing.xxs)
                VStack(alignment: .leading, spacing: DS.Spacing.xxs) {
                    Label("onboarding.tutorial.reply.done.title", systemImage: "checkmark.circle.fill")
                        .font(.headline)
                        .foregroundStyle(.green)
                    Text("onboarding.tutorial.reply.done.body")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                .accessibilityElement(children: .combine)
            }
        }
        .dsCard()
    }
}
