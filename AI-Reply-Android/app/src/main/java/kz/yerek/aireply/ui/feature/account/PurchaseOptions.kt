package kz.yerek.aireply.ui.feature.account

import android.app.Activity
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.relocation.BringIntoViewRequester
import androidx.compose.foundation.relocation.bringIntoViewRequester
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.AllInclusive
import androidx.compose.material.icons.filled.AutoAwesome
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import kz.yerek.aireply.BuildConfig
import kz.yerek.aireply.R
import kz.yerek.aireply.billing.BillingState
import kz.yerek.aireply.billing.IapRecommendationDto
import kz.yerek.aireply.billing.OfferedProduct
import kz.yerek.aireply.billing.PurchaseNotice
import kz.yerek.aireply.data.account.PlanDto
import kz.yerek.aireply.data.account.SubscriptionDto
import kz.yerek.aireply.data.account.UsageDto
import kz.yerek.aireply.debug.DebugSamples
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.common.openWebPage
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.Layout
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.Radius
import kz.yerek.aireply.ui.design.SecondaryButton
import kz.yerek.aireply.ui.design.Spacing
import java.math.BigDecimal
import java.text.DecimalFormat
import java.text.NumberFormat
import java.util.Currency
import java.util.Locale

/**
 * Plans and packs in one place: the free allowance, the subscriptions by
 * period, the one-time packs, restore and management.
 *
 * Тарифтер мен пакеттер: баға тек Google Play-ден, начисление серверде.
 *
 * A paid plan is shown only when both the server and Google Play know its
 * product, at Google Play's price: nothing here can be tapped and then fail
 * for want of a product. The numbers and what they mean come from the server.
 */
