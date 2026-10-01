package kz.yerek.aireply

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.ApiException
import kz.yerek.aireply.data.account.TransportFailure
import kz.yerek.aireply.data.account.TransportFailureReport
import kz.yerek.aireply.telemetry.EventBatch
import kz.yerek.aireply.telemetry.EventReporter
import kz.yerek.aireply.telemetry.EventSchema
import kz.yerek.aireply.telemetry.EventsApi
import kz.yerek.aireply.telemetry.EventsResult
import kz.yerek.aireply.telemetry.SessionTracker
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Diagnostics: only allow-listed events, only with consent and a willing
 * server, in batches, and never lost to a flaky network — nor kept forever.
 *
 * Диагностика: тек рұқсат етілген оқиғалар, келісіммен, топтап.
 */
class EventReporterTest {

    private var now = 1_700_000_000_000L
    private var userAllows = true
    private var serverAllows: Boolean? = true
    private var session: String? = "5e55a0b1-0000-4000-8000-000000000001"
    private var token: String? = null
    private var account = EventReporter.ANONYMOUS
    private var consent = true
    private var counter = 0
    private val api = RecordingEvents()

    private class RecordingEvents : EventsApi {
        val batches = mutableListOf<Pair<EventBatch, String?>>()
        var answer: (EventBatch, String?) -> EventsResult = { batch, _ -> EventsResult(accepted = batch.events.size) }

        override suspend fun send(batch: EventBatch, token: String?): EventsResult {
            batches += batch to token
            return answer(batch, token)
        }
    }

    /** Scheduled flushes never run here; the tests flush by hand. */
    private val reporter = EventReporter(
        api = api,
        installationId = { "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d" },
        sessionId = { session },
        userAllows = { userAllows },
        serverAllows = { serverAllows },
        token = { token },
        consentGiven = { consent },
        accountKey = { account },
        scope = CoroutineScope(Job().apply { cancel() }),
        clock = { now },
        newId = { "00000000-0000-4000-8000-" + (counter++).toString().padStart(12, '0') }
    )

    // ------------------------------------------------------------ allowlist

    @Test
    fun `only allow-listed events with valid properties are queued`() {
        assertFalse(reporter.record("keystroke"))
        assertFalse(reporter.record("message_sent", mapOf("text" to "hello")))
        assertFalse("wrong type", reporter.record(EventSchema.APP_OPENED, mapOf("cold_start" to "yes")))
        assertFalse("unknown property", reporter.record(EventSchema.LOGOUT, mapOf("reason" to "user")))
        assertFalse("not an id", reporter.record(EventSchema.NOTIFICATION_OPENED, mapOf("notification_id" to "a b")))
        assertFalse("not an allowed value", reporter.record(EventSchema.LOGIN_FAILED, mapOf("method" to "phone")))
        assertFalse("the server's list has it, this app does not send it",
            reporter.record("payment_success", mapOf("plan_code" to "pro")))
        assertTrue(reporter.pending.isEmpty())

        reporter.appOpened(coldStart = true)
        reporter.notificationOpened(mapOf("notification_id" to "n-1", "delivery_id" to "d-1", "type" to "promo"))
        reporter.pushTokenRegistrationFailed("rate_limited", "req_abcdefghijklmnop")
        reporter.apiError(TransportFailureReport("/api/v1/me", TransportFailure.OFFLINE, "req_abcdefghijklmnop"))
        assertEquals(
            listOf("app_opened", "notification_opened", "push_token_registration_failed", "api_error"),
            reporter.pending.map { it.name }
        )
        reporter.pending.forEach { assertTrue(it.name, EventSchema.isValid(it.name, it.properties)) }
        assertEquals(0L, reporter.pending.last().properties["status"])
    }

    @Test
    fun `nothing is kept when the user switched diagnostics off`() {
        reporter.appOpened(coldStart = true)
        userAllows = false
        reporter.appOpened(coldStart = false)
        assertEquals(1, reporter.pending.size)

        runBlocking { reporter.flush() }
        assertTrue("switching off drops what was queued", reporter.pending.isEmpty())
        assertTrue(api.batches.isEmpty())
    }

    @Test
    fun `events wait for the server's answer and are dropped if it says no`() = runBlocking {
        serverAllows = null
        reporter.appOpened(coldStart = true)
        reporter.flush()
        assertTrue("not sent before the server has answered", api.batches.isEmpty())
        assertEquals(1, reporter.pending.size)

        serverAllows = false
        reporter.onServerFeaturesChanged()
        assertTrue(reporter.pending.isEmpty())
        assertFalse("and nothing new is taken", reporter.appOpened(coldStart = false))
    }

