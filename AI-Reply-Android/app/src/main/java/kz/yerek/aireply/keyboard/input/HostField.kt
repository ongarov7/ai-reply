package kz.yerek.aireply.keyboard.input

import android.text.InputType
import android.view.inputmethod.EditorInfo
import android.view.inputmethod.InputConnection
import kz.yerek.aireply.platform.ReplyLog
import java.text.BreakIterator

/**
 * Everything the keyboard does to the host application's text field, in one
 * place.
 *
 * Every method tolerates a null connection: an input method is routinely asked
 * to do something a few milliseconds after the field it was editing went away,
 * and a keyboard that crashes inside WhatsApp is the worst failure this project
 * can produce.
 *
 * COMPOSING TEXT. While smart correction is on, the word being typed is the
 * field's composing text ([compose]): the app underlines it and the keyboard
 * can still replace it. Every other edit ends the word first, exactly as typed
 * ([finishComposing]), so no operation ever works on a half-composed field;
 * with smart correction off nothing is ever composed and every call below is
 * the plain `commitText` / `deleteSurroundingText` it always was.
 */
class HostField(private val connectionProvider: () -> InputConnection?) {

    var editorInfo: EditorInfo? = null
        private set

    /** The word being typed, shown by the app as composing text; empty when there is none. */
    var composingText: String = ""
        private set

    /** The text before [composingText] when the word started, for autocorrect's guard. */
    var textBeforeComposing: String = ""
        private set

    private val selection = HostSelection()

    /** Where the composing text starts in the host's text; -1 when unknown. */
    private var composingStart = -1

    private val connection: InputConnection? get() = connectionProvider()

    // ------------------------------------------------------------- lifecycle

    /**
     * A field starts, or the app restarted the same one. A word still being
     * composed is ended first: after a restart the app keeps its composing
     * region, and the next letter must not replace it.
     *
     * The keyboard shown again over the same field gets the same EditorInfo,
     * whose initial selection is long out of date; the selection reports kept
     * coming meanwhile, so what they said is kept.
     */
    fun startInput(info: EditorInfo?, restarting: Boolean) {
        if (restarting && composingText.isNotEmpty()) connection?.finishComposingText()
        clearComposing()
        val shownAgain = !restarting && info != null && info === editorInfo
        editorInfo = info
        if (!shownAgain) selection.reset(info?.initialSelStart ?: -1, info?.initialSelEnd ?: -1)
    }

    /** The field went away; its connection may already be gone, so nothing is sent. */
    fun endInput() {
        clearComposing()
        selection.forget()
    }

    /**
     * The app reported its selection. Returns true when that was not an echo
     * of the keyboard's own edits - a tap elsewhere, a paste, the app clearing
     * the field. A word being typed then ends where it stands, the caret
     * untouched.
     */
    fun selectionChanged(selectionStart: Int, selectionEnd: Int, composingStart: Int, composingEnd: Int): Boolean {
        val external = selection.report(selectionStart, selectionEnd, composingStart, composingEnd)
        if (external && composingText.isNotEmpty()) {
            connection?.finishComposingText()
            clearComposing()
        }
        return external
    }

    /** One undoable step for the app, and one selection report: several edits as a batch. */
    fun <T> batch(edit: () -> T): T {
        val connection = connection
        connection?.beginBatchEdit()
        try {
            return edit()
        } finally {
            connection?.endBatchEdit()
        }
    }

    // ------------------------------------------------------------- composing

    /**
     * The text before the caret when a new word may start there, or null: the
     * caret's position must be known (it anchors the composing region), and
     * neither side of the caret or selection may be part of a word - typing
     * into the middle of one stays plain, as it always was.
     */
    fun contextForNewWord(isWordCharacter: (Char) -> Boolean): String? {
        val connection = connection ?: return null
        if (composingText.isNotEmpty() || !selection.isKnown) return null
        val before = connection.getTextBeforeCursor(WORD_CONTEXT, 0)?.toString() ?: return null
        if (before.lastOrNull()?.let(isWordCharacter) == true) return null
        val after = connection.getTextAfterCursor(1, 0)?.toString().orEmpty()
        if (after.firstOrNull()?.let(isWordCharacter) == true) return null
        return before
    }

