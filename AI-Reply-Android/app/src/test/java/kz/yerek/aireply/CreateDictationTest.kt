package kz.yerek.aireply

import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.keyboard.reply.ReplyComposerFlow
import kz.yerek.aireply.keyboard.voice.DictationNotice
import kz.yerek.aireply.keyboard.voice.VoiceAnnouncement
import kz.yerek.aireply.keyboard.voice.VoiceStatusText
import kz.yerek.aireply.voice.MicPermission
import kz.yerek.aireply.voice.VoiceFailure
import kz.yerek.aireply.voice.VoiceState
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The microphone in the ✨ Create panel (and the same one in the reply
 * composer), driven through a fake recogniser.
 *
 * What these pin, from the customer's request: the user can dictate the
 * request - «вежливо откажи», «скажи, что согласен», «да» - in the language
 * of the layout on screen; the words land in the request at the caret and
 * stay editable; NOTHING is ever sent because of dictation; and every way of
 * leaving ends the recording cleanly.
 */
class CreateDictationTest {

    private val h = PanelsHarness()

    private val request: String get() = h.compose.session!!.instruction.text

    // ---------------------------------------------------------------- basics

    @Test
    fun `dictating fills the request, live words first, and sends nothing`() {
        h.openCreate()
        h.panels.microphoneTapped()
        h.settle()
        assertEquals(VoiceState.Starting, h.dictation.state)

        h.client.ready()
        h.client.hears("вежливо")
        h.settle()
        assertEquals(VoiceState.Listening("вежливо"), h.dictation.state)
        assertEquals("the field waits for the final words", "", request)

        h.client.hears("вежливо откажи")
        h.settle()
        assertEquals(VoiceState.Listening("вежливо откажи"), h.dictation.state)

        h.stopWith("вежливо откажи")
        assertEquals("Вежливо откажи", request)
        assertEquals(VoiceState.Idle, h.dictation.state)
        assertEquals(1, h.client.stops)
        assertEquals("still Create, still the request", ReplyComposerFlow.Stage.Composing, h.compose.flow.stage)
        assertTrue("dictation never sends", h.composeRequests.isEmpty())
        assertTrue(h.replyRequests.isEmpty())
    }

    @Test
    fun `the words go in at the caret and stay editable`() {
        h.openCreate("Скажи, что согласен")
        h.compose.session!!.instruction.moveCursor("Скажи,".length)
        h.startListening()
        h.stopWith("да")
        assertEquals("Скажи, да что согласен", request)
        assertEquals("Скажи, да".length, h.compose.session!!.instruction.cursor)

        // Typing carries on from there.
        h.compose.session!!.instruction.insert(",")
        assertEquals("Скажи, да, что согласен", request)
    }

    @Test
    fun `a recogniser that ends by itself delivers the same way`() {
        h.openCreate()
        h.startListening("да")
        h.client.finalResult("да")
        h.settle()
        assertEquals("Да", request)
        assertEquals(VoiceState.Idle, h.dictation.state)
    }

    @Test
    fun `while the words are being recognised another tap does nothing`() {
        h.openCreate()
        h.startListening("да")
        h.panels.microphoneTapped()
        h.settle()
        assertEquals(VoiceState.Processing, h.dictation.state)
        h.panels.microphoneTapped()
        h.settle()
        assertEquals(1, h.client.stops)
        assertEquals(1, h.client.languages.size)
        h.client.finalResult("да")
        h.settle()
        assertEquals("Да", request)
    }

    @Test
    fun `one recogniser serves every recording until the keyboard goes away`() {
        h.openCreate()
        h.startListening()
        h.stopWith("да")
        h.startListening()
        h.stopWith("конечно")
        assertEquals("Да конечно", request)
        assertEquals(1, h.clients.size)
    }

    // ---------------------------------------------------------------- errors

