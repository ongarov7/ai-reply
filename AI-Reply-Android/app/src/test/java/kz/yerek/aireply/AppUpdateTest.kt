package kz.yerek.aireply

import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kz.yerek.aireply.update.AppUpdateController
import kz.yerek.aireply.update.AppUpdatePolicy
import kz.yerek.aireply.update.UpdateCheck
import kz.yerek.aireply.update.UpdateStore
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The update card: one automatic check a day, «Позже» hides one version for a
 * week, a newer version shows at once, and nothing shows for the version
 * already installed.
 */
class AppUpdateTest {

    private class MemoryStore : UpdateStore {
        override var updateLastCheck: Long? = null
        override var updateOfferedVersion: Int? = null
        override var updateDismissedVersion: Int? = null
        override var updateDismissedAt: Long? = null
    }

    private val day = 24L * 60 * 60 * 1000
    private var now = 1_000_000_000L
    private var answer: UpdateCheck = UpdateCheck.Answered(9)
    private var calls = 0
    private val store = MemoryStore()

    private fun controller(enabled: Boolean = true) = AppUpdateController(
        source = { calls++; answer },
        store = store,
        installedVersion = 4,
        enabled = enabled,
        clock = { now }
    )

    @Test
    fun `the automatic check runs once a day`() {
        assertTrue(AppUpdatePolicy.isCheckDue(now, null))
        assertFalse(AppUpdatePolicy.isCheckDue(now + day - 1, now))
        assertTrue(AppUpdatePolicy.isCheckDue(now + day, now))
        // A clock set back does not lock the check out.
        assertTrue(AppUpdatePolicy.isCheckDue(now - 5, now))
    }

    @Test
    fun `later hides one version for a week, and a newer one shows at once`() {
        assertTrue(AppUpdatePolicy.showsCard(9, 4, null, null, now))
        assertFalse(AppUpdatePolicy.showsCard(9, 4, 9, now, now + 6 * day))
        assertTrue(AppUpdatePolicy.showsCard(9, 4, 9, now, now + 7 * day))
        assertTrue(AppUpdatePolicy.showsCard(10, 4, 9, now, now + day))
    }

    @Test
    fun `nothing shows for the installed version or an older one`() {
        assertFalse(AppUpdatePolicy.showsCard(4, 4, null, null, now))
        assertFalse(AppUpdatePolicy.showsCard(3, 4, null, null, now))
        assertFalse(AppUpdatePolicy.showsCard(null, 4, null, null, now))
    }

    @Test
    fun `a check that finds a version shows the card and keeps it for later`() = runBlocking {
        val updates = controller()
        updates.checkIfDue()
        assertTrue(updates.state.value.showsCard)
        assertEquals(9, store.updateOfferedVersion)
        // Second check the same day: no request.
        updates.checkIfDue()
        assertEquals(1, calls)
    }

    @Test
    fun `later dismisses the card and a manual check ignores the daily limit`() = runBlocking {
        val updates = controller()
        updates.checkIfDue()
        updates.remindLater()
        assertFalse(updates.state.value.showsCard)
        assertEquals(9, store.updateDismissedVersion)
        updates.checkNow()
        assertEquals(2, calls)
        assertFalse("the week still holds", updates.state.value.showsCard)
    }

    @Test
    fun `a manual check says the app is current, offline or unavailable`() = runBlocking {
        val updates = controller()
        answer = UpdateCheck.Answered(null)
        updates.checkNow()
        assertEquals(AppUpdateController.Status.CURRENT, updates.state.value.status)
        answer = UpdateCheck.Offline
        updates.checkNow()
        assertEquals(AppUpdateController.Status.OFFLINE, updates.state.value.status)
        answer = UpdateCheck.Failed
        updates.checkNow()
        assertEquals(AppUpdateController.Status.UNAVAILABLE, updates.state.value.status)
    }

    @Test
    fun `an automatic check that fails stays quiet and keeps the last offer`() = runBlocking {
        store.updateOfferedVersion = 9
        val updates = controller()
        answer = UpdateCheck.Failed
        updates.checkIfDue()
        assertEquals(AppUpdateController.Status.IDLE, updates.state.value.status)
        assertTrue(updates.state.value.showsCard)
    }

    @Test
    fun `a check cancelled halfway does not stay checking and does not spend the day`() = runBlocking {
        val before = now - 2 * day
        store.updateLastCheck = before
        var hanging = true
        val updates = AppUpdateController(
            source = {
                if (hanging) awaitCancellation()
                calls++
                answer
            },
            store = store,
            installedVersion = 4,
            enabled = true,
            clock = { now }
        )

        val check = launch(start = CoroutineStart.UNDISPATCHED) { updates.checkIfDue() }
        assertEquals(AppUpdateController.Status.CHECKING, updates.state.value.status)
        check.cancelAndJoin()

        assertEquals(AppUpdateController.Status.IDLE, updates.state.value.status)
        assertEquals(before, store.updateLastCheck)

        hanging = false
        updates.checkIfDue()
        assertEquals("the next look at Home asks again", 1, calls)
        assertTrue(updates.state.value.showsCard)
    }

    @Test
    fun `a debug build never asks the store`() = runBlocking {
        val updates = controller(enabled = false)
        updates.checkIfDue()
        assertEquals(0, calls)
    }
}