@OptIn(ExperimentalFoundationApi::class)
@Composable
internal fun PurchaseOptions(onOpenPlans: (() -> Unit)? = null) {
    val services = LocalServices.current
    val billing = services.billing
    val scope = rememberCoroutineScope()
    val state by billing.state.collectAsStateWithLifecycle()
    val account by services.account.state.collectAsStateWithLifecycle()
    val language = services.settings.effectiveAppLanguage.code
    val context = LocalContext.current
    val usage = if (services.sampleData) DebugSamples.usage else account.usage

    LaunchedEffect(Unit) {
        services.account.loadPlans()
        billing.load()
    }

    var period by rememberSaveable { mutableStateOf(PlanCatalogue.MONTH) }
    val catalogue = PlanCatalogue(account.plans, state.subscriptions)

    // «Посмотреть Base»: the plan it points at, and one scroll to it for every tap.
    var viewedOffer by remember { mutableStateOf<String?>(null) }
    var views by remember { mutableIntStateOf(0) }
    val viewedCard = remember { BringIntoViewRequester() }

    LaunchedEffect(views) {
        if (views == 0) return@LaunchedEffect
        // After the layout pass, so there is somewhere to scroll to.
        withFrameNanos { }
        viewedCard.bringIntoView()
    }

    Column(verticalArrangement = Arrangement.spacedBy(Spacing.xxs)) {
        Text(
            stringResource(R.string.plans_heading),
            style = MaterialTheme.typography.titleLarge,
            fontWeight = FontWeight.Bold,
            modifier = Modifier.semantics { heading() }
        )
        Text(
            stringResource(R.string.plans_intro),
            style = MaterialTheme.typography.bodyMedium,
            color = LocalExtraColors.current.textSecondary
        )
    }

    StatusCard(state)

    if (state.busyProductId == null && !state.restoring) {
        state.recommendation?.let { recommendation ->
            recommendedOffer(recommendation, state.subscriptions, usage, account.subscription)?.let { offer ->
                RecommendationCard(
                    recommendation,
                    offer,
                    onView = {
                        period = PlanCatalogue.MONTH
                        viewedOffer = offer.storeProductId
                        views++
                    },
                    onDismiss = billing::dismissRecommendation
                )
            }
        }
    }

    catalogue.freePlan?.let { free ->
        AppCard {
            Text(free.localizedName(language), style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
            val welcome = usage.welcome?.takeIf { it.total > 0 }
            Text(
                if (welcome != null) {
                    pluralStringResource(R.plurals.plans_welcome_once, welcome.total, welcome.total)
                } else {
                    free.localizedDescription(language)
                },
                style = MaterialTheme.typography.bodyMedium,
                color = LocalExtraColors.current.textSecondary,
                modifier = Modifier.padding(top = Spacing.xs)
            )
        }
    }

    if (catalogue.options.isNotEmpty()) {
        Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
            SectionTitle(stringResource(R.string.plans_subscriptions))
            if (catalogue.periods.size > 1) {
                SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
                    catalogue.periods.forEachIndexed { index, value ->
                        SegmentedButton(
                            selected = catalogue.selectedPeriod(period) == value,
                            onClick = { period = value },
                            shape = SegmentedButtonDefaults.itemShape(index, catalogue.periods.size),
                            label = {
                                Text(stringResource(if (value == PlanCatalogue.YEAR) R.string.plans_period_year else R.string.plans_period_month))
                            }
                        )
                    }
                }
            }
            catalogue.visible(period).forEach { option ->
                val current = account.subscription?.status == "active" && account.subscription?.plan?.id == option.planId
                val viewed = option.offer.storeProductId == viewedOffer
                SubscriptionCard(
                    option,
                    state,
                    current,
                    viewed,
                    modifier = if (viewed) Modifier.bringIntoViewRequester(viewedCard) else Modifier
                ) { activity -> billing.buy(activity, option.offer) }
            }
        }
    }

    Catalogue(state, onOpenPlans) { scope.launch { reloadPlans(services) } }

    Column(
        modifier = Modifier.fillMaxWidth(),
        verticalArrangement = Arrangement.spacedBy(Spacing.xxs),
        horizontalAlignment = Alignment.CenterHorizontally
    ) {
        TextButton(
            onClick = {
                scope.launch {
                    billing.restore()
                    services.account.refresh()
                }
            },
            enabled = !state.restoring && state.busyProductId == null,
            modifier = Modifier.fillMaxWidth()
        ) {
            Icon(Icons.Filled.Refresh, contentDescription = null, modifier = Modifier.size(18.dp))
            Spacer(Modifier.width(Spacing.xs))
            Text(stringResource(R.string.plans_restore))
        }
        if (catalogue.options.isNotEmpty() || account.subscription?.source == PLAY_SOURCE) {
            TextButton(
                onClick = { context.openWebPage(managementUrl(account.subscription?.plan?.id, state.subscriptions, context.packageName)) },
                modifier = Modifier.fillMaxWidth()
            ) {
                Text(stringResource(R.string.plans_manage))
            }
        }
        Footnote(stringResource(R.string.plans_restore_detail))
    }

    Column(verticalArrangement = Arrangement.spacedBy(Spacing.xs)) {
        Footnote(stringResource(R.string.plans_renewal))
        Row(horizontalArrangement = Arrangement.spacedBy(Spacing.m)) {
            val pageLanguage = services.settings.effectiveAppLanguage.code
            TextButton(onClick = { context.openWebPage(account.legalConfig.termsUrl, pageLanguage) }) {
                Text(stringResource(R.string.legal_terms), style = MaterialTheme.typography.bodySmall)
            }
            TextButton(onClick = { context.openWebPage(account.legalConfig.privacyUrl, pageLanguage) }) {
                Text(stringResource(R.string.legal_privacy), style = MaterialTheme.typography.bodySmall)
            }
        }
        account.user?.identifier?.takeIf(String::isNotEmpty)?.let { identifier ->
            Footnote(stringResource(R.string.plans_footer_account, identifier))
        }
    }
}

private suspend fun reloadPlans(services: kz.yerek.aireply.ServiceLocator) {
    services.account.loadServerConfig()
    services.account.refresh()
    services.account.loadPlans()
    services.billing.load()
}

