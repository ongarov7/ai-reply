package kz.yerek.aireply.ui.feature.account

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import kz.yerek.aireply.BuildConfig
import kz.yerek.aireply.R
import kz.yerek.aireply.data.account.PlanDto
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.common.AppScreen
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.AppSection
import kz.yerek.aireply.ui.design.PrimaryButton
import kz.yerek.aireply.ui.design.ReadableColumn
import kz.yerek.aireply.ui.design.Spacing
import java.time.LocalDate
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import java.util.Locale

/**
 * Current plan, what is left today, and the plans that can replace it.
 *
 * Тариф пен күндік квота: шешімді әрқашан сервер қабылдайды.
 *
 * The numbers come from the server on every appearance. The cached copy exists
 * only so the keyboard can render instantly; it is never the source of truth,
 * and a stale cache can only ever be pessimistic.
 *
 * Nothing here can be bought yet: there is no Google Play Billing in this
 * build, so a release build lists the plans without prices or a Choose
 * button. A debug build against a development server with demo checkout shows
 * them for a plan the server marks purchasable.
 */
@Composable
fun SubscriptionScreen(onBack: () -> Unit, onBuy: () -> Unit = {}) {
    val services = LocalServices.current
    val account = services.account
    val state by account.state.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    val appLanguage = services.settings.effectiveAppLanguage
    val language = appLanguage.code

    val summary = UsageSummary.of(state.usage, services.usageCache.current())
    val balance = BalanceSummary.of(state.usage)

    LaunchedEffect(Unit) {
        account.loadPlans()
        account.refresh()
    }

    AppScreen(title = stringResource(R.string.subscription_title), onBack = onBack) {
        ReadableColumn {
            AppCard {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.Top
                ) {
                    Column {
                        Text(
                            stringResource(R.string.subscription_current),
                            style = MaterialTheme.typography.labelMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant
                        )
                        Text(
                            state.subscription?.plan?.localizedName(language).orEmpty(),
                            style = MaterialTheme.typography.titleMedium
                        )
                    }
                    // A plan counted by the month shows the month; one not counted, no number.
                    if (summary != null) {
                        Column(horizontalAlignment = Alignment.End) {
                            Text(
                                summary.remaining.toString(),
                                style = MaterialTheme.typography.headlineMedium,
                                fontWeight = FontWeight.SemiBold,
                                color = if (summary.hasReplies) {
                                    MaterialTheme.colorScheme.primary
                                } else {
                                    MaterialTheme.colorScheme.onSurface
                                }
                            )
                            Text(
                                stringResource(
                                    if (summary.period == UsageSummary.Period.MONTH) R.string.subscription_remaining_month
                                    else R.string.subscription_remaining
                                ),
                                style = MaterialTheme.typography.labelSmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant
                            )
                        }
                    }
                }

                if (summary != null) {
                    LinearProgressIndicator(
                        progress = { summary.used.toFloat() / summary.limit },
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(top = Spacing.s),
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
                state.subscription?.expiresAt?.let { formatExpiry(it, appLanguage.locale) }?.let { expiry ->
                    Footnote(stringResource(R.string.subscription_renews, expiry))
                }
            }

            // What the account has beyond the plan, from a server that sends it.
            if (balance.hasBalance) {
                AppSection(stringResource(R.string.balance_title)) {
                    AppCard(contentPadding = PaddingValues(horizontal = Spacing.m, vertical = Spacing.xs)) {
                        BalanceRows(balance)
                    }
                }
            }

            // Packs and the pass, only where the server sells them on Android.
            if (services.purchasesOffered) {
                PrimaryButton(stringResource(R.string.purchase_title), onClick = onBuy)
            }

            // Only worth a section when there is something besides the current plan.
            if (state.plans.any { it.id != state.subscription?.plan?.id }) {
                AppSection(stringResource(R.string.subscription_available)) {
                    state.plans.forEach { plan -> PlanRow(plan, language, state.busy) { chosen ->
                        scope.launch { account.choosePlan(chosen) }
                    } }
                }
            }

            state.errorMessage?.let { message ->
                Text(
                    stringResource(message),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.error
                )
            }
        }
    }
}

@Composable
private fun PlanRow(
    plan: PlanDto,
    language: String,
    busy: Boolean,
    onChoose: (PlanDto) -> Unit
) {
    val services = LocalServices.current
    val state by services.account.state.collectAsStateWithLifecycle()
    val isCurrent = state.subscription?.plan?.id == plan.id
    val offersPurchase = canOfferPurchase(plan)

    AppCard(modifier = Modifier.padding(bottom = Spacing.s)) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween
        ) {
            Text(plan.localizedName(language), style = MaterialTheme.typography.titleSmall)
            // A price only next to a plan that can actually be bought here.
            if (offersPurchase) {
                Text(
                    plan.priceText,
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant
                )
            }
        }
        Text(
            plan.localizedDescription(language),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant
        )
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(top = Spacing.xs),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically
        ) {
            planAllowance(plan)?.let { allowance ->
                Text(
                    allowance,
                    style = MaterialTheme.typography.labelLarge,
                    color = MaterialTheme.colorScheme.primary
                )
            } ?: Spacer(Modifier.weight(1f))
            if (isCurrent) {
                Text(
                    stringResource(R.string.subscription_current_badge),
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.primary
                )
            } else if (offersPurchase) {
                TextButton(enabled = !busy, onClick = { onChoose(plan) }) {
                    Text(stringResource(R.string.subscription_choose))
                }
            }
        }
    }
}

/** "30 replies a day", "300 replies a month", or nothing for a plan that counts neither. */
@Composable
private fun planAllowance(plan: PlanDto): String? = when {
    plan.dailyLimit > 0 -> pluralStringResource(R.plurals.subscription_per_day, plan.dailyLimit, plan.dailyLimit)
    plan.monthlyLimit > 0 -> pluralStringResource(R.plurals.subscription_per_month, plan.monthlyLimit, plan.monthlyLimit)
    else -> null
}

/**
 * Price and Choose: never in a release build, which has no Play Billing (a
 * digital plan sold in the app must go through it), and in a debug build only
 * for a paid plan the server says it would sell (demo checkout).
 */
internal fun canOfferPurchase(plan: PlanDto, debugBuild: Boolean = BuildConfig.DEBUG): Boolean =
    debugBuild && plan.purchasable && !plan.isFree

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
