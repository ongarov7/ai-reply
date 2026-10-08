package kz.yerek.aireply.ai

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.ensureActive
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kotlin.coroutines.coroutineContext

/**
 * Writes a NEW message from the user's description - the keyboard's "Create"
 * mode. The second AI flow, independent of replying:
 *
 *  * there is no incoming message: the clipboard is never read, nothing copied
 *    is ever sent, and the reply prompt is not involved;
 *  * the request carries the instruction (plus, for a server that accepts
 *    them, the sender's grammatical gender and the layout language), and the
 *    server writes the message with its own compose prompt
 *    (`POST /api/v1/ai/compose`);
 *  * it spends the same daily quota as a reply and fails with the same closed
 *    set of [AIReplyError]s.
 */
class ComposeService(
    private val configuration: AIConfiguration,
    /** Builds the authenticated transport for the backend at the given URL. */
    private val accountTransport: (String) -> ComposeTransport,
    /** Replaces the account transport, or returns null to use it. Tests and DEBUG mocks. */
    private val transportOverride: ((Request) -> ComposeTransport?)? = null
) {

    data class Request(
        /** What the user wants written, in their own words. */
        val instruction: String,
        /**
         * The APP's language. The message follows the language of the
         * instruction; the server uses this only when that is unclear.
         */
        val uiLanguage: AppLanguage,
        /** Another version of a message the user has already seen. */
        val isRegeneration: Boolean = false,
        /**
         * The keyboard layout on screen when Write was tapped: the next hint
         * after the instruction's own language. Null outside the keyboard.
         */
        val inputLanguage: KeyboardLanguage? = null
    )

    suspend fun compose(request: Request): GeneratedReply {
        val instruction = when (val result = validate(request.instruction)) {
            is ValidationResult.Valid -> result.instruction
            is ValidationResult.Invalid -> result.error.raise()
        }
        val prepared = request.copy(instruction = instruction)

        val override = transportOverride?.invoke(prepared)
        if (override == null) configuration.checkReady()
        val transport = override ?: accountTransport(configuration.backendBaseUrl)

        return try {
            val message = transport.compose(prepared)
            coroutineContext.ensureActive()
            message
        } catch (cancellation: CancellationException) {
            throw cancellation
        } catch (exception: AIReplyException) {
            configuration.requestFailed(exception.error)
            throw exception
        } catch (throwable: Throwable) {
            throw AIReplyException(ReplyNetworking.mapError(throwable))
        }
    }

    sealed interface ValidationResult {
        data class Valid(val instruction: String) : ValidationResult
        data class Invalid(val error: AIReplyError) : ValidationResult
    }

    companion object {
        /**
         * Not empty, and within the limit the server publishes as
         * `max_instruction_length` (400 unless an administrator changed it).
         * Counted in code points, like the server.
         */
        fun validate(
            instruction: String,
            limit: Int = AILimits.current.instructionCharacters
        ): ValidationResult {
            val trimmed = instruction.trim()
            if (trimmed.isEmpty()) return ValidationResult.Invalid(AIReplyError.NoInstruction)
            if (trimmed.codePointCount(0, trimmed.length) > limit) {
                return ValidationResult.Invalid(AIReplyError.InstructionTooLong(limit))
            }
            return ValidationResult.Valid(trimmed)
        }
    }
}

/** Where a composed message comes from: the backend, or a test double. */
fun interface ComposeTransport {
    suspend fun compose(request: ComposeService.Request): GeneratedReply
}
