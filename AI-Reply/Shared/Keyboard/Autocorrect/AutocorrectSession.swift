import Foundation

/// What one keyboard session knows about the user's own words: the learned
/// words of the current layout, the corrections taken back since the keyboard
/// appeared, and the system lexicon.
///
/// A value: the keyboard owns it, mutates it on its own queue, and hands the
/// engine a copy with every query.
struct AutocorrectSession: Equatable, Sendable {

    var learned: LearnedWords
    var rejected = RejectedCorrections()
    var lexicon = AutocorrectLexicon()

    init(learned: LearnedWords) {
        self.learned = learned
    }

    /// Backspace right after an automatic correction: the correction is not
    /// offered for that word again this session, and the word as typed is
    /// learned. The caller puts the typed word back in the text
    /// (`AppliedAutocorrection` says how).
    mutating func undo(_ applied: AppliedAutocorrection) {
        rejected.insert(applied.correction)
        learned.learn(applied.correction.original)
    }

    /// The user tapped what they typed in the suggestion strip.
    mutating func keep(_ word: String) {
        learned.learn(word)
    }
}

/// Corrections the user took back in this session, by lowercased word pair.
struct RejectedCorrections: Equatable, Sendable {

    private struct Pair: Hashable, Sendable {
        let original: String
        let replacement: String
    }

    private var pairs: Set<Pair> = []

    mutating func insert(_ correction: AutocorrectCorrection) {
        pairs.insert(Pair(original: WordList.key(for: correction.original), replacement: WordList.key(for: correction.replacement)))
    }

    func contains(original: String, replacement: String) -> Bool {
        pairs.contains(Pair(original: WordList.key(for: original), replacement: WordList.key(for: replacement)))
    }
}

/// An automatic correction the keyboard has just made, kept until the next
/// key: if that key is backspace, the correction is taken back.
struct AppliedAutocorrection: Equatable, Sendable {

    let correction: AutocorrectCorrection
    /// What was typed after the word: a space, punctuation or a new line.
    let separator: String

    /// Characters to delete before the caret: the correction and its separator.
    var charactersToDelete: Int { correction.replacement.count + separator.count }

    /// What goes back in their place: the word as typed, without the separator.
    var restoredText: String { correction.original }
}
