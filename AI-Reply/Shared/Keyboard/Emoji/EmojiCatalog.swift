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
/// Stored on this device only, in the keyboard's own container
/// (`FileRecentEmojiStore`): never the App Group, never a backup, never sent
/// anywhere. It also works without Full Access, when the App Group cannot be
/// written.
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

/// A small file in the process's own Application Support - in the keyboard,
/// the extension's container, which it can write without Full Access.
///
/// Not `UserDefaults.standard`: that plist is part of the extension's
/// backup domain, and the recents are something typed (contract §3.10: on
/// the device only, no App Group, no backup). The file is marked excluded
/// from backup after every write, because an atomic write puts a new file in
/// place and the mark does not carry over; its folder is marked too. Not
/// Caches either: iOS empties
/// those when storage runs low, and the recents would vanish for no reason
/// the user can see. Tests pass a directory of their own.
struct FileRecentEmojiStore: RecentEmojiStore {

    static let fileName = "recent-emoji.json"

    /// Where the keyboard keeps them: this process's Application Support,
    /// never the App Group container.
    static var defaultDirectory: URL {
        URL.applicationSupportDirectory.appending(path: "Keyboard", directoryHint: .isDirectory)
    }

    let fileURL: URL

    init(directory: URL = Self.defaultDirectory) {
        fileURL = directory.appending(path: Self.fileName, directoryHint: .notDirectory)
    }

    func load() -> [String] {
        guard let data = try? Data(contentsOf: fileURL),
              let items = try? JSONDecoder().decode([String].self, from: data) else { return [] }
        return RecentEmoji(items: items).items
    }

    func save(_ items: [String]) {
        guard let data = try? JSONEncoder().encode(Array(items.prefix(RecentEmoji.capacity))) else { return }
        let directory = fileURL.deletingLastPathComponent()
        do {
            // The folder first: a folder out of backups keeps what is in it
            // out too, so the new file is never in a backup, not even
            // between the write and its own mark.
            try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
            try Self.excludeFromBackup(directory)
            try data.write(to: fileURL, options: .atomic)
            try Self.excludeFromBackup(fileURL)
        } catch {
            // A lost write costs nothing the user typed, only the order of
            // the recents the next time the keyboard starts.
        }
    }

    private static func excludeFromBackup(_ url: URL) throws {
        var values = URLResourceValues()
        values.isExcludedFromBackup = true
        var url = url
        try url.setResourceValues(values)
    }
}

/// Saves the recents off the main thread: in order, and only the newest.
///
/// Every tap on an emoji saves the whole list. On a concurrent queue an older
/// list could be written after a newer one, and the keyboard would start next
/// time without the last emoji typed. Here one serial queue writes, and a
/// list asked for while an earlier one is still waiting replaces it: a quick
/// run of taps costs one or two writes, and the newest list is always the one
/// that stays.
///
/// One per process (`keyboard`): iOS can make a new keyboard view controller
/// for every appearance in the same process, and the new one must read what
/// the old one was still saving.
final class RecentEmojiWriter: @unchecked Sendable {

    /// The keyboard's own: its container, out of backups.
    static let keyboard = RecentEmojiWriter(store: FileRecentEmojiStore())

    let store: RecentEmojiStore
    private let queue = DispatchQueue(label: "kz.ai-reply.keyboard.recent-emoji", qos: .utility)
    private let lock = NSLock()
    /// The list waiting for the queue. Set means a write is scheduled that
    /// has not taken it yet. Guarded by `lock`.
    private var pending: [String]?

    init(store: RecentEmojiStore) {
        self.store = store
    }

    /// The saved recents, counting a save still on its way.
    func load() -> [String] {
        lock.lock()
        let waiting = pending
        lock.unlock()
        if let waiting { return RecentEmoji(items: waiting).items }
        // A write that already took its list finishes first.
        return queue.sync { store.load() }
    }

    func save(_ items: [String]) {
        lock.lock()
        let isScheduled = pending != nil
        pending = items
        lock.unlock()
        guard !isScheduled else { return }
        queue.async { [self] in
            lock.lock()
            let newest = pending
            pending = nil
            lock.unlock()
            if let newest { store.save(newest) }
        }
    }

    /// Returns once every save asked for so far is written. For tests.
    func waitUntilSaved() {
        queue.sync {}
    }
}
