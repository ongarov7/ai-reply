package kz.yerek.aireply

import android.content.ClipDescription
import android.text.InputType
import android.view.inputmethod.EditorInfo
import kz.yerek.aireply.keyboard.autocorrect.AutocorrectGate
import kz.yerek.aireply.keyboard.input.ContextTextProvider
import kz.yerek.aireply.keyboard.input.HostField
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * What the keyboard does to the host's field: the return key, deleting and
 * moving over whole characters, Add at the end, and the fields and clips it
 * leaves alone.
 *
 * Хост өрісі: Enter, бүтін таңбаны өшіру, курсор, белгіленген мәтін.
 */
class ImeHostFieldTest {

    private var connection = ImeFakeInputConnection()
    private val host = HostField { connection }

    private fun start(
        field: ImeFakeInputConnection,
        inputType: Int = TEXT,
        imeOptions: Int = 0
    ) {
        connection = field
        host.startInput(
            EditorInfo().apply {
                this.inputType = inputType
                this.imeOptions = imeOptions
                initialSelStart = field.selectionStart
                initialSelEnd = field.selectionEnd
            },
            restarting = false
        )
    }

    private fun deliverReports() {
        while (connection.reports.isNotEmpty()) {
            val report = connection.reports.removeFirst()
            host.selectionChanged(report.selectionStart, report.selectionEnd, report.composingStart, report.composingEnd)
        }
    }

    // ------------------------------------------------------------- return

    @Test
    fun `a field that asks for no enter action gets a new line, as its face says`() {
        start(ImeFakeInputConnection(), imeOptions = EditorInfo.IME_ACTION_SEND)
        assertFalse("Send sends", host.returnIsNewline)

        start(ImeFakeInputConnection(), imeOptions = EditorInfo.IME_ACTION_SEND or EditorInfo.IME_FLAG_NO_ENTER_ACTION)
        assertTrue(host.returnIsNewline)

        start(ImeFakeInputConnection(), inputType = TEXT or InputType.TYPE_TEXT_FLAG_MULTI_LINE, imeOptions = EditorInfo.IME_ACTION_SEND)
        assertTrue(host.returnIsNewline)

        start(ImeFakeInputConnection(), imeOptions = EditorInfo.IME_ACTION_NONE)
        assertTrue(host.returnIsNewline)
    }

    // ------------------------------------------------------------- delete

    @Test
    fun `delete takes a whole flag, family or accented letter`() {
        listOf(
            "Сәлем 🇰🇿" to "Сәлем ",
            "ок 👨‍👩‍👧" to "ок ",
            "👍🏽" to "",
            "café" to "caf",
            "ab" to "a"
        ).forEach { (before, after) ->
            start(ImeFakeInputConnection(before))
            host.deleteBackward()
            assertEquals(before, after, connection.toString())
        }
    }

    @Test
    fun `delete with a selection removes just the selection`() {
        start(ImeFakeInputConnection("Привет мир").apply { selectExternally(0, 7) })
        connection.reports.clear()
        host.deleteBackward()
        assertEquals("мир", connection.toString())
    }

    @Test
    fun `an empty field stays empty`() {
        start(ImeFakeInputConnection(""))
        host.deleteBackward()
        assertEquals("", connection.toString())
        assertFalse("deleteSurroundingText" in connection.calls)
    }

    // --------------------------------------------------------------- move

    @Test
    fun `the trackpad steps over an emoji instead of into it`() {
        // a · 🇰🇿 (four UTF-16 units) · b
        start(ImeFakeInputConnection("a🇰🇿b"))
        host.moveCursorBy(-2)
        assertEquals(1, connection.selectionStart)
        deliverReports()

        host.moveCursorBy(1)
        assertEquals(5, connection.selectionStart)
        deliverReports()

        host.moveCursorBy(1)
        assertEquals(6, connection.selectionStart)
    }

    @Test
    fun `the trackpad stops at the ends`() {
        start(ImeFakeInputConnection("ok", caret = 1))
        host.moveCursorBy(-5)
        assertEquals(0, connection.selectionStart)
        deliverReports()
        host.moveCursorBy(9)
        assertEquals(2, connection.selectionStart)
    }

    // ---------------------------------------------------------------- Add

    @Test
    fun `Add goes after the last character even with text selected`() {
        start(ImeFakeInputConnection("Hello world").apply { selectExternally(0, 5) })
        connection.reports.clear()

        host.moveCaretToEnd()

        assertEquals(11, connection.selectionStart)
        assertEquals(11, connection.selectionEnd)
        assertEquals("the selection is kept, not typed over", "Hello world", connection.toString())
    }

    @Test
    fun `Add from the middle of the text`() {
        start(ImeFakeInputConnection("Hello world", caret = 3))
        host.moveCaretToEnd()
        assertEquals(11, connection.selectionStart)
    }

    // ------------------------------------------------------- autocorrect gate

    @Test
    fun `names and addresses are never corrected by a separator`() {
        val text = InputType.TYPE_CLASS_TEXT
        assertFalse(AutocorrectGate.autoReplaces(text or InputType.TYPE_TEXT_VARIATION_PERSON_NAME))
        assertFalse(AutocorrectGate.autoReplaces(text or InputType.TYPE_TEXT_VARIATION_POSTAL_ADDRESS))
        assertFalse(
            AutocorrectGate.autoReplaces(
                text or InputType.TYPE_TEXT_VARIATION_PERSON_NAME or InputType.TYPE_TEXT_FLAG_CAP_WORDS
            )
        )
        assertTrue(AutocorrectGate.autoReplaces(TEXT))
        assertTrue(AutocorrectGate.autoReplaces(text))
        assertTrue("no EditorInfo: as before", AutocorrectGate.autoReplaces(null as EditorInfo?))
        // Still suggested there: only the automatic replacement is off.
        assertTrue(AutocorrectGate.allows(EditorInfo().apply { inputType = text or InputType.TYPE_TEXT_VARIATION_PERSON_NAME }, true))
    }

    // ----------------------------------------------------------- clipboard

    @Test
    fun `a clip flagged sensitive is refused on every API level`() {
        assertEquals(
            "the platform's own key, spelled out for API 26-32",
            ClipDescription.EXTRA_IS_SENSITIVE,
            ContextTextProvider.EXTRA_IS_SENSITIVE
        )
        assertTrue(ContextTextProvider.isMarkedSensitive { key -> key == "android.content.extra.IS_SENSITIVE" })
        assertFalse(ContextTextProvider.isMarkedSensitive { false })
        assertFalse("no extras", ContextTextProvider.isMarkedSensitive { null })
        assertFalse("unreadable extras", ContextTextProvider.isMarkedSensitive { error("bad parcel") })
    }

    private companion object {
        const val TEXT = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_SHORT_MESSAGE
    }
}
