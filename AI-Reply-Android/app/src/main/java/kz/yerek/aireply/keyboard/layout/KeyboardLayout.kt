package kz.yerek.aireply.keyboard.layout

import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.core.lang.KeyboardPlane
import kz.yerek.aireply.keyboard.KeyVisualStyle
import kz.yerek.aireply.keyboard.KeyboardKey
import kz.yerek.aireply.keyboard.emoji.EmojiTab

// ------------------------------------------------------------------- model

/** How wide a slot on a row is. */
sealed interface KeyWidth {
    /** Multiples of the row's key unit. */
    data class Units(val units: Float) : KeyWidth

    /**
     * A share of whatever width is left once the fixed slots are placed.
     *
     * [gaps] is for a key standing in for several flexible keys of the rows
     * around it - the number pad's `0` when there is no `-` or `.` beside
     * it: it also takes the gaps between the keys it replaces, so its edges
     * line up with theirs exactly.
     */
    data class Flexible(val weight: Float, val gaps: Int = 0) : KeyWidth
}

/**
 * A part of a page that is not drawn as keys: Compose lays it over the key
 * canvas and takes every touch inside its area.
 */
enum class PanelKind {
    /** The emoji grid, above the emoji page's bottom row. */
    EMOJI_GRID,

    /** The emoji categories, between ABC and delete. */
    EMOJI_CATEGORIES
}

/**
 * One position on a row: a key, a [panel], or an empty spacer that only
 * takes width. Spacers draw nothing and own no touches - the neighbouring
 * keys do. A panel owns its touches like a key.
 */
data class KeySlot(
    val key: KeyboardKey?,
    val width: KeyWidth = ONE,
    /** Offered on a long press, in the order the panel shows them. */
    val alternates: List<String> = emptyList(),
    /** The small caption in the corner: what a long press types first. */
    val hint: String? = null,
    /**
     * How a character or space key is drawn, when its place on the page does
     * not say: on a number pad the digits are drawn like letters wherever
     * they are, and the side columns like the controls. Null everywhere else.
     */
    val style: KeyVisualStyle? = null,
    /** Not a key but a panel standing in a key's place on the row. */
    val panel: PanelKind? = null
) {
    /** Takes touches and gaps like a key: a key or a panel, not a spacer. */
    val isSolid: Boolean get() = key != null || panel != null

    companion object {
        val ONE = KeyWidth.Units(1f)
        fun spacer(width: KeyWidth) = KeySlot(null, width)
        fun panel(kind: PanelKind, width: KeyWidth) = KeySlot(null, width, panel = kind)
    }
}

data class KeyRow(
    val slots: List<KeySlot>,
    /**
     * Columns the row's key unit is derived from; null means the page's own
     * grid. The bottom row uses the 10-column grid on every layout, so ?123,
     * space and return keep one size whatever language is up.
     */
    val unitColumns: Int? = null,
    /** Spacing between controls; their unit size stays on the page grid. */
    val keyGap: Float? = null
)

