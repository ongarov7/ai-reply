package kz.yerek.aireply.push

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.ApiException
import kz.yerek.aireply.data.account.SessionAuth
import java.security.MessageDigest

/**
 * Keeps the server's record of this installation current: metadata, the push
 * token, the notification permission, the in-app switch — and whose it is.
 *
 * Орнатуды серверде тіркеу: өзгергенде ғана, әйтпесе тәулігіне бір рет.
 *
 * WHEN. Every trigger (app start, sign-in, sign-out, a new token, a permission
 * or switch change, returning to the app) asks for a sync; a sync sends only
 * when the payload — or the signed-in account — differs from the last one the
 * server accepted, and otherwise at most once every [RESYNC_INTERVAL_MS].
 *
 * WHOSE. Signed in, the request carries the access token and the server
 * attaches the installation to that account; a 401 refreshes the token once
 * and retries once. Signed out, it goes without a token, which makes the
 * installation anonymous and detaches it from whoever used the phone before.
 * The account is never named in the body.
 *
 * FAILURES. Network trouble, 5xx, 401 and 429 retry with exponential backoff
 * (and never sooner than a Retry-After); any other 4xx means this exact payload
 * is wrong, so it is not sent again until something in it changes. Nothing
 * here blocks the UI, and nothing here can crash the app.
 */
