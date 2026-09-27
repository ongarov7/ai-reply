package kz.yerek.aireply.keyboard.input

import android.text.InputType
import android.view.inputmethod.EditorInfo
import android.view.inputmethod.InputConnection
import kz.yerek.aireply.platform.ReplyLog

/**
 * Everything the keyboard does to the host application's text field, in one
 * place.
 *
 * Every method tolerates a null connection: an input method is routinely asked
 * to do something a few milliseconds after the field it was editing went away,
 * and a keyboard that crashes inside WhatsApp is the worst failure this project
 * can produce.
 */
class HostField(private val connectionProvider: () -> InputConnection?) {

    var editorInfo: EditorInfo? = null

    private val connection: InputConnection? get() = connectionProvider()

    /**
     * How much context is read at a time.
     *
     * Each read crosses a process boundary, so this is deliberately bounded:
     * enough to decide capitalisation and whether the field is empty, never the
     * whole document. iOS caps the same read at 180 characters for the same
     * reason.
     */
    private val contextWindow = 512

    // --------------------------------------------------------------- writing

    fun commitText(text: String) {
        connection?.commitText(text, 1)
    }

    fun deleteBackward() {
        val connection = connection ?: return
        // Delete the selection if there is one; otherwise one character. Asking
        // for one "character" by code point rather than by UTF-16 unit is what
        // makes an emoji delete whole instead of leaving half a surrogate pair.
        val selected = connection.getSelectedText(0)
        if (!selected.isNullOrEmpty()) {
            connection.commitText("", 1)
            return
        }
        val before = connection.getTextBeforeCursor(2, 0)?.toString().orEmpty()
        val length = when {
            before.isEmpty() -> return
            before.length >= 2 && Character.isSurrogatePair(before[before.length - 2], before[before.length - 1]) -> 2
            else -> 1
        }
        connection.deleteSurroundingText(length, 0)
    }

    /**
     * Deletes the word before the caret (or the selection), as a held delete
     * key does. One cross-process read, one delete.
     */
    fun deleteWordBackward() {
        val connection = connection ?: return
        val selected = connection.getSelectedText(0)
        if (!selected.isNullOrEmpty()) {
            connection.commitText("", 1)
            return
        }
        val before = connection.getTextBeforeCursor(WORD_WINDOW, 0)?.toString().orEmpty()
        val length = kz.yerek.aireply.keyboard.layout.TextDeletion.wordLength(before)
        if (length > 0) connection.deleteSurroundingText(length, 0)
    }

    /**
     * Moves the caret by whole characters (space-bar trackpad) from
     * [position], the caret as `onUpdateSelection` last reported it. Reads
     * just enough text either side to step over emoji rather than into them.
     */
    fun moveCursorBy(offset: Int, position: Int) {
        val connection = connection ?: return
        if (offset == 0 || position < 0) return
        val window = kotlin.math.abs(offset) * 2 + 2
        val before = connection.getTextBeforeCursor(window, 0)?.toString().orEmpty()
        val after = connection.getTextAfterCursor(window, 0)?.toString().orEmpty()
        var units = 0
        if (offset < 0) {
            var index = before.length
            repeat(-offset) {
                if (index <= 0) return@repeat
                index = if (index >= 2 && Character.isSurrogatePair(before[index - 2], before[index - 1])) index - 2 else index - 1
            }
            units = index - before.length
        } else {
            var index = 0
            repeat(offset) {
                if (index >= after.length) return@repeat
                index = if (index + 1 < after.length && Character.isSurrogatePair(after[index], after[index + 1])) index + 2 else index + 1
            }
            units = index
        }
        if (units == 0) return
        val target = (position + units).coerceAtLeast(0)
        connection.setSelection(target, target)
    }

    /** Puts the caret after the last character, so Add appends at the end. */
    fun moveCaretToEnd() {
        val connection = connection ?: return
        val after = connection.getTextAfterCursor(MAX_CLEAR, 0)?.length ?: 0
        if (after == 0) return
        val before = connection.getTextBeforeCursor(MAX_CLEAR, 0)?.length ?: 0
        connection.setSelection(before + after, before + after)
    }

    fun sendReturn() {
        val connection = connection ?: return
        val action = editorInfo?.imeOptions?.and(EditorInfo.IME_MASK_ACTION) ?: EditorInfo.IME_ACTION_NONE
        val isMultiline = editorInfo?.inputType?.and(InputType.TYPE_TEXT_FLAG_MULTI_LINE) != 0
        // A multi-line field wants a newline; a single-line field with a Send or
        // Search action wants that action. Getting this backwards either sends a
        // half-written message or leaves the user unable to send at all.
        if (isMultiline || action == EditorInfo.IME_ACTION_NONE || action == EditorInfo.IME_ACTION_UNSPECIFIED) {
            connection.commitText("\n", 1)
        } else {
            connection.performEditorAction(action)
        }
    }

