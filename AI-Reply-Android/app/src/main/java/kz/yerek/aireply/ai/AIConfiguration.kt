package kz.yerek.aireply.ai

/** Fixed production configuration shared by the app and input method. */
class AIConfiguration(private val isAccountSignedIn: () -> Boolean) {

    val backendBaseUrl: String = DEFAULT_BACKEND_BASE_URL
    val requiresAccount: Boolean = true
    val isReady: Boolean get() = isAccountSignedIn()

    companion object {
        const val DEFAULT_BACKEND_BASE_URL = "https://api.meily.kz"
        const val MAX_OUTPUT_TOKENS = 180
        const val REQUEST_TIMEOUT_MS = 25_000

        /** The message limit the server publishes, or its fallback. See [AILimits]. */
        val maxMessageCharacters: Int get() = AILimits.current.sourceCharacters

        /** The instruction limit the server publishes, or its fallback. */
        val maxInstructionCharacters: Int get() = AILimits.current.instructionCharacters
    }
}
