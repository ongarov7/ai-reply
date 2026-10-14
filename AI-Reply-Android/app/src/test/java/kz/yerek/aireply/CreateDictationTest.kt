package kz.yerek.aireply

import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.keyboard.reply.ReplyComposerFlow
import kz.yerek.aireply.keyboard.voice.DictationEvent
import kz.yerek.aireply.keyboard.voice.DictationMessage
import kz.yerek.aireply.keyboard.voice.DictationPhase
import kz.yerek.aireply.voice.MicPermission
import kz.yerek.aireply.voice.TranscriptionPurpose
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The microphone in the two AI panels - ✨ Create and the reply composer -
 * recording an instruction: «Ответь клиенту на казахском, вежливо и коротко».
 *
 * What these pin, from the customer's request: the words come back from the
 * server and land in the request at the caret, whole and editable; NOTHING AI
 * is ever sent because of voice - Reply and Write wait while a recording is
 * on or being recognised, and are tapped by the user; an instruction past
 * its limit is never cut, it waits to be shortened; and every way of leaving
 * ends the recording cleanly.
 */
class CreateDictationTest {

    private val h = PanelsHarness()

    private val request: String get() = h.compose.session!!.instruction.text

    // ---------------------------------------------------------------- basics

    @Test
    fun `a recorded request is recognised into the field and nothing AI is sent`() {
        h.openCreate()
        h.startRecording(4_000)
        assertEquals(DictationPhase.RECORDING, h.dictation.phase)
        assertTrue("no live words: nothing is in the field yet", request.isEmpty())

        h.dictation.finish()
        h.settle()
        assertEquals(DictationPhase.TRANSCRIBING, h.dictation.phase)
        h.transcriber.answers("ответь клиенту вежливо и коротко")
        h.settle()

        assertEquals("Ответь клиенту вежливо и коротко", request)
        assertEquals(DictationPhase.REVIEWING, h.dictation.phase)
        assertEquals("still Create, still the request", ReplyComposerFlow.Stage.Composing, h.compose.flow.stage)
        assertEquals(TranscriptionPurpose.INSTRUCTION, h.transcriber.requests.single().purpose)
        assertTrue("voice never writes", h.composeRequests.isEmpty())
        assertTrue(h.replyRequests.isEmpty())
    }

    @Test
    fun `the words go in at the caret and stay editable`() {
        h.openCreate("Скажи, что согласен")
        h.compose.session!!.instruction.moveCursor("Скажи,".length)
        h.startRecording()
        h.finishWith("да")
        assertEquals("Скажи, да что согласен", request)
        assertEquals("Скажи, да".length, h.compose.session!!.instruction.cursor)

        // Typing carries on from there, and settles the review.
        h.compose.session!!.instruction.insert(",")
        h.dictation.userEdited()
        assertEquals("Скажи, да, что согласен", request)
        assertEquals(DictationPhase.IDLE, h.dictation.phase)
    }

    @Test
    fun `Undo takes the dictated words back out`() {
        h.openCreate("Поздравь")
        h.startRecording()
        h.finishWith("коллегу с днём рождения")
        assertEquals("Поздравь коллегу с днём рождения", request)
        assertTrue(h.dictation.canUndo)
        h.dictation.undo()
        assertEquals("Поздравь", request)
        assertEquals(DictationPhase.IDLE, h.dictation.phase)
        assertFalse(h.dictation.canUndo)
    }

    @Test
    fun `an edit settles the words - no Undo over the user's own typing`() {
        h.openCreate()
        h.startRecording()
        h.finishWith("откажи")
        h.compose.session!!.instruction.insert(" вежливо")
        assertFalse("the text is no longer what voice left", h.dictation.canUndo)
        h.dictation.undo()
        assertEquals("Откажи вежливо", request)
    }

    @Test
    fun `a second recording adds to the first`() {
        h.openCreate()
        h.startRecording()
        h.finishWith("да")
        h.startRecording()
        h.finishWith("конечно")
        assertEquals("Да конечно", request)
        assertEquals(2, h.recorders.size)
        assertEquals("a fresh id for fresh audio", 2, h.transcriber.requests.map { it.requestId }.distinct().size)
    }

