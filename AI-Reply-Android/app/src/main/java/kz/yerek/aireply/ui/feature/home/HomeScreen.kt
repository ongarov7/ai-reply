package kz.yerek.aireply.ui.feature.home

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.AutoAwesome
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Keyboard
import androidx.compose.material.icons.filled.Schedule
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.outlined.ChatBubbleOutline
import androidx.compose.material.icons.outlined.PersonOutline
import androidx.compose.material3.BadgedBox
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import kz.yerek.aireply.R
import kz.yerek.aireply.debug.DebugSamples
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.common.AppScreen
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.common.NavigationRow
import kz.yerek.aireply.ui.common.RowDividerIndented
import kz.yerek.aireply.ui.common.RowGroup
import kz.yerek.aireply.ui.common.UnreadBadge
import kz.yerek.aireply.ui.common.privacyStatement
import kz.yerek.aireply.ui.common.rememberKeyboardStatus
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.AppMark
import kz.yerek.aireply.ui.design.AppSection
import kz.yerek.aireply.ui.design.Layout
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.Radius
import kz.yerek.aireply.ui.design.ReadableColumn
import kz.yerek.aireply.ui.feature.setup.KeyboardSetupAction
import kz.yerek.aireply.ui.design.Spacing
import kz.yerek.aireply.ui.design.StepRow
import kz.yerek.aireply.ui.feature.account.BalanceRows
import kz.yerek.aireply.ui.feature.account.BalanceSummary
import kz.yerek.aireply.ui.feature.account.hasExtraRows
import kz.yerek.aireply.ui.feature.account.headlineNumber
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
    val unread by services.support.unread.collectAsStateWithLifecycle()

    AppScreen(
        title = stringResource(R.string.home_title),
        actions = {
            IconButton(onClick = { onOpen(Routes.Settings) }) {
                // An operator's unread reply waits in Settings ▸ Chat with support.
                BadgedBox(badge = { if (unread > 0) UnreadBadge() }) {
                    Icon(
                        Icons.Filled.Settings,
                        contentDescription = if (unread > 0) {
                            stringResource(R.string.cd_settings) + ", " + pluralStringResource(R.plurals.support_unread, unread, unread)
                        } else {
                            stringResource(R.string.cd_settings)
                        }
                    )
                }
            }
        }
    ) {
        ReadableColumn {

            Row(
                horizontalArrangement = Arrangement.spacedBy(Spacing.m),
                verticalAlignment = Alignment.Top
            ) {
                AppMark(size = 64.dp)
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

            Text(
                stringResource(R.string.home_description),
                style = MaterialTheme.typography.bodyLarge,
                color = LocalExtraColors.current.textSecondary
            )

            UsageCard(onOpen = onOpen)

            AppUpdateCard()

            NotificationPromptCard()

            CreateReplyButton { onOpen(Routes.Compose) }

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

                        // ONE action, for the step that is actually missing, and
                        // none once the keyboard is on and chosen. The guide is
                        // still in Settings ▸ Keyboard setup.
                        KeyboardSetupAction(status, context)
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
                AppCard { Footnote(privacyStatement()) }
            }
        }
    }
}

/** The way into «Создать ответ»: the screen's main action. */
@Composable
private fun CreateReplyButton(onClick: () -> Unit) {
    Button(
        onClick = onClick,
        shape = RoundedCornerShape(Radius.medium),
        colors = ButtonDefaults.buttonColors(containerColor = MaterialTheme.colorScheme.primary),
        modifier = Modifier.fillMaxWidth().defaultMinSize(minHeight = Layout.minimumTouchTarget)
    ) {
        Icon(Icons.Filled.AutoAwesome, contentDescription = null, modifier = Modifier.size(20.dp))
        Spacer(Modifier.width(Spacing.xs))
        Text(stringResource(R.string.home_tryit), style = MaterialTheme.typography.labelLarge, modifier = Modifier.weight(1f))
        Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, contentDescription = null, modifier = Modifier.size(20.dp))
    }
}

/**
 * What can be used now and where it comes from, and a way to the plans.
 *
 * Қолжетімді жауаптар — серверден, кештен емес.
 *
 * Shown only for accounts, because only the server knows the numbers: a dash
 * until it has answered, and a spent part is a number like any other, never a
 * warning. The cached value renders instantly and the fresh one replaces it.
 */
@Composable
private fun UsageCard(onOpen: (String) -> Unit) {
    val services = LocalServices.current
    val state by services.account.state.collectAsStateWithLifecycle()
    val language = services.settings.effectiveAppLanguage.code

    LaunchedEffect(Unit) { services.account.refresh() }

    // DEBUG builds with the sample data: the card as a server with the balance would fill it.
    val usage = if (services.sampleData) DebugSamples.usage else state.usage
    if (!state.isSignedIn && !services.sampleData) return

    val balance = BalanceSummary.of(usage, services.usageCache.current())
    val plan = state.subscription?.plan
    val welcome = balance.welcomeHeadline?.takeIf { plan?.isFree == true || services.sampleData }

    AppCard(modifier = Modifier.clickable { onOpen(Routes.Subscription) }) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(Spacing.m),
            verticalAlignment = Alignment.CenterVertically
        ) {
            Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(Spacing.xxs)) {
                Text(
                    if (welcome != null) {
                        stringResource(R.string.balance_welcome_title, welcome.remaining, welcome.total)
                    } else {
                        stringResource(R.string.home_usage_available)
                    },
                    style = MaterialTheme.typography.titleSmall
                )
                plan?.localizedName(language)?.takeIf(String::isNotEmpty)?.let {
                    Text(it, style = MaterialTheme.typography.bodySmall, color = LocalExtraColors.current.textSecondary)
                }
            }
            Column(horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(Spacing.xxs)) {
                Text(
                    balance.headlineNumber(usage.isKnown),
                    style = MaterialTheme.typography.headlineLarge,
                    fontWeight = FontWeight.SemiBold,
                    color = MaterialTheme.colorScheme.primary
                )
                if (usage.isKnown && !balance.unlimited) {
                    Text(
                        stringResource(R.string.home_usage_left_short),
                        style = MaterialTheme.typography.labelSmall,
                        color = LocalExtraColors.current.textSecondary
                    )
                }
            }
            Icon(
                Icons.AutoMirrored.Filled.KeyboardArrowRight,
                contentDescription = null,
                tint = LocalExtraColors.current.textTertiary,
                modifier = Modifier.size(20.dp)
            )
        }
        if (balance.hasExtraRows) {
            HorizontalDivider(modifier = Modifier.padding(top = Spacing.xs))
            BalanceRows(balance, extraOnly = true)
        }
    }
}
