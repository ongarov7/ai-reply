package kz.yerek.aireply.ai

import android.content.Context
import androidx.annotation.StringRes
import kz.yerek.aireply.R

/**
 * Every user-visible string the AI reply flow needs, resolved in a specific
 * language.
 *
 * WHICH LANGUAGE, AND WHY. There are two languages in this product and they
 * answer different questions:
 *
 *  * the KEYBOARD LAYOUT decides what the character keys type and what the
 *    space and return keys are called;
 *  * the APP LANGUAGE decides what the PRODUCT says — template chips, Insert,
 *    Regenerate, statuses and errors.
 *
 * They are deliberately not the same. A Kazakh user typing on the English
 * layout to write a Russian reply must still see "Кірістіру", because the
 * product is theirs and the layout is just a keyboard.
 *
 * On iOS this had to be a hand-written Swift table, because a keyboard
 * extension cannot see which language the containing app is set to. Here it is
 * a thin wrapper over a [Context] from
 * [kz.yerek.aireply.core.lang.LocalizedContext], so the strings are the same
 * `strings.xml` entries every screen uses.
 */
class AppStrings(private val context: Context) {

    operator fun get(@StringRes id: Int): String = context.getString(id)

    fun get(@StringRes id: Int, vararg args: Any): String = context.getString(id, *args)

    /**
     * Maps an error onto the sentence the user actually sees. Short, plain,
     * actionable, and free of status codes, JSON and provider names.
     *
     */
    fun message(error: AIReplyError): String = when (error) {
        AIReplyError.NoSourceMessage -> get(R.string.kb_err_no_source)
        is AIReplyError.MessageTooLong -> get(R.string.kb_err_message_too_long, error.limit)
        AIReplyError.ClipboardUnavailable -> get(R.string.kb_err_clipboard_unavailable)
        AIReplyError.NotConfigured -> get(R.string.kb_err_not_configured)
        AIReplyError.Offline -> get(R.string.kb_err_offline)
        AIReplyError.TimedOut -> get(R.string.kb_err_timed_out)
        // Cancelling is something the user did on purpose. Telling them it
        // happened is noise, so this is the one error with no sentence.
        AIReplyError.Cancelled -> ""
        AIReplyError.AuthenticationFailed -> get(R.string.kb_err_sign_in_required)
        AIReplyError.RateLimited -> get(R.string.kb_err_rate_limited)
        AIReplyError.QuotaExhausted -> get(R.string.kb_err_quota_exhausted)
        AIReplyError.EmptyResponse -> get(R.string.kb_err_empty_response)
        AIReplyError.ServiceUnavailable -> get(R.string.kb_err_service_unavailable)
        AIReplyError.NoInstruction -> get(R.string.kb_compose_err_no_instruction)
        is AIReplyError.InstructionTooLong -> get(R.string.kb_compose_err_too_long, error.limit)
    }
}
