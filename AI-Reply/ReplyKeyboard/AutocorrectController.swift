import UIKit

protocol AutocorrectControllerDelegate: AnyObject {
    /// New strip contents for the word being typed; empty hides the strip.
    func autocorrect(_ controller: AutocorrectController, didUpdate suggestions: [AutocorrectSuggestion])
}

/// Smart correction for the keyboard: which word is being typed, what the
/// strip shows for it, what a separator turns it into, and how a backspace
/// takes that back (DESIGN §5.5, §7).
///
/// Everything here runs on the main thread except the dictionary work, which
/// goes to one serial queue; only the newest request is ever shown. Typing
/// never waits: until a layout's dictionaries are loaded, and whenever an
/// answer is not ready yet, the keyboard simply suggests nothing.
///
/// Nothing typed leaves the device or reaches a log. Learned words stay in the
/// App Group on this phone.
@MainActor
final class AutocorrectController {

    weak var delegate: AutocorrectControllerDelegate?

    /// One per keyboard process: the dictionaries outlive the view controller.
    private static let loader: AutocorrectLoader? = AutocorrectLoader.bundledDirectory().map(AutocorrectLoader.init)

    private let queue = DispatchQueue(label: "kz.ai-reply.autocorrect.suggestions", qos: .userInitiated)
    private let latestTicket = TicketBox()
    private let store: LearnedWordsStore

    private(set) var language: KeyboardLanguage
    /// The user's words, the corrections taken back, the system lexicon.
    /// Owned here on the main thread; each query gets a copy.
    private var session: AutocorrectSession
    /// The smart-correction setting, read when the keyboard appears.
    private var isSettingOn = true
    /// On screen: dictionaries are only touched from `viewDidAppear` on, so
    /// the first frame never shares the CPU with them.
    private var isVisible = false

    private var isActive: Bool { isSettingOn && isVisible }

    private var ticket = 0
    /// Whether the last strip handed to the delegate had anything in it.
    private var showsSuggestions = false
    private var requested: TypedWord?
    private var latest: (word: TypedWord, suggestions: AutocorrectSuggestions)?
    /// A correction just made; the very next key decides whether it stays.
    private var applied: AppliedAutocorrection?

    private lazy var checker = UITextChecker()
    private var lexiconRequested = false

    init(language: KeyboardLanguage, store: LearnedWordsStore = DefaultsLearnedWordsStore()) {
        self.language = language
        self.store = store
        self.session = AutocorrectSession(learned: LearnedWords(language: language))
    }

    // MARK: Lifecycle

    /// The keyboard is about to show: the setting is read now, never on a key.
    func keyboardWillAppear(settingEnabled: Bool, language: KeyboardLanguage) {
        isSettingOn = settingEnabled
        isVisible = false
        lexiconRequested = false
        session.rejected = RejectedCorrections()
        forget()
        switchLanguage(to: language)
        // Whatever the strip showed last time belongs to another field.
        publish([])
    }

    /// The keyboard is on screen: the dictionaries start loading in the
    /// background, and the system lexicon (text replacements, contact names)
    /// is asked for once.
    func keyboardDidAppear(lexiconSource: UIInputViewController) {
        isVisible = true
        guard isSettingOn else { return }
        Self.loader?.prepare(language)
        loadLearnedWords(for: language)
        guard !lexiconRequested else { return }
        lexiconRequested = true
        lexiconSource.requestSupplementaryLexicon { [weak self] lexicon in
            let entries = lexicon.entries.map {
                AutocorrectLexicon.Entry(userInput: $0.userInput, documentText: $0.documentText)
            }
            self?.queue.async {
                let built = AutocorrectLexicon(entries: entries)
                DispatchQueue.main.async { self?.session.lexicon = built }
            }
        }
    }

    func switchLanguage(to next: KeyboardLanguage) {
        guard next != language || session.learned.language != next else { return }
        language = next
        session.learned = LearnedWords(language: next)
        forget()
        guard isActive else { return }
        Self.loader?.prepare(next)
        loadLearnedWords(for: next)
    }

    // MARK: Typing

    /// The text before the caret changed. `context` is that text; nil, or
    /// `allowed` false, means nothing should be suggested here.
    func textDidChange(before context: String?, allowed: Bool) {
        let word = isActive && allowed ? context.flatMap(TypedWord.init(before:)) : nil
        // The same word asks nothing new - except that a strip still on
        // screen for no word at all (after `restart()`) must go.
        guard word != requested || (word == nil && showsSuggestions) else { return }
        requested = word
        ticket += 1
        latestTicket.set(ticket)
        guard let word, let engine = Self.loader?.engine(for: language) else {
            latest = nil
            publish([])
            return
        }
        let ticket = self.ticket
        let session = self.session
        let box = latestTicket
        queue.async { [weak self] in
            // A newer keystroke already asked again: this answer would never be shown.
            guard box.value == ticket else { return }
            let suggestions = engine.suggestions(for: word, session: session)
            DispatchQueue.main.async {
                self?.receive(suggestions, for: word, ticket: ticket)
            }
        }
    }

    /// What a separator typed now should turn the word before the caret
    /// into. Only an answer already computed for exactly this word is used:
    /// a separator never waits for the dictionaries.
    func correction(before context: String?, allowed: Bool) -> AutocorrectCorrection? {
        guard isActive, allowed,
              let word = context.flatMap(TypedWord.init(before:)),
              let latest, latest.word == word else { return nil }
        return latest.suggestions.correction
    }

