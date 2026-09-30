package kz.yerek.aireply.data.account

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.isActive
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.Json
import java.io.IOException
import java.net.ConnectException
import java.net.HttpURLConnection
import java.net.NoRouteToHostException
import java.net.SocketTimeoutException
import java.net.URL
import java.net.UnknownHostException
import javax.net.ssl.SSLException
import kotlin.coroutines.coroutineContext

/**
 * Every way a backend call can fail, as a value the UI can localize.
 *
 * Сервер қатесі — тұрақты код, аудармасы қосымшада.
 *
 * The server sends a stable `code`; the English `message` next to it is for a
 * developer reading a log, never for a user. Mapping happens once, here, so no
 * screen has to know what an HTTP status means.
 */
sealed interface ApiError {
    data object Offline : ApiError
    data object TimedOut : ApiError
    data object Cancelled : ApiError

    /** The session is gone: the user has to sign in again. */
    data object Unauthorized : ApiError
    data object AccountDisabled : ApiError

    /** A wrong code. [attemptsRemaining] is how many more tries it allows, when the server says. */
    data class InvalidOtp(val attemptsRemaining: Int? = null) : ApiError
    data object OtpExpired : ApiError
    /** The code was used already, or a newer one replaced it. */
    data object OtpAlreadyUsed : ApiError
    /** Too many wrong tries: this code is dead and a new one is needed. */
    data object OtpAttemptsExceeded : ApiError
    /** A code was sent moments ago; the next may be asked for after [retryAfterSeconds]. */
    data class ResendCooldown(val retryAfterSeconds: Int?) : ApiError
    data object InvalidEmail : ApiError
    /** The server could not hand the e-mail to its mail provider. */
    data object EmailDeliveryFailed : ApiError
    /** The address already belongs to another account. */
    data object EmailInUse : ApiError
    /** The server refused Google's ID token. Not a session problem: nothing to refresh. */
    data object InvalidIdToken : ApiError
    /** The server cannot check Google or Apple tokens right now. */
    data object AuthProviderUnavailable : ApiError

    data class RateLimited(val retryAfterSeconds: Int?) : ApiError

    /** Daily quota is spent. [resetsAt] is ISO-8601 from the server. */
    data class DailyLimitReached(
        val limit: Int,
        val usedToday: Int,
        val resetsAt: String?
    ) : ApiError

    data object SubscriptionExpired : ApiError
    data object PaymentRequired : ApiError
    data object ProviderUnavailable : ApiError
    data object ProviderTimeout : ApiError
    data object EmptyResponse : ApiError
    data object InvalidRequest : ApiError
    /** The incoming message is longer than the server's limit, which it sent. */
    data class SourceTooLong(val limit: Int) : ApiError
    data object NotFound : ApiError
    data object Conflict : ApiError
    data object Server : ApiError

    /** The client could not make sense of the response at all. */
    data object MalformedResponse : ApiError
}

/**
 * Thrown across suspend boundaries; the payload is what the UI actually reads.
 *
 * [requestId] is the server's id for the request (from the error envelope or
 * the `X-Request-ID` response header), else the one this client sent: the
 * value to quote in a bug report, since the server's log line carries it too.
 * [httpStatus] is null for a failure that was not an HTTP exchange, and 0 when
 * the request got no HTTP answer at all ([transport] then says why).
 */
class ApiException(
    val error: ApiError,
    val requestId: String? = null,
    val httpStatus: Int? = null,
    val transport: TransportFailure? = null
) : Exception(error::class.simpleName)

fun ApiError.raise(): Nothing = throw ApiException(this)

/**
 * Thin HTTP client for the AI Reply backend.
 *
 * WHY HttpURLConnection, AGAIN. Same reason as [kz.yerek.aireply.ai.ReplyNetworking]:
 * this package is reachable from the input method, and a networking library
 * there is thousands of classes loaded while the user stares at a blank key
 * strip. Four verbs, one JSON codec, no interceptors.
 *
 * PATCH is deliberately absent: HttpURLConnection refuses it outright, so the
 * profile update goes through the server's POST alias instead of a reflection
 * hack on a platform class.
 */
