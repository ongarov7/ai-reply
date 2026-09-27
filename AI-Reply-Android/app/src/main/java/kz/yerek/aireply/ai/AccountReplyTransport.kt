package kz.yerek.aireply.ai

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.ApiException
import kz.yerek.aireply.data.account.AccountSession
import kz.yerek.aireply.data.account.AccountUsageCache
import kz.yerek.aireply.data.account.ReplyResponseDto
import kz.yerek.aireply.domain.model.BusinessContext
import kz.yerek.aireply.domain.model.EmojiPolicy
import kz.yerek.aireply.domain.model.ReplyLength
import kz.yerek.aireply.domain.model.ReplyTone
import kz.yerek.aireply.domain.model.WorkingHours
import kz.yerek.aireply.domain.model.WorkingHoursBehaviour

/**
 * Generates a reply through the authenticated `/api/v1/ai/reply` endpoint.
 *
 * Жауап серверде жасалады: құрылғыда провайдер кілті жоқ.
 *
 * This is the transport a signed-in user gets. Compared with the legacy
 * install-token path it adds two things: the request is attributed to a real
 * account, and the response carries the quota back, so the keyboard can show
 * "3 left today" without a second round trip. The profile is not sent — the
 * server holds it — but the template and working-hours context still are,
 * because those are per-reply choices the user just made on screen.
 */
