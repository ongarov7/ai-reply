package kz.yerek.aireply

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.async
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.yield
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.ApiException
import kz.yerek.aireply.push.NotificationPreferencesApi
import kz.yerek.aireply.push.NotificationPreferencesDto
import kz.yerek.aireply.push.NotificationPreferencesRepository
import kz.yerek.aireply.push.PushUiState
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The category switches (optimistic, with rollback) and when the Home card
 * may appear.
 *
 * Санаттар: бірден ауысады, сервер қабылдамаса — кері қайтады.
 */
class NotificationPreferencesTest {

    private class FakePreferencesApi : NotificationPreferencesApi {
        var server = mapOf(
            "account" to true, "subscription" to true, "security" to true, "system" to true, "marketing" to true
        )
        val optional = listOf("account", "subscription", "system", "marketing")
        var gate: CompletableDeferred<Unit>? = null
        var fail = false
        val updates = mutableListOf<Map<String, Boolean>>()

        override suspend fun load() = NotificationPreferencesDto(server, optional)

        override suspend fun update(changes: Map<String, Boolean>): NotificationPreferencesDto {
            updates += changes
            gate?.await()
            if (fail) throw ApiException(ApiError.Offline, httpStatus = 0)
            server = server + changes
            return NotificationPreferencesDto(server, optional)
        }
    }

    @Test
    fun `a switch moves at once and stays when the server agrees`() = runBlocking {
        val api = FakePreferencesApi()
        val repository = NotificationPreferencesRepository(api)
        repository.load()
        assertTrue(repository.state.value.isOn("marketing"))

        api.gate = CompletableDeferred()
        val change = async { repository.set("marketing", false) }
        yield()
        assertFalse("optimistic: off before the server answered", repository.state.value.isOn("marketing"))

        api.gate!!.complete(Unit)
        assertTrue(change.await())
        assertFalse(repository.state.value.isOn("marketing"))
        assertTrue(repository.state.value.pending.isEmpty())
        assertEquals(listOf(mapOf("marketing" to false)), api.updates)
    }

    @Test
    fun `a switch the server did not take moves back`() = runBlocking {
        val api = FakePreferencesApi().apply { fail = true }
        val repository = NotificationPreferencesRepository(api)
        repository.load()

        assertFalse(repository.set("subscription", false))
        assertTrue("rolled back", repository.state.value.isOn("subscription"))
        assertTrue(repository.state.value.failed)
    }

    @Test
    fun `security cannot be switched off and is not even asked`() = runBlocking {
        val api = FakePreferencesApi()
        val repository = NotificationPreferencesRepository(api)
        repository.load()

        assertTrue(repository.state.value.isLocked("security"))
        assertFalse(repository.set("security", false))
        assertTrue(repository.state.value.isOn("security"))
        assertTrue(api.updates.isEmpty())
    }

    @Test
    fun `signing out forgets the previous account's switches`() = runBlocking {
        val repository = NotificationPreferencesRepository(FakePreferencesApi())
        repository.load()
        repository.clear()
        assertFalse(repository.state.value.isLoaded)
    }

    // ------------------------------------------------------------ Home card

    private val eligible = PushUiState(
        supportedInBuild = true,
        serverDelivers = true,
        serverHasInstallations = true,
        runtimePermission = true,
        granted = false
    )

    @Test
    fun `the card appears only where the system can still ask`() {
        assertTrue(eligible.showsPrompt)
        assertFalse("no Firebase in this build", eligible.copy(supportedInBuild = false).showsPrompt)
        assertFalse("the server cannot deliver", eligible.copy(serverDelivers = false).showsPrompt)
        assertFalse("before Android 13 there is nothing to ask", eligible.copy(runtimePermission = false).showsPrompt)
        assertFalse("already allowed", eligible.copy(granted = true).showsPrompt)
        assertFalse("denied for good", eligible.copy(permanentlyDenied = true).showsPrompt)
        assertFalse("Not now", eligible.copy(promptDismissed = true).showsPrompt)
        assertFalse("switched off in the app", eligible.copy(notificationsEnabled = false).showsPrompt)
        assertTrue(
            "debug builds can force it",
            eligible.copy(supportedInBuild = false, promptDismissed = true, debugForced = true).showsPrompt
        )
    }

    @Test
    fun `settings offer the dialog only while it can still appear`() {
        assertTrue(eligible.canAskSystem)
        assertFalse(eligible.copy(permanentlyDenied = true).canAskSystem)
        assertFalse(eligible.copy(runtimePermission = false).canAskSystem)
        assertFalse(PushUiState().isAvailable)
        assertTrue(eligible.isAvailable)
    }
}
