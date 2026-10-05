import Foundation

/// A dictionary word that is close to what was typed.
struct AutocorrectCandidate: Equatable, Sendable {
    /// As listed: `привет`, `Москва`.
    let word: String
    let rank: Int
    /// Edit distance in tenths (see `EditCosts`).
    let cost: Int
    /// Distance plus a frequency penalty: lower is better.
    let score: Double

    var distance: Double { Double(cost) / 10 }
}

/// Typing smarts for one keyboard layout, on the phone, with no network: is a
/// word real, which words is a typo close to, how does a word end, and should
/// a separator fix it.
///
/// THREADING. Everything is loaded before the engine exists and never changes,
/// so any queue may query it; the user's own words come in with each query as
/// an `AutocorrectSession` value. Get engines from `AutocorrectLoader`.
final class AutocorrectEngine: Sendable {

    let language: KeyboardLanguage
    let words: WordList
    /// The languages whose known-words filters this layout consults: the
    /// Kazakh layout is the Russian one plus a row, and people type both.
    private let filters: [KnownWordsFilter]
    /// Where candidates come from: the layout's own list first, then the
    /// other language typed on these keys (Russian on the Kazakh layout).
    private let pools: [CandidatePool]

    init(
        language: KeyboardLanguage,
        words: WordList,
        filters: [KnownWordsFilter],
        otherWords: [WordList] = [],
        proximity: KeyProximity
    ) {
        self.language = language
        self.words = words
        self.filters = filters
        self.pools = ([words] + otherWords).map { CandidatePool(words: $0, costs: EditCosts(alphabet: $0.alphabet, proximity: proximity)) }
    }

    /// Languages whose filters `language` checks, own first.
    static func knownLanguages(for language: KeyboardLanguage) -> [KeyboardLanguage] {
        switch language {
        case .english: return [.english]
        case .russian: return [.russian, .kazakh]
        case .kazakh:  return [.kazakh, .russian]
        }
    }

    /// Languages whose word lists `language` searches besides its own: the
    /// Kazakh layout corrects Russian words too.
    static func otherWordLanguages(for language: KeyboardLanguage) -> [KeyboardLanguage] {
        language == .kazakh ? [.russian] : []
    }

    // MARK: Known words

    /// Whether `word` is a real word on this layout: learned, in the system
    /// lexicon, or in a known-words filter - for Cyrillic also with `ё` typed
    /// as `е`.
    func isKnown(_ word: String, session: AutocorrectSession? = nil) -> Bool {
        let key = WordList.key(for: word)
        guard !key.isEmpty else { return false }
        if let session {
            if session.learned.language == language, session.learned.contains(key) { return true }
            if session.lexicon.knows(key) { return true }
        }
        if filtersContain(key) { return true }
        return language != .english && key.contains("ё") && filtersContain(key.replacingOccurrences(of: "ё", with: "е"))
    }

    private func filtersContain(_ key: String) -> Bool {
        let hashes = KnownWordsFilter.Hashes(key: key)
        return filters.contains { $0.contains(hashes) }
    }

    // MARK: Candidates

    /// The `limit` listed words closest to `word`, best first: edit distance
    /// plus 0.15 · log10(rank + 1) in the word's own list, ties to the more
    /// frequent word. Words of the typed length ± 1 are scanned (± 2 from
    /// seven letters on), up to a distance of 1.0 for three letters or fewer,
    /// 1.7 for four or five, 2.0 beyond.
    ///
    /// Never offered: a word that turns a typed Kazakh letter into its plain
    /// letter, and on the English layout a contraction typed without its
    /// apostrophe (`dont`), which has its own fix. A word typed with a Kazakh
    /// letter is Kazakh, so only the layout's own list is searched for it -
    /// the Russian list never competes (as on Android).
    func candidates(for word: String, limit: Int) -> [AutocorrectCandidate] {
        let key = WordList.key(for: word)
        let units = Array(key.utf16)
        guard limit > 0, !units.isEmpty else { return [] }
        let searched = units.contains { Self.kazakhLetterUnits.contains($0) } ? Array(pools.prefix(1)) : pools
        let found = searched.enumerated().flatMap { index, pool in
            nearest(units, in: pool, limit: limit).map { (pool: index, candidate: $0) }
        }
        guard searched.count > 1 else { return found.map(\.candidate) }
        // The same word may be listed in both languages: it counts once, at
        // its better score.
        var seen: Set<String> = []
        return found
            .sorted { a, b in
                let (x, y) = (a.candidate, b.candidate)
                if x.score != y.score { return x.score < y.score }
                if x.rank != y.rank { return x.rank < y.rank }
                return a.pool < b.pool
            }
            .filter { seen.insert(WordList.key(for: $0.candidate.word)).inserted }
            .prefix(limit)
            .map(\.candidate)
    }

    /// Listed words that start with `word` and are longer, most frequent
    /// first. Nothing for a single letter.
    func completions(for word: String, limit: Int) -> [String] {
        let key = WordList.key(for: word)
        guard key.utf16.count >= 2 else { return [] }
        return words.completionRanks(of: key, limit: limit) { !self.isNeverOffered(self.words.key(at: $0)) }
            .map(words.word(at:))
    }

    /// `word` cased the way the user typed: a capitalised word gives a
    /// capitalised suggestion, a lowercase one the listed form (`москва` →
    /// `Москва`, `i` → `I`).
    static func matchingCase(of typed: String, _ word: String) -> String {
        guard typed.first?.isUppercase == true else { return word }
        return word.prefix(1).uppercased() + word.dropFirst()
    }

    static let frequencyWeight = 0.15

    static func rankPenalty(_ rank: Int) -> Double {
        frequencyWeight * log10(Double(rank + 1))
    }

