import Foundation

/// The example conversations the welcome screen and the tutorial show, in
/// the app's language and, in Russian, the user's grammatical gender.
///
/// Мысал хат алмасулар: қолданба тілінде, орысшада пайдаланушының жынысына сай.
///
/// Fixed texts from the string catalog, not generated: the tutorial never
/// touches the network or the reply quota. Only the welcome reply differs by
/// gender, and only in Russian («Рада» / «Рад» / a neutral sentence); the
/// other languages carry the same text under all three keys. The tutorial's
/// replies are worded so they need no gender at all.
struct OnboardingSamples {

    let settings: AppSettings
    /// nil (never asked) reads like `unspecified`: neutral wording.
    let gender: GrammaticalGender?

    // MARK: Welcome

    var welcomeIncoming: String { settings.localized("onboarding.welcome.incoming") }

    var welcomeReply: String {
        switch gender ?? .unspecified {
        case .male:        return settings.localized("onboarding.welcome.reply.male")
        case .female:      return settings.localized("onboarding.welcome.reply.female")
        case .unspecified: return settings.localized("onboarding.welcome.reply.unspecified")
        }
    }

    // MARK: Tutorial

    /// The message the tutorial copies and answers.
    var demoMessage: String { settings.localized("onboarding.demo.message") }

    /// The ready reply to `demoMessage` for one kind of recipient: a local
    /// example of how the tone changes, never a generated text.
    func demoReply(for kind: RelationshipKind) -> String {
        settings.localized(Self.replyKey(for: kind))
    }

    static func replyKey(for kind: RelationshipKind) -> String {
        switch kind {
        case .friend:           return "onboarding.demo.reply.friend"
        case .client, .custom:  return "onboarding.demo.reply.client"
        case .work:             return "onboarding.demo.reply.colleague"
        case .business:         return "onboarding.demo.reply.partner"
        }
    }
}
