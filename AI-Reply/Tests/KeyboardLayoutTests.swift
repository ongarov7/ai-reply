import XCTest
@testable import AIReply

/// The layouts are transcribed from the iOS keyboards. These tests pin them,
/// so a "small tidy-up" cannot quietly move a letter a Kazakh user's thumb
/// already knows where to find.
final class KeyboardLayoutTests: XCTestCase {

    // MARK: Letters

    func testKazakhLettersMatchTheIOSLayout() {
        XCTAssertEqual(KeyboardLayout.letters(for: .kazakh), [
            ["ә", "і", "ң", "ғ", "ү", "ұ", "қ", "ө", "һ"],
            ["й", "ц", "у", "к", "е", "н", "г", "ш", "щ", "з", "х"],
            ["ф", "ы", "в", "а", "п", "р", "о", "л", "д", "ж", "э"],
            ["я", "ч", "с", "м", "и", "т", "ь", "б", "ю"]
        ])
    }

    func testRussianLettersMatchTheIOSLayout() {
        XCTAssertEqual(KeyboardLayout.letters(for: .russian), [
            ["й", "ц", "у", "к", "е", "н", "г", "ш", "щ", "з", "х"],
            ["ф", "ы", "в", "а", "п", "р", "о", "л", "д", "ж", "э"],
            ["я", "ч", "с", "м", "и", "т", "ь", "б", "ю"]
        ])
    }

    func testEnglishLettersAreQwerty() {
        XCTAssertEqual(KeyboardLayout.letters(for: .english), [
            ["q", "w", "e", "r", "t", "y", "u", "i", "o", "p"],
            ["a", "s", "d", "f", "g", "h", "j", "k", "l"],
            ["z", "x", "c", "v", "b", "n", "m"]
        ])
    }

    /// Every Kazakh letter is typeable, and `ё` / `ъ` live behind a long press
    /// exactly as on iOS instead of taking a key of their own.
    func testEveryCyrillicLetterIsReachable() {
        for language in [KeyboardLanguage.kazakh, .russian] {
            let face = Set(KeyboardLayout.letters(for: language).joined())
            XCTAssertFalse(face.contains("ё"), "\(language) shows ё on the face")
            XCTAssertFalse(face.contains("ъ"), "\(language) shows ъ on the face")
            XCTAssertEqual(KeyboardLayout.alternates(for: "е", language: language), ["ё"])
            XCTAssertEqual(KeyboardLayout.alternates(for: "ь", language: language), ["ъ"])
        }
        let kazakh = Set(KeyboardLayout.letters(for: .kazakh).joined())
        for letter in ["ә", "і", "ң", "ғ", "ү", "ұ", "қ", "ө", "һ"] {
            XCTAssertTrue(kazakh.contains(letter), "\(letter) is missing")
        }
    }

    // MARK: Numbers and symbols

    func testNumbersPlanes() {
        XCTAssertEqual(KeyboardLayout.numbers(for: .english)[1], ["-", "/", ":", ";", "(", ")", "$", "&", "@", "\""])
        // Tenge on the face for the market this keyboard is for.
        XCTAssertEqual(KeyboardLayout.numbers(for: .kazakh)[1], ["-", "/", ":", ";", "(", ")", "₸", "&", "@", "\""])
        XCTAssertEqual(KeyboardLayout.numbers(for: .russian)[1][6], "₸")
        XCTAssertEqual(KeyboardLayout.alternates(for: "₸", language: .kazakh).first, "₽")
        for language in KeyboardLanguage.allCases {
            let rows = KeyboardLayout.numbers(for: language)
            XCTAssertEqual(rows[0], ["1", "2", "3", "4", "5", "6", "7", "8", "9", "0"])
            XCTAssertEqual(rows[2], [".", ",", "?", "!", "'"])
        }
    }

    func testSymbolsPlanes() {
        XCTAssertEqual(KeyboardLayout.symbols(for: .english)[0], ["[", "]", "{", "}", "#", "%", "^", "*", "+", "="])
        XCTAssertEqual(KeyboardLayout.symbols(for: .english)[1], ["_", "\\", "|", "~", "<", ">", "€", "£", "¥", "•"])
        XCTAssertEqual(KeyboardLayout.symbols(for: .kazakh)[1], ["_", "\\", "|", "~", "<", ">", "$", "€", "£", "•"])
    }

    // MARK: Pages

