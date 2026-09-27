package kz.yerek.aireply.keyboard.input

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import kz.yerek.aireply.core.text.codePointLength
import kz.yerek.aireply.keyboard.layout.TextDeletion
import java.text.BreakIterator
import kotlin.math.abs

/**
 * A text field the KEYBOARD edits, rather than one the system edits.
 *
 * WHY THIS EXISTS. An input method cannot host a normally focused text field:
 * there is no second keyboard to type into it, and asking the system for focus
 * from inside an IME window is the sort of thing that works on one OEM's build
 * and not the next. So the composer's fields keep their text here and are
 * edited by the same key presses that would otherwise go to the host app -
 * the keys simply route elsewhere.
 *
 * Every edit is character-aware: an emoji or a letter with a combining mark
 * is deleted, or stepped over, whole.
 */
class KeyboardTextFieldState(initial: String = "") {

    var text: String by mutableStateOf(initial)
        private set

    /** Caret position, as a UTF-16 offset into [text]. */
    var cursor: Int by mutableIntStateOf(initial.length)
        private set

    val isBlank: Boolean get() = text.isBlank()

    fun set(value: String, moveCursorToEnd: Boolean = true) {
        text = value
        cursor = if (moveCursorToEnd) value.length else cursor.coerceIn(0, value.length)
    }

    fun clear() {
        text = ""
        cursor = 0
    }

    fun moveCursor(to: Int) {
        cursor = boundaryAtOrBefore(to.coerceIn(0, text.length))
    }

    /**
     * Inserts at the caret. With [limit], an insertion that would take the
     * text past that many characters is refused - at the keystroke, where the
     * user sees it, rather than silently cut at request time.
     */
    fun insert(value: String, limit: Int? = null): Boolean {
        val at = cursor.coerceIn(0, text.length)
        val next = text.substring(0, at) + value + text.substring(at)
        if (limit != null && next.codePointLength() > limit) return false
        text = next
        cursor = at + value.length
        return true
    }

    /** Deletes the character before the caret - a whole emoji or combined letter. */
    fun deleteBackward(): Boolean {
        val at = cursor.coerceIn(0, text.length)
        if (at == 0) return false
        val start = previousBoundary(at)
        text = text.substring(0, start) + text.substring(at)
        cursor = start
        return true
    }

    /** Deletes the word before the caret, as a held delete key does. */
    fun deleteWordBackward(): Boolean {
        val at = cursor.coerceIn(0, text.length)
        val length = TextDeletion.wordLength(text.substring(0, at))
        if (length == 0) return false
        val start = at - length
        text = text.substring(0, start) + text.substring(at)
        cursor = start
        return true
    }

    /** Moves the caret by whole characters, for the space-bar trackpad. */
    fun moveCursorBy(offset: Int) {
        var position = cursor.coerceIn(0, text.length)
        repeat(abs(offset)) {
            position = if (offset < 0) previousBoundary(position) else nextBoundary(position)
        }
        cursor = position
    }

    fun textBeforeCursor(): String = text.substring(0, cursor.coerceIn(0, text.length))

    // ------------------------------------------------------------ boundaries

    private fun characters(): BreakIterator =
        BreakIterator.getCharacterInstance().also { it.setText(text) }

    private fun previousBoundary(offset: Int): Int {
        if (offset <= 0) return 0
        return characters().preceding(offset).takeIf { it != BreakIterator.DONE } ?: 0
    }

    private fun nextBoundary(offset: Int): Int {
        if (offset >= text.length) return text.length
        return characters().following(offset).takeIf { it != BreakIterator.DONE } ?: text.length
    }

    private fun boundaryAtOrBefore(offset: Int): Int {
        if (offset <= 0 || offset >= text.length) return offset
        val iterator = characters()
        return if (iterator.isBoundary(offset)) offset else iterator.preceding(offset).coerceAtLeast(0)
    }
}
