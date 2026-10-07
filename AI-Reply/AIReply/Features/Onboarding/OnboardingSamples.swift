import Foundation

/// The example conversations the tutorial and the practice play through, in
/// the app's language and, in Russian, the user's grammatical gender.
///
/// Мысал хат алмасулар: қолданба тілінде, орысшада пайдаланушының жынысына сай.
///
/// Fixed texts from the string catalog, not generated: the practice never
/// touches the network or the daily quota. Only the Russian sentences differ
/// by gender; the other languages carry the same text under all three keys.
struct OnboardingSamples {

    let settings: AppSettings
    /// nil (never asked) reads like `unspecified`: neutral wording.
    let gender: GrammaticalGender?

    var keyboardStrings: AIReplyStrings { AIReplyStrings.forLanguage(settings.effectiveLanguage) }

    // MARK: Practice

    var practiceIncoming: String { settings.localized("onboarding.sample.practice.incoming") }

    var practiceReply: String {
        switch gender ?? .unspecified {
        case .male:        return settings.localized("onboarding.sample.practice.reply.male")
        case .female:      return settings.localized("onboarding.sample.practice.reply.female")
        case .unspecified: return settings.localized("onboarding.sample.practice.reply.unspecified")
        }
    }

    // MARK: Copy-and-reply tutorial

    var tutorialIncoming: String { settings.localized("onboarding.sample.tutorial.incoming") }

    var tutorialInstruction: String {
        switch gender ?? .unspecified {
        case .male:        return settings.localized("onboarding.sample.tutorial.instruction.male")
        case .female:      return settings.localized("onboarding.sample.tutorial.instruction.female")
        case .unspecified: return settings.localized("onboarding.sample.tutorial.instruction.unspecified")
        }
    }

    var tutorialReply: String {
        switch gender ?? .unspecified {
        case .male:        return settings.localized("onboarding.sample.tutorial.reply.male")
        case .female:      return settings.localized("onboarding.sample.tutorial.reply.female")
        case .unspecified: return settings.localized("onboarding.sample.tutorial.reply.unspecified")
        }
    }
}
