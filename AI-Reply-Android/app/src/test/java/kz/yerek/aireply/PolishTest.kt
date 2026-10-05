package kz.yerek.aireply

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kz.yerek.aireply.ai.AIConfiguration
import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.ai.AIReplyException
import kz.yerek.aireply.ai.AccountPolishTransport
import kz.yerek.aireply.ai.PolishService
import kz.yerek.aireply.ai.PolishTransport
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.keyboard.input.KeyboardTextFieldState
import kz.yerek.aireply.keyboard.reply.InstructionPolish
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The instruction polish (DESIGN §5.5, §6.5): asked for only after a pause in
 * a real sentence, cancelled by the next edit, never shown when stale, quiet
 * about failures and silent after two in a row; taking it is one tap and
 * Undo stays for five seconds.
 *
 * Нұсқауды түзету ұсынысы: үзілістен кейін, бір түртумен, кері қайтарумен.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class PolishTest {

    /** Holds every request until the test answers it. */
    private class ScriptedTransport : PolishTransport {
        val requests = mutableListOf<PolishService.Request>()
        private val pending = ArrayDeque<CompletableDeferred<String?>>()

        override suspend fun polish(request: PolishService.Request): String? {
            requests += request
            val answer = CompletableDeferred<String?>()
            pending.addLast(answer)
            return answer.await()
        }

        fun answer(text: String?) = pending.removeFirst().complete(text)
        fun fail(error: AIReplyError) = pending.removeFirst().completeExceptionally(AIReplyException(error))
    }

    private val dispatcher = StandardTestDispatcher()
    private val scope = TestScope(dispatcher)
    private val transport = ScriptedTransport()
    private val service = PolishService(
        configuration = AIConfiguration { true },
        accountTransport = { error("the account transport must not be used in tests") },
        transportOverride = { transport }
    )
    private var allowed = true
    private val polish = InstructionPolish(scope, service).apply {
        isAllowed = { allowed }
        inputLanguage = { KeyboardLanguage.KAZAKH }
    }
    private val field = KeyboardTextFieldState()

    private val note = "ответь ему вежливо я сегодня не могу завтра могу"
    private val polished = "Ответь ему вежливо: я сегодня не могу, завтра могу."

    private fun write(text: String) {
        field.set(text)
        polish.edited(field)
    }

    private fun pause(millis: Long = InstructionPolish.PAUSE_MS) {
        scope.advanceTimeBy(millis)
        scope.runCurrent()
    }

    // ---------------------------------------------------------------- timing

    @Test
    fun `asks only after a pause in typing`() {
        write(note)
        pause(InstructionPolish.PAUSE_MS - 1)
        assertTrue(transport.requests.isEmpty())
        pause(1)
        assertEquals(1, transport.requests.size)
        assertEquals(note, transport.requests.single().text)
        assertEquals(KeyboardLanguage.KAZAKH, transport.requests.single().inputLanguage)
    }

    @Test
    fun `each edit restarts the pause`() {
        write("ответь ему вежливо")
        pause(1_000)
        write(note)
        pause(1_000)
        assertTrue(transport.requests.isEmpty())
        pause(400)
        assertEquals("only the latest text", listOf(note), transport.requests.map { it.text })
    }

    @Test
    fun `the suggestion shows when the answer fits the text`() {
        write(note)
        pause()
        transport.answer(polished)
        scope.runCurrent()
        assertEquals(polished, polish.offer?.text)
        assertTrue(polish.offer?.field === field)
    }

    @Test
    fun `an edit while waiting drops the late answer`() {
        write(note)
        pause()
        write("$note и")
        transport.answer(polished)
        scope.runCurrent()
        assertNull(polish.offer)
    }

    @Test
    fun `an edit hides the suggestion`() {
        write(note)
        pause()
        transport.answer(polished)
        scope.runCurrent()
        write("$note!")
        assertNull(polish.offer)
    }

    // ---------------------------------------------------------- what is sent

    @Test
    fun `short notes, the same text again, or a refusal ask nothing`() {
        write("ок")
        pause()
        write("ответь да")
        pause()
        assertTrue("under three words or twelve characters", transport.requests.isEmpty())

        allowed = false
        write(note)
        pause()
        assertTrue("setting off, no server flag or a reply being written", transport.requests.isEmpty())

        allowed = true
        polish.edited(field)
        pause()
        transport.answer(null)
        scope.runCurrent()
        assertNull("nothing to suggest", polish.offer)
        polish.edited(field)
        pause()
        assertEquals("the same text is not asked about twice", 1, transport.requests.size)
    }

    @Test
    fun `two failures in a row stop it until the keyboard is shown again`() {
        write(note)
        pause()
        transport.fail(AIReplyError.Offline)
        scope.runCurrent()
        write("$note пожалуйста")
        pause()
        transport.fail(AIReplyError.RateLimited)
        scope.runCurrent()
        assertNull("failures are never shown", polish.offer)

        write("$note спасибо")
        pause()
        assertEquals(2, transport.requests.size)

        polish.reset()
        write("$note спасибо")
        pause()
        assertEquals(3, transport.requests.size)
    }

    @Test
    fun `dismissing cancels a pending request`() {
        write(note)
        polish.dismiss()
        pause()
        assertTrue(transport.requests.isEmpty())
    }

    // ----------------------------------------------------------- accept, undo

    @Test
    fun `taking the suggestion replaces the text and offers Undo for five seconds`() {
        write(note)
        pause()
        transport.answer(polished)
        scope.runCurrent()

        assertTrue(polish.accept() === field)
        assertEquals(polished, field.text)
        assertEquals("caret at the end", polished.length, field.cursor)
        assertNull(polish.offer)
        assertEquals(note, polish.taken?.previous)

        pause(InstructionPolish.UNDO_MS)
        assertNull("Undo is gone", polish.taken)
        polish.edited(field)
        pause()
        assertEquals("the polished text is not polished again", 1, transport.requests.size)
    }

    @Test
    fun `Undo puts the instruction back as written`() {
        write(note)
        pause()
        transport.answer(polished)
        scope.runCurrent()
        polish.accept()

        assertTrue(polish.undo() === field)
        assertEquals(note, field.text)
        assertNull(polish.taken)
        polish.edited(field)
        pause()
        assertEquals("a refused suggestion is not offered again", 1, transport.requests.size)
    }

    @Test
    fun `a stale suggestion cannot be taken`() {
        write(note)
        pause()
        transport.answer(polished)
        scope.runCurrent()
        field.set("$note ещё")
        assertNull(polish.accept())
        assertEquals("$note ещё", field.text)
    }

    // ------------------------------------------------------------- the wire

    @Test
    fun `the request carries the instruction and the layout, nothing else`() {
        val body = Json.parseToJsonElement(
            AccountPolishTransport.encode(PolishService.Request(note, KeyboardLanguage.KAZAKH), "1.2.3")
        ).jsonObject
        assertEquals(setOf("text", "input_language", "platform", "app_version"), body.keys)
        assertEquals(note, body.getValue("text").jsonPrimitive.content)
        assertEquals("kk", body.getValue("input_language").jsonPrimitive.content)
        assertEquals("android", body.getValue("platform").jsonPrimitive.content)

        val noLayout = Json.parseToJsonElement(AccountPolishTransport.encode(PolishService.Request(note), "1.2.3")).jsonObject
        assertFalse("input_language" in noLayout.keys)
    }

    @Test
    fun `server errors become quiet failures`() {
        assertEquals(AIReplyError.Offline, AccountPolishTransport.map(ApiError.Offline))
        assertEquals(AIReplyError.RateLimited, AccountPolishTransport.map(ApiError.RateLimited(null)))
        assertEquals(AIReplyError.ServiceUnavailable, AccountPolishTransport.map(ApiError.NotFound))
    }

    @Test
    fun `only real sentences within the limit qualify`() {
        assertTrue(PolishService.qualifies(note, limit = 400))
        assertFalse("too short", PolishService.qualifies("да да да", limit = 400))
        assertFalse("two words", PolishService.qualifies("ответь пожалуйста", limit = 400))
        assertFalse("over the limit", PolishService.qualifies(note, limit = 20))
        assertTrue("Kazakh", PolishService.qualifies("ертең кездесейік деп айт", limit = 400))
    }
}