    // --------------------------------------------------------------- reading

    fun textBeforeCursor(limit: Int = contextWindow): String =
        connection?.getTextBeforeCursor(limit, 0)?.toString().orEmpty()

    /**
     * Conservative check, matching iOS: a host can legitimately return nothing
     * for either side, and treating "no context" as "empty" is the safe reading.
     * The worst case is that we insert normally into a field that was already
     * empty.
     */
    fun appearsToHaveText(): Boolean {
        val connection = connection ?: return false
        val before = connection.getTextBeforeCursor(contextWindow, 0)?.toString().orEmpty()
        val after = connection.getTextAfterCursor(contextWindow, 0)?.toString().orEmpty()
        return (before + after).isNotBlank()
    }

    /**
     * A space unless the existing text already ends in whitespace, so Add does
     * not jam two sentences together or double-space them.
     */
    fun separatorForAppend(): String {
        val before = textBeforeCursor()
        val last = before.lastOrNull() ?: return ""
        return if (last.isWhitespace()) "" else " "
    }

    /**
     * Clears the field.
     *
     * One call, not a loop: `deleteSurroundingText` takes both directions at
     * once, so a 2000-character draft is a single cross-process call. The iOS
     * version has to loop because `deleteBackward()` is the only deletion its
     * proxy offers, and it carries a round limit to stop a runaway loop inside
     * another app's field. Neither is needed here.
     */
    fun clear() {
        val connection = connection ?: return
        val before = connection.getTextBeforeCursor(MAX_CLEAR, 0)?.length ?: 0
        val after = connection.getTextAfterCursor(MAX_CLEAR, 0)?.length ?: 0
        if (before == 0 && after == 0) return
        connection.deleteSurroundingText(before, after)
        ReplyLog.event { "cleared host field, $before before / $after after" }
    }

    // ------------------------------------------------------------- inspection

    /**
     * Password and similar fields, where the AI panel stays off entirely.
     *
     * iOS keyboards are simply never shown a secure field's content; on Android
     * the keyboard is shown it, so refusing is something this code has to do
     * rather than something the platform does for it.
     */
    val isSecureField: Boolean
        get() {
            val type = editorInfo?.inputType ?: return false
            val clazz = type and InputType.TYPE_MASK_CLASS
            val variation = type and InputType.TYPE_MASK_VARIATION
            return when (clazz) {
                InputType.TYPE_CLASS_TEXT -> variation == InputType.TYPE_TEXT_VARIATION_PASSWORD ||
                    variation == InputType.TYPE_TEXT_VARIATION_VISIBLE_PASSWORD ||
                    variation == InputType.TYPE_TEXT_VARIATION_WEB_PASSWORD
                InputType.TYPE_CLASS_NUMBER -> variation == InputType.TYPE_NUMBER_VARIATION_PASSWORD
                else -> false
            }
        }

    /** Whether the host asked for sentence capitalisation. */
    val capitalizesSentences: Boolean
        get() {
            val type = editorInfo?.inputType ?: return true
            if (type and InputType.TYPE_MASK_CLASS != InputType.TYPE_CLASS_TEXT) return false
            if (isSecureField) return false
            val variation = type and InputType.TYPE_MASK_VARIATION
            if (variation == InputType.TYPE_TEXT_VARIATION_EMAIL_ADDRESS ||
                variation == InputType.TYPE_TEXT_VARIATION_URI ||
                variation == InputType.TYPE_TEXT_VARIATION_WEB_EMAIL_ADDRESS
            ) {
                return false
            }
            // Most messengers set no capitalisation flag at all but still expect
            // sentence behaviour, which is what iOS defaults to as well.
            return type and InputType.TYPE_TEXT_FLAG_CAP_CHARACTERS == 0 &&
                type and InputType.TYPE_TEXT_FLAG_CAP_WORDS == 0
        }

    private companion object {
        /**
         * Upper bound on a single clear. Well past any realistic chat draft, and
         * bounded so a misbehaving host cannot make this allocate without limit.
         */
        const val MAX_CLEAR = 10_000

        /** Enough to find the start of any real word. */
        const val WORD_WINDOW = 64
    }
}
