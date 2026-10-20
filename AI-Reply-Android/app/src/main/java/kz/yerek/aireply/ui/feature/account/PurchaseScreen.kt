package kz.yerek.aireply.ui.feature.account

import android.app.Activity
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material.icons.outlined.Schedule
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import kz.yerek.aireply.R
import kz.yerek.aireply.billing.BillingState
import kz.yerek.aireply.billing.OfferedProduct
import kz.yerek.aireply.billing.PurchaseNotice
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.common.AppScreen
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.AppSection
import kz.yerek.aireply.ui.design.Layout
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.ReadableColumn
import kz.yerek.aireply.ui.design.SecondaryButton
import kz.yerek.aireply.ui.design.Spacing

/**
 * «Докупить ответы»: packs and the pass, with Google Play's prices.
 *
 * Жауап сатып алу: пакеттер мен рұқсаттама, баға Google Play-ден.
 *
 * Reached from the exhausted sheet, the plan screen and `aireply://purchase`
 * (the keyboard's limit notice). Every product is one the server lists and
 * Play prices; while purchases are off the screen says so and offers the
 * plans instead. A purchase is finished by the server, so what the screen
 * reports after Play's sheet closes is the server's answer.
 */
@Composable
fun PurchaseScreen(onBack: () -> Unit, onOpenPlans: () -> Unit) {
    val services = LocalServices.current
    val scope = rememberCoroutineScope()
    val billing = services.billing
    val state by billing.state.collectAsStateWithLifecycle()
    val account by services.account.state.collectAsStateWithLifecycle()
    val balance = BalanceSummary.of(account.usage)

    LaunchedEffect(Unit) {
        billing.load()
        services.account.refreshUsage()
    }

    AppScreen(title = stringResource(R.string.purchase_title), onBack = onBack) {
        ReadableColumn {
            if (balance.hasBalance) {
                AppCard(contentPadding = PaddingValues(horizontal = Spacing.m, vertical = Spacing.xs)) {
                    BalanceRows(balance)
                }
            }

            when (state.phase) {
                BillingState.Phase.IDLE, BillingState.Phase.LOADING -> Box(
                    modifier = Modifier.fillMaxWidth().padding(vertical = Spacing.xl),
                    contentAlignment = Alignment.Center
                ) { CircularProgressIndicator() }

                BillingState.Phase.OFF -> MessageCard(
                    title = stringResource(R.string.purchase_unavailable_title),
                    body = stringResource(R.string.purchase_off_body)
                ) {
                    SecondaryButton(stringResource(R.string.purchase_see_plans), onClick = onOpenPlans)
                }

                BillingState.Phase.UNAVAILABLE -> MessageCard(
                    title = stringResource(R.string.purchase_unavailable_title),
                    body = stringResource(R.string.purchase_unavailable_body)
                ) {
                    Row(horizontalArrangement = Arrangement.spacedBy(Spacing.xs)) {
                        SecondaryButton(stringResource(R.string.purchase_retry)) { scope.launch { billing.load() } }
                        SecondaryButton(stringResource(R.string.purchase_see_plans), onClick = onOpenPlans)
                    }
                }

                BillingState.Phase.READY -> {
                    Footnote(stringResource(R.string.purchase_intro))
                    state.notice?.let { NoticeLine(it) }
                    if (state.products.isEmpty()) {
                        MessageCard(title = stringResource(R.string.purchase_empty), body = null) {
                            SecondaryButton(stringResource(R.string.purchase_see_plans), onClick = onOpenPlans)
                        }
                    }
                    if (state.packs.isNotEmpty()) {
                        AppSection(stringResource(R.string.purchase_packs)) {
                            state.packs.forEach { product ->
                                ProductCard(product, state) { activity -> billing.buy(activity, product) }
                            }
                        }
                    }
                    if (state.passes.isNotEmpty()) {
                        AppSection(stringResource(R.string.purchase_passes)) {
                            state.passes.forEach { product ->
                                ProductCard(product, state) { activity -> billing.buy(activity, product) }
                            }
                            Footnote(stringResource(R.string.purchase_pass_conditions))
                        }
                    }
                    if (state.products.isNotEmpty()) Footnote(stringResource(R.string.purchase_footer))
                }
            }
        }
    }

    // A message is about the last purchase; it does not follow the user to the next visit.
    DisposableEffect(Unit) { onDispose { billing.clearNotice() } }
}

