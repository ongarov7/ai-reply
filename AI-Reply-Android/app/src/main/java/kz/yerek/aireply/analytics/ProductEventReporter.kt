package kz.yerek.aireply.analytics

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.serialization.json.JsonPrimitive
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.ApiException
import kz.yerek.aireply.data.account.ProductEventDto
import kz.yerek.aireply.data.account.ProductEventsRequest
import kz.yerek.aireply.platform.ReplyLog
import java.time.Instant

/** Delivers one batch to the server, or throws when it did not arrive. */
fun interface ProductEventsTransport {
    suspend fun send(request: ProductEventsRequest)
}

/**
 * The [ProductEventSink] that sends the app's events to the server.
 *
 * Оқиғалар тек жадта жиналып, шағын топпен жіберіледі; дискіге ештеңе жазылмайды.
 *
 * Events wait in memory only, never on disk, and leave in batches of at most
 * [MAX_BATCH]: as soon as [FLUSH_THRESHOLD] are waiting, when the app goes to
 * the background ([flush]), and [FLUSH_DELAY_MS] after an event started
 * waiting. At most [MAX_QUEUED] wait; past that the oldest are dropped.
 *
 * Only a signed-in account on a server that announced the endpoint gets
 * events; one recorded otherwise is dropped, not kept for later. Delivery is
 * best effort: a batch that failed for a passing reason (offline, timeout,
 * rate limit, server error) is tried once more with the next flush, then
 * dropped. The keyboard records nothing, so nothing here runs on its behalf.
 */
class ProductEventReporter(
    private val transport: ProductEventsTransport,
    private val appVersion: String,
    private val isSignedIn: () -> Boolean,
    /** The server announced `features.product_events`. */
    private val isEnabled: () -> Boolean,
    /** Outlives every screen: a batch is not cancelled by leaving one. */
    private val scope: CoroutineScope,
    private val clock: () -> Long = System::currentTimeMillis
) : ProductEventSink {

    private class Pending(val event: ProductEventDto, val retried: Boolean = false)

    /** Guards itself and [timer]. */
    private val queue = ArrayDeque<Pending>()
    private var timer: Job? = null

    /** One batch in flight at a time, so a retry can never overtake newer events. */
    private val sending = Mutex()

    /** How many events are waiting. */
    val pendingCount: Int get() = synchronized(queue) { queue.size }

    override fun record(name: String, properties: Map<String, ProductEventValue>) {
        ReplyLog.event { "event $name $properties" }
        if (!isSignedIn() || !isEnabled()) return
        val event = wireEvent(name, properties) ?: return
        val reachedThreshold = synchronized(queue) {
            queue.addLast(Pending(event))
            trimToCapacity()
            queue.size >= FLUSH_THRESHOLD
        }
        if (reachedThreshold) flush() else scheduleFlush()
    }

    /** Sends everything waiting. The app calls it when it goes to the background. */
    fun flush() {
        scope.launch { drain() }
    }

    /** Forgets everything waiting: the account it was recorded for has signed out. */
    fun discard() {
        synchronized(queue) { queue.clear() }
    }

    private suspend fun drain() {
        if (!sending.tryLock()) return // the batch in flight goes on to whatever is waiting
        try {
            while (true) {
                if (!isSignedIn() || !isEnabled()) {
                    discard()
                    break
                }
                val batch = takeBatch() ?: break
                if (!deliver(batch)) break
            }
        } finally {
            sending.unlock()
        }
        // What arrived while the last batch was in flight, or the one retry.
        scheduleFlush()
    }

    /** True when the batch arrived. */
    private suspend fun deliver(batch: List<Pending>): Boolean {
        val request = ProductEventsRequest(
            platform = PLATFORM,
            appVersion = appVersion,
            events = batch.map { it.event }
        )
        return try {
            transport.send(request)
            true
        } catch (cancellation: CancellationException) {
            throw cancellation
        } catch (failure: Throwable) {
            val retry = isPassing(failure)
            ReplyLog.event { "events not sent: ${failure::class.simpleName}, retry once: $retry" }
            if (retry) requeue(batch)
            false
        }
    }

    private fun takeBatch(): List<Pending>? = synchronized(queue) {
        if (queue.isEmpty()) null else List(minOf(MAX_BATCH, queue.size)) { queue.removeFirst() }
    }

    /** Back to the front, once: an event that already had its retry is dropped. */
    private fun requeue(batch: List<Pending>) {
        val again = batch.filterNot { it.retried }.map { Pending(it.event, retried = true) }
        synchronized(queue) {
            queue.addAll(0, again)
            trimToCapacity()
        }
    }

    private fun scheduleFlush() {
        synchronized(queue) {
            if (queue.isEmpty() || timer?.isActive == true) return
            timer = scope.launch {
                delay(FLUSH_DELAY_MS)
                synchronized(queue) { timer = null }
                drain()
            }
        }
    }

    /** Called with the queue locked. */
    private fun trimToCapacity() {
        while (queue.size > MAX_QUEUED) queue.removeFirst()
    }

    private fun wireEvent(name: String, properties: Map<String, ProductEventValue>): ProductEventDto? {
        // The facade only ever passes enum names and step ids; anything else
        // is a programming error, and free text must never leave the phone.
        val codes = properties.values.filterIsInstance<ProductEventValue.Code>().map { it.value }
        if (!(codes + properties.keys + name).all(MACHINE_CODE::matches)) {
            ReplyLog.event { "event dropped: not a machine code" }
            return null
        }
        return ProductEventDto(
            name = name,
            ts = Instant.ofEpochSecond(clock() / 1000).toString(),
            props = properties.mapValues { (_, value) -> value.wire() }
        )
    }

    companion object {
        const val PLATFORM = "android"
        const val MAX_BATCH = 20
        const val FLUSH_THRESHOLD = 10
        const val MAX_QUEUED = 100
        const val FLUSH_DELAY_MS = 30_000L

        private val MACHINE_CODE = Regex("[A-Za-z][A-Za-z0-9_]{0,39}")

        private fun ProductEventValue.wire(): JsonPrimitive = when (this) {
            is ProductEventValue.Number -> JsonPrimitive(value)
            is ProductEventValue.Flag -> JsonPrimitive(value)
            is ProductEventValue.Code -> JsonPrimitive(value)
        }

        /** A failure the same batch may get past next time. Anything else would fail again. */
        private fun isPassing(failure: Throwable): Boolean = when ((failure as? ApiException)?.error) {
            is ApiError.Offline, is ApiError.TimedOut, is ApiError.RateLimited, is ApiError.Server -> true
            else -> false
        }
    }
}