class AccountReplyTransport(
    private val baseUrl: String,
    private val session: AccountSession,
    private val usageCache: AccountUsageCache,
    private val context: RequestContext,
    private val timeoutMs: Int = AIConfiguration.REQUEST_TIMEOUT_MS
) : ReplyTransport {

    /**
     * What this one reply needs. No device identifier, no contacts, no chat
     * history, no location: only the message being answered and how to answer it.
     */
    data class RequestContext(
        val message: String,
        /** What the user typed or dictated for this reply. May be blank. */
        val userInstruction: String,
        val templateId: String,
        val templateName: String,
        val templateRelationship: String,
        val templateTone: ReplyTone,
        val templateInstructions: String,
        val templateReplyLength: ReplyLength,
        val templateEmojiPolicy: EmojiPolicy,
        val templateWorkingHoursBehaviour: WorkingHoursBehaviour,
        val templateBusiness: BusinessContext?,
        /**
         * The APP's language, used for logging and template naming only. The
         * reply's language follows the incoming message, always.
         */
        val appLanguage: String,
        val business: WorkingHours.Context?,
        val appVersion: String
    )

    override suspend fun generate(prompt: ReplyPromptBuilder.Prompt): GeneratedReply {
        if (!session.isSignedIn) AIReplyError.AuthenticationFailed.raise()

        val client = ApiClient(baseUrl, timeoutMs)
        val body = json.encodeToString(
            ReplyRequest.serializer(),
            ReplyRequest(
                sourceText = context.message,
                instruction = context.userInstruction.trim().ifEmpty { null },
                language = context.appLanguage,
                templateId = context.templateId,
                template = Template(
                    name = context.templateName,
                    relationship = context.templateRelationship,
                    tone = context.templateTone.raw,
                    instructions = context.templateInstructions,
                    replyLength = context.templateReplyLength.raw,
                    emojiPolicy = context.templateEmojiPolicy.raw,
                    workingHoursBehaviour = context.templateWorkingHoursBehaviour.raw,
                    business = Business.of(context.templateBusiness)
                ),
                businessContext = context.business?.let {
                    WorkingHoursBlock(
                        enabled = it.isEnabled,
                        isWithinWorkingHours = it.isWithinWorkingHours,
                        currentLocalTime = it.currentLocalTime,
                        nextWorkingPeriod = it.nextWorkingPeriod,
                        weeklySchedule = it.weeklySchedule
                    )
                },
                platform = "android",
                appVersion = context.appVersion
            )
        )

        val payload = try {
            session.authenticated { token ->
                client.request("POST", "api/v1/ai/reply", body, token)
            }
        } catch (exception: ApiException) {
            throw AIReplyException(map(exception.error))
        }

        val decoded = runCatching {
            json.decodeFromString(ReplyResponseDto.serializer(), payload)
        }.getOrNull() ?: AIReplyError.ServiceUnavailable.raise()

        // The server's count, cached so the keyboard can render it instantly.
        usageCache.store(decoded.usage)

        val text = ReplyNetworking.unwrapQuotes(decoded.reply.trim())
        if (text.isEmpty()) AIReplyError.EmptyResponse.raise()
        return GeneratedReply(text = text, detectedLanguage = decoded.detectedLanguage)
    }

    // ----------------------------------------------------------- wire format

    @Serializable
    private data class ReplyRequest(
        @SerialName("source_text") val sourceText: String,
        val instruction: String? = null,
        val language: String,
        @SerialName("template_id") val templateId: String,
        val template: Template,
        @SerialName("business_context") val businessContext: WorkingHoursBlock? = null,
        val platform: String,
        @SerialName("app_version") val appVersion: String
    )

    @Serializable
    private data class Business(
        val offering: String? = null,
        val summary: String? = null,
        val rules: List<String>? = null
    ) {
        companion object {
            fun of(source: BusinessContext?): Business? {
                if (source == null || source.isEmpty) return null
                return Business(
                    offering = source.offering.trim().ifEmpty { null },
                    summary = source.summary.trim().ifEmpty { null },
                    rules = source.cleanRules.ifEmpty { null }
                )
            }
        }
    }

    @Serializable
    private data class Template(
        val name: String,
        val relationship: String,
        val tone: String,
        val instructions: String,
        @SerialName("reply_length") val replyLength: String,
        @SerialName("emoji_policy") val emojiPolicy: String,
        @SerialName("working_hours_behaviour") val workingHoursBehaviour: String,
        val business: Business? = null
    )

    @Serializable
    private data class WorkingHoursBlock(
        val enabled: Boolean,
        @SerialName("is_within_working_hours") val isWithinWorkingHours: Boolean,
        @SerialName("current_local_time") val currentLocalTime: String,
        @SerialName("next_working_period") val nextWorkingPeriod: String? = null,
        @SerialName("weekly_schedule") val weeklySchedule: String? = null
    )

    companion object {
        private val json = Json {
            ignoreUnknownKeys = true
            encodeDefaults = true
            explicitNulls = false
        }

        /**
         * Backend failures become the closed set the UI already knows how to
         * show.
         *
         * A spent quota and a burst of requests are different problems with
         * different fixes - change plan or wait until tomorrow, versus wait a
         * few seconds - so they stay different errors. They used to share one,
         * and a user who tapped Regenerate twice was told their day's replies
         * were gone.
         */
        fun map(error: ApiError): AIReplyError = when (error) {
            is ApiError.Offline -> AIReplyError.Offline
            is ApiError.TimedOut, is ApiError.ProviderTimeout -> AIReplyError.TimedOut
            is ApiError.Cancelled -> AIReplyError.Cancelled
            is ApiError.Unauthorized, is ApiError.AccountDisabled -> AIReplyError.AuthenticationFailed
            is ApiError.DailyLimitReached, is ApiError.SubscriptionExpired,
            is ApiError.PaymentRequired -> AIReplyError.QuotaExhausted
            is ApiError.RateLimited -> AIReplyError.RateLimited
            is ApiError.EmptyResponse -> AIReplyError.EmptyResponse
            is ApiError.SourceTooLong -> {
                // The server is the source of truth: remember its limit, so
                // the counter and the next check use it.
                AILimits.storeSourceLimit(error.limit)
                AIReplyError.MessageTooLong(error.limit)
            }
            is ApiError.InvalidRequest ->
                AIReplyError.MessageTooLong(AILimits.current.sourceCharacters)
            else -> AIReplyError.ServiceUnavailable
        }
    }
}
