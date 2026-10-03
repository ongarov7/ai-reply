package kz.yerek.aireply

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.runCurrent
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.boolean
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kz.yerek.aireply.ai.AIConfiguration
import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.ai.AccountComposeTransport
import kz.yerek.aireply.ai.ComposeService
import kz.yerek.aireply.ai.ComposeTransport
import kz.yerek.aireply.ai.GeneratedReply
import kz.yerek.aireply.ai.AIReplyException
import kz.yerek.aireply.ai.ReplyDraftNormalizer
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.ComposeResponseDto
import kz.yerek.aireply.keyboard.reply.ComposeSession
import kz.yerek.aireply.keyboard.reply.ComposeSessionController
import kz.yerek.aireply.keyboard.reply.ReplyComposerFlow
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The keyboard's "Create" mode: writing a new message from a description.
 *
 * What these pin: the instruction is the ONLY text that leaves the device; one
 * request at a time; Regenerate asks for another version and keeps the first;
 * New clears everything; a stopped request's answer never lands; every server
 * failure becomes one closed error with a sentence.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class ComposeFlowTest {

    /** Holds every request until the test answers it. */
    private class ScriptedTransport : ComposeTransport {
        val requests = mutableListOf<ComposeService.Request>()
        private val pending = ArrayDeque<CompletableDeferred<GeneratedReply>>()
        val waiting: Int get() = pending.size

        override suspend fun compose(request: ComposeService.Request): GeneratedReply {
            requests += request
            val answer = CompletableDeferred<GeneratedReply>()
            pending.addLast(answer)
            return answer.await()
        }

        fun answer(text: String) = pending.removeFirst().complete(GeneratedReply(text = text))
        fun fail(error: AIReplyError) = pending.removeFirst().completeExceptionally(AIReplyException(error))
    }

    private val instruction = "Поздравь директора Сакена Бакпакбековича с 55-летием. Тепло, с эмодзи."
    private val dispatcher = StandardTestDispatcher()
    private val scope = TestScope(dispatcher)
    private val transport = ScriptedTransport()
    private val service = ComposeService(
        configuration = AIConfiguration { true },
        accountTransport = { error("the account transport must not be used in tests") },
        transportOverride = { transport }
    )
    private val controller = ComposeSessionController(scope, service, ReplyDraftNormalizer()).apply {
        uiLanguage = AppLanguage.RUSSIAN
    }

    private fun openWith(text: String): ComposeSession {
        controller.open()
        val session = controller.session!!
        session.instruction.set(text)
        controller.instructionEdited()
        return session
    }

    // ------------------------------------------------------------ generation

    @Test
    fun `write sends only the trimmed instruction and shows the message`() {
        val session = openWith("  $instruction\n")
        controller.generate()
        assertTrue(controller.flow.isGenerating)
        scope.runCurrent()

        assertEquals(1, transport.requests.size)
        val request = transport.requests.single()
        assertEquals(instruction, request.instruction)
        assertEquals(AppLanguage.RUSSIAN, request.uiLanguage)
        assertFalse(request.isRegeneration)

        transport.answer("Уважаемый Сакен Бакпакбекович!\n\nПоздравляем! 🎉")
        scope.runCurrent()
        assertEquals(ReplyComposerFlow.Stage.Result, controller.flow.stage)
        assertEquals("Уважаемый Сакен Бакпакбекович!\n\nПоздравляем! 🎉", session.draft.text)
    }

    @Test
    fun `an empty instruction never reaches the network`() {
        openWith("   ")
        controller.generate()
        scope.runCurrent()
        assertEquals(AIReplyError.NoInstruction, controller.flow.error)
        assertEquals(ReplyComposerFlow.Stage.Composing, controller.flow.stage)
        assertTrue(transport.requests.isEmpty())
    }

    @Test
    fun `repeated taps send one request`() {
        openWith(instruction)
        repeat(5) { controller.generate() }
        scope.runCurrent()
        assertEquals(1, transport.requests.size)
    }

    @Test
    fun `regenerate asks for another version and keeps the first`() {
        val session = openWith(instruction)
        controller.generate(); scope.runCurrent()
        transport.answer("Первый вариант"); scope.runCurrent()

        controller.generate(); scope.runCurrent()
        assertTrue(transport.requests.last().isRegeneration)
        assertEquals(instruction, transport.requests.last().instruction)
        transport.answer("Второй вариант"); scope.runCurrent()

        assertEquals(2, controller.flow.drafts.count)
        assertEquals("Второй вариант", session.draft.text)
        controller.showPreviousVersion()
        assertEquals("Первый вариант", session.draft.text)
    }

    @Test
    fun `a failure keeps the instruction and the next try clears the error`() {
        val session = openWith(instruction)
        controller.generate(); scope.runCurrent()
        transport.fail(AIReplyError.Offline); scope.runCurrent()

        assertEquals(AIReplyError.Offline, controller.flow.error)
        assertEquals(ReplyComposerFlow.Stage.Composing, controller.flow.stage)
        assertEquals(instruction, session.instruction.text)

        controller.generate(); scope.runCurrent()
        assertNull(controller.flow.error)
        transport.fail(AIReplyError.QuotaExhausted); scope.runCurrent()
        assertEquals(AIReplyError.QuotaExhausted, controller.flow.error)
    }

    @Test
    fun `stop ignores the late answer`() {
        openWith(instruction)
        controller.generate(); scope.runCurrent()
        controller.stop()
        transport.answer("Поздно"); scope.runCurrent()
        assertTrue(controller.flow.drafts.isEmpty)
        assertEquals(ReplyComposerFlow.Stage.Composing, controller.flow.stage)
    }

    // ------------------------------------------------------ new, back, insert

    @Test
    fun `new clears the instruction and every version`() {
        openWith(instruction)
        controller.generate(); scope.runCurrent()
        transport.answer("Текст"); scope.runCurrent()

        controller.reset()
        val fresh = controller.session!!
        assertEquals("", fresh.instruction.text)
        assertTrue(fresh.flow.drafts.isEmpty)
        assertFalse(fresh.hasContent)
    }

    @Test
    fun `back returns to the instruction with versions kept`() {
        val session = openWith(instruction)
        controller.generate(); scope.runCurrent()
        transport.answer("Текст"); scope.runCurrent()

        controller.back()
        assertEquals(ReplyComposerFlow.Stage.Composing, controller.flow.stage)
        assertEquals(1, controller.flow.drafts.count)
        assertEquals(instruction, session.instruction.text)
    }

    @Test
    fun `insert takes the edited message, never the instruction`() {
        val session = openWith(instruction)
        controller.generate(); scope.runCurrent()
        transport.answer("Сәлем!"); scope.runCurrent()

        controller.toggleEditing()
        session.draft.set("Сәлем, достар! 👋 ")
        controller.draftEdited()
        assertEquals(
            ReplyComposerFlow.InsertDecision.Insert("Сәлем, достар! 👋"),
            controller.requestInsert(hostHasText = false)
        )
        assertEquals(ReplyComposerFlow.InsertDecision.AskAboutExistingText, controller.requestInsert(hostHasText = true))
        assertEquals(
            ReplyComposerFlow.ConflictResolution.Cancelled,
            controller.resolveConflict(ReplyComposerFlow.ConflictChoice.CANCEL)
        )
    }

    // -------------------------------------------------------------- lifecycle

    @Test
    fun `parking stops the request and a recent session comes back`() {
        val session = openWith(instruction)
        controller.generate(); scope.runCurrent()
        controller.park(now = 1_000L)
        assertFalse(controller.flow.isGenerating)
        transport.answer("Поздно"); scope.runCurrent()
        assertTrue(controller.flow.drafts.isEmpty)

        controller.restoreIfRecent(now = 2_000L)
        assertTrue(controller.isActive)
        assertEquals(instruction, session.instruction.text)
    }

    @Test
    fun `an old parked session is dropped`() {
        openWith(instruction)
        controller.park(now = 1_000L)
        controller.restoreIfRecent(now = 1_000L + 11L * 60 * 1000)
        assertFalse(controller.isActive)
    }

    // ------------------------------------------------- validation and wire

    @Test
    fun `validation counts code points against the server limit`() {
        assertEquals(
            ComposeService.ValidationResult.Invalid(AIReplyError.NoInstruction),
            ComposeService.validate(" \n ", limit = 400)
        )
        assertTrue(ComposeService.validate("ә".repeat(400), limit = 400) is ComposeService.ValidationResult.Valid)
        assertEquals(
            ComposeService.ValidationResult.Invalid(AIReplyError.InstructionTooLong(400)),
            ComposeService.validate("ә".repeat(401), limit = 400)
        )
    }

    @Test
    fun `the wire format carries the instruction only`() {
        val body = AccountComposeTransport.encode(
            ComposeService.Request("Күлжан апайды құттықта", AppLanguage.KAZAKH, isRegeneration = true),
            appVersion = "1.4.0"
        )
        val json: JsonObject = Json.parseToJsonElement(body).jsonObject
        assertEquals(setOf("instruction", "language", "regenerate", "platform", "app_version"), json.keys)
        assertEquals("Күлжан апайды құттықта", json["instruction"]!!.jsonPrimitive.content)
        assertEquals("kk", json["language"]!!.jsonPrimitive.content)
        assertTrue(json["regenerate"]!!.jsonPrimitive.boolean)
        assertEquals("android", json["platform"]!!.jsonPrimitive.content)
    }

    @Test
    fun `server instruction errors are recognised`() {
        assertEquals(
            ApiError.InstructionTooLong(350),
            ApiClient.mapServerError(400, """{"error":{"code":"INVALID_REQUEST","details":{"field":"instruction","max_characters":350}}}""")
        )
        assertEquals(
            ApiError.InstructionMissing,
            ApiClient.mapServerError(400, """{"error":{"code":"INVALID_REQUEST","details":{"field":"instruction"}}}""")
        )
    }

    @Test
    fun `backend errors become compose errors`() {
        assertEquals(AIReplyError.InstructionTooLong(350), AccountComposeTransport.map(ApiError.InstructionTooLong(350)))
        assertEquals(AIReplyError.NoInstruction, AccountComposeTransport.map(ApiError.InstructionMissing))
        assertEquals(AIReplyError.QuotaExhausted, AccountComposeTransport.map(ApiError.DailyLimitReached(7, 7, null)))
        assertEquals(AIReplyError.RateLimited, AccountComposeTransport.map(ApiError.RateLimited(5)))
        assertEquals(AIReplyError.AuthenticationFailed, AccountComposeTransport.map(ApiError.Unauthorized))
        assertEquals(AIReplyError.Offline, AccountComposeTransport.map(ApiError.Offline))
        assertEquals(AIReplyError.TimedOut, AccountComposeTransport.map(ApiError.ProviderTimeout))
        // A server without the endpoint must not say "message too long".
        assertEquals(AIReplyError.ServiceUnavailable, AccountComposeTransport.map(ApiError.NotFound))
        assertEquals(AIReplyError.ServiceUnavailable, AccountComposeTransport.map(ApiError.InvalidRequest))
    }

    @Test
    fun `the compose response decodes`() {
        val decoded = Json { ignoreUnknownKeys = true }.decodeFromString(
            ComposeResponseDto.serializer(),
            """{"text":"Құрметті Күлжан апай! 🌷","detected_language":"kk",
               "usage":{"daily_limit":7,"used_today":3,"remaining_today":4,"resets_at":"2026-03-11T19:00:00Z","timezone":"Asia/Almaty"}}"""
        )
        assertEquals("Құрметті Күлжан апай! 🌷", decoded.text)
        assertEquals("kk", decoded.detectedLanguage)
    }
}
