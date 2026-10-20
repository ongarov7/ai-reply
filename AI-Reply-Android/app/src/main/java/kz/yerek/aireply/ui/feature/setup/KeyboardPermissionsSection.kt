package kz.yerek.aireply.ui.feature.setup

import android.content.Context
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.HelpOutline
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.ErrorOutline
import androidx.compose.material.icons.filled.Keyboard
import androidx.compose.material.icons.outlined.ContentPaste
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kz.yerek.aireply.R
import kz.yerek.aireply.analytics.ProductEvent
import kz.yerek.aireply.analytics.ProductEvents
import kz.yerek.aireply.keyboard.input.KeyboardStatus
import kz.yerek.aireply.push.NotificationPermission
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.common.ChecklistState
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.common.NavigationRow
import kz.yerek.aireply.ui.common.RowDividerIndented
import kz.yerek.aireply.ui.common.RowGroup
import kz.yerek.aireply.ui.common.evidenceTime
import kz.yerek.aireply.ui.common.rememberKeyboardCheck
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.RowDivider
import kz.yerek.aireply.ui.design.Spacing
import kz.yerek.aireply.ui.feature.settings.rememberNotificationPermissionRequest
import kz.yerek.aireply.ui.navigation.Routes

/**
 * «Настройка клавиатуры и разрешений»: enabled, default, paste, notifications
 * and when this was last checked, each unresolved line with the one action
 * that resolves it. In Settings and on its own screen (aireply://permissions).
 *
 * Пернетақта мен рұқсаттар: әр шешілмеген жолда бір әрекет бар.
 *
 * Everything is re-read when the app comes back from system settings, when
 * the keyboard picker closes and on «Проверить снова».
 */
@Composable
fun KeyboardPermissionsContent(onOpen: (String) -> Unit) {
    var helping by rememberSaveable { mutableStateOf(false) }

    KeyboardPermissionsCard(onOpen)
    RowGroup {
        NavigationRow(Icons.Filled.Keyboard, stringResource(R.string.settings_setup_guide)) {
            onOpen(Routes.KeyboardSetup)
        }
        RowDividerIndented()
        NavigationRow(Icons.Outlined.ContentPaste, stringResource(R.string.paste_help_title)) {
            onOpen(Routes.PasteHelp)
        }
        RowDividerIndented()
        NavigationRow(Icons.AutoMirrored.Outlined.HelpOutline, stringResource(R.string.permissions_help)) {
            helping = true
        }
    }
    Footnote(stringResource(R.string.permissions_footer))

    if (helping) {
        KeyboardHelpSheet(
            onDismiss = { helping = false },
            onPasteHelp = {
                helping = false
                onOpen(Routes.PasteHelp)
            }
        )
    }
}

@Composable
private fun KeyboardPermissionsCard(onOpen: (String) -> Unit) {
    val services = LocalServices.current
    val context = LocalContext.current
    val check = rememberKeyboardCheck()
    val current = check.current
    val push by services.push.ui.collectAsStateWithLifecycle()
    // Re-read with every check: a paste in the keyboard happens while this screen is away.
    val pasteAt = remember(current) { services.deviceState.lastSuccessfulPasteAt }
    val rows = PermissionStatusModel.rows(current.status, pasteAt, NotificationFacts.of(push))
    val requestNotifications = rememberNotificationPermissionRequest(fromPrompt = false)

    AppCard {
        Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
            rows.forEach { row ->
                PermissionLine(row) { action ->
                    when (action) {
                        PermissionAction.OPEN_KEYBOARD_SETTINGS -> openKeyboardSettings(context)
                        PermissionAction.CHOOSE_KEYBOARD -> KeyboardStatus.showKeyboardPicker(context)
                        PermissionAction.PASTE_HELP -> onOpen(Routes.PasteHelp)
                        PermissionAction.ALLOW_NOTIFICATIONS -> requestNotifications()
                        PermissionAction.OPEN_NOTIFICATION_SETTINGS -> NotificationPermission.openSystemSettings(context)
                    }
                }
                RowDivider()
            }
            StatusLine(
                icon = Icons.Outlined.Info,
                tint = LocalExtraColors.current.textSecondary,
                title = stringResource(R.string.permissions_last_check),
                value = evidenceTime(current.checkedAt, standalone = true),
                action = stringResource(R.string.permissions_recheck),
                onAction = check::refresh
            )
        }
    }
}