/** Google Play's subscription centre, at this subscription when it is one of ours. */
private fun managementUrl(planId: String?, subscriptions: List<OfferedProduct>, packageName: String): String {
    val product = subscriptions.firstOrNull { it.planId == planId }?.storeProductId
    val query = if (product != null) "sku=$product&package=$packageName" else "package=$packageName"
    return "https://play.google.com/store/account/subscriptions?$query"
}

private const val PLAY_SOURCE = "google_play"

@Composable
private fun SectionTitle(text: String) {
    Text(
        text,
        style = MaterialTheme.typography.titleMedium,
        fontWeight = FontWeight.SemiBold,
        modifier = Modifier.semantics { heading() }
    )
}

@Composable
private fun StatusCard(state: BillingState) {
    val text = when {
        state.restoring -> R.string.plans_status_restoring
        state.notice != null -> noticeText(state.notice)
        else -> return
    }
    AppCard(modifier = Modifier.semantics(mergeDescendants = true) { liveRegion = LiveRegionMode.Polite }) {
        Row(horizontalArrangement = Arrangement.spacedBy(Spacing.s), verticalAlignment = Alignment.Top) {
            if (state.restoring) CircularProgressIndicator(modifier = Modifier.size(18.dp), strokeWidth = 2.dp)
            Text(stringResource(text), style = MaterialTheme.typography.bodyMedium)
        }
    }
}

private fun noticeText(notice: PurchaseNotice): Int = when (notice) {
    PurchaseNotice.GRANTED -> R.string.plans_status_granted
    PurchaseNotice.PENDING -> R.string.plans_status_pending
    PurchaseNotice.CANCELLED -> R.string.plans_status_cancelled
    PurchaseNotice.FAILED -> R.string.plans_status_failed
    PurchaseNotice.UNAVAILABLE -> R.string.plans_status_unavailable
    PurchaseNotice.NOT_CONFIRMED -> R.string.plans_status_not_confirmed
    PurchaseNotice.OTHER_ACCOUNT -> R.string.plans_status_other_account
    PurchaseNotice.RETRY_LATER -> R.string.plans_status_retry
    PurchaseNotice.RESTORED -> R.string.plans_status_restored
    PurchaseNotice.NOTHING_TO_RESTORE -> R.string.plans_status_no_restore
}

/** For an account that keeps buying packs: what they cost so far, and Base at Play's price. */
@Composable
private fun RecommendationCard(recommendation: IapRecommendationDto, offer: OfferedProduct, onView: () -> Unit, onDismiss: () -> Unit) {
    val spent = formatSpend(recommendation.spentMinor, recommendation.currency, LocalServices.current.settings.effectiveAppLanguage.locale)
    AppCard {
        Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
            Row(horizontalArrangement = Arrangement.spacedBy(Spacing.xs), verticalAlignment = Alignment.CenterVertically) {
                Icon(Icons.Filled.AutoAwesome, contentDescription = null, tint = MaterialTheme.colorScheme.primary, modifier = Modifier.size(18.dp))
                Text(stringResource(R.string.purchase_recommendation_title), style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
            }
            Text(
                if (offer.monthlyLimit > 0) {
                    pluralStringResource(R.plurals.purchase_recommendation_body_monthly, offer.monthlyLimit, spent, offer.price, offer.monthlyLimit)
                } else {
                    pluralStringResource(R.plurals.purchase_recommendation_body, offer.dailyLimit, spent, offer.price, offer.dailyLimit)
                },
                style = MaterialTheme.typography.bodyMedium
            )
            OutlinedButton(
                onClick = onView,
                shape = RoundedCornerShape(Radius.medium),
                border = BorderStroke(1.dp, MaterialTheme.colorScheme.outline),
                modifier = Modifier.fillMaxWidth().defaultMinSize(minHeight = Layout.minimumTouchTarget)
            ) {
                Text(stringResource(R.string.purchase_recommendation_choose), style = MaterialTheme.typography.labelLarge)
            }
            TextButton(onClick = onDismiss, modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.purchase_recommendation_dismiss), textAlign = TextAlign.Center)
            }
        }
    }
}

