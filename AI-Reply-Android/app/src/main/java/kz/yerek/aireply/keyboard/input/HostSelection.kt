package kz.yerek.aireply.keyboard.input

/**
 * Where the caret is in the host's field, as far as the keyboard can tell, and
 * which selection reports are only echoes of the keyboard's own edits.
 *
 * Хост өрісіндегі курсор: өз өзгерістеріміздің жаңғырығын сыртқы өзгерістен ажырату.
 *
 * WHY. `onUpdateSelection` arrives asynchronously, one report per edit and
 * often several keystrokes late. A report is the keyboard's own edit coming
 * back when it matches a state one of its edits produced; anything else - a
 * tap elsewhere, a paste, the app clearing the field after sending - is
 * external, and a word being typed must end there. Each edit records the state
 * it leaves; a report consumes every state up to the one it matches, so late
 * and coalesced reports (a batch edit reports once) are both recognised.
 *
 * Positions are UTF-16 offsets in the host's text; -1 means unknown. While
 * unknown, nothing is predicted and the keyboard does not start a word.
 */
internal class HostSelection {

    /** A selection with the composing region the app would report for it (-1, -1: none). */
    private data class State(val start: Int, val end: Int, val composingStart: Int, val composingEnd: Int)

    /** The selection after the keyboard's latest edit, or as last reported. */
    var start: Int = -1
        private set
    var end: Int = -1
        private set

    /** The last external report showed a composing region the keyboard did not make. */
    var appHasComposing: Boolean = false
        private set

    private val expected = ArrayDeque<State>()

    val isKnown: Boolean get() = start >= 0 && end >= start

    val isCollapsed: Boolean get() = isKnown && start == end

    /** A new field, or one the app restarted: what its EditorInfo says, nothing expected. */
    fun reset(start: Int, end: Int) {
        this.start = start
        this.end = end
        appHasComposing = false
        expected.clear()
    }

    /** An edit whose outcome the keyboard cannot predict (an editor action, say). */
    fun forget() {
        start = -1
        end = -1
        expected.clear()
    }

    /**
     * The keyboard's own edit leaves the caret at [caret], with composing text
     * from [composingStart] to [composingEnd], or none.
     */
    fun expect(caret: Int, composingStart: Int = -1, composingEnd: Int = -1) {
        start = caret
        end = caret
        if (composingStart < 0) appHasComposing = false
        expected.addLast(State(caret, caret, composingStart, composingEnd))
        while (expected.size > MAX_EXPECTED) expected.removeFirst()
    }

    /**
     * A selection report from the app. True when it is external: not a state
     * any of the keyboard's pending edits produced.
     */
    fun report(selectionStart: Int, selectionEnd: Int, composingStart: Int, composingEnd: Int): Boolean {
        val reported = State(selectionStart, selectionEnd, composingStart, composingEnd)
        val index = expected.indexOf(reported)
        if (index >= 0) {
            repeat(index + 1) { expected.removeFirst() }
            return false
        }
        // A repeat of a state already consumed (some editors report twice).
        if (expected.isEmpty() && selectionStart == start && selectionEnd == end && composingStart < 0 && !appHasComposing) {
            return false
        }
        reset(selectionStart, selectionEnd)
        appHasComposing = composingStart >= 0
        return true
    }

    private companion object {
        /** Far more edits than ever wait for their reports; bounded all the same. */
        const val MAX_EXPECTED = 32
    }
}
