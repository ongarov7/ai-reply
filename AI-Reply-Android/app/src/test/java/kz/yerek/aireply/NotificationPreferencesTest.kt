package kz.yerek.aireply

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.async
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.yield
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.ApiException
import kz.yerek.aireply.push.NotificationPermission
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
            if (fail) throw ApiException(ApiError.Offline)
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
    fun `offers stay off until the user switches them on`() = runBlocking {
        val before = NotificationPreferencesRepository(FakePreferencesApi()).state.value
        assertFalse("nothing loaded yet: marketing is opt-in", before.isOn("marketing"))
        listOf("account", "subscription", "security", "system").forEach { category ->
            assertTrue("$category keeps its default", before.isOn(category))
        }

        // A server whose answer does not mention marketing.
        val api = FakePreferencesApi().apply { server = server - "marketing" }
        val repository = NotificationPreferencesRepository(api)
        repository.load()
        assertFalse(repository.state.value.isOn("marketing"))
        assertTrue(repository.state.value.isOn("account"))

        assertTrue(repository.set("marketing", true))
        assertTrue("the user's choice wins", repository.state.value.isOn("marketing"))
        assertEquals(listOf(mapOf("marketing" to true)), api.updates)
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
    fun `a dismissed dialog is not a permanent denial`() {
        fun permanent(before: Boolean, after: Boolean, deniedBefore: Boolean = false) =
            NotificationPermission.isPermanentDenial(
                granted = false, rationaleBefore = before, rationaleAfter = after, deniedBefore = deniedBefore
            )

        assertFalse("first request dismissed (tap outside, Back)", permanent(before = false, after = false))
        assertFalse("first \"Don't allow\": Android will ask again", permanent(before = false, after = true))
        assertFalse("dismissed after one denial", permanent(before = true, after = true))
        assertTrue("second \"Don't allow\"", permanent(before = true, after = false))
        assertTrue(
            "blocked already: the dialog does not even show",
            permanent(before = false, after = false, deniedBefore = true)
        )
        assertFalse(
            NotificationPermission.isPermanentDenial(
                granted = true, rationaleBefore = true, rationaleAfter = false, deniedBefore = true
            )
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