/**
 * The monthly Base [recommendation] points at, for an account that can take it
 * up: not on an unlimited or running paid plan, and not at a price being updated.
 */
internal fun recommendedOffer(
    recommendation: IapRecommendationDto,
    subscriptions: List<OfferedProduct>,
    usage: UsageDto,
    subscription: SubscriptionDto?
): OfferedProduct? {
    if (usage.unlimited || (subscription?.status == "active" && !subscription.plan.isFree)) return null
    return subscriptions.firstOrNull { offer ->
        offer.storeProductId == recommendation.baseStoreProductId && offer.billingPeriod == PlanCatalogue.MONTH &&
            ((offer.entitlementKind == "daily" && offer.dailyLimit > 0) || (offer.entitlementKind == "monthly" && offer.monthlyLimit > 0)) &&
            !offer.hasPriceMismatch
    }
}

/** What was spent on packs in the app's language, with the tenge's own sign. */
internal fun formatSpend(spentMinor: Long, currency: String, locale: Locale): String {
    val amount = BigDecimal(spentMinor).movePointLeft(2)
    return runCatching {
        val money = Currency.getInstance(currency)
        val format = NumberFormat.getCurrencyInstance(locale) as DecimalFormat
        format.currency = money
        format.minimumFractionDigits = money.defaultFractionDigits
        format.maximumFractionDigits = money.defaultFractionDigits
        if (currency == "KZT") format.decimalFormatSymbols = format.decimalFormatSymbols.apply { currencySymbol = "₸" }
        format.format(amount)
    }.getOrElse { "${amount.toPlainString()} $currency" }
}

@Composable
private fun Catalogue(state: BillingState, onOpenPlans: (() -> Unit)?, onRetry: () -> Unit) {
    when (state.phase) {
        BillingState.Phase.IDLE, BillingState.Phase.LOADING -> if (state.products.isEmpty()) {
            AppCard {
                Row(horizontalArrangement = Arrangement.spacedBy(Spacing.s), verticalAlignment = Alignment.CenterVertically) {
                    CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp)
                    Text(stringResource(R.string.plans_loading), color = LocalExtraColors.current.textSecondary)
                }
            }
        } else {
            Products(state)
        }
        BillingState.Phase.OFF, BillingState.Phase.UNAVAILABLE -> Unavailable(onOpenPlans, onRetry)
        BillingState.Phase.READY -> if (state.products.isEmpty()) Unavailable(onOpenPlans, onRetry) else Products(state)
    }
}

@Composable
private fun Unavailable(onOpenPlans: (() -> Unit)?, onRetry: () -> Unit) {
    AppCard {
        Text(stringResource(R.string.plans_unavailable), color = LocalExtraColors.current.textSecondary)
        Row(modifier = Modifier.padding(top = Spacing.xs), horizontalArrangement = Arrangement.spacedBy(Spacing.xs)) {
            SecondaryButton(stringResource(R.string.purchase_retry), onClick = onRetry)
            onOpenPlans?.let { SecondaryButton(stringResource(R.string.purchase_see_plans), onClick = it) }
        }
    }
}

@Composable
private fun Products(state: BillingState) {
    val billing = LocalServices.current.billing
    if (state.packs.isNotEmpty()) {
        Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
            SectionTitle(stringResource(R.string.plans_without_subscription))
            Footnote(stringResource(R.string.plans_packs_footer))
            state.packs.forEach { product -> ProductCard(product, state) { activity -> billing.buy(activity, product) } }
        }
    }
    if (state.passes.isNotEmpty()) {
        Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
            SectionTitle(stringResource(R.string.purchase_passes))
            state.passes.forEach { product -> ProductCard(product, state) { activity -> billing.buy(activity, product) } }
            Footnote(stringResource(R.string.purchase_pass_conditions))
        }
    }
}

