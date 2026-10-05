import XCTest
@testable import AIReply

/// Localization is a product requirement here, not a nicety: the bug this
/// iteration set out to fix was a Kazakh app showing an English keyboard row.
/// These tests fail if that can happen again.
final class LocalizationTests: XCTestCase {

    // MARK: Keyboard vocabulary

    /// The exact words the brief specifies, per language. If a translation is
    /// dropped or a fallback creeps in, this is where it is caught.
    func testKeyboardVocabularyMatchesTheBrief() {
        let english = AIReplyStrings.forLanguage(.english)
        XCTAssertEqual(english.insert, "Insert")
        XCTAssertEqual(english.regenerate, "Regenerate")
        XCTAssertEqual(english.cancel, "Cancel")
        XCTAssertEqual(english.replaceExisting, "Replace")
        XCTAssertEqual(english.appendToExisting, "Add")
        XCTAssertEqual(english.generating, "Generating…")
        XCTAssertEqual(english.noSourceMessage, "Copy a message first")

        let russian = AIReplyStrings.forLanguage(.russian)
        XCTAssertEqual(russian.insert, "Вставить")
        XCTAssertEqual(russian.regenerate, "Сгенерировать заново")
        XCTAssertEqual(russian.cancel, "Отмена")
        XCTAssertEqual(russian.replaceExisting, "Заменить")
        XCTAssertEqual(russian.appendToExisting, "Добавить")
        XCTAssertEqual(russian.generating, "Создаю ответ…")
        XCTAssertEqual(russian.noSourceMessage, "Сначала скопируйте сообщение")

        let kazakh = AIReplyStrings.forLanguage(.kazakh)
        XCTAssertEqual(kazakh.insert, "Кірістіру")
        XCTAssertEqual(kazakh.regenerate, "Қайта жасау")
        XCTAssertEqual(kazakh.cancel, "Бас тарту")
        XCTAssertEqual(kazakh.replaceExisting, "Ауыстыру")
        XCTAssertEqual(kazakh.appendToExisting, "Қосу")
        XCTAssertEqual(kazakh.generating, "Жауап дайындалуда…")
        XCTAssertEqual(kazakh.noSourceMessage, "Алдымен хабарламаны көшіріңіз")

        // The persona row's only action, ✨, is an icon: every language names
        // it for VoiceOver in its own words.
        for language in AppLanguage.allCases {
            XCTAssertFalse(AIReplyStrings.forLanguage(language).compose.createButtonAccessibility.isEmpty)
        }
        XCTAssertNotEqual(russian.compose.createButtonAccessibility, english.compose.createButtonAccessibility)
        XCTAssertNotEqual(kazakh.compose.createButtonAccessibility, english.compose.createButtonAccessibility)
        XCTAssertNotEqual(AIReplyStrings.forLanguage(.uzbek).compose.createButtonAccessibility,
                          english.compose.createButtonAccessibility)
    }

    func testTemplateNamesMatchTheBrief() {
        XCTAssertEqual(ReplyTemplate.builtIn(.friend, sortIndex: 0).displayName(appLanguage: .kazakh), "Дос")
        XCTAssertEqual(ReplyTemplate.builtIn(.client, sortIndex: 0).displayName(appLanguage: .kazakh), "Клиент")
        XCTAssertEqual(ReplyTemplate.builtIn(.business, sortIndex: 0).displayName(appLanguage: .kazakh), "Бизнес")
        XCTAssertEqual(ReplyTemplate.builtIn(.work, sortIndex: 0).displayName(appLanguage: .kazakh), "Жұмыс")
        XCTAssertEqual(ReplyTemplate.builtInName(.custom, language: .kazakh), "Басқа")

        XCTAssertEqual(ReplyTemplate.builtIn(.friend, sortIndex: 0).displayName(appLanguage: .russian), "Друг")
        XCTAssertEqual(ReplyTemplate.builtIn(.work, sortIndex: 0).displayName(appLanguage: .russian), "Работа")
        XCTAssertEqual(ReplyTemplate.builtInName(.custom, language: .russian), "Свой")

        XCTAssertEqual(ReplyTemplate.builtIn(.friend, sortIndex: 0).displayName(appLanguage: .english), "Friend")
        XCTAssertEqual(ReplyTemplate.builtIn(.friend, sortIndex: 0).displayName(appLanguage: .uzbek), "Do‘st")
    }

