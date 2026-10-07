package kz.yerek.aireply

import android.text.InputType
import android.view.inputmethod.EditorInfo
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.runCurrent
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.keyboard.autocorrect.AutocorrectController
import kz.yerek.aireply.keyboard.autocorrect.AutocorrectGate
import kz.yerek.aireply.keyboard.autocorrect.LearnedWords
import kz.yerek.aireply.keyboard.autocorrect.Suggestion
import kz.yerek.aireply.keyboard.input.HostField
import kz.yerek.aireply.keyboard.input.KeyboardTextFieldState
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Smart correction in a host app's field (DESIGN §6.5): the word being typed
 * is composing text, a separator applies the correction, an immediate
 * backspace takes it back, a caret moved elsewhere ends the word where it
 * stands - and with smart correction off, typing is plain `commitText`.
 *
 * Хост өрісінде теру: теріліп жатқан сөз, түзету, кері қайтару, курсор.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class ImeComposingTest {

    private val dispatcher = StandardTestDispatcher()
    private val scope = TestScope(dispatcher)
    private val learned = MemoryLearnedWordsStore()
    private val engine = AutocorrectTestDictionaries.engine(learned)

    private var connection = ImeFakeInputConnection()
    private val host = HostField { connection }
    private val controller = AutocorrectController(engine, host, scope, dispatcher)

    private lateinit var info: EditorInfo

    private fun start(
        field: ImeFakeInputConnection = ImeFakeInputConnection(),
        inputType: Int = MESSAGE,
        imeOptions: Int = 0,
        smartCorrection: Boolean = true,
        language: KeyboardLanguage = KeyboardLanguage.RUSSIAN
    ) {
        connection = field
        info = EditorInfo().apply {
            this.inputType = inputType
            this.imeOptions = imeOptions
            initialSelStart = field.selectionStart
            initialSelEnd = field.selectionEnd
        }
        host.startInput(info, restarting = false)
        controller.startInput(info, smartCorrection, language)
        scope.runCurrent()
    }

    /**
     * Types [keys] one character at a time, each word's analysis landing
     * before the next key as it does at human speed; the app's selection
     * reports arrive right away unless [late].
     */
    private fun type(keys: String, late: Boolean = false) {
        keys.forEach { key ->
            controller.typeInHost(key.toString())
            if (!late) deliverReports()
            scope.runCurrent()
        }
    }

    /** Taps the strip's [text], the app's report arriving right away. */
    private fun pick(text: String) {
        assertTrue("$text is on the strip", controller.pick(controller.suggestions.first { it.text == text }))
        deliverReports()
        scope.runCurrent()
    }

    private fun backspace(): Boolean {
        val handled = controller.deleteInHost()
        if (!handled) host.deleteBackward()
        deliverReports()
        scope.runCurrent()
        return handled
    }

    private fun deliverReports() {
        while (connection.reports.isNotEmpty()) {
            val report = connection.reports.removeFirst()
            controller.hostSelectionChanged(report.selectionStart, report.selectionEnd, report.composingStart, report.composingEnd)
        }
    }

    // ---------------------------------------------------------------- typing

    @Test
    fun `the word being typed is composing text until a separator`() {
        start()
        type("сегодян")
        assertEquals("сегодян", connection.composing)
        assertEquals("сегодян", connection.toString())
        assertFalse("nothing committed yet", "commitText" in connection.calls)

        type(" ")
        assertEquals("the correction and the space", "сегодня ", connection.toString())
        assertEquals("", connection.composing)
        assertEquals(8, connection.selectionEnd)
    }

    @Test
    fun `punctuation applies the correction too, and keeps the case`() {
        start(ImeFakeInputConnection("Ну, "))
        type("Превет,")
        assertEquals("Ну, Привет,", connection.toString())
        type(" сегодян.")
        assertEquals("Ну, Привет, сегодня.", connection.toString())
    }

    @Test
    fun `a separator typed before the word's analysis lands ends it as typed`() {
        start()
        // Faster than the background analysis: nothing has landed for this word.
        "сегодян ".forEach { key ->
            controller.typeInHost(key.toString())
            deliverReports()
        }
        assertEquals("never searched on the key press itself", "сегодян ", connection.toString())
        scope.runCurrent()

        type("сегодян")
        type(" ")
        assertEquals("once the analysis is there, the space corrects", "сегодян сегодня ", connection.toString())
    }

    @Test
    fun `a known word is kept as typed`() {
        start()
        type("привет ")
        assertEquals("привет ", connection.toString())
    }

    @Test
    fun `backspace in the word edits the composing text`() {
        start()
        type("превт")
        assertTrue(backspace())
        assertEquals("прев", connection.composing)
        type("ет ")
        assertEquals("привет ", connection.toString())
    }

    // ------------------------------------------------------------------ undo

    @Test
    fun `backspace right after a correction takes it back for good`() {
        start()
        type("сегодян ")
        assertEquals("сегодня ", connection.toString())

        assertTrue("the undo, not a plain delete", backspace())
        assertEquals("сегодян", connection.toString())
        assertEquals("the word is being typed again", "сегодян", connection.composing)

        type(" ")
        assertEquals("not corrected a second time", "сегодян ", connection.toString())
        assertEquals("learned", listOf("сегодян"), LearnedWords.decode(learned.values[KeyboardLanguage.RUSSIAN]).words)
    }

    @Test
    fun `only the very next key can undo`() {
        start()
        type("сегодян ")
        type("и")
        assertTrue("edits the new word", backspace())
        assertFalse("an ordinary delete now", backspace())
        assertEquals("сегодня", connection.toString())
    }

    @Test
    fun `a field without personalised learning rejects the correction but learns nothing`() {
        start(imeOptions = EditorInfo.IME_FLAG_NO_PERSONALIZED_LEARNING)
        type("сегодян ")
        backspace()
        type(" ")
        assertEquals("сегодян ", connection.toString())
        assertEquals(0, learned.writes)
    }

    // ----------------------------------------------------------------- caret

    @Test
    fun `late reports of the keyboard's own edits do not end the word`() {
        start()
        type("сегодян", late = true)
        assertEquals(7, connection.reports.size)
        deliverReports()
        assertEquals("сегодян", connection.composing)
        assertFalse("finishComposingText" in connection.calls)
        type(" ")
        assertEquals("сегодня ", connection.toString())
    }

    @Test
    fun `a caret moved elsewhere ends the word where it stands`() {
        start(ImeFakeInputConnection("Ну "))
        type("сегодян")
        connection.moveCaretExternally(0)
        deliverReports()

        assertEquals("finishComposingText", connection.calls.last())
        assertEquals("", connection.composing)
        assertEquals("the text stays as typed", "Ну сегодян", connection.toString())
        assertEquals("the caret stays where the user put it", 0, connection.selectionEnd)
        assertTrue("no strip for a word that is gone", controller.suggestions.isEmpty())

        // The caret is at the start of a word now: typing there stays plain.
        type("А")
        assertEquals("АНу сегодян", connection.toString())
        assertEquals("", connection.composing)
    }

    @Test
    fun `the app clearing the field after sending ends the word`() {
        start()
        type("прив")
        connection.clearExternally()
        deliverReports()
        type("да")
        assertEquals("a fresh word, nothing of the old one", "да", connection.toString())
        assertEquals("да", connection.composing)
    }

    @Test
    fun `typing over a selection replaces it with the new word`() {
        start(ImeFakeInputConnection("Привет мир"))
        connection.selectExternally(7, 10)
        deliverReports()
        type("друг")
        assertEquals("Привет друг", connection.toString())
        assertEquals("друг", connection.composing)
    }

    @Test
    fun `typing inside a word stays plain`() {
        start(ImeFakeInputConnection("привет", caret = 3))
        type("м")
        assertEquals("примвет", connection.toString())
        assertFalse("setComposingText" in connection.calls)
    }

    @Test
    fun `a restarted field keeps the word as typed`() {
        start()
        type("прив")
        host.startInput(info, restarting = true)
        assertEquals("finishComposingText", connection.calls.last())
        assertEquals("", connection.composing)
        assertEquals("прив", connection.toString())
    }

    @Test
    fun `the keyboard shown again over the same field keeps the caret it knows`() {
        start(ImeFakeInputConnection("Ну "))
        type("привет ")
        // Hidden and shown again: the same EditorInfo, its initial caret long gone.
        controller.endWord()
        host.startInput(info, restarting = false)
        controller.startInput(info, smartCorrection = true, language = KeyboardLanguage.RUSSIAN)
        type("сегодян ")
        assertEquals("Ну привет сегодня ", connection.toString())
        assertFalse("no word was cut short", "finishComposingText" in connection.calls)
    }

    @Test
    fun `Send takes the word as typed`() {
        start(inputType = InputType.TYPE_CLASS_TEXT, imeOptions = EditorInfo.IME_ACTION_SEND)
        type("сегодян")
        controller.endWord()
        host.sendReturn()
        assertEquals("сегодян", connection.toString())
        assertEquals(listOf("finishComposingText", "performEditorAction"), connection.calls.takeLast(2))
    }

    @Test
    fun `a new line in a message ends the word like a space`() {
        start()
        type("сегодян\n")
        assertEquals("сегодня\n", connection.toString())
    }

    @Test
    fun `every other edit ends the word first`() {
        start()
        type("сегодян")
        host.moveCursorBy(-2)
        assertEquals(listOf("finishComposingText", "setSelection"), connection.calls.takeLast(2))
        assertEquals("сегодян", connection.toString())
    }

    // ----------------------------------------------------------------- strip

    @Test
    fun `a tap on the strip replaces the word and adds a space`() {
        start()
        type("сегодян")
        val correction = controller.suggestions.first { it.kind == Suggestion.Kind.CORRECTION }
        assertTrue(controller.pick(correction))
        assertEquals("сегодня ", connection.toString())
        assertTrue(controller.suggestions.isEmpty())
    }

    @Test
    fun `punctuation after a picked word takes the space's place`() {
        start()
        type("сего")
        pick("сегодня")
        assertEquals("сегодня ", connection.toString())

        type(",")
        assertEquals("Gboard's «слово, », not «слово ,»", "сегодня, ", connection.toString())
        assertEquals(9, connection.selectionEnd)
        type("и")
        assertEquals("сегодня, и", connection.toString())
    }

    @Test
    fun `every mark that follows a word replaces the picked word's space`() {
        listOf(".", ",", "!", "?", ";", ":").forEach { mark ->
            start()
            type("сего")
            pick("сегодня")
            type(mark)
            assertEquals("сегодня$mark ", connection.toString())
            type(mark)
            assertEquals("a second mark follows the first", "сегодня$mark$mark ", connection.toString())
        }
    }

    @Test
    fun `a space after a picked word is not doubled`() {
        start()
        type("сего")
        pick("сегодня")
        type(" ")
        assertEquals("сегодня ", connection.toString())
        type("и")
        assertEquals("сегодня и", connection.toString())

        // After a mark that took the space's place, too.
        type(" сего")
        pick("сегодня")
        type(", ")
        assertEquals("сегодня и сегодня, ", connection.toString())
    }

    @Test
    fun `only the very next key finds the picked word's space`() {
        start()
        type("сего")
        pick("сегодня")
        type("и,")
        assertEquals("сегодня и,", connection.toString())

        start()
        type("сего")
        pick("сегодня")
        assertFalse("backspace deletes the space as usual", backspace())
        type(",")
        assertEquals("сегодня,", connection.toString())
    }

    @Test
    fun `a caret moved after the pick leaves the space before it alone`() {
        start(ImeFakeInputConnection("Ну "))
        type("сего")
        pick("сегодня")
        connection.moveCaretExternally(3)
        deliverReports()
        type(",")
        assertEquals("Ну ,сегодня ", connection.toString())
    }

    @Test
    fun `tapping what was typed keeps it and learns it`() {
        start()
        type("сегодян")
        assertTrue(controller.pick(controller.suggestions.first { it.kind == Suggestion.Kind.TYPED }))
        assertEquals("сегодян ", connection.toString())
        assertTrue(engine.isKnown("сегодян", KeyboardLanguage.RUSSIAN))
    }

    // ---------------------------------------------------------------- gating

    @Test
    fun `with smart correction off typing is plain commitText`() {
        start(smartCorrection = false)
        type("сегодян ")
        assertFalse(backspace())
        assertEquals("сегодян", connection.toString())
        assertEquals(setOf("commitText", "deleteSurroundingText"), connection.calls.toSet())
        assertTrue(controller.suggestions.isEmpty())
    }

    @Test
    fun `a password field is typed plainly`() {
        start(inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_PASSWORD)
        assertFalse(controller.correctsHost)
        type("сегодян ")
        assertEquals("сегодян ", connection.toString())
        assertEquals(setOf("commitText"), connection.calls.toSet())
    }

    @Test
    fun `fields that are not prose are left alone`() {
        val text = InputType.TYPE_CLASS_TEXT
        listOf(
            text or InputType.TYPE_TEXT_VARIATION_PASSWORD,
            text or InputType.TYPE_TEXT_VARIATION_VISIBLE_PASSWORD,
            text or InputType.TYPE_TEXT_VARIATION_WEB_PASSWORD,
            text or InputType.TYPE_TEXT_VARIATION_URI,
            text or InputType.TYPE_TEXT_VARIATION_EMAIL_ADDRESS,
            text or InputType.TYPE_TEXT_VARIATION_WEB_EMAIL_ADDRESS,
            text or InputType.TYPE_TEXT_VARIATION_FILTER,
            text or InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS,
            InputType.TYPE_NULL,
            InputType.TYPE_CLASS_NUMBER,
            InputType.TYPE_CLASS_PHONE,
            InputType.TYPE_CLASS_DATETIME
        ).forEach { type -> assertFalse("input type $type", AutocorrectGate.allows(type, enabled = true)) }

        assertTrue(AutocorrectGate.allows(MESSAGE, enabled = true))
        assertTrue(AutocorrectGate.allows(text or InputType.TYPE_TEXT_VARIATION_PERSON_NAME, enabled = true))
        assertFalse("the user's setting", AutocorrectGate.allows(MESSAGE, enabled = false))
        assertFalse(AutocorrectGate.learns(EditorInfo.IME_FLAG_NO_PERSONALIZED_LEARNING or EditorInfo.IME_ACTION_SEND))
        assertTrue(AutocorrectGate.learns(EditorInfo.IME_ACTION_SEND))
    }

    // ------------------------------------------------- the keyboard's own fields

    @Test
    fun `the keyboard's own fields are corrected and undone the same way`() {
        start()
        val field = KeyboardTextFieldState()
        "Ответь что сегодян ".forEach {
            controller.typeInField(field, it.toString(), limit = 400)
            scope.runCurrent()
        }
        assertEquals("Ответь что сегодня ", field.text)

        assertTrue(controller.deleteInField(field, limit = 400))
        assertEquals("Ответь что сегодян", field.text)
        assertEquals(field.text.length, field.cursor)
    }

    @Test
    fun `a correction that would pass the field's limit is not applied`() {
        start()
        val field = KeyboardTextFieldState()
        "спасиб".forEach { controller.typeInField(field, it.toString(), limit = 7) }
        scope.runCurrent()
        assertTrue("the space still fits", controller.typeInField(field, " ", limit = 7))
        assertEquals("спасиб ", field.text)
    }

    @Test
    fun `the keyboard's own fields take punctuation after a picked word the same way`() {
        start()
        val field = KeyboardTextFieldState()
        "Скажи сего".forEach { controller.typeInField(field, it.toString(), limit = 400) }
        scope.runCurrent()
        assertTrue(controller.pick(controller.suggestions.first { it.text == "сегодня" }))
        assertEquals("Скажи сегодня ", field.text)

        assertTrue(controller.typeInField(field, ".", limit = 400))
        assertEquals("Скажи сегодня. ", field.text)
        assertFalse("the space is there already", controller.typeInField(field, " ", limit = 400))
        assertEquals("Скажи сегодня. ", field.text)
        assertEquals(field.text.length, field.cursor)
    }

    @Test
    fun `at the field's limit the mark still replaces the picked word's space`() {
        start()
        val field = KeyboardTextFieldState()
        "Скажи сего".forEach { controller.typeInField(field, it.toString(), limit = 14) }
        scope.runCurrent()
        assertTrue(controller.pick(controller.suggestions.first { it.text == "сегодня" }))
        assertEquals(14, field.text.length)

        assertTrue(controller.typeInField(field, ",", limit = 14))
        assertEquals("Скажи сегодня,", field.text)
    }

    private companion object {
        /** What messengers ask for: multi-line short-message text with sentence caps. */
        const val MESSAGE = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_SHORT_MESSAGE or
            InputType.TYPE_TEXT_FLAG_MULTI_LINE or InputType.TYPE_TEXT_FLAG_CAP_SENTENCES
    }
}
