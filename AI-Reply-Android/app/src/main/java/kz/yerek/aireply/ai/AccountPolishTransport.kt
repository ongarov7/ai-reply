package kz.yerek.aireply.ai

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.ApiException
import kz.yerek.aireply.data.account.AccountSession

/**
 * Asks the authenticated `/api/v1/ai/polish` endpoint for a cleaner version of
 * the user's instruction.
 *
 * Нұсқауды серверде түзету: квота жұмсалмайды, қате болса ештеңе көрсетілмейді.
 *
 * The body is the instruction and the layout code, nothing else. Only used
 * once the server announced the endpoint ([AIFeatures.instructionPolish]); the
 * endpoint itself always accepts `input_language`.
 */
class AccountPolishTransport(
    private val baseUrl: String,
    private val session: AccountSession,
    private val appVersion: String,
    /** Short: a suggestion that arrives late is a suggestion nobody wants. */
    private val timeoutMs: Int = TIMEOUT_MS
) : PolishTransport {

    override suspend fun polish(request: PolishService.Request): String? {
        if (!session.isSignedIn) AIReplyError.AuthenticationFailed.raise()

        val client = ApiClient(baseUrl, timeoutMs)
        val payload = try {
            session.authenticated { token ->
                client.request("POST", "api/v1/ai/polish", encode(request, appVersion), token)
            }
        } catch (exception: ApiException) {
            throw AIReplyException(map(exception.error))
        }

        val decoded = runCatching { json.decodeFromString(PolishResponse.serializer(), payload) }.getOrNull()
            ?: AIReplyError.ServiceUnavailable.raise()
        return decoded.text.takeIf { decoded.changed }
    }

    @Serializable
    internal data class PolishRequest(
        val text: String,
        @SerialName("input_language") val inputLanguage: String? = null,
        val platform: String,
        @SerialName("app_version") val appVersion: String
    )

    @Serializable
    internal data class PolishResponse(
        val text: String = "",
        val changed: Boolean = false
    )

    companion object {
        const val TIMEOUT_MS = 10_000

        private val json = Json {
            ignoreUnknownKeys = true
            encodeDefaults = true
            explicitNulls = false
        }

        /** The wire format, exposed so tests can pin it without a server. */
        internal fun encode(request: PolishService.Request, appVersion: String): String =
            json.encodeToString(
                PolishRequest.serializer(),
                PolishRequest(
                    text = request.text,
                    inputLanguage = request.inputLanguage?.code,
                    platform = "android",
                    appVersion = appVersion
                )
            )

        /** The closed error set; the keyboard shows none of them, it only counts failures. */
        fun map(error: ApiError): AIReplyError = when (error) {
            is ApiError.Offline -> AIReplyError.Offline
            is ApiError.TimedOut, is ApiError.ProviderTimeout -> AIReplyError.TimedOut
            is ApiError.Cancelled -> AIReplyError.Cancelled
            is ApiError.Unauthorized, is ApiError.AccountDisabled -> AIReplyError.AuthenticationFailed
            is ApiError.RateLimited -> AIReplyError.RateLimited
            else -> AIReplyError.ServiceUnavailable
        }
    }
}
