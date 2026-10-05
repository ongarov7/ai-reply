package kz.yerek.aireply.ai

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.ensureActive
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.core.text.codePointLength
import kotlin.coroutines.coroutineContext

/**
 * A cleaner version of the instruction the user typed for the AI: typos,
 * punctuation, capitals - never different words or meaning (DESIGN §2.6,
 * §6.5). Only ever a suggestion the user may take.
 *
 * Нұсқаудың түзетілген нұсқасы: тек ұсыныс, өздігінен ауыстырмайды.
 *
 *  * Only the instruction is sent: never the copied message, never a draft.
 *  * It spends no reply quota; the server keeps its own small rate limit.
 *  * Every failure is quiet for the user: the caller simply shows nothing.
 */
class PolishService(
    private val configuration: AIConfiguration,
    /** Builds the authenticated transport for the backend at the given URL. */
    private val accountTransport: (String) -> PolishTransport,
    /** Replaces the account transport, or returns null to use it. Tests and DEBUG mocks. */
    private val transportOverride: (() -> PolishTransport?)? = null
) {

    data class Request(
        /** The instruction as the user typed it. */
        val text: String,
        /** The keyboard layout on screen: a hint for the server's language check. */
        val inputLanguage: KeyboardLanguage? = null
    )

    /** Whether a suggestion is possible without asking anyone: an account, or a DEBUG mock. */
    val isAvailable: Boolean get() = configuration.isReady || transportOverride?.invoke() != null

    /**
     * The polished instruction, or null when there is nothing to suggest:
     * the text is too short to bother, the server found it fine, or its
     * version would not fit the instruction's [limit] (the server allows a
     * little growth, and taking it would only make Reply fail). Throws
     * [AIReplyException] when the request failed.
     */
    suspend fun polish(request: Request, limit: Int = AILimits.current.instructionCharacters): String? {
        val text = request.text.trim()
        if (!qualifies(text, limit)) return null

        val override = transportOverride?.invoke()
        if (override == null && !configuration.isReady) AIReplyError.AuthenticationFailed.raise()
        val transport = override ?: accountTransport(configuration.backendBaseUrl)

        val polished = try {
            val result = transport.polish(request.copy(text = text))
            coroutineContext.ensureActive()
            result
        } catch (cancellation: CancellationException) {
            throw cancellation
        } catch (exception: AIReplyException) {
            throw exception
        } catch (throwable: Throwable) {
            throw AIReplyException(ReplyNetworking.mapError(throwable))
        }
        return polished?.trim()?.takeIf { it.isNotEmpty() && it != text && it.codePointLength() <= limit }
    }

    companion object {
        /** Shorter notes are left alone: too little to fix, not worth a request. */
        const val MIN_CHARACTERS = 12
        const val MIN_WORDS = 3

        /**
         * Long enough to be worth polishing ([MIN_CHARACTERS], [MIN_WORDS])
         * and within the instruction limit the server enforces.
         */
        fun qualifies(text: String, limit: Int = AILimits.current.instructionCharacters): Boolean {
            val trimmed = text.trim()
            val length = trimmed.codePointLength()
            if (length < MIN_CHARACTERS || length > limit) return false
            return trimmed.split(WHITESPACE).count { word -> word.any(Char::isLetterOrDigit) } >= MIN_WORDS
        }

        private val WHITESPACE = Regex("\\s+")
    }
}

/** Where a polished instruction comes from: the backend, or a test double. Null: nothing to suggest. */
fun interface PolishTransport {
    suspend fun polish(request: PolishService.Request): String?
}