/** A complete page: every row, the bottom one included. */
data class KeyboardPage(
    val language: KeyboardLanguage,
    val plane: KeyboardPlane,
    /**
     * 10 for Latin, numbers and symbols; 11 for the Cyrillic layouts; the
     * pads' 6, which sizes their side columns.
     */
    val columns: Int,
    val rows: List<KeyRow>,
    /** A panel over the rows - the emoji grid - in [panelRows] rows' worth of height. */
    val panelAbove: PanelKind? = null,
    /**
     * The rows of height [panelAbove] takes, as if they were key rows: the
     * page is laid out as `panelRows + rows.size` rows, so its own rows sit
     * exactly where the letters page has its bottom ones.
     */
    val panelRows: Int = 0
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
    val field: FieldKind = FieldKind.TEXT,
    /** The number pad's `-`: the field takes negative numbers. */
    val signed: Boolean = false,
    /** The number pad's `.` (`,` on a long press): the field takes fractions. */
    val decimal: Boolean = false,
    /**
     * A row of digits over the letters, as Gboard has in password fields.
     * The caller asks for it only where [KeyboardLayout.numberRowFits].
     */
    val numberRow: Boolean = false,
    /**
     * The emoji key right after ?123 on the letters. Off by default and off
     * where emoji do not belong: e-mail, URL and password fields; pads have
     * no bottom row to put it in.
     */
    val showsEmojiKey: Boolean = false
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
 *
 * The emoji key sits right after ?123 on the letters ([PageOptions.showsEmojiKey]);
 * its page ([emojiPage]) is the emoji grid over a bottom row of ABC, the
 * categories and delete, at the letters' own height.
 *
 * A field that only takes numbers opens on a pad instead ([padPage]): a
 * phone keypad, a number pad or a date pad, with big keys and ABC to escape
 * to the letters. A password field gets Gboard's row of digits over the
 * letters, where the keyboard's one height has room for it ([numberRowFits]).
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

    /** Shift and delete on the Latin rows, and the Return key. */
    const val MODIFIER_UNITS = 1.5f
    const val BOTTOM_PLANE_UNITS = 1.35f
    private const val BOTTOM_ROW_GAP = 3f

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

    /**
     * Gboard's =\< page, without its bottom row - with `<` and `>` on keys of
     * their own where Gboard has `¶` and `∆` (now long presses on `•` and
     * `π`): the page is named after `<`, and with it every printable ASCII
     * character has a key, not only a long press.
     */
    fun symbols(language: KeyboardLanguage): List<List<String>> = listOf(
        listOf("~", "`", "|", "•", "√", "π", "÷", "×", "<", ">"),
        listOf("£", if (language == KeyboardLanguage.ENGLISH) "¢" else "$", "€", "¥", "^", "°", "=", "{", "}", "\\"),
        listOf("%", "©", "®", "™", "✓", "[", "]")
    )

    /**
     * Rows on a page, the bottom row included. The pads have four whatever
     * the field, so they always fit the shared height. A password field's
     * number row is one more, and only asked for where [numberRowFits].
     */
    fun rowCount(language: KeyboardLanguage, plane: KeyboardPlane): Int = when (plane) {
        KeyboardPlane.LETTERS -> letters(language).size + 1
        KeyboardPlane.NUMBERS, KeyboardPlane.SYMBOLS -> 4
        // The grid takes the letters' rows above the bottom one.
        KeyboardPlane.EMOJI -> rowCount(language, KeyboardPlane.LETTERS)
        KeyboardPlane.PHONE_PAD, KeyboardPlane.NUMBER_PAD, KeyboardPlane.DATE_PAD -> PAD_ROWS.size + 1
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

    /**
     * Whether [language]'s letters take a number row in a password field
     * without the keyboard growing and without any key getting shorter: the
     * page with the extra row must have no more rows than the tallest page of
     * the [enabled] layouts already has.
     *
     * With Kazakh on (the default) the keyboard is five rows high, so the
     * English and Russian password pages get their digits on top for free.
     * Kazakh's own page already has five rows - a sixth would take every key
     * down to 31 dp on a 320 dp phone - and a keyboard of English and Russian
     * only is four rows high, where a fifth would take them to 33 dp. There
     * the digits stay where they always are: a long press on the top row,
     * with the digit drawn in the key's corner, and ?123.
     */
    fun numberRowFits(language: KeyboardLanguage, enabled: Collection<KeyboardLanguage>): Boolean =
        rowCount(language, KeyboardPlane.LETTERS) + 1 <= maximumRowCount(enabled)

    // --------------------------------------------------------------- pages

    fun page(language: KeyboardLanguage, plane: KeyboardPlane, options: PageOptions): KeyboardPage =
        when (plane) {
            KeyboardPlane.LETTERS -> lettersPage(language, options)
            KeyboardPlane.NUMBERS -> punctuationPage(language, plane, numbers(language), options)
            KeyboardPlane.SYMBOLS -> punctuationPage(language, plane, symbols(language), options)
            KeyboardPlane.EMOJI -> emojiPage(language)
            KeyboardPlane.PHONE_PAD -> padPage(
                language, plane,
                side = listOf("+", "(", ")", "-"),
                beforeZero = "*", afterZero = "#",
                // The dialler's habit: a held 0 is +.
                zeroAlternates = listOf("+")
            )
            KeyboardPlane.NUMBER_PAD -> padPage(
                language, plane,
                side = emptyList(),
                beforeZero = "-".takeIf { options.signed },
                afterZero = ".".takeIf { options.decimal }
            )
            KeyboardPlane.DATE_PAD -> padPage(
                language, plane,
                side = listOf(".", "/", ":", "-"),
                beforeZero = null, afterZero = null
            )
        }

    private fun lettersPage(language: KeyboardLanguage, options: PageOptions): KeyboardPage {
        val rows = letters(language)
        val result = mutableListOf<KeyRow>()
        val edge = KeySlot.spacer(KeyWidth.Flexible(1f))

        // A password's digits get a row of their own, across the 10-column
        // grid on every layout, and the corner hints they replace go.
        if (options.numberRow) {
            result += KeyRow(characterSlots(DIGITS.map { it.toString() }, language), unitColumns = 10)
        }
        val hints = !options.numberRow

        when (language) {
            KeyboardLanguage.ENGLISH -> {
                val modifier = KeyWidth.Units(MODIFIER_UNITS)
                result += KeyRow(characterSlots(rows[0], language, withDigits = hints))
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
                    val keys = characterSlots(row, language, withDigits = hints && index == digitRow)
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

    /**
     * The emoji page: the grid over one row of ABC, the categories and
     * delete - ABC where ?123 is and delete where return is, on the bottom
     * row's 10-column grid, at the height and place of the letters' bottom
     * row. ABC goes back to the letters as they were.
     *
     * Эмодзи беті: тор, астында ABC, санаттар және өшіру.
     */
    private fun emojiPage(language: KeyboardLanguage): KeyboardPage {
        val modifier = KeyWidth.Units(MODIFIER_UNITS)
        val bar = KeyRow(
            listOf(
                KeySlot(KeyboardKey.Plane(KeyboardPlane.LETTERS), KeyWidth.Units(BOTTOM_PLANE_UNITS)),
                KeySlot.panel(PanelKind.EMOJI_CATEGORIES, KeyWidth.Flexible(1f)),
                KeySlot(KeyboardKey.Backspace, modifier)
            ),
            unitColumns = 10,
            keyGap = BOTTOM_ROW_GAP
        )
        return KeyboardPage(
            language = language,
            plane = KeyboardPlane.EMOJI,
            columns = 10,
            rows = listOf(bar),
            panelAbove = PanelKind.EMOJI_GRID,
            panelRows = rowCount(language, KeyboardPlane.LETTERS) - 1
        )
    }

    /** The pads' digits above the row with `0`. */
    private val PAD_ROWS: List<List<String>> = listOf(
        listOf("1", "2", "3"),
        listOf("4", "5", "6"),
        listOf("7", "8", "9")
    )

    /**
     * The pads' grid: a side column is one key of a six-column row, and the
     * three digit columns share what is left - so a digit is about a third
     * wider than a side key, Gboard's proportions.
     */
    private const val PAD_COLUMNS = 6

    /**
     * A pad: a dialler's grid of big keys, as the system keyboards have for
     * these fields.
     *
     * ```
     *   +  1  2  3  ABC        1  2  3  ABC        .  1  2  3  ABC
     *   (  4  5  6   ␣         4  5  6   ␣         /  4  5  6   ␣
     *   )  7  8  9   ⌫         7  8  9   ⌫         :  7  8  9   ⌫
     *   -  *  0  #   ⏎         -  0  .   ⏎         -  [  0  ]   ⏎
     *       phone            number, signed           date
     *                         and decimal
     * ```
     *
     * Three big columns of digits, drawn like letters; the field's own
     * characters down the left ([side]), drawn like the controls; and down
     * the right the column every pad shares - ABC back to the letters, space,
     * then delete over return, as on the letters.
     *
     * [beforeZero] and [afterZero] flank `0`, under `7` and `9`. When a number
     * field takes no `-` or no `.`, the `0` grows over the empty place rather
     * than leaving a hole: a hole's touches belong to its neighbours, and a
     * stray tap beside `0` must type `0`, not press return.
     *
     * No layout key and no globe: a pad has no language, and ABC leads to
     * both. Long presses offer only what such a field takes: `+` on the
     * phone's `0`, `,` on the decimal point.
     */
    private fun padPage(
        language: KeyboardLanguage,
        plane: KeyboardPlane,
        side: List<String>,
        beforeZero: String?,
        afterZero: String?,
        zeroAlternates: List<String> = emptyList()
    ): KeyboardPage {
        val column = KeyWidth.Units(1f)

        fun digit(value: String, width: KeyWidth = KeyWidth.Flexible(1f), alternates: List<String> = emptyList()) =
            KeySlot(
                key = KeyboardKey.Character(value),
                width = width,
                alternates = alternates,
                hint = alternates.firstOrNull(),
                style = KeyVisualStyle.LETTER
            )

        val zeroSpan = 1 + listOf(beforeZero, afterZero).count { it == null }
        val zeroRow = listOfNotNull(
            beforeZero?.let { digit(it) },
            digit("0", KeyWidth.Flexible(zeroSpan.toFloat(), gaps = zeroSpan - 1), zeroAlternates),
            afterZero?.let { digit(it, alternates = if (it == ".") listOf(",") else emptyList()) }
        )
        val grid = PAD_ROWS.map { row -> row.map { digit(it) } } + listOf(zeroRow)
        val right = listOf(
            KeySlot(KeyboardKey.Plane(KeyboardPlane.LETTERS), column),
            KeySlot(KeyboardKey.Space, column, style = KeyVisualStyle.SPECIAL),
            KeySlot(KeyboardKey.Backspace, column),
            KeySlot(KeyboardKey.Return, column)
        )
        require(side.isEmpty() || side.size == grid.size) { "a pad's side column fills every row" }

        val rows = grid.mapIndexed { index, digits ->
            val left = side.getOrNull(index)
                ?.let { listOf(KeySlot(KeyboardKey.Character(it), column, style = KeyVisualStyle.SPECIAL)) }
                .orEmpty()
            KeyRow(left + digits + right[index])
        }
        return KeyboardPage(language, plane, columns = PAD_COLUMNS, rows = rows)
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
     * ?123 / ABC, the emoji key (letters only), the comma (or the globe when
     * Android asks for one), the layout key, space, the full stop and return -
     * sized on the 10-column grid so the row is identical on every layout.
     */
    private fun bottomRow(language: KeyboardLanguage, planeKey: KeyboardPlane, options: PageOptions): KeyRow {
        val slots = mutableListOf(KeySlot(KeyboardKey.Plane(planeKey), KeyWidth.Units(BOTTOM_PLANE_UNITS)))

        // The letters' row only: ?123 and =\< stay exactly as they were.
        if (options.showsEmojiKey && planeKey == KeyboardPlane.NUMBERS) slots += KeySlot(KeyboardKey.Plane(KeyboardPlane.EMOJI))

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
        if (options.showsLanguageKey) slots += KeySlot(KeyboardKey.Layout, KeyWidth.Units(1f))

        slots += KeySlot(KeyboardKey.Space, KeyWidth.Flexible(1f))

        val stopAlternates = when (options.field) {
            FieldKind.TEXT -> listOf(",", "?", "!", "…", ":", ";", "'", "\"", "-")
            FieldKind.EMAIL, FieldKind.URL -> listOf(".com", ".kz", ".ru")
        }
        slots += KeySlot(KeyboardKey.Character("."), KeyWidth.Units(0.9f), alternates = stopAlternates)
        slots += KeySlot(KeyboardKey.Return, KeyWidth.Units(MODIFIER_UNITS))
        // Reclaim secondary-key width and spare spacing for Space, retaining
        // the full system-switch and Return widths on the shared grid.
        return KeyRow(slots, unitColumns = 10, keyGap = BOTTOM_ROW_GAP)
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
        "@" to listOf("#"),
        "<" to listOf("≤", "«"),
        ">" to listOf("≥", "»"),
        "•" to listOf("·", "¶"),
        "π" to listOf("∆")
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
        // Drawn as a face; the name is what TalkBack reads.
        KeyboardPlane.EMOJI -> emoji
        // No key leads to a pad - the field opens on it - but a name it has.
        KeyboardPlane.PHONE_PAD, KeyboardPlane.NUMBER_PAD, KeyboardPlane.DATE_PAD -> "123"
    }

    val emoji: String
        get() = when (language) {
            KeyboardLanguage.ENGLISH -> "Emoji"
            KeyboardLanguage.RUSSIAN, KeyboardLanguage.KAZAKH -> "Эмодзи"
        }

    /** The emoji page's header over a category and its tab's name, like the ABC key beside them. */
    fun emojiTab(tab: EmojiTab): String = when (language) {
        KeyboardLanguage.ENGLISH -> when (tab) {
            EmojiTab.RECENT -> "Recent"
            EmojiTab.SMILEYS_PEOPLE -> "Smileys & people"
            EmojiTab.ANIMALS_NATURE -> "Animals & nature"
            EmojiTab.FOOD_DRINK -> "Food & drink"
            EmojiTab.TRAVEL_PLACES -> "Travel & places"
            EmojiTab.ACTIVITIES -> "Activities"
            EmojiTab.OBJECTS -> "Objects"
            EmojiTab.SYMBOLS -> "Symbols"
            EmojiTab.FLAGS -> "Flags"
        }
        KeyboardLanguage.RUSSIAN -> when (tab) {
            EmojiTab.RECENT -> "Недавние"
            EmojiTab.SMILEYS_PEOPLE -> "Смайлы и люди"
            EmojiTab.ANIMALS_NATURE -> "Животные и природа"
            EmojiTab.FOOD_DRINK -> "Еда и напитки"
            EmojiTab.TRAVEL_PLACES -> "Путешествия и места"
            EmojiTab.ACTIVITIES -> "Занятия"
            EmojiTab.OBJECTS -> "Предметы"
            EmojiTab.SYMBOLS -> "Символы"
            EmojiTab.FLAGS -> "Флаги"
        }
        KeyboardLanguage.KAZAKH -> when (tab) {
            EmojiTab.RECENT -> "Соңғылар"
            EmojiTab.SMILEYS_PEOPLE -> "Смайликтер мен адамдар"
            EmojiTab.ANIMALS_NATURE -> "Жануарлар мен табиғат"
            EmojiTab.FOOD_DRINK -> "Тағам мен сусын"
            EmojiTab.TRAVEL_PLACES -> "Саяхат пен орындар"
            EmojiTab.ACTIVITIES -> "Әрекеттер"
            EmojiTab.OBJECTS -> "Заттар"
            EmojiTab.SYMBOLS -> "Таңбалар"
            EmojiTab.FLAGS -> "Жалаулар"
        }
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
