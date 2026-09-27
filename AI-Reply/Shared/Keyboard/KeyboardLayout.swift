import UIKit

// MARK: - Model

/// The three planes a layout has, exactly as on the iOS keyboard.
enum KeyboardPlane: Hashable, Sendable {
    case letters
    case numbers
    case symbols
}

/// What a key DOES. Pure data: the extension decides how it looks and where
/// its text goes, the tests pin it to the iOS layouts.
enum KeyAction: Hashable, Sendable {
    /// Types this text. Letters follow shift; everything else is typed as is.
    case character(String)
    case shift
    case delete
    case space
    case returnKey
    /// Letters / numbers / symbols.
    case plane(KeyboardPlane)
    /// This keyboard's own ҚАЗ / РУС / ENG switch.
    case nextLanguage
    /// The system globe: next keyboard on tap, the keyboard list on hold.
    case nextKeyboard

    var isCharacter: Bool {
        if case .character = self { return true }
        return false
    }
}

/// How wide a slot on a row is.
enum KeyWidth: Hashable, Sendable {
    /// Multiples of the row's key unit.
    case units(CGFloat)
    /// A share of whatever width is left once the fixed slots are placed.
    case flexible(CGFloat)
}

/// One position on a row: a key, or an empty spacer that only takes width.
/// Spacers draw nothing and own no touches - the neighbouring keys do.
struct KeySlot: Hashable, Sendable {
    let action: KeyAction?
    let width: KeyWidth
    /// Characters offered on a long press, in the order the popup shows them.
    let alternates: [String]

    static func key(_ action: KeyAction, _ width: KeyWidth = .units(1), alternates: [String] = []) -> KeySlot {
        KeySlot(action: action, width: width, alternates: alternates)
    }

    static func spacer(_ width: KeyWidth) -> KeySlot {
        KeySlot(action: nil, width: width, alternates: [])
    }
}

struct KeyRow: Hashable, Sendable {
    var slots: [KeySlot]
    /// Columns the row's key unit is derived from. nil means the page's own
    /// grid. The bottom row uses the 10-column Latin grid on every layout, so
    /// 123, the globe and return keep one size whatever language is up.
    var unitColumns: Int?

    init(_ slots: [KeySlot], unitColumns: Int? = nil) {
        self.slots = slots
        self.unitColumns = unitColumns
    }
}

/// A complete page: every row, including the bottom one.
struct KeyboardPageSpec: Hashable, Sendable {
    let language: KeyboardLanguage
    let plane: KeyboardPlane
    /// Columns of the page's grid: 10 for Latin, numbers and symbols; 11 for
    /// the Cyrillic layouts.
    let columns: Int
    let rows: [KeyRow]

    var characterRows: [[String]] {
        rows.map { row in
            row.slots.compactMap { slot -> String? in
                if case .character(let value)? = slot.action { return value }
                return nil
            }
        }
        .filter { !$0.isEmpty }
    }
}

/// What the text field is for, as far as the bottom row is concerned.
enum KeyboardFieldKind: Hashable, Sendable {
    case text
    case email
    case url
    case social

    init(_ type: UIKeyboardType) {
        switch type {
        case .emailAddress: self = .email
        case .URL: self = .url
        case .twitter: self = .social
        default: self = .text
        }
    }

    /// Fields where iOS opens on the numbers plane.
    static func startsOnNumbers(_ type: UIKeyboardType) -> Bool {
        switch type {
        case .numberPad, .decimalPad, .asciiCapableNumberPad, .numbersAndPunctuation, .phonePad, .namePhonePad:
            return true
        default:
            return false
        }
    }
}

struct KeyboardPageOptions: Hashable, Sendable {
    /// `needsInputModeSwitchKey`: iPhones with a Home button, and any host that
    /// hides the system globe, need the keyboard to draw its own.
    var showsNextKeyboardKey: Bool = false
    /// The ҚАЗ / РУС / ENG key. Hidden when only one layout is switched on.
    var showsLanguageKey: Bool = true
    var field: KeyboardFieldKind = .text
}

// MARK: - Layouts

/// The layouts, transcribed from the iOS keyboards (iOS 18) rather than
/// invented:
///
/// * English - QWERTY 10 / 9 / 7, the middle row inset by half a key, shift and
///   delete one and a third keys wide.
/// * Russian - ЙЦУКЕН on an 11-column grid: 11 / 11 / shift + 9 + delete. `ё`
///   and `ъ` are not on the face of the keyboard; they are long-press
///   alternates of `е` and `ь`, as on iOS.
/// * Kazakh - the Russian rows under a top row of the nine Kazakh letters in
///   Apple's order, `ә і ң ғ ү ұ қ ө һ`, centred on the same grid with keys the
///   same size as every other row.
///
/// Numbers and symbols follow the iOS planes for each language, including the
/// Russian and Kazakh currency key; `₸` is the first long-press alternate on
/// every currency key.
enum KeyboardLayout {

