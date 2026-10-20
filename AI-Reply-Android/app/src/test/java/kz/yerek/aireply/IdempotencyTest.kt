package kz.yerek.aireply

import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonObject
import kz.yerek.aireply.ai.AIConfiguration
import kz.yerek.aireply.ai.AIFeatures
import kz.yerek.aireply.ai.AILimits
import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.ai.AIReplyException
import kz.yerek.aireply.ai.AIReplyService
import kz.yerek.aireply.ai.AccountComposeTransport
import kz.yerek.aireply.ai.AccountReplyTransport
import kz.yerek.aireply.ai.ComposeService
import kz.yerek.aireply.ai.ComposeTransport
import kz.yerek.aireply.ai.GeneratedReply
import kz.yerek.aireply.ai.IdempotencyKey
import kz.yerek.aireply.ai.ReplyPromptBuilder
import kz.yerek.aireply.ai.ReplyTransport
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.data.account.AccountUsageCache
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.ServerConfigDto
import kz.yerek.aireply.data.account.ServerFeaturesDto
import kz.yerek.aireply.data.settings.SettingsStore
import kz.yerek.aireply.domain.model.EmojiPolicy
import kz.yerek.aireply.domain.model.ReplyConfiguration
import kz.yerek.aireply.domain.model.ReplyLength
import kz.yerek.aireply.domain.model.ReplyTone
import kz.yerek.aireply.domain.model.WorkingHoursBehaviour
import kz.yerek.aireply.support.FakeCredentials
import kz.yerek.aireply.support.FakeResponse
import kz.yerek.aireply.support.FakeServer
import kz.yerek.aireply.support.sessionOn
import kz.yerek.aireply.support.tokenPair
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Idempotent generation (contract §3.3): one key per user action, the same
 * key only for the automatic retry of that action, and the 409 answers.
 *
 * Бір әрекет — бір кілт: қайталау екі рет есептелмейді.
 */
class IdempotencyTest {

    @After
    fun forgetServerAnswers() {
        AILimits.install(InMemoryPreferences())
    }

    private fun announce(idempotent: Boolean) {
        AILimits.install(InMemoryPreferences())
        AILimits.apply(ServerConfigDto(features = ServerFeaturesDto(idempotentGeneration = idempotent)))
    }

    private fun context(key: String?) = AccountReplyTransport.RequestContext(
        message = "Сколько стоит доставка?",
        userInstruction = "",
        templateId = "client",
        templateName = "Клиент",
        templateRelationship = "client",
        templateTone = ReplyTone.PROFESSIONAL,
        templateInstructions = "",
        templateReplyLength = ReplyLength.SHORT,
        templateEmojiPolicy = EmojiPolicy.MINIMAL,
        templateWorkingHoursBehaviour = WorkingHoursBehaviour.IGNORE,
        templateBusiness = null,
        appLanguage = "ru",
        business = null,
        appVersion = "1.0",
        idempotencyKey = key
    )

    private fun transport(server: FakeServer, credentials: FakeCredentials, key: String?) = AccountReplyTransport(
        baseUrl = "https://example.test",
        session = sessionOn(server, credentials),
        usageCache = AccountUsageCache(SettingsStore(InMemoryPreferences())),
        context = context(key),
        clientFactory = { url, _ -> ApiClient(url, openConnection = server::open) }
    )

    private val prompt = ReplyPromptBuilder.Prompt(developer = "", user = "")

    @Test
    fun `the key is a header, and only for a server that announced it`() {
        val key = IdempotencyKey.create()
        assertTrue("a UUID is what the server accepts", IdempotencyKey.isValid(key))
        assertEquals(emptyMap<String, String>(), IdempotencyKey.headers(key, AIFeatures.NONE))
        assertEquals(mapOf("Idempotency-Key" to key), IdempotencyKey.headers(key, AIFeatures.NONE.copy(idempotentGeneration = true)))
        assertEquals(emptyMap<String, String>(), IdempotencyKey.headers(null, AIFeatures.NONE.copy(idempotentGeneration = true)))
        assertEquals(
            "a key the server would refuse is not sent",
            emptyMap<String, String>(),
            IdempotencyKey.headers("short", AIFeatures.NONE.copy(idempotentGeneration = true))
        )
    }

