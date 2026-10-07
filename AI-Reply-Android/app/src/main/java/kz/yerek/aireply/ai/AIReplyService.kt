package kz.yerek.aireply.ai

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.ensureActive
import kz.yerek.aireply.BuildConfig
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.core.text.clampToCodePoints
import kz.yerek.aireply.core.text.codePointLength
import kz.yerek.aireply.domain.model.ReplyConfiguration
import kz.yerek.aireply.domain.model.ReplyTemplate
import java.time.LocalDateTime
import kotlin.coroutines.coroutineContext

/**
 * The single entry point for generating a reply.
 *
 * Responsibilities, and nothing else: validate the input, assemble the context,
 * choose a transport, run the call, map every failure onto [AIReplyError]. No
 * UI, no storage writes, no clipboard access and no knowledge of which screen
 * asked.
 *
 * The keyboard and the app both go through it, so what a user sees in "Try a
 * reply" is what the keyboard will produce. A separate code path would drift.
 */
class AIReplyService(
    private val configuration: AIConfiguration,
    private val nameTemplate: (ReplyTemplate, AppLanguage) -> String,
    /**
     * Builds the authenticated account transport. Injected rather than
     * constructed here so this class stays free of storage and DI concerns.
     */
    private val accountTransport: (String, AccountReplyTransport.RequestContext) -> ReplyTransport,
    /**
     * Replaces the account transport for a request, or returns null to use
     * it. Tests use this, and so do DEBUG builds' canned replies.
     */
    private val transportOverride: ((Request, ReplyPromptBuilder.Prompt) -> ReplyTransport?)? = null
) {

    /**
     * What the caller supplies. The service never reads the clipboard itself;
     * the message arrives already acquired by an explicit user action.
     */
    data class Request(
        val message: String,
        val template: ReplyTemplate,
        val configuration: ReplyConfiguration,
        /**
         * The APP's language, used only to name the selected template in the
         * prompt the way the user saw it on the chip. It is NOT the reply
         * language: that follows the incoming message, always.
         */
        val uiLanguage: AppLanguage,
        /** What the user typed or dictated for this reply. May be blank. */
        val instruction: String = "",
        /**
         * The keyboard layout on screen when Reply was tapped. A hint for the
         * server when the incoming message itself is too short to tell; null
         * outside the keyboard.
         */
        val inputLanguage: KeyboardLanguage? = null,
        /**
         * Injectable so working-hours behaviour is testable without waiting for
         * 18:30.
         */
        val now: LocalDateTime = LocalDateTime.now()
    )

    suspend fun generate(request: Request): GeneratedReply {
        val message = when (val result = validate(request.message)) {
            is ValidationResult.Valid -> result.message
            is ValidationResult.Invalid -> result.error.raise()
        }


        val profile = request.configuration.profile
        val templateName = nameTemplate(request.template, request.uiLanguage)
        val instruction = request.instruction
            .trim()
            .clampToCodePoints(AIConfiguration.maxInstructionCharacters)

        // The template's own schedule wins when it has one; otherwise the
        // profile's applies. Resolved HERE, deterministically, so the model is
        // never asked to work out what "after hours" means — it is told.
        val hours = request.template.effectiveWorkingHours(profile.workingHours)

        // Only computed when the user actually enabled working hours, so a
        // profile without them sends no time context at all.
        val businessContext = if (hours.isEnabled) hours.context(request.now) else null

        val prompt = ReplyPromptBuilder.build(
            ReplyPromptBuilder.Input(
                message = message,
                template = request.template,
                templateName = templateName,
                profileDescription = profile.promptDescription,
                profileRole = profile.role,
                preferredTone = profile.preferredTone,
                profileBusiness = profile.business,
                templateBusiness = request.template.effectiveBusiness,
                businessContext = businessContext,
                userInstruction = instruction
            )
        )

        val override = transportOverride?.invoke(request, prompt)
        // Only the real service needs an account; an override does not.
        if (override == null && !configuration.isReady) AIReplyError.AuthenticationFailed.raise()
        val transport = override
            ?: makeTransport(request, message, templateName, instruction, businessContext)

        return try {
            val reply = transport.generate(prompt)
            coroutineContext.ensureActive()
            reply
        } catch (cancellation: CancellationException) {
            throw cancellation
        } catch (exception: AIReplyException) {
            throw exception
        } catch (throwable: Throwable) {
            throw AIReplyException(ReplyNetworking.mapError(throwable))
        }
    }

    private fun makeTransport(
        request: Request,
        message: String,
        templateName: String,
        instruction: String,
        businessContext: kz.yerek.aireply.domain.model.WorkingHours.Context?
    ): ReplyTransport {
        val template = request.template
        val profile = request.configuration.profile
        return accountTransport(
            configuration.backendBaseUrl,
            AccountReplyTransport.RequestContext(
                message = message,
                userInstruction = instruction,
                templateId = template.id,
                templateName = templateName,
                templateRelationship = template.relationship.raw,
                templateTone = template.tone,
                templateInstructions = template.instructions,
                templateReplyLength = template.replyLength,
                templateEmojiPolicy = template.emojiPolicy,
                templateWorkingHoursBehaviour = template.workingHoursBehaviour,
                templateBusiness = template.effectiveBusiness,
                appLanguage = request.uiLanguage.code,
                business = businessContext,
                appVersion = BuildConfig.VERSION_NAME,
                profileDescription = profile.promptDescription,
                profileRole = profile.role.trim(),
                profileTone = profile.preferredTone,
                profileBusiness = profile.business,
                grammaticalGender = profile.grammaticalGender,
                inputLanguage = request.inputLanguage?.code
            )
        )
    }

    sealed interface ValidationResult {
        data class Valid(val message: String) : ValidationResult
        data class Invalid(val error: AIReplyError) : ValidationResult
    }

    companion object {
        /**
         * The message-length rule, with the limit the server publishes
         * ([AILimits]; 400 until the server says otherwise).
         *
         * It applies to the INCOMING MESSAGE ONLY. The profile, the template
         * instructions, the user's instruction and the developer rules are
         * separate and are not counted against it — the limit exists to keep one
         * pasted chat message sane, not to cap the prompt.
         *
         * Counted in code points, which is what the user sees as characters for
         * these three languages and what the backend counts too.
         */
        fun validate(message: String, limit: Int = AIConfiguration.maxMessageCharacters): ValidationResult {
            val trimmed = message.trim()
            if (trimmed.isEmpty()) return ValidationResult.Invalid(AIReplyError.NoSourceMessage)
            if (trimmed.codePointLength() > limit) {
                return ValidationResult.Invalid(AIReplyError.MessageTooLong(limit))
            }
            return ValidationResult.Valid(trimmed)
        }

        fun characterCount(message: String): Int = message.trim().codePointLength()
    }
}
