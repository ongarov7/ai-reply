import Foundation

/// Reads each language's dictionaries once, off the main thread, and hands
/// out ready engines.
///
/// Typing never waits for a dictionary: until a language is loaded,
/// `engine(for:)` returns nil (and starts the load), and the keyboard shows no
/// suggestions. Loaded word lists and filters are shared between the layouts
/// that need them - the Kazakh engine reuses the Russian ones.
final class AutocorrectLoader: @unchecked Sendable {

    let directory: URL

    private let queue = DispatchQueue(label: "kz.ai-reply.autocorrect.loader", qos: .utility)
    private let lock = NSLock()

    /// Guarded by `lock`.
    private var engines: [KeyboardLanguage: AutocorrectEngine] = [:]
    private var loading: Set<KeyboardLanguage> = []
    /// Languages whose files are missing or damaged: not retried on every key.
    private var failed: Set<KeyboardLanguage> = []

    /// Only touched on `queue`.
    private var wordLists: [KeyboardLanguage: WordList] = [:]
    private var filters: [KeyboardLanguage: KnownWordsFilter] = [:]

    /// `directory` holds `<lang>.words` and `<lang>.known`.
    init(directory: URL) {
        self.directory = directory
    }

    /// Where the keyboard bundle keeps the dictionaries, or nil in a target
    /// that does not ship them (the containing app).
    static func bundledDirectory(in bundle: Bundle = .main) -> URL? {
        let words = bundle.url(forResource: "en", withExtension: "words")
            ?? bundle.url(forResource: "en", withExtension: "words", subdirectory: "Dictionaries")
        return words?.deletingLastPathComponent()
    }

    /// The engine for `language` when its dictionaries are in memory;
    /// otherwise nil, and they start loading in the background.
    func engine(for language: KeyboardLanguage) -> AutocorrectEngine? {
        lock.lock()
        defer { lock.unlock() }
        if let engine = engines[language] { return engine }
        startLoading(language)
        return nil
    }

    /// Starts loading `language` in the background - e.g. when the keyboard
    /// appears or switches layout, ahead of the first key.
    func prepare(_ language: KeyboardLanguage) {
        lock.lock()
        startLoading(language)
        lock.unlock()
    }

    /// Loads `language` and waits for it. Never from the main thread.
    func loadEngine(for language: KeyboardLanguage) throws -> AutocorrectEngine {
        try queue.sync { try build(language) }
    }

    /// Call with `lock` held.
    private func startLoading(_ language: KeyboardLanguage) {
        guard engines[language] == nil, !loading.contains(language), !failed.contains(language) else { return }
        loading.insert(language)
        queue.async { [self] in
            let succeeded: Bool
            do {
                _ = try build(language)
                succeeded = true
            } catch {
                // Autocorrect is simply off for this layout; typing is unaffected.
                ReplyLog.event("autocorrect: \(language.rawValue) dictionaries unavailable (\(error))")
                succeeded = false
            }
            lock.lock()
            loading.remove(language)
            if !succeeded { failed.insert(language) }
            lock.unlock()
        }
    }

    /// On `queue`.
    private func build(_ language: KeyboardLanguage) throws -> AutocorrectEngine {
        lock.lock()
        let existing = engines[language]
        lock.unlock()
        if let existing { return existing }

        let engine = AutocorrectEngine(
            language: language,
            words: try wordList(language),
            filters: try AutocorrectEngine.knownLanguages(for: language).map(filter),
            otherWords: try AutocorrectEngine.otherWordLanguages(for: language).map(wordList),
            proximity: KeyProximity(layout: language)
        )
        lock.lock()
        engines[language] = engine
        lock.unlock()
        return engine
    }

    private func wordList(_ language: KeyboardLanguage) throws -> WordList {
        if let cached = wordLists[language] { return cached }
        let list = try WordList(contentsOf: directory.appendingPathComponent("\(language.rawValue).words"))
        wordLists[language] = list
        return list
    }

    private func filter(_ language: KeyboardLanguage) throws -> KnownWordsFilter {
        if let cached = filters[language] { return cached }
        let filter = try KnownWordsFilter(contentsOf: directory.appendingPathComponent("\(language.rawValue).known"))
        filters[language] = filter
        return filter
    }
}