    @Test
    fun `the retry after a token refresh is the same action and sends the same key`() = runBlocking {
        announce(idempotent = true)
        var replies = 0
        val server = FakeServer { request ->
            when (request.path) {
                "/api/v1/auth/refresh" -> tokenPair("access-2", "refresh-2")
                "/api/v1/ai/reply" -> if (replies++ == 0) {
                    FakeResponse(401, FakeServer.envelope("TOKEN_EXPIRED"))
                } else {
                    FakeResponse(200, """{"reply":"Доставка 1500 тенге."}""")
                }
                else -> FakeResponse(404)
            }
        }
        val key = IdempotencyKey.create()
        val reply = transport(server, FakeCredentials("access-1", "refresh-1"), key).generate(prompt)

        assertEquals("Доставка 1500 тенге.", reply.text)
        val sent = server.requests.filter { it.path == "/api/v1/ai/reply" }
        assertEquals(2, sent.size)
        assertEquals(listOf(key, key), sent.map { it.header("Idempotency-Key") })
        val body = Json.parseToJsonElement(sent.first().body).jsonObject
        assertFalse("never in the body: the server refuses unknown fields", body.keys.any { it.contains("idempotency") })
    }

    @Test
    fun `an older server gets no header`() = runBlocking {
        announce(idempotent = false)
        val server = FakeServer { FakeResponse(200, """{"reply":"Ок"}""") }
        transport(server, FakeCredentials("access-1", "refresh-1"), IdempotencyKey.create()).generate(prompt)
        assertNull(server.requests.single().header("Idempotency-Key"))
    }

    @Test
    fun `every reply tap is a new action with a key of its own`() = runBlocking {
        val keys = mutableListOf<String?>()
        val service = AIReplyService(
            configuration = AIConfiguration { true },
            nameTemplate = { template, _ -> template.id },
            accountTransport = { _, context ->
                keys += context.idempotencyKey
                object : ReplyTransport {
                    override suspend fun generate(prompt: ReplyPromptBuilder.Prompt) = GeneratedReply("Ок")
                }
            }
        )
        val template = ReplyConfiguration.INITIAL.visibleTemplates.first()
        val request = AIReplyService.Request("Привет", template, ReplyConfiguration.INITIAL, AppLanguage.RUSSIAN)
        service.generate(request)
        service.generate(request)

        assertEquals(2, keys.size)
        assertTrue(keys.all { it != null && IdempotencyKey.isValid(it) })
        assertNotEquals("Regenerate is a new action", keys[0], keys[1])

        service.generate(request.copy(idempotencyKey = "retry-of-one-action"))
        assertEquals("a caller that retries one action keeps its key", "retry-of-one-action", keys.last())
    }

    @Test
    fun `every write and regenerate tap is a new action too`() = runBlocking {
        val keys = mutableListOf<String?>()
        val service = ComposeService(
            configuration = AIConfiguration { true },
            accountTransport = { error("not used") },
            transportOverride = { request ->
                keys += request.idempotencyKey
                ComposeTransport { GeneratedReply("Поздравляю!") }
            }
        )
        val request = ComposeService.Request("Поздравь коллегу", AppLanguage.RUSSIAN)
        service.compose(request)
        service.compose(request.copy(isRegeneration = true))
        assertEquals(2, keys.toSet().size)
        assertTrue(keys.all { it != null && IdempotencyKey.isValid(it) })

        val body = Json.parseToJsonElement(AccountComposeTransport.encode(request.copy(idempotencyKey = "abc-12345"), "1.0")).jsonObject
        assertFalse(body.keys.any { it.contains("idempotency") })
    }

    @Test
    fun `a 409 for the key says whether waiting helps`() {
        fun conflict(reason: String) =
            """{"error":{"code":"CONFLICT","message":"x","details":{"field":"idempotency_key","reason":"$reason"}}}"""

        val inProgress = ApiClient.mapServerError(409, conflict("in_progress"))
        assertEquals(ApiError.GenerationConflict(inProgress = true), inProgress)
        assertEquals("the first request is still running: wait", AIReplyError.RateLimited, AccountReplyTransport.map(inProgress))

        val used = ApiClient.mapServerError(409, conflict("used"))
        assertEquals(ApiError.GenerationConflict(inProgress = false), used)
        assertEquals("a new tap is a new action", AIReplyError.ServiceUnavailable, AccountReplyTransport.map(used))
        assertEquals(AIReplyError.ServiceUnavailable, AccountComposeTransport.map(used))

        assertEquals(
            "any other conflict is unchanged",
            ApiError.Conflict,
            ApiClient.mapServerError(409, """{"error":{"code":"CONFLICT","details":{"field":"email"}}}""")
        )
    }

    @Test
    fun `a 409 from the server reaches the caller as a closed error`() = runBlocking {
        announce(idempotent = true)
        val server = FakeServer {
            FakeResponse(409, """{"error":{"code":"CONFLICT","details":{"field":"idempotency_key","reason":"in_progress"}}}""")
        }
        val failure = runCatching {
            transport(server, FakeCredentials("access-1", "refresh-1"), IdempotencyKey.create()).generate(prompt)
        }.exceptionOrNull() as AIReplyException
        assertEquals(AIReplyError.RateLimited, failure.error)
        assertFalse(failure.canBuy)
    }
}
