package kz.yerek.aireply.ui.feature.home

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.AutoAwesome
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Keyboard
import androidx.compose.material.icons.filled.Schedule
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.outlined.ChatBubbleOutline
import androidx.compose.material.icons.outlined.PersonOutline
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import kz.yerek.aireply.R
import kz.yerek.aireply.keyboard.input.KeyboardStatus
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.common.AppScreen
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.common.NavigationRow
import kz.yerek.aireply.ui.common.RowDividerIndented
import kz.yerek.aireply.ui.common.RowGroup
import kz.yerek.aireply.ui.common.rememberKeyboardStatus
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.AppMark
import kz.yerek.aireply.ui.design.AppSection
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.ReadableColumn
import kz.yerek.aireply.ui.design.SecondaryButton
import kz.yerek.aireply.ui.design.Spacing
import kz.yerek.aireply.ui.design.StepRow
import kz.yerek.aireply.ui.navigation.Routes
import androidx.compose.runtime.LaunchedEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle

/**
 * The root screen.
 *
 * Section order matches iOS: mark and subtitle, a reply test, account usage,
 * the notifications card (only when it applies), profile links, keyboard
 * status, workflow and privacy.
 */
@Composable
fun HomeScreen(onOpen: (String) -> Unit) {
    val services = LocalServices.current
    val context = LocalContext.current
    val status by rememberKeyboardStatus()

    AppScreen(
        title = stringResource(R.string.home_title),
        actions = {
            IconButton(onClick = { onOpen(Routes.Settings) }) {
                Icon(
                    Icons.Filled.Settings,
                    contentDescription = stringResource(R.string.cd_settings)
                )
            }
        }
    ) {
        ReadableColumn {

            Row(
                horizontalArrangement = Arrangement.spacedBy(Spacing.m),
                verticalAlignment = Alignment.Top
            ) {
                AppMark(size = 56.dp)
                Column(verticalArrangement = Arrangement.spacedBy(Spacing.xxs)) {
                    Text(
                        stringResource(R.string.home_title),
                        style = MaterialTheme.typography.headlineLarge
                    )
                    Text(
                        stringResource(R.string.home_subtitle),
                        style = MaterialTheme.typography.titleSmall,
                        color = LocalExtraColors.current.textSecondary
                    )
                }
            }

            AppCard(modifier = Modifier.clickable { onOpen(Routes.Compose) }) {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.spacedBy(Spacing.s),
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Icon(
                        Icons.Filled.AutoAwesome,
                        contentDescription = null,
                        tint = MaterialTheme.colorScheme.primary,
                        modifier = Modifier.size(22.dp)
                    )
                    Text(stringResource(R.string.home_tryit), style = MaterialTheme.typography.bodyLarge)
                    Spacer(Modifier.weight(1f))
                    Icon(
                        Icons.AutoMirrored.Filled.KeyboardArrowRight,
                        contentDescription = null,
                        tint = LocalExtraColors.current.textTertiary,
                        modifier = Modifier.size(20.dp)
                    )
                }
            }

            UsageCard(onOpen = onOpen)

            NotificationPromptCard()

            AppSection(stringResource(R.string.home_profile_title)) {
                RowGroup {
                    NavigationRow(
                        Icons.Outlined.PersonOutline,
                        stringResource(R.string.home_profile_edit)
                    ) { onOpen(Routes.Profile) }
                    RowDividerIndented()
                    NavigationRow(
                        Icons.Outlined.ChatBubbleOutline,
                        stringResource(R.string.home_profile_templates)
                    ) { onOpen(Routes.Templates) }
                    RowDividerIndented()
                    NavigationRow(
                        Icons.Filled.Schedule,
                        stringResource(R.string.home_profile_hours)
                    ) { onOpen(Routes.WorkingHours) }
                }
            }

            AppSection(stringResource(R.string.home_keyboard_title)) {
                AppCard {
                    Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
                        Row(
                            horizontalArrangement = Arrangement.spacedBy(Spacing.s),
                            verticalAlignment = Alignment.CenterVertically
                        ) {
                            Icon(
                                if (status.isReady) Icons.Filled.CheckCircle else Icons.Filled.Keyboard,
                                contentDescription = null,
                                tint = if (status.isReady) {
                                    LocalExtraColors.current.success
                                } else {
                                    LocalExtraColors.current.textSecondary
                                },
                                modifier = Modifier.size(22.dp)
                            )
                            Text(
                                stringResource(
                                    when {
                                        status.isReady -> R.string.android_keyboard_ready
                                        status.isEnabled -> R.string.android_keyboard_enabled_not_selected
                                        else -> R.string.android_keyboard_not_enabled
                                    }
                                ),
                                style = MaterialTheme.typography.bodyMedium
                            )
                        }

                        if (!status.isReady) {
                            StepRow(1, stringResource(R.string.android_step_enable))
                            StepRow(2, stringResource(R.string.android_step_select))
                        }

                        Row(horizontalArrangement = Arrangement.spacedBy(Spacing.s)) {
                            SecondaryButton(
                                text = stringResource(R.string.android_open_keyboard_settings),
                                onClick = { KeyboardStatus.openKeyboardSettings(context) }
                            )
                            SecondaryButton(
                                text = stringResource(R.string.settings_setup_guide),
                                onClick = { onOpen(Routes.KeyboardSetup) }
                            )
                        }
                    }
                }
            }

            AppSection(stringResource(R.string.home_howitworks_title)) {
                AppCard {
                    Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
                        Text(
                            stringResource(R.string.home_howitworks_body),
                            style = MaterialTheme.typography.bodyLarge
                        )
                        Footnote(stringResource(R.string.android_howitworks_clipboard))
                    }
                }
            }

            AppSection(stringResource(R.string.home_privacy_title)) {
                AppCard { Footnote(stringResource(R.string.settings_privacy_body)) }
            }
        }
    }
}

