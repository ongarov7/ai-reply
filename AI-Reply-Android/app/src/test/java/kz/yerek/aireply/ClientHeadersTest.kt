package kz.yerek.aireply

import kotlinx.coroutines.runBlocking
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.ApiException
import kz.yerek.aireply.data.account.ApiRoutes
import kz.yerek.aireply.data.account.HeaderScope
import kz.yerek.aireply.data.account.RequestIds
import kz.yerek.aireply.data.account.RequestMetadata
import kz.yerek.aireply.data.account.TransportFailure
import kz.yerek.aireply.data.account.TransportFailureReport
import kz.yerek.aireply.data.account.diagnosticCode
import kz.yerek.aireply.push.ClientContext
import kz.yerek.aireply.push.InstallationIdStore
import kz.yerek.aireply.support.FakeResponse
import kz.yerek.aireply.support.FakeServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File
import java.net.SocketTimeoutException

/**
 * The metadata every request carries, the request id, and the installation id.
 *
 * Әр сұраныстың метадерегі: бір жерде қосылады, пернетақтада — анонимді бөлігі ғана.
 */
class ClientHeadersTest {

    @get:Rule
    val folder = TemporaryFolder()

    @After
    fun resetObserver() {
        RequestMetadata.transportFailureObserver = null
    }

    private fun context(directory: File = folder.root, session: String? = "5e55a0b1-0000-4000-8000-000000000001") =
        ClientContext(
            installationIds = InstallationIdStore { directory },
            appVersion = "1.3.2",
            appBuild = "142",
            osVersion = "15",
            manufacturer = "samsung",
            deviceModel = "SM-S928B",
            language = { "kk" },
            timeZone = { "Asia/Almaty" },
            sessionId = { session }
        )

    private val ok = FakeServer { FakeResponse(200, "{}") }

    private fun client(server: FakeServer, metadata: RequestMetadata, scope: HeaderScope = HeaderScope.APP) =
        ApiClient("https://example.test", scope = scope, metadata = metadata, openConnection = server::open)

    // -------------------------------------------------------------- headers

    @Test
    fun `every app request carries the metadata headers and a fresh request id`() = runBlocking {
        val context = context()
        val client = client(ok, context)
        client.request("GET", "api/v1/me", token = "t")
        client.request("POST", "api/v1/installations", "{}")

        val (first, second) = ok.requests
        assertEquals("android", first.header("X-Platform"))
        assertEquals("1.3.2", first.header("X-App-Version"))
        assertEquals("142", first.header("X-App-Build"))
        assertEquals("15", first.header("X-OS-Version"))
        assertEquals(context.installationId, first.header("X-Installation-ID"))
        assertEquals("5e55a0b1-0000-4000-8000-000000000001", first.header("X-Session-ID"))
        assertEquals("Bearer t", first.header("Authorization"))
        assertNull("no token, no Authorization", second.header("Authorization"))

        val ids = listOf(first, second).map { it.header("X-Request-ID")!! }
        ids.forEach { assertTrue(it, Regex("^req_[a-z0-9]{16}$").matches(it)) }
        assertNotEquals("one id per request", ids[0], ids[1])
    }

    @Test
    fun `keyboard requests carry nothing that ties them to the installation or a session`() = runBlocking {
        client(ok, context(), HeaderScope.KEYBOARD).request("POST", "api/v1/ai/reply", "{}")

        val request = ok.requests.single()
        assertEquals("android", request.header("X-Platform"))
        assertEquals("1.3.2", request.header("X-App-Version"))
        assertTrue(request.header("X-Request-ID")!!.startsWith("req_"))
        assertNull(request.header("X-Installation-ID"))
        assertNull(request.header("X-Session-ID"))
    }

    @Test
    fun `no session yet means no session header`() = runBlocking {
        client(ok, context(session = null)).request("GET", "api/v1/config")
        assertNull(ok.requests.single().header("X-Session-ID"))
    }

    @Test
    fun `values a header cannot carry are dropped, not sent`() {
        assertEquals("1.3.2", ClientContext.headerSafe(" 1.3.2 "))
        assertEquals(
            "CR, LF and the colon go: no header can be injected",
            "15 (beta)X-Evil 1",
            ClientContext.headerSafe("15 (beta)\r\nX-Evil: 1")
        )
        assertEquals("", ClientContext.headerSafe("Ω"))
        assertEquals(32, ClientContext.headerSafe("9".repeat(80)).length)

        val headers = ClientContext(
            InstallationIdStore { folder.root }, appVersion = "Ω", appBuild = "1", osVersion = "14",
            manufacturer = "x", deviceModel = "y", language = { "en" }
        ).headers(HeaderScope.APP)
        assertFalse("an empty version is left out", headers.containsKey("X-App-Version"))
    }