    static let englishLetters: [[String]] = [
        ["q", "w", "e", "r", "t", "y", "u", "i", "o", "p"],
        ["a", "s", "d", "f", "g", "h", "j", "k", "l"],
        ["z", "x", "c", "v", "b", "n", "m"]
    ]

    static let russianLetters: [[String]] = [
        ["й", "ц", "у", "к", "е", "н", "г", "ш", "щ", "з", "х"],
        ["ф", "ы", "в", "а", "п", "р", "о", "л", "д", "ж", "э"],
        ["я", "ч", "с", "м", "и", "т", "ь", "б", "ю"]
    ]

    static let kazakhTopRow: [String] = ["ә", "і", "ң", "ғ", "ү", "ұ", "қ", "ө", "һ"]

    static var kazakhLetters: [[String]] { [kazakhTopRow] + russianLetters }

    static func letters(for language: KeyboardLanguage) -> [[String]] {
        switch language {
        case .english: return englishLetters
        case .russian: return russianLetters
        case .kazakh:  return kazakhLetters
        }
    }

    static func numbers(for language: KeyboardLanguage) -> [[String]] {
        let currency = language == .english ? "$" : "₽"
        return [
            ["1", "2", "3", "4", "5", "6", "7", "8", "9", "0"],
            ["-", "/", ":", ";", "(", ")", currency, "&", "@", "\""],
            [".", ",", "?", "!", "'"]
        ]
    }

    static func symbols(for language: KeyboardLanguage) -> [[String]] {
        let secondRow: [String] = language == .english
            ? ["_", "\\", "|", "~", "<", ">", "€", "£", "¥", "•"]
            : ["_", "\\", "|", "~", "<", ">", "$", "€", "£", "•"]
        return [
            ["[", "]", "{", "}", "#", "%", "^", "*", "+", "="],
            secondRow,
            [".", ",", "?", "!", "'"]
        ]
    }

    /// Rows on the letters page, including the bottom row.
    static func rowCount(language: KeyboardLanguage, plane: KeyboardPlane) -> Int {
        switch plane {
        case .letters: return letters(for: language).count + 1
        case .numbers, .symbols: return 4
        }
    }

    /// The most rows any page of these layouts has. Every page is laid out into
    /// the height this many rows need, so the keyboard never changes height
    /// when the user switches planes or languages.
    static func maximumRowCount(languages: [KeyboardLanguage]) -> Int {
        let all = languages.isEmpty ? KeyboardLanguage.cycleOrder : languages
        return all.map { rowCount(language: $0, plane: .letters) }.max() ?? 4
    }

    // MARK: Pages

    static func page(language: KeyboardLanguage, plane: KeyboardPlane, options: KeyboardPageOptions) -> KeyboardPageSpec {
        switch plane {
        case .letters:
            return lettersPage(language: language, options: options)
        case .numbers:
            return punctuationPage(language: language, plane: .numbers, rows: numbers(for: language), options: options)
        case .symbols:
            return punctuationPage(language: language, plane: .symbols, rows: symbols(for: language), options: options)
        }
    }

    /// Shift and delete on the Latin layout: one and a third keys wide, with
    /// the letters centred between them.
    static let latinModifierWidth: CGFloat = 4.0 / 3.0

    private static func lettersPage(language: KeyboardLanguage, options: KeyboardPageOptions) -> KeyboardPageSpec {
        let rows = letters(for: language)
        var result: [KeyRow] = []

        switch language {
        case .english:
            result.append(KeyRow(characterSlots(rows[0], language: language)))
            result.append(KeyRow([.spacer(.flexible(1))] + characterSlots(rows[1], language: language) + [.spacer(.flexible(1))]))
            result.append(KeyRow(
                [.key(.shift, .units(latinModifierWidth)), .spacer(.flexible(1))]
                    + characterSlots(rows[2], language: language)
                    + [.spacer(.flexible(1)), .key(.delete, .units(latinModifierWidth))]
            ))

        case .russian, .kazakh:
            let last = rows.count - 1
            for (index, row) in rows.enumerated() {
                let keys = characterSlots(row, language: language)
                if index == last {
                    result.append(KeyRow([.key(.shift)] + keys + [.key(.delete)]))
                } else if row.count < 11 {
                    // The Kazakh row: nine keys centred on the eleven-column
                    // grid, the same size as the keys below them.
                    result.append(KeyRow([.spacer(.flexible(1))] + keys + [.spacer(.flexible(1))]))
                } else {
                    result.append(KeyRow(keys))
                }
            }
        }

        result.append(bottomRow(planeKey: .numbers, options: options))
        return KeyboardPageSpec(language: language, plane: .letters, columns: language == .english ? 10 : 11, rows: result)
    }

