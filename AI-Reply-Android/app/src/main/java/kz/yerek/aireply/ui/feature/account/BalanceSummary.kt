package kz.yerek.aireply.ui.feature.account

import kz.yerek.aireply.data.account.AccountUsageCache
import kz.yerek.aireply.data.account.UsageDto
import java.time.Instant
import java.time.OffsetDateTime

/**
 * The account's replies as the screens show them: the server's numbers, split
 * by where they come from.
 *
 * Жауап балансы: бүгін қолжетімді, сәлемдесу бонусы, сатып алынған жауаптар, рұқсаттама.
 *
 * Built from the server's usage alone, so it is never a guess. A server that
 * sends the balance ([hasBalance]) gets rows; an older one does not, and the
 * screens keep the plan's own count ([UsageSummary]) exactly as before. A part
 * the server does not send has no row, and a spent part is a plain number,
 * never a warning colour.
 */
data class BalanceSummary(
    /** The server sent `remaining_total` (now or the last time, from the cache). */
    val hasBalance: Boolean,
    /** What can be generated right now: plan, pass, free and purchased replies together. */
    val availableNow: Int,
    /** The free replies of a new account; a row while the account has a grant. */
    val welcome: Part?,
    /** Administrator grants and purchased packs; a row while some are left. */
    val purchased: Int?,
    /** A running pass; a row until it ends. */
    val pass: Pass?
) {
    data class Part(val remaining: Int, val total: Int)

    data class Pass(val until: OffsetDateTime, val remaining: Int)

    /** «Бесплатные ответы: 35 из 50» while free replies are left. */
    val welcomeHeadline: Part? get() = welcome?.takeIf { it.remaining > 0 }

    companion object {
        /** No balance from the server (yet): the screens use [UsageSummary]. */
        val NONE = BalanceSummary(hasBalance = false, availableNow = 0, welcome = null, purchased = null, pass = null)

        fun of(usage: UsageDto, cached: AccountUsageCache.Snapshot? = null, now: Instant = Instant.now()): BalanceSummary {
            val total = usage.remainingTotal
            if (total == null) {
                // Before the server's first answer, the last known total renders at once.
                val cachedTotal = cached?.remainingTotal
                return if (!usage.isKnown && cachedTotal != null) NONE.copy(hasBalance = true, availableNow = cachedTotal) else NONE
            }
            val welcome = usage.welcome?.takeIf { it.total > 0 }?.let {
                Part(remaining = it.remaining.coerceIn(0, it.total), total = it.total)
            }
            val pass = usage.pass?.let { pass ->
                parse(pass.expiresAt)?.takeIf { it.toInstant().isAfter(now) }?.let { Pass(it, pass.remaining.coerceAtLeast(0)) }
            }
            return BalanceSummary(
                hasBalance = true,
                availableNow = total.coerceAtLeast(0),
                welcome = welcome,
                purchased = usage.credits?.takeIf { it > 0 },
                pass = pass
            )
        }

        /** RFC 3339 with any offset, with or without fractions; null for anything else. */
        fun parse(raw: String): OffsetDateTime? = runCatching { OffsetDateTime.parse(raw.trim()) }.getOrNull()
    }
}