    // ----------------------------------------------------------- the limit

    @Test
    fun `dictated words are never cut - the request is kept whole and waits to be shortened`() {
        val h = PanelsHarness(instructionLimit = 20)
        h.openCreate("Ответь коротко:")
        h.startRecording()
        h.finishWith("да конечно приду завтра")
        val text = h.compose.session!!.instruction.text
        assertEquals("Ответь коротко: да конечно приду завтра", text)
        assertEquals(DictationMessage.TooLong(20), h.dictation.message)

        h.panels.primaryTapped()
        h.settle()
        assertTrue("an over-limit request is not sent", h.composeRequests.isEmpty())

        h.compose.session!!.instruction.set("Ответь коротко: да")
        h.dictation.userEdited()
        assertNull(h.dictation.message)
        h.panels.primaryTapped()
        h.settle()
        assertEquals(listOf("Ответь коротко: да"), h.composeRequests.map { it.instruction })
    }

    @Test
    fun `exactly at the limit is fine, and is sent when Reply is tapped`() {
        val h = PanelsHarness(instructionLimit = 20)
        h.openReply(text = "Скажи что,")
        h.startRecording()
        h.finishWith("приду в 9")
        val instruction = h.replies.session!!.instruction.text
        assertEquals("Скажи что, приду в 9", instruction)
        assertEquals(20, instruction.codePointCount(0, instruction.length))
        assertNull(h.dictation.message)
        h.panels.primaryTapped()
        h.settle()
        assertEquals("Скажи что, приду в 9", h.replyRequests.single().instruction)
    }

    @Test
    fun `a reply's instruction past the limit is not sent either - never clipped`() {
        val h = PanelsHarness(instructionLimit = 20)
        h.openReply(text = "Скажи что")
        h.startRecording()
        h.finishWith("я обязательно приду завтра вечером")
        h.panels.primaryTapped()
        h.settle()
        assertTrue(h.replyRequests.isEmpty())
    }

    // ------------------------------------------------- Reply / Write wait

    @Test
    fun `Write waits while recording and recognising, and sends the words only when tapped`() {
        h.openCreate("Откажи")
        h.startRecording()
        assertFalse(h.panels.canStartRequest)
        h.panels.primaryTapped()
        h.settle()
        assertTrue("no request while recording", h.composeRequests.isEmpty())

        h.dictation.finish()
        h.settle()
        h.panels.primaryTapped()
        h.settle()
        assertTrue("none while recognising", h.composeRequests.isEmpty())

        h.transcriber.answers("вежливо")
        h.settle()
        assertTrue(h.panels.canStartRequest)
        assertTrue("and none by itself after", h.composeRequests.isEmpty())

        h.panels.primaryTapped()
        h.settle()
        assertEquals(listOf("Откажи вежливо"), h.composeRequests.map { it.instruction })
        assertEquals("the review is settled by Write", DictationPhase.IDLE, h.dictation.phase)
    }

    @Test
    fun `Reply waits the same way in the reply composer`() {
        h.openReply(text = "Скажи что")
        h.startRecording()
        h.panels.primaryTapped()
        h.settle()
        assertTrue(h.replyRequests.isEmpty())
        h.finishWith("свободен")
        assertTrue(h.replyRequests.isEmpty())
        h.panels.primaryTapped()
        h.settle()
        assertEquals("Скажи что свободен", h.replyRequests.single().instruction)
    }

    @Test
    fun `Cancel before Write sends nothing at all`() {
        h.openCreate("Откажи")
        h.startRecording()
        h.dictation.cancel()
        assertTrue(h.transcriber.requests.isEmpty())
        assertTrue(h.composeRequests.isEmpty())
        assertEquals("Откажи", request)
        assertTrue(h.panels.canStartRequest)
    }

