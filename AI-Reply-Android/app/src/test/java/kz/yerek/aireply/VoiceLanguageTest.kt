package kz.yerek.aireply

import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.keyboard.voice.DictationMessage
import kz.yerek.aireply.keyboard.voice.DictationPhase
import kz.yerek.aireply.keyboard.voice.VoiceMessage
import kz.yerek.aireply.keyboard.voice.VoiceStatusText
import kz.yerek.aireply.voice.RecognitionLanguage
import kz.yerek.aireply.voice.RecorderFailure
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Which languages a recording is recognised in (the layout on screen first,
 * ҚАЗ / РУС / ENG) and what the voice controls say - one table for the app's
 * field and both panels.
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
    fun `transcription hints start with the layout on screen`() {
        val all = KeyboardLanguage.CYCLE_ORDER
        assertEquals(listOf("kk", "ru", "en"), RecognitionLanguage.hints(KeyboardLanguage.KAZAKH, all))
        assertEquals(listOf("ru", "kk", "en"), RecognitionLanguage.hints(KeyboardLanguage.RUSSIAN, all))
        assertEquals(listOf("en", "kk", "ru"), RecognitionLanguage.hints(KeyboardLanguage.ENGLISH, all))
        // Kazakh speech mixes in Russian, so Russian is always a hint for it.
        assertEquals(listOf("kk", "ru"), RecognitionLanguage.hints(KeyboardLanguage.KAZAKH, listOf(KeyboardLanguage.KAZAKH)))
        assertEquals(listOf("ru"), RecognitionLanguage.hints(KeyboardLanguage.RUSSIAN, listOf(KeyboardLanguage.RUSSIAN)))
        assertEquals(listOf("en", "ru"), RecognitionLanguage.hints(KeyboardLanguage.ENGLISH, listOf(KeyboardLanguage.RUSSIAN)))
    }

    @Test
    fun `the status follows the recording, the limit announced at the start and counted down at the end`() {
        fun status(phase: DictationPhase, captured: Long, limitReached: Boolean = false, message: DictationMessage? = null) =
            VoiceStatusText.status(phase, captured, 40_000, limitReached, message, forHost = true)
        assertEquals(VoiceMessage.Text(R.string.voice_rec_limit_hint, 40), status(DictationPhase.STARTING, 0))
        assertEquals(VoiceMessage.Text(R.string.voice_rec_limit_hint, 40), status(DictationPhase.RECORDING, 1_000))
        assertEquals(VoiceMessage.Text(R.string.voice_rec_recording), status(DictationPhase.RECORDING, 18_000))
        assertEquals(VoiceMessage.Text(R.string.voice_rec_left, 10), status(DictationPhase.RECORDING, 30_000))
        assertEquals(VoiceMessage.Text(R.string.voice_rec_left, 10), status(DictationPhase.RECORDING, 30_001))
        assertEquals(VoiceMessage.Text(R.string.voice_rec_left, 5), status(DictationPhase.RECORDING, 35_000))
        assertEquals(VoiceMessage.Text(R.string.voice_rec_left, 1), status(DictationPhase.RECORDING, 39_999))
        assertEquals(VoiceMessage.Text(R.string.voice_rec_paused), status(DictationPhase.PAUSED, 15_000))
        assertEquals(VoiceMessage.Text(R.string.voice_rec_limit_reached), status(DictationPhase.PAUSED, 40_000, limitReached = true))
        assertEquals(VoiceMessage.Text(R.string.voice_rec_recognizing), status(DictationPhase.TRANSCRIBING, 12_000))
        assertEquals(VoiceMessage.Text(R.string.voice_rec_retrying), status(DictationPhase.RECOVERING, 12_000))
        assertEquals(
            VoiceMessage.Error(AIReplyError.Offline),
            status(DictationPhase.ERROR, 12_000, message = DictationMessage.Failed(AIReplyError.Offline))
        )
        assertEquals(VoiceMessage.Text(R.string.voice_rec_ready), status(DictationPhase.REVIEWING, 12_000))
        assertNull(status(DictationPhase.IDLE, 0))
        assertEquals(VoiceMessage.Text(R.string.voice_kb_no_speech), status(DictationPhase.IDLE, 0, message = DictationMessage.NothingHeard))
    }

    @Test
    fun `the timer reads minutes and seconds of the forty`() {
        assertEquals("00:00 / 00:40", VoiceStatusText.timer(0, 40_000))
        assertEquals("00:18 / 00:40", VoiceStatusText.timer(18_900, 40_000))
        assertEquals("00:40 / 00:40", VoiceStatusText.timer(40_000, 40_000))
        assertEquals(22, VoiceStatusText.secondsLeft(40_000 - 18_000))
        assertEquals(1, VoiceStatusText.secondsLeft(1))
        assertEquals(0, VoiceStatusText.secondsLeft(0))
        assertEquals("0:18/0:40", VoiceStatusText.shortTimer(18_900, 40_000))
        assertEquals("0:40/0:40", VoiceStatusText.shortTimer(40_000, 40_000))
    }

    @Test
    fun `every message has its sentence`() {
        assertEquals(VoiceMessage.Text(R.string.voice_kb_permission_needed), VoiceStatusText.message(DictationMessage.PermissionRequired))
        assertEquals(VoiceMessage.Text(R.string.voice_kb_permission_denied), VoiceStatusText.message(DictationMessage.PermissionDenied))
        assertEquals(
            VoiceMessage.Text(R.string.voice_rec_interrupted),
            VoiceStatusText.message(DictationMessage.Microphone(RecorderFailure.INTERRUPTED))
        )
        assertEquals(
            VoiceMessage.Text(R.string.voice_rec_mic_unavailable),
            VoiceStatusText.message(DictationMessage.Microphone(RecorderFailure.UNAVAILABLE))
        )
        assertEquals(VoiceMessage.Text(R.string.voice_rec_too_short), VoiceStatusText.message(DictationMessage.TooShort))
        assertEquals(VoiceMessage.Text(R.string.voice_rec_not_saved), VoiceStatusText.message(DictationMessage.NotSaved))
        assertEquals(VoiceMessage.Text(R.string.kb_compose_err_too_long, 400), VoiceStatusText.message(DictationMessage.TooLong(400)))
        assertEquals(VoiceMessage.Error(AIReplyError.VoiceLimitReached), VoiceStatusText.message(DictationMessage.Failed(AIReplyError.VoiceLimitReached)))
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
