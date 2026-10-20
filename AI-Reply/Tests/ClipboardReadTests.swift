import XCTest
@testable import AIReply

/// Reading the copied message on the user's tap: a refused paste is told
/// apart from an empty clipboard, says what to do in every language, and is
/// never answered with a Retry that cannot work.
final class ClipboardReadTests: XCTestCase {

    // MARK: Classification

    func testTextIsTrimmed() {
        XCTAssertEqual(ClipboardRead.classify(hasStrings: true, string: "  Привет!\n"), .text("Привет!"))
    }

    func testNothingCopiedIsEmpty() {
        XCTAssertEqual(ClipboardRead.classify(hasStrings: false, string: nil), .empty)
        // Nothing to read means nothing was read: a stray value is ignored.
        XCTAssertEqual(ClipboardRead.classify(hasStrings: false, string: "old"), .empty)
        XCTAssertEqual(ClipboardRead.classify(hasStrings: true, string: " \n\t"), .empty)
        XCTAssertEqual(ClipboardRead.classify(hasStrings: true, string: ""), .empty)
    }

    /// iOS says there is text, and the read returns none: "Don't Allow", or
    /// "Paste from Other Apps" set to deny.
    func testTextThatCameBackAsNothingWasRefused() {
        XCTAssertEqual(ClipboardRead.classify(hasStrings: true, string: nil), .denied)
    }

    // MARK: Retry

    func testRetryIsNotOfferedWhereItCannotWork() {
        for error: AIReplyError in [.fullAccessRequired, .pasteDenied, .noSourceMessage, .messageTooLong(limit: 400),
                                    .noInstruction, .instructionTooLong(limit: 600)] {
            XCTAssertFalse(error.offersRetry, "\(error)")
        }
        for error: AIReplyError in [.offline, .timedOut, .rateLimited, .serviceUnavailable, .emptyResponse] {
            XCTAssertTrue(error.offersRetry, "\(error)")
        }
    }

    // MARK: Words

    /// Every language says iOS refused it, names the setting the way iOS
    /// names it there, and offers typing instead.
    func testRefusedPasteSaysWhereToAllowIt() {
        let expected: [AppLanguage: [String]] = [
            .english: ["Settings › AI Reply › Paste from Other Apps", "Allow", "type"],
            .russian: ["Настройки › AI Reply", "«Вставка из других приложений»", "«Разрешить»", "вручную"],
            .kazakh: ["Реттеулер › AI Reply", "«Басқа қолданбалардан көшіріп қою»", "«Рұқсат беру»", "қолмен"],
            .uzbek: ["Settings › AI Reply › Paste from Other Apps", "Allow", "qo\u{02BB}lda"]
        ]
        let english = AIReplyStrings.forLanguage(.english)
        for language in AppLanguage.allCases {
            let strings = AIReplyStrings.forLanguage(language)
            let message = strings.message(for: .pasteDenied)
            XCTAssertEqual(message, strings.pasteDenied)
            XCTAssertTrue(message.hasPrefix("iOS"), language.rawValue)
            for part in expected[language] ?? [] {
                XCTAssertTrue(message.contains(part), "\(language.rawValue): \(part)")
            }
            XCTAssertNotEqual(message, strings.noSourceMessage, "a refusal is not an empty clipboard")
            XCTAssertFalse(strings.typeSourceMessage.isEmpty)
            if language != .english {
                XCTAssertNotEqual(message, english.pasteDenied, language.rawValue)
                XCTAssertNotEqual(strings.typeSourceMessage, english.typeSourceMessage, language.rawValue)
            }
        }
    }
}
