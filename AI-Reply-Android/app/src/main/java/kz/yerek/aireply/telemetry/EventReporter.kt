package kz.yerek.aireply.telemetry

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.ApiException
import kz.yerek.aireply.data.account.TransportFailureReport
import java.util.UUID

/**
 * Minimal app diagnostics: a small in-memory queue of allow-listed events,
 * sent in batches to `POST /api/v1/events`.
 *
 * Диагностика: тек рұқсат етілген оқиғалар, жадта, топтап жіберіледі.
 *
 * - Nothing is recorded before the terms are accepted (events from before
 *   are dropped, not kept for later) or while "Share diagnostics" is off, and
 *   nothing is sent unless the server announced `features.telemetry`. Before
 *   the server has answered, events wait in memory; if it says no, they are
 *   dropped. Turning the switch off drops the queue too.
 * - Each event is stamped with the account it happened under. A batch goes
 *   with the bearer token only when that account is still the signed-in one;
 *   otherwise it goes without, so one account's events never land on another.
 * - At most [MAX_QUEUE] events are held (the oldest go first) and nothing is
 *   written to disk: a killed process loses at most one minute of events.
 * - A batch goes out [FLUSH_INTERVAL_MS] after the first queued event and when
 *   the app goes to the background; network trouble, 5xx and 429 keep it for a
 *   later retry with backoff, any other 4xx drops it. Event ids make a retried
 *   batch safe to count once.
 * - The keyboard never calls this: its requests use the keyboard header scope,
 *   which reports nothing, and it has no event of its own.
 */
