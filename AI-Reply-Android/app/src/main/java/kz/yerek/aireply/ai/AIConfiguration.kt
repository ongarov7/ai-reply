package kz.yerek.aireply.ai

/**
 * Fixed production configuration shared by the app and input method.
 *
 * A request needs an account AND the current legal versions accepted on this
 * phone: the consent screen is where the user agrees to the text going to the
 * AI provider, so nothing is sent before it, from the app or the keyboard.
 */
class AIConfiguration(
    /** The current terms and privacy versions are accepted here (the legal consent store). */
    private val hasLegalConsent: () -> Boolean = { true },
    /** The server answered CONSENT_REQUIRED: the app shows its consent screen again. */
    private val onConsentRequired: () -> Unit = {},
    private val isAccountSignedIn: () -> Boolean
) {

    val backendBaseUrl: String = DEFAULT_BACKEND_BASE_URL
    val requiresAccount: Boolean = true
    val isReady: Boolean get() = notReadyReason == null

    /** Why a request cannot be made right now, or null when it can. */
    val notReadyReason: AIReplyError?
        get() = when {
            !isAccountSignedIn() -> AIReplyError.AuthenticationFailed
            !hasLegalConsent() -> AIReplyError.ConsentRequired
            else -> null
        }

    /**
     * Throws [notReadyReason] when there is one. A consent missing here (new
     * versions the keyboard learned about first) is handed to the app too, so
     * opening it shows the consent screen rather than Home.
     */
    fun checkReady() {
        val reason = notReadyReason ?: return
        requestFailed(reason)
        reason.raise()
    }

    /** A request failed with [error]; a missing consent is handed to the app. */
    fun requestFailed(error: AIReplyError) {
        if (error == AIReplyError.ConsentRequired) onConsentRequired()
    }

    companion object {
        const val DEFAULT_BACKEND_BASE_URL = "https://ai-reply.kz"
        const val MAX_OUTPUT_TOKENS = 180
        const val REQUEST_TIMEOUT_MS = 25_000

        /** The message limit the server publishes, or its fallback. See [AILimits]. */
        val maxMessageCharacters: Int get() = AILimits.current.sourceCharacters

        /** The instruction limit the server publishes, or its fallback. */
        val maxInstructionCharacters: Int get() = AILimits.current.instructionCharacters
    }
}
