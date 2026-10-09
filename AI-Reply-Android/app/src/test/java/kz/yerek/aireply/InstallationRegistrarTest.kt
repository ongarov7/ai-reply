package kz.yerek.aireply

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonObject
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.ApiException
import kz.yerek.aireply.data.account.ServerConfigDto
import kz.yerek.aireply.data.account.SessionAuth
import kz.yerek.aireply.push.HttpPushApi
import kz.yerek.aireply.push.InstallationApi
import kz.yerek.aireply.push.InstallationRegistrar
import kz.yerek.aireply.push.InstallationRegistrar.Outcome
import kz.yerek.aireply.push.InstallationRequest
import kz.yerek.aireply.push.InstallationResponse
import kz.yerek.aireply.push.InvalidTokenRenewal
import kz.yerek.aireply.push.NotificationOpenedRequest
import kz.yerek.aireply.push.PushStateStore
import kz.yerek.aireply.push.PushTokenDto
import kz.yerek.aireply.support.FakeCredentials
import kz.yerek.aireply.support.FakeResponse
import kz.yerek.aireply.support.FakeServer
import kz.yerek.aireply.support.sessionOn
import kz.yerek.aireply.support.tokenPair
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.concurrent.atomic.AtomicInteger

/**
 * When the installation is registered, with which token, and what happens on
 * failure.
 *
 * Орнатуды тіркеу: өзгергенде ғана, 401-де бір рет жаңартып қайталау, шыққанда анонимді.
 */
class InstallationRegistrarTest {

    private var now = 1_700_000_000_000L
    private val store = PushStateStore(InMemoryPreferences())
    private val api = RecordingApi()
    private var request = sample()
    private val idle = CoroutineScope(Job())

    private class SimpleSession(var signedIn: Boolean, var token: String = "t1") : SessionAuth {
        override val isSignedIn: Boolean get() = signedIn
        override suspend fun <T> authenticated(work: suspend (String) -> T): T = work(token)
    }

    private class RecordingApi : InstallationApi {
        val calls = mutableListOf<Pair<InstallationRequest, String?>>()
        var answer: (InstallationRequest, String?) -> InstallationResponse = { request, token ->
            InstallationResponse(
                installationId = request.installationId,
                attached = token != null,
                pushStatus = if (request.push != null) "active" else "none",
                pushAvailable = true
            )
        }

        override suspend fun register(request: InstallationRequest, token: String?): InstallationResponse {
            calls += request to token
            return answer(request, token)
        }
    }

    private fun registrar(session: SessionAuth, enabled: Boolean = true) = InstallationRegistrar(
        api = api,
        session = session,
        store = store,
        snapshot = { request },
        isEnabled = { enabled },
        scope = idle,
        clock = { now }
    )

    // ---------------------------------------------------------------- dedupe

    @Test
    fun `sends once, then only when something changes or a day has passed`() = runBlocking {
        val registrar = registrar(SimpleSession(signedIn = false))

        assertTrue(registrar.syncOnce() is Outcome.Synced)
        assertEquals(Outcome.UpToDate, registrar.syncOnce())
        now += 23 * HOUR
        assertEquals(Outcome.UpToDate, registrar.syncOnce())
        assertEquals(1, api.calls.size)

        request = request.copy(notificationPermission = "authorized")
        assertTrue("a changed payload goes at once", registrar.syncOnce() is Outcome.Synced)

        now += 24 * HOUR
        assertTrue("unchanged, but a day later", registrar.syncOnce() is Outcome.Synced)
        assertEquals(3, api.calls.size)
    }

    @Test
    fun `a new push token is sent at once`() = runBlocking {
        val registrar = registrar(SimpleSession(signedIn = false))
        registrar.syncOnce()
        request = request.copy(push = PushTokenDto("fcm", "fcm-token-000000000000000002"))
        assertTrue(registrar.syncOnce() is Outcome.Synced)
        assertEquals("fcm-token-000000000000000002", api.calls.last().first.push?.token)
    }

    @Test
    fun `nothing is sent to a server without installations`() = runBlocking {
        assertEquals(Outcome.Disabled, registrar(SimpleSession(signedIn = true), enabled = false).syncOnce())
        assertTrue(api.calls.isEmpty())
    }

