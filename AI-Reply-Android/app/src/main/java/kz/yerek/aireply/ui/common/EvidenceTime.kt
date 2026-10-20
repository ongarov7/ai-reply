package kz.yerek.aireply.ui.common

import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import kz.yerek.aireply.R
import kz.yerek.aireply.ui.LocalServices
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import java.util.Locale

/**
 * When something was last seen working - a paste, a check - in the app's
 * language: «сегодня, 14:05», or «20 окт. 2026 г., 14:05» on another day.
 *
 * Тексерілген уақыт: бүгін болса — тек сағат, әйтпесе — күні мен сағаты.
 */
object EvidenceTime {

    /** [today] wraps a time of today ("Today, %1$s"). */
    fun format(
        at: Long,
        now: Long,
        locale: Locale,
        zone: ZoneId = ZoneId.systemDefault(),
        today: (String) -> String
    ): String {
        val moment = Instant.ofEpochMilli(at).atZone(zone)
        val sameDay = moment.toLocalDate() == Instant.ofEpochMilli(now).atZone(zone).toLocalDate()
        return if (sameDay) {
            today(DateTimeFormatter.ofLocalizedTime(FormatStyle.SHORT).withLocale(locale).format(moment))
        } else {
            DateTimeFormatter.ofLocalizedDateTime(FormatStyle.MEDIUM, FormatStyle.SHORT).withLocale(locale).format(moment)
        }
    }
}

/**
 * [EvidenceTime.format] in the app's language. Inside a sentence «сегодня»
 * stays lower-case; on a line of its own ([standalone]) it starts the line.
 */
@Composable
fun evidenceTime(at: Long, standalone: Boolean = false): String {
    val locale = LocalServices.current.settings.effectiveAppLanguage.locale
    val todayFormat = stringResource(R.string.evidence_today)
    val text = EvidenceTime.format(at, System.currentTimeMillis(), locale) { time -> String.format(locale, todayFormat, time) }
    return if (standalone) text.replaceFirstChar { it.titlecase(locale) } else text
}