/**
 * What is left today, and a way to the plans.
 *
 * Бүгін неше жауап қалғаны — серверден, кештен емес.
 *
 * Shown only for accounts, because only the server knows the number. The cached
 * value renders instantly and the fresh one replaces it a moment later.
 */
@Composable
private fun UsageCard(onOpen: (String) -> Unit) {
    val services = LocalServices.current
    val state by services.account.state.collectAsStateWithLifecycle()
    val language = services.settings.effectiveAppLanguage.code

    LaunchedEffect(Unit) { services.account.refresh() }

    if (!state.isSignedIn) return

    val cached = services.usageCache.current()
    val remaining = if (state.usage.dailyLimit > 0) state.usage.remainingToday else cached.remainingToday
    val limit = if (state.usage.dailyLimit > 0) state.usage.dailyLimit else cached.dailyLimit

    AppCard(modifier = Modifier.clickable { onOpen(Routes.Subscription) }) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically
        ) {
            Column(verticalArrangement = Arrangement.spacedBy(Spacing.xxs)) {
                Text(
                    stringResource(R.string.home_usage_title),
                    style = MaterialTheme.typography.titleSmall
                )
                Text(
                    state.subscription?.plan?.localizedName(language).orEmpty(),
                    style = MaterialTheme.typography.bodySmall,
                    color = LocalExtraColors.current.textSecondary
                )
            }
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(horizontalAlignment = Alignment.End) {
                    Text(
                        remaining.toString(),
                        style = MaterialTheme.typography.headlineSmall,
                        color = if (remaining > 0) MaterialTheme.colorScheme.primary
                        else MaterialTheme.colorScheme.error
                    )
                    Text(
                        stringResource(R.string.home_usage_left, limit),
                        style = MaterialTheme.typography.labelSmall,
                        color = LocalExtraColors.current.textSecondary
                    )
                }
                Spacer(Modifier.size(Spacing.xs))
                Icon(
                    Icons.AutoMirrored.Filled.KeyboardArrowRight,
                    contentDescription = null,
                    tint = LocalExtraColors.current.textTertiary,
                    modifier = Modifier.size(20.dp)
                )
            }
        }
    }
}