class ApiClient(
    private val baseUrl: String,
    private val timeoutMs: Int = DEFAULT_TIMEOUT_MS,
    /** Which metadata headers go out; see [HeaderScope]. */
    private val scope: HeaderScope = HeaderScope.APP,
    /** Null reads [RequestMetadata.installed] at request time. Tests pass their own. */
    private val metadata: RequestMetadata? = null,
    /** The connection factory; a test seam, never replaced in the app. */
    private val openConnection: (URL) -> HttpURLConnection = { it.openConnection() as HttpURLConnection }
) {

    val json: Json = Json {
        ignoreUnknownKeys = true
        encodeDefaults = true
        explicitNulls = false
    }

    /**
     * Performs one request and returns the body, or throws [ApiException].
     *
     * The response body of a FAILED request is read for the error envelope only
     * and never logged: it can quote the request back, and a request can carry
     * a private message.
     */
    suspend fun request(
        method: String,
        path: String,
        body: String? = null,
        token: String? = null
    ): String = withContext(Dispatchers.IO) {
        val url = baseUrl.trimEnd('/') + "/" + path.trimStart('/')
        val requestId = RequestIds.next()
        // Read here, on the IO dispatcher: the first read of the installation
        // id touches a file.
        val extraHeaders = runCatching { (metadata ?: RequestMetadata.installed).headers(scope) }
            .getOrDefault(emptyMap())
        val connection = try {
            openConnection(URL(url)).apply {
                requestMethod = method
                connectTimeout = timeoutMs
                readTimeout = timeoutMs
                useCaches = false
                setRequestProperty("Accept", "application/json")
                if (body != null) setRequestProperty("Content-Type", "application/json; charset=utf-8")
                if (token != null) setRequestProperty("Authorization", "Bearer $token")
                extraHeaders.forEach { (name, value) -> setRequestProperty(name, value) }
                setRequestProperty(HEADER_REQUEST_ID, requestId)
                doOutput = body != null
            }
        } catch (throwable: Exception) {
            // A malformed URL or a header value the platform refuses: the
            // request never left, which is a transport failure, not a crash.
            throw transportFailure(path, throwable, requestId)
        }

        // Cancellation is real, not cooperative-only: the socket read is
        // unblocked as soon as the calling coroutine goes away.
        val disconnectOnCancel = coroutineContext[Job]?.invokeOnCompletion { cause ->
            if (cause != null) runCatching { connection.disconnect() }
        }

        try {
            if (body != null) {
                connection.outputStream.use { it.write(body.toByteArray(Charsets.UTF_8)) }
            }

            val status = connection.responseCode
            val stream = if (status in 200..299) connection.inputStream else connection.errorStream
            val text = stream?.bufferedReader(Charsets.UTF_8)?.use { it.readText() }.orEmpty()

            if (status !in 200..299) {
                throw ApiException(
                    error = mapServerError(status, text, connection.getHeaderField("Retry-After")),
                    requestId = requestIdOf(text) ?: connection.getHeaderField(HEADER_REQUEST_ID) ?: requestId,
                    httpStatus = status
                )
            }
            text
        } catch (cancellation: CancellationException) {
            throw cancellation
        } catch (api: ApiException) {
            throw api
        } catch (throwable: Throwable) {
            // A read unblocked by the cancellation above is not a network
            // problem worth reporting.
            throw transportFailure(path, throwable, requestId, report = coroutineContext.isActive)
        } finally {
            disconnectOnCancel?.dispose()
            runCatching { connection.disconnect() }
        }
    }

    /**
     * A request that got no HTTP answer. App-scope failures are reported to
     * [RequestMetadata.transportFailureObserver]; the keyboard's never are.
     */
    private fun transportFailure(
        path: String,
        throwable: Throwable,
        requestId: String,
        report: Boolean = true
    ): ApiException {
        val kind = transportKind(throwable)
        if (report && scope == HeaderScope.APP && throwable !is CancellationException) {
            RequestMetadata.transportFailureObserver?.let { observer ->
                runCatching { observer(TransportFailureReport(ApiRoutes.pattern(path), kind, requestId)) }
            }
        }
        return ApiException(
            error = mapTransportError(throwable),
            requestId = requestId,
            httpStatus = 0,
            transport = kind
        )
    }

    companion object {
        const val DEFAULT_TIMEOUT_MS = 25_000

        const val HEADER_REQUEST_ID = "X-Request-ID"

        private val envelopeJson = Json { ignoreUnknownKeys = true }

        /** `error.request_id` from an error envelope, when it is one. */
        fun requestIdOf(body: String): String? = runCatching {
            envelopeJson.decodeFromString<ErrorEnvelopeDto>(body).error.requestId
        }.getOrNull()?.takeIf { it.isNotBlank() && RequestIds.isValid(it) }

        /** Why a request got no HTTP answer, as diagnostics name it. */
        fun transportKind(throwable: Throwable): TransportFailure = when (throwable) {
            is SocketTimeoutException -> TransportFailure.TIMEOUT
            is UnknownHostException, is ConnectException, is NoRouteToHostException -> TransportFailure.OFFLINE
            is SSLException -> TransportFailure.TLS
            else -> TransportFailure.IO
        }

        /** Stable server codes first; the HTTP status only as a fallback. */
        fun mapServerError(status: Int, body: String, retryAfterHeader: String? = null): ApiError {
            val payload = runCatching {
                envelopeJson.decodeFromString<ErrorEnvelopeDto>(body).error
            }.getOrNull()
            val details = payload?.details
            val retryAfter = details?.retryAfterSeconds ?: retryAfterHeader?.toIntOrNull()

            when (payload?.code) {
                "UNAUTHORIZED", "TOKEN_EXPIRED" -> return ApiError.Unauthorized
                "ACCOUNT_DISABLED" -> return ApiError.AccountDisabled
                "INVALID_OTP" -> return ApiError.InvalidOtp(details?.attemptsRemaining)
                "OTP_EXPIRED" -> return ApiError.OtpExpired
                "OTP_ALREADY_USED" -> return ApiError.OtpAlreadyUsed
                "OTP_ATTEMPTS_EXCEEDED" -> return ApiError.OtpAttemptsExceeded
                "OTP_RESEND_COOLDOWN" -> return ApiError.ResendCooldown(retryAfter)
                "INVALID_EMAIL" -> return ApiError.InvalidEmail
                "EMAIL_DELIVERY_FAILED" -> return ApiError.EmailDeliveryFailed
                "EMAIL_ALREADY_IN_USE" -> return ApiError.EmailInUse
                // A 401, but about Google's token, not ours: no refresh, no sign-out.
                "INVALID_ID_TOKEN" -> return ApiError.InvalidIdToken
                "AUTH_PROVIDER_UNAVAILABLE" -> return ApiError.AuthProviderUnavailable
                "RATE_LIMITED" -> return ApiError.RateLimited(retryAfter)
                "DAILY_LIMIT_REACHED", "MONTHLY_LIMIT_REACHED" -> return ApiError.DailyLimitReached(
                    limit = details?.dailyLimit ?: 0,
                    usedToday = details?.usedToday ?: 0,
                    resetsAt = details?.resetsAt
                )
                "SUBSCRIPTION_EXPIRED" -> return ApiError.SubscriptionExpired
                "PAYMENT_REQUIRED" -> return ApiError.PaymentRequired
                "AI_PROVIDER_UNAVAILABLE" -> return ApiError.ProviderUnavailable
                "AI_TIMEOUT" -> return ApiError.ProviderTimeout
                "AI_EMPTY_RESPONSE" -> return ApiError.EmptyResponse
                "INVALID_REQUEST" -> {
                    val limit = details?.maxCharacters
                    return if (details?.field == "source_text" && limit != null) {
                        ApiError.SourceTooLong(limit)
                    } else {
                        ApiError.InvalidRequest
                    }
                }
                "NOT_FOUND" -> return ApiError.NotFound
                "CONFLICT" -> return ApiError.Conflict
            }

            // No envelope: a proxy error page, or the old /v1 shape.
            return when (status) {
                401, 403 -> ApiError.Unauthorized
                404 -> ApiError.NotFound
                408, 504 -> ApiError.ProviderTimeout
                409 -> ApiError.Conflict
                413 -> ApiError.InvalidRequest
                429 -> ApiError.RateLimited(retryAfter)
                in 400..499 -> ApiError.InvalidRequest
                else -> ApiError.Server
            }
        }

        fun mapTransportError(throwable: Throwable): ApiError = when (throwable) {
            is CancellationException -> ApiError.Cancelled
            is SocketTimeoutException -> ApiError.TimedOut
            is UnknownHostException, is ConnectException, is NoRouteToHostException -> ApiError.Offline
            is SSLException -> ApiError.Server
            is IOException -> ApiError.Server
            else -> ApiError.Server
        }
    }
}