    @Test
    fun `nothing heard says so and leaves the request as it was`() {
        h.openCreate("Откажи")
        h.startListening()
        h.panels.microphoneTapped()
        h.settle()
        h.client.fails(VoiceFailure.NO_SPEECH)
        h.settle()
        assertEquals("Откажи", request)
        assertEquals(VoiceState.Failed(VoiceFailure.NO_SPEECH, "ru-RU"), h.dictation.state)
        assertEquals(R.string.voice_kb_no_speech, VoiceStatusText.message(h.dictation.state, null)?.id)
        assertFalse("Write is available again", h.dictation.isActive)

        // Typing moves on: the message goes.
        h.dictation.userEdited()
        assertEquals(VoiceState.Idle, h.dictation.state)
    }

    @Test
    fun `no network, a busy microphone and an unknown language each end the recording with their message`() {
        h.openCreate()
        val expected = mapOf(
            VoiceFailure.NETWORK to R.string.kb_err_offline,
            VoiceFailure.BUSY to R.string.voice_kb_busy,
            VoiceFailure.GENERIC to R.string.voice_kb_failed
        )
        for ((reason, message) in expected) {
            h.startListening()
            h.client.fails(reason)
            h.settle()
            assertFalse(h.dictation.isActive)
            assertEquals(message, VoiceStatusText.message(h.dictation.state, null)?.id)
            assertEquals("", request)
        }
    }

    @Test
    fun `Kazakh unavailable on the phone names Kazakh, and switching the layout clears it`() {
        h.layout = KeyboardLanguage.KAZAKH
        h.openCreate()
        h.startListening()
        h.client.fails(VoiceFailure.LANGUAGE_UNAVAILABLE)
        h.settle()
        assertEquals(VoiceState.Failed(VoiceFailure.LANGUAGE_UNAVAILABLE, "kk-KZ"), h.dictation.state)
        assertEquals(R.string.voice_kb_language_unavailable_kk, VoiceStatusText.message(h.dictation.state, null)?.id)

        h.layout = KeyboardLanguage.RUSSIAN
        h.panels.layoutChanged()
        assertEquals(VoiceState.Idle, h.dictation.state)

        h.startListening()
        h.stopWith("откажи")
        assertEquals(listOf("kk-KZ", "ru-RU"), h.client.languages)
        assertEquals("Откажи", request)
    }

    // --------------------------------------------------------------- limit

    @Test
    fun `dictation never passes the request limit and says so`() {
        val h = PanelsHarness(instructionLimit = 20)
        h.openCreate("Ответь коротко:")
        h.startListening()
        h.stopWith("да конечно приду завтра")
        val text = h.compose.session!!.instruction.text
        assertEquals("Ответь коротко: да", text)
        assertEquals(DictationNotice(20), h.dictation.notice)
        assertEquals(R.string.kb_compose_err_too_long, VoiceStatusText.message(h.dictation.state, h.dictation.notice)?.id)
        assertEquals(20, VoiceStatusText.message(h.dictation.state, h.dictation.notice)?.argument)

        h.dictation.userEdited()
        assertNull(h.dictation.notice)
    }

    // -------------------------------------------------------------- language

    @Test
    fun `the recogniser listens in the language of the layout, in Create`() {
        h.openCreate()
        for ((layout, tag) in listOf(
            KeyboardLanguage.KAZAKH to "kk-KZ",
            KeyboardLanguage.RUSSIAN to "ru-RU",
            KeyboardLanguage.ENGLISH to "en-US"
        )) {
            h.layout = layout
            h.startListening()
            h.stopWith("ok")
            assertEquals(tag, h.client.languages.last())
        }
    }

    @Test
    fun `and the same in the reply composer`() {
        h.openReply()
        for ((layout, tag) in listOf(
            KeyboardLanguage.KAZAKH to "kk-KZ",
            KeyboardLanguage.RUSSIAN to "ru-RU",
            KeyboardLanguage.ENGLISH to "en-US"
        )) {
            h.layout = layout
            h.startListening()
            h.stopWith("ok")
            assertEquals(tag, h.client.languages.last())
        }
        assertEquals("Ok ok ok", h.replies.session!!.instruction.text)
        assertTrue(h.replyRequests.isEmpty())
    }

