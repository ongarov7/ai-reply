package kz.yerek.aireply

import kotlinx.coroutines.CompletableDeferred
import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.keyboard.voice.DictationEvent
import kz.yerek.aireply.keyboard.voice.DictationMessage
import kz.yerek.aireply.keyboard.voice.DictationPhase
import kz.yerek.aireply.keyboard.voice.DictationTarget
import kz.yerek.aireply.voice.RecorderFailure
import kz.yerek.aireply.voice.TranscriptionPurpose
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * One recording from the microphone to the words: the 40-second allowance,
 * Pause / Resume / Done / Cancel, the request to the server and every way it
 * can fail - driven through a fake microphone and a fake server.
 *
 * The allowance is counted in audio the recorder actually captured, so these
 * tests move time by recording, and move the clock separately to show that
 * time without recording costs nothing.
 */
class VoiceSessionTest {

    private val h = PanelsHarness()
    private val field = FakeHostField()
    private val d get() = h.dictation

    /** 🎙 on the persona row: a recording for the app's field. */
    private fun startHost(languages: List<String> = listOf("ru", "kk", "en")) {
        d.start(DictationTarget.Host(field), languages)
        h.settle()
        h.recorder.capturing()
        h.settle()
    }

    private fun timeLeft() = h.events.filterIsInstance<DictationEvent.TimeLeft>().map { it.secondsLeft }

    // ------------------------------------------------------------ the 40 seconds

    @Test
    fun `ten seconds are recorded, recognised and put into the field`() {
        startHost()
        assertEquals(DictationPhase.RECORDING, d.phase)
        h.recorder.record(10_000)
        assertEquals(10_000, d.capturedMs)
        assertEquals(30_000, d.remainingMs)
        assertTrue("nothing is sent while recording", h.transcriber.requests.isEmpty())
        assertTrue("and nothing goes into the field", field.inserted.isEmpty())

        h.finishWith("Сәлем, қалайсың?")
        assertEquals(listOf("Сәлем, қалайсың?"), field.inserted)
        assertEquals(DictationPhase.IDLE, d.phase)
        val request = h.transcriber.requests.single()
        assertEquals(10_000, request.audio.durationMs)
        assertEquals(listOf("ru", "kk", "en"), request.languages)
        assertEquals(TranscriptionPurpose.DICTATION, request.purpose)
        assertTrue("the audio is deleted once the words are in", h.recorder.discards >= 1)
        assertTrue("no AI request", h.replyRequests.isEmpty() && h.composeRequests.isEmpty())
    }

    @Test
    fun `thirty seconds says ten are left, thirty five says five, once each`() {
        startHost()
        h.recorder.record(29_999)
        assertEquals(emptyList<Int>(), timeLeft())
        h.recorder.record(1)
        assertEquals(listOf(10), timeLeft())
        h.recorder.record(2_000)
        assertEquals("said once", listOf(10), timeLeft())
        h.recorder.record(2_999)
        assertEquals(listOf(10), timeLeft())
        h.recorder.record(1)
        assertEquals(listOf(10, 5), timeLeft())
        assertEquals(DictationPhase.RECORDING, d.phase)
        assertEquals(5_000, d.remainingMs)
    }

    @Test
    fun `at forty seconds the recording ends by itself and is recognised - and nothing else is sent`() {
        startHost()
        h.recorder.record(30_000)
        h.recorder.record(5_000)
        h.recorder.record(5_000)
        h.recorder.record(5_000)
        h.settle()
        assertEquals(40_000, d.capturedMs)
        assertFalse("the microphone closed itself", h.recorder.running)
        assertTrue(d.limitReached)
        assertEquals(listOf(10, 5, 0), timeLeft())
        assertEquals("finished the way Done does", DictationPhase.TRANSCRIBING, d.phase)
        assertEquals(40_000, h.transcriber.requests.single().audio.durationMs)

        h.transcriber.answers("Ертең кездесеміз")
        h.settle()
        assertEquals(listOf("Ертең кездесеміз"), field.inserted)
        assertTrue("the limit never starts an AI reply", h.replyRequests.isEmpty() && h.composeRequests.isEmpty())
    }

    @Test
    fun `no audio is captured after the limit`() {
        startHost()
        h.recorder.record(40_000)
        h.recorder.record(5_000)
        assertEquals(40_000, h.recorder.capturedMs)
        assertEquals(40_000, d.capturedMs)
        d.resume()
        assertEquals("no Resume once the allowance is used", 0, h.recorder.resumes)
    }

