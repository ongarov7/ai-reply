import Foundation

// MARK: - Plans

struct WatchReplyPlan: Equatable, Sendable {
    var source: String
    var sourceKind: ReplyContextSource
    var instruction: String
    /// nil: the persona's own length.
    var length: ReplyLength?
    /// nil: the reply follows the copied message.
    var responseLanguage: ReplyLanguagePreference?
    var inputLanguage: KeyboardLanguage?
}

struct WatchComposePlan: Equatable, Sendable {
    var instruction: String
    var inputLanguage: KeyboardLanguage?
}

/// Which of the two existing pipelines an instruction goes through. There is
/// no third: the watch adds a way to give an instruction, not a way to write.
enum WatchGenerationPlan: Equatable, Sendable {
    case reply(WatchReplyPlan)
    case compose(WatchComposePlan)
    /// Something was copied but iOS refused the paste. Treating that as
    /// "nothing copied" would write a new message when the user asked for a
    /// reply, so the reply composer opens with a card to type the message into.
    case needsCopiedMessage(instruction: String)
    case refused(WatchVoiceFailure, limit: Int?)
}

// MARK: - Router

enum WatchVoiceRouter {

    struct Limits: Equatable, Sendable {
        /// `ReplyInstruction.maximumCharacters`.
        var replyInstruction: Int
        /// `AILimits.current.instructionCharacters`.
        var composeInstruction: Int
    }

    /// - Parameters:
    ///   - copied: the copied message, read once on behalf of this command.
    ///   - existingText: the selection or the field's text, for a rewrite.
    static func plan(
        for command: WatchVoiceCommand,
        copied: Result<ReplyContext, AIReplyError>?,
        existingText: String?,
        limits: Limits
    ) -> WatchGenerationPlan {
        let dictated = command.instruction
        let options = command.options
        let spoken = command.instructionLanguage
        let inputLanguage = KeyboardLanguage(rawValue: spoken.rawValue)

        func compose(_ instruction: String) -> WatchGenerationPlan {
            guard fits(instruction, limits.composeInstruction) else {
                return .refused(.instructionTooLong, limit: limits.composeInstruction)
            }
            return .compose(WatchComposePlan(instruction: instruction, inputLanguage: inputLanguage))
        }

        switch options.mode {
        case .newMessage:
            return compose(WatchVoiceInstruction.forCompose(dictated, options: options, spokenIn: spoken))

        case .reply:
            switch copied {
            case .success(let context):
                let instruction = WatchVoiceInstruction.forReply(dictated, options: options, spokenIn: spoken)
                guard fits(instruction, limits.replyInstruction) else {
                    return .refused(.instructionTooLong, limit: limits.replyInstruction)
                }
                return .reply(WatchReplyPlan(
                    source: context.text,
                    sourceKind: context.source,
                    instruction: instruction,
                    length: options.length.map(replyLength),
                    responseLanguage: options.responseLanguage.flatMap { ReplyLanguagePreference(rawValue: $0.rawValue) },
                    inputLanguage: inputLanguage
                ))
            case .failure(.pasteDenied):
                return .needsCopiedMessage(instruction: dictated)
            case .failure(.noSourceMessage), .none:
                // Nothing to answer: the instruction still describes a message.
                return compose(WatchVoiceInstruction.forCompose(dictated, options: options, spokenIn: spoken))
            case .failure(let error):
                return .refused(WatchVoiceFailure(error), limit: nil)
            }

        case .rewrite, .shorten, .expand:
            guard let existing = existingText?.trimmingCharacters(in: .whitespacesAndNewlines), !existing.isEmpty else {
                return .refused(.nothingToRewrite, limit: nil)
            }
            return compose(WatchVoiceInstruction.forEditing(existing, dictated: dictated, options: options, spokenIn: spoken))
        }
    }

    /// The server's reply length has two values. "Long" is the longer of
    /// them, plus the wish in the instruction (`WatchVoiceInstruction`).
    private static func replyLength(_ length: WatchVoiceLength) -> ReplyLength {
        length == .short ? .short : .medium
    }

    private static func fits(_ instruction: String, _ limit: Int) -> Bool {
        instruction.unicodeScalars.count <= limit
    }
}

// MARK: - Instruction text

/// What the watch's choices add to the dictated words.
///
/// First person and in parentheses, in the language the user spoke, for the
/// reason `ReplyInstruction.languageRule` gives: this all travels in the block
/// the server's rules treat as a person's request. It is the only way to ask
/// for these on `/ai/reply` and `/ai/compose` without changing the server.
enum WatchVoiceInstruction {