    // ------------------------------------------------------------- stopping

    @Test
    fun `switching the layout stops the recording and keeps its words`() {
        h.openCreate()
        h.startListening("скажи что согласен")
        h.layout = KeyboardLanguage.ENGLISH
        h.panels.layoutChanged()
        h.settle()
        assertEquals(1, h.client.stops)
        assertEquals(VoiceState.Processing, h.dictation.state)
        h.client.finalResult("скажи что согласен")
        h.settle()
        assertEquals("Скажи что согласен", request)
    }

    @Test
    fun `hiding the keyboard releases the microphone and drops what was not delivered`() {
        h.openCreate("Откажи")
        h.startListening("вежливо")
        val first = h.client
        h.panels.keyboardHidden(h.now)
        assertEquals(1, first.releases)
        assertEquals(VoiceState.Idle, h.dictation.state)

        // The keyboard comes back: the request is there, untouched; a new
        // recording gets a fresh recogniser.
        h.panels.keyboardShown(h.now + 1_000, secureField = false)
        assertEquals("Откажи", request)
        h.startListening()
        assertEquals(2, h.clients.size)
        h.stopWith("вежливо")
        assertEquals("Откажи вежливо", request)
    }

    @Test
    fun `closing the panel drops the recording and nothing lands later`() {
        h.openCreate()
        h.startListening("вежливо откажи")
        val session = h.compose.session!!
        h.panels.closeAll()
        assertEquals(1, h.client.cancels)
        assertEquals(VoiceState.Idle, h.dictation.state)
        assertNull(h.compose.session)

        // A late result from the platform would find nowhere to go.
        h.client.finalResult("вежливо откажи")
        h.settle()
        assertEquals("", session.instruction.text)
        assertTrue(h.inserted.isEmpty())
    }

    @Test
    fun `words for a request that is no longer being written are dropped`() {
        h.openCreate("Откажи")
        h.startListening("вежливо")
        h.panels.layoutChanged()
        h.settle()
        // However the panel left the request stage, the late words must not
        // slip into a field nobody is looking at.
        val session = h.compose.session!!
        session.flow = session.flow.startingGeneration()!!
        h.client.finalResult("вежливо")
        h.settle()
        assertEquals("Откажи", session.instruction.text)
        assertTrue(h.inserted.isEmpty())
    }

    @Test
    fun `New drops the recording with the request`() {
        h.openCreate("Откажи")
        h.startListening("вежливо")
        h.panels.startOver()
        assertEquals(1, h.client.cancels)
        assertEquals("", request)
        assertEquals(VoiceState.Idle, h.dictation.state)
    }

    @Test
    fun `opening a persona drops the recording, and no reply gets its words`() {
        h.openCreate()
        h.startListening("вежливо")
        h.panels.openReply(h.friend) { kz.yerek.aireply.keyboard.input.ContextTextProvider.Result.Failure(kz.yerek.aireply.ai.AIReplyError.NoSourceMessage) }
        assertEquals(1, h.client.cancels)
        assertNull(h.compose.session)
        h.client.finalResult("вежливо")
        h.settle()
        assertEquals("", h.replies.session!!.instruction.text)
        assertTrue(h.replyRequests.isEmpty())
    }

    @Test
    fun `putting a reply aside drops its recording`() {
        h.openReply()
        h.startListening("да")
        h.panels.putReplyAside()
        assertEquals(1, h.client.cancels)
        assertTrue(h.replies.isSuspended)
        assertEquals(VoiceState.Idle, h.dictation.state)
    }

    @Test
    fun `turning the phone stops the recording like Stop`() {
        h.openCreate()
        h.startListening("да")
        h.dictation.stop()
        h.settle()
        h.client.finalResult("да")
        h.settle()
        assertEquals("Да", request)
    }

    // ------------------------------------------------- Write while recording