    /// No product string may be identical across all three languages unless it
    /// genuinely is a loan word. This is the test that would have failed when
    /// the keyboard was showing English labels in a Kazakh app.
    func testNoEnglishFallbackLeaksIntoOtherLanguages() {
        let english = AIReplyStrings.forLanguage(.english)
        let russian = AIReplyStrings.forLanguage(.russian)
        let kazakh = AIReplyStrings.forLanguage(.kazakh)

        for (label, en, ru, kk) in [
            ("insert", english.insert, russian.insert, kazakh.insert),
            ("regenerate", english.regenerate, russian.regenerate, kazakh.regenerate),
            ("generating", english.generating, russian.generating, kazakh.generating),
            ("cancel", english.cancel, russian.cancel, kazakh.cancel),
            ("noSourceMessage", english.noSourceMessage, russian.noSourceMessage, kazakh.noSourceMessage)
        ] {
            XCTAssertNotEqual(en, ru, "\(label) is untranslated in Russian")
            XCTAssertNotEqual(en, kk, "\(label) is untranslated in Kazakh")
        }
    }

    // MARK: Composer vocabulary

    /// The composer's own labels. The instruction placeholder is the single
    /// most important string in the new flow: it is what tells the user the
    /// field is for what THEY want said, not for the reply itself.
    func testComposerStringsAreTranslatedInEveryLanguage() {
        XCTAssertEqual(AIReplyStrings.forLanguage(.english).instructionPlaceholder, "How should I reply?")
        XCTAssertEqual(AIReplyStrings.forLanguage(.russian).instructionPlaceholder, "Как ответить?")
        XCTAssertEqual(AIReplyStrings.forLanguage(.kazakh).instructionPlaceholder, "Қалай жауап беру керек?")

        let english = AIReplyStrings.forLanguage(.english)
        for language in AppLanguage.allCases where language != .english {
            let strings = AIReplyStrings.forLanguage(language)
            for (label, own, en) in [
                ("instructionPlaceholder", strings.instructionPlaceholder, english.instructionPlaceholder),
                ("copiedMessage", strings.copiedMessage, english.copiedMessage),
                ("pasteMessage", strings.pasteMessage, english.pasteMessage),
                ("clearSource", strings.clearSource, english.clearSource),
                ("back", strings.back, english.back),
                ("editReply", strings.editReply, english.editReply),
                ("retry", strings.retry, english.retry)
            ] {
                XCTAssertFalse(own.isEmpty, "\(label) is empty in \(language.rawValue)")
                XCTAssertNotEqual(own, en, "\(label) is untranslated in \(language.rawValue)")
            }
        }
    }

    /// Quick intents are presets for the INSTRUCTION, so every language needs
    /// the same set - a Kazakh user must not silently get fewer options - and
    /// the phrase that lands in the field has to be in their language.
    func testQuickIntentsAreCompleteAndTranslated() {
        let english = AIReplyStrings.forLanguage(.english).quickIntents
        XCTAssertEqual(english.count, 8)
        let expected = english.map(\.id)

        for language in AppLanguage.allCases {
            let intents = AIReplyStrings.forLanguage(language).quickIntents
            XCTAssertEqual(intents.map(\.id), expected, "\(language.rawValue) has a different intent set")
            for intent in intents {
                XCTAssertFalse(intent.label.isEmpty, "\(intent.id) has no label in \(language.rawValue)")
                XCTAssertFalse(intent.phrase.isEmpty, "\(intent.id) has no phrase in \(language.rawValue)")
                // A pill has to fit a keyboard-width row next to Generate.
                XCTAssertLessThanOrEqual(intent.label.count, 18, "\(intent.id) label is too long")
            }
        }

        for language in AppLanguage.allCases where language != .english {
            let intents = AIReplyStrings.forLanguage(language).quickIntents
            for (own, en) in zip(intents, english) {
                XCTAssertNotEqual(own.phrase, en.phrase, "\(own.id) is untranslated in \(language.rawValue)")
            }
        }
    }

    /// One pill serves everyone, so no phrase may speak for the user in one
    /// gender («согласен» / «согласна»); the reply's wording follows the
    /// profile instead.
    func testQuickIntentPhrasesAreGenderNeutral() {
        let gendered = ["согласен", "согласна", "готов ", "готова", "рад ", "рада", "смог", "смогла",
                        "занят", "занята", "сделал", "сделала", "должен", "должна"]
        let russian = AIReplyStrings.forLanguage(.russian)
        let phrases = (russian.quickIntents + russian.compose.intents).map { $0.phrase.lowercased() + " " }
        for phrase in phrases {
            for word in gendered {
                XCTAssertFalse(phrase.contains(word), "\(phrase) speaks for the user as one gender")
            }
        }
    }

    // MARK: Language resolution

