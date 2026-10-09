package kz.yerek.aireply.ai

/**
 * Every way generating a reply can fail, as a value the UI localizes.
 *
 * Deliberately closed and deliberately coarse. The user needs to know what to
 * do next; they do not need an HTTP status, a JSON body or an upstream error
 * string, and showing them one would leak detail that has no business on a
 * keyboard above WhatsApp.
 */
sealed interface AIReplyError {

    /** Nothing was copied, or the copied value held no text. */
    data object NoSourceMessage : AIReplyError

    /** The copied message is longer than the limit. */
    data class MessageTooLong(val limit: Int) : AIReplyError

    /**
     * The clipboard could not be read. Reachable on iOS when Full Access is off;
     * on Android it means the clip was empty, unreadable, or flagged sensitive
     * by the app that produced it.
     */
    data object ClipboardUnavailable : AIReplyError

    /** The account is not ready to make a request. */
    data object NotConfigured : AIReplyError

    /** Device is offline. */
    data object Offline : AIReplyError

    /** The request exceeded its time budget. */
    data object TimedOut : AIReplyError

    /** The user or the keyboard cancelled before a reply arrived. */
    data object Cancelled : AIReplyError

    /** The account session is missing, expired or rejected. */
    data object AuthenticationFailed : AIReplyError

    /** Too many requests in a short time. Waiting a moment fixes it. */
    data object RateLimited : AIReplyError

    /**
     * The plan's replies for today are spent. Waiting a moment does NOT fix
     * it, which is why it is not [RateLimited].
     */
    data object QuotaExhausted : AIReplyError

    /** This month's replies are spent: they come back next month, not tomorrow. */
    data object MonthlyQuotaExhausted : AIReplyError

    /**
     * The current terms, privacy policy and AI processing are not accepted on
     * this phone, or the server says the account's consent is missing. The
     * app has to be opened to accept them again.
     */
    data object ConsentRequired : AIReplyError

    /** The service answered, but not with a usable reply. */
    data object EmptyResponse : AIReplyError

    /** Anything else: a 5xx, a malformed payload, an unreachable host. */
    data object ServiceUnavailable : AIReplyError

    /** Create mode: Write was tapped with nothing described yet. */
    data object NoInstruction : AIReplyError

    /** Create mode: the description is longer than the server's limit. */
    data class InstructionTooLong(val limit: Int) : AIReplyError
}

/** Thrown across suspend boundaries; the payload is what the UI actually reads. */
class AIReplyException(val error: AIReplyError) : Exception(error::class.simpleName)

fun AIReplyError.raise(): Nothing = throw AIReplyException(this)
