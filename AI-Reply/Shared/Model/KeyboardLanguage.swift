import Foundation

/// The three layouts this keyboard ships. The selected value drives the
/// character keys and the key captions; product labels follow the APP language
/// instead (see `AIReplyStrings`).
///
/// The character rows themselves live in `KeyboardLayout`, next to the numbers
/// and symbols planes, so every layout is described - and tested - in one place.
enum KeyboardLanguage: String, CaseIterable, Hashable, Sendable {
    case english = "en"
    case russian = "ru"
    case kazakh = "kk"

    /// The order the language key cycles through and the settings list shows.
    /// Kazakh first: it is the layout this keyboard exists for.
    static let cycleOrder: [KeyboardLanguage] = [.kazakh, .russian, .english]

    /// The next layout among `enabled`, in `cycleOrder`. A language that is not
    /// enabled (the user just turned it off in the app) still moves forward
    /// rather than getting stuck.
    func next(in enabled: [KeyboardLanguage]) -> KeyboardLanguage {
        let usable = Self.cycleOrder.filter { enabled.contains($0) }
        guard !usable.isEmpty else { return self }
        guard let index = usable.firstIndex(of: self) else {
            // Current layout was disabled: move to the first enabled one after it.
            let order = Self.cycleOrder
            let start = order.firstIndex(of: self) ?? 0
            for step in 1...order.count {
                let candidate = order[(start + step) % order.count]
                if usable.contains(candidate) { return candidate }
            }
            return usable[0]
        }
        return usable[(index + 1) % usable.count]
    }

    /// Kept for callers that do not care about the enabled set.
    var next: KeyboardLanguage { next(in: Self.cycleOrder) }

    /// The layout's own name, never translated into another language: the
    /// space bar flashes it after a switch, the way iOS does.
    var nativeName: String {
        switch self {
        case .english: return "English"
        case .russian: return "Русский"
        case .kazakh:  return "Қазақша"
        }
    }
}

// MARK: - Persistence

/// Remembers the chosen layout, and which layouts are switched on, between
/// keyboard sessions.
///
/// Stored in the App Group so the containing app can show and change both.
enum KeyboardLanguageStore {

    static func load(settings: SharedSettings = .shared) -> KeyboardLanguage {
        let enabled = enabledLanguages(settings: settings)
        if let raw = settings.keyboardLanguageCode,
           let language = KeyboardLanguage(rawValue: raw),
           enabled.contains(language) {
            return language
        }
        // First run: start on the layout that matches the app's language.
        let preferred = settings.effectiveAppLanguage.keyboardLanguage
        return enabled.contains(preferred) ? preferred : (enabled.first ?? .kazakh)
    }

    static func save(_ language: KeyboardLanguage, settings: SharedSettings = .shared) {
        settings.setKeyboardLanguageCode(language.rawValue)
    }

    static func saveAsync(_ language: KeyboardLanguage) {
        DispatchQueue.global(qos: .utility).async {
            save(language)
        }
    }

    /// Layouts the language key cycles through, in `cycleOrder`. Never empty:
    /// a keyboard with no layout cannot type, so an empty or unreadable value
    /// means "all of them".
    static func enabledLanguages(settings: SharedSettings = .shared) -> [KeyboardLanguage] {
        let codes = Set(settings.enabledKeyboardLanguageCodes ?? [])
        let enabled = KeyboardLanguage.cycleOrder.filter { codes.contains($0.rawValue) }
        return enabled.isEmpty ? KeyboardLanguage.cycleOrder : enabled
    }

    static func setEnabledLanguages(_ languages: [KeyboardLanguage], settings: SharedSettings = .shared) {
        let ordered = KeyboardLanguage.cycleOrder.filter { languages.contains($0) }
        settings.setEnabledKeyboardLanguageCodes(ordered.isEmpty ? nil : ordered.map(\.rawValue))
    }
}
