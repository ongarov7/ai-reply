package kz.yerek.aireply.ui.feature.account

import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.res.stringResource
import kz.yerek.aireply.R
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.common.AppScreen
import kz.yerek.aireply.ui.design.ReadableColumn

/**
 * «Тарифы и ответы»: reached from the exhausted sheet and `aireply://purchase`
 * (the keyboard's limit notice). A purchase is finished by the server, so what
 * the screen reports after Play's sheet closes is the server's answer.
 */
@Composable
fun PurchaseScreen(onBack: () -> Unit, onOpenPlans: () -> Unit) {
    val services = LocalServices.current

    LaunchedEffect(Unit) { services.account.refreshUsage() }

    AppScreen(title = stringResource(R.string.plans_title), onBack = onBack) {
        ReadableColumn {
            PurchaseOptions(onOpenPlans)
        }
    }

    // A message is about the last purchase; it does not follow the user to the next visit.
    DisposableEffect(Unit) { onDispose { services.billing.clearNotice() } }
}
