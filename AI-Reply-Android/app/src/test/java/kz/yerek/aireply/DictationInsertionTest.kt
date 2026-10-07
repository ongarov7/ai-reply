package kz.yerek.aireply

import kz.yerek.aireply.keyboard.input.KeyboardTextFieldState
import kz.yerek.aireply.keyboard.voice.DictationInsertion
import kz.yerek.aireply.keyboard.voice.DictationInsertion.Result
import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * Dictated words join the request the way a careful typist would have typed
 * them: at the caret, one space where one belongs, a capital at a sentence
 * start, and never past the limit.
 */
class DictationInsertionTest {

    /** [before]|[after] with the caret at the bar. */
    private fun field(before: String, after: String = ""): KeyboardTextFieldState =
        KeyboardTextFieldState(before + after).apply { moveCursor(before.length) }

    private fun KeyboardTextFieldState.withCaret(): String = text.substring(0, cursor) + "|" + text.substring(cursor)

    private fun dictate(before: String, heard: String, after: String = "", limit: Int? = null): Pair<String, Result> {
        val state = field(before, after)
        val result = DictationInsertion.insert(state, heard, limit)
        return state.withCaret() to result
    }

    @Test
    fun `into an empty request it starts a sentence`() {
        assertEquals("Вежливо откажи|" to Result.INSERTED, dictate("", "вежливо откажи"))
        assertEquals("Сәлем|" to Result.INSERTED, dictate("", "сәлем"))
        assertEquals("Say yes|" to Result.INSERTED, dictate("", "say yes"))
    }

    @Test
    fun `after a word it adds exactly one space and keeps the casing`() {
        assertEquals("Скажи, что согласен|" to Result.INSERTED, dictate("Скажи,", "что согласен"))
        assertEquals("Откажи вежливо|" to Result.INSERTED, dictate("Откажи ", "вежливо"))
        assertEquals("Откажи Алмасу|" to Result.INSERTED, dictate("Откажи", "Алмасу"))
    }

    @Test
    fun `after a full stop the next sentence gets its capital`() {
        assertEquals("Откажи. Но вежливо|" to Result.INSERTED, dictate("Откажи.", "но вежливо"))
        assertEquals("Откажи! Спасибо|" to Result.INSERTED, dictate("Откажи! ", "спасибо"))
        assertEquals("Привет\nКак дела|" to Result.INSERTED, dictate("Привет\n", "как дела"))
    }

    @Test
    fun `no space after an opening bracket or quote`() {
        assertEquals("Скажи (да|" to Result.INSERTED, dictate("Скажи (", "да"))
        assertEquals("Напиши «спасибо|" to Result.INSERTED, dictate("Напиши «", "спасибо"))
        assertEquals("Write \"thanks|" to Result.INSERTED, dictate("Write \"", "thanks"))
        // ...but a quote that closes a word is followed by one.
        assertEquals("Скажи \"да\" и|" to Result.INSERTED, dictate("Скажи \"да\"", "и"))
    }

    @Test
    fun `after an emoji there is a space`() {
        assertEquals("Спасибо 😊 до встречи|" to Result.INSERTED, dictate("Спасибо 😊", "до встречи"))
    }

    @Test
    fun `in the middle of the text it is spaced on both sides and the caret follows it`() {
        assertEquals("Скажи да| что приду" to Result.INSERTED, dictate("Скажи", "да", after = "что приду"))
        assertEquals("Скажи да| что приду" to Result.INSERTED, dictate("Скажи", "да", after = " что приду"))
        assertEquals("Скажи да|." to Result.INSERTED, dictate("Скажи", "да", after = "."))
        assertEquals("Да, приду| завтра" to Result.INSERTED, dictate("", "да, приду", after = "завтра"))
    }

    @Test
    fun `runs of whitespace from the recogniser collapse to one space`() {
        assertEquals("Да нет|" to Result.INSERTED, dictate("", "  да \n  нет  "))
    }

    @Test
    fun `nothing heard leaves the field untouched`() {
        assertEquals("Скажи|" to Result.EMPTY, dictate("Скажи", "   "))
    }

    @Test
    fun `at the limit only whole words that fit go in`() {
        // 16 characters typed, 20 allowed: "раз" fits, "раз два" does not.
        val (text, result) = dictate("Ответь коротко: ", "раз два три", limit = 20)
        assertEquals(Result.TRIMMED, result)
        assertEquals("Ответь коротко: раз|", text)
        assertEquals(19, text.length - 1)
    }

    @Test
    fun `the limit counts the spaces it adds`() {
        val (text, result) = dictate("Да", "нет", limit = 6)
        assertEquals("Да нет|" to Result.INSERTED, text to result)
        assertEquals("Да|" to Result.NOTHING_FIT, dictate("Да", "нет", limit = 5))
    }

    @Test
    fun `a full field takes nothing and keeps its caret`() {
        assertEquals("12345|" to Result.NOTHING_FIT, dictate("12345", "ещё", limit = 5))
    }

    @Test
    fun `limits are counted in characters, not UTF-16 units`() {
        // The emoji is one character (two UTF-16 units): "😊 да" is 4 of 4.
        assertEquals("😊 да|" to Result.INSERTED, dictate("😊", "да", limit = 4))
    }
}
