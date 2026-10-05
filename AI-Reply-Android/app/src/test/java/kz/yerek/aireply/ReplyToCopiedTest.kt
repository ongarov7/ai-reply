package kz.yerek.aireply

import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.domain.model.ReplyConfiguration
import kz.yerek.aireply.domain.model.ReplyTemplate
import kz.yerek.aireply.keyboard.input.ContextTextProvider
import kz.yerek.aireply.keyboard.input.ReplyContext
import kz.yerek.aireply.keyboard.input.ReplyContextSource
import kz.yerek.aireply.keyboard.reply.KeyboardPanels
import kz.yerek.aireply.keyboard.reply.KeyboardPanels.CopiedReply
import kz.yerek.aireply.keyboard.reply.ReplyComposerFlow
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * "Reply to copied" in the ✨ panel: the request the user typed or dictated
 * becomes the instruction of a reply to the message they copied. The
 * clipboard is read on that tap and no other time, never in a password field,
 * and nothing is generated until the user taps Reply.
 */
class ReplyToCopiedTest {

    private val h = PanelsHarness()
    private val copied = "Сәлеметсіз бе! Ертең кездесуге уақытыңыз бар ма?"
    private var reads = 0

    private fun clipboard(text: String?): () -> ContextTextProvider.Result = {
        reads++
        if (text == null) {
            ContextTextProvider.Result.Failure(AIReplyError.NoSourceMessage)
        } else {
            ContextTextProvider.Result.Success(ReplyContext(text, ReplyContextSource.CLIPBOARD))
        }
    }

    @Test
    fun `the typed request becomes the reply's instruction, and nothing is generated`() {
        h.openCreate("Вежливо откажи")
        val outcome = h.panels.replyToCopied(h.friend, secureField = false, acquire = clipboard(copied))
        h.settle()

        assertEquals(CopiedReply.Opened, outcome)
        assertEquals(1, reads)
        assertNull("Create is gone", h.compose.session)
        assertNotNull(h.replies.session)
        val reply = h.replies.session!!
        assertEquals(copied, reply.source.text)
        assertEquals("Вежливо откажи", reply.instruction.text)
        assertEquals("friend", reply.template.id)
        assertEquals(ReplyComposerFlow.Stage.Composing, reply.flow.stage)
        assertTrue("waiting for Reply", h.replyRequests.isEmpty())
        assertTrue(h.composeRequests.isEmpty())

        // Reply sends the copied message with the carried request.
        h.panels.primaryTapped()
        h.settle()
        assertEquals(copied, h.replyRequests.single().message)
        assertEquals("Вежливо откажи", h.replyRequests.single().instruction)
    }

    @Test
    fun `a dictated request is carried over the same way`() {
        h.openCreate()
        h.startListening("скажи что согласен")
        h.stopWith("скажи что согласен")
        h.panels.replyToCopied(h.friend, secureField = false, acquire = clipboard(copied))
        assertEquals("Скажи что согласен", h.replies.session!!.instruction.text)
        assertTrue(h.replyRequests.isEmpty())
    }

    @Test
    fun `an empty request opens the reply with an empty instruction`() {
        h.openCreate()
        assertEquals(CopiedReply.Opened, h.panels.replyToCopied(h.friend, secureField = false, acquire = clipboard(copied)))
        assertEquals("", h.replies.session!!.instruction.text)
    }

    @Test
    fun `nothing copied keeps Create as it was and says so`() {
        h.openCreate("Вежливо откажи")
        val outcome = h.panels.replyToCopied(h.friend, secureField = false, acquire = clipboard(null))

        assertEquals(CopiedReply.NothingCopied(AIReplyError.NoSourceMessage), outcome)
        assertNull("no reply opened", h.replies.session)
        val created = h.compose.session!!
        assertEquals("Вежливо откажи", created.instruction.text)
        assertEquals(ReplyComposerFlow.Stage.Composing, created.flow.stage)
        assertEquals("«Сначала скопируйте сообщение»", AIReplyError.NoSourceMessage, created.flow.error)

        // Editing the request clears the message, as with any error.
        h.compose.instructionEdited()
        assertNull(h.compose.flow.error)
    }

    @Test
    fun `in a password field the clipboard is not even read`() {
        h.openCreate("Вежливо откажи")
        val outcome = h.panels.replyToCopied(h.friend, secureField = true, acquire = clipboard(copied))
        assertEquals(CopiedReply.Refused, outcome)
        assertEquals(0, reads)
        assertNull(h.replies.session)
        assertEquals("Вежливо откажи", h.compose.session!!.instruction.text)
    }

    @Test
    fun `while the microphone is on it waits for the words`() {
        h.openCreate()
        h.startListening("вежливо")
        assertEquals(CopiedReply.Refused, h.panels.replyToCopied(h.friend, secureField = false, acquire = clipboard(copied)))
        assertEquals(0, reads)
        assertNotNull(h.compose.session)
    }

    @Test
    fun `not while a message is being written`() {
        h.openCreate("Поздравь коллегу")
        h.panels.primaryTapped()
        assertEquals(CopiedReply.Refused, h.panels.replyToCopied(h.friend, secureField = false, acquire = clipboard(copied)))
        assertEquals(0, reads)
    }

    // --------------------------------------------------------------- persona

    private val configuration = ReplyConfiguration.INITIAL

    private fun hidden(id: String): ReplyConfiguration = configuration.copy(
        templates = configuration.templates.map { if (it.id == id) it.copy(isVisible = false) else it }
    )

    private fun ReplyTemplate?.id(): String? = this?.id

    @Test
    fun `the persona is the last one used`() {
        assertEquals("work", KeyboardPanels.personaForCopied(configuration, "work").id())
        assertEquals("client", KeyboardPanels.personaForCopied(configuration, "client").id())
    }

    @Test
    fun `otherwise Friend`() {
        assertEquals("friend", KeyboardPanels.personaForCopied(configuration, null).id())
        assertEquals("friend", KeyboardPanels.personaForCopied(configuration, "deleted-template").id())
        // A persona the user has since hidden from the row is not used.
        assertEquals("friend", KeyboardPanels.personaForCopied(hidden("work"), "work").id())
    }

    @Test
    fun `and the first on the row when Friend is hidden too`() {
        val withoutFriend = hidden("friend")
        assertEquals(withoutFriend.visibleTemplates.first().id, KeyboardPanels.personaForCopied(withoutFriend, null).id())
    }
}