    func testBottomRowFollowsTheField() {
        func actions(_ options: KeyboardPageOptions) -> [KeyAction] {
            KeyboardLayout.page(language: .kazakh, plane: .letters, options: options)
                .rows.last!.slots.compactMap(\.action)
        }
        XCTAssertEqual(actions(KeyboardPageOptions()), [.plane(.numbers), .nextLanguage, .space, .returnKey])
        XCTAssertEqual(
            actions(KeyboardPageOptions(showsNextKeyboardKey: true, showsLanguageKey: true)),
            [.plane(.numbers), .nextKeyboard, .nextLanguage, .space, .returnKey]
        )
        XCTAssertEqual(actions(KeyboardPageOptions(showsLanguageKey: false)), [.plane(.numbers), .space, .returnKey])
        XCTAssertTrue(actions(KeyboardPageOptions(field: .email)).contains(.character("@")))
        XCTAssertTrue(actions(KeyboardPageOptions(field: .url)).contains(.character("/")))
    }

    func testShiftAndDeleteAreOnTheLastLetterRow() {
        for language in KeyboardLanguage.allCases {
            let page = KeyboardLayout.page(language: language, plane: .letters, options: KeyboardPageOptions())
            let lastLetterRow = page.rows[page.rows.count - 2].slots.compactMap(\.action)
            XCTAssertEqual(lastLetterRow.first, .shift, "\(language)")
            XCTAssertEqual(lastLetterRow.last, .delete, "\(language)")
        }
    }

    func testKazakhNeedsFiveRowsAndTheOthersFour() {
        XCTAssertEqual(KeyboardLayout.rowCount(language: .kazakh, plane: .letters), 5)
        XCTAssertEqual(KeyboardLayout.rowCount(language: .russian, plane: .letters), 4)
        XCTAssertEqual(KeyboardLayout.rowCount(language: .english, plane: .letters), 4)
        XCTAssertEqual(KeyboardLayout.maximumRowCount(languages: [.russian, .english]), 4)
        XCTAssertEqual(KeyboardLayout.maximumRowCount(languages: KeyboardLanguage.cycleOrder), 5)
    }

    // MARK: Captions

    func testCaptionsFollowTheLayout() {
        XCTAssertEqual(KeyboardLabels(.kazakh).letters, "АӘБ")
        XCTAssertEqual(KeyboardLabels(.russian).letters, "АБВ")
        XCTAssertEqual(KeyboardLabels(.english).letters, "ABC")
        XCTAssertEqual(KeyboardLabels(.kazakh).space, "бос орын")
        XCTAssertEqual(KeyboardLabels(.russian).space, "Пробел")
        XCTAssertEqual(KeyboardLabels(.english).space, "space")
        XCTAssertEqual(KeyboardLabels(.russian).returnKey(for: .default), .text("Ввод"))
        if case .symbol(_, let spoken) = KeyboardLabels(.kazakh).returnKey(for: .default) {
            XCTAssertEqual(spoken, "Қайтару")
        } else {
            XCTFail("Kazakh return is the return arrow, as on iOS")
        }
    }

    // MARK: Switching

    func testLanguageKeyCyclesThroughEnabledLayoutsOnly() {
        XCTAssertEqual(KeyboardLanguage.kazakh.next(in: KeyboardLanguage.cycleOrder), .russian)
        XCTAssertEqual(KeyboardLanguage.russian.next(in: KeyboardLanguage.cycleOrder), .english)
        XCTAssertEqual(KeyboardLanguage.english.next(in: KeyboardLanguage.cycleOrder), .kazakh)
        XCTAssertEqual(KeyboardLanguage.kazakh.next(in: [.kazakh, .russian]), .russian)
        XCTAssertEqual(KeyboardLanguage.russian.next(in: [.kazakh, .russian]), .kazakh)
        // The current layout was switched off in the app: move on, never stall.
        XCTAssertEqual(KeyboardLanguage.english.next(in: [.kazakh, .russian]), .kazakh)
        XCTAssertEqual(KeyboardLanguage.kazakh.next(in: [.kazakh]), .kazakh)
    }

    func testEnabledLanguagesAreNeverEmpty() {
        let defaults = UserDefaults(suiteName: "KeyboardLayoutTests.\(UUID().uuidString)")!
        let settings = SharedSettings(defaults: defaults)
        XCTAssertEqual(KeyboardLanguageStore.enabledLanguages(settings: settings), KeyboardLanguage.cycleOrder)
        KeyboardLanguageStore.setEnabledLanguages([.english, .kazakh], settings: settings)
        XCTAssertEqual(KeyboardLanguageStore.enabledLanguages(settings: settings), [.kazakh, .english])
        KeyboardLanguageStore.setEnabledLanguages([], settings: settings)
        XCTAssertEqual(KeyboardLanguageStore.enabledLanguages(settings: settings), KeyboardLanguage.cycleOrder)
    }
}
