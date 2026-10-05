import XCTest
@testable import AIReply

/// The words of the suggestion strip and the instruction suggestion: present
/// in every app language, really translated, and Kazakh in Kazakh letters.
final class TypingStringsTests: XCTestCase {

    private func values(_ strings: TypingStrings) -> [String: String] {
        [
            "keepTyped": strings.keepTyped("сәлем"),
            "correction": strings.correction("сәлем"),
            "polishSuggestion": strings.polishSuggestion("Сәлем."),
            "polishSuggestionHint": strings.polishSuggestionHint,
            "undo": strings.undo,
            "undoAccessibility": strings.undoAccessibility
        ]
    }

    func testEveryLanguageHasItsOwnWords() {
        let english = values(TypingStrings.forLanguage(.english))
        for language in AppLanguage.allCases {
            let own = values(AIReplyStrings.forLanguage(language).typing)
            for (key, value) in own {
                XCTAssertFalse(value.isEmpty, "\(key) is empty in \(language.rawValue)")
                if language != .english {
                    XCTAssertNotEqual(value, english[key], "\(key) is English in \(language.rawValue)")
                }
            }
            XCTAssertTrue(own["keepTyped"]?.contains("сәлем") == true, "the word is in the sentence (\(language.rawValue))")
        }
    }

    func testKazakhUsesKazakhLetters() {
        let kazakh = TypingStrings.forLanguage(.kazakh)
        let letters = Set("әғқңөұүһі")
        for text in [kazakh.keepTypedFormat, kazakh.correctionFormat, kazakh.polishSuggestionFormat, kazakh.polishSuggestionHint, kazakh.undoAccessibility] {
            XCTAssertTrue(text.contains(where: letters.contains), text)
        }
    }
}