    @Test
    fun `the mic does nothing once the request is being written`() {
        h.openCreate("Поздравь с днём рождения")
        h.panels.primaryTapped()
        assertTrue(h.compose.flow.isGenerating)
        h.panels.microphoneTapped()
        h.settle()
        assertTrue("no recorder was even made", h.recorders.isEmpty())
    }

    // -------------------------------------------------------------- languages

    @Test
    fun `the layout on screen is the first hint, the user's other layouts follow`() {
        h.openCreate()
        for ((layout, hints) in listOf(
            KeyboardLanguage.KAZAKH to listOf("kk", "ru", "en"),
            KeyboardLanguage.RUSSIAN to listOf("ru", "kk", "en"),
            KeyboardLanguage.ENGLISH to listOf("en", "kk", "ru")
        )) {
            h.layout = layout
            h.startRecording()
            h.finishWith("ok")
            assertEquals(hints, h.transcriber.requests.last().languages)
        }
    }

    @Test
    fun `Kazakh always brings Russian along - a single layout is the only hint otherwise`() {
        h.openReply()
        h.layouts = listOf(KeyboardLanguage.KAZAKH)
        h.layout = KeyboardLanguage.KAZAKH
        h.startRecording()
        h.finishWith("иә")
        assertEquals(listOf("kk", "ru"), h.transcriber.requests.last().languages)

        h.layouts = listOf(KeyboardLanguage.ENGLISH)
        h.layout = KeyboardLanguage.ENGLISH
        h.startRecording()
        h.finishWith("yes")
        assertEquals(listOf("en"), h.transcriber.requests.last().languages)
    }

    @Test
    fun `switching the layout before Done changes the hints, not the recording`() {
        h.openCreate()
        h.layout = KeyboardLanguage.RUSSIAN
        h.startRecording()
        h.layout = KeyboardLanguage.KAZAKH
        h.panels.layoutChanged()
        assertEquals(DictationPhase.RECORDING, h.dictation.phase)
        h.finishWith("рақмет")
        assertEquals("kk", h.transcriber.requests.single().languages.first())
    }

    // ------------------------------------------------------------- leaving

    @Test
    fun `hiding the keyboard pauses the recording - back, it can be finished`() {
        h.openCreate("Откажи")
        h.startRecording(5_000)
        h.panels.keyboardHidden(h.now)
        assertEquals(DictationPhase.PAUSED, h.dictation.phase)
        assertFalse(h.recorder.running)
        h.panels.keyboardShown(h.now + 1_000, secureField = false)
        assertEquals(DictationPhase.PAUSED, h.dictation.phase)
        h.finishWith("вежливо")
        assertEquals("Откажи вежливо", request)
    }

    @Test
    fun `words recognised while the keyboard is away still land in the parked request`() {
        h.openCreate("Откажи")
        h.startRecording()
        h.dictation.finish()
        h.settle()
        h.panels.keyboardHidden(h.now)
        h.transcriber.answers("вежливо")
        h.settle()
        h.panels.keyboardShown(h.now + 1_000, secureField = false)
        assertEquals("Откажи вежливо", request)
    }

    @Test
    fun `closing the panel drops the recording and nothing lands later`() {
        h.openCreate()
        h.startRecording()
        h.dictation.finish()
        h.settle()
        val session = h.compose.session!!
        h.panels.closeAll()
        h.settle()
        assertEquals(1, h.transcriber.cancelled)
        assertEquals(DictationPhase.IDLE, h.dictation.phase)
        assertNull(h.compose.session)
        assertEquals("", session.instruction.text)
        assertTrue(h.changed.isEmpty())
    }

    @Test
    fun `New drops the recording with the request`() {
        h.openCreate("Откажи")
        h.startRecording()
        h.panels.startOver()
        assertTrue(h.recorder.discards >= 1)
        assertEquals("", request)
        assertEquals(DictationPhase.IDLE, h.dictation.phase)
        assertTrue(h.transcriber.requests.isEmpty())
    }

