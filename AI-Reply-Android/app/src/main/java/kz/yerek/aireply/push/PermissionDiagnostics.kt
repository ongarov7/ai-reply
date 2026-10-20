package kz.yerek.aireply.push

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kz.yerek.aireply.data.account.ApiException
import kz.yerek.aireply.data.account.SessionAuth
import kz.yerek.aireply.data.settings.DeviceStateStore
import kz.yerek.aireply.keyboard.input.KeyboardStatus
import kz.yerek.aireply.platform.ReplyLog
import kz.yerek.aireply.ui.feature.onboarding.OnboardingStatus
import java.time.Instant
import java.time.temporal.ChronoUnit
import java.util.concurrent.atomic.AtomicBoolean

/**
 * `PUT /api/v1/installations/{installation_id}/diagnostics`, field for field
 * (contract §3.4). Strict on the server: nothing is added that it does not
 * define. Enums and times only, never anything that was copied or typed.
 */
@Serializable
data class DiagnosticsReport(
    @SerialName("keyboard_setup_status") val keyboardSetupStatus: String,
    @SerialName("keyboard_selected_status") val keyboardSelectedStatus: String,
    @SerialName("full_access_status") val fullAccessStatus: String,
    @SerialName("clipboard_status") val clipboardStatus: String,
    @SerialName("onboarding_status") val onboardingStatus: String,
    @SerialName("status_source") val statusSource: String,
    /** RFC 3339 UTC, or absent before the first paste. */
    @SerialName("last_successful_paste_at") val lastSuccessfulPasteAt: String? = null,
    @SerialName("last_keyboard_check_at") val lastKeyboardCheckAt: String? = null
)

/**
 * What this phone has seen, as the server's status vocabulary.
 *
 * Рұқсат күйлері: пернетақта қосулы ма, таңдалған ба, қою жұмыс істеді ме.
 *
 * Android answers the keyboard questions itself, so both are facts:
 * `verified_enabled` or `observed_denied`. Android has no Full Access
 * (`unsupported`). Paste has no system answer: `verified_enabled` once a
 * paste the user asked for worked here, `unknown` until then - never
 * "denied".
 */
object PermissionDiagnostics {

    const val VERIFIED_ENABLED = "verified_enabled"
    const val OBSERVED_DENIED = "observed_denied"
    const val UNSUPPORTED = "unsupported"
    const val UNKNOWN = "unknown"
    const val SOURCE_APP_CHECK = "app_check"

    /** Sent again unchanged after this long, so the server's evidence stays fresh. */
    const val RESEND_INTERVAL_MS = 24L * 60 * 60 * 1000

    fun report(
        keyboard: KeyboardStatus.Snapshot,
        lastPasteAt: Long?,
        onboarding: OnboardingStatus?,
        checkedAt: Long
    ): DiagnosticsReport = DiagnosticsReport(
        keyboardSetupStatus = if (keyboard.isEnabled) VERIFIED_ENABLED else OBSERVED_DENIED,
        keyboardSelectedStatus = if (keyboard.isSelected) VERIFIED_ENABLED else OBSERVED_DENIED,
        fullAccessStatus = UNSUPPORTED,
        clipboardStatus = if (lastPasteAt != null) VERIFIED_ENABLED else UNKNOWN,
        onboardingStatus = onboarding?.wire ?: UNKNOWN,
        statusSource = SOURCE_APP_CHECK,
        lastSuccessfulPasteAt = lastPasteAt?.let(::rfc3339),
        lastKeyboardCheckAt = rfc3339(checkedAt)
    )

    /**
     * What counts as a change: the statuses and the day of the last paste.
     * The check time is new on every return and is not one.
     */
    fun fingerprint(report: DiagnosticsReport): String = listOf(
        report.keyboardSetupStatus,
        report.keyboardSelectedStatus,
        report.fullAccessStatus,
        report.clipboardStatus,
        report.onboardingStatus,
        report.lastSuccessfulPasteAt?.take(10).orEmpty()
    ).joinToString("|")

    /** Changed since the last report, or that one is a day old (or from a clock that went back). */
    fun isDue(fingerprint: String, lastFingerprint: String?, lastAt: Long, now: Long): Boolean =
        fingerprint != lastFingerprint || now - lastAt !in 0 until RESEND_INTERVAL_MS

    /** To the second, in UTC: `2026-10-20T10:00:00Z`. */
    fun rfc3339(epochMillis: Long): String = Instant.ofEpochMilli(epochMillis).truncatedTo(ChronoUnit.SECONDS).toString()
}

/** Sends one report. Throws [ApiException]. */
fun interface DiagnosticsApi {
    /** [token] null sends it anonymously, which the server accepts only for an anonymous installation. */
    suspend fun put(installationId: String, report: DiagnosticsReport, token: String?)
}

/**
 * Reports the permission diagnostics when the app comes back to the front.
 *
 * Only with `features.permission_diagnostics`, the terms accepted and the
 * installation registered ([isEnabled]), and only when something changed or
 * the last report is a day old. A failure is tried again on the next return;
 * nothing here blocks the UI or throws into it.
 */
class DiagnosticsReporter(
    private val api: DiagnosticsApi,
    private val session: SessionAuth,
    private val store: DeviceStateStore,
    private val installationId: () -> String,
    private val isEnabled: () -> Boolean,
    /** The report as it would be sent now. */
    private val snapshot: () -> DiagnosticsReport,
    private val scope: CoroutineScope,
    private val clock: () -> Long = System::currentTimeMillis
) {

    enum class Outcome { DISABLED, UP_TO_DATE, SENT, FAILED }

    private val running = AtomicBoolean(false)

    fun onResume() {
        if (!isEnabled()) return
        scope.launch { reportIfDue() }
    }

    suspend fun reportIfDue(): Outcome {
        if (!isEnabled()) return Outcome.DISABLED
        if (!running.compareAndSet(false, true)) return Outcome.UP_TO_DATE
        try {
            val report = snapshot()
            val fingerprint = PermissionDiagnostics.fingerprint(report)
            val now = clock()
            if (!PermissionDiagnostics.isDue(fingerprint, store.diagnosticsFingerprint, store.diagnosticsReportedAt, now)) {
                return Outcome.UP_TO_DATE
            }
            val id = installationId()
            if (session.isSignedIn) {
                session.authenticated { token -> api.put(id, report, token) }
            } else {
                api.put(id, report, null)
            }
            store.noteDiagnosticsReported(fingerprint, now)
            return Outcome.SENT
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (failure: ApiException) {
            ReplyLog.warn(failure) { "diagnostics not reported (HTTP ${failure.httpStatus ?: "none"})" }
            // A body the server refuses would be refused again: wait a day. Anything else: next return.
            if (failure.httpStatus == 400) store.noteDiagnosticsReported(PermissionDiagnostics.fingerprint(snapshot()), clock())
            return Outcome.FAILED
        } catch (other: Exception) {
            ReplyLog.warn(other) { "diagnostics not reported" }
            return Outcome.FAILED
        } finally {
            running.set(false)
        }
    }
}
