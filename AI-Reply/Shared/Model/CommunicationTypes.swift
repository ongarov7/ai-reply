import Foundation

/// How a reply should sound. Kept deliberately short: five presets a user can
/// hold in their head beat twenty sliders nobody touches.
enum ReplyTone: String, CaseIterable, Codable, Identifiable, Sendable {
    case natural
    case friendly
    case professional
    case formal
    case short

    var id: String { rawValue }
}

/// How long a reply should be. Two values, because the real constraint is
/// "messenger short", and the model already knows what that means.
enum ReplyLength: String, CaseIterable, Codable, Identifiable, Sendable {
    case short
    case medium

    var id: String { rawValue }
}

/// How much emoji a reply may use. A Friend template that answers with no
/// emoji at all reads as cold; a Client template that answers with three reads
/// as unserious. One setting, three values, is enough to separate them.
enum EmojiPolicy: String, CaseIterable, Codable, Identifiable, Sendable {
    case allowed
    case minimal
    case none

    var id: String { rawValue }
}

/// What a template should do with the user's working hours.
enum WorkingHoursBehaviour: String, CaseIterable, Codable, Identifiable, Sendable {
    /// Never bring hours up. Right for the Friend template.
    case ignore
    /// Mention them only when the incoming message asks for something
    /// time-sensitive that falls outside them. The sane default.
    case mentionWhenRelevant = "mention_when_relevant"
    /// Acknowledge them whenever they are relevant at all.
    case alwaysMention = "always_mention"

    var id: String { rawValue }
}

/// The relationship a template describes. Drives the built-in behaviour that
/// the brief specifies for each one.
///
/// The raw values are stored (templates' `id` and `relationship`, the
/// profile's active relationships, `shared.lastTemplateID`) and sent to the
/// server, so they never change: `business` is the one the product calls
/// Partner (Партнёр), `work` the one it calls Colleague (Коллега). An unknown
/// value would fail the whole configuration's decode.
enum RelationshipKind: String, CaseIterable, Codable, Identifiable, Sendable {
    case friend
    case client
    /// Partner - Партнёр, Серіктес.
    case business
    /// Colleague - Коллега, Әріптес.
    case work
    case custom

    var id: String { rawValue }

    /// The four the app ships with, in bar order: Друг, Клиент, Коллега,
    /// Партнёр. `custom` is not one of them: it is what a user-created
    /// template is. Only a new configuration takes this order; an existing
    /// user keeps the one their templates' `sortIndex` already holds.
    static let builtIns: [RelationshipKind] = [.friend, .client, .work, .business]

    /// The SF Symbol drawn with the name, on the keyboard's persona row and in
    /// the app.
    ///
    /// Every one exists on iOS 17.0, the deployment target (checked against
    /// the SF Symbols availability table). SF Symbols has no `handshake`, so
    /// Partner is two people with a line between them; it differs from
    /// Colleague's briefcase and from Friend's two people side by side.
    var symbolName: String {
        switch self {
        case .friend:   return "person.2"
        case .client:   return "person"
        case .work:     return "briefcase"
        case .business: return "person.line.dotted.person"
        case .custom:   return "person.crop.circle"
        }
    }

    /// Sensible starting point for a template of this kind.
    var defaultTone: ReplyTone {
        switch self {
        case .friend:   return .friendly
        case .client:   return .professional
        case .business: return .professional
        case .work:     return .natural
        case .custom:   return .natural
        }
    }

    var defaultEmojiPolicy: EmojiPolicy {
        switch self {
        case .friend:   return .allowed
        case .client:   return .minimal
        case .business: return .none
        case .work:     return .minimal
        case .custom:   return .minimal
        }
    }

    /// Whether this kind of conversation has business facts worth configuring.
    /// A friend does not: asking someone what they sell before they can set up
    /// a casual template would be a worse product, not a more capable one.
    var usesBusinessContext: Bool {
        switch self {
        case .friend:   return false
        case .client, .business, .work, .custom: return true
        }
    }

    var defaultWorkingHoursBehaviour: WorkingHoursBehaviour {
        switch self {
        case .friend:   return .ignore
        case .client:   return .mentionWhenRelevant
        case .business: return .mentionWhenRelevant
        case .work:     return .mentionWhenRelevant
        case .custom:   return .mentionWhenRelevant
        }
    }
}