    // -------------------------------------------------------------- batches

    @Test
    fun `at most a hundred events wait, sent fifty at a time`() = runBlocking {
        repeat(120) { reporter.logout() }
        assertEquals("the oldest are dropped", 100, reporter.pending.size)
        assertEquals("00000000-0000-4000-8000-000000000020", reporter.pending.first().id)

        reporter.flush()

        assertEquals(listOf(50, 50), api.batches.map { it.first.events.size })
        assertTrue(reporter.pending.isEmpty())
    }

    @Test
    fun `a batch holds one session's events`() = runBlocking {
        reporter.appOpened(coldStart = true)
        reporter.appBackgrounded(12)
        session = "5e55a0b1-0000-4000-8000-000000000002"
        reporter.appOpened(coldStart = false)

        reporter.flush()

        assertEquals(
            listOf("5e55a0b1-0000-4000-8000-000000000001", "5e55a0b1-0000-4000-8000-000000000002"),
            api.batches.map { it.first.sessionId }
        )
        assertEquals(listOf(2, 1), api.batches.map { it.first.events.size })
    }

    @Test
    fun `the body is the server's shape`() {
        now = 1_759_311_000_000L // 2025-10-01T09:30:00Z
        reporter.notificationOpened(mapOf("notification_id" to "n-1", "delivery_id" to "d-1"))
        reporter.appBackgrounded(42)
        val batch = EventBatch("0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d", session, reporter.pending)

        val json = Json.parseToJsonElement(batch.toJson()).jsonObject
        assertEquals(setOf("installation_id", "session_id", "events"), json.keys)
        val events = json["events"]!!.jsonArray
        val first = events[0].jsonObject
        assertEquals(setOf("id", "name", "occurred_at", "properties"), first.keys)
        assertEquals("notification_opened", first["name"]!!.jsonPrimitive.content)
        assertEquals("2025-10-01T09:30:00Z", first["occurred_at"]!!.jsonPrimitive.content)
        assertEquals("42", events[1].jsonObject["properties"]!!.jsonObject["foreground_seconds"].toString())

        val anonymous = Json.parseToJsonElement(EventBatch("i-1234567", null, reporter.pending).toJson()).jsonObject
        assertFalse("no session, no session_id", anonymous.containsKey("session_id"))
    }

    // ------------------------------------------------------------- failures

    @Test
    fun `network trouble, 5xx and 429 keep the events for later`() = runBlocking {
        reporter.appOpened(coldStart = true)
        listOf(
            ApiException(ApiError.Offline, httpStatus = 0, transport = TransportFailure.OFFLINE),
            ApiException(ApiError.Server, httpStatus = 503),
            ApiException(ApiError.RateLimited(null), httpStatus = 429)
        ).forEachIndexed { index, failure ->
            api.answer = { _, _ -> throw failure }
            now += 60L * 60 * 1000 // past any backoff
            reporter.flush()
            assertEquals("attempt ${index + 1}", index + 1, api.batches.size)
            assertEquals(1, reporter.pending.size)
        }

        api.answer = { batch, _ -> EventsResult(accepted = batch.events.size) }
        reporter.flush()
        assertEquals("backing off: not yet", 3, api.batches.size)
        now += 60L * 60 * 1000
        reporter.flush()
        assertTrue(reporter.pending.isEmpty())
    }

    @Test
    fun `any other 4xx drops the batch`() = runBlocking {
        api.answer = { _, _ -> throw ApiException(ApiError.InvalidRequest, httpStatus = 400) }
        reporter.appOpened(coldStart = true)
        reporter.flush()
        assertTrue(reporter.pending.isEmpty())
    }

    @Test
    fun `a token the server refuses is dropped, not the events`() = runBlocking {
        account = "user-a"
        token = "stale"
        api.answer = { batch, sentToken ->
            if (sentToken != null) throw ApiException(ApiError.Unauthorized, httpStatus = 401)
            EventsResult(accepted = batch.events.size)
        }
        reporter.logout()
        reporter.flush()
        assertEquals(listOf("stale", null), api.batches.map { it.second })
        assertTrue(reporter.pending.isEmpty())
    }

    @Test
    fun `a server that switched events off stops the sending`() = runBlocking {
        api.answer = { _, _ -> EventsResult(disabled = true) }
        reporter.logout()
        reporter.flush()
        assertTrue(reporter.pending.isEmpty())
        assertFalse(reporter.logout())
    }

