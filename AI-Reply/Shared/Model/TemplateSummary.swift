import Foundation

/// The few bytes the keyboard needs to DRAW the template row, and nothing else.
///
/// WHY THIS EXISTS. The keyboard's first frame used to be seeded from
/// `ReplyConfiguration.initial`, i.e. the four default built-ins, because the
/// real configuration is a file read that must not happen on the main thread
/// during appearance. A user who renamed Client, hid Work or added Supplier
/// therefore saw the wrong chips for one frame and then a visible swap.
///
/// The fix is not to read the file sooner. It is to keep a summary small enough
/// that reading it synchronously is free: identifiers, the three localized
/// names, and order. No instructions, no business context, no rules, no profile
/// text - none of which the row needs, and all of which the full configuration
/// still carries for the request that generation actually makes.
struct TemplateSummary: Codable, Hashable, Sendable {

    let id: String
    /// Name per app language code ("en" / "ru" / "kk"). Stored resolved rather
    /// than as a relationship, so the keyboard never has to know how a built-in
    /// is spelled in Kazakh.
    let names: [String: String]

    func name(for language: AppLanguage) -> String {
        names[language.rawValue] ?? names["en"] ?? id
    }

    /// The kind the chip's icon is drawn for. A built-in's identifier is its
    /// relationship; anything else is a template the user made.
    var relationship: RelationshipKind {
        RelationshipKind.builtIns.first { $0.rawValue == id } ?? .custom
    }

    init(template: ReplyTemplate) {
        self.id = template.id
        self.names = Dictionary(
            uniqueKeysWithValues: AppLanguage.allCases.map {
                ($0.rawValue, template.displayName(appLanguage: $0))
            }
        )
    }

    init(id: String, names: [String: String]) {
        self.id = id
        self.names = names
    }
}

extension SharedSettings {

    private static let templateSummariesKey = "shared.templateSummaries"
    private static let templateSummariesVersionKey = "shared.templateSummariesVersion"

    /// Raised whenever what a summary holds changes without the user saving
    /// anything: version 2 renamed Business and Work to Partner and
    /// Colleague. The names are stored resolved, so an older summary would
    /// show the old ones on the keyboard's first frame.
    static let templateSummariesVersion = 2

    /// The visible templates, in bar order, as the app last saved them.
    ///
    /// Returns nil when nothing has been written yet, which is the honest
    /// answer for a fresh install: the caller then falls back to the defaults
    /// rather than showing an empty row.
    var templateSummaries: [TemplateSummary]? {
        guard let data = defaults.data(forKey: Self.templateSummariesKey),
              let decoded = try? JSONDecoder().decode([TemplateSummary].self, from: data),
              !decoded.isEmpty else { return nil }
        return decoded
    }

    /// Written by the app whenever the configuration is saved. A few hundred
    /// bytes of property list, not a serialized database - the full
    /// configuration stays in its own file in the App Group container.
    func setTemplateSummaries(_ summaries: [TemplateSummary]) {
        guard let data = try? JSONEncoder().encode(summaries) else { return }
        defaults.set(data, forKey: Self.templateSummariesKey)
        defaults.set(Self.templateSummariesVersion, forKey: Self.templateSummariesVersionKey)
    }

    /// Writes the summaries again, once, when the stored ones were made by an
    /// older version - called by the app at launch, so the keyboard's first
    /// frame after an update already shows the current names. Returns
    /// whether it wrote.
    @discardableResult
    func refreshTemplateSummariesIfOutdated(from configuration: ReplyConfiguration) -> Bool {
        guard defaults.integer(forKey: Self.templateSummariesVersionKey) < Self.templateSummariesVersion else {
            return false
        }
        setTemplateSummaries(configuration.visibleTemplates.map(TemplateSummary.init(template:)))
        return true
    }
}
