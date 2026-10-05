import Foundation

/// What each typing slip costs, in tenths, for one word list typed on one
/// layout. Integer tenths keep every threshold exact: 0.6 + 0.7 is 13, never
/// 1.2999….
///
/// * inserting or deleting a letter: 1.0; deleting the second of a doubled
///   letter (typed "сеггодня"): 0.6;
/// * swapping two adjacent letters: 0.7;
/// * a neighbouring key: 0.6, any other letter: 1.0;
/// * Kazakh: a plain letter typed for its Kazakh letter (а→ә, г→ғ, к→қ, н→ң,
///   о→ө, у→ұ, у→ү, ұ→ү, х→һ, и→і, ы→і): 0.3. The other way round is
///   forbidden on every layout - a Kazakh letter the user typed is never
///   turned into a plain one;
/// * Cyrillic е and ё, either way: 0.1.
///
/// The Android keyboard charges exactly the same, so both engines agree.
struct EditCosts: Sendable {

    static let insertion: Int32 = 10
    static let deletion: Int32 = 10
    static let doubledLetterDeletion: Int32 = 6
    static let transposition: Int32 = 7
    static let neighbourSubstitution: Int32 = 6
    static let substitution: Int32 = 10
    static let kazakhLetter: Int32 = 3
    static let yo: Int32 = 1
    /// Far above any distance the engine accepts.
    static let forbidden: Int32 = 1_000
    /// The least an edit costs once letters of one class (see
    /// `letterClasses`) count as the same: a neighbouring key or a doubled letter.
    static let cheapestEdit: Int32 = min(neighbourSubstitution, doubledLetterDeletion)

    /// Typed plain letter → the Kazakh letter it stands for.
    static let kazakhPairs: Set<String> = ["аә", "гғ", "кқ", "нң", "оө", "уұ", "уү", "ұү", "хһ", "иі", "ыі"]

    /// Candidate codes per row.
    let width: Int
    /// The word list's letters, by code.
    private let letters: [String]
    private let proximity: KeyProximity
    /// Row = typed code, column = candidate code.
    private let table: [Int32]
    /// Code → its class: letters that replace each other for less than
    /// `cheapestEdit` (е/ё, the Kazakh pairs) share one. Units outside the
    /// alphabet share the last class.
    let letterClasses: [UInt8]

    init(alphabet: [UInt16], proximity: KeyProximity) {
        let letters = alphabet.map { String(decoding: [$0], as: UTF16.self) }
        var table: [Int32] = []
        table.reserveCapacity(letters.count * letters.count)
        for typed in letters {
            for candidate in letters {
                table.append(Self.substitution(typed, candidate, proximity: proximity))
            }
        }
        self.width = letters.count
        self.letters = letters
        self.proximity = proximity
        self.table = table
        self.letterClasses = Self.classes(width: letters.count) { typed, candidate in
            table[typed * letters.count + candidate]
        }
    }

    func substitution(typed: UInt8, candidate: UInt8) -> Int32 {
        table[Int(typed) * width + Int(candidate)]
    }

    /// What replacing a typed unit with each letter of the list costs, by
    /// candidate code - also for a unit the list never uses, such as a Kazakh
    /// letter measured against the Russian words.
    func substitutions(typed code: UInt8, unit: UInt16) -> ArraySlice<Int32> {
        if Int(code) < width {
            return table[Int(code) * width..<(Int(code) + 1) * width]
        }
        let typed = String(decoding: [unit], as: UTF16.self)
        return ArraySlice(letters.map { Self.substitution(typed, $0, proximity: proximity) })
    }

    /// Joins letters whose substitution, either way, is under `cheapestEdit`.
    private static func classes(width: Int, cost: (Int, Int) -> Int32) -> [UInt8] {
        var classes = (0...width).map { UInt8($0) }
        func root(_ code: Int) -> Int {
            var code = code
            while Int(classes[code]) != code { code = Int(classes[code]) }
            return code
        }
        for typed in 0..<width {
            for candidate in 0..<width where min(cost(typed, candidate), cost(candidate, typed)) < cheapestEdit {
                let (a, b) = (root(typed), root(candidate))
                if a != b { classes[max(a, b)] = UInt8(min(a, b)) }
            }
        }
        return (0...width).map { UInt8(root($0)) }
    }

    static func substitution(_ typed: String, _ candidate: String, proximity: KeyProximity) -> Int32 {
        guard typed != candidate else { return 0 }
        if kazakhPairs.contains(typed + candidate) { return kazakhLetter }
        if kazakhPairs.contains(candidate + typed) { return forbidden }
        if Set([typed, candidate]) == ["е", "ё"] { return yo }
        return proximity.areAdjacent(typed, candidate) ? neighbourSubstitution : substitution
    }
}

/// Weighted Damerau–Levenshtein distance (optimal string alignment) from one
/// typed word to many dictionary words, with two early cutoffs that never
/// change a result:
///
/// * the letters: every typed letter the word lacks and every word letter
///   the typed one lacks needs an edit of its own, at least `cheapestEdit`
///   each - most of a dictionary is ruled out by this count alone;
/// * the table: a word is dropped as soon as two consecutive rows are all
///   over the limit.
///
/// PERFORMANCE. This loop runs for tens of thousands of words per keystroke,
/// so it works on raw buffers the instance owns (no array or closure per
/// word) and uses wrapping arithmetic, which keeps even a Debug build fast.
/// One instance per query, used from one thread.
final class EditDistance {

