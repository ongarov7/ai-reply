import Foundation

/// Replace the word just typed with another when a separator is typed.
struct AutocorrectCorrection: Equatable, Sendable {
    /// The word as typed.
    let original: String
    /// What goes in its place, already cased like the typed word.
    let replacement: String
}

/// One slot of the suggestion strip.
struct AutocorrectSuggestion: Equatable, Sendable {

    enum Kind: Equatable, Sendable {
        /// The word exactly as typed, shown in quotes: tapping it keeps it
        /// and teaches it to the keyboard.
        case typed
        /// The correction the next separator applies, shown as the default.
        case correction
        /// A candidate, completion or hint: tapping it replaces the word.
        case word
    }

    let text: String
    let kind: Kind
}

/// What the strip shows for the word being typed, and the correction a
/// separator would apply - computed together so the keyboard can do both
/// from one background pass.
struct AutocorrectSuggestions: Equatable, Sendable {
    let correction: AutocorrectCorrection?
    let items: [AutocorrectSuggestion]

    static let none = AutocorrectSuggestions(correction: nil, items: [])
}

extension AutocorrectEngine {

    static let stripLimit = 3

    /// Missing apostrophes fixed even though the typed form is a word of its
    /// own (`wont`, `hes`). `its`, `ill`, `id`, `well`, `were` and `lets` are
    /// left out on purpose: both spellings are common words.
    static let contractions: [String: String] = [
        "i": "I",
        "im": "I'm", "ive": "I've",
        "dont": "don't", "cant": "can't", "wont": "won't",
        "isnt": "isn't", "doesnt": "doesn't", "didnt": "didn't",
        "wasnt": "wasn't", "werent": "weren't", "arent": "aren't",
        "havent": "haven't", "hasnt": "hasn't", "hadnt": "hadn't",
        "couldnt": "couldn't", "wouldnt": "wouldn't", "shouldnt": "shouldn't",
        "thats": "that's", "whats": "what's", "theres": "there's", "heres": "here's",
        "theyre": "they're", "youre": "you're",
        "youll": "you'll", "youve": "you've", "weve": "we've",
        "theyll": "they'll", "theyve": "they've",
        "hes": "he's", "shes": "she's"
    ]

    // MARK: Decision

    /// What a separator typed after `word` should do: nil keeps the word.
    ///
    /// Never for a guarded word, a word taken back this session, or a known
    /// word (text replacements and the English contractions excepted).
    /// Otherwise the best candidate b wins over the second s when the word has
    /// at least three letters, b is within 1.0 (up to four letters), 1.3 (up
    /// to seven) or 2.0, s scores at least 0.25 worse, and b is among the
    /// 40,000 most frequent words or within 0.6.
    func correction(for word: TypedWord, session: AutocorrectSession? = nil) -> AutocorrectCorrection? {
        guard TokenGuard.reason(for: word, language: language, learned: learned(in: session)) == nil else { return nil }
        let key = WordList.key(for: word.text)
        if let fixed = fixedReplacement(for: word.text, key: key, session: session) {
            return applicable(fixed, for: word.text, session: session)
        }
        guard !isKnown(key, session: session) else { return nil }
        return correction(for: word.text, key: key, among: candidates(for: key, limit: 2), session: session)
    }

    /// The strip for `word` (nil: nothing is being typed), and the pending
    /// correction:
    ///
    /// * a pending correction: [typed] [correction] [next candidate or completion];
    /// * otherwise completions of what was typed first, then (for an unknown
    ///   word) candidates - with a text replacement or a Kazakh-letter hint
    ///   ahead of them when there is one.
    func suggestions(for word: TypedWord?, session: AutocorrectSession? = nil) -> AutocorrectSuggestions {
        guard let word else { return .none }
        let reason = TokenGuard.reason(for: word, language: language, learned: learned(in: session))
        guard reason == nil || reason == .learned else { return .none }

        let typed = word.text
        let key = WordList.key(for: typed)
        let known = isKnown(key, session: session)
        let found = known ? [] : candidates(for: key, limit: Self.stripLimit)
        let fixed = reason == nil ? fixedReplacement(for: typed, key: key, session: session) : nil
        var list = SuggestionList()

        let pending: AutocorrectCorrection?
        if let fixed {
            pending = applicable(fixed, for: typed, session: session)
        } else {
            pending = reason == nil && !known ? correction(for: typed, key: key, among: found, session: session) : nil
        }
        if let pending {
            list.append(typed, .typed)
            list.append(pending.replacement, .correction)
            found.forEach { list.append(Self.matchingCase(of: typed, $0.word), .word) }
            completions(for: key, limit: Self.stripLimit).forEach { list.append(Self.matchingCase(of: typed, $0), .word) }
            return AutocorrectSuggestions(correction: pending, items: list.items)
        }

        if let replacement = session?.lexicon.replacement(for: key) {
            // Taken back earlier this session, so no longer applied - still first.
            list.append(replacement, .word)
        } else if known, reason == nil, let hint = kazakhLetterHint(for: key) {
            list.append(Self.matchingCase(of: typed, hint), .word)
        }
        completions(for: key, limit: Self.stripLimit).forEach { list.append(Self.matchingCase(of: typed, $0), .word) }
        found.forEach { list.append(Self.matchingCase(of: typed, $0.word), .word) }
        return AutocorrectSuggestions(correction: nil, items: list.items)
    }

