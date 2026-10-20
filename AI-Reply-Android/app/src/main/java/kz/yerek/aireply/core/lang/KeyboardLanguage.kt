package kz.yerek.aireply.core.lang

/**
 * The three layouts this keyboard ships. The selected value drives both the
 * character layout and the key captions, so the two can never drift apart.
 *
 * The layout tables themselves live in
 * [kz.yerek.aireply.keyboard.layout.KeyboardLayout]; this type only names the
 * layouts and orders them.
 */
enum class KeyboardLanguage(val code: String) {
    ENGLISH("en"),
    RUSSIAN("ru"),
    KAZAKH("kk");

    /**
     * The layout the language key switches to next: ҚАЗ → РУС → ENG → ҚАЗ,
     * skipping the ones the user switched off. Never returns a layout outside
     * [enabled] unless [enabled] is empty.
     */
    fun next(enabled: Collection<KeyboardLanguage> = CYCLE_ORDER): KeyboardLanguage {
        val order = CYCLE_ORDER.filter { it in enabled }.ifEmpty { CYCLE_ORDER }
        val index = order.indexOf(this)
        return if (index < 0) order.first() else order[(index + 1) % order.size]
    }

    /** The layout's own name, as its speakers write it. */
    val nativeName: String
        get() = when (this) {
            ENGLISH -> "English"
            RUSSIAN -> "Русский"
            KAZAKH -> "Қазақша"
        }

    /** What the language key shows while this layout is up. */
    val badge: String
        get() = when (this) {
            ENGLISH -> "ENG"
            RUSSIAN -> "РУС"
            KAZAKH -> "ҚАЗ"
        }

    companion object {
        /** The order the language key walks through. */
        val CYCLE_ORDER: List<KeyboardLanguage> = listOf(KAZAKH, RUSSIAN, ENGLISH)

        fun fromCode(code: String?): KeyboardLanguage? =
            entries.firstOrNull { it.code == code }
    }
}

/**
 * Letters, the two punctuation planes behind ?123 and =\<, the emoji page,
 * and the number pads a field that only takes numbers opens on.
 */
enum class KeyboardPlane {
    LETTERS,
    NUMBERS,
    SYMBOLS,

    /**
     * The emoji grid, with ABC, the categories and delete under it. Reached
     * from the letters' emoji key; never a field's own page, and not a pad.
     */
    EMOJI,

    /** A dialler's keypad, for phone numbers: `* 0 #`, `+ ( ) -`. */
    PHONE_PAD,

    /** Digits for number fields - amounts, codes, PINs - with `-` and `.` only where the field takes them. */
    NUMBER_PAD,

    /** Digits and the date and time separators `. / : -`. */
    DATE_PAD;

    /** One of the pads: no letters, no shift, no language. */
    val isPad: Boolean get() = this == PHONE_PAD || this == NUMBER_PAD || this == DATE_PAD
}
