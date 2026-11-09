import XCTest
@testable import AIReply

/// Every sentence Watch Voice says, in every language the app has.
final class WatchVoiceLocalizationTests: XCTestCase {

    private static let allFailures: [WatchVoiceFailure] = [
        .unsupportedVersion, .invalidRequest, .instructionEmpty, .instructionTooLong, .expired, .storageUnavailable,
        .fullAccessRequired, .notSignedIn, .consentRequired, .offline, .timedOut, .rateLimited, .quotaExhausted,
        .monthlyQuotaExhausted, .serviceUnavailable, .nothingToRewrite, .copiedMessageUnreadable, .copiedMessageTooLong,
        .transcriptionUnavailable, .transcriptionFailed, .audioRejected, .nothingHeard, .voiceLimitReached,
        .microphoneDenied, .phoneUnreachable
    ]

    private func fields(_ strings: WatchVoiceStrings) -> [String] {
        Mirror(reflecting: strings).children.compactMap { $0.value as? String }
    }

    func testTheKeyboardsWordsExistInEveryLanguage() {
        for language in AppLanguage.allCases {
            for value in fields(WatchVoiceStrings.forLanguage(language)) {
                XCTAssertFalse(value.trimmingCharacters(in: .whitespaces).isEmpty, language.rawValue)
            }
        }
    }

    func testNoLanguageFallsBackToEnglish() {
        let english = fields(.english)
        for language in [AppLanguage.russian, .kazakh, .uzbek] {
            let translated = fields(WatchVoiceStrings.forLanguage(language))
            XCTAssertEqual(english.count, translated.count)
            for (source, target) in zip(english, translated) {
                XCTAssertNotEqual(source, target, "\(language.rawValue): \(source)")
            }
        }
    }

    func testTheWordsTheBriefNames() {
        XCTAssertEqual(WatchVoiceStrings.english.insertResponse, "Insert Response")
        XCTAssertEqual(WatchVoiceStrings.english.controlName, "Watch Voice")
        XCTAssertEqual(WatchVoiceStrings.russian.insertResponse, "Вставить ответ")
        XCTAssertEqual(WatchVoiceStrings.kazakh.insertResponse, "Жауапты кірістіру")
    }

    func testEveryFailureHasASentence() {
        for language in AppLanguage.allCases {
            let strings = WatchVoiceStrings.forLanguage(language)
            for failure in Self.allFailures {
                XCTAssertFalse(strings.message(for: failure).isEmpty, "\(language.rawValue) \(failure)")
            }
        }
    }

    // MARK: The watch app's catalog

    private func catalog(_ name: String) throws -> [String: Any] {
        let url = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
            .appendingPathComponent("AIReplyWatch").appendingPathComponent(name)
        let object = try JSONSerialization.jsonObject(with: Data(contentsOf: url)) as? [String: Any]
        return try XCTUnwrap(object?["strings"] as? [String: Any])
    }

    private func assertComplete(_ strings: [String: Any], file: StaticString = #filePath, line: UInt = #line) {
        for (key, value) in strings {
            let localizations = (value as? [String: Any])?["localizations"] as? [String: Any] ?? [:]
            XCTAssertEqual(Set(localizations.keys), ["en", "ru", "kk", "uz"], key, file: file, line: line)
        }
    }

    func testTheWatchAppIsTranslated() throws {
        let strings = try catalog("Localizable.xcstrings")
        XCTAssertFalse(strings.isEmpty)
        assertComplete(strings)
    }

    func testTheMicrophonePromptIsTranslatedAndNamesWhereTheVoiceGoes() throws {
        let strings = try catalog("InfoPlist.xcstrings")
        assertComplete(strings)
        let prompt = strings["NSMicrophoneUsageDescription"] as? [String: Any]
        let localizations = prompt?["localizations"] as? [String: Any] ?? [:]
        for (language, value) in localizations {
            let text = ((value as? [String: Any])?["stringUnit"] as? [String: Any])?["value"] as? String ?? ""
            XCTAssertTrue(text.contains("OpenAI"), language)
        }
    }

    /// App Review 5.1.2: the screen before the first recording says who gets
    /// the voice, in every language.
    func testTheWatchConsentNamesWhereTheVoiceGoes() throws {
        let strings = try catalog("Localizable.xcstrings")
        let body = strings["watch.consent.body"] as? [String: Any]
        let localizations = body?["localizations"] as? [String: Any] ?? [:]
        XCTAssertEqual(Set(localizations.keys), ["en", "ru", "kk", "uz"])
        for (language, value) in localizations {
            let text = ((value as? [String: Any])?["stringUnit"] as? [String: Any])?["value"] as? String ?? ""
            XCTAssertTrue(text.contains("OpenAI"), language)
        }
        XCTAssertNotNil(strings["watch.consent.allow"])
        XCTAssertNotNil(strings["watch.consent.decline"])
        XCTAssertNotNil(strings["watch.consent.withdraw"])
    }

    func testTheAppsOwnDisclosureCoversTheWatchMicrophone() {
        let settings = AppSettings(store: SharedSettings(defaults: UserDefaults(suiteName: "WatchVoiceLocalizationTests.\(UUID())")!))
        for language in AppLanguage.allCases {
            settings.setLanguage(language)
            for key in ["legal.consent.ai", "settings.privacy.body"] {
                XCTAssertTrue(settings.localized(key).contains("Apple Watch"), "\(key) in \(language.rawValue)")
            }
        }
    }

    /// The guide has fourteen steps and the screen lists them by number.
    func testTheGuideIsComplete() throws {
        let url = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
            .appendingPathComponent("AIReply/Resources/Localizable.xcstrings")
        let object = try JSONSerialization.jsonObject(with: Data(contentsOf: url)) as? [String: Any]
        let strings = try XCTUnwrap(object?["strings"] as? [String: Any])
        for step in 1...14 {
            XCTAssertNotNil(strings["watchGuide.\(step)"], "step \(step)")
        }
        for topic in ["install", "unavailable", "bluetooth", "keyboard", "fullAccess", "field", "internet", "language", "paste"] {
            XCTAssertNotNil(strings["watchHelp.\(topic)"], topic)
            XCTAssertNotNil(strings["watchHelp.\(topic).body"], topic)
        }
    }
}
