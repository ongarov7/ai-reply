import Foundation

/// The lightweight personal communication profile built during onboarding.
///
/// It never leaves the device except as part of an AI request the user
/// explicitly triggered, and only the two fields the model actually needs
/// (`description`, `preferredTone`) are sent even then.
struct UserProfile: Codable, Hashable, Sendable {

    /// Maximum stored length. The brief asks for roughly 500-1000 characters;
    /// 1000 is the cap and the editor shows a live counter well before it.
    static let maximumDescriptionCharacters = 1000

    /// Short single-line answers. A role is "online clothing store owner", not
    /// an essay, and a cap that small keeps the per-request prompt cheap.
    static let maximumRoleCharacters = 120

    /// Free text: who the user is and how they usually communicate. Typed or
    /// dictated. This is the "About me" answer.
    var descriptionText: String

    /// What the user does for a living, in their words: "online clothing store
    /// owner", "интернет-маркетолог", "дизайнер".
    var role: String

    /// What the user provides, and the rules that hold across every
    /// conversation. Per-template context is layered on top of this, never
    /// instead of it.
    var business: BusinessContext

    /// Default register, used when a template does not override it.
    var preferredTone: ReplyTone

    /// Which conversation kinds the user said they care about. Used to preselect
    /// which templates appear in the keyboard bar, never sent to the model.
    var activeRelationships: Set<RelationshipKind>

    var workingHours: WorkingHours

    var hasCompletedOnboarding: Bool

    /// The language replies should be written in. nil - the default - means
    /// "the language of the incoming message", which is what most people want
    /// most of the time.
    var replyLanguage: ReplyLanguagePreference?

    static let empty = UserProfile(
        descriptionText: "",
        role: "",
        business: .empty,
        preferredTone: .natural,
        activeRelationships: Set(RelationshipKind.builtIns),
        workingHours: .default,
        hasCompletedOnboarding: false
    )

    mutating func setRole(_ value: String) {
        role = value.unicodeScalars.count <= Self.maximumRoleCharacters
            ? value
            : String(value.prefix(Self.maximumRoleCharacters))
    }

    /// Clamped by scalar count, matching the backend's own counting so the two
    /// limits mean the same thing.
    mutating func setDescription(_ value: String) {
        let trimmed = value
        if trimmed.unicodeScalars.count <= Self.maximumDescriptionCharacters {
            descriptionText = trimmed
        } else {
            descriptionText = String(trimmed.prefix(Self.maximumDescriptionCharacters))
        }
    }

    /// What actually goes in an AI request. Trimmed, never the whole struct.
    var promptDescription: String {
        descriptionText.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    // Decoding tolerates a profile written by an older build that had fewer
    // fields, so an app update never wipes what the user typed.
    /// True once the user has told us anything at all about themselves. Used
    /// to decide whether the home screen should still be nudging them to.
    var hasAnyContext: Bool {
        !promptDescription.isEmpty
            || !role.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            || !business.isEmpty
    }

    enum CodingKeys: String, CodingKey {
        case descriptionText
        case role
        case business
        case preferredTone
        case activeRelationships
        case workingHours
        case hasCompletedOnboarding
        case replyLanguage
    }

    init(
        descriptionText: String,
        role: String = "",
        business: BusinessContext = .empty,
        preferredTone: ReplyTone,
        activeRelationships: Set<RelationshipKind>,
        workingHours: WorkingHours,
        hasCompletedOnboarding: Bool,
        replyLanguage: ReplyLanguagePreference? = nil
    ) {
        self.descriptionText = descriptionText
        self.role = role
        self.business = business
        self.preferredTone = preferredTone
        self.activeRelationships = activeRelationships
        self.workingHours = workingHours
        self.hasCompletedOnboarding = hasCompletedOnboarding
        self.replyLanguage = replyLanguage
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        descriptionText = try container.decodeIfPresent(String.self, forKey: .descriptionText) ?? ""
        role = try container.decodeIfPresent(String.self, forKey: .role) ?? ""
        business = try container.decodeIfPresent(BusinessContext.self, forKey: .business) ?? .empty
        preferredTone = try container.decodeIfPresent(ReplyTone.self, forKey: .preferredTone) ?? .natural
        activeRelationships = try container.decodeIfPresent(Set<RelationshipKind>.self, forKey: .activeRelationships)
            ?? Set(RelationshipKind.builtIns)
        workingHours = try container.decodeIfPresent(WorkingHours.self, forKey: .workingHours) ?? .default
        hasCompletedOnboarding = try container.decodeIfPresent(Bool.self, forKey: .hasCompletedOnboarding) ?? false
        // A value this build does not know (a newer app wrote it) reads as
        // "follow the message" rather than failing the whole profile.
        replyLanguage = (try? container.decodeIfPresent(ReplyLanguagePreference.self, forKey: .replyLanguage)) ?? nil
    }
}

/// A fixed reply language. Structured on purpose: the server accepts only
/// these codes and ignores anything else, so a preference can never smuggle
/// free text into the prompt.
enum ReplyLanguagePreference: String, Codable, CaseIterable, Identifiable, Sendable {
    case kazakh = "kk"
    case russian = "ru"
    case english = "en"
    case uzbek = "uz"

    var id: String { rawValue }
}