    /**
     * Shows [text] as the word being typed. The first call starts the word at
     * the caret (replacing a selection), with [before] as the text before it;
     * an empty [text] removes the word.
     */
    fun compose(text: String, before: String = textBeforeComposing) {
        val connection = connection ?: return
        if (composingText.isEmpty()) {
            if (text.isEmpty()) return
            // A region some other edit left behind would be replaced: end it.
            if (selection.appHasComposing) connection.finishComposingText()
            composingStart = if (selection.isKnown) selection.start else -1
            textBeforeComposing = before
        }
        if (text.isEmpty()) {
            connection.commitText("", 1)
            expectCaret(composingStart)
            clearComposing()
            return
        }
        connection.setComposingText(text, 1)
        composingText = text
        if (composingStart >= 0) {
            selection.expect(composingStart + text.length, composingStart, composingStart + text.length)
        } else {
            selection.forget()
        }
    }

    /** Replaces the word being typed with [text] - a correction and its separator - and ends it. */
    fun commitComposing(text: String) {
        if (composingText.isEmpty()) {
            commitText(text)
            return
        }
        connection?.commitText(text, 1)
        expectCaret(if (composingStart >= 0) composingStart + text.length else -1)
        clearComposing()
    }

    /** Ends the word being typed exactly as it is; the caret stays where it is. */
    fun finishComposing() {
        if (composingText.isEmpty()) return
        connection?.finishComposingText()
        expectCaret(if (selection.isCollapsed) selection.start else -1)
        clearComposing()
    }

    private fun clearComposing() {
        composingText = ""
        textBeforeComposing = ""
        composingStart = -1
    }

