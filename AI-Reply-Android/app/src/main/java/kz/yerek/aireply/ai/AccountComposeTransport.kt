package kz.yerek.aireply.ai

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.ApiException
import kz.yerek.aireply.data.account.AccountSession
import kz.yerek.aireply.data.account.AccountUsageCache
import kz.yerek.aireply.data.account.ComposeResponseDto

/**
 * Writes a message through the authenticated `/api/v1/ai/compose` endpoint.
 *
 * Хабарлама серверде жазылады: құрылғыда провайдер кілті жоқ.
 *
 * The body is the instruction and nothing else - no copied text, no profile,
 * no contacts. The response carries the quota back, like a reply's.
 */
class AccountComposeTransport(
    private val baseUrl: String,
    private val session: AccountSession,
    private val usageCache: AccountUsageCache,
    private val appVersion: String,
    private val timeoutMs: Int = AIConfiguration.REQUEST_TIMEOUT_MS
) : ComposeTransport {

    override suspend fun compose(request: ComposeService.Request): GeneratedReply {
        if (!session.isSignedIn) AIReplyError.AuthenticationFailed.raise()

        val client = ApiClient(baseUrl, timeoutMs)
        val body = encode(request, appVersion)
        val payload = try {
            session.authenticated { token ->
                client.request("POST", "api/v1/ai/compose", body, token)
            }
        } catch (exception: ApiException) {
            throw AIReplyException(map(exception.error))
        }

        val decoded = runCatching {
            json.decodeFromString(ComposeResponseDto.serializer(), payload)
        }.getOrNull() ?: AIReplyError.ServiceUnavailable.raise()

        usageCache.store(decoded.usage)

        val text = ReplyNetworking.unwrapQuotes(decoded.text.trim())
        if (text.isEmpty()) AIReplyError.EmptyResponse.raise()
        return GeneratedReply(text = text, detectedLanguage = decoded.detectedLanguage)
    }

    @Serializable
    internal data class ComposeRequest(
        val instruction: String,
        val language: String,
        val regenerate: Boolean,
        val platform: String,
        @SerialName("app_version") val appVersion: String
    )

    companion object {
        private val json = Json {
            ignoreUnknownKeys = true
            encodeDefaults = true
            explicitNulls = false
        }

        /** The wire format, exposed so tests can pin it without a server. */
        internal fun encode(request: ComposeService.Request, appVersion: String): String =
            json.encodeToString(
                ComposeRequest.serializer(),
                ComposeRequest(
                    instruction = request.instruction,
                    language = request.uiLanguage.code,
                    regenerate = request.isRegeneration,
                    platform = "android",
                    appVersion = appVersion
                )
            )

        /**
         * Same closed set as replies; only the request-shape errors differ,
         * because here the user's text is the instruction, not a copied message.
         */
        fun map(error: ApiError): AIReplyError = when (error) {
            is ApiError.InstructionTooLong -> AIReplyError.InstructionTooLong(error.limit)
            is ApiError.InstructionMissing -> AIReplyError.NoInstruction
            // A server without the endpoint (not deployed yet) or a request it
            // could not read: nothing the user can fix by rewording.
            is ApiError.NotFound, is ApiError.InvalidRequest, is ApiError.SourceTooLong ->
                AIReplyError.ServiceUnavailable
            else -> AccountReplyTransport.map(error)
        }
    }
}
