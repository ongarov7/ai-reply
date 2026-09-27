package kz.yerek.aireply.keyboard.layout

import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.core.lang.KeyboardPlane
import kz.yerek.aireply.keyboard.KeyboardKey

// ------------------------------------------------------------------- model

/** How wide a slot on a row is. */
sealed interface KeyWidth {
    /** Multiples of the row's key unit. */
    data class Units(val units: Float) : KeyWidth

    /** A share of whatever width is left once the fixed slots are placed. */
    data class Flexible(val weight: Float) : KeyWidth
}

/**
 * One position on a row: a key, or an empty spacer that only takes width.
 * Spacers draw nothing and own no touches - the neighbouring keys do.
 */
data class KeySlot(
    val key: KeyboardKey?,
    val width: KeyWidth = ONE,
    /** Offered on a long press, in the order the panel shows them. */
    val alternates: List<String> = emptyList(),
    /** The small caption in the corner: what a long press types first. */
    val hint: String? = null
) {
    companion object {
        val ONE = KeyWidth.Units(1f)
        fun spacer(width: KeyWidth) = KeySlot(null, width)
    }
}

data class KeyRow(
    val slots: List<KeySlot>,
    /**
     * Columns the row's key unit is derived from; null means the page's own
     * grid. The bottom row uses the 10-column grid on every layout, so ?123,
     * space and return keep one size whatever language is up.
     */
    val unitColumns: Int? = null
)

/** A complete page: every row, the bottom one included. */
data class KeyboardPage(
    val language: KeyboardLanguage,
    val plane: KeyboardPlane,
    /** 10 for Latin, numbers and symbols; 11 for the Cyrillic layouts. */
    val columns: Int,
    val rows: List<KeyRow>
) {
    val characterRows: List<List<String>>
        get() = rows.map { row ->
            row.slots.mapNotNull { (it.key as? KeyboardKey.Character)?.value }
        }.filter { it.isNotEmpty() }
}

/** What the field is for, as far as the bottom row is concerned. */
enum class FieldKind { TEXT, EMAIL, URL }

data class PageOptions(
    /** The system keyboard switch, shown when Android says to offer one. */
    val showsGlobeKey: Boolean = false,
    /** ҚАЗ / РУС / ENG. Hidden when only one layout is switched on. */
    val showsLanguageKey: Boolean = true,
    val field: FieldKind = FieldKind.TEXT
)

// ----------------------------------------------------------------- layouts

/**
 * The layouts, following Gboard and the national standards rather than
 * invented:
 *
 *  * English - QWERTY 10 / 9 / 7, the middle row inset by half a key, shift
 *    and delete one and a half keys wide.
 *  * Russian - ЙЦУКЕН on an 11-column grid: 11 / 11 / shift + 9 + delete.
 *    `ё` and `ъ` are long presses on `е` and `ь`, as on Gboard.
 *  * Kazakh - the Russian rows under a row of the nine Kazakh letters in the
 *    standard order `ә і ң ғ ү ұ қ ө һ`, centred on the same grid with keys
 *    the same size as every other row.
 *
 * The top letter row carries the digits on a long press, Gboard style, with
 * the digit drawn small in the key's corner.
 *
 * ?123 and =\< are Gboard's two punctuation pages. The currency key is `₸` on
 * the Kazakh and Russian layouts and `$` on the English one; the other
 * currencies are a long press away.
 */
object KeyboardLayout {

    val ENGLISH_LETTERS: List<List<String>> = listOf(
        listOf("q", "w", "e", "r", "t", "y", "u", "i", "o", "p"),
        listOf("a", "s", "d", "f", "g", "h", "j", "k", "l"),
        listOf("z", "x", "c", "v", "b", "n", "m")
    )

    val RUSSIAN_LETTERS: List<List<String>> = listOf(
        listOf("й", "ц", "у", "к", "е", "н", "г", "ш", "щ", "з", "х"),
        listOf("ф", "ы", "в", "а", "п", "р", "о", "л", "д", "ж", "э"),
        listOf("я", "ч", "с", "м", "и", "т", "ь", "б", "ю")
    )

    val KAZAKH_TOP_ROW: List<String> = listOf("ә", "і", "ң", "ғ", "ү", "ұ", "қ", "ө", "һ")

    val KAZAKH_LETTERS: List<List<String>> = listOf(KAZAKH_TOP_ROW) + RUSSIAN_LETTERS

    /** Shift and delete on the Latin rows, and the plane / return keys. */
    const val MODIFIER_UNITS = 1.5f

    private const val DIGITS = "1234567890"

    fun letters(language: KeyboardLanguage): List<List<String>> = when (language) {
        KeyboardLanguage.ENGLISH -> ENGLISH_LETTERS
        KeyboardLanguage.RUSSIAN -> RUSSIAN_LETTERS
        KeyboardLanguage.KAZAKH -> KAZAKH_LETTERS
    }

