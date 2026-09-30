package kz.yerek.aireply

import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.runBlocking
import kz.yerek.aireply.data.account.AccountSession
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.ApiException
import kz.yerek.aireply.data.account.DeviceDescriptor
import kz.yerek.aireply.data.account.HeaderScope
import kz.yerek.aireply.data.account.RequestMetadata
import kz.yerek.aireply.support.FakeCredentials
import kz.yerek.aireply.support.FakeResponse
import kz.yerek.aireply.support.FakeServer
import kz.yerek.aireply.support.sessionOn
import kz.yerek.aireply.support.tokenPair
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.fail
import org.junit.Test

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
        val client = ApiClient(
            "https://example.test",
            metadata = RequestMetadata.NONE,
            openConnection = server::open
        )

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

    @Test
    fun `the refresh request carries no installation or session id`() = runBlocking {
        val server = meServer()
        val credentials = FakeCredentials(access = "stale", refresh = "r1", fresh = false)
        RequestMetadata.installed =
            RequestMetadata { scope ->
                if (scope == HeaderScope.APP) {
                    mapOf("X-Installation-ID" to "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d")
                } else {
                    mapOf("X-Platform" to "android")
                }
            }
        try {
            val session = AccountSession(
                credentials = credentials,
                baseUrlProvider = { "https://example.test" },
                deviceDescriptor = {
                    DeviceDescriptor("d", "android", "1.0", "15", "en", "UTC")
                },
                clientFactory = { baseUrl, scope ->
                    ApiClient(baseUrl, scope = scope, openConnection = server::open)
                }
            )
            session.accessToken()
            val refresh = server.requests.single { it.path == "/api/v1/auth/refresh" }
            assertEquals("android", refresh.header("X-Platform"))
            assertNull(refresh.header("X-Installation-ID"))
        } finally {
            RequestMetadata.installed =
                RequestMetadata.NONE
        }
    }
}
