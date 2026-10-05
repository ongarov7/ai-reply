import XCTest
@testable import AIReply

/// Finding the word before the caret, and the words the keyboard must never
/// touch.
final class AutocorrectTokenGuardTests: XCTestCase {

    // MARK: Typed word

    func testTypedWordIsTheRunBeforeTheCaret() {
        XCTAssertEqual(TypedWord(before: "Привет, как дела"), TypedWord(text: "дела"))
        XCTAssertEqual(TypedWord(before: "Скажи «превт"), TypedWord(text: "превт", prefix: "«"))
        XCTAssertEqual(TypedWord(before: "go to https://site.ru"), TypedWord(text: "ru", prefix: "https://site."))
        XCTAssertEqual(TypedWord(before: "@превт"), TypedWord(text: "превт", prefix: "@"))
        XCTAssertEqual(TypedWord(before: "превт123"), TypedWord(text: "превт123"))
        XCTAssertEqual(TypedWord(before: "I dont"), TypedWord(text: "dont"))
        XCTAssertEqual(TypedWord(before: "don't"), TypedWord(text: "don't"))
        XCTAssertEqual(TypedWord(before: "кто-то"), TypedWord(text: "кто-то"))
        XCTAssertEqual(TypedWord(before: "-сәлем"), TypedWord(text: "сәлем", prefix: "-"))
    }

    func testNoWordRightBeforeTheCaret() {
        XCTAssertNil(TypedWord(before: ""))
        XCTAssertNil(TypedWord(before: "привет "))
        XCTAssertNil(TypedWord(before: "привет."))
        XCTAssertNil(TypedWord(before: "привет\n"))
        XCTAssertNil(TypedWord(before: "--"))
    }

    // MARK: Guard

    func testGuardedTokens() {
        let cases: [(String, KeyboardLanguage, TokenGuard.Reason)] = [
            ("@превт", .russian, .technical),
            ("#тег", .russian, .technical),
            ("https://site.ru", .russian, .technical),
            ("www.kaspi", .english, .technical),
            ("user_name", .english, .technical),
            ("a=b", .english, .technical),
            ("превт123", .russian, .digits),
            ("в", .russian, .tooShort),
            ("a", .english, .tooShort),
            ("ПРИВЕТ", .russian, .allCaps),
            ("OK", .english, .allCaps),
            ("iPhone", .english, .innerCapital),
            ("McDonald", .english, .innerCapital),
            ("прuвет", .russian, .mixedScript),
            ("hello", .russian, .otherScript),
            ("hello", .kazakh, .otherScript),
            ("привет", .english, .otherScript)
        ]
        for (context, language, reason) in cases {
            guard let word = TypedWord(before: context) else {
                XCTFail("no word in \(context)")
                continue
            }
            XCTAssertEqual(TokenGuard.reason(for: word, language: language, learned: LearnedWords(language: language)), reason, context)
        }
    }

    func testOrdinaryWordsPass() {
        let cases: [(String, KeyboardLanguage)] = [
            ("превт", .russian), ("Превт", .russian), ("қалайсын", .kazakh), ("кайда", .kazakh),
            ("teh", .english), ("i", .english), ("I", .english), ("dont", .english), ("ещё", .russian)
        ]
        for (text, language) in cases {
            XCTAssertNil(TokenGuard.reason(for: TypedWord(text: text), language: language, learned: LearnedWords(language: language)), text)
        }
    }

    func testLearnedWordsAreGuardedOnTheirOwnLayoutOnly() {
        let learned = LearnedWords(language: .russian, words: ["превт"])
        XCTAssertEqual(TokenGuard.reason(for: TypedWord(text: "Превт"), language: .russian, learned: learned), .learned)
        XCTAssertNil(TokenGuard.reason(for: TypedWord(text: "превт"), language: .kazakh, learned: learned))
    }
}
