package kz.yerek.aireply

import kz.yerek.aireply.keyboard.input.KeyboardStatus
import kz.yerek.aireply.push.NotificationPermission
import kz.yerek.aireply.push.PushUiState
import kz.yerek.aireply.ui.common.ChecklistState
import kz.yerek.aireply.ui.common.EvidenceTime
import kz.yerek.aireply.ui.feature.setup.NotificationFacts
import kz.yerek.aireply.ui.feature.setup.PermissionAction
import kz.yerek.aireply.ui.feature.setup.PermissionKind
import kz.yerek.aireply.ui.feature.setup.PermissionRow
import kz.yerek.aireply.ui.feature.setup.PermissionStatusModel
import kz.yerek.aireply.ui.feature.setup.PermissionValue
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.ZoneId
import java.time.ZonedDateTime
import java.util.Locale

/**
 * «Настройка клавиатуры и разрешений»: each line says only what Android
 * reported or what this phone has seen, and an unresolved line has one action.
 *
 * Рұқсаттар бөлімі: тек жүйе айтқаны мен көрілген дәлел.
 */
class PermissionStatusTest {

    private val ready = KeyboardStatus.Snapshot(isEnabled = true, isSelected = true)
    private val off = KeyboardStatus.Snapshot(isEnabled = false, isSelected = false)
    private val enabledOnly = KeyboardStatus.Snapshot(isEnabled = true, isSelected = false)

    private fun row(rows: List<PermissionRow>, kind: PermissionKind) = rows.single { it.kind == kind }

    @Test
    fun `a keyboard that is on and chosen asks for nothing`() {
        val rows = PermissionStatusModel.rows(ready, lastPasteAt = 1_000L, notifications = null)
        assertEquals(listOf(PermissionKind.KEYBOARD_ENABLED, PermissionKind.KEYBOARD_DEFAULT, PermissionKind.PASTE), rows.map { it.kind })
        rows.forEach { assertEquals(it.kind.name, ChecklistState.DONE, it.state); assertNull(it.kind.name, it.action) }
        assertEquals(1_000L, row(rows, PermissionKind.PASTE).at)
    }

    @Test
    fun `a keyboard that is off is turned on in settings before it can be chosen`() {
        val rows = PermissionStatusModel.rows(off, lastPasteAt = null, notifications = null)
        assertEquals(PermissionAction.OPEN_KEYBOARD_SETTINGS, row(rows, PermissionKind.KEYBOARD_ENABLED).action)
        val default = row(rows, PermissionKind.KEYBOARD_DEFAULT)
        assertEquals(PermissionValue.ENABLE_FIRST, default.value)
        assertNull("the picker lists only enabled keyboards", default.action)
    }

    @Test
    fun `an enabled keyboard that is not the default is chosen in the picker`() {
        val default = row(PermissionStatusModel.rows(enabledOnly, null, null), PermissionKind.KEYBOARD_DEFAULT)
        assertEquals(PermissionValue.OTHER_KEYBOARD, default.value)
        assertEquals(PermissionAction.CHOOSE_KEYBOARD, default.action)
    }

    @Test
    fun `paste is not checked until a paste was seen, never off`() {
        val paste = row(PermissionStatusModel.rows(ready, null, null), PermissionKind.PASTE)
        assertEquals(ChecklistState.UNKNOWN, paste.state)
        assertEquals(PermissionValue.PASTE_NOT_YET, paste.value)
        assertEquals(PermissionAction.PASTE_HELP, paste.action)
    }

    @Test
    fun `notifications follow the permission and the system switch`() {
        fun facts(canPost: Boolean, permission: String, granted: Boolean, permanentlyDenied: Boolean = false, runtime: Boolean = true) =
            NotificationFacts.of(
                PushUiState(
                    supportedInBuild = true,
                    serverDelivers = true,
                    runtimePermission = runtime,
                    granted = granted,
                    canPost = canPost,
                    permission = permission,
                    permanentlyDenied = permanentlyDenied
                )
            )
        fun line(facts: NotificationFacts?) = row(PermissionStatusModel.rows(ready, null, facts), PermissionKind.NOTIFICATIONS)

        val allowed = line(facts(canPost = true, permission = NotificationPermission.AUTHORIZED, granted = true))
        assertEquals(PermissionValue.NOTIFICATIONS_ALLOWED, allowed.value)
        assertNull(allowed.action)

        val notAsked = line(facts(canPost = false, permission = NotificationPermission.NOT_DETERMINED, granted = false))
        assertEquals(ChecklistState.UNKNOWN, notAsked.state)
        assertEquals(PermissionAction.ALLOW_NOTIFICATIONS, notAsked.action)

        val refusedForGood = line(
            facts(canPost = false, permission = NotificationPermission.DENIED, granted = false, permanentlyDenied = true)
        )
        assertEquals(PermissionValue.NOTIFICATIONS_BLOCKED, refusedForGood.value)
        assertEquals("only system settings can help", PermissionAction.OPEN_NOTIFICATION_SETTINGS, refusedForGood.action)

        val switchedOffBefore13 = line(
            facts(canPost = false, permission = NotificationPermission.DENIED, granted = true, runtime = false)
        )
        assertEquals(PermissionAction.OPEN_NOTIFICATION_SETTINGS, switchedOffBefore13.action)
    }

    @Test
    fun `no notification line where push cannot work`() {
        assertNull(NotificationFacts.of(PushUiState(supportedInBuild = true, serverDelivers = false)))
        val rows = PermissionStatusModel.rows(ready, null, NotificationFacts.of(PushUiState()))
        assertTrue(rows.none { it.kind == PermissionKind.NOTIFICATIONS })
    }

    @Test
    fun `a time of today is said as today, another day with its date`() {
        val zone = ZoneId.of("Asia/Almaty")
        val now = ZonedDateTime.of(2026, 10, 20, 18, 0, 0, 0, zone).toInstant().toEpochMilli()
        val morning = ZonedDateTime.of(2026, 10, 20, 9, 5, 0, 0, zone).toInstant().toEpochMilli()
        val yesterday = ZonedDateTime.of(2026, 10, 19, 23, 30, 0, 0, zone).toInstant().toEpochMilli()

        assertEquals("Today, 9:05 AM".replace(' ', ' '), EvidenceTime.format(morning, now, Locale.US, zone) { "Today, $it" }.replace(' ', ' '))
        val russian = EvidenceTime.format(yesterday, now, Locale.forLanguageTag("ru"), zone) { "Сегодня, $it" }
        assertTrue(russian, russian.contains("2026") && russian.contains("23:30"))
    }
}
