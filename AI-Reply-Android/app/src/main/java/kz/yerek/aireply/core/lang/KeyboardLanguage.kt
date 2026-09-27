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

/** Letters, and the two punctuation planes behind ?123 and =\<. */
enum class KeyboardPlane {
    LETTERS,
    NUMBERS,
    SYMBOLS
}