    @Test
    fun `nothing is registered before the terms are accepted, then at once`() = runBlocking {
        var consent = false
        val registrar = InstallationRegistrar(
            api = api, session = SimpleSession(signedIn = false), store = store, snapshot = { request },
            isEnabled = { consent }, scope = idle, clock = { now }
        )
        assertEquals(Outcome.Disabled, registrar.syncOnce())
        assertTrue(api.calls.isEmpty())

        consent = true
        assertTrue(registrar.syncOnce() is Outcome.Synced)
    }

    // --------------------------------------------------------------- account

    @Test
    fun `signed in it goes with the token, signed out without, even for the same payload`() = runBlocking {
        val session = SimpleSession(signedIn = true, token = "t1")
        store.accountUserId = "user-a"
        val registrar = registrar(session)

        registrar.syncOnce()
        assertEquals("t1", api.calls.single().second)

        // Sign-out: the payload is identical, the owner is not.
        session.signedIn = false
        store.accountUserId = null
        assertTrue(registrar.syncOnce() is Outcome.Synced)
        assertNull("anonymous: detached from the previous account", api.calls.last().second)
    }

    @Test
    fun `another account on the same phone registers again`() = runBlocking {
        val session = SimpleSession(signedIn = true, token = "token-a")
        store.accountUserId = "user-a"
        val registrar = registrar(session)
        registrar.syncOnce()

        session.token = "token-b"
        store.accountUserId = "user-b"
        assertTrue(registrar.syncOnce() is Outcome.Synced)
        assertEquals(listOf("token-a", "token-b"), api.calls.map { it.second })
    }

    @Test
    fun `a sign-out after a sign-in whose answer was lost still registers anonymously`() = runBlocking {
        val session = SimpleSession(signedIn = false, token = "t1")
        val registrar = registrar(session)
        val accept = api.answer
        assertTrue(registrar.syncOnce() is Outcome.Synced)

        // The server may have attached the installation; the answer never came.
        session.signedIn = true
        store.accountUserId = "user-a"
        api.answer = { _, _ -> throw ApiException(ApiError.Offline) }
        assertTrue(registrar.syncOnce() is Outcome.RetryLater)

        session.signedIn = false
        store.accountUserId = null
        api.answer = accept
        assertTrue("the anonymous state is sent again", registrar.syncOnce() is Outcome.Synced)
        assertEquals(listOf(null, "t1", null), api.calls.map { it.second })
    }

    @Test
    fun `a sign-in after a sign-out whose answer was lost attaches again`() = runBlocking {
        val session = SimpleSession(signedIn = true, token = "t1")
        store.accountUserId = "user-a"
        val registrar = registrar(session)
        val accept = api.answer
        assertTrue(registrar.syncOnce() is Outcome.Synced)

        // The anonymous registration may have detached it; the answer never came.
        session.signedIn = false
        store.accountUserId = null
        api.answer = { _, _ -> throw ApiException(ApiError.Server, httpStatus = 503) }
        assertTrue(registrar.syncOnce() is Outcome.RetryLater)

        session.signedIn = true
        store.accountUserId = "user-a"
        api.answer = accept
        assertTrue("the same account is attached again", registrar.syncOnce() is Outcome.Synced)
        assertEquals(listOf("t1", null, "t1"), api.calls.map { it.second })
    }

    @Test
    fun `a 401 refreshes the token once and retries once`() = runBlocking {
        val server = FakeServer { request ->
            if (request.path == "/api/v1/auth/refresh") tokenPair("new", "r2")
            else FakeResponse(404)
        }
        val credentials = FakeCredentials(access = "old", refresh = "r1", fresh = true)
        api.answer = { request, token ->
            if (token != "new") throw ApiException(ApiError.Unauthorized, httpStatus = 401)
            InstallationResponse(installationId = request.installationId, attached = true)
        }

        val outcome = registrar(sessionOn(server, credentials)).syncOnce()

        assertTrue(outcome is Outcome.Synced)
        assertEquals(listOf("old", "new"), api.calls.map { it.second })
        assertEquals(1, server.count("/api/v1/auth/refresh"))
    }

    // -------------------------------------------------------------- failures

