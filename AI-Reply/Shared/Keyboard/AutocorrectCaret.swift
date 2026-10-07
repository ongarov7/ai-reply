import Foundation

/// When smart correction may act on the word before the caret, and the check
/// it makes before it changes text (DESIGN §5.5, §7).
///
/// Ақылды түзету тек қазір теріліп жатқан сөзге ғана әсер етеді: пернетақта
/// ашылғанда немесе курсор сөздің ортасында тұрғанда ештеңе ұсынбайды.
///
/// * Only a word the user is typing NOW: a word character typed at the caret
///   since the keyboard appeared, the caret moved by itself or the keys moved
///   to another field. A word that was already there - the last word of a
///   pasted text, the field's text when the keyboard opens - gets no strip and
///   no correction, and the persona row stays in view.
/// * Never inside a word: with a word character right after the caret, only
///   half the word is before it, and correcting that half splits the word.
/// * Before anything in another app's field is replaced, the live text is read
///   once more and must still end with what is about to be replaced. The
///   keyboard's own copy of that text can be a moment behind the user - a
///   quick tap elsewhere, a field that turned Return into an action - and
///   deleting against it would delete the wrong text.
///
/// Pure, so these rules are unit-tested; the view controller only feeds it.
struct AutocorrectCaret: Equatable, Sendable {

    /// A word character was typed at the caret since it last moved by itself.
    private(set) var isTypingWord = false

    /// A key typed `text` at the caret. A letter, digit, apostrophe or hyphen
    /// means a word is being typed; anything else changes nothing - a space or
    /// a full stop ends the word, and a backspace may come back into it.
    mutating func typed(_ text: String) {
        guard text.count == 1, let character = text.first, Self.isWordCharacter(character) else { return }
        isTypingWord = true
    }

    /// The keyboard appeared, the caret moved without typing, or the keys now
    /// type somewhere else: nothing is being typed at the caret.
    mutating func reset() {
        isTypingWord = false
    }

    /// Whether the strip may show and a separator may correct at the caret.
    func allowsCorrection(caretInsideWord: Bool) -> Bool {
        isTypingWord && !caretInsideWord
    }

    /// Whether the text right after the caret continues a word, so the caret
    /// sits inside it.
    static func continuesWord(_ textAfterCaret: String?) -> Bool {
        guard let first = textAfterCaret?.first else { return false }
        return isWordCharacter(first)
    }

    /// Whether `expected` - a word, or a correction and its separator - is
    /// still what the live text has right before the caret, as a whole word:
    /// no letter or digit glued to its front. With `requiresWordEnd`, no word
    /// character may follow the caret either. Nothing known about the text
    /// (`before` nil or too short) is a no.
    static func liveText(before: String?, after: String? = nil, endsWith expected: String, requiresWordEnd: Bool) -> Bool {
        guard !expected.isEmpty, let before, before.hasSuffix(expected) else { return false }
        if let first = expected.first, isWordCharacter(first) {
            let rest = before.dropLast(expected.count)
            if let previous = rest.last, previous.isLetter || previous.isNumber { return false }
        }
        if requiresWordEnd, continuesWord(after) { return false }
        return true
    }

    /// Letters and digits, and the apostrophes and hyphen inside words
    /// (`don't`, `кто-то`) - the characters `TypedWord` builds a word from.
    static func isWordCharacter(_ character: Character) -> Bool {
        character.isLetter || character.isNumber || character == "'" || character == "\u{2019}" || character == "-"
    }
}
