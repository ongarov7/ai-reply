import Foundation

/// A communication template: the CONTEXT a reply is written in, never a
/// canned response. Nothing in here is a sentence the user will send; it is
/// all guidance the model applies to the message actually received.
struct ReplyTemplate: Codable, Hashable, Identifiable, Sendable {

    static let maximumInstructionCharacters = 600
    static let maximumNameCharacters = 60

    /// Stable identifier. The four built-ins use their relationship name
    /// ("friend", "client", "business", "work") so the backend and the prompt
    /// builder can recognise them; custom templates get a UUID string.
    let id: String

    /// True for the four templates the app ships. Built-ins can be edited and
    /// hidden but never deleted, so a user cannot end up with an empty bar and
    /// no way back.
    let isBuiltIn: Bool

    let relationship: RelationshipKind

    /// nil for a built-in that has not been renamed, which then shows its
    /// localized name. A custom template always has one.
    var customName: String?

    var tone: ReplyTone

    /// Anything else the user wants this template to know, in their own words.
    /// Dictation lands here verbatim: structured extraction adds to the fields
    /// below, it never replaces what the user actually said.
    var instructions: String

    var workingHoursBehaviour: WorkingHoursBehaviour
    var replyLength: ReplyLength
    var emojiPolicy: EmojiPolicy

    /// Business facts that apply only to this kind of conversation. `nil` for a
    /// template that has none - a Friend template should not carry an empty
    /// shop description into every prompt.
    var business: BusinessContext?

    /// Hours for this template only, when they differ from the profile's.
    /// `nil` means "use the profile's working hours", which is what almost
    /// everyone wants and what an empty editor leaves behind.
    var workingHoursOverride: WorkingHours?

    /// Hidden templates stay configured but leave the keyboard bar. This is how
    /// a built-in is "removed" without actually being destroyed.
    var isVisible: Bool

    /// Position in the keyboard bar and in the settings list.
    var sortIndex: Int

    // MARK: Naming

    /// The name to display, in the given language. Custom names are shown
    /// verbatim - a user who typed "Supplier" gets "Supplier" in every UI
    /// language, because that is their word, not ours to translate.
    func displayName(language: KeyboardLanguage) -> String {
        if let customName, !customName.trimmingCharacters(in: .whitespaces).isEmpty {
            return customName
        }
        return Self.builtInName(relationship, language: language)
    }

    /// Localized names for the built-in templates, as the owner set them
    /// (20.10.2026): Friend, Client, Colleague, Partner. The raw values stay
    /// `work` and `business`.
    static func builtInName(_ relationship: RelationshipKind, language: KeyboardLanguage) -> String {
        switch (relationship, language) {
        case (.friend, .english):   return "Friend"
        case (.friend, .russian):   return "Друг"
        case (.friend, .kazakh):    return "Дос"
        case (.client, .english):   return "Client"
        case (.client, .russian):   return "Клиент"
        case (.client, .kazakh):    return "Клиент"
        case (.business, .english): return "Partner"
        case (.business, .russian): return "Партнёр"
        case (.business, .kazakh):  return "Серіктес"
        case (.work, .english):     return "Colleague"
        case (.work, .russian):     return "Коллега"
        case (.work, .kazakh):      return "Әріптес"
        case (.custom, .english):   return "Custom"
        case (.custom, .russian):   return "Свой"
        case (.custom, .kazakh):    return "Өзімнің"
        }
    }

    // MARK: Factories

    static func builtIn(_ relationship: RelationshipKind, sortIndex: Int) -> ReplyTemplate {
        ReplyTemplate(
            id: relationship.rawValue,
            isBuiltIn: true,
            relationship: relationship,
            customName: nil,
            tone: relationship.defaultTone,
            // Empty on purpose. The behaviour for the four built-in kinds lives
            // in the prompt builder, which keeps it out of every user's saved
            // data and lets it be improved without a migration. This field is
            // for what THIS user wants to add on top.
            instructions: "",
            workingHoursBehaviour: relationship.defaultWorkingHoursBehaviour,
            replyLength: .short,
            emojiPolicy: relationship.defaultEmojiPolicy,
            business: relationship.usesBusinessContext ? .empty : nil,
            workingHoursOverride: nil,
            isVisible: true,
            sortIndex: sortIndex
        )
    }

    static func custom(name: String, sortIndex: Int) -> ReplyTemplate {
        ReplyTemplate(
            id: UUID().uuidString,
            isBuiltIn: false,
            relationship: .custom,
            customName: name,
            tone: .natural,
            instructions: "",
            workingHoursBehaviour: .mentionWhenRelevant,
            replyLength: .short,
            emojiPolicy: .minimal,
            business: .empty,
            workingHoursOverride: nil,
            isVisible: true,
            sortIndex: sortIndex
        )
    }