@Composable
private fun SubscriptionCard(
    option: PlanCatalogue.Option,
    state: BillingState,
    current: Boolean,
    viewed: Boolean,
    modifier: Modifier,
    onBuy: (Activity) -> Unit
) {
    val colors = MaterialTheme.colorScheme
    AppCard(
        modifier = if (current || viewed) {
            modifier.border(BorderStroke(1.5.dp, colors.primary), RoundedCornerShape(Radius.large))
        } else {
            modifier
        }
    ) {
        Column(verticalArrangement = Arrangement.spacedBy(Spacing.xs)) {
            Row(verticalAlignment = Alignment.Top) {
                Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(Spacing.xxs)) {
                    Text(option.offer.name, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
                    Allowance(option)
                }
                if (current) Icon(Icons.Filled.CheckCircle, stringResource(R.string.plans_current), tint = colors.primary)
            }
            Text(
                stringResource(if (option.billingPeriod == PlanCatalogue.YEAR) R.string.plans_period_year else R.string.plans_period_month),
                style = MaterialTheme.typography.bodyMedium,
                color = LocalExtraColors.current.textSecondary
            )
            Text(option.offer.price, style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.SemiBold)
            Text(
                stringResource(if (option.billingPeriod == PlanCatalogue.YEAR) R.string.plans_billed_year else R.string.plans_billed_month),
                style = MaterialTheme.typography.bodySmall,
                color = LocalExtraColors.current.textSecondary
            )
            if (option.offer.creditsNeverExpire) {
                Text(
                    stringResource(R.string.plans_replies_never_expire),
                    style = MaterialTheme.typography.bodySmall,
                    color = LocalExtraColors.current.textSecondary
                )
            }
            BuyButton(
                option.offer,
                state,
                label = stringResource(if (current) R.string.plans_current else R.string.plans_subscribe),
                enabled = !current,
                onBuy = onBuy
            )
            if (option.offer.hasPriceMismatch) MismatchNotice()
        }
    }
}

/** "1000 replies a month", "40 replies a day" or "Unlimited replies". */
@Composable
private fun ColumnScope.Allowance(option: PlanCatalogue.Option) {
    val secondary = LocalExtraColors.current.textSecondary
    when (val allowance = option.allowance) {
        PlanCatalogue.Allowance.Unlimited -> Row(
            horizontalArrangement = Arrangement.spacedBy(Spacing.xxs),
            verticalAlignment = Alignment.CenterVertically
        ) {
            Icon(Icons.Filled.AllInclusive, contentDescription = null, tint = MaterialTheme.colorScheme.primary, modifier = Modifier.size(18.dp))
            Text(stringResource(R.string.plans_unlimited), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.primary)
        }
        is PlanCatalogue.Allowance.Monthly -> Text(
            pluralStringResource(R.plurals.subscription_per_month, allowance.limit, allowance.limit),
            style = MaterialTheme.typography.bodyMedium,
            color = secondary
        )
        is PlanCatalogue.Allowance.Daily -> Text(
            pluralStringResource(R.plurals.subscription_per_day, allowance.limit, allowance.limit),
            style = MaterialTheme.typography.bodyMedium,
            color = secondary
        )
        PlanCatalogue.Allowance.Unspecified -> Unit
    }
}

@Composable
private fun ProductCard(product: OfferedProduct, state: BillingState, onBuy: (Activity) -> Unit) {
    val units = pluralStringResource(R.plurals.purchase_units, product.units, product.units)
    val pending = product.storeProductId in state.pendingProductIds
    AppCard {
        Column(verticalArrangement = Arrangement.spacedBy(Spacing.xs)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(product.name, style = MaterialTheme.typography.titleSmall, modifier = Modifier.weight(1f))
                Text(product.price, style = MaterialTheme.typography.titleSmall)
            }
            // A pack named by its count is not told its count twice.
            if (!product.name.trim().equals(units, ignoreCase = true)) {
                Text(units, style = MaterialTheme.typography.bodyMedium)
            }
            Text(
                when (product.kind) {
                    OfferedProduct.Kind.PASS -> pluralStringResource(R.plurals.purchase_hours, product.durationHours, product.durationHours)
                    else -> stringResource(R.string.plans_pack_detail)
                },
                style = MaterialTheme.typography.bodySmall,
                color = LocalExtraColors.current.textSecondary
            )
            if (pending) {
                Text(
                    stringResource(R.string.purchase_pending_badge),
                    style = MaterialTheme.typography.labelSmall,
                    color = LocalExtraColors.current.textSecondary
                )
            }
            BuyButton(product, state, label = stringResource(R.string.plans_buy_once), enabled = !pending, onBuy = onBuy)
            if (product.hasPriceMismatch) MismatchNotice()
        }
    }
}

