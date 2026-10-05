import Foundation
import Observation
import SwiftUI

/// App-level preferences, backed by the shared container so the keyboard
/// extension reads the same values.
///
/// Setters are explicit rather than `didSet` observers: `@Observable` rewrites
/// stored properties, and mixing that with property observers is a subtlety
/// this app has no reason to depend on.
@Observable
final class AppSettings {

    private(set) var appearance: AppearancePreference
    /// `nil` means "follow the system language".
    private(set) var language: AppLanguage?
    /// Layouts the keyboard's ҚАЗ / РУС / ENG key cycles through.
    private(set) var keyboardLanguages: [KeyboardLanguage]
    private(set) var keyboardHaptics: Bool
    /// Local typo fixes and suggestions in the keyboard.
    private(set) var smartCorrection: Bool

    @ObservationIgnored private let store: SharedSettings

    init(store: SharedSettings = .shared) {
        self.store = store
        self.appearance = store.appearance
        self.language = store.appLanguage
        self.keyboardLanguages = KeyboardLanguageStore.enabledLanguages(settings: store)
        self.keyboardHaptics = store.keyboardHapticsEnabled
        self.smartCorrection = store.smartCorrectionEnabled
    }

    /// Turns one layout on or off. The last one cannot be turned off: a
    /// keyboard with no layout could not type.
    func setKeyboardLanguage(_ language: KeyboardLanguage, enabled: Bool) {
        var next = keyboardLanguages.filter { $0 != language }
        if enabled { next.append(language) }
        guard !next.isEmpty else { return }
        KeyboardLanguageStore.setEnabledLanguages(next, settings: store)
        keyboardLanguages = KeyboardLanguageStore.enabledLanguages(settings: store)
    }

    func setKeyboardHaptics(_ enabled: Bool) {
        keyboardHaptics = enabled
        store.setKeyboardHapticsEnabled(enabled)
    }

    func setSmartCorrection(_ enabled: Bool) {
        smartCorrection = enabled
        store.setSmartCorrectionEnabled(enabled)
    }

    func setAppearance(_ value: AppearancePreference) {
        appearance = value
        store.setAppearance(value)
    }

    func setLanguage(_ value: AppLanguage?) {
        language = value
        store.setAppLanguage(value)
    }

    var effectiveLanguage: AppLanguage { language ?? .systemDefault }

    var locale: Locale { Locale(identifier: effectiveLanguage.localeIdentifier) }

    /// Resolves a string in the language the user actually chose.
    ///
    /// `Text("key")` already follows `\.locale` from the environment, but
    /// `String(localized:)` called from code does not: it resolves against the
    /// bundle's own preferred localization, which is the SYSTEM language - and
    /// its `locale:` argument only formats, it does not pick the table. That
    /// difference is invisible until someone with an English phone switches the
    /// app to Kazakh and finds one counter still reading "%d of %d characters"
    /// in English. So the lookup goes to the chosen language's own `.lproj`.
    /// Every string built in code goes through here.
    func localized(_ key: String) -> String {
        Self.bundle(for: effectiveLanguage).localizedString(forKey: key, value: nil, table: nil)
    }

    /// The app's table for one language; the main bundle if it is missing.
    private static func bundle(for language: AppLanguage) -> Bundle {
        Bundle.main.path(forResource: language.rawValue, ofType: "lproj").flatMap(Bundle.init(path:)) ?? .main
    }

    /// `nil` hands the decision back to Settings ▸ Display & Brightness.
    var colorScheme: ColorScheme? {
        switch appearance {
        case .system: return nil
        case .light:  return .light
        case .dark:   return .dark
        }
    }
}

extension AppearancePreference {
    var titleKey: LocalizedStringKey {
        switch self {
        case .system: return "settings.appearance.system"
        case .light:  return "settings.appearance.light"
        case .dark:   return "settings.appearance.dark"
        }
    }
}
