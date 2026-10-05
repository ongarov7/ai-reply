package kz.yerek.aireply

import android.os.Bundle
import android.os.Handler
import android.view.KeyEvent
import android.view.inputmethod.CompletionInfo
import android.view.inputmethod.CorrectionInfo
import android.view.inputmethod.ExtractedText
import android.view.inputmethod.ExtractedTextRequest
import android.view.inputmethod.InputConnection
import android.view.inputmethod.InputContentInfo

/**
 * A host app's text field, the way the framework's editor answers an input
 * method: text, a selection, a composing region, batch edits, and one
 * selection report per change (one per batch) that the test delivers to the
 * keyboard whenever it likes - immediately, late, or never.
 *
 * Хост өрісінің үлгісі: мәтін, курсор, теріліп жатқан аймақ.
 */
class ImeFakeInputConnection(initial: String = "", caret: Int = initial.length) : InputConnection {

    /** What `onUpdateSelection` would carry: the selection and the composing region (-1, -1: none). */
    data class Report(val selectionStart: Int, val selectionEnd: Int, val composingStart: Int, val composingEnd: Int)

    val text = StringBuilder(initial)
    var selectionStart = caret
        private set
    var selectionEnd = caret
        private set
    var composingStart = -1
        private set
    var composingEnd = -1
        private set

    /** Every editing call, by name, in order. */
    val calls = mutableListOf<String>()

    /** Selection reports not yet delivered to the keyboard. */
    val reports = ArrayDeque<Report>()

    private var batchDepth = 0

    val composing: String
        get() = if (composingStart < 0) "" else text.substring(composingStart, composingEnd)

    override fun toString(): String = text.toString()

    /** The user (or the app) moves the caret: the editor keeps its composing region and reports. */
    fun moveCaretExternally(position: Int) {
        selectionStart = position
        selectionEnd = position
        report()
    }

    /** The app empties the field, as a messenger does after sending. */
    fun clearExternally() {
        text.clear()
        composingStart = -1
        composingEnd = -1
        moveCaretExternally(0)
    }

    /** The user selects text. */
    fun selectExternally(start: Int, end: Int) {
        selectionStart = start
        selectionEnd = end
        report()
    }

    private fun report() {
        if (batchDepth == 0) reports.addLast(Report(selectionStart, selectionEnd, composingStart, composingEnd))
    }

    /** Replaces the composing region, or else the selection, with [value]; returns where it starts. */
    private fun replaceTarget(value: CharSequence): Int {
        val start = if (composingStart >= 0) composingStart else minOf(selectionStart, selectionEnd)
        val end = if (composingStart >= 0) composingEnd else maxOf(selectionStart, selectionEnd)
        text.replace(start, end, value.toString())
        selectionStart = start + value.length
        selectionEnd = selectionStart
        return start
    }

    // ------------------------------------------------------------- reading

    override fun getTextBeforeCursor(n: Int, flags: Int): CharSequence {
        val end = minOf(selectionStart, selectionEnd)
        return text.substring(maxOf(0, end - n), end)
    }

    override fun getTextAfterCursor(n: Int, flags: Int): CharSequence {
        val start = maxOf(selectionStart, selectionEnd)
        return text.substring(start, minOf(text.length, start + n))
    }

    override fun getSelectedText(flags: Int): CharSequence? =
        if (selectionStart == selectionEnd) null else text.substring(minOf(selectionStart, selectionEnd), maxOf(selectionStart, selectionEnd))

    override fun getCursorCapsMode(reqModes: Int): Int = 0

    override fun getExtractedText(request: ExtractedTextRequest?, flags: Int): ExtractedText? = null

    // ------------------------------------------------------------- editing

    override fun setComposingText(text: CharSequence, newCursorPosition: Int): Boolean {
        calls += "setComposingText"
        val start = replaceTarget(text)
        if (text.isEmpty()) {
            composingStart = -1
            composingEnd = -1
        } else {
            composingStart = start
            composingEnd = start + text.length
        }
        report()
        return true
    }

    override fun commitText(text: CharSequence, newCursorPosition: Int): Boolean {
        calls += "commitText"
        replaceTarget(text)
        composingStart = -1
        composingEnd = -1
        report()
        return true
    }

    override fun finishComposingText(): Boolean {
        calls += "finishComposingText"
        if (composingStart >= 0) {
            composingStart = -1
            composingEnd = -1
            report()
        }
        return true
    }

    override fun setComposingRegion(start: Int, end: Int): Boolean {
        calls += "setComposingRegion"
        composingStart = start
        composingEnd = end
        report()
        return true
    }

    override fun deleteSurroundingText(beforeLength: Int, afterLength: Int): Boolean {
        calls += "deleteSurroundingText"
        val start = minOf(selectionStart, selectionEnd)
        val end = maxOf(selectionStart, selectionEnd)
        val afterEnd = minOf(text.length, end + afterLength)
        text.delete(end, afterEnd)
        val beforeStart = maxOf(0, start - beforeLength)
        text.delete(beforeStart, start)
        selectionStart = beforeStart
        selectionEnd = beforeStart + (end - start)
        composingStart = -1
        composingEnd = -1
        report()
        return true
    }

    override fun deleteSurroundingTextInCodePoints(beforeLength: Int, afterLength: Int): Boolean =
        deleteSurroundingText(beforeLength, afterLength)

    override fun setSelection(start: Int, end: Int): Boolean {
        calls += "setSelection"
        selectionStart = start
        selectionEnd = end
        report()
        return true
    }

    override fun performEditorAction(editorAction: Int): Boolean {
        calls += "performEditorAction"
        return true
    }

    override fun beginBatchEdit(): Boolean {
        batchDepth++
        return true
    }

    override fun endBatchEdit(): Boolean {
        batchDepth--
        if (batchDepth == 0) report()
        return batchDepth > 0
    }

    // ----------------------------------------------------------- not used

    override fun commitCompletion(text: CompletionInfo?): Boolean = false
    override fun commitCorrection(correctionInfo: CorrectionInfo?): Boolean = false
    override fun performContextMenuAction(id: Int): Boolean = false
    override fun sendKeyEvent(event: KeyEvent?): Boolean = false
    override fun clearMetaKeyStates(states: Int): Boolean = false
    override fun reportFullscreenMode(enabled: Boolean): Boolean = false
    override fun performPrivateCommand(action: String?, data: Bundle?): Boolean = false
    override fun requestCursorUpdates(cursorUpdateMode: Int): Boolean = false
    override fun getHandler(): Handler? = null
    override fun closeConnection() = Unit
    override fun commitContent(inputContentInfo: InputContentInfo, flags: Int, opts: Bundle?): Boolean = false
}
