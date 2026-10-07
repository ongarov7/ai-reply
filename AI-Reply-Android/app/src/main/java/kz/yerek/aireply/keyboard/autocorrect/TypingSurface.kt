package kz.yerek.aireply.keyboard.autocorrect

import kz.yerek.aireply.keyboard.input.HostField
import kz.yerek.aireply.keyboard.input.KeyboardTextFieldState

/**
 * Where the word being typed lives, as autocorrect sees it: the host app's
 * field (the word is its composing text) or one of the keyboard's own fields
 * (the word is simply the letters before the caret).
 *
 * Сөз қайда терілуде: хост өрісінде немесе пернетақтаның өз өрісінде.
 */
internal interface TypingSurface {

    /** The word being typed, right before the caret; empty when there is none. */
    val word: String

    /** The text before [word]; only its last whitespace-free chunk matters to the guard. */
    val textBeforeWord: String

    /** Adds word characters to the word, starting one if the caret allows. False: not added. */
    fun extendWord(text: String): Boolean

    /** Takes the word's last character away. False when there is no word: the caller deletes as usual. */
    fun shortenWord(): Boolean

    /** Ends the word as [replacement] followed by [separator]. False when the field refused it. */
    fun endWord(replacement: String, separator: String): Boolean

    /** Types [text] as it is, ending the word as typed. */
    fun type(text: String): Boolean

    /**
     * Backspace right after [applied]: when its correction and separator are
     * still right before the caret, puts the original word back in their
     * place (no separator), being typed again after [before].
     */
    fun restore(applied: AppliedCorrection, before: String): Boolean
}

/** The host app's field: the word is composing text, so it can still be replaced. */
internal class HostSurface(private val host: HostField) : TypingSurface {

    override val word: String get() = host.composingText

    override val textBeforeWord: String get() = host.textBeforeComposing

    override fun extendWord(text: String): Boolean {
        val current = host.composingText
        if (current.isEmpty()) {
            // Typing into the middle of a word stays plain typing.
            val before = host.contextForNewWord(WordText::isWordCharacter) ?: return false
            host.compose(text, before)
        } else {
            host.compose(current + text)
        }
        return true
    }

    override fun shortenWord(): Boolean {
        val current = host.composingText
        if (current.isEmpty()) return false
        host.compose(current.substring(0, current.offsetByCodePoints(current.length, -1)))
        return true
    }

    override fun endWord(replacement: String, separator: String): Boolean {
        host.commitComposing(replacement + separator)
        return true
    }

    override fun type(text: String): Boolean {
        host.commitText(text)
        return true
    }

    override fun restore(applied: AppliedCorrection, before: String): Boolean {
        val committed = applied.correction + applied.separator
        if (host.composingText.isNotEmpty() || host.textBeforeCursor(committed.length) != committed) return false
        host.batch {
            host.deleteBefore(applied.undoLength)
            host.compose(applied.original, before)
        }
        return true
    }
}

/**
 * One of the keyboard's own fields (an instruction, the copied message). The
 * word is the run of letters before the caret, unless the caret sits inside
 * a word. [limit] is the field's character limit, if it has one.
 */
internal class FieldSurface(val state: KeyboardTextFieldState, private val limit: Int?) : TypingSurface {

    override val word: String
        get() {
            val after = state.text.getOrNull(state.cursor)
            if (after != null && WordText.isWordCharacter(after)) return ""
            return WordText.trailingWord(state.textBeforeCursor())
        }

    override val textBeforeWord: String get() = state.textBeforeCursor().dropLast(word.length)

    override fun extendWord(text: String): Boolean = state.insert(text, limit)

    /** Deleting in a field needs nothing special: the word is read from the text again. */
    override fun shortenWord(): Boolean = false

    override fun endWord(replacement: String, separator: String): Boolean {
        val current = word
        if (replacement == current) return state.insert(separator, limit)
        return state.replaceBeforeCursor(current.length, replacement + separator, limit)
    }

    override fun type(text: String): Boolean = state.insert(text, limit)

    override fun restore(applied: AppliedCorrection, before: String): Boolean {
        if (!state.textBeforeCursor().endsWith(applied.correction + applied.separator)) return false
        return state.replaceBeforeCursor(applied.undoLength, applied.original, limit)
    }
}