/** Opens the system keyboard list; counted, so the funnel shows who got there. */
internal fun openKeyboardSettings(context: Context) {
    ProductEvents.track(ProductEvent.KEYBOARD_SETTINGS_OPENED)
    KeyboardStatus.openKeyboardSettings(context)
}

@Composable
private fun PermissionLine(row: PermissionRow, onAction: (PermissionAction) -> Unit) {
    val extras = LocalExtraColors.current
    val (icon, tint) = when (row.state) {
        ChecklistState.DONE -> Icons.Filled.CheckCircle to extras.success
        ChecklistState.MISSING -> Icons.Filled.ErrorOutline to extras.warning
        ChecklistState.UNKNOWN -> Icons.AutoMirrored.Outlined.HelpOutline to extras.textSecondary
    }
    StatusLine(
        icon = icon,
        tint = tint,
        title = stringResource(title(row.kind)),
        value = valueText(row),
        action = row.action?.let { stringResource(actionLabel(it)) },
        onAction = { row.action?.let(onAction) }
    )
}

/** Icon, title, status, and the action under the status, aligned with the text. */
@Composable
private fun StatusLine(
    icon: ImageVector,
    tint: Color,
    title: String,
    value: String,
    action: String?,
    onAction: () -> Unit
) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        verticalAlignment = Alignment.Top,
        horizontalArrangement = Arrangement.spacedBy(Spacing.s)
    ) {
        Icon(icon, contentDescription = null, tint = tint, modifier = Modifier.padding(top = 1.dp).size(22.dp))
        Column(modifier = Modifier.weight(1f)) {
            Column(modifier = Modifier.semantics(mergeDescendants = true) {}) {
                Text(title, style = MaterialTheme.typography.bodyLarge)
                Text(value, style = MaterialTheme.typography.bodySmall, color = LocalExtraColors.current.textSecondary)
            }
            if (action != null) {
                // Under the status: a long label never squeezes the title.
                TextButton(onClick = onAction, contentPadding = PaddingValues(horizontal = 0.dp)) {
                    Text(action)
                }
            }
        }
    }
}

private fun title(kind: PermissionKind): Int = when (kind) {
    PermissionKind.KEYBOARD_ENABLED -> R.string.permissions_keyboard_enabled
    PermissionKind.KEYBOARD_DEFAULT -> R.string.permissions_keyboard_default
    PermissionKind.PASTE -> R.string.permissions_paste
    PermissionKind.NOTIFICATIONS -> R.string.permissions_notifications
}

@Composable
private fun valueText(row: PermissionRow): String = when (row.value) {
    PermissionValue.YES -> stringResource(R.string.permissions_value_yes)
    PermissionValue.NOT_ENABLED -> stringResource(R.string.permissions_value_not_enabled)
    PermissionValue.ENABLE_FIRST -> stringResource(R.string.permissions_value_enable_first)
    PermissionValue.OTHER_KEYBOARD -> stringResource(R.string.permissions_value_other_keyboard)
    PermissionValue.PASTE_SEEN -> stringResource(R.string.permissions_value_paste_seen, evidenceTime(row.at ?: 0L))
    PermissionValue.PASTE_NOT_YET -> stringResource(R.string.permissions_value_paste_not_yet)
    PermissionValue.NOTIFICATIONS_ALLOWED -> stringResource(R.string.push_settings_status_allowed)
    PermissionValue.NOTIFICATIONS_BLOCKED -> stringResource(R.string.permissions_value_notifications_off)
    PermissionValue.NOTIFICATIONS_NOT_ASKED -> stringResource(R.string.push_settings_status_not_asked)
}

private fun actionLabel(action: PermissionAction): Int = when (action) {
    PermissionAction.OPEN_KEYBOARD_SETTINGS -> R.string.permissions_action_keyboard_settings
    PermissionAction.CHOOSE_KEYBOARD -> R.string.permissions_action_choose
    PermissionAction.PASTE_HELP -> R.string.permissions_action_paste_help
    PermissionAction.ALLOW_NOTIFICATIONS -> R.string.push_settings_allow
    PermissionAction.OPEN_NOTIFICATION_SETTINGS -> R.string.push_settings_open_system
}
