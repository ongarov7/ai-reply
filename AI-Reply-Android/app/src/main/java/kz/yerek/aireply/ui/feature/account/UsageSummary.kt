package kz.yerek.aireply.ui.feature.account

import kz.yerek.aireply.data.account.AccountUsageCache
import kz.yerek.aireply.data.account.UsageDto

/**
 * The one number the usage card, the plan screen and Settings show: what is
 * left, out of what, and over which period.
 *
 * Қалған жауаптар: күндік лимит, ол жоқ болса айлық, ол да жоқ болса — сан жоқ.
 *
 * A plan without a daily allowance is not "0 of 0 left": it is counted by the
 * month, or not counted at all, and then no number is shown rather than a zero
 * that reads as "blocked". Before the server's first answer the cached daily
 * numbers stand in. Nothing here is ever an error colour - a spent allowance
 * is a fact, not a failure.
 */
data class UsageSummary(val remaining: Int, val limit: Int, val period: Period) {

    enum class Period { DAY, MONTH }

    val used: Int get() = (limit - remaining).coerceIn(0, limit)

    val hasReplies: Boolean get() = remaining > 0

    companion object {
        /** Null when the account has no counted allowance, or nothing is known yet. */
        fun of(usage: UsageDto, cached: AccountUsageCache.Snapshot? = null): UsageSummary? = when {
            usage.dailyLimit > 0 -> UsageSummary(usage.remainingToday.coerceAtLeast(0), usage.dailyLimit, Period.DAY)
            usage.monthlyLimit > 0 -> UsageSummary(
                (usage.monthlyLimit - usage.usedMonth).coerceIn(0, usage.monthlyLimit),
                usage.monthlyLimit,
                Period.MONTH
            )
            // The server has not answered yet: the last known daily numbers.
            usage == UsageDto.UNKNOWN && cached != null && cached.isKnown ->
                UsageSummary(cached.remainingToday.coerceAtLeast(0), cached.dailyLimit, Period.DAY)
            else -> null
        }
    }
}