    fun currency(language: KeyboardLanguage): String =
        if (language == KeyboardLanguage.ENGLISH) "$" else "₸"

    /** Gboard's ?123 page, without its bottom row. */
    fun numbers(language: KeyboardLanguage): List<List<String>> = listOf(
        listOf("1", "2", "3", "4", "5", "6", "7", "8", "9", "0"),
        listOf("@", "#", currency(language), "_", "&", "-", "+", "(", ")", "/"),
        listOf("*", "\"", "'", ":", ";", "!", "?")
    )

    /** Gboard's =\< page, without its bottom row. */
    fun symbols(language: KeyboardLanguage): List<List<String>> = listOf(
        listOf("~", "`", "|", "•", "√", "π", "÷", "×", "¶", "∆"),
        listOf("£", if (language == KeyboardLanguage.ENGLISH) "¢" else "$", "€", "¥", "^", "°", "=", "{", "}", "\\"),
        listOf("%", "©", "®", "™", "✓", "[", "]")
    )

    /** Rows on a page, the bottom row included. */
    fun rowCount(language: KeyboardLanguage, plane: KeyboardPlane): Int = when (plane) {
        KeyboardPlane.LETTERS -> letters(language).size + 1
        KeyboardPlane.NUMBERS, KeyboardPlane.SYMBOLS -> 4
    }

    /**
     * The most rows any page of these layouts has. Every page is laid out
     * into the height this many rows need, so the keyboard never changes
     * height when the user switches planes or languages.
     */
    fun maximumRowCount(languages: Collection<KeyboardLanguage>): Int {
        val all = languages.ifEmpty { KeyboardLanguage.CYCLE_ORDER }
        return all.maxOf { rowCount(it, KeyboardPlane.LETTERS) }
    }

    // --------------------------------------------------------------- pages

    fun page(language: KeyboardLanguage, plane: KeyboardPlane, options: PageOptions): KeyboardPage =
        when (plane) {
            KeyboardPlane.LETTERS -> lettersPage(language, options)
            KeyboardPlane.NUMBERS -> punctuationPage(language, plane, numbers(language), options)
            KeyboardPlane.SYMBOLS -> punctuationPage(language, plane, symbols(language), options)
        }

    private fun lettersPage(language: KeyboardLanguage, options: PageOptions): KeyboardPage {
        val rows = letters(language)
        val result = mutableListOf<KeyRow>()
        val edge = KeySlot.spacer(KeyWidth.Flexible(1f))

        when (language) {
            KeyboardLanguage.ENGLISH -> {
                val modifier = KeyWidth.Units(MODIFIER_UNITS)
                result += KeyRow(characterSlots(rows[0], language, withDigits = true))
                result += KeyRow(listOf(edge) + characterSlots(rows[1], language) + edge)
                result += KeyRow(
                    listOf(KeySlot(KeyboardKey.Shift, modifier), edge) +
                        characterSlots(rows[2], language) +
                        listOf(edge, KeySlot(KeyboardKey.Backspace, modifier))
                )
            }

            KeyboardLanguage.RUSSIAN, KeyboardLanguage.KAZAKH -> {
                // Digits ride on the ЙЦУКЕН row: it is the top row in
                // Russian and the one under the Kazakh letters in Kazakh.
                val digitRow = if (language == KeyboardLanguage.KAZAKH) 1 else 0
                rows.forEachIndexed { index, row ->
                    val keys = characterSlots(row, language, withDigits = index == digitRow)
                    result += when {
                        index == rows.lastIndex ->
                            KeyRow(listOf(KeySlot(KeyboardKey.Shift)) + keys + KeySlot(KeyboardKey.Backspace))
                        // The Kazakh row: nine keys centred on the eleven-column
                        // grid, the same size as the keys below them.
                        row.size < 11 -> KeyRow(listOf(edge) + keys + edge)
                        else -> KeyRow(keys)
                    }
                }
            }
        }

        result += bottomRow(language, KeyboardPlane.NUMBERS, options)
        return KeyboardPage(
            language = language,
            plane = KeyboardPlane.LETTERS,
            columns = if (language == KeyboardLanguage.ENGLISH) 10 else 11,
            rows = result
        )
    }

