package kz.yerek.aireply

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kz.yerek.aireply.ai.AIReportController
import kz.yerek.aireply.ai.AIReportDraft
import kz.yerek.aireply.ai.AIReportMode
import kz.yerek.aireply.ai.AIReportReason
import kz.yerek.aireply.ai.AIReports
import kz.yerek.aireply.data.account.AIReportRequest
import kz.yerek.aireply.data.account.AccountService
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.ApiException
import kz.yerek.aireply.data.account.DeviceDescriptor
import kz.yerek.aireply.data.account.ServerConfigDto
import kz.yerek.aireply.keyboard.reply.ReplyComposerFlow
import kz.yerek.aireply.keyboard.reply.ReplyDraftHistory
import kz.yerek.aireply.support.FakeCredentials
import kz.yerek.aireply.support.FakeResponse
import kz.yerek.aireply.support.FakeServer
import kz.yerek.aireply.support.sessionOn
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Reporting what the AI wrote: the body the server gets, what never goes
 * with it, and the report's few states.
 *
 * Шағым: себеп, мәтін тек пайдаланушы қаласа, басқа ештеңе жіберілмейді.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class AIReportsTest {

    private val json = Json { ignoreUnknownKeys = true }

    // ------------------------------------------------------------------ body

    @Test
    fun `the reply text goes only when the user keeps it on`() {
        val included = AIReports.request(AIReportMode.REPLY, AIReportReason.OFFENSIVE, "  Ответ модели \n", true, "1.0")
        assertEquals("reply", included.mode)
        assertEquals("offensive", included.reason)
        assertEquals("Ответ модели", included.text)
        assertEquals("android", included.platform)
        assertEquals("1.0", included.appVersion)
        assertNull(included.comment)

        val withheld = AIReports.request(AIReportMode.COMPOSE, AIReportReason.WRONG_LANGUAGE, "Ответ модели", false, "1.0")
        assertEquals("compose", withheld.mode)
        assertEquals("wrong_language", withheld.reason)
        assertNull(withheld.text)
    }

    @Test
    fun `every reason travels as the server's code`() {
        assertEquals(
            listOf("offensive", "harmful", "false_info", "wrong_language", "other"),
            AIReportReason.entries.map { it.raw }
        )
    }

    @Test
    fun `text and comment are cut to the server's limits, in characters`() {
        val long = "😀".repeat(AIReports.MAX_TEXT + 10)
        val request = AIReports.request(
            AIReportMode.REPLY, AIReportReason.OTHER, long, includeText = true, appVersion = "1.0",
            comment = "ё".repeat(AIReports.MAX_COMMENT + 1)
        )
        assertEquals(AIReports.MAX_TEXT, request.text!!.codePointCount(0, request.text!!.length))
        assertEquals(AIReports.MAX_COMMENT, request.comment!!.length)
    }

    @Test
    fun `the body on the wire names exactly the documented fields`() = runBlocking {
        val server = FakeServer { FakeResponse(201, """{"id":"rep_1"}""") }
        val service = AccountService(
            session = sessionOn(server, FakeCredentials(access = "a", refresh = "r")),
            baseUrlProvider = { "https://example.test" },
            deviceDescriptor = { DeviceDescriptor("device-1", "android", "1.0", "15", "en", "Asia/Almaty") },
            clientFactory = { baseUrl -> ApiClient(baseUrl, openConnection = server::open) }
        )

        val id = service.reportAIOutput(
            AIReports.request(AIReportMode.REPLY, AIReportReason.HARMFUL, "Сгенерированный ответ", true, "1.2")
        )

        assertEquals("rep_1", id)
        val request = server.requests.single()
        assertEquals("POST", request.method)
        assertEquals("/api/v1/ai/reports", request.path)
        assertEquals("Bearer a", request.header("Authorization"))
        val body = json.parseToJsonElement(request.body).jsonObject
        assertEquals(setOf("mode", "reason", "text", "platform", "app_version"), body.keys)
        assertEquals("reply", body.getValue("mode").jsonPrimitive.content)
        assertEquals("harmful", body.getValue("reason").jsonPrimitive.content)
        assertEquals("Сгенерированный ответ", body.getValue("text").jsonPrimitive.content)
        assertEquals("android", body.getValue("platform").jsonPrimitive.content)
        assertEquals("1.2", body.getValue("app_version").jsonPrimitive.content)
    }

    @Test
    fun `without the text the body has no text field at all`() = runBlocking {
        val server = FakeServer { FakeResponse(201, """{"id":"rep_2"}""") }
        val service = AccountService(
            session = sessionOn(server, FakeCredentials(access = "a", refresh = "r")),
            baseUrlProvider = { "https://example.test" },
            deviceDescriptor = { DeviceDescriptor("device-1", "android", "1.0", "15", "en", "Asia/Almaty") },
            clientFactory = { baseUrl -> ApiClient(baseUrl, openConnection = server::open) }
        )
        service.reportAIOutput(AIReports.request(AIReportMode.COMPOSE, AIReportReason.OTHER, "x", false, "1.0"))
        val body = json.parseToJsonElement(server.requests.single().body).jsonObject
        assertFalse("text" in body.keys)
        assertFalse("comment" in body.keys)
    }

    @Test
    fun `the feature is announced by the server`() {
        val config = json.decodeFromString(
            ServerConfigDto.serializer(),
            """{"features":{"ai_reports":true,"account_deletion":true},"legal":{"terms_version":"2026-10-08",""" +
                """"privacy_version":"2026-10-08","terms_url":"t","privacy_url":"p","contact_email":"",""" +
                """"support_url":"https://ai-reply.kz/support","account_deletion_url":"https://ai-reply.kz/account/delete",""" +
                """"ai_provider":"OpenAI"},"payment_mode":"off"}"""
        )
        assertTrue(config.features!!.aiReports)
        assertTrue(config.features!!.accountDeletion)
        assertEquals("https://ai-reply.kz/support", config.legal!!.supportUrl)
        assertEquals("OpenAI", config.legal!!.aiProvider)
        assertEquals("off", config.paymentMode)
        assertFalse("an older server has none", json.decodeFromString(ServerConfigDto.serializer(), """{"features":{}}""").features!!.aiReports)
    }

    // ---------------------------------------------------------------- states

    private class RecordingSender : kz.yerek.aireply.ai.AIReportSender {
        val sent = mutableListOf<AIReportRequest>()
        var gate: CompletableDeferred<Unit>? = null
        var fail = false

        override suspend fun send(request: AIReportRequest) {
            sent += request
            gate?.await()
            if (fail) throw ApiException(ApiError.RateLimited(null))
        }
    }

    @Test
    fun `nothing is sent before a reason, and the text is on by default`() {
        val scope = TestScope(StandardTestDispatcher())
        val sender = RecordingSender()
        val reports = AIReportController(scope, sender, "1.0")

        reports.open(AIReportMode.REPLY, "Ответ")
        val draft = reports.draft!!
        assertTrue(draft.includeText)
        assertFalse(draft.canSend)
        reports.send()
        scope.runCurrent()
        assertTrue(sender.sent.isEmpty())
        assertEquals(AIReportDraft.Status.EDITING, draft.status)
    }

    @Test
    fun `a sent report thanks the user, then closes by itself`() {
        val scope = TestScope(StandardTestDispatcher())
        val sender = RecordingSender().apply { gate = CompletableDeferred() }
        val reports = AIReportController(scope, sender, "1.0")
        reports.open(AIReportMode.COMPOSE, "Сообщение")
        val draft = reports.draft!!
        draft.reason = AIReportReason.FALSE_INFO
        draft.includeText = false

        reports.send()
        scope.runCurrent()
        assertEquals(AIReportDraft.Status.SENDING, draft.status)
        reports.send()
        scope.runCurrent()
        assertEquals("one tap, one report", 1, sender.sent.size)

        sender.gate!!.complete(Unit)
        scope.runCurrent()
        assertEquals(AIReportDraft.Status.SENT, draft.status)
        assertSame(draft, reports.draft)
        assertNull(sender.sent.single().text)
        assertEquals("compose", sender.sent.single().mode)

        scope.advanceTimeBy(AIReportController.THANKS_MS + 1)
        scope.runCurrent()
        assertFalse(reports.isOpen)
    }

    @Test
    fun `a failed report can be sent again`() {
        val scope = TestScope(StandardTestDispatcher())
        val sender = RecordingSender().apply { fail = true }
        val reports = AIReportController(scope, sender, "1.0")
        reports.open(AIReportMode.REPLY, "Ответ")
        reports.draft!!.reason = AIReportReason.OTHER

        reports.send()
        scope.runCurrent()
        assertEquals(AIReportDraft.Status.FAILED, reports.draft!!.status)
        assertTrue(reports.draft!!.canSend)

        sender.fail = false
        reports.send()
        scope.runCurrent()
        assertEquals(AIReportDraft.Status.SENT, reports.draft!!.status)
        assertEquals(2, sender.sent.size)
    }

    // ------------------------------------------------- what can be reported

    @Test
    fun `only what the model wrote, while it is shown as a result`() {
        val drafts = ReplyDraftHistory().appending("Ответ модели")
        val result = ReplyComposerFlow(stage = ReplyComposerFlow.Stage.Result, drafts = drafts)
        assertEquals("Ответ модели", result.reportableText)

        val edited = result.copy(drafts = drafts.editing("Мой ответ"))
        assertEquals("the model's words, not the user's edit", "Ответ модели", edited.reportableText)

        assertNull(result.copy(stage = ReplyComposerFlow.Stage.Editing).reportableText)
        assertNull(result.copy(stage = ReplyComposerFlow.Stage.Composing).reportableText)
        assertNull(result.copy(stage = ReplyComposerFlow.Stage.Generating(ReplyComposerFlow.Origin.RESULT)).reportableText)
        assertNull(
            "a reply the user typed themselves",
            ReplyComposerFlow(stage = ReplyComposerFlow.Stage.Result, drafts = ReplyDraftHistory().editing("Сам")).reportableText
        )
    }
}