    @Test
    fun `request ids have the documented shape and do not repeat`() {
        val ids = List(2_000) { RequestIds.next() }
        assertEquals(ids.size, ids.toSet().size)
        ids.forEach {
            assertTrue(it, Regex("^req_[a-z0-9]{16}$").matches(it))
            assertTrue(RequestIds.isValid(it))
        }
    }

    // --------------------------------------------------------------- errors

    @Test
    fun `an error carries the server's request id and status`() = runBlocking {
        val server = FakeServer { request ->
            when (request.path) {
                "/api/v1/enveloped" -> FakeResponse(429, FakeServer.envelope("RATE_LIMITED", "req_server00000001"))
                "/api/v1/header" -> FakeResponse(503, "<html>", mapOf("X-Request-ID" to "req_fromheader0001"))
                else -> FakeResponse(500, "")
            }
        }
        val client = client(server, RequestMetadata.NONE)

        val enveloped = failure { client.request("GET", "api/v1/enveloped") }
        assertEquals(ApiError.RateLimited(null), enveloped.error)
        assertEquals("req_server00000001", enveloped.requestId)
        assertEquals(429, enveloped.httpStatus)
        assertEquals("rate_limited", enveloped.diagnosticCode())

        val header = failure { client.request("GET", "api/v1/header") }
        assertEquals("req_fromheader0001", header.requestId)
        assertEquals(503, header.httpStatus)

        val bare = failure { client.request("GET", "api/v1/bare") }
        assertEquals("the id this client sent", server.requests.last().header("X-Request-ID"), bare.requestId)
        assertNull(bare.transport)
    }

    @Test
    fun `a request that got no answer is reported with its route, never the keyboard's`() = runBlocking {
        val reports = mutableListOf<TransportFailureReport>()
        RequestMetadata.transportFailureObserver = { reports += it }
        val server = FakeServer { throw SocketTimeoutException("slow") }

        val app = failure { client(server, RequestMetadata.NONE).request("POST", "api/v1/payments/pay_7f3a9c/confirm", "{}") }
        assertEquals(ApiError.TimedOut, app.error)
        assertEquals(0, app.httpStatus)
        assertEquals(TransportFailure.TIMEOUT, app.transport)
        assertEquals("timeout", app.diagnosticCode())

        val report = reports.single()
        assertEquals("/api/v1/payments/{id}/confirm", report.route)
        assertEquals(TransportFailure.TIMEOUT, report.failure)
        assertEquals(app.requestId, report.requestId)

        failure { client(server, RequestMetadata.NONE, HeaderScope.KEYBOARD).request("POST", "api/v1/ai/reply", "{}") }
        assertEquals("the keyboard reports nothing", 1, reports.size)
    }

    @Test
    fun `routes keep their words and lose their ids`() {
        assertEquals("/api/v1/me", ApiRoutes.pattern("api/v1/me"))
        assertEquals("/api/v1/me/notification-preferences", ApiRoutes.pattern("/api/v1/me/notification-preferences"))
        assertEquals(
            "/api/v1/installations/{id}/detach",
            ApiRoutes.pattern("api/v1/installations/0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d/detach")
        )
        assertEquals("/api/v1/config", ApiRoutes.pattern("api/v1/config?lang=kk"))
    }

    private suspend fun failure(block: suspend () -> Unit): ApiException {
        try {
            block()
        } catch (exception: ApiException) {
            return exception
        }
        fail("expected an ApiException")
        throw AssertionError()
    }

    // --------------------------------------------------------- installation id

    @Test
    fun `the installation id is made once and kept`() {
        val first = InstallationIdStore { folder.root }.id()
        assertTrue(first, Regex("^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$").matches(first))
        assertTrue(InstallationIdStore.isValid(first))
        assertEquals("the same process", first, InstallationIdStore { folder.root }.id())
        assertEquals("the file is the source", first, File(folder.root, InstallationIdStore.FILE_NAME).readText())
    }

    @Test
    fun `a reinstall or a restore gets a new installation id`() {
        val original = InstallationIdStore { folder.newFolder("first") }.id()
        val restored = InstallationIdStore { folder.newFolder("second") }.id()
        assertNotEquals(original, restored)
    }

    @Test
    fun `a damaged id file is replaced by a valid id`() {
        val directory = folder.newFolder("damaged")
        File(directory, InstallationIdStore.FILE_NAME).writeText("not an id; rm -rf")
        val id = InstallationIdStore { directory }.id()
        assertTrue(InstallationIdStore.isValid(id))
        assertEquals(id, InstallationIdStore { directory }.id())
    }

    @Test
    fun `an unwritable directory still gives one stable id for the process`() {
        val missing = File(folder.newFile("plain-file"), "cannot-be-a-directory")
        val store = InstallationIdStore { missing }
        val id = store.id()
        assertTrue(InstallationIdStore.isValid(id))
        assertEquals(id, store.id())
    }
}
