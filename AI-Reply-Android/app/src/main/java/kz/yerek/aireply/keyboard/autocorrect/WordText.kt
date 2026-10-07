package kz.yerek.aireply.keyboard.autocorrect

/**
 * How typing splits text into words for autocorrect: what continues a word,
 * what ends it with a correction, and what ends it as typed.
 *
 * Сөз шекарасы: әріп сөзді жалғайды, бос орын мен тыныс белгісі түзетеді.
 */
object WordText {

    /** Characters a separator is made of (DESIGN §5.5): the correction is applied on them. */
    private const val SEPARATORS = " \n.,!?;:"

    /**
     * Letters, digits and combining marks, plus the apostrophes and the
     * hyphen inside a word (`don't`, `кое-что`). Digits stay in the word so
     * the guard sees `превт123` whole and leaves it alone.
     */
    fun isWordCharacter(character: Char): Boolean =
        character.isLetterOrDigit() || character == '\'' || character == '’' || character == '-' ||
            Character.getType(character) == Character.NON_SPACING_MARK.toInt()

    /** One keystroke's text that extends the word being typed. */
    fun continuesWord(text: String, word: String): Boolean {
        if (text.isEmpty() || !text.all(::isWordCharacter)) return false
        // A word starts with a letter or a digit, never with ' or -.
        return word.isNotEmpty() || text.first().isLetterOrDigit()
    }

    /** Text that ends the word and applies a pending correction: a space, a new line, `.,!?;:`. */
    fun endsWordWithCorrection(text: String): Boolean = text.firstOrNull()?.let { it in SEPARATORS } == true

    /**
     * The word the engine is asked about: what was typed, if it ends in a
     * letter or digit. `кое-` or `don'` are words in the making, with nothing
     * to suggest yet.
     */
    fun lookupWord(word: String): String = if (word.lastOrNull()?.isLetterOrDigit() == true) word else ""

    /** The run of word characters at the end of [text]. */
    fun trailingWord(text: String): String = text.takeLastWhile(::isWordCharacter).trimStart('\'', '’', '-')
}
