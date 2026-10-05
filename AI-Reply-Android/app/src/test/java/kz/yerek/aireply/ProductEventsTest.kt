package kz.yerek.aireply

import kz.yerek.aireply.analytics.ProductEvent
import kz.yerek.aireply.analytics.ProductEventSink
import kz.yerek.aireply.analytics.ProductEventValue
import kz.yerek.aireply.analytics.ProductEvents
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test

/** The events facade: names on the wire, values never free text, "once" means once. */
class ProductEventsTest {

    private val recorded = mutableListOf<Pair<String, Map<String, ProductEventValue>>>()
    private val defaultSink = ProductEvents.sink

    @Before
    fun record() {
        ProductEvents.sink = ProductEventSink { name, properties -> recorded += name to properties }
    }

    @After
    fun restore() {
        ProductEvents.sink = defaultSink
        ProductEvents.install(InMemoryPreferences())
    }

    @Test
    fun `an event reaches the sink under its wire name`() {
        ProductEvents.track(
            ProductEvent.ONBOARDING_COMPLETED,
            mapOf("version" to ProductEventValue.Number(2), "skipped" to ProductEventValue.Flag(false))
        )
        assertEquals(
            listOf(
                "onboarding_completed" to mapOf(
                    "version" to ProductEventValue.Number(2),
                    "skipped" to ProductEventValue.Flag(false)
                )
            ),
            recorded
        )
    }

    @Test
    fun `a once event is recorded once per install`() {
        val install = InMemoryPreferences()
        ProductEvents.install(install)
        repeat(3) { ProductEvents.trackOnce(ProductEvent.KEYBOARD_ENABLED_DETECTED) }
        assertEquals(listOf("keyboard_enabled_detected"), recorded.map { it.first })

        ProductEvents.install(install)
        ProductEvents.trackOnce(ProductEvent.KEYBOARD_ENABLED_DETECTED)
        assertEquals("a restart does not repeat it", 1, recorded.size)

        ProductEvents.install(InMemoryPreferences())
        ProductEvents.trackOnce(ProductEvent.KEYBOARD_ENABLED_DETECTED)
        assertEquals("a new install does", 2, recorded.size)
    }

    @Test
    fun `every event name is the snake case the allow-list expects`() {
        ProductEvent.entries.forEach { event ->
            assertEquals(event.name.lowercase(), event.wireName)
        }
    }
}