    private static func punctuationPage(
        language: KeyboardLanguage,
        plane: KeyboardPlane,
        rows: [[String]],
        options: KeyboardPageOptions
    ) -> KeyboardPageSpec {
        let toggle: KeyboardPlane = plane == .numbers ? .symbols : .numbers
        let result: [KeyRow] = [
            KeyRow(characterSlots(rows[0], language: language)),
            KeyRow(characterSlots(rows[1], language: language)),
            KeyRow(
                [.key(.plane(toggle), .units(latinModifierWidth)), .spacer(.units(0.35))]
                    + rows[2].map { .key(.character($0), .flexible(1), alternates: alternates(for: $0, language: language)) }
                    + [.spacer(.units(0.35)), .key(.delete, .units(latinModifierWidth))]
            ),
            bottomRow(planeKey: .letters, options: options)
        ]
        return KeyboardPageSpec(language: language, plane: plane, columns: 10, rows: result)
    }

    private static func characterSlots(_ row: [String], language: KeyboardLanguage) -> [KeySlot] {
        row.map { .key(.character($0), alternates: alternates(for: $0, language: language)) }
    }

    /// 123 / ABC, the globe when iOS asks for one, the layout key, the field's
    /// own extras, space and return. Sized on the Latin grid so it is identical
    /// on every layout.
    private static func bottomRow(planeKey: KeyboardPlane, options: KeyboardPageOptions) -> KeyRow {
        let control = KeyWidth.units(latinModifierWidth)
        var slots: [KeySlot] = [.key(.plane(planeKey), control)]
        if options.showsNextKeyboardKey { slots.append(.key(.nextKeyboard, control)) }
        if options.showsLanguageKey { slots.append(.key(.nextLanguage, control)) }

        switch options.field {
        case .text:
            slots.append(.key(.space, .flexible(1)))
        case .email:
            slots.append(.key(.space, .flexible(1)))
            slots.append(.key(.character("@")))
            slots.append(.key(.character("."), alternates: [".com", ".kz", ".ru"]))
        case .url:
            slots.append(.key(.character("/")))
            slots.append(.key(.space, .flexible(1)))
            slots.append(.key(.character("."), alternates: [".com", ".kz", ".ru"]))
        case .social:
            slots.append(.key(.character("@")))
            slots.append(.key(.space, .flexible(1)))
            slots.append(.key(.character("#")))
        }

        slots.append(.key(.returnKey, .units(2.75)))
        return KeyRow(slots, unitColumns: 10)
    }

    // MARK: Long press

    /// Long-press alternatives, following iOS: `ё` behind `е` and `ъ` behind `ь`
    /// on the Cyrillic layouts, accented letters on the Latin one, and the
    /// typographic variants on the punctuation planes.
    static func alternates(for character: String, language: KeyboardLanguage) -> [String] {
        switch language {
        case .russian, .kazakh:
            if let cyrillic = cyrillicAlternates[character] { return cyrillic }
        case .english:
            if let latin = latinAlternates[character] { return latin }
        }
        return punctuationAlternates[character] ?? []
    }

    private static let cyrillicAlternates: [String: [String]] = [
        "е": ["ё"],
        "ь": ["ъ"]
    ]

    private static let latinAlternates: [String: [String]] = [
        "a": ["à", "á", "â", "ä", "æ", "ã", "å", "ā"],
        "c": ["ç", "ć", "č"],
        "e": ["è", "é", "ê", "ë", "ē", "ė", "ę"],
        "i": ["î", "ï", "í", "ī", "į", "ì"],
        "l": ["ł"],
        "n": ["ñ", "ń"],
        "o": ["ô", "ö", "ò", "ó", "œ", "ø", "ō", "õ"],
        "s": ["ß", "ś", "š"],
        "u": ["û", "ü", "ù", "ú", "ū"],
        "y": ["ÿ"],
        "z": ["ž", "ź", "ż"]
    ]