    @Test
    fun `network trouble backs off, and the same payload waits for it`() = runBlocking {
        api.answer = { _, _ -> throw ApiException(ApiError.Offline) }
        val registrar = registrar(SimpleSession(signedIn = false))

        assertEquals(Outcome.RetryLater(30_000), registrar.syncOnce())
        now += 10_000
        assertEquals("still waiting", Outcome.RetryLater(20_000), registrar.syncOnce())
        assertEquals(1, api.calls.size)

        now += 20_000
        assertEquals("twice as long after the second failure", Outcome.RetryLater(60_000), registrar.syncOnce())
        assertEquals(2, api.calls.size)
    }

    @Test
    fun `too many requests waits at least as long as the server asks`() = runBlocking {
        api.answer = { _, _ -> throw ApiException(ApiError.RateLimited(300), httpStatus = 429) }
        assertEquals(Outcome.RetryLater(300_000), registrar(SimpleSession(signedIn = false)).syncOnce())
    }

    @Test
    fun `a refused payload is not sent again until it changes`() = runBlocking {
        api.answer = { _, _ -> throw ApiException(ApiError.InvalidRequest, httpStatus = 400) }
        val registrar = registrar(SimpleSession(signedIn = false))

        assertEquals(Outcome.Rejected, registrar.syncOnce())
        assertEquals(Outcome.Rejected, registrar.syncOnce())
        assertEquals(1, api.calls.size)

        request = request.copy(locale = "ru")
        registrar.syncOnce()
        assertEquals(2, api.calls.size)
    }

    @Test
    fun `a wait stored while the clock ran ahead does not block retries`() = runBlocking {
        api.answer = { _, _ -> throw ApiException(ApiError.Server, httpStatus = 503) }
        val registrar = registrar(SimpleSession(signedIn = false))
        now += 365L * 24 * HOUR
        registrar.syncOnce() // fails: next attempt stored a year ahead of the real time
        now -= 365L * 24 * HOUR
        registrar.syncOnce()
        assertEquals("the corrected clock retries at once", 2, api.calls.size)
    }

    @Test
    fun `no wait is longer than an hour, whatever Retry-After says`() = runBlocking {
        api.answer = { _, _ -> throw ApiException(ApiError.RateLimited(86_400), httpStatus = 429) }
        assertEquals(
            Outcome.RetryLater(InstallationRegistrar.MAX_RETRY_MS),
            registrar(SimpleSession(signedIn = false)).syncOnce()
        )
    }

    @Test
    fun `a success clears the backoff`() = runBlocking {
        var fail = true
        api.answer = { request, _ ->
            if (fail) throw ApiException(ApiError.Server, httpStatus = 503)
            InstallationResponse(installationId = request.installationId)
        }
        val registrar = registrar(SimpleSession(signedIn = false))
        registrar.syncOnce()
        fail = false
        now += 30_000
        assertTrue(registrar.syncOnce() is Outcome.Synced)
        assertEquals(0, store.failureCount)
        assertEquals(0L, store.nextAttemptAt)
    }

    // -------------------------------------------------------------- triggers

    @Test
    fun `triggers during a sync fold into one more pass, and failures retry by themselves`() = runTest {
        val dispatcher = StandardTestDispatcher(testScheduler)
        val scope = CoroutineScope(dispatcher + Job())
        var failures = 1
        api.answer = { request, _ ->
            if (failures-- > 0) throw ApiException(ApiError.Offline)
            InstallationResponse(installationId = request.installationId)
        }
        val registrar = InstallationRegistrar(
            api = api,
            session = SimpleSession(signedIn = false),
            store = store,
            snapshot = { request },
            isEnabled = { true },
            scope = scope,
            clock = { now + testScheduler.currentTime }
        )

        registrar.requestSync()
        registrar.requestSync()
        registrar.requestSync()
        runCurrent()
        assertEquals("one attempt, then an up-to-date check", 1, api.calls.size)

        advanceTimeBy(30_001)
        runCurrent()
        assertEquals("the retry went by itself", 2, api.calls.size)
        assertFalse(store.lastSyncFingerprint.isNullOrEmpty())
        scope.coroutineContext[Job]?.cancel()
    }