@Composable
private fun ProductCard(product: OfferedProduct, state: BillingState, onBuy: (Activity) -> Unit) {
    val context = LocalContext.current
    val units = pluralStringResource(R.plurals.purchase_units, product.units, product.units)
    val terms = when (product.kind) {
        OfferedProduct.Kind.PACK -> stringResource(R.string.purchase_pack_terms)
        OfferedProduct.Kind.PASS -> stringResource(
            R.string.purchase_pass_terms,
            units,
            pluralStringResource(R.plurals.purchase_hours, product.durationHours, product.durationHours)
        )
    }
    val pending = product.storeProductId in state.pendingProductIds
    val busy = state.busyProductId == product.storeProductId
    val label = stringResource(R.string.purchase_buy_label, product.name, product.price)

    AppCard {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(Spacing.s),
            verticalAlignment = Alignment.CenterVertically
        ) {
            Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(Spacing.xxs)) {
                Text(product.name, style = MaterialTheme.typography.titleSmall)
                Text(
                    if (product.kind == OfferedProduct.Kind.PACK) "$units · $terms" else terms,
                    style = MaterialTheme.typography.bodySmall,
                    color = LocalExtraColors.current.textSecondary
                )
                if (pending) {
                    Row(horizontalArrangement = Arrangement.spacedBy(Spacing.xxs), verticalAlignment = Alignment.CenterVertically) {
                        Icon(
                            Icons.Outlined.Schedule,
                            contentDescription = null,
                            tint = LocalExtraColors.current.textSecondary,
                            modifier = Modifier.size(14.dp)
                        )
                        Text(
                            stringResource(R.string.purchase_pending_badge),
                            style = MaterialTheme.typography.labelSmall,
                            color = LocalExtraColors.current.textSecondary
                        )
                    }
                }
            }
            if (busy) {
                Box(modifier = Modifier.size(Layout.minimumTouchTarget), contentAlignment = Alignment.Center) {
                    CircularProgressIndicator(modifier = Modifier.size(22.dp), strokeWidth = 2.dp)
                }
            } else {
                FilledTonalButton(
                    onClick = { context.findActivity()?.let(onBuy) },
                    enabled = state.busyProductId == null && !pending,
                    modifier = Modifier.semantics { contentDescription = label }
                ) { Text(product.price) }
            }
        }
    }
}

/** What happened to the last purchase. Only a failure the user should know about uses the error colour. */
@Composable
private fun NoticeLine(notice: PurchaseNotice) {
    val failure = notice == PurchaseNotice.FAILED || notice == PurchaseNotice.NOT_CONFIRMED
    val color = when {
        failure -> MaterialTheme.colorScheme.error
        notice == PurchaseNotice.GRANTED -> LocalExtraColors.current.success
        else -> LocalExtraColors.current.textSecondary
    }
    Row(
        modifier = Modifier.fillMaxWidth().semantics(mergeDescendants = true) { liveRegion = LiveRegionMode.Polite },
        horizontalArrangement = Arrangement.spacedBy(Spacing.xs),
        verticalAlignment = Alignment.Top
    ) {
        Icon(
            when {
                failure -> Icons.Outlined.ErrorOutline
                notice == PurchaseNotice.GRANTED -> Icons.Outlined.CheckCircle
                else -> Icons.Outlined.Info
            },
            contentDescription = null,
            tint = color,
            modifier = Modifier.padding(top = 2.dp).size(18.dp)
        )
        Text(
            stringResource(
                when (notice) {
                    PurchaseNotice.GRANTED -> R.string.purchase_notice_granted
                    PurchaseNotice.PENDING -> R.string.purchase_notice_pending
                    PurchaseNotice.CANCELLED -> R.string.purchase_notice_cancelled
                    PurchaseNotice.FAILED -> R.string.purchase_notice_failed
                    PurchaseNotice.UNAVAILABLE -> R.string.purchase_notice_unavailable
                    PurchaseNotice.NOT_CONFIRMED -> R.string.purchase_notice_not_confirmed
                    PurchaseNotice.OTHER_ACCOUNT -> R.string.purchase_notice_other_account
                    PurchaseNotice.RETRY_LATER -> R.string.purchase_notice_retry_later
                }
            ),
            style = MaterialTheme.typography.bodyMedium,
            color = if (failure) color else MaterialTheme.colorScheme.onBackground
        )
    }
}

@Composable
private fun MessageCard(title: String, body: String?, actions: @Composable () -> Unit) {
    AppCard {
        Column(verticalArrangement = Arrangement.spacedBy(Spacing.xs)) {
            Text(title, style = MaterialTheme.typography.titleSmall)
            body?.let { Footnote(it) }
            Box(modifier = Modifier.padding(top = Spacing.xxs)) { actions() }
        }
    }
}