    private static let punctuationAlternates: [String: [String]] = [
        "0": ["°"],
        "-": ["–", "—", "•"],
        "/": ["\\"],
        "$": ["₸", "₽", "€", "£", "¥", "¢", "₩"],
        "₽": ["₸", "$", "€", "£", "¥"],
        "€": ["₸", "$", "₽", "£", "¥"],
        "&": ["§"],
        ".": ["…"],
        "?": ["¿"],
        "!": ["¡"],
        "'": ["‘", "’", "`"],
        "\"": ["„", "“", "”", "«", "»"],
        "%": ["‰"]
    ]
}

// MARK: - Captions

/// What the keys say, keyed by the LAYOUT - a user typing Kazakh sees
/// "бос орын" on the space bar whatever language the app is in.
struct KeyboardLabels: Sendable {

    let language: KeyboardLanguage

    init(_ language: KeyboardLanguage) {
        self.language = language
    }

    var space: String {
        switch language {
        case .english: return "space"
        case .russian: return "Пробел"
        case .kazakh:  return "бос орын"
        }
    }

    /// The key that goes back to letters from numbers or symbols.
    var letters: String {
        switch language {
        case .english: return "ABC"
        case .russian: return "АБВ"
        case .kazakh:  return "АӘБ"
        }
    }

    var numbers: String { "123" }
    var symbols: String { "#+=" }

    func planeKey(_ plane: KeyboardPlane) -> String {
        switch plane {
        case .letters: return letters
        case .numbers: return numbers
        case .symbols: return symbols
        }
    }

    /// What the layout key shows: the layout currently up.
    var badge: String {
        switch language {
        case .english: return "ENG"
        case .russian: return "РУС"
        case .kazakh:  return "ҚАЗ"
        }
    }

    // MARK: Return key

    enum ReturnFace: Equatable, Sendable {
        case text(String)
        /// An SF Symbol, with the word VoiceOver reads for it.
        case symbol(String, accessibilityLabel: String)
    }

    /// The return key, following the host field's `returnKeyType` the way the
    /// system keyboard does. Kazakh shows the return arrow for a plain return,
    /// as iOS does.
    func returnKey(for type: UIReturnKeyType) -> ReturnFace {
        switch language {
        case .english:
            switch type {
            case .go: return .text("go")
            case .google, .yahoo, .search: return .text("search")
            case .join: return .text("join")
            case .next: return .text("next")
            case .route: return .text("route")
            case .send: return .text("send")
            case .done: return .text("done")
            case .emergencyCall: return .text("Emergency")
            case .continue: return .text("continue")
            default: return .text("return")
            }
        case .russian:
            switch type {
            case .go: return .text("Перейти")
            case .google, .yahoo, .search: return .text("Найти")
            case .join: return .text("Войти")
            case .next: return .text("Далее")
            case .route: return .text("Маршрут")
            case .send: return .text("Отправить")
            case .done: return .text("Готово")
            case .emergencyCall: return .text("SOS")
            case .continue: return .text("Далее")
            default: return .text("Ввод")
            }
        case .kazakh:
            switch type {
            case .go: return .text("Өту")
            case .google, .yahoo, .search: return .text("Іздеу")
            case .join: return .text("Кіру")
            case .next: return .text("Келесі")
            case .route: return .text("Бағыт")
            case .send: return .text("Жіберу")
            case .done: return .text("Дайын")
            case .emergencyCall: return .text("SOS")
            case .continue: return .text("Жалғастыру")
            default: return .symbol("return.left", accessibilityLabel: "Қайтару")
            }
        }
    }

    /// Whether the return key is tinted, as the system keyboard tints the keys
    /// that confirm an action.
    static func returnKeyIsProminent(_ type: UIReturnKeyType) -> Bool {
        switch type {
        case .go, .google, .yahoo, .search, .join, .route, .send, .done, .emergencyCall, .continue:
            return true
        default:
            return false
        }
    }

    // MARK: Accessibility

    var shiftAccessibility: String {
        switch language {
        case .english: return "shift"
        case .russian: return "Шифт"
        case .kazakh:  return "Шифт"
        }
    }

    var capsLockAccessibility: String {
        switch language {
        case .english: return "caps lock"
        case .russian: return "Caps Lock"
        case .kazakh:  return "Caps Lock"
        }
    }

    var deleteAccessibility: String {
        switch language {
        case .english: return "delete"
        case .russian: return "Удалить"
        case .kazakh:  return "Өшіру"
        }
    }

    var nextKeyboardAccessibility: String {
        switch language {
        case .english: return "Next keyboard"
        case .russian: return "Следующая клавиатура"
        case .kazakh:  return "Келесі пернетақта"
        }
    }

    var nextLanguageAccessibility: String {
        switch language {
        case .english: return "Switch layout"
        case .russian: return "Сменить раскладку"
        case .kazakh:  return "Тілді ауыстыру"
        }
    }
}
