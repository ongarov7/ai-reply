package kz.yerek.aireply

import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.int
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kz.yerek.aireply.diagnostics.CrashReporter
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class CrashReporterTest {

    private class MemoryStore : CrashReporter.Store {
        var pending: CrashReporter.Pending? = null
        override fun load() = pending
        override fun save(pending: CrashReporter.Pending) { this.pending = pending }
        override fun clear() { pending = null }
    }

    private fun crash(): Throwable {
        val root = IllegalStateException("secret text the person typed")
        root.stackTrace = arrayOf(
            StackTraceElement("java.util.ArrayList", "get", "ArrayList.java", 437),
            StackTraceElement("kz.yerek.aireply.keyboard.ReplyKeyboardService", "onStartInput", "ReplyKeyboardService.kt", 120)
        )
        return RuntimeException("wrapper", root)
    }

    @Test
    fun summaryKeepsTheRootClassAndOurFrameButNeverTheMessage() {
        val (code, detail) = CrashReporter.summary(crash())
        assertEquals("IllegalStateException", code)
        assertEquals("ReplyKeyboardService.kt:120 onStartInput", detail)
        val store = MemoryStore()
        CrashReporter.record(store, crash())
        CrashReporter.record(store, crash())
        val body = CrashReporter.body(store.pending!!, appVersion = "1.4.0", osVersion = "15")
        assertFalse(body.contains("secret"))
        val json = Json.parseToJsonElement(body).jsonObject
        assertEquals("android", json["platform"]!!.jsonPrimitive.content)
        assertEquals("crash", json["kind"]!!.jsonPrimitive.content)
        assertEquals(2, json["count"]!!.jsonPrimitive.int)
    }

    @Test
    fun aSentSummaryIsClearedAndAnUnreachableServerKeepsItForTheNextStart() = runTest {
        val store = MemoryStore()
        CrashReporter.record(store, crash())
        CrashReporter.sendPending(store, "1.4.0") { throw java.io.IOException("offline") }
        assertEquals(1, store.pending!!.attempts)
        var sent: String? = null
        CrashReporter.sendPending(store, "1.4.0") { sent = it }
        assertTrue(sent!!.contains("IllegalStateException"))
        assertNull(store.pending)
    }

    @Test
    fun aRefusedSummaryIsDroppedAfterTheLastAttempt() = runTest {
        val store = MemoryStore()
        CrashReporter.record(store, crash())
        repeat(CrashReporter.MAX_ATTEMPTS) {
            CrashReporter.sendPending(store, "1.4.0") { throw IllegalArgumentException("400") }
        }
        var calls = 0
        CrashReporter.sendPending(store, "1.4.0") { calls++ }
        assertEquals(0, calls)
        assertNull(store.pending)
    }
}
