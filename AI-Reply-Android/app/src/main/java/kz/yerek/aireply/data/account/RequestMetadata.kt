package kz.yerek.aireply.data.account

import java.security.SecureRandom

/**
 * Which metadata headers a request may carry.
 *
 * Сұраныстың метадерегі: қосымша экрандары — толық, пернетақта — тек анонимді бөлігі.
 */
enum class HeaderScope {
    /** App screens: platform, versions, request id, and the installation and session ids. */
    APP,

    /**
     * Anything the keyboard can trigger (AI replies, token refresh, the limits
     * refresh): platform, versions and the request id only. Nothing that ties
     * typing in another app to this installation or to an app session.
     */
    KEYBOARD
}

/**
 * The headers that describe the calling app, added to every backend request in
 * one place ([ApiClient]). Metadata only: the server uses them for logs and
 * diagnostics, never for access decisions, and none of them identifies a person
 * or the hardware.
 */
fun interface RequestMetadata {

    fun headers(scope: HeaderScope): Map<String, String>

    companion object {
        /** Empty until the ServiceLocator installs the app's provider (and in unit tests). */
        val NONE = RequestMetadata { emptyMap() }

        @Volatile
        var installed: RequestMetadata = NONE

        /**
         * Told about APP-scope requests that got no HTTP answer at all. The
         * app's diagnostics report them as `api_error`; the keyboard's requests
         * never reach it.
         */
        @Volatile
        var transportFailureObserver: ((TransportFailureReport) -> Unit)? = null
    }
}

/** A request that never got an HTTP response. */
data class TransportFailureReport(
    /** The API route with ids replaced, e.g. `/api/v1/payments/{id}/confirm`. */
    val route: String,
    val failure: TransportFailure,
    val requestId: String
)

/** Why no HTTP response arrived. [code] is what diagnostics report. */
enum class TransportFailure(val code: String) {
    TIMEOUT("timeout"),
    OFFLINE("offline"),
    TLS("tls"),
    IO("io")
}

/**
 * `X-Request-ID` values: `req_` and 16 random `[a-z0-9]`, which the server keeps
 * (`^[A-Za-z0-9._:-]{8,64}$`) and echoes in its error envelope and its log.
 */
object RequestIds {

    private const val ALPHABET = "abcdefghijklmnopqrstuvwxyz0123456789"
    private const val LENGTH = 16
    private val random = SecureRandom()

    fun next(): String = buildString(4 + LENGTH) {
        append("req_")
        repeat(LENGTH) { append(ALPHABET[random.nextInt(ALPHABET.length)]) }
    }

    /** What the server accepts as a client request id. */
    fun isValid(value: String): Boolean = SERVER_PATTERN.matches(value)

    private val SERVER_PATTERN = Regex("^[A-Za-z0-9._:-]{8,64}$")
}

/**
 * An API path as a route pattern: ids become `{id}`, so a diagnostic never
 * carries a payment or installation id in its route.
 */
object ApiRoutes {

    private val WORD = Regex("^[a-z][a-z_-]*[0-9]?$")

    fun pattern(path: String): String {
        val clean = path.substringBefore('?').trim('/')
        if (clean.isEmpty()) return "/"
        return clean.split('/').joinToString(separator = "/", prefix = "/") { segment ->
            if (WORD.matches(segment) && segment.length <= 32) segment else "{id}"
        }
    }
}

/**
 * A short machine code for diagnostics: the transport cause when no answer
 * came, else [ApiError.code].
 */
fun ApiException.diagnosticCode(): String = transport?.code ?: error.code

/**
 * A stable machine code for each error, spelled out: release builds are
 * minified, so a class name would arrive as "a" or "b". A new error case does
 * not compile until it has a code here.
 */
val ApiError.code: String
    get() = when (this) {
        ApiError.Offline -> "offline"
        ApiError.TimedOut -> "timeout"
        ApiError.Cancelled -> "cancelled"
        ApiError.Unauthorized -> "unauthorized"
        ApiError.AccountDisabled -> "account_disabled"
        is ApiError.InvalidOtp -> "invalid_otp"
        ApiError.OtpExpired -> "otp_expired"
        ApiError.OtpAlreadyUsed -> "otp_already_used"
        ApiError.OtpAttemptsExceeded -> "otp_attempts_exceeded"
        is ApiError.ResendCooldown -> "otp_resend_cooldown"
        ApiError.InvalidEmail -> "invalid_email"
        ApiError.EmailDeliveryFailed -> "email_delivery_failed"
        ApiError.EmailInUse -> "email_in_use"
        ApiError.InvalidIdToken -> "invalid_id_token"
        ApiError.AuthProviderUnavailable -> "auth_provider_unavailable"
        is ApiError.RateLimited -> "rate_limited"
        is ApiError.DailyLimitReached -> "daily_limit_reached"
        ApiError.SubscriptionExpired -> "subscription_expired"
        ApiError.PaymentRequired -> "payment_required"
        ApiError.ProviderUnavailable -> "provider_unavailable"
        ApiError.ProviderTimeout -> "provider_timeout"
        ApiError.EmptyResponse -> "empty_response"
        ApiError.InvalidRequest -> "invalid_request"
        is ApiError.SourceTooLong -> "source_too_long"
        ApiError.NotFound -> "not_found"
        ApiError.Conflict -> "conflict"
        ApiError.Server -> "server"
        ApiError.MalformedResponse -> "malformed_response"
    }
