import Foundation

/// Words the user taught the keyboard on one layout - by taking a correction
/// back, or by tapping what they typed in the suggestion strip. A learned word
/// counts as known and is never corrected again.
///
/// At most `capacity` per language, the most recently used kept. Stored only
/// on this device (see `LearnedWordsStore`): never synced, never sent.
struct LearnedWords: Equatable, Sendable {

    static let capacity = 500

    let language: KeyboardLanguage
    /// Lowercased, oldest first.
    private(set) var words: [String]
    private var members: Set<String>

    /// `words` oldest first, as stored. Duplicates keep their latest place and
    /// only the newest `capacity` survive.
    init(language: KeyboardLanguage, words: [String] = []) {
        self.language = language
        self.words = []
        self.members = []
        for word in words { learn(word) }
    }

    func contains(_ word: String) -> Bool {
        members.contains(WordList.key(for: word))
    }

    mutating func learn(_ word: String) {
        let key = WordList.key(for: word)
        guard key.contains(where: \.isLetter) else { return }
        if members.contains(key) {
            words.removeAll { $0 == key }
        } else {
            members.insert(key)
        }
        words.append(key)
        if words.count > Self.capacity {
            members.remove(words.removeFirst())
        }
    }
}

// MARK: - Persistence

/// Where learned words live between keyboard sessions. The keyboard backs it
/// with the App Group defaults; tests use their own suite.
protocol LearnedWordsStore: Sendable {
    func learnedWords(for language: KeyboardLanguage) -> [String]
    func saveLearnedWords(_ words: [String], for language: KeyboardLanguage)
    /// Changes when every learned word was removed (the account was deleted
    /// in the app), so a running keyboard drops what it still holds instead
    /// of writing it back.
    var resetStamp: Double { get }
}

extension LearnedWordsStore {
    var resetStamp: Double { 0 }
}

/// Learned words as a string array per language under
/// `autocorrect.learned.<lang>`. `UserDefaults` is thread-safe, so the
/// keyboard reads and writes it off the main thread.
struct DefaultsLearnedWordsStore: LearnedWordsStore, @unchecked Sendable {

    let defaults: UserDefaults

    init(defaults: UserDefaults = AppGroup.defaults) {
        self.defaults = defaults
    }

    static func key(for language: KeyboardLanguage) -> String {
        "autocorrect.learned.\(language.rawValue)"
    }

    func learnedWords(for language: KeyboardLanguage) -> [String] {
        defaults.stringArray(forKey: Self.key(for: language)) ?? []
    }

    func saveLearnedWords(_ words: [String], for language: KeyboardLanguage) {
        defaults.set(words, forKey: Self.key(for: language))
    }

    static let resetStampKey = "autocorrect.learned.resetAt"

    var resetStamp: Double { defaults.double(forKey: Self.resetStampKey) }

    /// Forgets the learned words of every layout.
    func removeAll(now: Date = Date()) {
        for language in KeyboardLanguage.allCases {
            defaults.removeObject(forKey: Self.key(for: language))
        }
        defaults.set(now.timeIntervalSince1970, forKey: Self.resetStampKey)
    }
}

extension LearnedWords {

    init(language: KeyboardLanguage, store: LearnedWordsStore) {
        self.init(language: language, words: store.learnedWords(for: language))
    }

    func save(to store: LearnedWordsStore) {
        store.saveLearnedWords(words, for: language)
    }
}