    /// Whether the strip on screen was worked out for exactly `word`. A tap
    /// on the strip applies only to the word it was offered for: after a
    /// caret move, or while a newer answer is on its way, the strip may still
    /// show another word's suggestions for a moment.
    func offersSuggestions(for word: TypedWord) -> Bool {
        latest?.word == word
    }

    /// The separator after `correction` was typed: keep it for one key.
    func didApply(_ correction: AutocorrectCorrection, separator: String) {
        applied = AppliedAutocorrection(correction: correction, separator: separator)
    }

    /// Backspace right after a correction. When the text before the caret
    /// still ends with exactly what the correction left, `replace` is asked to
    /// put the word as typed back; only if it did is the correction taken
    /// back for this session and the word learned. False: nothing was taken
    /// back, and the backspace is an ordinary one.
    func takeBackCorrection(before context: String?, replace: (AppliedAutocorrection) -> Bool) -> Bool {
        guard let applied else { return false }
        self.applied = nil
        guard let context, context.hasSuffix(applied.correction.replacement + applied.separator),
              replace(applied) else { return false }
        session.undo(applied)
        saveLearnedWords()
        forget()
        return true
    }

    /// The keys now type somewhere else (the composer opened or closed, a
    /// field took focus): the next text change asks again even for the
    /// same word.
    func restart() {
        forget()
    }

    /// Any key other than backspace, or a change from elsewhere: the last
    /// correction stays.
    func keepCorrection() {
        applied = nil
    }

    /// The quoted word in the strip was tapped: keep it and remember it.
    func keep(_ word: String) {
        session.keep(word)
        saveLearnedWords()
        forget()
    }

    // MARK: Results

    private func receive(_ suggestions: AutocorrectSuggestions, for word: TypedWord, ticket: Int) {
        guard ticket == self.ticket else { return }
        let checked = vetoed(suggestions, for: word)
        latest = (word, checked)
        publish(checked.items)
    }

    private func publish(_ items: [AutocorrectSuggestion]) {
        showsSuggestions = !items.isEmpty
        delegate?.autocorrect(self, didUpdate: items)
    }

    /// The system spell checker has the last word on Russian and English:
    /// a word it knows is never replaced by itself, only offered an
    /// alternative. Text replacements and the missing English apostrophes
    /// are fixes the user asked for and are not second-guessed.
    private func vetoed(_ suggestions: AutocorrectSuggestions, for word: TypedWord) -> AutocorrectSuggestions {
        guard let correction = suggestions.correction,
              !isFixedReplacement(for: word.text),
              isSpelledCorrectly(word.text) else { return suggestions }
        let items = suggestions.items.compactMap { item -> AutocorrectSuggestion? in
            switch item.kind {
            case .typed: return nil
            case .correction: return AutocorrectSuggestion(text: correction.replacement, kind: .word)
            case .word: return item
            }
        }
        return AutocorrectSuggestions(correction: nil, items: items)
    }

    private func isFixedReplacement(for typed: String) -> Bool {
        let key = WordList.key(for: typed)
        if session.lexicon.replacement(for: key) != nil { return true }
        return language == .english && AutocorrectEngine.contractions[key] != nil
    }

    private func isSpelledCorrectly(_ word: String) -> Bool {
        guard let code = Self.checkerLanguage(for: language) else { return false }
        let range = NSRange(location: 0, length: (word as NSString).length)
        return checker.rangeOfMisspelledWord(in: word, range: range, startingAt: 0, wrap: false, language: code).location == NSNotFound
    }

    /// The spell checker's language for a layout, when iOS has one. Kazakh
    /// usually has none.
    private static func checkerLanguage(for language: KeyboardLanguage) -> String? {
        guard language != .kazakh else { return nil }
        return checkerLanguages[language] ?? nil
    }

    private static let checkerLanguages: [KeyboardLanguage: String?] = {
        let available = UITextChecker.availableLanguages
        func first(_ prefix: String) -> String? {
            available.first { $0 == prefix || $0.hasPrefix(prefix + "_") || $0.hasPrefix(prefix + "-") }
        }
        return [.english: first("en"), .russian: first("ru")]
    }()

    // MARK: State

    /// Drops the last answer and request: the next text change asks again.
    private func forget() {
        requested = nil
        latest = nil
        ticket += 1
        latestTicket.set(ticket)
    }

    private func loadLearnedWords(for language: KeyboardLanguage) {
        let store = self.store
        DispatchQueue.global(qos: .utility).async { [weak self] in
            let stored = LearnedWords(language: language, store: store)
            DispatchQueue.main.async {
                guard let self, self.language == language, self.session.learned.language == language else { return }
                // Words taught before the store answered stay taught.
                var merged = stored
                self.session.learned.words.forEach { merged.learn($0) }
                self.session.learned = merged
            }
        }
    }

    private func saveLearnedWords() {
        let learned = session.learned
        let store = self.store
        DispatchQueue.global(qos: .utility).async {
            learned.save(to: store)
        }
    }
}

/// The newest request's number, readable from the suggestion queue.
private final class TicketBox: @unchecked Sendable {
    private let lock = NSLock()
    private var current = 0

    var value: Int {
        lock.lock()
        defer { lock.unlock() }
        return current
    }

    func set(_ value: Int) {
        lock.lock()
        current = value
        lock.unlock()
    }
}
