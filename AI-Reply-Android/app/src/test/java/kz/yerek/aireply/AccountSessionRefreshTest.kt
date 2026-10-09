package kz.yerek.aireply

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withContext
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.ApiException
import kz.yerek.aireply.support.FakeCredentials
import kz.yerek.aireply.support.FakeResponse
import kz.yerek.aireply.support.FakeServer
import kz.yerek.aireply.support.sessionOn
import kz.yerek.aireply.support.tokenPair
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * A 401 on a token the app believed fresh: refresh once, retry once, with the
 * NEW token.
 *
 * 401-ден кейін жаңа токенмен бір рет қайталау — ескі токен қайтарылмайды.
 *
 * The bug this guards: refreshAccessToken() saw the rejected token still
 * "fresh" by its expiry and handed it straight back, so the retry failed with
 * the same 401 and the refresh never happened.
 */
class AccountSessionRefreshTest {

    private fun meServer(onRefresh: () -> FakeResponse = { tokenPair("new", "r2") }) = FakeServer { request ->
        when (request.path) {
            "/api/v1/auth/refresh" -> onRefresh()
            "/api/v1/me" ->
                if (request.header("Authorization") == "Bearer new") FakeResponse(200, "{}")
                else FakeResponse(401, FakeServer.envelope("UNAUTHORIZED"))
            else -> FakeResponse(404, FakeServer.envelope("NOT_FOUND"))
        }
    }

    @Test
    fun `a rejected token that still looks fresh is refreshed, and the retry uses the new one`() = runBlocking {
        val server = meServer()
        val credentials = FakeCredentials(access = "old", refresh = "r1", fresh = true)
        val session = sessionOn(server, credentials)
        val client = ApiClient("https://example.test", openConnection = server::open)

        val body = session.authenticated { token -> client.request("GET", "api/v1/me", token = token) }

        assertEquals("{}", body)
        assertEquals("one refresh", 1, server.count("/api/v1/auth/refresh"))
        assertEquals(
            listOf("Bearer old", "Bearer new"),
            server.requests.filter { it.path == "/api/v1/me" }.map { it.header("Authorization") }
        )
        assertEquals("new", credentials.access)
        assertEquals("the rotated refresh token is kept", "r2", credentials.refresh)
    }

    @Test
    fun `a token another caller already refreshed is reused, not rotated again`() = runBlocking {
        val server = meServer()
        val credentials = FakeCredentials(access = "new", refresh = "r2", fresh = true)

        val token = sessionOn(server, credentials).refreshAccessToken(rejected = "old")

        assertEquals("new", token)
        assertEquals(0, server.count("/api/v1/auth/refresh"))
    }

    @Test
    fun `parallel 401s share one refresh`() = runBlocking {
        val server = meServer()
        val credentials = FakeCredentials(access = "old", refresh = "r1", fresh = true)
        val session = sessionOn(server, credentials)

        val tokens = List(5) { async { session.refreshAccessToken(rejected = "old") } }.awaitAll()

        assertEquals(List(5) { "new" }, tokens)
        assertEquals("the refresh token is rotated exactly once", 1, server.count("/api/v1/auth/refresh"))
    }

    @Test
    fun `an expired token is refreshed before use`() = runBlocking {
        val server = meServer()
        val credentials = FakeCredentials(access = "stale", refresh = "r1", fresh = false)

        assertEquals("new", sessionOn(server, credentials).accessToken())
        assertEquals(1, server.count("/api/v1/auth/refresh"))
    }

    @Test
    fun `a refresh token the server refuses ends the session`() = runBlocking {
        val server = meServer(onRefresh = { FakeResponse(401, FakeServer.envelope("UNAUTHORIZED")) })
        val credentials = FakeCredentials(access = "old", refresh = "r1", fresh = true)

        try {
            sessionOn(server, credentials).authenticated { token ->
                if (token == "old") throw ApiException(ApiError.Unauthorized, httpStatus = 401)
                token
            }
            fail("expected the session to end")
        } catch (expected: ApiException) {
            assertEquals(ApiError.Unauthorized, expected.error)
        }
        assertNull(credentials.refresh)
        assertEquals(1, credentials.clears)
    }

    // ------------------------------------------------- sign-out vs refresh

