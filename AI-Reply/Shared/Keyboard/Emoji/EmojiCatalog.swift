import Foundation

// MARK: - Categories

/// The categories of the shared emoji catalog (`emoji-catalog.txt`), in its
/// order. The raw values are the file's own `@<category_id>` lines.
enum EmojiCategory: String, CaseIterable, Sendable {
    case smileysEmotion = "smileys_emotion"
    case peopleBody = "people_body"
    case animalsNature = "animals_nature"
    case foodDrink = "food_drink"
    case travelPlaces = "travel_places"
    case activities
    case objects
    case symbols
    case flags

    /// The category bar's icon. Each exists on iOS 17.0 (checked against the
    /// SF Symbols availability table).
    var symbolName: String {
        switch self {
        case .smileysEmotion: return "face.smiling"
        case .peopleBody:     return "hand.wave"
        case .animalsNature:  return "teddybear"
        case .foodDrink:      return "fork.knife"
        case .travelPlaces:   return "car"
        case .activities:     return "basketball"
        case .objects:        return "lightbulb"
        case .symbols:        return "heart"
        case .flags:          return "flag"
        }
    }
}

/// One stretch of the emoji page: the recently used ones, or a category.
struct EmojiSection: Equatable, Sendable {

    enum Kind: Hashable, Sendable {
        case recents
        case category(EmojiCategory)

        var symbolName: String {
            switch self {
            case .recents: return "clock"
            case .category(let category): return category.symbolName
            }
        }
    }

    let kind: Kind
    let emoji: [String]
}

// MARK: - Versions

/// An Emoji version ("15.1"). The catalog says which version each entry
/// needs, and the system font draws only what its iOS version knows: anything
/// newer would show as an empty box.
struct EmojiVersion: Comparable, Hashable, Sendable {
    let major: Int
    let minor: Int

    init(_ major: Int, _ minor: Int = 0) {
        self.major = major
        self.minor = minor
    }

    /// "15.1", "0.6". Anything else is nil.
    init?(_ text: Substring) {
        let parts = text.split(separator: ".", omittingEmptySubsequences: false)
        guard parts.count == 2, let major = Int(parts[0]), let minor = Int(parts[1]), major >= 0, minor >= 0 else {
            return nil
        }
        self.init(major, minor)
    }

    static func < (lhs: EmojiVersion, rhs: EmojiVersion) -> Bool {
        (lhs.major, lhs.minor) < (rhs.major, rhs.minor)
    }

    /// The newest emoji this iOS draws: Emoji 15.0 on iOS 17.0-17.3, 15.1
    /// from iOS 17.4. The catalog stops at 15.1, so nothing newer is needed.
    static func supported(by os: OperatingSystemVersion) -> EmojiVersion {
        if os.majorVersion > 17 || (os.majorVersion == 17 && os.minorVersion >= 4) {
            return EmojiVersion(15, 1)
        }
        return EmojiVersion(15, 0)
    }
}

// MARK: - Catalog

/// The shared emoji list, parsed.
///
/// Эмодзи тізімі: санаттар мен әр эмодзидің нұсқасы, екі пернетақтаға ортақ.
///
/// The file is generated (`tools/emoji`) and byte-identical on both
/// platforms: `@<category_id>` starts a category, every other line is
/// `<emoji>\t<version>`. The parser is tolerant on purpose - an unknown
/// category or a malformed line is skipped, not fatal - because the keyboard
/// must never fail to open over a data file.
struct EmojiCatalog: Equatable, Sendable {

    struct Entry: Equatable, Sendable {
        let emoji: String
        let version: EmojiVersion
    }

    struct Group: Equatable, Sendable {
        let category: EmojiCategory
        var entries: [Entry]
    }

    /// Every category the file had, in the file's order.
    let categories: [Group]

    static func parse(_ text: String) -> EmojiCatalog {
        var result: [Group] = []
        var current: EmojiCategory?
        var seen = Set<String>()
        for line in text.split(whereSeparator: \.isNewline) {
            if line.hasPrefix("@") {
                current = EmojiCategory(rawValue: String(line.dropFirst()).trimmingCharacters(in: .whitespaces))
                if let current, !result.contains(where: { $0.category == current }) {
                    result.append(Group(category: current, entries: []))
                }
                continue
            }
            guard let current, let index = result.firstIndex(where: { $0.category == current }) else { continue }
            let fields = line.split(separator: "\t", omittingEmptySubsequences: false)
            guard fields.count == 2, !fields[0].isEmpty, let version = EmojiVersion(fields[1]) else { continue }
            let emoji = String(fields[0])
            // One grapheme per entry, each once: a broken line must not put
            // two emoji into one cell or one emoji into two.
            guard emoji.count == 1, seen.insert(emoji).inserted else { continue }
            result[index].entries.append(Entry(emoji: emoji, version: version))
        }
        return EmojiCatalog(categories: result)
    }

    /// The categories with only what this system draws, empty ones left out.
    func sections(supporting version: EmojiVersion) -> [EmojiSection] {
        categories.compactMap { group in
            let emoji = group.entries.filter { $0.version <= version }.map(\.emoji)
            return emoji.isEmpty ? nil : EmojiSection(kind: .category(group.category), emoji: emoji)
        }
    }

    /// The page: recents first when there are any, then the categories.
    func page(recents: [String], supporting version: EmojiVersion) -> [EmojiSection] {
        let categories = sections(supporting: version)
        guard !recents.isEmpty else { return categories }
        return [EmojiSection(kind: .recents, emoji: recents)] + categories
    }

    /// The copy shipped with the keyboard, read from its bundle. nil when the
    /// file is missing or unreadable: the page then has no categories, and
    /// nothing else breaks.
    static func bundled(in bundle: Bundle = .main) -> EmojiCatalog? {
        guard let url = bundle.url(forResource: "emoji-catalog", withExtension: "txt"),
              let text = try? String(contentsOf: url, encoding: .utf8) else { return nil }
        return parse(text)
    }
}

// MARK: - Recents

/// The emoji used last, most recent first.
///
/// Stored on this device only, in the keyboard's own container: never the
/// App Group, never a backup of anything typed, never sent anywhere. It also
/// works without Full Access, when the App Group cannot be written.
struct RecentEmoji: Equatable, Sendable {

    static let capacity = 30

    private(set) var items: [String]

    init(items: [String] = []) {
        var seen = Set<String>()
        self.items = Array(items.filter { !$0.isEmpty && seen.insert($0).inserted }.prefix(Self.capacity))
    }

    /// Moves `emoji` to the front, once, and drops the oldest past capacity.
    mutating func record(_ emoji: String) {
        guard !emoji.isEmpty else { return }
        items.removeAll { $0 == emoji }
        items.insert(emoji, at: 0)
        if items.count > Self.capacity { items.removeLast(items.count - Self.capacity) }
    }
}

protocol RecentEmojiStore: Sendable {
    func load() -> [String]
    func save(_ items: [String])
}

/// `UserDefaults.standard` of the process: the keyboard extension's own
/// container. Tests pass a suite.
struct DefaultsRecentEmojiStore: RecentEmojiStore, @unchecked Sendable {

    static let key = "keyboard.recentEmoji"

    private let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    func load() -> [String] {
        RecentEmoji(items: defaults.stringArray(forKey: Self.key) ?? []).items
    }

    func save(_ items: [String]) {
        defaults.set(Array(items.prefix(RecentEmoji.capacity)), forKey: Self.key)
    }
}
