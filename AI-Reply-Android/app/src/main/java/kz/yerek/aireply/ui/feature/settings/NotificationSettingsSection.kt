package kz.yerek.aireply.ui.feature.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Notifications
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import kz.yerek.aireply.R
import kz.yerek.aireply.push.NotificationPermission
import kz.yerek.aireply.push.PushUiState
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.common.NavigationRow
import kz.yerek.aireply.ui.common.RowDividerIndented
import kz.yerek.aireply.ui.common.RowGroup
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.AppSection
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.Spacing

/**
 * Settings ▸ Notifications.
 *
 * Хабарламалар: жүйе рұқсаты, қосымшадағы ауыстырғыш, санаттар.
 *
 * What the phone allows (with the way to change it), the app's own switch,
 * and the categories of a signed-in account (security always on). Where push
 * cannot work — a build without Firebase, a server without it — it says so
 * instead of showing switches that do nothing.
 */
@Composable
fun NotificationSettingsSection(modifier: Modifier = Modifier) {
    val services = LocalServices.current
    val push = services.push
    val ui by push.ui.collectAsStateWithLifecycle()
    val account by services.account.state.collectAsStateWithLifecycle()
    val categories by push.preferences.state.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val scope = rememberCoroutineScope()

    val requestPermission = rememberNotificationPermissionRequest(fromPrompt = false)

    val showsCategories = account.isSignedIn && ui.isAvailable && ui.notificationsEnabled &&
        (ui.serverHasInstallations || ui.debugForced)

    LaunchedEffect(showsCategories) {
        if (showsCategories && !categories.isLoaded && !categories.loading) push.preferences.load()
    }

    AppSection(stringResource(R.string.push_settings_title), modifier = modifier) {
        if (ui.isAvailable) {
            RowGroup {
                StatusRow(
                    label = stringResource(R.string.push_settings_system),
                    value = stringResource(statusText(ui))
                )
                if (!ui.canPost) {
                    RowDividerIndented()
                    if (ui.canAskSystem) {
                        NavigationRow(
                            Icons.Outlined.Notifications,
                            stringResource(R.string.push_settings_allow)
                        ) { requestPermission() }
                    } else {
                        NavigationRow(
                            Icons.Outlined.Settings,
                            stringResource(R.string.push_settings_open_system)
                        ) { NotificationPermission.openSystemSettings(context) }
                    }
                }
                RowDividerIndented()
                SwitchRow(
                    label = stringResource(R.string.push_settings_enabled),
                    checked = ui.notificationsEnabled,
                    enabled = true
                ) { checked -> push.setNotificationsEnabled(checked) }
            }
            Footnote(stringResource(R.string.push_settings_footer))

            if (showsCategories) {
                when {
                    categories.isLoaded -> {
                        RowGroup {
                            CATEGORIES.forEachIndexed { index, (category, label) ->
                                if (index > 0) RowDividerIndented()
                                val locked = categories.isLocked(category)
                                SwitchRow(
                                    label = stringResource(label),
                                    checked = locked || categories.isOn(category),
                                    enabled = !locked
                                ) { checked -> scope.launch { push.preferences.set(category, checked) } }
                            }
                        }
                        Footnote(
                            stringResource(
                                if (categories.failed) R.string.push_categories_failed
                                else R.string.push_categories_footer
                            )
                        )
                    }
                    categories.loading -> Footnote(stringResource(R.string.push_categories_loading))
                    else -> AppCard {
                        Footnote(stringResource(R.string.push_categories_failed))
                        TextButton(onClick = { scope.launch { push.preferences.load() } }) {
                            Text(stringResource(R.string.kb_retry))
                        }
                    }
                }
            }
        } else {
            AppCard { Footnote(stringResource(R.string.push_settings_unavailable)) }
        }
    }
}

/** Server categories in the server's display order; unknown future ones are not shown. */
private val CATEGORIES = listOf(
    "account" to R.string.push_category_account,
    "subscription" to R.string.push_category_subscription,
    "security" to R.string.push_category_security,
    "system" to R.string.push_category_system,
    "marketing" to R.string.push_category_marketing
)

private fun statusText(ui: PushUiState): Int = when {
    ui.canPost -> R.string.push_settings_status_allowed
    ui.permission == NotificationPermission.NOT_DETERMINED -> R.string.push_settings_status_not_asked
    else -> R.string.push_settings_status_blocked
}

@Composable
private fun StatusRow(label: String, value: String) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .defaultMinSize(minHeight = 48.dp)
            .padding(horizontal = Spacing.m, vertical = Spacing.s),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(Spacing.s)
    ) {
        Text(label, style = MaterialTheme.typography.bodyLarge)
        Spacer(Modifier.weight(1f))
        Text(
            value,
            style = MaterialTheme.typography.bodyMedium,
            color = LocalExtraColors.current.textSecondary
        )
    }
}
