package kz.yerek.aireply.keyboard.voice

import kz.yerek.aireply.core.text.codePointLength
import kz.yerek.aireply.keyboard.input.KeyboardTextFieldState
import kz.yerek.aireply.keyboard.layout.AutoCapitalization

/**
 * How dictated words join the text already in a field: at the caret, the way
 * a careful typist would have typed them.
 *
 *  * One space between them and a word or punctuation before the caret - none
 *    after a space, a new line or an opening bracket or quote, so never two.
 *  * One space after them when a word follows the caret directly.
 *  * A capital at the start of a sentence; otherwise the recogniser's casing
 *    is kept (it knows "Алматы" is a name).
 *  * Never past [limit] characters: whole words up to the limit go in, and the
 *    caller is told that the rest did not fit.
 *
 * The caret ends after the inserted words, so the next dictation or keystroke
 * continues from there.
 */
object DictationInsertion {

    enum class Result {
        /** Everything heard is in the field. */
        INSERTED,

        /** The limit was reached: the first words are in, the rest are not. */
        TRIMMED,

        /** Not even one word fitted. The field is unchanged. */
        NOTHING_FIT,

        /** Nothing was heard. The field is unchanged. */
        EMPTY
    }

    fun insert(field: KeyboardTextFieldState, heard: String, limit: Int?): Result {
        val words = heard.split(WHITESPACE).filter { it.isNotEmpty() }
        if (words.isEmpty()) return Result.EMPTY

        val text = field.text
        val at = field.cursor.coerceIn(0, text.length)
        val before = text.substring(0, at)
        val after = text.substring(at)
        val lead = if (needsSpaceBefore(before)) " " else ""
        val trail = if (needsSpaceAfter(after)) " " else ""
        val capitalise = AutoCapitalization.isSentenceStart(before + lead)
        val room = limit?.let { it - text.codePointLength() }

        fun phrase(count: Int): String {
            val joined = words.subList(0, count).joinToString(" ")
            return if (capitalise) capitalised(joined) else joined
        }

        var count = words.size
        if (room != null) {
            while (count > 0 && (lead + phrase(count) + trail).codePointLength() > room) count--
        }
        if (count == 0) return Result.NOTHING_FIT

        val inserted = phrase(count)
        if (!field.insert(lead + inserted + trail, limit)) return Result.NOTHING_FIT
        field.moveCursor(at + lead.length + inserted.length)
        return if (count < words.size) Result.TRIMMED else Result.INSERTED
    }

    /** A space is needed after anything but a space, a new line or something that opens. */
    internal fun needsSpaceBefore(before: String): Boolean {
        val last = before.lastOrNull() ?: return false
        if (last.isWhitespace() || last in OPENING) return false
        if (last in EITHER_WAY) {
            // A straight or curly quote closes after a word and opens after a space.
            val previous = before.getOrNull(before.length - 2) ?: return false
            return !previous.isWhitespace() && previous !in OPENING
        }
        return true
    }

    /** A space is needed before a word that follows directly; not before punctuation. */
    internal fun needsSpaceAfter(after: String): Boolean {
        val first = after.firstOrNull() ?: return false
        return !first.isWhitespace() && first !in CLOSING && first !in EITHER_WAY
    }

    private fun capitalised(text: String): String =
        text.replaceFirstChar { if (it.isLowerCase()) it.titlecase() else it.toString() }

    private val WHITESPACE = Regex("\\s+")
    private const val OPENING = "([{«„"
    private const val EITHER_WAY = "\"'“‘"
    private const val CLOSING = ".,!?;:…)]}»”’%"
}