    private fun punctuationPage(
        language: KeyboardLanguage,
        plane: KeyboardPlane,
        rows: List<List<String>>,
        options: PageOptions
    ): KeyboardPage {
        val toggle = if (plane == KeyboardPlane.NUMBERS) KeyboardPlane.SYMBOLS else KeyboardPlane.NUMBERS
        val modifier = KeyWidth.Units(MODIFIER_UNITS)
        val edge = KeySlot.spacer(KeyWidth.Flexible(1f))
        val result = listOf(
            KeyRow(characterSlots(rows[0], language)),
            KeyRow(characterSlots(rows[1], language)),
            KeyRow(
                listOf(KeySlot(KeyboardKey.Plane(toggle), modifier), edge) +
                    characterSlots(rows[2], language) +
                    listOf(edge, KeySlot(KeyboardKey.Backspace, modifier))
            ),
            bottomRow(language, KeyboardPlane.LETTERS, options)
        )
        return KeyboardPage(language, plane, columns = 10, rows = result)
    }

    private fun characterSlots(
        row: List<String>,
        language: KeyboardLanguage,
        withDigits: Boolean = false
    ): List<KeySlot> = row.mapIndexed { index, character ->
        val digit = if (withDigits && index < DIGITS.length) DIGITS[index].toString() else null
        val specific = alternates(character, language)
        KeySlot(
            key = KeyboardKey.Character(character),
            // The language's own letters come first: `ё` matters more to a
            // Russian speaker than the `5` that ?123 also has.
            alternates = if (digit == null) specific else specific + digit,
            hint = digit
        )
    }

    /**
     * ?123 / ABC, the comma (or the globe when Android asks for one), the
     * layout key, space, the full stop and return - sized on the 10-column
     * grid so the row is identical on every layout.
     */
    private fun bottomRow(language: KeyboardLanguage, planeKey: KeyboardPlane, options: PageOptions): KeyRow {
        val slots = mutableListOf(KeySlot(KeyboardKey.Plane(planeKey), KeyWidth.Units(MODIFIER_UNITS)))

        // The left of space holds one key: the system keyboard switch when
        // Android wants one offered, otherwise the field's own character.
        if (options.showsGlobeKey) {
            slots += KeySlot(KeyboardKey.Globe)
        } else {
            val extra = when (options.field) {
                FieldKind.TEXT -> ","
                FieldKind.EMAIL -> "@"
                FieldKind.URL -> "/"
            }
            slots += KeySlot(KeyboardKey.Character(extra), alternates = alternates(extra, language))
        }
        if (options.showsLanguageKey) slots += KeySlot(KeyboardKey.Layout, KeyWidth.Units(1.25f))

        slots += KeySlot(KeyboardKey.Space, KeyWidth.Flexible(1f))

        val stopAlternates = when (options.field) {
            FieldKind.TEXT -> listOf(",", "?", "!", "…", ":", ";", "'", "\"", "-")
            FieldKind.EMAIL, FieldKind.URL -> listOf(".com", ".kz", ".ru")
        }
        slots += KeySlot(KeyboardKey.Character("."), alternates = stopAlternates)
        slots += KeySlot(KeyboardKey.Return, KeyWidth.Units(MODIFIER_UNITS))
        return KeyRow(slots, unitColumns = 10)
    }

    // ---------------------------------------------------------- long press

    /**
     * Long-press alternatives: `ё` behind `е` and `ъ` behind `ь` on the
     * Cyrillic layouts, accented letters on the Latin one, typographic
     * variants and other currencies on the punctuation pages.
     */
    fun alternates(character: String, language: KeyboardLanguage): List<String> {
        val specific = when (language) {
            KeyboardLanguage.RUSSIAN, KeyboardLanguage.KAZAKH -> CYRILLIC_ALTERNATES[character]
            KeyboardLanguage.ENGLISH -> LATIN_ALTERNATES[character]
        }
        return specific ?: PUNCTUATION_ALTERNATES[character].orEmpty()
    }

    private val CYRILLIC_ALTERNATES = mapOf(
        "е" to listOf("ё"),
        "ь" to listOf("ъ")
    )

    private val LATIN_ALTERNATES = mapOf(
        "a" to listOf("à", "á", "â", "ä", "æ", "ã", "å", "ā"),
        "c" to listOf("ç", "ć", "č"),
        "e" to listOf("è", "é", "ê", "ë", "ē", "ė", "ę"),
        "i" to listOf("î", "ï", "í", "ī", "į", "ì"),
        "l" to listOf("ł"),
        "n" to listOf("ñ", "ń"),
        "o" to listOf("ô", "ö", "ò", "ó", "œ", "ø", "ō", "õ"),
        "s" to listOf("ß", "ś", "š"),
        "u" to listOf("û", "ü", "ù", "ú", "ū"),
        "y" to listOf("ÿ"),
        "z" to listOf("ž", "ź", "ż")
    )

