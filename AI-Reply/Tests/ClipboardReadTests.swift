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
                                    .noInstruction, .instructionTooLong(limit: 600), .quotaExhausted, .monthlyQuotaExhausted] {
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
        // The option labels are iOS's own (PSSystemPolicy PASTE_BETWEEN_APPS_ALLOW):
        // «Разрешать», «Рұқсат бар».
        let expected: [AppLanguage: [String]] = [
            .english: [Self.pasteSettingPath[.english]!, "Allow", "type"],
            .russian: [Self.pasteSettingPath[.russian]!, "«Разрешать»", "вручную"],
            .kazakh: [Self.pasteSettingPath[.kazakh]!, "«Рұқсат бар»", "қолмен"],
            .uzbek: [Self.pasteSettingPath[.uzbek]!, "Allow", "qo\u{02BB}lda"]
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

    /// Where iOS keeps the setting, as Settings names it in each language
    /// (Preferences.app CFBundleDisplayName and PSSystemPolicy
    /// PASTE_BETWEEN_APPS). iOS has no Uzbek, so Uzbek uses the English labels.
    private static let pasteSettingPath: [AppLanguage: String] = [
        .english: "Settings ▸ AI Reply ▸ Paste from Other Apps",
        .russian: "«Настройки» ▸ AI Reply ▸ «Вставка из других приложений»",
        .kazakh: "«Реттеулер» ▸ AI Reply ▸ «Басқа қолданбалардан көшіріп қою»",
        .uzbek: "Settings ▸ AI Reply ▸ Paste from Other Apps"
    ]

    /// The keyboard's refusal and the app's paste help send the user to the
    /// same place, spelled the same way, in every language.
    func testAppAndKeyboardNameThePasteSettingAlike() {
        let settings = AppSettings(store: SharedSettings(defaults: UserDefaults(suiteName: "ClipboardReadTests.\(UUID())")!))
        for language in AppLanguage.allCases {
            settings.setLanguage(language)
            let path = Self.pasteSettingPath[language]!
            XCTAssertTrue(AIReplyStrings.forLanguage(language).pasteDenied.contains(path), "keyboard, \(language.rawValue)")
            // The app's paste help page names the same path and iOS's own
            // option. It keeps "AI Reply" on one line with a no-break space.
            let page = settings.localized("pasteHelp.path").replacingOccurrences(of: "\u{00A0}", with: " ")
            XCTAssertTrue(page.contains(path), "paste help, \(language.rawValue): \(page)")
            let option = language == .russian ? "«Разрешать»" : language == .kazakh ? "«Рұқсат бар»" : "Allow"
            XCTAssertTrue(page.contains(option), "paste help option, \(language.rawValue)")
            XCTAssertTrue(settings.localized("pasteHelp.step.allow").contains(option), "paste help step, \(language.rawValue)")
        }
    }
}