@Composable
private fun BuyButton(product: OfferedProduct, state: BillingState, label: String, enabled: Boolean, onBuy: (Activity) -> Unit) {
    val context = LocalContext.current
    val busy = state.busyProductId == product.storeProductId
    Button(
        onClick = { context.findActivity()?.let(onBuy) },
        enabled = enabled && state.busyProductId == null && !product.hasPriceMismatch && !state.restoring,
        shape = RoundedCornerShape(Radius.medium),
        colors = ButtonDefaults.buttonColors(containerColor = MaterialTheme.colorScheme.primary),
        modifier = Modifier.fillMaxWidth().padding(top = Spacing.xxs)
    ) {
        if (busy) {
            Box(Modifier.size(16.dp), contentAlignment = Alignment.Center) {
                CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp, color = MaterialTheme.colorScheme.onPrimary)
            }
            Spacer(Modifier.width(Spacing.xs))
        }
        Text(label, textAlign = TextAlign.Center)
    }
}

@Composable
private fun MismatchNotice() {
    Text(
        stringResource(R.string.plans_price_mismatch),
        style = MaterialTheme.typography.bodySmall,
        color = LocalExtraColors.current.textSecondary
    )
}

/**
 * The published plans matched with what Google Play sells, one period at a
 * time. A paid plan with no matching product is not listed; a selected period
 * with nothing in it falls back to the one that has something.
 */
internal class PlanCatalogue(plans: List<PlanDto>, subscriptions: List<OfferedProduct>) {

    sealed interface Allowance {
        data class Daily(val limit: Int) : Allowance
        data class Monthly(val limit: Int) : Allowance
        data object Unlimited : Allowance
        data object Unspecified : Allowance
    }

    data class Option(val plan: PlanDto?, val offer: OfferedProduct, val billingPeriod: String) {
        val planId: String? get() = offer.planId ?: plan?.id

        val allowance: Allowance
            get() = when {
                offer.entitlementKind == "unlimited" -> Allowance.Unlimited
                offer.entitlementKind == "monthly" && offer.monthlyLimit > 0 -> Allowance.Monthly(offer.monthlyLimit)
                offer.entitlementKind == "daily" && offer.dailyLimit > 0 -> Allowance.Daily(offer.dailyLimit)
                else -> Allowance.Unspecified
            }
    }

    val freePlan: PlanDto? = plans.firstOrNull { it.isFree }

    val options: List<Option> = run {
        val included = mutableSetOf<String>()
        val result = mutableListOf<Option>()
        plans.filter { !it.isFree }.forEach { plan ->
            val period = when (plan.periodDays) {
                30 -> MONTH
                365 -> YEAR
                else -> return@forEach
            }
            val offer = subscriptions.firstOrNull { it.planId == plan.id && it.billingPeriod == period } ?: return@forEach
            included += offer.storeProductId
            result += Option(plan, offer, period)
        }
        subscriptions.filter { it.storeProductId !in included }.forEach { offer ->
            val period = offer.billingPeriod?.takeIf { it == MONTH || it == YEAR } ?: return@forEach
            result += Option(null, offer, period)
        }
        result
    }

    val periods: List<String> = listOf(MONTH, YEAR).filter { period -> options.any { it.billingPeriod == period } }

    fun selectedPeriod(period: String): String? = if (period in periods) period else periods.firstOrNull()

    fun visible(period: String): List<Option> = selectedPeriod(period).let { selected -> options.filter { it.billingPeriod == selected } }

    companion object {
        const val MONTH = "month"
        const val YEAR = "year"
    }
}