    @Test
    fun `Write waits while the microphone is on, and sends the dictated words after`() {
        h.openCreate("Откажи")
        h.startListening("вежливо")
        assertFalse(h.panels.canStartRequest)

        h.panels.primaryTapped()
        h.settle()
        assertTrue("no request while recording", h.composeRequests.isEmpty())
        assertEquals(ReplyComposerFlow.Stage.Composing, h.compose.flow.stage)

        h.stopWith("вежливо")
        assertTrue(h.panels.canStartRequest)
        assertTrue("and none by itself after", h.composeRequests.isEmpty())

        h.panels.primaryTapped()
        h.settle()
        assertEquals(listOf("Откажи вежливо"), h.composeRequests.map { it.instruction })
    }

    @Test
    fun `Reply waits the same way in the reply composer`() {
        h.openReply(text = "Скажи что")
        h.startListening("свободен")
        h.panels.primaryTapped()
        h.settle()
        assertTrue(h.replyRequests.isEmpty())
        h.stopWith("свободен")
        assertTrue(h.replyRequests.isEmpty())
        h.panels.primaryTapped()
        h.settle()
        assertEquals("Скажи что свободен", h.replyRequests.single().instruction)
    }

    @Test
    fun `the mic does nothing once the request is being written`() {
        h.openCreate("Поздравь с днём рождения")
        h.panels.primaryTapped()
        assertTrue(h.compose.flow.isGenerating)
        h.panels.microphoneTapped()
        h.settle()
        assertTrue("no recogniser was even made", h.clients.isEmpty())
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
        assertTrue("nothing records while the keyboard is away", h.clients.isEmpty())

        h.now += 500
        h.panels.keyboardShown(h.now, secureField = false)
        h.settle()
        assertEquals(listOf("ru-RU"), h.client.languages)
        h.client.ready()
        h.stopWith("да")
        assertEquals("Да", request)
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
        assertTrue(h.clients.isEmpty())
        assertEquals(VoiceState.Idle, h.dictation.state)
    }

    @Test
    fun `a refusal shows the permission message, and a final refusal points to Settings`() {
        h.mic.granted = false
        h.openCreate()
        h.panels.microphoneTapped()
        h.settle()
        h.mic.answers(MicPermission.Outcome.DENIED)
        h.settle()
        assertEquals(VoiceState.PermissionRequired, h.dictation.state)
        assertEquals(R.string.voice_kb_permission_needed, VoiceStatusText.message(h.dictation.state, null)?.id)

        h.now += 5_000
        h.panels.microphoneTapped()
        h.settle()
        assertEquals("asked again", 2, h.mic.requests)
        h.mic.answers(MicPermission.Outcome.PERMANENTLY_DENIED)
        h.settle()
        assertEquals(VoiceState.PermissionDenied, h.dictation.state)
        assertEquals(R.string.voice_kb_permission_denied, VoiceStatusText.message(h.dictation.state, null)?.id)

        h.panels.microphoneTapped()
        assertEquals(1, h.mic.settingsOpened)
        assertEquals(2, h.mic.requests)
        assertTrue(h.clients.isEmpty())
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
    fun `a screen reader hears listening and stopped, never the words`() {
        h.openCreate()
        h.startListening("вежливо откажи")
        h.stopWith("вежливо откажи")
        assertEquals(listOf(VoiceAnnouncement.Listening, VoiceAnnouncement.Stopped), h.announcements)

        h.announcements.clear()
        h.startListening()
        h.client.fails(VoiceFailure.NO_SPEECH)
        h.settle()
        assertEquals(
            listOf(
                VoiceAnnouncement.Listening,
                VoiceAnnouncement.Message(VoiceState.Failed(VoiceFailure.NO_SPEECH, "ru-RU"), null)
            ),
            h.announcements
        )
    }

    // -------------------------------------------------------------- secure

    @Test
    fun `a password field ends Create and its recording`() {
        h.openCreate("Откажи")
        h.startListening("вежливо")
        h.panels.keyboardShown(h.now, secureField = true)
        assertNull(h.compose.session)
        assertEquals(VoiceState.Idle, h.dictation.state)
        assertEquals(1, h.client.cancels)
    }
}
