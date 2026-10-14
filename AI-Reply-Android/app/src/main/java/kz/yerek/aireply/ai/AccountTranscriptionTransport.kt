package kz.yerek.aireply.ai

import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kz.yerek.aireply.data.account.AccountSession
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.ApiException
import kz.yerek.aireply.data.account.MultipartBody
import kz.yerek.aireply.voice.TranscriptionRequest

/**
 * Sends a finished recording to the authenticated `/api/v1/ai/transcribe`
 * endpoint and reads the words back.
 *
 * Жазба серверге жіберіледі: OpenAI кілті құрылғыда емес, серверде.
 *
 * The body is `multipart/form-data`: the audio file and four short fields -
 * the request id (the same for every attempt with the same audio, so a repeat
 * comes from the server's cache), the length of the audio, the language hints
 * and what the words are for. Nothing else. Only used once the server
 * announced the endpoint ([AIFeatures.voiceTranscription]).
 */
class AccountTranscriptionTransport(
    private val baseUrl: String,
    private val session: AccountSession,
    /** Longer than a reply's: the upload and the transcription both happen inside it. */
    private val readTimeoutMs: Int = READ_TIMEOUT_MS,
    /** How a request is sent; a test seam. */
    private val clientFactory: (baseUrl: String) -> ApiClient = { url ->
        ApiClient(url, AIConfiguration.REQUEST_TIMEOUT_MS)
    }
) : TranscriptionTransport {

    override suspend fun transcribe(request: TranscriptionRequest): Transcript {
        if (!session.isSignedIn) AIReplyError.AuthenticationFailed.raise()

        val client = clientFactory(baseUrl)
        val form = form(request)
        // One deadline over all of it: a write never times out on its own, and
        // an uplink that stalls mid-upload would otherwise leave the keyboard
        // recognising forever. The cancellation closes the connection.
        val payload = try {
            withTimeoutOrNull(TOTAL_TIMEOUT_MS) {
                session.authenticated { token ->
                    client.postMultipart("api/v1/ai/transcribe", form, token, readTimeoutMs)
                }
            } ?: AIReplyError.TimedOut.raise()
        } catch (exception: ApiException) {
            throw AIReplyException(map(exception.error))
        }

        return decode(payload) ?: AIReplyError.ServiceUnavailable.raise()
    }

    /**
     * `{"text": "...", "languages": ["kk"], "duration_ms": 12345}`. The text
     * is required (it may be empty); the languages are advisory, so a shape
     * this build does not expect is dropped rather than losing the words.
     */
    @Serializable
    internal data class TranscriptionResponse(
        val text: String,
        val languages: JsonElement? = null,
        @SerialName("duration_ms") val durationMs: Long? = null
    )

    companion object {
        /** Upload, `gpt-transcribe` and the answer for up to 40 s of audio, with room to spare. */
        const val READ_TIMEOUT_MS = 45_000

        /** The whole request, a token refresh included: past this it has failed, whatever the socket says. */
        const val TOTAL_TIMEOUT_MS = 75_000L

        private val json = Json {
            ignoreUnknownKeys = true
            encodeDefaults = true
            explicitNulls = false
        }

        private val ISO_639_1 = Regex("[a-z]{2}")

        /**
         * The wire format, exposed so tests can pin it without a server.
         * `languages` is left out when there is no usable hint: an empty field
         * is not a hint.
         */
        internal fun form(
            request: TranscriptionRequest,
            boundary: String = MultipartBody.randomBoundary()
        ): MultipartBody {
            val audio = request.audio
            val hints = languageHints(request.languages)
            return MultipartBody(
                parts = buildList {
                    add(MultipartBody.Part.Field("request_id", request.requestId))
                    add(MultipartBody.Part.Field("duration_ms", audio.durationMs.coerceAtLeast(0).toString()))
                    if (hints.isNotEmpty()) add(MultipartBody.Part.Field("languages", hints.joinToString(",")))
                    add(MultipartBody.Part.Field("purpose", request.purpose.wire))
                    add(MultipartBody.Part.FilePart("audio", audio.file, contentType = audio.mimeType))
                },
                boundary = boundary
            )
        }

        /** Two-letter lowercase codes, the most likely first, each once. */
        internal fun languageHints(languages: List<String>): List<String> =
            languages.map { it.trim().lowercase() }.filter { it.matches(ISO_639_1) }.distinct()

        /** The transcript, or null when the body is not one. */
        internal fun decode(payload: String): Transcript? {
            val decoded = runCatching {
                json.decodeFromString(TranscriptionResponse.serializer(), payload)
            }.getOrNull() ?: return null
            return Transcript(
                text = decoded.text,
                languages = detectedLanguages(decoded.languages),
                durationMs = decoded.durationMs
            )
        }

        /** `["kk"]` as the server documents it; OpenAI's own `[{"code": "kk"}]` is read too. */
        private fun detectedLanguages(element: JsonElement?): List<String> =
            (element as? JsonArray).orEmpty().mapNotNull { entry ->
                val code = when (entry) {
                    is JsonPrimitive -> entry.contentOrNull
                    is JsonObject -> (entry["code"] as? JsonPrimitive)?.contentOrNull
                    else -> null
                }
                code?.trim()?.lowercase()?.takeIf { it.isNotEmpty() }
            }

        /**
         * The closed error set. The daily voice cap and a burst of requests are
         * different problems - tomorrow versus a few seconds - so they stay
         * different errors, as the reply quota does.
         */
        fun map(error: ApiError): AIReplyError = when (error) {
            is ApiError.Offline -> AIReplyError.Offline
            is ApiError.TimedOut, is ApiError.ProviderTimeout -> AIReplyError.TimedOut
            is ApiError.Cancelled -> AIReplyError.Cancelled
            is ApiError.Unauthorized, is ApiError.AccountDisabled -> AIReplyError.AuthenticationFailed
            is ApiError.ConsentRequired -> AIReplyError.ConsentRequired
            is ApiError.VoiceLimitReached -> AIReplyError.VoiceLimitReached
            is ApiError.RateLimited -> AIReplyError.RateLimited
            // A server without the endpoint (404, 405), audio it could not
            // read (400), a 5xx or a body that is not a transcript: nothing
            // the user fixes by speaking again.
            else -> AIReplyError.ServiceUnavailable
        }
    }
}
