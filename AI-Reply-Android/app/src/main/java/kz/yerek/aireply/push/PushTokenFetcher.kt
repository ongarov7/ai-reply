package kz.yerek.aireply.push

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kz.yerek.aireply.platform.ReplyLog

/**
 * Where asking Firebase for the token stands: one call at a time, failures
 * counted for the backoff, and a replacement owed until one has succeeded.
 *
 * Токен сұрау күйі: бір уақытта бір сұрау, ауыстыру орындалғанша сақталады.
 */
class PushTokenFetchState {

    enum class Action { FETCH, REPLACE }

    var pendingReplacement = false
        private set
    var inFlight: Action? = null
        private set
    var failures = 0
        private set

    val needsRetry: Boolean get() = pendingReplacement || failures > 0

    /** 30 seconds, doubling up to 30 minutes. */
    val retryDelayMs: Long
        get() = (FIRST_RETRY_MS shl (failures - 1).coerceIn(0, MAX_DOUBLINGS)).coerceAtMost(MAX_RETRY_MS)

    fun requestReplacement() {
        pendingReplacement = true
    }

    /** What to do now, or null while a call is running. */
    fun begin(): Action? {
        if (inFlight != null) return null
        return (if (pendingReplacement) Action.REPLACE else Action.FETCH).also { inFlight = it }
    }

    fun succeeded() {
        if (inFlight == Action.REPLACE) pendingReplacement = false
        inFlight = null
        failures = 0
    }

    fun failed() {
        inFlight = null
        failures = (failures + 1).coerceAtMost(MAX_FAILURES)
    }

    private companion object {
        const val FIRST_RETRY_MS = 30_000L
        const val MAX_RETRY_MS = 30L * 60 * 1000
        const val MAX_DOUBLINGS = 6
        const val MAX_FAILURES = 7
    }
}

/**
 * Gets the FCM registration token and keeps at it.
 *
 * FCM токені: сәтсіз болса, аралықты ұлғайта отырып қайта сұрайды.
 *
 * A failed call (offline, Play services not ready) is tried again after the
 * backoff while the app is in front, and at once on the next return to it. A
 * token the server reported invalid is replaced the same way, and a
 * replacement that failed is never downgraded to fetching the dead token back.
 *
 * @param current asks Firebase for the token it has.
 * @param replacement deletes it and makes a new one.
 * @param canFetch Firebase is in this build and the terms are accepted.
 */
class PushTokenFetcher(
    private val current: suspend () -> PushSupport.TokenResult,
    private val replacement: suspend () -> PushSupport.TokenResult,
    private val canFetch: () -> Boolean,
    private val isForeground: () -> Boolean,
    private val onToken: (String) -> Unit,
    private val scope: CoroutineScope
) {

    private val lock = Any()
    private val state = PushTokenFetchState()
    private var hasToken = false
    private var retryJob: Job? = null

    /** Firebase has not given this process a token yet, or a retry is owed. */
    val isNeeded: Boolean get() = synchronized(lock) { !hasToken || state.needsRetry }

    /** At launch, after the consent and on every return to the app. */
    suspend fun fetchIfNeeded() {
        if (isNeeded) fetch()
    }

    /**
     * One attempt; returns when it ended. While another is running nothing
     * starts, and a [replacing] request runs right after that one.
     */
    suspend fun fetch(replacing: Boolean = false) {
        val action = synchronized(lock) {
            if (replacing) state.requestReplacement()
            if (!canFetch()) return
            state.begin()?.also {
                retryJob?.cancel()
                retryJob = null
            }
        } ?: return

        val result = try {
            if (action == PushTokenFetchState.Action.REPLACE) replacement() else current()
        } catch (cancelled: CancellationException) {
            synchronized(lock) { state.failed() }
            throw cancelled
        } catch (failure: Exception) {
            ReplyLog.warn(failure) { "FCM token request failed" }
            PushSupport.TokenResult.Failed
        }

        if (result is PushSupport.TokenResult.Token) {
            synchronized(lock) {
                state.succeeded()
                hasToken = true
            }
            onToken(result.value)
        } else {
            synchronized(lock) { state.failed() }
        }

        val replacementNext = synchronized(lock) {
            val owed = state.pendingReplacement && state.failures == 0
            if (!owed && state.needsRetry) scheduleRetry()
            owed
        }
        if (replacementNext) fetch()
    }

    /** Called with [lock] held. */
    private fun scheduleRetry() {
        retryJob?.cancel()
        val wait = state.retryDelayMs
        retryJob = scope.launch {
            delay(wait)
            synchronized(lock) { retryJob = null }
            // In the background the next return to the app tries instead.
            if (isForeground()) fetch()
        }
    }
}
