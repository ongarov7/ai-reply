package kz.yerek.aireply

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kz.yerek.aireply.ai.AILimits
import kz.yerek.aireply.analytics.ProductEvent
import kz.yerek.aireply.analytics.ProductEventReporter
import kz.yerek.aireply.analytics.ProductEventValue
import kz.yerek.aireply.analytics.ProductEventsTransport
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.ApiException
import kz.yerek.aireply.data.account.ProductEventsRequest
import kz.yerek.aireply.data.account.ServerConfigDto
import kz.yerek.aireply.data.account.ServerFeaturesDto
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Product events on their way to the server: batched, bounded, in memory,
 * only for a signed-in account on a server that asked for them.
 *
 * Өнім оқиғалары: топпен, шектеулі, тек жадта.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class ProductEventReporterTest {

    /** The transport the tests watch; [failures] are thrown one per call, first to last. */
    private class FakeTransport : ProductEventsTransport {
        val sent = mutableListOf<ProductEventsRequest>()
        val failures = ArrayDeque<Throwable>()

        override suspend fun send(request: ProductEventsRequest) {
            failures.removeFirstOrNull()?.let { throw it }
            sent += request
        }

        val names: List<String> get() = sent.flatMap { batch -> batch.events.map { it.name } }
    }

    private val transport = FakeTransport()
    private var signedIn = true
    private var enabled = true

    private fun TestScope.reporter() = ProductEventReporter(
        transport = transport,
        appVersion = "1.4",
        isSignedIn = { signedIn },
        isEnabled = { enabled },
        scope = backgroundScope,
        clock = { 1_791_196_200_999L }
    )

    private fun ProductEventReporter.recordViews(count: Int) = repeat(count) { index ->
        record(ProductEvent.ONBOARDING_STEP_VIEWED.wireName, mapOf("step" to ProductEventValue.Code("s$index")))
    }

    @After
    fun forgetServerAnswers() {
        AILimits.install(InMemoryPreferences())
    }

    @Test
    fun `a batch is the agreed wire format`() = runTest {
        val reporter = reporter()
        reporter.record(
            ProductEvent.ONBOARDING_COMPLETED.wireName,
            mapOf(
                "version" to ProductEventValue.Number(2),
                "skipped" to ProductEventValue.Flag(false)
            )
        )
        reporter.record(ProductEvent.GENDER_SELECTED.wireName, mapOf("source" to ProductEventValue.Code("onboarding")))
        reporter.flush()
        runCurrent()

        val body = Json.parseToJsonElement(
            Json.encodeToString(ProductEventsRequest.serializer(), transport.sent.single())
        ).jsonObject
        assertEquals(setOf("platform", "app_version", "events"), body.keys)
        assertEquals("android", body["platform"]!!.jsonPrimitive.content)
        assertEquals("1.4", body["app_version"]!!.jsonPrimitive.content)

        val events = body["events"]!!.jsonArray.map { it.jsonObject }
        assertEquals(listOf("onboarding_completed", "gender_selected"), events.map { it["name"]!!.jsonPrimitive.content })
        assertEquals("to the second, in UTC", "2026-10-05T10:30:00Z", events[0]["ts"]!!.jsonPrimitive.content)

        val props = events[0]["props"]!!.jsonObject
        assertEquals("2", props["version"]!!.jsonPrimitive.content)
        assertFalse("a number, not a string", props["version"]!!.jsonPrimitive.isString)
        assertEquals("false", props["skipped"]!!.jsonPrimitive.content)
        assertFalse(props["skipped"]!!.jsonPrimitive.isString)
        assertTrue("a code is a string", events[1]["props"]!!.jsonObject["source"]!!.jsonPrimitive.isString)
    }

    @Test
    fun `an event without properties still sends an empty object`() = runTest {
        val reporter = reporter()
        reporter.record(ProductEvent.ONBOARDING_REOPENED.wireName, emptyMap())
        reporter.flush()
        runCurrent()

        val event = Json.parseToJsonElement(
            Json.encodeToString(ProductEventsRequest.serializer(), transport.sent.single())
        ).jsonObject["events"]!!.jsonArray.single().jsonObject
        assertEquals(setOf("name", "ts", "props"), event.keys)
        assertTrue(event["props"]!!.jsonObject.isEmpty())
    }

    @Test
    fun `a few events wait for the timer`() = runTest {
        val reporter = reporter()
        reporter.recordViews(3)
        runCurrent()
        assertTrue("nothing sent straight away", transport.sent.isEmpty())

        advanceTimeBy(ProductEventReporter.FLUSH_DELAY_MS - 1)
        runCurrent()
        assertTrue(transport.sent.isEmpty())

        advanceTimeBy(1)
        runCurrent()
        assertEquals(1, transport.sent.size)
        assertEquals(3, transport.sent.single().events.size)
        assertEquals(0, reporter.pendingCount)
    }

    @Test
    fun `ten waiting events are sent at once`() = runTest {
        val reporter = reporter()
        reporter.recordViews(ProductEventReporter.FLUSH_THRESHOLD - 1)
        runCurrent()
        assertTrue(transport.sent.isEmpty())

        reporter.recordViews(1)
        runCurrent()
        assertEquals(listOf(ProductEventReporter.FLUSH_THRESHOLD), transport.sent.map { it.events.size })
    }

    @Test
    fun `going to the background sends what is waiting`() = runTest {
        val reporter = reporter()
        reporter.recordViews(2)
        reporter.flush()
        runCurrent()
        assertEquals(2, transport.names.size)

        advanceTimeBy(ProductEventReporter.FLUSH_DELAY_MS * 2)
        assertEquals("the timer finds nothing left to send", 1, transport.sent.size)
    }

    @Test
    fun `a batch holds at most twenty events`() = runTest {
        val reporter = reporter()
        reporter.recordViews(45)
        runCurrent()
        assertEquals(listOf(20, 20, 5), transport.sent.map { it.events.size })
        assertEquals((0 until 45).map { "onboarding_step_viewed" }, transport.names)
    }

    @Test
    fun `at most a hundred wait and the oldest go first`() = runTest {
        val reporter = reporter()
        reporter.recordViews(130)
        assertEquals(ProductEventReporter.MAX_QUEUED, reporter.pendingCount)

        runCurrent()
        val steps = transport.sent.flatMap { batch -> batch.events.map { it.props.getValue("step").content } }
        assertEquals((30 until 130).map { "s$it" }, steps)
    }

    @Test
    fun `nothing is recorded while signed out or before the server asks for events`() = runTest {
        signedIn = false
        val reporter = reporter()
        reporter.recordViews(12)
        assertEquals(0, reporter.pendingCount)

        signedIn = true
        enabled = false
        reporter.recordViews(12)
        assertEquals(0, reporter.pendingCount)

        advanceTimeBy(ProductEventReporter.FLUSH_DELAY_MS * 2)
        assertTrue(transport.sent.isEmpty())
    }

    @Test
    fun `events of an account that signed out are never sent`() = runTest {
        val reporter = reporter()
        reporter.recordViews(3)
        signedIn = false
        advanceTimeBy(ProductEventReporter.FLUSH_DELAY_MS + 1)
        assertTrue(transport.sent.isEmpty())
        assertEquals(0, reporter.pendingCount)

        signedIn = true
        reporter.recordViews(2)
        reporter.discard()
        reporter.flush()
        runCurrent()
        assertTrue("discarded on sign-out", transport.sent.isEmpty())
    }

    @Test
    fun `a passing failure is retried once, then dropped`() = runTest {
        val reporter = reporter()
        transport.failures += ApiException(ApiError.Offline)
        transport.failures += ApiException(ApiError.TimedOut)
        reporter.recordViews(2)
        reporter.flush()
        runCurrent()
        assertTrue(transport.sent.isEmpty())
        assertEquals("kept for one more try", 2, reporter.pendingCount)

        advanceTimeBy(ProductEventReporter.FLUSH_DELAY_MS + 1)
        assertTrue(transport.sent.isEmpty())
        assertEquals("the second failure drops them", 0, reporter.pendingCount)

        reporter.recordViews(1)
        reporter.flush()
        runCurrent()
        assertEquals("later events still go", 1, transport.names.size)
    }

    @Test
    fun `a refused batch is not retried`() = runTest {
        val reporter = reporter()
        transport.failures += ApiException(ApiError.InvalidRequest)
        reporter.recordViews(2)
        reporter.flush()
        runCurrent()
        assertEquals(0, reporter.pendingCount)

        advanceTimeBy(ProductEventReporter.FLUSH_DELAY_MS * 2)
        assertTrue(transport.sent.isEmpty())
    }

    @Test
    fun `free text never becomes an event`() = runTest {
        val reporter = reporter()
        reporter.record(ProductEvent.ONBOARDING_STEP_VIEWED.wireName, mapOf("step" to ProductEventValue.Code("Сәлем, қалайсың?")))
        reporter.record("message text", emptyMap())
        reporter.record(ProductEvent.GENDER_SELECTED.wireName, mapOf("my note" to ProductEventValue.Flag(true)))
        assertEquals(0, reporter.pendingCount)
    }

    // ----------------------------------------------------------- the flag

    @Test
    fun `the events flag is off unless the server announces it, and is kept`() {
        assertFalse(ServerFeaturesDto().productEvents)
        val decoded = Json { ignoreUnknownKeys = true }
            .decodeFromString(ServerFeaturesDto.serializer(), """{"product_events":true}""")
        assertTrue(decoded.productEvents)

        val file = InMemoryPreferences()
        AILimits.install(file)
        assertFalse(AILimits.features.productEvents)

        AILimits.apply(ServerConfigDto(features = ServerFeaturesDto(productEvents = true)))
        AILimits.install(file)
        assertTrue("read back from the file", AILimits.features.productEvents)

        AILimits.apply(ServerConfigDto())
        assertFalse(AILimits.features.productEvents)
    }
}