    private fun expectCaret(caret: Int) {
        if (caret >= 0) selection.expect(caret) else selection.forget()
    }

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
        finishComposing()
        val connection = connection ?: return
        connection.commitText(text, 1)
        // Replaces a selection, if there was one.
        expectCaret(if (selection.isKnown) selection.start + text.length else -1)
    }

    fun deleteBackward() {
        finishComposing()
        val connection = connection ?: return
        // Delete the selection if there is one; otherwise one character as the
        // user sees it - a whole emoji (a flag, a family, a skin tone) or a
        // letter with its combining marks, never half of one, exactly as the
        // keyboard's own fields do (KeyboardTextFieldState).
        val selected = connection.getSelectedText(0)
        if (!selected.isNullOrEmpty()) {
            connection.commitText("", 1)
            expectCaret(if (selection.isKnown) selection.start else -1)
            return
        }
        val before = connection.getTextBeforeCursor(CLUSTER_WINDOW, 0)?.toString().orEmpty()
        if (before.isEmpty()) return
        deleteBefore(before.length - previousCluster(before, before.length))
    }

    /**
     * Deletes the word before the caret (or the selection), as a held delete
     * key does. One cross-process read, one delete.
     */
    fun deleteWordBackward() {
        finishComposing()
        val connection = connection ?: return
        val selected = connection.getSelectedText(0)
        if (!selected.isNullOrEmpty()) {
            connection.commitText("", 1)
            expectCaret(if (selection.isKnown) selection.start else -1)
            return
        }
        val before = connection.getTextBeforeCursor(WORD_WINDOW, 0)?.toString().orEmpty()
        val length = kz.yerek.aireply.keyboard.layout.TextDeletion.wordLength(before)
        if (length > 0) deleteBefore(length)
    }

    /** Deletes [length] UTF-16 units right before a collapsed caret. */
    fun deleteBefore(length: Int) {
        finishComposing()
        val connection = connection ?: return
        connection.deleteSurroundingText(length, 0)
        expectCaret(if (selection.isCollapsed) (selection.start - length).coerceAtLeast(0) else -1)
    }

    /**
     * Moves the caret by whole characters (space-bar trackpad), stepping over
     * an emoji or a combined letter rather than into it. Reads just enough
     * text either side for that.
     */
    fun moveCursorBy(offset: Int) {
        finishComposing()
        val connection = connection ?: return
        val position = selection.end
        if (offset == 0 || position < 0) return
        val window = (kotlin.math.abs(offset) * CLUSTER_WINDOW).coerceAtMost(MAX_MOVE_WINDOW)
        val units = if (offset < 0) {
            val before = connection.getTextBeforeCursor(window, 0)?.toString().orEmpty()
            var index = before.length
            repeat(-offset) { if (index > 0) index = previousCluster(before, index) }
            index - before.length
        } else {
            val after = connection.getTextAfterCursor(window, 0)?.toString().orEmpty()
            var index = 0
            repeat(offset) { if (index < after.length) index = nextCluster(after, index) }
            index
        }
        if (units == 0) return
        val target = (position + units).coerceAtLeast(0)
        connection.setSelection(target, target)
        selection.expect(target)
    }

    /**
     * Puts the caret after the last character, so Add appends at the end. A
     * selection counts as text: it is kept, not typed over.
     */
    fun moveCaretToEnd() {
        finishComposing()
        val connection = connection ?: return
        val after = connection.getTextAfterCursor(MAX_CLEAR, 0)?.length ?: 0
        val selected = connection.getSelectedText(0)?.length ?: 0
        if (after == 0 && selected == 0) return
        val before = connection.getTextBeforeCursor(MAX_CLEAR, 0)?.length ?: 0
        val end = before + selected + after
        connection.setSelection(end, end)
        selection.forget()
    }

    /**
     * True when return means a new line rather than the field's action (Send,
     * Search…). A field that asks for no enter action gets a new line, which
     * is also the face the return key shows there.
     */
    val returnIsNewline: Boolean
        get() {
            val options = editorInfo?.imeOptions ?: EditorInfo.IME_ACTION_NONE
            if (options and EditorInfo.IME_FLAG_NO_ENTER_ACTION != 0) return true
            val action = options and EditorInfo.IME_MASK_ACTION
            val isMultiline = editorInfo?.inputType?.and(InputType.TYPE_TEXT_FLAG_MULTI_LINE) != 0
            return isMultiline || action == EditorInfo.IME_ACTION_NONE || action == EditorInfo.IME_ACTION_UNSPECIFIED
        }

    fun sendReturn() {
        finishComposing()
        val connection = connection ?: return
        // A multi-line field wants a newline; a single-line field with a Send or
        // Search action wants that action. Getting this backwards either sends a
        // half-written message or leaves the user unable to send at all.
        if (returnIsNewline) {
            commitText("\n")
        } else {
            val action = editorInfo?.imeOptions?.and(EditorInfo.IME_MASK_ACTION) ?: EditorInfo.IME_ACTION_NONE
            connection.performEditorAction(action)
            // Sending usually clears the field: nothing about it is predictable.
            selection.forget()
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
        finishComposing()
        val connection = connection ?: return
        val before = connection.getTextBeforeCursor(MAX_CLEAR, 0)?.length ?: 0
        val after = connection.getTextAfterCursor(MAX_CLEAR, 0)?.length ?: 0
        if (before == 0 && after == 0) return
        connection.deleteSurroundingText(before, after)
        selection.forget()
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
         * Text read to find one user-perceived character: room for the longest
         * emoji sequences (a family with skin tones is about 25 UTF-16 units).
         */
        const val CLUSTER_WINDOW = 32

        /** Upper bound on the text read for one trackpad move. */
        const val MAX_MOVE_WINDOW = 1_024

        /** Where the character ending at [offset] in [text] starts. */
        fun previousCluster(text: String, offset: Int): Int {
            if (offset <= 0) return 0
            val iterator = BreakIterator.getCharacterInstance().also { it.setText(text) }
            return iterator.preceding(offset).takeIf { it != BreakIterator.DONE } ?: 0
        }

        /** Where the character starting at [offset] in [text] ends. */
        fun nextCluster(text: String, offset: Int): Int {
            if (offset >= text.length) return text.length
            val iterator = BreakIterator.getCharacterInstance().also { it.setText(text) }
            return iterator.following(offset).takeIf { it != BreakIterator.DONE } ?: text.length
        }

        /**
         * Upper bound on a single clear. Well past any realistic chat draft, and
         * bounded so a misbehaving host cannot make this allocate without limit.
         */
        const val MAX_CLEAR = 10_000

        /** Enough to find the start of any real word. */
        const val WORD_WINDOW = 64

        /**
         * The text read when a word starts: enough for autocorrect's guard (the
         * chunk before the word) and no more, since each read crosses processes.
         */
        const val WORD_CONTEXT = 48
    }
}