    @Test
    fun `opening a persona drops the recording, and no reply gets its words`() {
        h.openCreate()
        h.startRecording()
        h.panels.openReply(h.friend) { kz.yerek.aireply.keyboard.input.ContextTextProvider.Result.Failure(kz.yerek.aireply.ai.AIReplyError.NoSourceMessage) }
        assertEquals(DictationPhase.IDLE, h.dictation.phase)
        assertNull(h.compose.session)
        assertTrue(h.transcriber.requests.isEmpty())
        assertEquals("", h.replies.session!!.instruction.text)
    }

    @Test
    fun `putting a reply aside drops its recording`() {
        h.openReply()
        h.startRecording()
        h.panels.putReplyAside()
        assertTrue(h.replies.isSuspended)
        assertEquals(DictationPhase.IDLE, h.dictation.phase)
    }

    @Test
    fun `a password field ends Create and its recording`() {
        h.openCreate("Откажи")
        h.startRecording()
        h.panels.keyboardShown(h.now, secureField = true)
        assertNull(h.compose.session)
        assertEquals(DictationPhase.IDLE, h.dictation.phase)
        assertTrue(h.transcriber.requests.isEmpty())
    }

    // ------------------------------------------------------------ permission

    @Test
    fun `allowing the microphone starts the recording once the keyboard is back`() {
        h.mic.granted = false
        h.openCreate()
        h.panels.microphoneTapped()
        h.settle()
        assertEquals(1, h.mic.requests)
        // The system dialog hides the keyboard.
        h.panels.keyboardHidden(h.now)

        h.mic.answers(MicPermission.Outcome.GRANTED)
        h.settle()
        assertTrue("nothing records while the keyboard is away", h.recorders.isEmpty())

        h.now += 500
        h.panels.keyboardShown(h.now, secureField = false)
        h.settle()
        assertEquals(1, h.recorders.size)
        assertEquals(DictationPhase.STARTING, h.dictation.phase)
    }

    @Test
    fun `an allowed microphone does not start by itself much later`() {
        h.mic.granted = false
        h.openCreate()
        h.panels.microphoneTapped()
        h.settle()
        h.panels.keyboardHidden(h.now)
        h.mic.answers(MicPermission.Outcome.GRANTED)
        h.settle()
        h.now += 60_000
        h.panels.keyboardShown(h.now, secureField = false)
        h.settle()
        assertTrue(h.recorders.isEmpty())
        assertEquals(DictationPhase.IDLE, h.dictation.phase)
    }

    @Test
    fun `a refusal says so, and a final refusal points to Settings`() {
        h.mic.granted = false
        h.openCreate()
        h.panels.microphoneTapped()
        h.settle()
        h.mic.answers(MicPermission.Outcome.DENIED)
        h.settle()
        assertEquals(DictationMessage.PermissionRequired, h.dictation.message)

        h.now += 5_000
        h.panels.microphoneTapped()
        h.settle()
        assertEquals("asked again", 2, h.mic.requests)
        h.mic.answers(MicPermission.Outcome.PERMANENTLY_DENIED)
        h.settle()
        assertEquals(DictationMessage.PermissionDenied, h.dictation.message)

        h.panels.microphoneTapped()
        assertEquals(1, h.mic.settingsOpened)
        assertEquals(2, h.mic.requests)
        assertTrue(h.recorders.isEmpty())
        assertEquals("", request)
    }

    @Test
    fun `a second tap while the dialog is opening asks only once`() {
        h.mic.granted = false
        h.openCreate()
        h.panels.microphoneTapped()
        h.settle()
        h.panels.microphoneTapped()
        h.settle()
        assertEquals(1, h.mic.requests)
    }

    // --------------------------------------------------------- accessibility

    @Test
    fun `a screen reader hears what happens, never the words`() {
        h.openCreate()
        h.startRecording()
        h.finishWith("вежливо откажи")
        assertEquals(
            listOf(DictationEvent.Recording, DictationEvent.Recognizing, DictationEvent.Inserted),
            h.events
        )
    }
}