class EventReporter(
    private val api: EventsApi,
    private val installationId: () -> String,
    private val sessionId: () -> String?,
    /** The user's "Share diagnostics" switch. */
    private val userAllows: () -> Boolean,
    /** `features.telemetry`; null until the server has answered. */
    private val serverAllows: () -> Boolean?,
    /** A fresh access token to attribute the batch to the account, or null. Never refreshes. */
    private val token: () -> String?,
    /** The legal consent: nothing is recorded or sent before it. */
    private val consentGiven: () -> Boolean = { true },
    /** Who is signed in now: the user id, [SIGNED_IN] when not known yet, [ANONYMOUS] when nobody. */
    private val accountKey: () -> String = { ANONYMOUS },
    private val scope: CoroutineScope,
    private val clock: () -> Long = System::currentTimeMillis,
    private val newId: () -> String = { UUID.randomUUID().toString() },
    private val flushIntervalMs: Long = FLUSH_INTERVAL_MS
) {

    private val lock = Any()
    private val queue = ArrayDeque<TelemetryEvent>()
    private val flushMutex = Mutex()
    private var scheduled: Job? = null
    private var retryAt = 0L
    private var failures = 0
    private val lastApiError = HashMap<String, Long>()

    @Volatile
    private var disabledByServer = false

    /** Events waiting to be sent (for tests and the debug screen). */
    val pending: List<TelemetryEvent> get() = synchronized(lock) { queue.toList() }

    // ------------------------------------------------------------ events

    fun appOpened(coldStart: Boolean) = record(EventSchema.APP_OPENED, mapOf("cold_start" to coldStart))

    fun appBackgrounded(foregroundSeconds: Long) {
        record(EventSchema.APP_BACKGROUNDED, mapOf("foreground_seconds" to foregroundSeconds.coerceAtLeast(0)))
        flushSoon()
    }

    /** Only for a failure inside a sign-in SDK, which the server never saw. */
    fun loginFailed(method: String, errorCode: String) =
        record(EventSchema.LOGIN_FAILED, mapOf("method" to method, "error_code" to errorCode))

    fun logout() = record(EventSchema.LOGOUT)

    fun pushPermissionRequested() = record(EventSchema.PUSH_PERMISSION_REQUESTED)

    fun pushPermissionGranted() =
        record(EventSchema.PUSH_PERMISSION_GRANTED, mapOf("status" to "authorized"))

    fun pushPermissionDenied() = record(EventSchema.PUSH_PERMISSION_DENIED)

    fun pushTokenRegistered() = record(EventSchema.PUSH_TOKEN_REGISTERED, mapOf("provider" to PROVIDER))

    fun pushTokenRefreshed() = record(EventSchema.PUSH_TOKEN_REFRESHED, mapOf("provider" to PROVIDER))

    fun pushTokenRegistrationFailed(errorCode: String, requestId: String?) {
        val properties = LinkedHashMap<String, Any>()
        properties["provider"] = PROVIDER
        properties["error_code"] = errorCode
        if (EventSchema.isCode(requestId)) properties["request_id"] = requestId!!
        record(EventSchema.PUSH_TOKEN_REGISTRATION_FAILED, properties)
    }

    fun notificationOpened(properties: Map<String, Any>) = record(EventSchema.NOTIFICATION_OPENED, properties)

    /**
     * A request that got no HTTP answer (status 0). Throttled per route and
     * cause, and never for this reporter's own endpoint.
     */
    fun apiError(report: TransportFailureReport) {
        if (report.route == "/" + HttpEventsApi.ROUTE) return
        val key = report.route + "|" + report.failure.code
        val now = clock()
        synchronized(lock) {
            val last = lastApiError[key]
            if (last != null && now - last < API_ERROR_INTERVAL_MS) return
            lastApiError[key] = now
        }
        val properties = LinkedHashMap<String, Any>()
        properties["route"] = report.route
        properties["status"] = 0L
        properties["error_code"] = report.failure.code
        if (EventSchema.isCode(report.requestId)) properties["request_id"] = report.requestId
        record(EventSchema.API_ERROR, properties)
    }

    // ------------------------------------------------------------- queue

    /** Queues one event if consent, the user and the server allow it. False when refused. */
    fun record(name: String, properties: Map<String, Any> = emptyMap()): Boolean {
        if (!consentGiven() || !userAllows() || disabledByServer || serverAllows() == false) return false
        if (!EventSchema.isValid(name, properties)) return false
        val event = TelemetryEvent(newId(), name, clock(), properties, sessionId(), accountKey())
        synchronized(lock) {
            queue.addLast(event)
            while (queue.size > MAX_QUEUE) queue.removeFirst()
        }
        scheduleFlush(flushIntervalMs)
        return true
    }

    /** Drops everything queued: the user switched diagnostics off. */
    fun clear() {
        synchronized(lock) {
            queue.clear()
            scheduled?.cancel()
            scheduled = null
        }
    }

    /** Sends what is queued as soon as possible (the app went to the background). */
    fun flushSoon() = scheduleFlush(0)

    /** The terms were just accepted: anything recorded since may go out. */
    fun onConsentGiven() = flushSoon()

    /** The server's features arrived; queued events may go out now, or be dropped. */
    fun onServerFeaturesChanged() {
        when (serverAllows()) {
            true -> flushSoon()
            false -> clear()
            null -> Unit
        }
    }

    /**
     * [replace] is for a flush rescheduling itself: the job asking is still
     * active, and must not be mistaken for a flush that is already coming.
     */
    private fun scheduleFlush(delayMs: Long, replace: Boolean = false) {
        synchronized(lock) {
            if (queue.isEmpty()) return
            if (!replace && delayMs > 0 && scheduled?.isActive == true) return
            scheduled = scope.launch {
                val wait = maxOf(delayMs, retryAt - clock())
                if (wait > 0) delay(wait)
                runCatching { flush() }
            }
        }
    }

    /** Sends queued batches until the queue is empty or a send fails. */
    suspend fun flush() {
        flushMutex.withLock {
            while (true) {
                if (!userAllows() || disabledByServer) {
                    clear()
                    return
                }
                // Consent withdrawn (new terms not accepted yet): hold, send nothing.
                if (!consentGiven()) return
                when (serverAllows()) {
                    null -> return
                    false -> {
                        clear()
                        return
                    }
                    true -> Unit
                }
                val now = clock()
                // A wait longer than any backoff dates from a clock that was far ahead.
                if (now < retryAt && retryAt - now <= MAX_BACKOFF_MS) {
                    scheduleFlush(retryAt - now, replace = true)
                    return
                }
                val batch = nextBatch(now) ?: return
                when (send(batch)) {
                    Outcome.SENT -> {
                        remove(batch.events)
                        failures = 0
                        retryAt = 0
                    }
                    Outcome.DROP -> remove(batch.events)
                    Outcome.DISABLED -> {
                        disabledByServer = true
                        clear()
                        return
                    }
                    Outcome.RETRY -> {
                        failures++
                        val backoff = backoffMs(failures)
                        retryAt = clock() + backoff
                        scheduleFlush(backoff, replace = true)
                        return
                    }
                }
            }
        }
    }

    private enum class Outcome { SENT, DROP, RETRY, DISABLED }

    private suspend fun send(batch: EventBatch): Outcome {
        // Attributed only to the account the events happened under, and only
        // while it is still the one signed in.
        val token = if (batch.accountKey != ANONYMOUS && batch.accountKey == accountKey()) token() else null
        return try {
            if (api.send(batch, token).disabled) Outcome.DISABLED else Outcome.SENT
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (failure: ApiException) {
            if (failure.error is ApiError.Unauthorized && token != null) {
                // A token the server no longer takes: events do not need one.
                return try {
                    if (api.send(batch, null).disabled) Outcome.DISABLED else Outcome.SENT
                } catch (cancelled: CancellationException) {
                    throw cancelled
                } catch (again: ApiException) {
                    classify(again)
                } catch (other: Exception) {
                    Outcome.RETRY
                }
            }
            classify(failure)
        } catch (other: Exception) {
            Outcome.RETRY
        }
    }

    private fun classify(failure: ApiException): Outcome {
        val status = failure.httpStatus ?: return Outcome.RETRY
        return when {
            status == 0 -> Outcome.RETRY
            status == 429 || status >= 500 -> Outcome.RETRY
            status in 400..499 -> Outcome.DROP
            else -> Outcome.RETRY
        }
    }

    /** The oldest events of one session, within the server's size limits. */
    private fun nextBatch(now: Long): EventBatch? {
        val events = synchronized(lock) {
            // The server refuses events older than a week.
            queue.removeAll { now - it.occurredAtMillis > MAX_AGE_MS }
            val first = queue.firstOrNull() ?: return null
            queue.asSequence()
                .takeWhile { it.sessionId == first.sessionId && it.accountKey == first.accountKey }
                .take(MAX_BATCH)
                .toList()
        }
        var batch = EventBatch(installationId(), events.first().sessionId, events, events.first().accountKey)
        while (batch.events.size > 1 && batch.toJson().toByteArray(Charsets.UTF_8).size > MAX_BODY_BYTES) {
            batch = batch.copy(events = batch.events.take(batch.events.size / 2))
        }
        return batch
    }

    private fun remove(sent: List<TelemetryEvent>) {
        val ids = sent.mapTo(HashSet()) { it.id }
        synchronized(lock) { queue.removeAll { it.id in ids } }
    }

    companion object {
        /** [accountKey] when nobody is signed in. */
        const val ANONYMOUS = "anon"

        /** [accountKey] when signed in but the user id is not known yet. */
        const val SIGNED_IN = "signed-in"

        const val PROVIDER = "fcm"
        const val MAX_QUEUE = 100
        const val MAX_BATCH = 50
        /** The server takes 64 KB; a margin for headers and rounding. */
        const val MAX_BODY_BYTES = 60 * 1024
        const val FLUSH_INTERVAL_MS = 60_000L
        const val MAX_AGE_MS = 7L * 24 * 60 * 60 * 1000 - 60 * 60 * 1000
        const val API_ERROR_INTERVAL_MS = 60_000L
        const val MAX_BACKOFF_MS = 15L * 60 * 1000

        fun backoffMs(failures: Int): Long {
            val exponent = (failures - 1).coerceIn(0, 10)
            return (FLUSH_INTERVAL_MS shl exponent).coerceAtMost(MAX_BACKOFF_MS)
        }
    }
}