    @Test
    fun `an old server has no installations, push or preferred language`() {
        val json = Json { ignoreUnknownKeys = true }
        val production = json.decodeFromString(
            ServerConfigDto.serializer(),
            """{"features": {"reply_preferences": true, "email_otp": true, "google_sign_in": true}}"""
        ).features!!
        assertFalse(production.installations)
        assertFalse(production.pushNotifications)
        assertFalse(production.preferredLanguage)
        assertTrue("sign-in flags still default to on", production.appleSignIn)

        val current = json.decodeFromString(
            ServerConfigDto.serializer(),
            """{"features": {"installations": true, "push_notifications": true, "preferred_language": true}}"""
        ).features!!
        assertTrue(current.installations && current.pushNotifications && current.preferredLanguage)
    }

    @Test
    fun `a sync asked for while the last pass is finishing is never lost`() = runBlocking {
        val scope = CoroutineScope(Dispatchers.Default + Job())
        val version = AtomicInteger(0)
        val lastSent = AtomicInteger(-1)
        val concurrentStore = PushStateStore(InMemoryPreferences())
        val registrar = InstallationRegistrar(
            api = { request, _ ->
                lastSent.set(request.appBuild.toInt())
                InstallationResponse(installationId = request.installationId)
            },
            session = SimpleSession(signedIn = false),
            store = concurrentStore,
            snapshot = { request.copy(appBuild = version.get().toString()) },
            isEnabled = { true },
            scope = scope,
            clock = { now }
        )
        repeat(3_000) { round ->
            version.incrementAndGet()
            registrar.requestSync()
            if (round % 64 == 0) delay(1)
        }
        try {
            withTimeout(20_000) {
                while (lastSent.get() != version.get()) delay(5)
            }
        } finally {
            scope.coroutineContext[Job]?.cancel()
        }
        assertEquals("the newest state reached the server", version.get(), lastSent.get())
    }

    // ------------------------------------------------------------------ body

    @Test
    fun `the body has exactly the server's fields, and no push without a token`() {
        val withoutToken = Json.parseToJsonElement(
            HttpPushApi.json.encodeToString(InstallationRequest.serializer(), request)
        ).jsonObject
        assertEquals(
            setOf(
                "installation_id", "platform", "app_version", "app_build", "os_name", "os_version",
                "device_model", "manufacturer", "locale", "timezone", "notification_permission",
                "notifications_enabled"
            ),
            withoutToken.keys
        )

        val withToken = Json.parseToJsonElement(
            HttpPushApi.json.encodeToString(
                InstallationRequest.serializer(),
                request.copy(push = PushTokenDto("fcm", "fcm-token-000000000000000001"))
            )
        ).jsonObject
        assertEquals(setOf("provider", "token"), withToken["push"]!!.jsonObject.keys)
        assertEquals("\"android\"", withToken["platform"].toString())
        assertEquals("false", withToken["notifications_enabled"].toString())
    }

    @Test
    fun `the fingerprint changes with the account but not with nothing`() {
        val same = InstallationRegistrar.fingerprint(request, "user-a")
        assertEquals(same, InstallationRegistrar.fingerprint(request.copy(), "user-a"))
        assertFalse(same == InstallationRegistrar.fingerprint(request, "user-b"))
        assertFalse(same == InstallationRegistrar.fingerprint(request, InstallationRegistrar.ANONYMOUS))
        assertFalse("the token is not kept in the clear", same.contains("fcm"))
    }

    // ------------------------------------------------------------------ wire

    @Test
    fun `a registration goes to the installations endpoint with the token it was given`() = runBlocking {
        val server = FakeServer {
            FakeResponse(200, """{"installation_id":"${request.installationId}","attached":true,"push_status":"active","push_available":true,"preferences":{"marketing":false}}""")
        }
        val api = HttpPushApi(
            client = { ApiClient("https://example.test", openConnection = server::open) },
            session = SimpleSession(signedIn = true)
        )

        val response = api.register(request.copy(push = PushTokenDto("fcm", "fcm-token-000000000000000003")), "t1")
        api.register(request, null)

        val (signedIn, anonymous) = server.requests.toList()
        assertEquals("POST", signedIn.method)
        assertEquals("/api/v1/installations", signedIn.path)
        assertEquals("Bearer t1", signedIn.header("Authorization"))
        assertNull("anonymous: no token at all", anonymous.header("Authorization"))
        assertNull("no metadata headers", signedIn.header("X-Installation-ID"))
        assertEquals("\"fcm\"", Json.parseToJsonElement(signedIn.body).jsonObject["push"]!!.jsonObject["provider"].toString())
        assertTrue(response.attached)
        assertEquals(mapOf("marketing" to false), response.preferences)
    }

