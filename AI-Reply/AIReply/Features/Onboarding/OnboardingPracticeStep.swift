import SwiftUI

/// A practice run on the sketch: copy, switch to AI Reply, pick a persona,
/// Reply, Insert.
///
/// Жаттығу толығымен құрылғыда өтеді: желі жоқ, күндік лимит жұмсалмайды.
///
/// Entirely local - no network, no quota. The "written" reply is a fixed
/// sample in the app's language and, in Russian, the user's gender.
struct OnboardingPracticeStep: View {

    @Environment(AppSettings.self) private var settings
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    let samples: OnboardingSamples

    @State private var stage: PracticeStage = .incoming

    /// Long enough to read as work, short enough not to bore.
    private static let writingDuration: Duration = .seconds(1.2)

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.l) {
            OnboardingStepTitle("onboarding.practice.title", "onboarding.practice.prompt")

            if let hint = stage.hint {
                Label {
                    Text(hint).font(.subheadline.weight(.medium))
                } icon: {
                    Image(systemName: "hand.tap").foregroundStyle(Color.accentColor)
                }
                .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                .accessibilityAddTraits(.updatesFrequently)
            }

            MockDevice {
                chat
            } keyboard: {
                keyboard
            }

            if stage == .inserted {
                VStack(alignment: .leading, spacing: DS.Spacing.xs) {
                    Label("onboarding.practice.done.title", systemImage: "checkmark.seal.fill")
                        .font(.headline)
                        .foregroundStyle(Color.green)
                    Text("onboarding.practice.done.body")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                .dsCard()
            }
        }
        .animation(reduceMotion ? nil : .easeInOut(duration: 0.25), value: stage)
        .task(id: stage) {
            guard stage == .writing else { return }
            try? await Task.sleep(for: Self.writingDuration)
            guard !Task.isCancelled else { return }
            stage = .result
        }
        .onChange(of: stage) { _, stage in
            if stage == .inserted { ProductEvents.track(.onboardingPracticeCompleted) }
        }
    }

    // MARK: Sketch

    private var chat: some View {
        VStack(alignment: .leading, spacing: 0) {
            MockIncomingBubble(text: samples.practiceIncoming, isHighlighted: stage == .incoming) {
                switch stage {
                case .incoming:
                    Button { stage = .copied } label: {
                        MockCopyMenu(isHighlighted: true)
                            .frame(minHeight: DS.Layout.minimumTouchTarget)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                case .copied:
                    MockCopiedBadge()
                default:
                    EmptyView()
                }
            }
            MockMessageField(text: stage == .inserted ? samples.practiceReply : nil)
        }
    }

    @ViewBuilder
    private var keyboard: some View {
        let strings = samples.keyboardStrings
        switch stage {
        case .incoming:
            MockKeys()
        case .copied:
            MockKeys {
                Button { stage = .keyboard } label: {
                    Label("onboarding.practice.openKeyboard", systemImage: "globe")
                        .font(.footnote.weight(.semibold))
                        .lineLimit(1)
                        .foregroundStyle(Color.white)
                        .padding(.horizontal, DS.Spacing.s)
                        .frame(minHeight: 30)
                        .background(Capsule().fill(Color.accentColor))
                        .frame(minHeight: DS.Layout.minimumTouchTarget)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                // Its whole name on one line; the space bar takes what is left.
                .fixedSize()
            }
        case .keyboard:
            VStack(spacing: DS.Spacing.xs) {
                MockPersonaRow(language: settings.effectiveLanguage) { _ in stage = .composer }
                MockKeys()
            }
        case .composer:
            MockComposer(strings: strings, source: samples.practiceIncoming) {
                MockActionButton(title: strings.generate) { stage = .writing }
            }
        case .writing:
            MockComposer(strings: strings, source: samples.practiceIncoming) {
                HStack(spacing: DS.Spacing.xs) {
                    ProgressView()
                    Text(verbatim: strings.generating)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                .frame(minHeight: DS.Layout.minimumTouchTarget)
            }
        case .result:
            MockComposer(strings: strings, source: samples.practiceIncoming, draft: samples.practiceReply) {
                MockActionButton(title: strings.insert) { stage = .inserted }
            }
        case .inserted:
            VStack(spacing: DS.Spacing.xs) {
                MockPersonaRow(language: settings.effectiveLanguage)
                MockKeys()
            }
        }
    }
}

/// Where the practice run is. Each stage waits for one tap, except `writing`,
/// which moves on by itself.
enum PracticeStage: CaseIterable {
    case incoming
    case copied
    case keyboard
    case composer
    case writing
    case result
    case inserted

    /// What to do now. None once the reply is in: the step says it is done.
    var hint: LocalizedStringKey? {
        switch self {
        case .incoming: return "onboarding.practice.hint.copy"
        case .copied:   return "onboarding.practice.hint.switch"
        case .keyboard: return "onboarding.practice.hint.persona"
        case .composer: return "onboarding.practice.hint.reply"
        case .writing:  return "onboarding.practice.hint.writing"
        case .result:   return "onboarding.practice.hint.insert"
        case .inserted: return nil
        }
    }
}
