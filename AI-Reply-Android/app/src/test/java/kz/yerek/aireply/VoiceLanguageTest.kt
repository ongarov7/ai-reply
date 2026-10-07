package kz.yerek.aireply

import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.keyboard.voice.DictationNotice
import kz.yerek.aireply.keyboard.voice.VoiceStatusText
import kz.yerek.aireply.voice.RecognitionLanguage
import kz.yerek.aireply.voice.VoiceFailure
import kz.yerek.aireply.voice.VoiceState
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Which language the recogniser listens in (the layout on screen, ҚАЗ / РУС /
 * ENG) and what the status line says - one table for both panels.
 */
class VoiceLanguageTest {

    @Test
    fun `each layout has its recognition language`() {
        assertEquals("kk-KZ", RecognitionLanguage.tagFor(KeyboardLanguage.KAZAKH))
        assertEquals("ru-RU", RecognitionLanguage.tagFor(KeyboardLanguage.RUSSIAN))
        assertEquals("en-US", RecognitionLanguage.tagFor(KeyboardLanguage.ENGLISH))
        for (layout in KeyboardLanguage.entries) {
            assertEquals(layout, RecognitionLanguage.layoutFor(RecognitionLanguage.tagFor(layout)))
        }
        assertNull(RecognitionLanguage.layoutFor(null))
        assertNull(RecognitionLanguage.layoutFor("de-DE"))
    }

    @Test
    fun `a recogniser's language list covers a tag by language, whatever the spelling`() {
        assertTrue(RecognitionLanguage.covers("kk-KZ", "kk-KZ"))
        assertTrue(RecognitionLanguage.covers("kk_KZ", "kk-KZ"))
        assertTrue(RecognitionLanguage.covers("kk", "kk-KZ"))
        assertTrue(RecognitionLanguage.covers("en-GB", "en-US"))
        assertTrue(RecognitionLanguage.covers("RU-ru", "ru-RU"))
        assertFalse(RecognitionLanguage.covers("ru-RU", "kk-KZ"))
        assertFalse(RecognitionLanguage.covers("", "kk-KZ"))
    }

    @Test
    fun `an unavailable language is named in the message`() {
        fun unavailable(tag: String?) =
            VoiceStatusText.message(VoiceState.Failed(VoiceFailure.LANGUAGE_UNAVAILABLE, tag), null)?.id
        assertEquals(R.string.voice_kb_language_unavailable_kk, unavailable("kk-KZ"))
        assertEquals(R.string.voice_kb_language_unavailable_ru, unavailable("ru-RU"))
        assertEquals(R.string.voice_kb_language_unavailable_en, unavailable("en-US"))
        assertEquals(R.string.voice_kb_unavailable, unavailable(null))
    }

    @Test
    fun `the status line says something only while there is something to say`() {
        assertNull(VoiceStatusText.message(VoiceState.Idle, null))
        assertEquals(R.string.voice_kb_listening, VoiceStatusText.message(VoiceState.Starting, null)?.id)
        assertEquals(R.string.voice_kb_listening, VoiceStatusText.message(VoiceState.Listening("да"), null)?.id)
        assertEquals(R.string.voice_kb_processing, VoiceStatusText.message(VoiceState.Processing, null)?.id)
        assertEquals(R.string.voice_kb_unavailable, VoiceStatusText.message(VoiceState.Failed(VoiceFailure.UNAVAILABLE), null)?.id)
        // The limit notice shows once the microphone is idle, never over a recording.
        assertEquals(R.string.kb_compose_err_too_long, VoiceStatusText.message(VoiceState.Idle, DictationNotice(400))?.id)
        assertEquals(R.string.voice_kb_listening, VoiceStatusText.message(VoiceState.Starting, DictationNotice(400))?.id)
    }

    @Test
    fun `a long transcript shows its newest words, cut between words`() {
        // A "line" that holds 16 characters.
        val fits = { text: String -> text.length <= 16 }
        assertEquals("скажи что да", VoiceStatusText.tail("скажи что да", fits))
        assertEquals("…я приду завтра", VoiceStatusText.tail("скажи что я приду завтра", fits))
        assertEquals("…слишкомдлинноеслово", VoiceStatusText.tail("да слишкомдлинноеслово", fits))
    }
}