    @Test
    fun `paused at fifteen seconds, it resumes with twenty five left`() {
        startHost()
        h.recorder.record(15_000)
        d.pause()
        assertEquals(DictationPhase.PAUSED, d.phase)
        assertFalse("Pause closes the microphone", h.recorder.running)
        assertEquals(25_000, d.remainingMs)
        assertEquals(DictationEvent.Paused, h.events.last())

        d.resume()
        assertEquals(DictationPhase.STARTING, d.phase)
        h.recorder.capturing()
        assertEquals(DictationPhase.RECORDING, d.phase)
        assertEquals(25_000, d.remainingMs)
        h.recorder.record(30_000)
        assertEquals("only the 25 seconds left were taken", 40_000, d.capturedMs)
        assertTrue(d.limitReached)
    }

    @Test
    fun `a pause of a minute costs nothing`() {
        startHost()
        h.recorder.record(30_000)
        d.pause()
        h.passes(60_000)
        assertEquals(30_000, d.capturedMs)
        assertEquals(10_000, d.remainingMs)
        assertEquals(DictationPhase.PAUSED, d.phase)
        d.resume()
        h.recorder.capturing()
        h.recorder.record(9_000)
        assertEquals(1_000, d.remainingMs)
        assertEquals(DictationPhase.RECORDING, d.phase)
    }

    @Test
    fun `several pauses use the allowance once, and one file is sent`() {
        startHost()
        repeat(3) {
            h.recorder.record(10_000)
            d.pause()
            h.passes(5_000)
            d.resume()
            h.recorder.capturing()
        }
        assertEquals(30_000, d.capturedMs)
        h.recorder.record(10_000)
        h.settle()
        assertTrue(d.limitReached)
        assertEquals(1, h.recorder.starts)
        assertEquals(3, h.recorder.resumes)
        assertEquals("one recording, one file", 1, h.recorder.finishes)
        assertEquals(1, h.transcriber.requests.size)
    }

    @Test
    fun `opening the microphone takes none of the allowance`() {
        d.start(DictationTarget.Host(field), listOf("ru"))
        h.passes(2_000)
        assertEquals(DictationPhase.STARTING, d.phase)
        assertEquals(0, d.capturedMs)
        assertEquals(40_000, d.remainingMs)
    }

    // ------------------------------------------------------------------ Done

    @Test
    fun `Done twice sends one request`() {
        startHost()
        h.recorder.record(5_000)
        d.finish()
        d.finish()
        h.settle()
        assertEquals(1, h.transcriber.requests.size)
        assertEquals(DictationPhase.TRANSCRIBING, d.phase)
        assertFalse(d.canFinish)
    }

    @Test
    fun `Done while paused sends what was recorded`() {
        startHost()
        h.recorder.record(8_000)
        d.pause()
        h.finishWith("Келемін")
        assertEquals(8_000, h.transcriber.requests.single().audio.durationMs)
        assertEquals(listOf("Келемін"), field.inserted)
    }

    @Test
    fun `a tap is not a recording`() {
        startHost()
        h.recorder.record(200)
        d.finish()
        h.settle()
        assertEquals(DictationMessage.TooShort, d.message)
        assertEquals(DictationPhase.IDLE, d.phase)
        assertTrue(h.transcriber.requests.isEmpty())
    }

    @Test
    fun `nothing said leaves the field alone and says so`() {
        startHost()
        h.recorder.record(4_000)
        h.finishWith("   ")
        assertTrue(field.inserted.isEmpty())
        assertEquals(DictationMessage.NothingHeard, d.message)
        assertEquals(DictationPhase.IDLE, d.phase)
    }

    // ---------------------------------------------------------------- Cancel

    @Test
    fun `Cancel while recording sends nothing and deletes the audio`() {
        startHost()
        h.recorder.record(12_000)
        d.cancel()
        assertEquals(DictationPhase.IDLE, d.phase)
        assertFalse(h.recorder.running)
        assertTrue(h.recorder.discards >= 1)
        assertTrue(h.transcriber.requests.isEmpty())
        assertTrue(field.inserted.isEmpty())
        assertEquals(DictationEvent.Cancelled, h.events.last())
        // The microphone's late reports are ignored.
        h.recorder.record(1_000)
        assertEquals(0, d.capturedMs)
    }

    @Test
    fun `Cancel while paused, before anything is uploaded`() {
        startHost()
        h.recorder.record(6_000)
        d.pause()
        d.cancel()
        assertTrue(h.transcriber.requests.isEmpty())
        assertEquals(DictationPhase.IDLE, d.phase)
    }

