package kz.yerek.aireply.ui.feature.account

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kz.yerek.aireply.R
import kz.yerek.aireply.debug.DebugSamples
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.common.AppScreen
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.ReadableColumn
import kz.yerek.aireply.ui.design.Spacing
import java.time.LocalDate
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import java.util.Locale

/**
 * «Баланс и доступ»: the current plan, everything that can be used now and
 * where it comes from, then the plans and packs.
 *
 * Тариф пен баланс: шешімді әрқашан сервер қабылдайды.
 *
 * The numbers come from the server on every appearance. The cached copy exists
 * only so the keyboard can render instantly; it is never the source of truth,
 * and a stale cache can only ever be pessimistic.
 */
@Composable
fun SubscriptionScreen(onBack: () -> Unit, onBuy: () -> Unit = {}) {
    val services = LocalServices.current
    val account = services.account
    val state by account.state.collectAsStateWithLifecycle()
    val appLanguage = services.settings.effectiveAppLanguage
    val language = appLanguage.code

    val usage = if (services.sampleData) DebugSamples.usage else state.usage
    val summary = UsageSummary.of(usage, services.usageCache.current())
    val balance = BalanceSummary.of(usage, services.usageCache.current())

    LaunchedEffect(Unit) { account.refresh() }

    AppScreen(title = stringResource(R.string.subscription_title), onBack = onBack) {
        ReadableColumn {
            AppCard {
                Column(verticalArrangement = Arrangement.spacedBy(Spacing.xs)) {
                    Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(Spacing.xxs)) {
                            Text(
                                stringResource(R.string.subscription_current),
                                style = MaterialTheme.typography.labelMedium,
                                color = LocalExtraColors.current.textSecondary
                            )
                            Text(
                                state.subscription?.plan?.localizedName(language) ?: "—",
                                style = MaterialTheme.typography.titleMedium,
                                fontWeight = FontWeight.SemiBold
                            )
                        }
                        Column(horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(Spacing.xxs)) {
                            Text(
                                balance.headlineNumber(usage.isKnown),
                                style = MaterialTheme.typography.displaySmall,
                                fontWeight = FontWeight.SemiBold,
                                color = if (usage.isKnown) MaterialTheme.colorScheme.onSurface else LocalExtraColors.current.textSecondary
                            )
                            Text(
                                stringResource(if (balance.unlimited) R.string.plans_unlimited else R.string.subscription_remaining),
                                style = MaterialTheme.typography.labelSmall,
                                color = LocalExtraColors.current.textSecondary
                            )
                        }
                    }

                    // Paid daily grants go to the permanent balance, so they are not a daily allowance.
                    if (usage.dailyCreditGrant > 0) {
                        Footnote(pluralStringResource(R.plurals.subscription_daily_accrual, usage.dailyCreditGrant, usage.dailyCreditGrant))
                    }

                    if (summary != null && summary.limit > 0 && !balance.unlimited) {
                        LinearProgressIndicator(
                            progress = { summary.used.toFloat() / summary.limit },
                            modifier = Modifier.fillMaxWidth().padding(top = Spacing.xxs),
                            trackColor = MaterialTheme.colorScheme.background,
                            drawStopIndicator = {}
                        )
                        Footnote(
                            stringResource(
                                if (summary.period == UsageSummary.Period.MONTH) R.string.subscription_used_month
                                else R.string.subscription_used_today,
                                summary.used,
                                summary.limit
                            )
                        )
                    }

                    if (balance.hasExtraRows) {
                        HorizontalDivider()
                        BalanceRows(balance, extraOnly = true)
                    }

                    if (usage.creditsNeverExpire) Footnote(stringResource(R.string.plans_replies_never_expire))

                    state.subscription?.let { subscription ->
                        subscription.expiresAt?.let { formatExpiry(it, appLanguage.locale) }?.let { expiry ->
                            Footnote(
                                if (subscription.status == "active" && subscription.autoRenews == true) {
                                    stringResource(R.string.plans_renews_on, expiry)
                                } else {
                                    stringResource(R.string.subscription_renews, expiry)
                                }
                            )
                        }
                        if (subscription.status == "active" && subscription.autoRenews == false) {
                            Footnote(stringResource(R.string.plans_renewal_off))
                        }
                    }
                }
            }

            PurchaseOptions(onOpenPlans = null)
        }
    }

    DisposableEffect(Unit) { onDispose { services.billing.clearNotice() } }
}

/** The large number on the balance cards: ∞, what is usable now, or a dash until the server has answered. */
internal fun BalanceSummary.headlineNumber(known: Boolean): String = when {
    !known && !hasBalance -> "—"
    unlimited -> "∞"
    else -> availableNow.toString()
}

/**
 * The plan's end date in the app's language ("8 Nov 2026", «8 нояб. 2026 г.»),
 * on this phone's calendar. Null when the server sent nothing readable.
 */
internal fun formatExpiry(raw: String, locale: Locale, zone: ZoneId = ZoneId.systemDefault()): String? {
    val value = raw.trim()
    if (value.isEmpty()) return null
    val date = runCatching { OffsetDateTime.parse(value).atZoneSameInstant(zone).toLocalDate() }.getOrNull()
        ?: runCatching { LocalDate.parse(value.take(10)) }.getOrNull()
        ?: return null
    return DateTimeFormatter.ofLocalizedDate(FormatStyle.MEDIUM).withLocale(locale).format(date)
}