    static var defaults: [ReplyTemplate] {
        RelationshipKind.builtIns.enumerated().map { index, kind in
            builtIn(kind, sortIndex: index)
        }
    }

    // MARK: Editing

    mutating func setInstructions(_ value: String) {
        instructions = value.unicodeScalars.count <= Self.maximumInstructionCharacters
            ? value
            : String(value.prefix(Self.maximumInstructionCharacters))
    }

    /// The business facts actually worth sending for this template: the
    /// template's own, or nothing. Callers layer the profile's underneath.
    var effectiveBusiness: BusinessContext? {
        guard let business, !business.isEmpty else { return nil }
        return business
    }

    /// The hours that govern this template. The override wins when it exists
    /// and is switched on; otherwise the profile's own schedule applies.
    func effectiveWorkingHours(profile: WorkingHours) -> WorkingHours {
        guard let workingHoursOverride, workingHoursOverride.isEnabled else { return profile }
        return workingHoursOverride
    }

    /// Ensures the business container exists before an editor writes into it,
    /// so a custom template created before this field existed can still be
    /// given one.
    mutating func ensureBusinessContext() {
        if business == nil { business = .empty }
    }

    mutating func setName(_ value: String) {
        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else {
            // Clearing a built-in's name restores its localized one. Clearing a
            // custom template's name is refused, because it has no fallback.
            if isBuiltIn { customName = nil }
            return
        }
        customName = String(trimmed.prefix(Self.maximumNameCharacters))
    }

    // MARK: Coding

    // A configuration written before these fields existed has to keep loading,
    // and the defaults have to be per-relationship rather than blanket - a
    // Friend decoded from an old file must not acquire a business container it
    // never had.
    enum CodingKeys: String, CodingKey {
        case id, isBuiltIn, relationship, customName, tone, instructions
        case workingHoursBehaviour, replyLength, emojiPolicy, business, workingHoursOverride
        case isVisible, sortIndex
    }

    init(
        id: String,
        isBuiltIn: Bool,
        relationship: RelationshipKind,
        customName: String?,
        tone: ReplyTone,
        instructions: String,
        workingHoursBehaviour: WorkingHoursBehaviour,
        replyLength: ReplyLength,
        emojiPolicy: EmojiPolicy,
        business: BusinessContext?,
        workingHoursOverride: WorkingHours?,
        isVisible: Bool,
        sortIndex: Int
    ) {
        self.id = id
        self.isBuiltIn = isBuiltIn
        self.relationship = relationship
        self.customName = customName
        self.tone = tone
        self.instructions = instructions
        self.workingHoursBehaviour = workingHoursBehaviour
        self.replyLength = replyLength
        self.emojiPolicy = emojiPolicy
        self.business = business
        self.workingHoursOverride = workingHoursOverride
        self.isVisible = isVisible
        self.sortIndex = sortIndex
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        isBuiltIn = try container.decodeIfPresent(Bool.self, forKey: .isBuiltIn) ?? false
        let kind = try container.decodeIfPresent(RelationshipKind.self, forKey: .relationship) ?? .custom
        relationship = kind
        customName = try container.decodeIfPresent(String.self, forKey: .customName)
        tone = try container.decodeIfPresent(ReplyTone.self, forKey: .tone) ?? kind.defaultTone
        instructions = try container.decodeIfPresent(String.self, forKey: .instructions) ?? ""
        workingHoursBehaviour = try container.decodeIfPresent(
            WorkingHoursBehaviour.self, forKey: .workingHoursBehaviour
        ) ?? kind.defaultWorkingHoursBehaviour
        replyLength = try container.decodeIfPresent(ReplyLength.self, forKey: .replyLength) ?? .short
        emojiPolicy = try container.decodeIfPresent(EmojiPolicy.self, forKey: .emojiPolicy)
            ?? kind.defaultEmojiPolicy
        business = try container.decodeIfPresent(BusinessContext.self, forKey: .business)
            ?? (kind.usesBusinessContext ? .empty : nil)
        workingHoursOverride = try container.decodeIfPresent(WorkingHours.self, forKey: .workingHoursOverride)
        isVisible = try container.decodeIfPresent(Bool.self, forKey: .isVisible) ?? true
        sortIndex = try container.decodeIfPresent(Int.self, forKey: .sortIndex) ?? 0
    }
}
