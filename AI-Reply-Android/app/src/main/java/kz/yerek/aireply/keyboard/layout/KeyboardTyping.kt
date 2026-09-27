package kz.yerek.aireply.keyboard.layout

/**
 * Shift, the way Gboard and the iOS keyboard behave:
 *
 *  * one tap arms it for the next letter;
 *  * two taps in quick succession lock it (caps lock); one more tap unlocks;
 *  * the keyboard arms it by itself at the start of a sentence, and takes an
 *    automatic shift back if the caret moves away from that start - but never
 *    overrides a shift the user set by hand.
 */
class ShiftState {

    enum class Mode { OFF, ONCE, LOCKED }

    var mode: Mode = Mode.OFF
        private set

    /** True when the current [Mode.ONCE] was set by auto-capitalization. */
    var isAutomatic: Boolean = false
        private set

    private var lastTap = Long.MIN_VALUE / 2

    val isActive: Boolean get() = mode != Mode.OFF
    val isLocked: Boolean get() = mode == Mode.LOCKED

    /** The user tapped shift at [timeMillis] (a monotonic clock). */
    fun tap(timeMillis: Long) {
        if (mode == Mode.LOCKED) {
            mode = Mode.OFF
            isAutomatic = false
            lastTap = Long.MIN_VALUE / 2
            return
        }
        if (timeMillis - lastTap <= DOUBLE_TAP_MS) {
            mode = Mode.LOCKED
            isAutomatic = false
            lastTap = Long.MIN_VALUE / 2
            return
        }
        mode = if (mode == Mode.OFF) Mode.ONCE else Mode.OFF
        isAutomatic = false
        lastTap = timeMillis
    }

    /** A character was typed: a one-shot shift is spent. */
    fun characterTyped() {
        lastTap = Long.MIN_VALUE / 2
        if (mode == Mode.ONCE) {
            mode = Mode.OFF
            isAutomatic = false
        }
    }

    /** Applies auto-capitalization for the text now before the caret. */
    fun applyAutomatic(shouldCapitalize: Boolean) {
        when (mode) {
            Mode.LOCKED -> Unit
            Mode.OFF -> if (shouldCapitalize) {
                mode = Mode.ONCE
                isAutomatic = true
            }
            Mode.ONCE -> if (!shouldCapitalize && isAutomatic) {
                mode = Mode.OFF
                isAutomatic = false
            }
        }
    }

    fun reset() {
        mode = Mode.OFF
        isAutomatic = false
        lastTap = Long.MIN_VALUE / 2
    }

    /** The text a key types in this state. */
    fun apply(text: String): String = if (isActive) text.uppercase() else text

    companion object {
        const val DOUBLE_TAP_MS = 320L
    }
}

/** What the host field asked for, from `EditorInfo.inputType`. */
enum class Capitalization { NONE, SENTENCES, WORDS, CHARACTERS }

/**
 * Whether the next letter should be a capital, decided from the field's
 * capitalization mode and the text before the caret.
 */
object AutoCapitalization {

    fun shouldCapitalize(before: String?, mode: Capitalization): Boolean = when (mode) {
        Capitalization.NONE -> false
        Capitalization.CHARACTERS -> true
        Capitalization.WORDS -> before.isNullOrEmpty() || before.last().isWhitespace()
        Capitalization.SENTENCES -> isSentenceStart(before)
    }

    /**
     * An empty field, a new line, or sentence punctuation followed by a
     * space. "Hello.|" is not a sentence start yet; "Hello. |" is.
     */
    fun isSentenceStart(context: String?): Boolean {
        if (context.isNullOrEmpty()) return true
        var index = context.length
        var trailingSpaces = 0
        while (index > 0) {
            val character = context[index - 1]
            if (character == '\n') return true
            if (character != ' ' && character != ' ' && character != '\t') break
            trailingSpaces++
            index--
        }
        if (index == 0) return true
        if (trailingSpaces == 0) return false

        // Step over closing quotes and brackets: `He said "Yes." |` starts a
        // sentence too.
        var cursor = index - 1
        while (cursor > 0 && context[cursor] in "\"'»”’)]") cursor--
        return context[cursor] in ".!?…"
    }
}

/**
 * The double-space full stop: a second space typed quickly after a word turns
 * the first one into ". ".
 */
object SpaceShortcut {

    const val INTERVAL_MS = 450L

    fun shouldInsertPeriod(before: String?, millisSinceLastSpace: Long): Boolean {
        if (millisSinceLastSpace > INTERVAL_MS || before == null) return false
        if (!before.endsWith(" ") || before.endsWith("  ")) return false
        val last = before.dropLast(1).lastOrNull() ?: return false
        return last.isLetterOrDigit() || last in "\"')»”’"
    }
}

/** Word-at-a-time deletion, used once delete has been held for a while. */
object TextDeletion {

    /**
     * How many UTF-16 units one word-delete removes from the end of
     * [context]: the whitespace before the caret plus the word (or the run of
     * punctuation) before that. Always at least one character while there is
     * text, and never half of a surrogate pair.
     */
    fun wordLength(context: String): Int {
        if (context.isEmpty()) return 0
        var index = context.length

        while (index > 0 && context[index - 1].isWhitespace()) index--
        if (index == 0) return context.length

        fun isWordCharacter(character: Char): Boolean =
            character.isLetterOrDigit() || character == '\'' || character == '’' || character == '-' ||
                Character.getType(character) == Character.NON_SPACING_MARK.toInt()

        // A word, or a run of anything else (punctuation, emoji - both
        // halves of a surrogate pair are "anything else", so an emoji goes
        // whole).
        val deletingWord = isWordCharacter(context[index - 1])
        while (index > 0) {
            val character = context[index - 1]
            if (character.isWhitespace() || isWordCharacter(character) != deletingWord) break
            index--
        }
        return context.length - index
    }
}
