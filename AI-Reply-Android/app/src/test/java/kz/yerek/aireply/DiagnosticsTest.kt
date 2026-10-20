package kz.yerek.aireply

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.ApiException
import kz.yerek.aireply.data.settings.DeviceStateStore
import kz.yerek.aireply.keyboard.input.KeyboardStatus
import kz.yerek.aireply.push.DiagnosticsReport
import kz.yerek.aireply.push.DiagnosticsReporter
import kz.yerek.aireply.push.HttpPushApi
import kz.yerek.aireply.push.PermissionDiagnostics
import kz.yerek.aireply.support.FakeCredentials
import kz.yerek.aireply.support.FakeResponse
import kz.yerek.aireply.support.FakeServer
import kz.yerek.aireply.support.sessionOn
import kz.yerek.aireply.ui.feature.onboarding.OnboardingStatus
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Permission diagnostics (contract §3.4): the statuses Android allows, sent
 * only when they changed or a day passed, never any text.
 *
 * Рұқсат диагностикасы: өзгергенде не тәулігіне бір рет, мәтінсіз.
 */
class DiagnosticsTest {

    private val enabledSelected = KeyboardStatus.Snapshot(isEnabled = true, isSelected = true)
    private val checkedAt = 1_792_490_400_000L // 2026-10-20T10:00:00Z
    private val day = PermissionDiagnostics.RESEND_INTERVAL_MS

    @Test
    fun `android's facts become the contract's statuses`() {
        val ready = PermissionDiagnostics.report(enabledSelected, lastPasteAt = null, OnboardingStatus.IN_PROGRESS, checkedAt)
        assertEquals(
            DiagnosticsReport(
                keyboardSetupStatus = "verified_enabled",
                keyboardSelectedStatus = "verified_enabled",
                fullAccessStatus = "unsupported",
                clipboardStatus = "unknown",
                onboardingStatus = "in_progress",
                statusSource = "app_check",
                lastSuccessfulPasteAt = null,
                lastKeyboardCheckAt = "2026-10-20T10:00:00Z"
            ),
            ready
        )

        val off = PermissionDiagnostics.report(KeyboardStatus.Snapshot(isEnabled = false, isSelected = false), null, null, checkedAt)
        assertEquals("observed_denied", off.keyboardSetupStatus)
        assertEquals("observed_denied", off.keyboardSelectedStatus)
        assertEquals("unknown", off.onboardingStatus)

        val pasted = PermissionDiagnostics.report(enabledSelected, checkedAt - 3_600_000L + 123, OnboardingStatus.COMPLETED, checkedAt)
        assertEquals("a paste that worked is the only evidence", "verified_enabled", pasted.clipboardStatus)
        assertEquals("2026-10-20T09:00:00Z", pasted.lastSuccessfulPasteAt)
        assertEquals("completed", pasted.onboardingStatus)
    }

    @Test
    fun `the body has the documented fields and nothing else`() {
        val body = HttpPushApi.json.encodeToString(
            DiagnosticsReport.serializer(),
            PermissionDiagnostics.report(enabledSelected, null, OnboardingStatus.SKIPPED, checkedAt)
        )
        val keys = Json.parseToJsonElement(body).jsonObject.keys
        val documented = setOf(
            "keyboard_setup_status", "keyboard_selected_status", "full_access_status", "clipboard_status",
            "onboarding_status", "status_source", "last_successful_paste_at", "last_keyboard_check_at"
        )
        assertTrue(keys.toString(), documented.containsAll(keys))
        assertTrue("required ones are there", keys.containsAll(documented - "last_successful_paste_at"))
    }

    @Test
    fun `only a change or a day makes a report due`() {
        val first = PermissionDiagnostics.report(enabledSelected, null, OnboardingStatus.COMPLETED, checkedAt)
        val later = PermissionDiagnostics.report(enabledSelected, null, OnboardingStatus.COMPLETED, checkedAt + 60_000)
        val print = PermissionDiagnostics.fingerprint(first)
        assertEquals("a new check time alone is no change", print, PermissionDiagnostics.fingerprint(later))

        assertTrue("never reported", PermissionDiagnostics.isDue(print, null, 0, checkedAt))
        assertFalse(PermissionDiagnostics.isDue(print, print, checkedAt, checkedAt + day - 1))
        assertTrue("a day later", PermissionDiagnostics.isDue(print, print, checkedAt, checkedAt + day))
        assertTrue("a clock that went back", PermissionDiagnostics.isDue(print, print, checkedAt, checkedAt - 1))

        val unselected = PermissionDiagnostics.report(KeyboardStatus.Snapshot(true, false), null, OnboardingStatus.COMPLETED, checkedAt)
        assertTrue(PermissionDiagnostics.isDue(PermissionDiagnostics.fingerprint(unselected), print, checkedAt, checkedAt + 1))
        val pasted = PermissionDiagnostics.report(enabledSelected, checkedAt, OnboardingStatus.COMPLETED, checkedAt)
        assertTrue("the first paste is news", PermissionDiagnostics.isDue(PermissionDiagnostics.fingerprint(pasted), print, checkedAt, checkedAt + 1))
    }

