package kz.yerek.aireply.ui.feature.account

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import kz.yerek.aireply.R
import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.domain.model.GrammaticalGender
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.PrimaryButton
import kz.yerek.aireply.ui.design.SecondaryButton
import kz.yerek.aireply.ui.design.Spacing

/**
 * When a generation in the app is refused because every reply is spent
 * (contract §3.9).
 *
 * Жауаптар бітті: сатып алу (бар болса), тарифтер, кейінірек.
 */
object ExhaustedRules {

    /** The plan's day or month, and everything beyond it, is spent: the sheet, never a red error. */
    fun isLimit(error: AIReplyError): Boolean =
        error == AIReplyError.QuotaExhausted || error == AIReplyError.MonthlyQuotaExhausted

    /**
     * «Докупить ответы» only when the server sells replies on Android
     * ([purchasesOffered], `features.iap_android`) and said so for this
     * refusal too (`details.can_buy`).
     */
    fun offersPurchase(purchasesOffered: Boolean, canBuy: Boolean): Boolean = purchasesOffered && canBuy

    /** The text: with a purchase to offer, in the sender's grammatical form; without one, the plans alone. */
    @StringRes
    fun body(offersPurchase: Boolean, gender: GrammaticalGender?): Int = when {
        !offersPurchase -> R.string.exhausted_body_plans
        gender == GrammaticalGender.MALE -> R.string.exhausted_body_male
        gender == GrammaticalGender.FEMALE -> R.string.exhausted_body_female
        else -> R.string.exhausted_body
    }
}

/**
 * «Лимит ответов исчерпан»: the title, what happened, and the ways on -
 * «Докупить ответы» when it is offered, «Посмотреть тарифы», «Позже». Back and
 * a swipe down are «Позже».
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ExhaustedSheet(
    offersPurchase: Boolean,
    gender: GrammaticalGender?,
    onBuy: () -> Unit,
    onPlans: () -> Unit,
    onDismiss: () -> Unit
) {
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        containerColor = MaterialTheme.colorScheme.surface
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .verticalScroll(rememberScrollState())
                .navigationBarsPadding()
                .padding(start = Spacing.l, end = Spacing.l, bottom = Spacing.m),
            verticalArrangement = Arrangement.spacedBy(Spacing.s),
            horizontalAlignment = Alignment.CenterHorizontally
        ) {
            Text(
                stringResource(R.string.kb_limit_title),
                style = MaterialTheme.typography.titleLarge,
                modifier = Modifier.fillMaxWidth().semantics { heading() }
            )
            Text(
                stringResource(ExhaustedRules.body(offersPurchase, gender)),
                style = MaterialTheme.typography.bodyMedium,
                color = LocalExtraColors.current.textSecondary,
                modifier = Modifier.fillMaxWidth().padding(bottom = Spacing.xs)
            )
            if (offersPurchase) {
                PrimaryButton(stringResource(R.string.purchase_title), onClick = onBuy)
                SecondaryButton(stringResource(R.string.purchase_see_plans), modifier = Modifier.fillMaxWidth(), onClick = onPlans)
            } else {
                PrimaryButton(stringResource(R.string.purchase_see_plans), onClick = onPlans)
            }
            TextButton(onClick = onDismiss, modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.exhausted_later))
            }
        }
    }
}