class InstallationRegistrar(
    private val api: InstallationApi,
    private val session: SessionAuth,
    private val store: PushStateStore,
    /** The current state, as it would be sent now. */
    private val snapshot: () -> InstallationRequest,
    /**
     * `features.installations` and the user's legal consent: false on servers
     * without the endpoint, and before the terms are accepted nothing is sent.
     */
    private val isEnabled: () -> Boolean,
    private val scope: CoroutineScope,
    private val clock: () -> Long = System::currentTimeMillis,
    private val listener: Listener = object : Listener {}
) {

    interface Listener {
        fun onSynced(request: InstallationRequest, response: InstallationResponse) {}
        fun onFailed(request: InstallationRequest, failure: ApiException?) {}
    }

    sealed interface Outcome {
        /** The server has no installations endpoint (or the app does not know yet). */
        data object Disabled : Outcome
        data object UpToDate : Outcome
        data class Synced(val response: InstallationResponse) : Outcome
        /** A 4xx for this payload; it will not be retried until it changes. */
        data object Rejected : Outcome
        data class RetryLater(val afterMillis: Long) : Outcome
    }

    private val lock = Any()
    private var running = false
    private var rerun = false
    private var retryJob: Job? = null

    /**
     * Asks for a sync; returns at once. Calls while one is running are folded
     * into one more pass after it, and none is ever lost: the pass that
     * decides to stop clears `running` in the same locked step in which it
     * sees no further request, so a call either lands before that step (and
     * gets its pass) or after it (and starts a new one).
     */
    fun requestSync() {
        synchronized(lock) {
            if (running) {
                rerun = true
                return
            }
            running = true
            retryJob?.cancel()
            retryJob = null
        }
        val job = scope.launch { runPasses() }
        // Only a job that never ran or ended by an exception still holds
        // `running` here; a normal end released it inside runPasses().
        job.invokeOnCompletion { cause ->
            if (cause == null) return@invokeOnCompletion
            val again = synchronized(lock) {
                running = false
                rerun.also { rerun = false }
            }
            if (again && cause !is CancellationException) requestSync()
        }
    }

    private suspend fun runPasses() {
        while (true) {
            synchronized(lock) { rerun = false }
            val signedIn = session.isSignedIn
            val outcome = runCatching { syncOnce() }.getOrElse { failure ->
                if (failure is CancellationException) throw failure
                Outcome.RetryLater(backoffMillis(1))
            }
            if (outcome is Outcome.RetryLater) scheduleRetry(outcome.afterMillis)
            // Signed out in the meantime (a refresh token the server revoked):
            // register anonymously straight away.
            val signedOutMeanwhile = session.isSignedIn != signedIn
            val another = synchronized(lock) {
                if (signedOutMeanwhile) rerun = true
                if (!rerun) running = false
                rerun
            }
            if (!another) return
        }
    }

    private fun scheduleRetry(afterMillis: Long) {
        synchronized(lock) {
            retryJob?.cancel()
            retryJob = scope.launch {
                delay(afterMillis)
                requestSync()
            }
        }
    }

    /** One attempt, if one is due. The unit tests drive this directly. */
    suspend fun syncOnce(): Outcome {
        if (!isEnabled()) return Outcome.Disabled
        val request = snapshot()
        val signedIn = session.isSignedIn
        val fingerprint = fingerprint(request, if (signedIn) store.accountUserId ?: SIGNED_IN else ANONYMOUS)
        val now = clock()

        if (store.lastSyncFingerprint == fingerprint && now - store.lastSyncAt in 0 until RESYNC_INTERVAL_MS) {
            return Outcome.UpToDate
        }
        if (store.rejectedFingerprint == fingerprint && now - store.rejectedAt in 0 until RESYNC_INTERVAL_MS) {
            return Outcome.Rejected
        }
        // A wait longer than any backoff was stored while the clock was far
        // ahead; it must not block retries until that date.
        val wait = store.nextAttemptAt - now
        if (store.failedFingerprint == fingerprint && wait > 0 && wait <= MAX_RETRY_MS) {
            return Outcome.RetryLater(wait)
        }

        return try {
            val response = if (signedIn) {
                session.authenticated { token -> api.register(request, token) }
            } else {
                api.register(request, null)
            }
            store.recordSuccess(fingerprint, now)
            listener.onSynced(request, response)
            Outcome.Synced(response)
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (failure: ApiException) {
            listener.onFailed(request, failure)
            if (isPermanent(failure)) {
                store.recordRejected(fingerprint, now)
                Outcome.Rejected
            } else {
                retryLater(fingerprint, now, failure)
            }
        } catch (other: Exception) {
            listener.onFailed(request, null)
            retryLater(fingerprint, now, null)
        }
    }

    private fun retryLater(fingerprint: String, now: Long, failure: ApiException?): Outcome.RetryLater {
        val attempts = if (store.failedFingerprint == fingerprint) store.failureCount + 1 else 1
        val retryAfter = ((failure?.error as? ApiError.RateLimited)?.retryAfterSeconds ?: 0) * 1000L
        val wait = maxOf(backoffMillis(attempts), retryAfter).coerceAtMost(MAX_RETRY_MS)
        store.recordFailure(fingerprint, now + wait)
        return Outcome.RetryLater(wait)
    }

    companion object {
        /** Unchanged installations are re-sent this often, to keep "last seen" honest. */
        const val RESYNC_INTERVAL_MS = 24L * 60 * 60 * 1000

        private const val FIRST_RETRY_MS = 30_000L

        /** The longest wait between attempts, a Retry-After included. */
        const val MAX_RETRY_MS = 60L * 60 * 1000

        const val ANONYMOUS = "anonymous"
        const val SIGNED_IN = "signed-in"

        fun backoffMillis(attempts: Int): Long {
            val exponent = (attempts - 1).coerceIn(0, 16)
            return (FIRST_RETRY_MS shl exponent).coerceAtMost(MAX_RETRY_MS)
        }

        /** A 4xx other than 401, 408 and 429: the payload itself was refused. */
        fun isPermanent(failure: ApiException): Boolean {
            val status = failure.httpStatus ?: return false
            return status in 400..499 && status != 401 && status != 408 && status != 429
        }

        /** What was sent and to whom, hashed: the token is not kept twice in the clear. */
        fun fingerprint(request: InstallationRequest, account: String): String {
            val canonical = HttpPushApi.json.encodeToString(InstallationRequest.serializer(), request) + "\n" + account
            val digest = MessageDigest.getInstance("SHA-256").digest(canonical.toByteArray(Charsets.UTF_8))
            return digest.joinToString("") { "%02x".format(it) }
        }
    }
}
