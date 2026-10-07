import SwiftUI

/// Copy → switch keyboard → persona → (instruction) → Reply → Insert, shown
/// on the sketch one stage at a time.
///
/// The stages move on by themselves and on a tap. With Reduce Motion or
/// VoiceOver on, the same stages are a plain numbered list instead: nothing
/// moves, and nothing changes under a screen reader's cursor.
struct OnboardingCopyReplyStep: View {

    @Environment(AppSettings.self) private var settings
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.accessibilityVoiceOverEnabled) private var voiceOver

    let samples: OnboardingSamples

    @State private var stage: CopyReplyStage = .copy

    /// Long enough to read a caption twice.
    private static let stageDuration: Duration = .seconds(3.2)

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.l) {
            OnboardingStepTitle("onboarding.usage.title", "onboarding.usage.prompt")

            if reduceMotion || voiceOver {
                staticSteps
            } else {
                animatedSteps
            }

            DSSection(title: "setup.paste.title") {
                VStack(alignment: .leading, spacing: DS.Spacing.xs) {
                    Text("setup.paste.body").font(.body)
                    Text("setup.paste.footer").font(.footnote).foregroundStyle(.secondary)
                }
                .dsCard()
            }
        }
        .onAppear { ProductEvents.track(.pasteTutorialViewed) }
    }

    private var staticSteps: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            ForEach(CopyReplyStage.allCases, id: \.self) { stage in
                DSStepRow(index: stage.number, text: stage.caption)
            }
        }
        .dsCard()
    }

    private var animatedSteps: some View {
        // Every stage is laid out at once and only the current one is
        // visible - caption and sketch alike - so the block keeps one height
        // and nothing jumps as the stages change.
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            ZStack(alignment: .topLeading) {
                ForEach(CopyReplyStage.allCases, id: \.self) { candidate in
                    DSStepRow(index: candidate.number, text: candidate.caption)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .opacity(candidate == stage ? 1 : 0)
                }
            }
            .accessibilityHidden(true)

            ZStack(alignment: .top) {
                ForEach(CopyReplyStage.allCases, id: \.self) { candidate in
                    CopyReplyMock(stage: candidate, samples: samples, language: settings.effectiveLanguage)
                        .opacity(candidate == stage ? 1 : 0)
                }
            }
            // A tap gesture rather than a Button: a button label is measured
            // at its ideal width first, and the sketch's text then wraps to
            // the wrong number of lines.
            .contentShape(Rectangle())
            .onTapGesture { stage = stage.next }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(stage.caption)
            .accessibilityHint("onboarding.copyReply.tapHint")
            .accessibilityAddTraits(.isButton)
            .accessibilityAction { stage = stage.next }

            HStack(spacing: DS.Spacing.xs) {
                ForEach(CopyReplyStage.allCases, id: \.self) { candidate in
                    Circle()
                        .fill(candidate == stage ? Color.accentColor : Color.dsSeparator)
                        .frame(width: 6, height: 6)
                }
            }
            .frame(maxWidth: .infinity)
            .accessibilityHidden(true)
        }
        .animation(.easeInOut(duration: 0.3), value: stage)
        // Restarted by every change, so a tap also resets the timer.
        .task(id: stage) {
            try? await Task.sleep(for: Self.stageDuration)
            guard !Task.isCancelled else { return }
            stage = stage.next
        }
    }
}

enum CopyReplyStage: Int, CaseIterable {
    case copy
    case switchKeyboard
    case persona
    case instruction
    case reply
    case inserted

    var number: Int { rawValue + 1 }

    /// The stage after this one; the last starts over.
    var next: CopyReplyStage { Self.allCases[(rawValue + 1) % Self.allCases.count] }

    var caption: LocalizedStringKey {
        switch self {
        case .copy:           return "onboarding.copyReply.stage.copy"
        case .switchKeyboard: return "onboarding.copyReply.stage.switch"
        case .persona:        return "onboarding.copyReply.stage.persona"
        case .instruction:    return "onboarding.copyReply.stage.instruction"
        case .reply:          return "onboarding.copyReply.stage.reply"
        case .inserted:       return "onboarding.copyReply.stage.inserted"
        }
    }
}

/// The sketch at one stage of the tutorial.
private struct CopyReplyMock: View {
    let stage: CopyReplyStage
    let samples: OnboardingSamples
    let language: AppLanguage

    var body: some View {
        MockDevice {
            VStack(alignment: .leading, spacing: 0) {
                MockIncomingBubble(text: samples.tutorialIncoming, isHighlighted: stage == .copy) {
                    switch stage {
                    case .copy:                     MockCopyMenu(isHighlighted: true)
                    case .switchKeyboard, .persona: MockCopiedBadge()
                    default:                        EmptyView()
                    }
                }
                MockMessageField(text: stage == .inserted ? samples.tutorialReply : nil)
            }
        } keyboard: {
            keyboard
        }
    }

    @ViewBuilder
    private var keyboard: some View {
        let strings = samples.keyboardStrings
        switch stage {
        case .copy:
            MockKeys()
        case .switchKeyboard:
            MockKeys(highlightsGlobe: true)
        case .persona:
            VStack(spacing: DS.Spacing.xs) {
                MockPersonaRow(language: language, highlighted: .client)
                MockKeys()
            }
        case .instruction:
            MockComposer(strings: strings, source: samples.tutorialIncoming, instruction: samples.tutorialInstruction) {
                MockActionButton(title: strings.generate)
            }
        case .reply:
            MockComposer(strings: strings, source: samples.tutorialIncoming, draft: samples.tutorialReply) {
                MockActionButton(title: strings.insert)
            }
        case .inserted:
            VStack(spacing: DS.Spacing.xs) {
                MockPersonaRow(language: language)
                MockKeys()
            }
        }
    }
}