    static func forReply(_ dictated: String, options: WatchVoiceOptions, spokenIn language: WatchVoiceLanguage) -> String {
        // Short and medium are the persona's `reply_length`; the response
        // language is the profile's `reply_language`. Only "long" needs words.
        join(dictated, options.length == .long ? lengthNote(.long, language) : nil)
    }

    static func forCompose(_ dictated: String, options: WatchVoiceOptions, spokenIn language: WatchVoiceLanguage) -> String {
        join(dictated, options.length.flatMap { lengthNote($0, language) },
             options.responseLanguage.map { languageNote($0, language) })
    }

    static func forEditing(_ existing: String, dictated: String, options: WatchVoiceOptions,
                           spokenIn language: WatchVoiceLanguage) -> String {
        let request = join(directive(options.mode, language), dictated, options.length.flatMap { lengthNote($0, language) },
                           options.responseLanguage.map { languageNote($0, language) })
        return request + "\n\n" + textLabel(language) + ": " + existing
    }

    private static func join(_ parts: String?...) -> String {
        parts.compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " ")
    }

    private static func lengthNote(_ length: WatchVoiceLength, _ language: WatchVoiceLanguage) -> String? {
        switch (length, language) {
        case (.medium, _):        return nil
        case (.short, .english):  return "(Keep it short.)"
        case (.short, .russian):  return "(Напиши коротко.)"
        case (.short, .kazakh):   return "(Қысқа жаз.)"
        case (.long, .english):   return "(Write a longer, more detailed message.)"
        case (.long, .russian):   return "(Напиши подробнее и развёрнуто.)"
        case (.long, .kazakh):    return "(Толығырақ, кеңірек жаз.)"
        }
    }

    private static func languageNote(_ target: WatchVoiceLanguage, _ language: WatchVoiceLanguage) -> String {
        switch (target, language) {
        case (.english, .english): return "(Write it in English.)"
        case (.russian, .english): return "(Write it in Russian.)"
        case (.kazakh, .english):  return "(Write it in Kazakh.)"
        case (.english, .russian): return "(Напиши на английском языке.)"
        case (.russian, .russian): return "(Напиши на русском языке.)"
        case (.kazakh, .russian):  return "(Напиши на казахском языке.)"
        case (.english, .kazakh):  return "(Ағылшын тілінде жаз.)"
        case (.russian, .kazakh):  return "(Орыс тілінде жаз.)"
        case (.kazakh, .kazakh):   return "(Қазақ тілінде жаз.)"
        }
    }

    private static func directive(_ mode: WatchVoiceMode, _ language: WatchVoiceLanguage) -> String? {
        switch (mode, language) {
        case (.reply, _), (.newMessage, _): return nil
        case (.rewrite, .english): return "Rewrite my text below."
        case (.rewrite, .russian): return "Перепиши мой текст ниже."
        case (.rewrite, .kazakh):  return "Төмендегі мәтінімді қайта жаз."
        case (.shorten, .english): return "Make my text below shorter and keep its meaning."
        case (.shorten, .russian): return "Сократи мой текст ниже, сохрани смысл."
        case (.shorten, .kazakh):  return "Төмендегі мәтінімді мағынасын сақтап қысқарт."
        case (.expand, .english):  return "Make my text below longer and more detailed."
        case (.expand, .russian):  return "Сделай мой текст ниже длиннее и подробнее."
        case (.expand, .kazakh):   return "Төмендегі мәтінімді толығырақ әрі кеңірек жаз."
        }
    }

    private static func textLabel(_ language: WatchVoiceLanguage) -> String {
        switch language {
        case .english: return "My text"
        case .russian: return "Мой текст"
        case .kazakh:  return "Менің мәтінім"
        }
    }
}

// MARK: - Failures

extension WatchVoiceFailure {

    /// The keyboard's closed set of errors, in the protocol's words.
    init(_ error: AIReplyError) {
        switch error {
        case .noSourceMessage, .pasteDenied:  self = .copiedMessageUnreadable
        case .messageTooLong:                 self = .copiedMessageTooLong
        case .fullAccessRequired:             self = .fullAccessRequired
        case .notConfigured, .authenticationFailed: self = .notSignedIn
        case .offline:                        self = .offline
        case .timedOut:                       self = .timedOut
        case .rateLimited:                    self = .rateLimited
        case .quotaExhausted:                 self = .quotaExhausted
        case .monthlyQuotaExhausted:          self = .monthlyQuotaExhausted
        case .consentRequired:                self = .consentRequired
        case .noInstruction:                  self = .instructionEmpty
        case .instructionTooLong:             self = .instructionTooLong
        case .cancelled, .emptyResponse, .serviceUnavailable: self = .serviceUnavailable
        }
    }
}