    @Test
    fun `a refused registration keeps its status for the backoff rule`() = runBlocking {
        val server = FakeServer { FakeResponse(400, FakeServer.envelope("INVALID_REQUEST")) }
        val api = HttpPushApi(
            client = { ApiClient("https://example.test", openConnection = server::open) },
            session = SimpleSession(signedIn = false)
        )
        val failure = runCatching { api.register(request, null) }.exceptionOrNull() as ApiException
        assertEquals(400, failure.httpStatus)
        assertTrue(InstallationRegistrar.isPermanent(failure))
        assertFalse(InstallationRegistrar.isPermanent(ApiException(ApiError.Offline)))
        assertFalse(InstallationRegistrar.isPermanent(ApiException(ApiError.Unauthorized, httpStatus = 401)))
    }

    @Test
    fun `a tapped notification is recorded for this installation, without a token`() = runBlocking {
        val server = FakeServer { FakeResponse(200, """{"ok":true,"recorded":true}""") }
        val api = HttpPushApi(
            client = { ApiClient("https://example.test", openConnection = server::open) },
            session = SimpleSession(signedIn = true)
        )

        val response = api.opened(NotificationOpenedRequest(request.installationId, "5e55a0b1-0000-4000-8000-000000000001"))

        assertTrue(response.recorded)
        val sent = server.requests.single()
        assertEquals("POST", sent.method)
        assertEquals("/api/v1/notifications/opened", sent.path)
        assertNull(sent.header("Authorization"))
        val body = Json.parseToJsonElement(sent.body).jsonObject
        assertEquals(setOf("installation_id", "delivery_id"), body.keys)
        assertEquals("\"${request.installationId}\"", body["installation_id"].toString())
    }

    // --------------------------------------------------- invalid FCM token

    @Test
    fun `a token the server reports invalid is renewed once per process`() {
        val renewal = InvalidTokenRenewal()
        val withToken = sample().copy(push = PushTokenDto("fcm", "old-token"))
        val invalid = InstallationResponse(installationId = withToken.installationId, pushStatus = "invalid")

        assertFalse("active", renewal.shouldRenew(withToken, invalid.copy(pushStatus = "active")))
        assertFalse("replaced by another install", renewal.shouldRenew(withToken, invalid.copy(pushStatus = "replaced")))
        assertFalse("no token was sent", renewal.shouldRenew(sample(), invalid))
        assertTrue(renewal.shouldRenew(withToken, invalid))
        assertFalse(
            "a new token refused as well does not loop",
            renewal.shouldRenew(withToken.copy(push = PushTokenDto("fcm", "new-token")), invalid)
        )
    }

    @Test
    fun `the registrar hands the answer for the token it sent to its listener`() = runBlocking {
        request = sample().copy(push = PushTokenDto("fcm", "old-token"))
        api.answer = { sent, _ -> InstallationResponse(installationId = sent.installationId, pushStatus = "invalid") }
        val renewal = InvalidTokenRenewal()
        val renewed = mutableListOf<String?>()
        val registrar = InstallationRegistrar(
            api = api,
            session = SimpleSession(signedIn = true),
            store = store,
            snapshot = { request },
            isEnabled = { true },
            scope = idle,
            clock = { now },
            listener = { sent, response -> if (renewal.shouldRenew(sent, response)) renewed += sent.push?.token }
        )

        assertTrue(registrar.syncOnce() is Outcome.Synced)
        assertEquals(listOf<String?>("old-token"), renewed)
    }

    private fun sample() = InstallationRequest(
        installationId = "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d",
        platform = "android",
        appVersion = "1.0",
        appBuild = "1",
        osName = "Android",
        osVersion = "15",
        deviceModel = "SM-S928B",
        manufacturer = "samsung",
        locale = "kk",
        timezone = "Asia/Almaty",
        notificationPermission = "not_determined",
        notificationsEnabled = false,
        push = null
    )

    private companion object {
        const val HOUR = 60L * 60 * 1000
    }
}
