import Foundation
import Observation
import AirReplyCore
import AirReplySystem
import AirReplyVoice

/// Where the quick panel appears.
public enum PanelPlacement: String, Codable, CaseIterable, Sendable {
    /// Upper third of the screen under the pointer, like Spotlight.
    case top
    /// Just above the Dock of the screen under the pointer.
    case bottom
    /// Where the user last dragged it.
    case remembered
}

public enum AppearancePreference: String, Codable, CaseIterable, Sendable {
    case system
    case light
    case dark
}

/// Local preferences of this Mac (UserDefaults). Account data lives on the server.
@MainActor
@Observable
public final class AppSettings {
    private let defaults: UserDefaults

    public var shortcut: KeyCombo { didSet { save(shortcut, Key.shortcut) } }
    public var placement: PanelPlacement { didSet { defaults.set(placement.rawValue, forKey: Key.placement) } }
    /// Panel origin relative to the visible frame of its screen (0…1), for `.remembered`.
    public var rememberedPosition: CGPoint? { didSet { save(rememberedPosition, Key.rememberedPosition) } }
    public var appearance: AppearancePreference { didSet { defaults.set(appearance.rawValue, forKey: Key.appearance) } }
    /// Show text copied in another app as "reply to" when the panel opens.
    public var offerCopiedText: Bool { didSet { defaults.set(offerCopiedText, forKey: Key.offerCopiedText) } }
    /// Hide the panel right after "Copy", so ⌘V goes straight into the messenger.
    public var closeAfterCopy: Bool { didSet { defaults.set(closeAfterCopy, forKey: Key.closeAfterCopy) } }
    /// Post ⌘V into the previous app after copying (needs the event-posting permission). Off by default.
    public var pasteAfterCopy: Bool { didSet { defaults.set(pasteAfterCopy, forKey: Key.pasteAfterCopy) } }
    public var dictationLanguage: DictationLanguage { didSet { defaults.set(dictationLanguage.rawValue, forKey: Key.dictationLanguage) } }
    /// Recipient code last used for compose (server vocabulary, e.g. "client").
    public var lastRecipient: String { didSet { defaults.set(lastRecipient, forKey: Key.lastRecipient) } }

    public init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        shortcut = Self.load(KeyCombo.self, defaults, Key.shortcut) ?? .optionSpace
        placement = defaults.string(forKey: Key.placement).flatMap(PanelPlacement.init(rawValue:)) ?? .top
        rememberedPosition = Self.load(CGPoint.self, defaults, Key.rememberedPosition)
        appearance = defaults.string(forKey: Key.appearance).flatMap(AppearancePreference.init(rawValue:)) ?? .system
        offerCopiedText = defaults.object(forKey: Key.offerCopiedText) as? Bool ?? true
        closeAfterCopy = defaults.object(forKey: Key.closeAfterCopy) as? Bool ?? true
        pasteAfterCopy = defaults.object(forKey: Key.pasteAfterCopy) as? Bool ?? false
        dictationLanguage = defaults.string(forKey: Key.dictationLanguage).flatMap(DictationLanguage.init(rawValue:)) ?? .auto
        lastRecipient = defaults.string(forKey: Key.lastRecipient) ?? "client"
    }

    private enum Key {
        static let shortcut = "settings.shortcut"
        static let placement = "settings.placement"
        static let rememberedPosition = "settings.rememberedPosition"
        static let appearance = "settings.appearance"
        static let offerCopiedText = "settings.offerCopiedText"
        static let closeAfterCopy = "settings.closeAfterCopy"
        static let pasteAfterCopy = "settings.pasteAfterCopy"
        static let dictationLanguage = "settings.dictationLanguage"
        static let lastRecipient = "settings.lastRecipient"
    }

    private func save<T: Encodable>(_ value: T?, _ key: String) {
        guard let value, let data = try? JSONEncoder().encode(value) else {
            defaults.removeObject(forKey: key)
            return
        }
        defaults.set(data, forKey: key)
    }

    private static func load<T: Decodable>(_ type: T.Type, _ defaults: UserDefaults, _ key: String) -> T? {
        defaults.data(forKey: key).flatMap { try? JSONDecoder().decode(T.self, from: $0) }
    }
}
