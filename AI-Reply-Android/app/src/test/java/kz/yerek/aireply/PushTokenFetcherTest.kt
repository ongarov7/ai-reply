package kz.yerek.aireply

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kz.yerek.aireply.push.PushSupport.TokenResult
import kz.yerek.aireply.push.PushTokenFetchState
import kz.yerek.aireply.push.PushTokenFetchState.Action
import kz.yerek.aireply.push.PushTokenFetcher
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Getting the FCM token when the first attempt fails, and replacing one the
 * server reported invalid.
 *
 * FCM токені: сәтсіз болса қайта сұралады, жарамсызы ауыстырылады.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class PushTokenFetcherTest {

    // ------------------------------------------------------------- the rules

    @Test
    fun `a failed replacement stays owed`() {
        val state = PushTokenFetchState()
        state.requestReplacement()
        assertEquals(Action.REPLACE, state.begin())
        state.failed()
        assertTrue(state.needsRetry)
        assertTrue(state.pendingReplacement)
        assertEquals(30_000L, state.retryDelayMs)

        assertEquals("never a plain fetch of the dead token", Action.REPLACE, state.begin())
        state.succeeded()
        assertFalse(state.needsRetry)
        assertFalse(state.pendingReplacement)
    }

    @Test
    fun `a replacement asked for during a fetch waits for it`() {
        val state = PushTokenFetchState()
        assertEquals(Action.FETCH, state.begin())
        state.requestReplacement()
        assertNull("never two calls at once", state.begin())

        state.succeeded()
        assertTrue("a plain fetch does not settle it", state.pendingReplacement)
        assertEquals(Action.REPLACE, state.begin())
        state.succeeded()
        assertFalse(state.needsRetry)
    }

    @Test
    fun `the wait doubles up to half an hour`() {
        val state = PushTokenFetchState()
        val waits = (0 until 9).map {
            state.begin()
            state.failed()
            state.retryDelayMs / 1000
        }
        assertEquals(listOf(30L, 60L, 120L, 240L, 480L, 960L, 1800L, 1800L, 1800L), waits)

        state.begin()
        state.succeeded()
        assertFalse(state.needsRetry)
        assertEquals(0, state.failures)
    }

    // ----------------------------------------------------------- the fetcher

    private class Firebase {
        val calls = mutableListOf<String>()
        val answers = ArrayDeque<TokenResult>()
        var gate: CompletableDeferred<Unit>? = null

        suspend fun answer(kind: String): TokenResult {
            calls += kind
            gate?.await()
            return answers.removeFirstOrNull() ?: TokenResult.Failed
        }
    }

    private val firebase = Firebase()
    private val tokens = mutableListOf<String>()
    private var allowed = true
    private var foreground = true

    private fun TestScope.fetcher() = CoroutineScope(StandardTestDispatcher(testScheduler) + Job()).let { scope ->
        scope to PushTokenFetcher(
            current = { firebase.answer("fetch") },
            replacement = { firebase.answer("replace") },
            canFetch = { allowed },
            isForeground = { foreground },
            onToken = { tokens += it },
            scope = scope
        )
    }

    @Test
    fun `a failed fetch is tried again after the backoff until it works`() = runTest {
        val (scope, fetcher) = fetcher()
        firebase.answers += listOf(TokenResult.Failed, TokenResult.Failed, TokenResult.Token("token-1"))

        scope.launch { fetcher.fetch() }
        runCurrent()
        assertEquals(1, firebase.calls.size)

        advanceTimeBy(29_999)
        runCurrent()
        assertEquals("not before the wait is over", 1, firebase.calls.size)
        advanceTimeBy(2)
        runCurrent()
        assertEquals(2, firebase.calls.size)

        advanceTimeBy(60_001)
        runCurrent()
        assertEquals(3, firebase.calls.size)
        assertEquals(listOf("token-1"), tokens)
        assertFalse(fetcher.isNeeded)

        advanceTimeBy(3_600_000)
        runCurrent()
        assertEquals("nothing more once it worked", 3, firebase.calls.size)
        scope.coroutineContext[Job]?.cancel()
    }

    @Test
    fun `a token is asked for once a process`() = runTest {
        val (scope, fetcher) = fetcher()
        firebase.answers += TokenResult.Token("token-1")

        scope.launch {
            fetcher.fetchIfNeeded()
            fetcher.fetchIfNeeded()
        }
        runCurrent()

        assertEquals(listOf("fetch"), firebase.calls)
        scope.coroutineContext[Job]?.cancel()
    }

    @Test
    fun `in the background a retry waits for the next return to the app`() = runTest {
        val (scope, fetcher) = fetcher()
        foreground = false

        scope.launch { fetcher.fetch() }
        advanceTimeBy(3_600_000)
        runCurrent()
        assertEquals(1, firebase.calls.size)
        assertTrue(fetcher.isNeeded)

        foreground = true
        firebase.answers += TokenResult.Token("token-1")
        scope.launch { fetcher.fetchIfNeeded() }
        runCurrent()
        assertEquals(2, firebase.calls.size)
        assertEquals(listOf("token-1"), tokens)
        scope.coroutineContext[Job]?.cancel()
    }

    @Test
    fun `coming back tries at once and takes the waiting retry's place`() = runTest {
        val (scope, fetcher) = fetcher()
        scope.launch { fetcher.fetch() }
        runCurrent()

        advanceTimeBy(10_000)
        firebase.answers += TokenResult.Token("token-1")
        scope.launch { fetcher.fetchIfNeeded() }
        runCurrent()
        assertEquals(2, firebase.calls.size)

        advanceTimeBy(3_600_000)
        runCurrent()
        assertEquals("the old retry is gone", 2, firebase.calls.size)
        scope.coroutineContext[Job]?.cancel()
    }

    @Test
    fun `a failed replacement is tried again as a replacement`() = runTest {
        val (scope, fetcher) = fetcher()
        firebase.answers += listOf(TokenResult.Token("dead"), TokenResult.Failed, TokenResult.Token("fresh"))

        scope.launch {
            fetcher.fetch()
            fetcher.fetch(replacing = true)
        }
        runCurrent()
        assertEquals(listOf("fetch", "replace"), firebase.calls)
        assertTrue(fetcher.isNeeded)

        advanceTimeBy(30_001)
        runCurrent()
        assertEquals(listOf("fetch", "replace", "replace"), firebase.calls)
        assertEquals(listOf("dead", "fresh"), tokens)
        assertFalse(fetcher.isNeeded)
        scope.coroutineContext[Job]?.cancel()
    }

    @Test
    fun `a replacement asked for during a fetch runs right after it`() = runTest {
        val (scope, fetcher) = fetcher()
        val gate = CompletableDeferred<Unit>()
        firebase.gate = gate
        firebase.answers += listOf(TokenResult.Token("dead"), TokenResult.Token("fresh"))

        scope.launch { fetcher.fetch() }
        runCurrent()
        scope.launch { fetcher.fetch(replacing = true) }
        runCurrent()
        assertEquals("no second call while one is running", listOf("fetch"), firebase.calls)

        gate.complete(Unit)
        runCurrent()
        assertEquals(listOf("fetch", "replace"), firebase.calls)
        assertEquals(listOf("dead", "fresh"), tokens)
        assertFalse(fetcher.isNeeded)
        scope.coroutineContext[Job]?.cancel()
    }

    @Test
    fun `nothing is asked before the terms are accepted`() = runTest {
        val (scope, fetcher) = fetcher()
        allowed = false
        scope.launch { fetcher.fetchIfNeeded() }
        runCurrent()
        assertTrue(firebase.calls.isEmpty())

        allowed = true
        firebase.answers += TokenResult.Token("token-1")
        scope.launch { fetcher.fetchIfNeeded() }
        runCurrent()
        assertEquals(listOf("fetch"), firebase.calls)
        scope.coroutineContext[Job]?.cancel()
    }
}