    private class Harness {
        val store = DeviceStateStore(InMemoryPreferences())
        var enabled = true
        var now = 1_792_490_400_000L
        var keyboard = KeyboardStatus.Snapshot(isEnabled = true, isSelected = false)
        val sent = mutableListOf<Pair<DiagnosticsReport, String?>>()
        var failWith: ApiException? = null
        val credentials = FakeCredentials("access-1", "refresh-1")
        val server = FakeServer { FakeResponse(500) }

        val reporter = DiagnosticsReporter(
            api = { _, report, token ->
                failWith?.let { throw it }
                sent += report to token
            },
            session = sessionOn(server, credentials),
            store = store,
            installationId = { "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d" },
            isEnabled = { enabled },
            snapshot = { PermissionDiagnostics.report(keyboard, store.lastSuccessfulPasteAt, OnboardingStatus.COMPLETED, now) },
            scope = CoroutineScope(Dispatchers.Unconfined),
            clock = { now }
        )
    }

    @Test
    fun `the reporter sends on a change or after a day, and nothing while switched off`() = runBlocking {
        val h = Harness()
        h.enabled = false
        assertEquals(DiagnosticsReporter.Outcome.DISABLED, h.reporter.reportIfDue())
        h.reporter.onResume()
        assertTrue(h.sent.isEmpty())

        h.enabled = true
        assertEquals(DiagnosticsReporter.Outcome.SENT, h.reporter.reportIfDue())
        assertEquals("signed in: with the account's token", "access-1", h.sent.single().second)

        h.now += 60_000
        assertEquals(DiagnosticsReporter.Outcome.UP_TO_DATE, h.reporter.reportIfDue())

        h.keyboard = KeyboardStatus.Snapshot(isEnabled = true, isSelected = true)
        assertEquals(DiagnosticsReporter.Outcome.SENT, h.reporter.reportIfDue())
        assertEquals("verified_enabled", h.sent.last().first.keyboardSelectedStatus)

        h.now += PermissionDiagnostics.RESEND_INTERVAL_MS
        assertEquals("a day later, unchanged", DiagnosticsReporter.Outcome.SENT, h.reporter.reportIfDue())
        assertEquals(3, h.sent.size)
    }

    @Test
    fun `a failed report is tried on the next return, a refused one after a day`() = runBlocking {
        val h = Harness()
        h.failWith = ApiException(ApiError.NotFound, 404)
        assertEquals(DiagnosticsReporter.Outcome.FAILED, h.reporter.reportIfDue())
        h.failWith = null
        assertEquals("not recorded: sent on the next return", DiagnosticsReporter.Outcome.SENT, h.reporter.reportIfDue())

        h.keyboard = KeyboardStatus.Snapshot(isEnabled = false, isSelected = false)
        h.failWith = ApiException(ApiError.InvalidRequest, 400)
        assertEquals(DiagnosticsReporter.Outcome.FAILED, h.reporter.reportIfDue())
        h.failWith = null
        assertEquals("the same body would be refused again", DiagnosticsReporter.Outcome.UP_TO_DATE, h.reporter.reportIfDue())
    }

    @Test
    fun `signed out it goes without a token`() = runBlocking {
        val h = Harness()
        h.credentials.refresh = null
        h.credentials.access = null
        h.reporter.reportIfDue()
        assertNull(h.sent.single().second)
    }

    @Test
    fun `the report is a PUT on the installation`() = runBlocking {
        val server = FakeServer { FakeResponse(200, "{}") }
        val api = HttpPushApi(
            client = { ApiClient("https://example.test", openConnection = server::open) },
            session = sessionOn(server, FakeCredentials("a", "r"))
        )
        api.putDiagnostics(
            "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d",
            PermissionDiagnostics.report(enabledSelected, null, OnboardingStatus.NOT_STARTED, checkedAt),
            "a"
        )
        val request = server.requests.single()
        assertEquals("PUT", request.method)
        assertEquals("/api/v1/installations/0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d/diagnostics", request.path)
        assertEquals("Bearer a", request.header("Authorization"))
        val body = Json.parseToJsonElement(request.body).jsonObject
        assertEquals("not_started", body["onboarding_status"]!!.jsonPrimitive.content)
        assertTrue("no paste yet: absent, never a made-up time", body["last_successful_paste_at"] == null || body["last_successful_paste_at"] == JsonNull)
    }
}