    /// The furthest a candidate may be from a typed word of `length` units.
    static func searchRadius(length: Int) -> Int32 {
        length <= 3 ? 10 : length <= 5 ? 17 : 20
    }

    /// Whether a word list searched besides the layout's own has `key` as
    /// typed: on the Kazakh layout, a listed Russian word (`был`, `куда`).
    func isListedInOtherLanguage(_ key: String) -> Bool {
        pools.dropFirst().contains { $0.words.rank(of: key) != nil }
    }

    /// A listed spelling with a fix of its own: `dont` is offered as `don't`.
    func isNeverOffered(_ key: String) -> Bool {
        guard language == .english, let fixed = Self.contractions[key] else { return false }
        return WordList.key(for: fixed) != key
    }

    // MARK: Search

    private func nearest(_ units: [UInt16], in pool: CandidatePool, limit: Int) -> [AutocorrectCandidate] {
        let length = units.count
        let maximumCost = Self.searchRadius(length: length)
        let spread = length >= 7 ? 2 : 1
        let words = pool.words
        let typed = words.encode(String(decoding: units, as: UTF16.self))
        let distance = EditDistance(typed: typed, units: units, costs: pool.costs, longest: length + spread)
        var found = CandidateList(limit: limit)
        // Both filters need the word as text; most queries need neither.
        let mayLoseKazakhLetter = units.contains { Self.kazakhLetterUnits.contains($0) }
        let mayBeContraction = language == .english
        // Once the list is full, what a word must stay under to make it: its
        // distance, and - the frequency penalty only grows with rank - rank + 1.
        var costLimit = maximumCost
        var rankLimit = Double.infinity

        for other in Self.scanOrder(length: length, spread: spread) {
            // Inserting or deleting the length difference is unavoidable.
            let lengthCost = Int32(abs(other - length)) * (other > length ? EditCosts.insertion : EditCosts.cheapestEdit)
            words.withWords(length: other) { ranks, codes in
                var index = 0
                while index < ranks.count, lengthCost <= costLimit {
                    let rank = Int(ranks[index])
                    // Ranks only grow along a bucket: nothing after this can win.
                    guard Double(rank + 1) < rankLimit else { break }
                    let candidate = codes.baseAddress! + index * other
                    index += 1
                    // Zero is the typed word itself: a candidate is another word.
                    guard let cost = distance.distance(to: candidate, count: other, limit: costLimit), cost > 0 else { continue }
                    let score = Double(cost) / 10 + Self.rankPenalty(rank)
                    guard found.admits(rank: rank, score: score) else { continue }
                    if mayLoseKazakhLetter || mayBeContraction {
                        let key = words.key(at: rank)
                        guard !isNeverOffered(key), !Self.losesKazakhLetter(typed: units, candidate: Array(key.utf16)) else { continue }
                    }
                    found.insert(rank: rank, cost: Int(cost), score: score)
                    if let worst = found.worstScore {
                        costLimit = min(maximumCost, Int32((worst * 10 + 1e-9).rounded(.down)))
                        rankLimit = pow(10, worst / Self.frequencyWeight)
                    }
                }
            }
        }
        return found.entries.map { entry in
            AutocorrectCandidate(word: words.word(at: entry.rank), rank: entry.rank, cost: entry.cost, score: entry.score)
        }
    }

    /// The typed length first, then one shorter and longer, then two.
    private static func scanOrder(length: Int, spread: Int) -> [Int] {
        [length] + (1...spread).flatMap { [length - $0, length + $0] }.filter { $0 > 0 }
    }

    /// Whether `candidate` turns a Kazakh letter of `typed` into its plain
    /// letter by any route: the forbidden substitution, or - which the
    /// distance alone allows in a long word - deleting `қ` and inserting `к`.
    static func losesKazakhLetter(typed: [UInt16], candidate: [UInt16]) -> Bool {
        kazakhPairUnits.contains { plain, kazakh in
            count(kazakh, in: typed) > count(kazakh, in: candidate) && count(plain, in: candidate) > count(plain, in: typed)
        }
    }

    private static let kazakhPairUnits: [(plain: UInt16, kazakh: UInt16)] = EditCosts.kazakhPairs.map { pair in
        let units = Array(pair.utf16)
        return (units[0], units[1])
    }

    private static let kazakhLetterUnits = Set(kazakhPairUnits.map(\.kazakh))

    private static func count(_ unit: UInt16, in units: [UInt16]) -> Int {
        units.reduce(0) { $0 + ($1 == unit ? 1 : 0) }
    }
}

/// One word list to search, with the costs of typing it on this layout.
private struct CandidatePool: Sendable {
    let words: WordList
    let costs: EditCosts
}

/// The best few candidates so far, kept sorted.
private struct CandidateList {

    struct Entry {
        let rank: Int
        let cost: Int
        let score: Double
    }

    let limit: Int
    private(set) var entries: [Entry] = []

    /// The score a new candidate has to beat once the list is full.
    var worstScore: Double? {
        entries.count == limit ? entries.last?.score : nil
    }

    /// Whether a word with this score and rank would get in.
    func admits(rank: Int, score: Double) -> Bool {
        guard let worst = entries.last, entries.count == limit else { return true }
        return score < worst.score || (score == worst.score && rank < worst.rank)
    }

    mutating func insert(rank: Int, cost: Int, score: Double) {
        let entry = Entry(rank: rank, cost: cost, score: score)
        let index = entries.firstIndex { score < $0.score || (score == $0.score && rank < $0.rank) } ?? entries.count
        guard index < limit else { return }
        entries.insert(entry, at: index)
        if entries.count > limit { entries.removeLast() }
    }
}
