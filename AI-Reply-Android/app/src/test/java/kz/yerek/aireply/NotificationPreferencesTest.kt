package kz.yerek.aireply

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.async
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.yield
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.ApiException
import kz.yerek.aireply.data.account.SessionAuth
import kz.yerek.aireply.push.HttpPushApi
import kz.yerek.aireply.push.NotificationPermission
import kz.yerek.aireply.push.NotificationPreferencesApi
import kz.yerek.aireply.push.NotificationPreferencesDto
import kz.yerek.aireply.push.NotificationPreferencesRepository
import kz.yerek.aireply.push.PushStateStore
import kz.yerek.aireply.push.PushUiState
import kz.yerek.aireply.support.FakeResponse
import kz.yerek.aireply.support.FakeServer
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
        val owners = mutableListOf<String?>()

        override suspend fun load() = NotificationPreferencesDto(server, optional)

        override suspend fun update(changes: Map<String, Boolean>, expectedUserId: String?): NotificationPreferencesDto {
            updates += changes
            owners += expectedUserId
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

    @Test
    fun `a change stays bound to the account that made it`() = runBlocking {
        val api = FakePreferencesApi()
        var account: String? = "first"
        val repository = NotificationPreferencesRepository(api, accountId = { account })
        repository.load()

        api.gate = CompletableDeferred()
        val change = async { repository.set("marketing", false) }
        yield()
        account = "second"
        repository.clear()
        api.gate!!.complete(Unit)
        change.await()

        assertEquals(listOf<String?>("first"), api.owners)
        assertFalse("the next account does not inherit the answer", repository.state.value.isLoaded)
    }

    @Test
    fun `a change names its account on the wire, and none when it is not known`() = runBlocking {
        val server = FakeServer { FakeResponse(200, """{"preferences":{"marketing":true},"optional":["marketing"]}""") }
        val api = HttpPushApi(
            client = { ApiClient("https://example.test", openConnection = server::open) },
            session = object : SessionAuth {
                override val isSignedIn: Boolean get() = true
                override suspend fun <T> authenticated(work: suspend (String) -> T): T = work("t1")
            }
        )

        api.update(mapOf("marketing" to true), "user-a")
        api.update(mapOf("marketing" to false), null)

        val (named, unknown) = server.requests.toList()
        assertEquals("PUT", named.method)
        assertEquals("/api/v1/me/notification-preferences", named.path)
        val body = Json.parseToJsonElement(named.body).jsonObject
        assertEquals(setOf("preferences", "expected_user_id"), body.keys)
        assertEquals("user-a", body["expected_user_id"]!!.jsonPrimitive.content)
        assertEquals(setOf("preferences"), Json.parseToJsonElement(unknown.body).jsonObject.keys)
    }

    // ------------------------------------------------------------ Home card

    private val eligible = PushUiState(
        supportedInBuild = true,
        serverDelivers = true,
        serverHasInstallations = true,
        runtimePermission = true,
        granted = false
    )

    private val allowed = eligible.copy(granted = true, canPost = true)

    @Test
    fun `the card asks where the system still can`() {
        assertTrue(eligible.showsPrompt(offersOn = false))
        assertTrue("offers already on: the permission is still worth asking", eligible.showsPrompt(offersOn = true))
        assertFalse("no Firebase in this build", eligible.copy(supportedInBuild = false).showsPrompt(false))
        assertFalse("the server cannot deliver", eligible.copy(serverDelivers = false).showsPrompt(false))
        assertFalse("denied for good", eligible.copy(permanentlyDenied = true).showsPrompt(false))
        assertFalse("answered", eligible.copy(promptDismissed = true).showsPrompt(false))
        assertFalse("switched off in the app", eligible.copy(notificationsEnabled = false).showsPrompt(false))
        assertTrue(
            "debug builds can force it",
            eligible.copy(supportedInBuild = false, promptDismissed = true, debugForced = true).showsPrompt(false)
        )
    }

    @Test
    fun `allowed notifications still leave the choice about offers to the account`() {
        assertTrue("granted for an earlier account", allowed.showsPrompt(offersOn = false))
        assertFalse("nothing left to choose", allowed.showsPrompt(offersOn = true))
        assertFalse("answered", allowed.copy(promptDismissed = true).showsPrompt(false))
        assertFalse("no categories on this server", allowed.copy(serverHasInstallations = false).showsPrompt(false))
        assertTrue(
            "before Android 13 only the choice is left",
            allowed.copy(runtimePermission = false).showsPrompt(false)
        )
        assertFalse(
            "granted, but switched off in system settings: nothing would arrive",
            eligible.copy(granted = true, canPost = false).showsPrompt(false)
        )
    }

    @Test
    fun `the card is answered per account`() {
        val store = PushStateStore(InMemoryPreferences())
        assertFalse(store.isPromptDismissed("first"))

        store.setPromptDismissed("first", true)
        assertTrue(store.isPromptDismissed("first"))
        assertFalse("another account has not answered it", store.isPromptDismissed("second"))
        assertFalse("nor has an unknown one", store.isPromptDismissed(null))

        store.setPromptDismissed(null, true)
        assertTrue(store.isPromptDismissed(null))
        assertFalse(store.isPromptDismissed("third"))
    }

    @Test
    fun `offers can be chosen before the categories have loaded`() = runBlocking {
        val api = FakePreferencesApi().apply { server = server + ("marketing" to false) }
        val repository = NotificationPreferencesRepository(api)

        assertFalse(repository.state.value.isLocked("marketing"))
        assertTrue(repository.state.value.isLocked("security"))
        assertTrue(repository.set("marketing", true))
        assertTrue(repository.state.value.isOn("marketing"))
        assertEquals(listOf(mapOf("marketing" to true)), api.updates)
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
        assertTrue(eligible.hasCategories)
        assertFalse(PushUiState().hasCategories)
    }
}
