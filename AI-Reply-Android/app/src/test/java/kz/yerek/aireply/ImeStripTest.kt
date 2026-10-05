package kz.yerek.aireply

import android.text.InputType
import android.view.inputmethod.EditorInfo
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.runCurrent
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.keyboard.autocorrect.AutocorrectController
import kz.yerek.aireply.keyboard.autocorrect.Suggestion
import kz.yerek.aireply.keyboard.input.HostField
import kz.yerek.aireply.keyboard.input.KeyboardTextFieldState
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * What the suggestion strip shows and where (DESIGN §6.5, §7.6, §10.3): over
 * the keys for the host's field, in the panel's action row for the keyboard's
 * own fields; three slots at most, only while a word is being typed, and only
 * the latest keystroke's answer.
 *
 * Ұсыныстар жолағы: не көрсетіледі, қай жерде, қашан.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class ImeStripTest {

    private val dispatcher = StandardTestDispatcher()
    private val scope = TestScope(dispatcher)
    private val engine = AutocorrectTestDictionaries.engine()
    private var connection = ImeFakeInputConnection()
    private val host = HostField { connection }
    private val controller = AutocorrectController(engine, host, scope, dispatcher)

    private fun start(language: KeyboardLanguage, smartCorrection: Boolean = true, field: ImeFakeInputConnection = ImeFakeInputConnection()) {
        connection = field
        val info = EditorInfo().apply {
            inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_MULTI_LINE
            initialSelStart = field.selectionStart
            initialSelEnd = field.selectionEnd
        }
        host.startInput(info, restarting = false)
        controller.startInput(info, smartCorrection, language)
    }

    private fun type(keys: String) {
        keys.forEach { key ->
            controller.typeInHost(key.toString())
            deliverReports()
        }
        scope.runCurrent()
    }

    private fun deliverReports() {
        while (connection.reports.isNotEmpty()) {
            val report = connection.reports.removeFirst()
            controller.hostSelectionChanged(report.selectionStart, report.selectionEnd, report.composingStart, report.composingEnd)
        }
    }

    private val texts: List<String> get() = controller.suggestions.map { it.text }

    @Test
    fun `a pending correction shows the typed word, the correction and one more`() {
        start(KeyboardLanguage.RUSSIAN)
        type("сегодян")
        assertEquals(AutocorrectController.Place.HOST, controller.place)
        assertEquals(Suggestion("сегодян", Suggestion.Kind.TYPED), controller.suggestions[0])
        assertEquals(Suggestion("сегодня", Suggestion.Kind.CORRECTION), controller.suggestions[1])
        assertEquals(3, controller.suggestions.size)
    }

    @Test
    fun `completions of what is typed come first`() {
        start(KeyboardLanguage.KAZAKH)
        type("рахм")
        assertEquals("рахмет", texts.first())

        start(KeyboardLanguage.ENGLISH)
        type("tomo")
        assertEquals("tomorrow", texts.first())
    }

    @Test
    fun `the Kazakh spelling of a plainly typed word is offered first`() {
        start(KeyboardLanguage.KAZAKH)
        type("Кайда")
        assertEquals(Suggestion("Қайда", Suggestion.Kind.HINT), controller.suggestions.first())
        type(" ")
        assertEquals("never applied by itself", "Кайда ", connection.toString())
    }

    @Test
    fun `the strip goes when the word ends`() {
        start(KeyboardLanguage.RUSSIAN)
        type("сего")
        assertTrue(texts.isNotEmpty())
        type(" ")
        assertTrue(controller.suggestions.isEmpty())
        assertNull(controller.place)
    }

    @Test
    fun `nothing is suggested for handles, numbers and capitals`() {
        start(KeyboardLanguage.RUSSIAN)
        type("@превт")
        assertTrue(controller.suggestions.isEmpty())
        type(" превт123")
        assertTrue(controller.suggestions.isEmpty())
        type(" ПРИВЕТ")
        assertTrue(controller.suggestions.isEmpty())
    }

    @Test
    fun `only the latest keystroke's answer is shown`() {
        start(KeyboardLanguage.RUSSIAN)
        "сего".forEach { controller.typeInHost(it.toString()) }
        deliverReports()
        scope.runCurrent()
        assertEquals(engine.analyze("сего", "", KeyboardLanguage.RUSSIAN).suggestions, controller.suggestions)
    }

    @Test
    fun `the keyboard's own fields show it in the panel`() {
        start(KeyboardLanguage.RUSSIAN)
        val field = KeyboardTextFieldState()
        "Скажи сего".forEach { controller.typeInField(field, it.toString(), limit = null) }
        scope.runCurrent()
        assertEquals(AutocorrectController.Place.COMPOSER, controller.place)
        assertTrue("сегодня" in texts)

        assertTrue(controller.pick(controller.suggestions.first { it.text == "сегодня" }))
        assertEquals("Скажи сегодня ", field.text)
        assertTrue(controller.suggestions.isEmpty())
    }

    @Test
    fun `a caret moved inside a field shows the word before it`() {
        start(KeyboardLanguage.RUSSIAN)
        val field = KeyboardTextFieldState("Скажи сего завтра")
        field.moveCursor(10)
        controller.fieldChanged(field, limit = null)
        scope.runCurrent()
        assertTrue("сегодня" in texts)

        field.moveCursor(8)
        controller.fieldChanged(field, limit = null)
        scope.runCurrent()
        assertTrue("inside a word there is nothing to suggest", controller.suggestions.isEmpty())
    }

    @Test
    fun `with smart correction off there is no strip`() {
        start(KeyboardLanguage.RUSSIAN, smartCorrection = false)
        type("сегодян")
        assertTrue(controller.suggestions.isEmpty())
    }

    @Test
    fun `switching the layout ends the word and the strip`() {
        start(KeyboardLanguage.RUSSIAN)
        type("сегодян")
        controller.switchLanguage(KeyboardLanguage.ENGLISH)
        assertTrue(controller.suggestions.isEmpty())
        assertEquals("kept as typed", "сегодян", connection.toString())
        assertEquals("", connection.composing)
    }
}