    private val PUNCTUATION_ALTERNATES = mapOf(
        "0" to listOf("°", "ⁿ", "∅"),
        "1" to listOf("¹", "½", "⅓", "¼", "⅛"),
        "2" to listOf("²", "⅔"),
        "3" to listOf("³", "¾", "⅜"),
        "-" to listOf("–", "—", "·"),
        "/" to listOf("\\"),
        "$" to listOf("₸", "₽", "€", "£", "¥", "¢"),
        "₸" to listOf("₽", "$", "€", "£", "¥"),
        "€" to listOf("₸", "$", "₽", "£", "¥"),
        "&" to listOf("§"),
        "?" to listOf("¿"),
        "!" to listOf("¡"),
        "'" to listOf("‘", "’", "`"),
        "\"" to listOf("„", "“", "”", "«", "»"),
        "%" to listOf("‰"),
        "*" to listOf("†", "‡", "★"),
        "(" to listOf("<", "[", "{"),
        ")" to listOf(">", "]", "}"),
        "+" to listOf("±"),
        "=" to listOf("≠", "≈", "∞"),
        "@" to listOf("#")
    )
}

// ---------------------------------------------------------------- captions

/** What the return key shows, following the host field's IME action. */
enum class ReturnFace { NEWLINE, SEND, SEARCH, GO, NEXT, PREVIOUS, DONE }

/**
 * What the keys say, keyed by the LAYOUT: a user typing Kazakh sees Kazakh
 * captions whatever language the app is in.
 */
data class KeyboardLabels(val language: KeyboardLanguage) {

    /** Gboard shows the layout's name on the space bar. */
    val space: String get() = language.nativeName

    /** The key that goes back to letters from ?123 or =\<. */
    val letters: String
        get() = when (language) {
            KeyboardLanguage.ENGLISH -> "ABC"
            KeyboardLanguage.RUSSIAN -> "АБВ"
            KeyboardLanguage.KAZAKH -> "АӘБ"
        }

    fun planeKey(plane: KeyboardPlane): String = when (plane) {
        KeyboardPlane.LETTERS -> letters
        KeyboardPlane.NUMBERS -> "?123"
        KeyboardPlane.SYMBOLS -> "=\\<"
    }

    val badge: String get() = language.badge

    // Accessibility.

    val shift: String
        get() = when (language) {
            KeyboardLanguage.ENGLISH -> "Shift"
            KeyboardLanguage.RUSSIAN, KeyboardLanguage.KAZAKH -> "Шифт"
        }

    val capsLock: String get() = "Caps Lock"

    val delete: String
        get() = when (language) {
            KeyboardLanguage.ENGLISH -> "Delete"
            KeyboardLanguage.RUSSIAN -> "Удалить"
            KeyboardLanguage.KAZAKH -> "Өшіру"
        }

    val spaceDescription: String
        get() = when (language) {
            KeyboardLanguage.ENGLISH -> "Space"
            KeyboardLanguage.RUSSIAN -> "Пробел"
            KeyboardLanguage.KAZAKH -> "Бос орын"
        }

    val nextKeyboard: String
        get() = when (language) {
            KeyboardLanguage.ENGLISH -> "Next keyboard"
            KeyboardLanguage.RUSSIAN -> "Следующая клавиатура"
            KeyboardLanguage.KAZAKH -> "Келесі пернетақта"
        }

    val switchLayout: String
        get() = when (language) {
            KeyboardLanguage.ENGLISH -> "Switch layout"
            KeyboardLanguage.RUSSIAN -> "Сменить раскладку"
            KeyboardLanguage.KAZAKH -> "Тілді ауыстыру"
        }

    fun returnDescription(face: ReturnFace): String = when (language) {
        KeyboardLanguage.ENGLISH -> when (face) {
            ReturnFace.NEWLINE -> "Return"
            ReturnFace.SEND -> "Send"
            ReturnFace.SEARCH -> "Search"
            ReturnFace.GO -> "Go"
            ReturnFace.NEXT -> "Next"
            ReturnFace.PREVIOUS -> "Previous"
            ReturnFace.DONE -> "Done"
        }
        KeyboardLanguage.RUSSIAN -> when (face) {
            ReturnFace.NEWLINE -> "Ввод"
            ReturnFace.SEND -> "Отправить"
            ReturnFace.SEARCH -> "Найти"
            ReturnFace.GO -> "Перейти"
            ReturnFace.NEXT -> "Далее"
            ReturnFace.PREVIOUS -> "Назад"
            ReturnFace.DONE -> "Готово"
        }
        KeyboardLanguage.KAZAKH -> when (face) {
            ReturnFace.NEWLINE -> "Енгізу"
            ReturnFace.SEND -> "Жіберу"
            ReturnFace.SEARCH -> "Іздеу"
            ReturnFace.GO -> "Өту"
            ReturnFace.NEXT -> "Келесі"
            ReturnFace.PREVIOUS -> "Алдыңғы"
            ReturnFace.DONE -> "Дайын"
        }
    }
}
