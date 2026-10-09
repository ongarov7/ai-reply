package kz.yerek.aireply.ui.feature.account

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
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
fun SubscriptionScreen(onBack: () -> Unit) {
    val services = LocalServices.current
    val account = services.account
    val state by account.state.collectAsStateWithLifecycle()
    val scope = rememberCoroutineScope()
    val appLanguage = services.settings.effectiveAppLanguage
    val language = appLanguage.code

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
                    Column(horizontalAlignment = Alignment.End) {
                        Text(
                            state.usage.remainingToday.toString(),
                            style = MaterialTheme.typography.headlineMedium,
                            fontWeight = FontWeight.SemiBold,
                            color = if (state.usage.remainingToday > 0) {
                                MaterialTheme.colorScheme.primary
                            } else {
                                MaterialTheme.colorScheme.error
                            }
                        )
                        Text(
                            stringResource(R.string.subscription_remaining),
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant
                        )
                    }
                }

                LinearProgressIndicator(
                    progress = {
                        val limit = state.usage.dailyLimit
                        if (limit <= 0) 0f else (state.usage.usedToday.toFloat() / limit).coerceIn(0f, 1f)
                    },
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(top = Spacing.s)
                )

                Footnote(
                    stringResource(
                        R.string.subscription_used_today,
                        state.usage.usedToday,
                        state.usage.dailyLimit
                    )
                )
                state.subscription?.expiresAt?.let { formatExpiry(it, appLanguage.locale) }?.let { expiry ->
                    Footnote(stringResource(R.string.subscription_renews, expiry))
                }
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
            Text(
                stringResource(R.string.subscription_per_day, plan.dailyLimit),
                style = MaterialTheme.typography.labelLarge,
                color = MaterialTheme.colorScheme.primary
            )
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