    private let typedCount: Int
    private let typed: UnsafeMutablePointer<UInt8>
    /// Deletion cost of each typed letter (cheaper for a doubled one).
    private let deletions: UnsafeMutablePointer<Int32>
    private let width: Int
    /// Row per typed letter: what substituting each candidate code costs.
    private let substitutions: UnsafeMutablePointer<Int32>
    /// False when a typed unit outside the alphabet replaces some letter for
    /// less than `cheapestEdit`: the letter count would then not be a bound.
    private let lettersBound: Bool
    private let rowSize: Int
    private let rows: UnsafeMutablePointer<Int32>
    /// Code → letter class, and how many typed letters each class has.
    private let classes: UnsafeMutablePointer<UInt8>
    private let typedClassCounts: UnsafeMutablePointer<Int16>
    /// `typedClassCounts`, used up while counting one word and then restored.
    private let unmatched: UnsafeMutablePointer<Int16>

    /// - Parameters:
    ///   - typed: the typed word, encoded by the word list.
    ///   - units: the same word as UTF-16, for spotting doubled letters.
    ///   - longest: the longest candidate this instance will measure.
    init(typed: [UInt8], units: [UInt16], costs: EditCosts, longest: Int) {
        typedCount = typed.count
        self.typed = .allocate(capacity: typed.count)
        self.typed.initialize(from: typed, count: typed.count)
        deletions = .allocate(capacity: units.count)
        for index in units.indices {
            let doubled = index > 0 && units[index] == units[index - 1]
            deletions[index] = doubled ? EditCosts.doubledLetterDeletion : EditCosts.deletion
        }
        width = costs.width
        substitutions = .allocate(capacity: max(1, typed.count * costs.width))
        var lettersBound = true
        for (index, code) in typed.enumerated() {
            let row = costs.substitutions(typed: code, unit: units[index])
            (substitutions + index * costs.width).initialize(from: Array(row), count: costs.width)
            if Int(code) >= costs.width, row.contains(where: { $0 < EditCosts.cheapestEdit }) { lettersBound = false }
        }
        self.lettersBound = lettersBound
        rowSize = longest + 1
        rows = .allocate(capacity: 3 * rowSize)

        let classCount = costs.letterClasses.count
        classes = .allocate(capacity: classCount)
        classes.initialize(from: costs.letterClasses, count: classCount)
        typedClassCounts = .allocate(capacity: classCount)
        typedClassCounts.initialize(repeating: 0, count: classCount)
        for code in typed {
            typedClassCounts[Int(costs.letterClasses[Int(code)])] += 1
        }
        unmatched = .allocate(capacity: classCount)
        unmatched.initialize(from: typedClassCounts, count: classCount)
    }

    deinit {
        typed.deallocate()
        deletions.deallocate()
        substitutions.deallocate()
        rows.deallocate()
        classes.deallocate()
        typedClassCounts.deallocate()
        unmatched.deallocate()
    }

    /// Whether the letters alone already need edits worth more than `limit`:
    /// (word letters left unmatched + typed letters in excess) × `cheapestEdit`.
    /// Stops counting as soon as the answer is yes.
    private func lettersExceed(_ limit: Int32, candidate: UnsafePointer<UInt8>, count columns: Int) -> Bool {
        let allowed = Int(limit / EditCosts.cheapestEdit) - max(0, typedCount - columns)
        var misses = 0
        var counted = 0
        while counted < columns, misses <= allowed {
            let letterClass = Int(classes[Int(candidate[counted])])
            if unmatched[letterClass] > 0 {
                unmatched[letterClass] -= 1
            } else {
                misses += 1
            }
            counted += 1
        }
        var index = 0
        while index < counted {
            let letterClass = Int(classes[Int(candidate[index])])
            unmatched[letterClass] = typedClassCounts[letterClass]
            index += 1
        }
        return misses > allowed
    }

    /// The distance in tenths from the typed word to the `columns` codes at
    /// `candidate`, or nil when it is over `limit`.
    func distance(to candidate: UnsafePointer<UInt8>, count columns: Int, limit: Int32) -> Int32? {
        guard typedCount > 0, columns < rowSize,
              !lettersBound || !lettersExceed(limit, candidate: candidate, count: columns) else { return nil }
        var beforePrevious = rows
        var previous = rows + rowSize
        var current = rows + 2 * rowSize
        for column in 0...columns {
            previous[column] = Int32(truncatingIfNeeded: column) &* EditCosts.insertion
        }
        var previousMinimum: Int32 = 0

        for row in 1...typedCount {
            let letter = typed[row - 1]
            let substitutions = self.substitutions + (row - 1) * width
            let deletion = deletions[row - 1]
            current[0] = previous[0] &+ deletion
            var minimum = current[0]
            var column = 1
            while column <= columns {
                let other = candidate[column - 1]
                var value = previous[column] &+ deletion
                let inserted = current[column - 1] &+ EditCosts.insertion
                if inserted < value { value = inserted }
                let substituted = previous[column - 1] &+ (letter == other ? 0 : substitutions[Int(other)])
                if substituted < value { value = substituted }
                if row > 1, column > 1, letter != other,
                   letter == candidate[column - 2], typed[row - 2] == other {
                    let swapped = beforePrevious[column - 2] &+ EditCosts.transposition
                    if swapped < value { value = swapped }
                }
                current[column] = value
                if value < minimum { minimum = value }
                column += 1
            }
            // Any alignment passes through one of every two consecutive rows
            // (a swap skips at most one).
            if minimum > limit && previousMinimum > limit { return nil }
            previousMinimum = minimum
            (beforePrevious, previous, current) = (previous, current, beforePrevious)
        }
        let result = previous[columns]
        return result <= limit ? result : nil
    }
}
