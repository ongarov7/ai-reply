package kz.yerek.aireply.ui.feature.setup

import kz.yerek.aireply.keyboard.input.KeyboardStatus
import kz.yerek.aireply.push.NotificationPermission
import kz.yerek.aireply.push.PushUiState
import kz.yerek.aireply.ui.common.ChecklistState

/** One line of «Настройка клавиатуры и разрешений». */
enum class PermissionKind { KEYBOARD_ENABLED, KEYBOARD_DEFAULT, PASTE, NOTIFICATIONS }

/** What a line says. Each one is a sentence of its own, never a guess dressed as a fact. */
enum class PermissionValue {
    YES,
    NOT_ENABLED,
    ENABLE_FIRST,
    OTHER_KEYBOARD,
    PASTE_SEEN,
    PASTE_NOT_YET,
    NOTIFICATIONS_ALLOWED,
    NOTIFICATIONS_BLOCKED,
    NOTIFICATIONS_NOT_ASKED
}

/** The one thing to do about a line that is not resolved. */
enum class PermissionAction {
    OPEN_KEYBOARD_SETTINGS,
    CHOOSE_KEYBOARD,
    PASTE_HELP,
    ALLOW_NOTIFICATIONS,
    OPEN_NOTIFICATION_SETTINGS
}

data class PermissionRow(
    val kind: PermissionKind,
    val state: ChecklistState,
    val value: PermissionValue,
    /** Null once resolved: a working line asks for nothing. */
    val action: PermissionAction?,
    /** For [PermissionValue.PASTE_SEEN]: when, in epoch milliseconds. */
    val at: Long? = null
)

/** The notification facts the section needs, from [PushUiState]. */
data class NotificationFacts(val canPost: Boolean, val notAsked: Boolean, val canAskSystem: Boolean) {
    companion object {
        /** Null where push cannot work at all (no Firebase, or a server that cannot send): no line then. */
        fun of(ui: PushUiState): NotificationFacts? = if (!ui.isAvailable) {
            null
        } else {
            NotificationFacts(
                canPost = ui.canPost,
                notAsked = ui.permission == NotificationPermission.NOT_DETERMINED,
                canAskSystem = ui.canAskSystem
            )
        }
    }
}

/**
 * Turns what Android reports into the section's lines.
 *
 * Пернетақта, қою және хабарлама күйлері: тек жүйе айтқаны немесе көрілген дәлел.
 *
 * Android answers "enabled" and "default" itself, so those are facts. Paste
 * has no system answer: it is "works" only after a paste this phone has seen,
 * and "not checked yet" until then - never "off". Notifications are the
 * runtime permission on Android 13+ and the app's switch in system settings
 * on every version.
 */
object PermissionStatusModel {

    fun rows(keyboard: KeyboardStatus.Snapshot, lastPasteAt: Long?, notifications: NotificationFacts?): List<PermissionRow> =
        listOfNotNull(
            if (keyboard.isEnabled) {
                PermissionRow(PermissionKind.KEYBOARD_ENABLED, ChecklistState.DONE, PermissionValue.YES, null)
            } else {
                PermissionRow(
                    PermissionKind.KEYBOARD_ENABLED,
                    ChecklistState.MISSING,
                    PermissionValue.NOT_ENABLED,
                    PermissionAction.OPEN_KEYBOARD_SETTINGS
                )
            },
            when {
                keyboard.isSelected ->
                    PermissionRow(PermissionKind.KEYBOARD_DEFAULT, ChecklistState.DONE, PermissionValue.YES, null)
                // The picker lists only enabled keyboards: the line above has the action.
                !keyboard.isEnabled ->
                    PermissionRow(PermissionKind.KEYBOARD_DEFAULT, ChecklistState.MISSING, PermissionValue.ENABLE_FIRST, null)
                else -> PermissionRow(
                    PermissionKind.KEYBOARD_DEFAULT,
                    ChecklistState.MISSING,
                    PermissionValue.OTHER_KEYBOARD,
                    PermissionAction.CHOOSE_KEYBOARD
                )
            },
            if (lastPasteAt != null) {
                PermissionRow(PermissionKind.PASTE, ChecklistState.DONE, PermissionValue.PASTE_SEEN, null, at = lastPasteAt)
            } else {
                PermissionRow(PermissionKind.PASTE, ChecklistState.UNKNOWN, PermissionValue.PASTE_NOT_YET, PermissionAction.PASTE_HELP)
            },
            notifications?.let(::notificationRow)
        )

    private fun notificationRow(facts: NotificationFacts): PermissionRow = when {
        facts.canPost -> PermissionRow(
            PermissionKind.NOTIFICATIONS,
            ChecklistState.DONE,
            PermissionValue.NOTIFICATIONS_ALLOWED,
            null
        )
        facts.notAsked && facts.canAskSystem -> PermissionRow(
            PermissionKind.NOTIFICATIONS,
            ChecklistState.UNKNOWN,
            PermissionValue.NOTIFICATIONS_NOT_ASKED,
            PermissionAction.ALLOW_NOTIFICATIONS
        )
        else -> PermissionRow(
            PermissionKind.NOTIFICATIONS,
            ChecklistState.MISSING,
            PermissionValue.NOTIFICATIONS_BLOCKED,
            if (facts.canAskSystem) PermissionAction.ALLOW_NOTIFICATIONS else PermissionAction.OPEN_NOTIFICATION_SETTINGS
        )
    }
}