    @Test
    fun `Cancel while the file is being written sends nothing`() {
        startHost()
        h.recorder.record(6_000)
        val encoding = CompletableDeferred<Unit>()
        h.recorder.encoding = encoding
        d.finish()
        assertEquals(DictationPhase.FINALIZING, d.phase)
        d.cancel()
        encoding.complete(Unit)
        h.settle()
        assertTrue(h.transcriber.requests.isEmpty())
        assertEquals(DictationPhase.IDLE, d.phase)
    }

    @Test
    fun `Cancel while recognising abandons the request and a late answer goes nowhere`() {
        startHost()
        h.recorder.record(6_000)
        d.finish()
        h.settle()
        assertTrue(h.transcriber.isWaiting)
        d.cancel()
        h.settle()
        assertEquals(1, h.transcriber.cancelled)
        assertFalse(h.transcriber.isWaiting)
        assertTrue(field.inserted.isEmpty())
        assertEquals(DictationPhase.IDLE, d.phase)
    }

    @Test
    fun `Cancel twice is harmless`() {
        startHost()
        h.recorder.record(3_000)
        d.cancel()
        d.cancel()
        assertEquals(1, h.events.count { it == DictationEvent.Cancelled })
        assertEquals(DictationPhase.IDLE, d.phase)
    }

    // -------------------------------------------------------------- failures

    @Test
    fun `a lost connection is retried once, quietly, with the same request id`() {
        startHost()
        h.recorder.record(5_000)
        d.finish()
        h.settle()
        h.transcriber.fails(AIReplyError.Offline)
        h.settle()
        assertEquals(DictationPhase.RECOVERING, d.phase)
        h.passes(1_500)
        assertEquals(DictationPhase.TRANSCRIBING, d.phase)
        assertEquals(2, h.transcriber.requests.size)
        assertEquals(1, h.transcriber.requests.map { it.requestId }.distinct().size)
        h.transcriber.answers("Жақсы")
        h.settle()
        assertEquals(listOf("Жақсы"), field.inserted)
    }

    @Test
    fun `a second failure stops with Try again, which keeps the audio and the id`() {
        startHost()
        h.recorder.record(5_000)
        d.finish()
        h.settle()
        h.transcriber.fails(AIReplyError.TimedOut)
        h.settle()
        h.passes(1_500)
        h.transcriber.fails(AIReplyError.Offline)
        h.settle()
        assertEquals(DictationPhase.ERROR, d.phase)
        assertEquals(DictationMessage.Failed(AIReplyError.Offline), d.message)
        assertTrue("Cancel and Try again are on screen", d.showsControls)
        assertFalse("no Resume", d.canResume)
        assertEquals("the audio stays for Try again", 0, h.recorder.discards)

        d.retry()
        h.settle()
        assertEquals(3, h.transcriber.requests.size)
        assertEquals(1, h.transcriber.requests.map { it.requestId }.distinct().size)
        assertEquals(1, h.recorder.finishes)
        h.transcriber.answers("Жақсы")
        h.settle()
        assertEquals(listOf("Жақсы"), field.inserted)
    }

    @Test
    fun `today's voice limit is not retried`() {
        startHost()
        h.recorder.record(5_000)
        d.finish()
        h.settle()
        h.transcriber.fails(AIReplyError.VoiceLimitReached)
        h.settle()
        assertEquals(DictationPhase.ERROR, d.phase)
        assertEquals(DictationMessage.Failed(AIReplyError.VoiceLimitReached), d.message)
        assertEquals(1, h.transcriber.requests.size)
    }

    @Test
    fun `no consent or no account - nothing is recorded at all`() {
        h.transcriber.problem = AIReplyError.ConsentRequired
        d.start(DictationTarget.Host(field), listOf("kk"))
        assertTrue(h.recorders.isEmpty())
        assertEquals(DictationMessage.Failed(AIReplyError.ConsentRequired), d.message)
        assertEquals(DictationPhase.IDLE, d.phase)
    }

    @Test
    fun `a server without transcription offers nothing`() {
        h.transcriber.isOffered = false
        d.start(DictationTarget.Host(field), listOf("kk"))
        assertTrue(h.recorders.isEmpty())
        assertNull(d.message)
    }

    @Test
    fun `a microphone that will not open says so, and nothing is recorded`() {
        h.nextStartFailure = RecorderFailure.UNAVAILABLE
        d.start(DictationTarget.Host(field), listOf("ru"))
        assertEquals(DictationPhase.IDLE, d.phase)
        assertEquals(DictationMessage.Microphone(RecorderFailure.UNAVAILABLE), d.message)
        assertTrue(h.transcriber.requests.isEmpty())
    }

    @Test
    fun `a microphone taken before any audio arrived ends the recording`() {
        d.start(DictationTarget.Host(field), listOf("ru"))
        assertEquals(DictationPhase.STARTING, d.phase)
        h.recorder.fails(RecorderFailure.INTERRUPTED)
        assertEquals(DictationPhase.IDLE, d.phase)
        assertEquals(DictationMessage.Microphone(RecorderFailure.INTERRUPTED), d.message)
    }

