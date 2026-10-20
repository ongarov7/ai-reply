package kz.yerek.aireply.ui.feature.account

import android.text.format.DateFormat
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import kz.yerek.aireply.R
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.RowDivider
import kz.yerek.aireply.ui.design.Spacing
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter

/**
 * The rows of a [BalanceSummary]: label on the left, the number on the right.
 * Today's total comes first and is the only one in the primary text weight;
 * the parts it is made of follow, each only when the server sent it.
 *
 * Балансты жолдар: бүгін қолжетімді, бонус, сатып алынған, рұқсаттама.
 */
@Composable
fun BalanceRows(summary: BalanceSummary, modifier: Modifier = Modifier) {
    val locale = LocalServices.current.settings.effectiveAppLanguage.locale
    Column(modifier = modifier.fillMaxWidth()) {
        BalanceRow(stringResource(R.string.balance_today), summary.availableNow.toString(), isTotal = true)
        summary.welcome?.let { welcome ->
            RowDivider()
            BalanceRow(
                stringResource(R.string.balance_welcome),
                stringResource(R.string.balance_welcome_value, welcome.remaining, welcome.total)
            )
        }
        summary.purchased?.let { purchased ->
            RowDivider()
            BalanceRow(stringResource(R.string.balance_purchased), purchased.toString())
        }
        summary.pass?.let { pass ->
            RowDivider()
            BalanceRow(stringResource(R.string.balance_pass, formatPassEnd(pass.until, locale)), pass.remaining.toString())
        }
    }
}

@Composable
private fun BalanceRow(title: String, value: String, isTotal: Boolean = false) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = Spacing.xs)
            .semantics(mergeDescendants = true) {},
        horizontalArrangement = Arrangement.spacedBy(Spacing.s),
        verticalAlignment = Alignment.CenterVertically
    ) {
        Text(title, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f))
        Text(
            value,
            style = MaterialTheme.typography.bodyMedium,
            fontWeight = if (isTotal) FontWeight.SemiBold else FontWeight.Normal,
            color = if (isTotal) MaterialTheme.colorScheme.onSurface else LocalExtraColors.current.textSecondary
        )
    }
}

/** «21 окт., 15:00» on this phone's clock, in the app's language. */
internal fun formatPassEnd(until: OffsetDateTime, locale: java.util.Locale, zone: ZoneId = ZoneId.systemDefault()): String {
    val pattern = runCatching { DateFormat.getBestDateTimePattern(locale, "dMMMHHmm") }.getOrNull() ?: "d MMM, HH:mm"
    return DateTimeFormatter.ofPattern(pattern, locale).format(until.atZoneSameInstant(zone))
}