    /// The Kazakh spelling of a word typed with plain letters (`кайда` →
    /// `қайда`): offered first on the Kazakh layout, never applied by itself.
    /// Only for a known word that neither the Kazakh nor the Russian list has
    /// as typed - a listed Russian word (`был`, `куда`, `они`) is meant as
    /// written - and only a listed word that differs from it in plain letters
    /// standing for Kazakh ones alone; the fewest such letters and the most
    /// frequent word win (0.3 a letter plus the rank penalty).
    func kazakhLetterHint(for word: String) -> String? {
        let key = WordList.key(for: word)
        guard language == .kazakh, words.rank(of: key) == nil, !isListedInOtherLanguage(key) else { return nil }
        let typed = Array(key.utf16)
        let codes = words.encode(key)
        guard !codes.contains(words.unknownCode) else { return nil }
        return words.withWords(length: typed.count) { ranks, candidates in
            var best: (rank: Int, score: Double)?
            for index in 0..<ranks.count {
                var letters = 0
                var matches = true
                for position in 0..<codes.count {
                    let other = candidates[index * codes.count + position]
                    guard other != codes[position] else { continue }
                    guard Self.isPlain(typed[position], for: words.alphabet[Int(other)]) else {
                        matches = false
                        break
                    }
                    letters += 1
                }
                guard matches, letters > 0 else { continue }
                let rank = Int(ranks[index])
                let score = Double(Int32(letters) * EditCosts.kazakhLetter) / 10 + Self.rankPenalty(rank)
                if best == nil || score < best!.score { best = (rank, score) }
            }
            return best.map { words.word(at: $0.rank) }
        } ?? nil
    }

    private static func isPlain(_ plain: UInt16, for kazakh: UInt16) -> Bool {
        EditCosts.kazakhPairs.contains(String(decoding: [plain, kazakh], as: UTF16.self))
    }

    // MARK: Helpers

    private func learned(in session: AutocorrectSession?) -> LearnedWords {
        session?.learned ?? LearnedWords(language: language)
    }

    /// A text replacement, or an English contraction: what `key` is always
    /// written as, whether or not the typed form is a word.
    private func fixedReplacement(for typed: String, key: String, session: AutocorrectSession?) -> String? {
        if let text = session?.lexicon.replacement(for: key) { return text }
        guard language == .english, let contraction = Self.contractions[key] else { return nil }
        return Self.matchingCase(of: typed, contraction)
    }

    /// `replacement` as a correction, unless it changes nothing or was taken
    /// back this session - then the word stays as typed, with no other fix.
    private func applicable(_ replacement: String, for typed: String, session: AutocorrectSession?) -> AutocorrectCorrection? {
        guard replacement != typed,
              session?.rejected.contains(original: typed, replacement: replacement) != true else { return nil }
        return AutocorrectCorrection(original: typed, replacement: replacement)
    }

    /// §7.5 applied to candidates already found for an unknown word.
    private func correction(
        for typed: String,
        key: String,
        among candidates: [AutocorrectCandidate],
        session: AutocorrectSession?
    ) -> AutocorrectCorrection? {
        let length = key.utf16.count
        guard length >= 3, let best = candidates.first else { return nil }
        let maximumCost = length <= 4 ? 10 : length <= 7 ? 13 : 20
        guard best.cost <= maximumCost,
              candidates.count < 2 || candidates[1].score - best.score >= 0.25,
              best.rank < 40_000 || best.cost <= 6,
              session?.rejected.contains(original: typed, replacement: best.word) != true else { return nil }
        return AutocorrectCorrection(original: typed, replacement: Self.matchingCase(of: typed, best.word))
    }
}

/// Up to three strip entries, no text twice. The typed word is only ever
/// shown as itself: candidates and completions are never the typed key.
private struct SuggestionList {

    private(set) var items: [AutocorrectSuggestion] = []

    mutating func append(_ text: String, _ kind: AutocorrectSuggestion.Kind) {
        guard items.count < AutocorrectEngine.stripLimit, !items.contains(where: { $0.text == text }) else { return }
        items.append(AutocorrectSuggestion(text: text, kind: kind))
    }
}
