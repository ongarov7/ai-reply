import Foundation

/// How the containing app resolves light and dark mode.
enum AppearancePreference: String, CaseIterable, Sendable {
    case system
    case light
    case dark
}

/// The small amount of NON-SENSITIVE state the containing app and the keyboard
/// extension both need to see.
///
/// App Group defaults are a plain property list in a container both processes
/// can read. Nothing secret belongs here - no tokens, no credentials, no
/// message text. When authentication is added, tokens go in the Keychain with a
/// shared access group instead, and only derived, non-sensitive flags surface
/// here.
struct SharedSettings {

    static let shared = SharedSettings()

    /// Internal rather than private so `TemplateSummary` can add its own
    /// accessors in the file that owns that type, instead of this one growing a
    /// section about something it does not otherwise know about.
    let defaults: UserDefaults

    init(defaults: UserDefaults = AppGroup.defaults) {
        self.defaults = defaults
    }

    private enum Key {
        static let keyboardLanguage = "shared.keyboardLanguage"
        static let appLanguage = "shared.appLanguage"
        static let appearance = "shared.appearance"
        static let keyboardLastSeen = "shared.keyboardLastSeen"
        static let keyboardFullAccess = "shared.keyboardFullAccess"
        static let keyboardHeight = "shared.keyboardHeight"
        static let keyboardWidth = "shared.keyboardWidth"
        static let enabledKeyboardLanguages = "shared.enabledKeyboardLanguages"
        static let lastTemplateID = "shared.lastTemplateID"
        static let keyboardHaptics = "shared.keyboardHaptics"

        /// Where the keyboard stored its layout before the App Group existed.
        /// Read once so an existing install does not silently reset to English.
        static let legacyKeyboardLanguage = "ReplyKeyboard.selectedLanguage"
    }

    // MARK: Keyboard layout

    /// Raw layout code ("en" / "ru" / "kk"). Kept as a string so the app does
    /// not have to compile the keyboard's layout tables just to read it.
    var keyboardLanguageCode: String? {
        if let stored = defaults.string(forKey: Key.keyboardLanguage) {
            return stored
        }
        // One-time migration from the extension's private container.
        if let legacy = UserDefaults.standard.string(forKey: Key.legacyKeyboardLanguage) {
            defaults.set(legacy, forKey: Key.keyboardLanguage)
            return legacy
        }
        return nil
    }

    func setKeyboardLanguageCode(_ code: String) {
        defaults.set(code, forKey: Key.keyboardLanguage)
    }

    /// Layout codes the language key cycles through. nil means "all of them",
    /// which is also what an install from before this setting existed gets.
    var enabledKeyboardLanguageCodes: [String]? {
        defaults.stringArray(forKey: Key.enabledKeyboardLanguages)
    }

    func setEnabledKeyboardLanguageCodes(_ codes: [String]?) {
        if let codes, !codes.isEmpty {
            defaults.set(codes, forKey: Key.enabledKeyboardLanguages)
        } else {
            defaults.removeObject(forKey: Key.enabledKeyboardLanguages)
        }
    }

    /// Key-press haptics. On by default; they only ever play with Full Access,
    /// which is the only way a keyboard extension can use the Taptic Engine.
    var keyboardHapticsEnabled: Bool {
        defaults.object(forKey: Key.keyboardHaptics) as? Bool ?? true
    }

    func setKeyboardHapticsEnabled(_ enabled: Bool) {
        defaults.set(enabled, forKey: Key.keyboardHaptics)
    }

    // MARK: Persona row

    /// The template the user last replied with. Only an identifier - never
    /// anything they wrote - so the row can show it selected next time.
    var lastTemplateID: String? {
        defaults.string(forKey: Key.lastTemplateID)
    }

    func setLastTemplateID(_ id: String?) {
        if let id {
            defaults.set(id, forKey: Key.lastTemplateID)
        } else {
            defaults.removeObject(forKey: Key.lastTemplateID)
        }
    }

    // MARK: App interface

    /// nil means "follow the system language".
    var appLanguage: AppLanguage? {
        guard let raw = defaults.string(forKey: Key.appLanguage) else { return nil }
        return AppLanguage(rawValue: raw)
    }

    func setAppLanguage(_ language: AppLanguage?) {
        if let language {
            defaults.set(language.rawValue, forKey: Key.appLanguage)
        } else {
            defaults.removeObject(forKey: Key.appLanguage)
        }
    }

    /// The language actually in effect right now.
    var effectiveAppLanguage: AppLanguage {
        appLanguage ?? .systemDefault
    }

    var appearance: AppearancePreference {
        guard let raw = defaults.string(forKey: Key.appearance),
              let value = AppearancePreference(rawValue: raw) else { return .system }
        return value
    }

    func setAppearance(_ appearance: AppearancePreference) {
        defaults.set(appearance.rawValue, forKey: Key.appearance)
    }

    // MARK: Keyboard status

    /// When the keyboard extension last actually ran.
    ///
    /// There is no public API that tells the containing app whether its own
    /// keyboard has been enabled in iOS Settings - `UITextInputMode` exposes
    /// languages, not extension identifiers. A timestamp the extension writes
    /// itself is the honest way to answer it: if the keyboard has run, the user
    /// has enabled it. It is never a guess presented as a fact.
    var keyboardLastSeen: Date? {
        let stamp = defaults.double(forKey: Key.keyboardLastSeen)
        return stamp > 0 ? Date(timeIntervalSince1970: stamp) : nil
    }

    /// Full Access as the keyboard last observed it. Only meaningful together
    /// with `keyboardLastSeen`.
    var keyboardHasFullAccess: Bool {
        defaults.bool(forKey: Key.keyboardFullAccess)
    }

    var isKeyboardConfigured: Bool { keyboardLastSeen != nil }

    /// Called by the extension once it is on screen. Writes only a timestamp
    /// and a permission flag - never anything the user typed or copied.
    func markKeyboardActive(hasFullAccess: Bool) {
        defaults.set(Date().timeIntervalSince1970, forKey: Key.keyboardLastSeen)
        defaults.set(hasFullAccess, forKey: Key.keyboardFullAccess)
    }

    // MARK: Keyboard height

    struct KeyboardHeight {
        let height: Double
        let width: Double
    }

    /// The last height the keyboard settled on in its idle state.
    ///
    /// PERFORMANCE. Without this the input view is first laid out at the
    /// system's default height and only resizes once our own height constraint
    /// is installed during the first layout pass - a visible jump every single
    /// time the user switches to this keyboard. Seeding the constraint from the
    /// cached value in `viewDidLoad` makes the very first frame the right size.
    ///
    /// A stale value is harmless: the first real layout recomputes the height
    /// from the live width and corrects it.
    var keyboardHeight: KeyboardHeight? {
        let height = defaults.double(forKey: Key.keyboardHeight)
        let width = defaults.double(forKey: Key.keyboardWidth)
        guard height > 0, width > 0 else { return nil }
        return KeyboardHeight(height: height, width: width)
    }

    func setKeyboardHeight(_ height: Double, width: Double) {
        guard height > 0, width > 0 else { return }
        defaults.set(height, forKey: Key.keyboardHeight)
        defaults.set(width, forKey: Key.keyboardWidth)
    }
}