    func testAppLanguageMapsToTheRightLayoutVocabulary() {
        XCTAssertEqual(AppLanguage.kazakh.keyboardLanguage, .kazakh)
        XCTAssertEqual(AppLanguage.russian.keyboardLanguage, .russian)
        XCTAssertEqual(AppLanguage.english.keyboardLanguage, .english)
        XCTAssertEqual(AppLanguage.uzbek.keyboardLanguage, .english)
    }

    /// A user with a Kazakh app typing on the Russian layout must still see
    /// Kazakh product labels.
    ///
    /// The type system is what enforces this now: `AIReplyStrings` is keyed by
    /// `AppLanguage` and there is no way to reach it from a `KeyboardLanguage`,
    /// so a product label CANNOT be resolved from the layout by accident. This
    /// test pins the intent; `KeyboardStrings`, which owns the layout captions,
    /// lives in the extension target and is verified on device.
    func testProductStringsAreKeyedByAppLanguage() {
        for language in AppLanguage.allCases {
            let strings = AIReplyStrings.forLanguage(language)
            XCTAssertFalse(strings.insert.isEmpty)
        }
        XCTAssertEqual(AIReplyStrings.forLanguage(.kazakh).insert, "Кірістіру")
    }

    // MARK: Catalog completeness

    /// Every key compiled into the app must exist in every supported language.
    /// Reads the built `.lproj` tables rather than the source catalog, so it
    /// checks what actually ships.
    func testEveryStringIsTranslatedInEveryLanguage() throws {
        let tables = try ["en", "ru", "kk", "uz"].map { language -> (String, [String: String]) in
            let path = try XCTUnwrap(
                Bundle.main.path(forResource: "Localizable", ofType: "strings", inDirectory: "\(language).lproj"),
                "\(language).lproj/Localizable.strings is missing from the app bundle"
            )
            let table = try XCTUnwrap(NSDictionary(contentsOfFile: path) as? [String: String])
            return (language, table)
        }

        let englishKeys = Set(tables[0].1.keys)
        XCTAssertGreaterThan(englishKeys.count, 150, "the catalog looks truncated")

        for (language, table) in tables.dropFirst() {
            let missing = englishKeys.subtracting(table.keys)
            XCTAssertTrue(missing.isEmpty, "\(language) is missing: \(missing.sorted().prefix(10))")
        }
    }

    /// Strings built in code follow the language chosen in the app, not the
    /// phone's: whatever the simulator runs in, each choice reads its own.
    func testCodeBuiltStringsFollowTheChosenLanguage() {
        let settings = AppSettings(store: SharedSettings(defaults: UserDefaults(suiteName: "LocalizationTests.\(UUID())")!))
        let expected: [AppLanguage: String] = [.english: "Continue", .russian: "Далее", .kazakh: "Әрі қарай"]
        for (language, value) in expected {
            settings.setLanguage(language)
            XCTAssertEqual(settings.localized("onboarding.next"), value, language.rawValue)
        }
        settings.setLanguage(.russian)
        XCTAssertEqual(String(format: settings.localized("onboarding.step"), 2, 5), "Шаг 2 из 5")
    }

    /// Catches a key that was added to ru/kk by copying the English value.
    func testUserFacingScreensAreActuallyTranslated() throws {
        let suspects = [
            "setup.title", "setup.paste.body", "setup.fullAccess.why",
            "onboarding.keyboard.title", "onboarding.usage.title",
            "onboarding.gender.title", "onboarding.fullAccess.prompt", "onboarding.practice.title",
            "onboarding.sample.practice.incoming", "setup.status.keyboard.unknown",
            "settings.smartCorrection", "settings.smartCorrection.footer", "settings.tutorial",
            "profile.gender", "profile.gender.footer",
            "profile.role", "profile.rules", "templates.section.style",
            "settings.privacy.body", "home.privacy.body", "onboarding.fullAccess.warning",
            "profile.replyLanguage.auto", "profile.replyLanguage.footer",
            "voice.suggestions.title", "voice.suggestions.rules.footer", "voice.status.failed"
        ]

        func value(_ key: String, _ language: String) throws -> String {
            let path = try XCTUnwrap(
                Bundle.main.path(forResource: "Localizable", ofType: "strings", inDirectory: "\(language).lproj")
            )
            let table = try XCTUnwrap(NSDictionary(contentsOfFile: path) as? [String: String])
            return try XCTUnwrap(table[key], "\(key) missing in \(language)")
        }

        for key in suspects {
            let en = try value(key, "en")
            XCTAssertNotEqual(try value(key, "ru"), en, "\(key) is English in Russian")
            XCTAssertNotEqual(try value(key, "kk"), en, "\(key) is English in Kazakh")
            XCTAssertNotEqual(try value(key, "uz"), en, "\(key) is English in Uzbek")
        }
    }
}