    @Test
    fun `a call mid-recording pauses it and keeps what was said`() {
        startHost()
        h.recorder.record(7_000)
        h.recorder.fails(RecorderFailure.INTERRUPTED)
        assertEquals(DictationPhase.PAUSED, d.phase)
        assertEquals(DictationMessage.Microphone(RecorderFailure.INTERRUPTED), d.message)
        assertEquals(7_000, d.capturedMs)
        h.finishWith("Кейін хабарласамын")
        assertEquals(listOf("Кейін хабарласамын"), field.inserted)
    }

    @Test
    fun `a permission taken away mid-recording asks for it again`() {
        startHost()
        h.recorder.record(3_000)
        h.recorder.fails(RecorderFailure.PERMISSION)
        assertEquals(DictationPhase.PAUSED, d.phase)
        assertEquals(DictationMessage.PermissionRequired, d.message)
        h.mic.granted = false
        d.resume()
        assertEquals(0, h.recorder.resumes)
        assertEquals(DictationMessage.PermissionRequired, d.message)
    }

    // ------------------------------------------------------- the app's field

    @Test
    fun `typing pauses a recording, so the keys are not recorded`() {
        startHost()
        h.recorder.record(4_000)
        d.keyPressed()
        assertEquals(DictationPhase.PAUSED, d.phase)
        assertFalse(h.recorder.running)
        d.keyPressed()
        assertEquals(1, h.recorder.pauses)
    }

    @Test
    fun `the keyboard going away pauses the recording - back over the same field it waits`() {
        startHost()
        h.recorder.record(9_000)
        d.hidden()
        assertEquals(DictationPhase.PAUSED, d.phase)
        assertFalse("never recording without the keyboard", h.recorder.running)
        h.passes(30_000)
        d.shown()
        assertEquals(DictationPhase.PAUSED, d.phase)
        assertEquals(31_000, d.remainingMs)
        d.resume()
        h.recorder.capturing()
        assertEquals(DictationPhase.RECORDING, d.phase)
    }

    @Test
    fun `back over another field, the recording is dropped`() {
        startHost()
        h.recorder.record(9_000)
        d.hidden()
        field.live = false
        d.shown()
        assertEquals(DictationPhase.IDLE, d.phase)
        assertTrue(h.recorder.discards >= 1)
        assertTrue(field.inserted.isEmpty())
    }

    @Test
    fun `another field while the keyboard stays up drops the recording too`() {
        startHost()
        h.recorder.record(9_000)
        field.live = false
        d.shown()
        assertEquals(DictationPhase.IDLE, d.phase)
    }

    @Test
    fun `a recording left too long behind the keyboard is dropped`() {
        startHost()
        h.recorder.record(9_000)
        d.hidden()
        h.passes(11 * 60 * 1000L)
        d.shown()
        assertEquals(DictationPhase.IDLE, d.phase)
    }

    @Test
    fun `words recognised while the keyboard was away wait for Insert`() {
        startHost()
        h.recorder.record(5_000)
        d.finish()
        h.settle()
        d.hidden()
        field.live = false
        h.transcriber.answers("Сәлем")
        h.settle()
        assertTrue("never into a field the keyboard is not over", field.inserted.isEmpty())
        assertEquals(DictationPhase.REVIEWING, d.phase)
        assertEquals("Сәлем", d.pendingText)
        assertTrue(d.showsHostBar)

        field.live = true
        d.shown()
        d.insertReviewed()
        assertEquals(listOf("Сәлем"), field.inserted)
        assertEquals(DictationPhase.IDLE, d.phase)
    }

    @Test
    fun `waiting words are dropped for another field`() {
        startHost()
        h.recorder.record(5_000)
        d.finish()
        h.settle()
        d.hidden()
        field.live = false
        h.transcriber.answers("Сәлем")
        h.settle()
        d.shown()
        assertEquals(DictationPhase.IDLE, d.phase)
        assertNull(d.pendingText)
        assertTrue(field.inserted.isEmpty())
    }

    @Test
    fun `the screen reader hears the steps, never the words`() {
        startHost()
        h.recorder.record(3_000)
        d.pause()
        d.resume()
        h.recorder.capturing()
        h.finishWith("Сәлем")
        assertEquals(
            listOf(
                DictationEvent.Recording,
                DictationEvent.Paused,
                DictationEvent.Recording,
                DictationEvent.Recognizing,
                DictationEvent.Inserted
            ),
            h.events
        )
    }
}