    @Test
    fun `events older than the server keeps are not sent`() = runBlocking {
        reporter.logout()
        now += 8L * 24 * 60 * 60 * 1000
        reporter.appOpened(coldStart = false)
        reporter.flush()
        assertEquals(listOf("app_opened"), api.batches.single().first.events.map { it.name })
    }

    @Test
    fun `connection errors are counted sparingly, and never the reporter's own`() {
        val offline = TransportFailureReport("/api/v1/me", TransportFailure.OFFLINE, "req_abcdefghijklmnop")
        reporter.apiError(offline)
        reporter.apiError(offline)
        reporter.apiError(TransportFailureReport("/api/v1/events", TransportFailure.OFFLINE, "req_abcdefghijklmnop"))
        assertEquals(1, reporter.pending.size)
        now += 61_000
        reporter.apiError(offline)
        assertEquals(2, reporter.pending.size)
    }

    // ------------------------------------------------------------- accounts

    @Test
    fun `events go with the token only under the account they happened in`() = runBlocking {
        account = "user-a"
        token = "token-a"
        reporter.notificationOpened(mapOf("notification_id" to "n-1"))
        reporter.logout()
        account = EventReporter.ANONYMOUS
        token = null
        reporter.appOpened(coldStart = false)
        account = "user-b"
        token = "token-b"
        reporter.appOpened(coldStart = false)

        reporter.flush()

        assertEquals(
            "user A's events are not attributed to user B",
            listOf<String?>(null, null, "token-b"),
            api.batches.map { it.second }
        )
        assertEquals(listOf(2, 1, 1), api.batches.map { it.first.events.size })
    }

    @Test
    fun `a signed-in account goes with its token`() = runBlocking {
        account = "user-a"
        token = "token-a"
        reporter.logout()
        reporter.flush()
        assertEquals("token-a", api.batches.single().second)
    }

    // -------------------------------------------------------------- consent

    @Test
    fun `nothing is recorded or sent before the terms are accepted`() = runBlocking {
        consent = false
        assertFalse(reporter.appOpened(coldStart = true))
        assertTrue("dropped, not kept for later", reporter.pending.isEmpty())

        consent = true
        reporter.appOpened(coldStart = false)
        consent = false
        reporter.flush()
        assertTrue("held while consent is missing", api.batches.isEmpty())

        consent = true
        reporter.flush()
        assertEquals(1, api.batches.size)
    }

    @Test
    fun `a retry time from a clock that ran ahead does not block sending`() = runBlocking {
        api.answer = { _, _ -> throw ApiException(ApiError.Server, httpStatus = 503) }
        now += 365L * 24 * 60 * 60 * 1000 // the clock runs a year ahead
        reporter.logout()
        reporter.flush() // fails: the retry time is stored a year ahead
        api.answer = { batch, _ -> EventsResult(accepted = batch.events.size) }
        now -= 365L * 24 * 60 * 60 * 1000 // the clock is corrected
        reporter.flush()
        assertEquals(2, api.batches.size)
        assertTrue(reporter.pending.isEmpty())
    }

    // ------------------------------------------------------------- sessions

    @Test
    fun `sessions start cold, survive short breaks and renew after half an hour`() {
        var clock = 0L
        var ids = 0
        val opened = mutableListOf<Boolean>()
        val away = mutableListOf<Long>()
        val tracker = SessionTracker(
            clock = { clock },
            newId = { "session-${ids++}" },
            listener = object : SessionTracker.Listener {
                override fun onForeground(coldStart: Boolean) {
                    opened += coldStart
                }

                override fun onBackground(foregroundMillis: Long) {
                    away += foregroundMillis
                }
            }
        )
        assertNull(tracker.sessionId)

        tracker.screenStarted()
        assertEquals("session-0", tracker.sessionId)
        clock += 5_000
        tracker.screenStopped(changingConfigurations = true)
        tracker.screenStarted()
        assertEquals("a language change is not leaving the app", listOf(true), opened)

        clock += 5_000
        tracker.screenStopped(changingConfigurations = false)
        assertEquals(listOf(10_000L), away)

        clock += 29 * 60_000
        tracker.screenStarted()
        assertEquals("back within 30 minutes: same session", "session-0", tracker.sessionId)
        tracker.screenStopped(changingConfigurations = false)

        clock += 30 * 60_000
        tracker.screenStarted()
        assertEquals("session-1", tracker.sessionId)
        assertEquals(listOf(true, false, false), opened)
        assertTrue(tracker.isForeground)
    }
}