    @Test
    fun `sign-out waits for a refresh in flight and logs out the rotated token`() = runBlocking {
        val onTheWire = CountDownLatch(1)
        val release = CountDownLatch(1)
        val server = FakeServer { request ->
            when (request.path) {
                "/api/v1/auth/refresh" -> {
                    onTheWire.countDown()
                    release.await(5, TimeUnit.SECONDS)
                    tokenPair("a2", "r2")
                }
                "/api/v1/auth/logout" -> FakeResponse(200, """{"ok":true}""")
                else -> FakeResponse(404)
            }
        }
        val credentials = FakeCredentials(access = "a1", refresh = "r1", fresh = false)
        val session = sessionOn(server, credentials)

        val refreshing = async(Dispatchers.Default) { runCatching { session.accessToken() } }
        withContext(Dispatchers.IO) { onTheWire.await(5, TimeUnit.SECONDS) }
        val signingOut = async(Dispatchers.Default) { session.signOut() }
        delay(200)
        assertFalse("sign-out waits for the refresh lock", signingOut.isCompleted)

        release.countDown()
        signingOut.await()
        refreshing.await()

        assertNull("nothing signed the app back in", credentials.refresh)
        assertFalse(session.isSignedIn)
        val logout = server.requests.single { it.path == "/api/v1/auth/logout" }
        assertTrue("the logout names the rotated token: ${logout.body}", logout.body.contains("\"r2\""))
    }

    @Test
    fun `a refresh that returns after the session was cleared stores nothing`() = runBlocking {
        val onTheWire = CountDownLatch(1)
        val release = CountDownLatch(1)
        val server = FakeServer {
            onTheWire.countDown()
            release.await(5, TimeUnit.SECONDS)
            tokenPair("a2", "r2")
        }
        val credentials = FakeCredentials(access = "a1", refresh = "r1", fresh = false)
        val session = sessionOn(server, credentials)

        val result = async(Dispatchers.Default) { runCatching { session.accessToken() } }
        withContext(Dispatchers.IO) { onTheWire.await(5, TimeUnit.SECONDS) }
        credentials.clear() // signed out elsewhere, without the lock
        release.countDown()

        val failure = result.await().exceptionOrNull() as ApiException
        assertEquals(ApiError.Unauthorized, failure.error)
        assertNull(credentials.refresh)
        assertEquals(0, credentials.stores)
    }

    @Test
    fun `a refused refresh does not clear a session stored meanwhile`() = runBlocking {
        val onTheWire = CountDownLatch(1)
        val release = CountDownLatch(1)
        val server = FakeServer {
            onTheWire.countDown()
            release.await(5, TimeUnit.SECONDS)
            FakeResponse(401, FakeServer.envelope("UNAUTHORIZED"))
        }
        val credentials = FakeCredentials(access = "a1", refresh = "r1", fresh = false)
        val session = sessionOn(server, credentials)

        val result = async(Dispatchers.Default) { runCatching { session.accessToken() } }
        withContext(Dispatchers.IO) { onTheWire.await(5, TimeUnit.SECONDS) }
        credentials.store("b1", "rb1", 900) // another account signed in meanwhile
        release.countDown()

        assertTrue(result.await().isFailure)
        assertEquals("the new account's session is kept", "rb1", credentials.refresh)
        assertEquals(0, credentials.clears)
    }

    // ------------------------------------------------- the logout header

    @Test
    fun `the logout alone names the installation, so the server detaches it`() = runBlocking {
        val server = FakeServer { request ->
            when (request.path) {
                "/api/v1/auth/refresh" -> tokenPair("a2", "r2")
                "/api/v1/auth/logout" -> FakeResponse(200, """{"ok":true}""")
                else -> FakeResponse(404)
            }
        }
        val credentials = FakeCredentials(access = "a1", refresh = "r1", fresh = false)
        val session = sessionOn(server, credentials, installationId = { INSTALLATION })

        session.accessToken()
        session.signOut()

        val refresh = server.requests.single { it.path == "/api/v1/auth/refresh" }
        assertNull("no other request carries it", refresh.header("X-Installation-ID"))
        val logout = server.requests.single { it.path == "/api/v1/auth/logout" }
        assertEquals(INSTALLATION, logout.header("X-Installation-ID"))
        assertTrue(logout.body.contains("\"r2\""))
        assertFalse(session.isSignedIn)
    }

    @Test
    fun `before the terms are accepted the logout names no installation`() = runBlocking {
        val server = FakeServer { FakeResponse(200, """{"ok":true}""") }
        val credentials = FakeCredentials(access = "a1", refresh = "r1")

        sessionOn(server, credentials, installationId = { null }).signOut()

        assertNull(server.requests.single().header("X-Installation-ID"))
    }

    @Test
    fun `a logout that never arrives still signs out here`() = runBlocking {
        val server = FakeServer { throw java.io.IOException("offline") }
        val credentials = FakeCredentials(access = "a1", refresh = "r1")
        val session = sessionOn(server, credentials, installationId = { INSTALLATION })

        session.signOut()

        assertFalse(session.isSignedIn)
        assertEquals(1, credentials.clears)
    }

    private companion object {
        const val INSTALLATION = "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d"
    }
}
