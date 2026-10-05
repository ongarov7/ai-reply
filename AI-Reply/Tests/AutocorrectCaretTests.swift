import XCTest
@testable import AIReply

/// When the keyboard's smart correction may act at the caret, and the check it
/// makes before changing another app's text: no strip for a word that was
/// already there, never half a word, never against a stale copy of the text.
final class AutocorrectCaretTests: XCTestCase {

    // MARK: A word being typed now

    /// The keyboard appears with the caret after "Ок": the persona row stays,
    /// and a space typed there corrects nothing.
    func testAWordAlreadyThereIsNotBeingTyped() {
        let caret = AutocorrectCaret()
        XCTAssertFalse(caret.isTypingWord)
        XCTAssertFalse(caret.allowsCorrection(caretInsideWord: false))
    }

    func testTypingALetterStartsAWord() {
        var caret = AutocorrectCaret()
        caret.typed("с")
        XCTAssertTrue(caret.allowsCorrection(caretInsideWord: false))
    }

    func testDigitsApostrophesAndHyphensAreWordCharacters() {
        for text in ["7", "'", "\u{2019}", "-", "Ә"] {
            var caret = AutocorrectCaret()
            caret.typed(text)
            XCTAssertTrue(caret.isTypingWord, text)
        }
    }

    func testSeparatorsAndSpacesDoNotStartAWord() {
        for text in [" ", ".", ",", "!", "?", "\n", "😀", "ab"] {
            var caret = AutocorrectCaret()
            caret.typed(text)
            XCTAssertFalse(caret.isTypingWord, text.debugDescription)
        }
    }

    /// A caret move, the keyboard appearing again or another field: the word
    /// at the old caret is no longer being typed.
    func testResetEndsTheWord() {
        var caret = AutocorrectCaret()
        caret.typed("a")
        caret.reset()
        XCTAssertFalse(caret.allowsCorrection(caretInsideWord: false))
    }

    // MARK: Never inside a word

    /// "сегод|ян": typing inside a word offers nothing, so neither a strip
    /// pick nor a space can turn it into "сегодня ян".
    func testTypingInsideAWordNeverCorrects() {
        var caret = AutocorrectCaret()
        caret.typed("о")
        let inside = AutocorrectCaret.continuesWord("ян")
        XCTAssertTrue(inside)
        XCTAssertFalse(caret.allowsCorrection(caretInsideWord: inside))
    }

    func testWhatFollowsTheCaret() {
        XCTAssertTrue(AutocorrectCaret.continuesWord("ян привет"))
        XCTAssertTrue(AutocorrectCaret.continuesWord("1"))
        XCTAssertTrue(AutocorrectCaret.continuesWord("'s"))
        XCTAssertTrue(AutocorrectCaret.continuesWord("-то"))
        XCTAssertFalse(AutocorrectCaret.continuesWord(nil))
        XCTAssertFalse(AutocorrectCaret.continuesWord(""))
        XCTAssertFalse(AutocorrectCaret.continuesWord(" ян"))
        XCTAssertFalse(AutocorrectCaret.continuesWord(", ян"))
        XCTAssertFalse(AutocorrectCaret.continuesWord("\nян"))
    }

    // MARK: The live text before a change

    func testACorrectionNeedsTheWordRightBeforeTheCaret() {
        XCTAssertTrue(AutocorrectCaret.liveText(before: "Привет, сегодян", after: "", endsWith: "сегодян", requiresWordEnd: true))
        XCTAssertTrue(AutocorrectCaret.liveText(before: "сегодян", after: nil, endsWith: "сегодян", requiresWordEnd: true))
        XCTAssertTrue(AutocorrectCaret.liveText(before: "@сегодян", after: " и", endsWith: "сегодян", requiresWordEnd: true))
    }

    /// The caret was tapped elsewhere a moment ago and the keyboard's copy of
    /// the text did not catch up: the live text says otherwise, so nothing
    /// is deleted there.
    func testAMovedCaretIsNeverCorrected() {
        XCTAssertFalse(AutocorrectCaret.liveText(before: "Привет", after: " как дела", endsWith: "сегодян", requiresWordEnd: true))
        XCTAssertFalse(AutocorrectCaret.liveText(before: "сегод", after: "ян", endsWith: "сегодян", requiresWordEnd: true))
        XCTAssertFalse(AutocorrectCaret.liveText(before: nil, endsWith: "сегодян", requiresWordEnd: true))
        XCTAssertFalse(AutocorrectCaret.liveText(before: "", endsWith: "сегодян", requiresWordEnd: true))
    }

    /// The word must end at the caret: with "ян" after it, it is half a word.
    func testAWordContinuingAfterTheCaretIsNotReplaced() {
        XCTAssertFalse(AutocorrectCaret.liveText(before: "сегод", after: "ян", endsWith: "сегод", requiresWordEnd: true))
        XCTAssertTrue(AutocorrectCaret.liveText(before: "сегод", after: "ян", endsWith: "сегод", requiresWordEnd: false))
    }

    /// "сегодян" is the end of "присегодян" only as text, not as a word.
    func testTheWordMustStartAtAWordBoundary() {
        XCTAssertFalse(AutocorrectCaret.liveText(before: "присегодян", endsWith: "сегодян", requiresWordEnd: true))
        XCTAssertFalse(AutocorrectCaret.liveText(before: "x2the ", endsWith: "the ", requiresWordEnd: false))
        XCTAssertTrue(AutocorrectCaret.liveText(before: "кто-то", endsWith: "то", requiresWordEnd: true))
    }

    /// Undo after Return: a multi-line field has the new line and the word
    /// comes back; a single-line field turned Return into an action (search,
    /// send-and-clear) and has no new line, so backspace stays a backspace.
    func testUndoAfterReturnOnlyWhereTheNewLineIsThere() {
        XCTAssertTrue(AutocorrectCaret.liveText(before: "best the\n", endsWith: "the\n", requiresWordEnd: false))
        XCTAssertFalse(AutocorrectCaret.liveText(before: "best the", endsWith: "the\n", requiresWordEnd: false))
        XCTAssertFalse(AutocorrectCaret.liveText(before: "", endsWith: "the\n", requiresWordEnd: false))
    }

    func testUndoAfterSpaceAndPunctuation() {
        XCTAssertTrue(AutocorrectCaret.liveText(before: "Привет, сегодня ", endsWith: "сегодня ", requiresWordEnd: false))
        XCTAssertTrue(AutocorrectCaret.liveText(before: "the,", endsWith: "the,", requiresWordEnd: false))
        // Typed after the correction since: not the correction any more.
        XCTAssertFalse(AutocorrectCaret.liveText(before: "сегодня в", endsWith: "сегодня ", requiresWordEnd: false))
    }
}
