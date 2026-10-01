package kz.yerek.aireply.telemetry

/**
 * One app event, as queued for `POST /api/v1/events`.
 *
 * Property values are only ever [Boolean], [Long] or a short [String] code or
 * id. There is no field for free text: a message, a reply, a keystroke or the
 * clipboard cannot be expressed as an event at all.
 */
data class TelemetryEvent(
    /** A UUID, so a batch retried after a lost answer is not counted twice. */
    val id: String,
    val name: String,
    val occurredAtMillis: Long,
    val properties: Map<String, Any>,
    /** The foreground session the event belongs to, if any. */
    val sessionId: String?,
    /**
     * The account it happened under (a user id, or EventReporter.ANONYMOUS).
     * Never sent: it decides whether the batch may carry the bearer token.
     */
    val accountKey: String = EventReporter.ANONYMOUS
)

/**
 * The events this app sends and their properties.
 *
 * Оқиғалар тізімі: сервердің тізімінің (internal/telemetry/events.go) шағын бөлігі ғана.
 *
 * A subset of the server's allowlist, on purpose: app opens and backgrounds,
 * the push permission and token, notification taps, sign-out, a Google sign-in
 * that failed inside Google's own SDK (the server never saw it), and requests
 * that got no HTTP answer at all (the server cannot log what never arrived).
 * Everything the server already records itself — sign-ins, payments, API
 * errors with a status — is left to the server. The keyboard sends nothing.
 *
 * Anything not described here is refused before it is queued.
 */
object EventSchema {

    enum class Kind { BOOL, INT, CODE, ID, ROUTE, ENUM }

    class Property(val kind: Kind, val values: Set<String> = emptySet())

    private val method = Property(Kind.ENUM, setOf("email", "google", "apple"))
    private val provider = Property(Kind.ENUM, setOf("fcm", "apns"))
    private val code = Property(Kind.CODE)
    private val id = Property(Kind.ID)

    const val APP_OPENED = "app_opened"
    const val APP_BACKGROUNDED = "app_backgrounded"
    const val LOGIN_FAILED = "login_failed"
    const val LOGOUT = "logout"
    const val PUSH_PERMISSION_REQUESTED = "push_permission_requested"
    const val PUSH_PERMISSION_GRANTED = "push_permission_granted"
    const val PUSH_PERMISSION_DENIED = "push_permission_denied"
    const val PUSH_TOKEN_REGISTERED = "push_token_registered"
    const val PUSH_TOKEN_REFRESHED = "push_token_refreshed"
    const val PUSH_TOKEN_REGISTRATION_FAILED = "push_token_registration_failed"
    const val NOTIFICATION_OPENED = "notification_opened"
    const val API_ERROR = "api_error"

    val events: Map<String, Map<String, Property>> = mapOf(
        APP_OPENED to mapOf("cold_start" to Property(Kind.BOOL)),
        APP_BACKGROUNDED to mapOf("foreground_seconds" to Property(Kind.INT)),
        LOGIN_FAILED to mapOf("method" to method, "error_code" to code),
        LOGOUT to emptyMap(),
        PUSH_PERMISSION_REQUESTED to emptyMap(),
        PUSH_PERMISSION_GRANTED to mapOf(
            "status" to Property(Kind.ENUM, setOf("authorized", "provisional", "ephemeral"))
        ),
        PUSH_PERMISSION_DENIED to emptyMap(),
        PUSH_TOKEN_REGISTERED to mapOf("provider" to provider),
        PUSH_TOKEN_REFRESHED to mapOf("provider" to provider),
        PUSH_TOKEN_REGISTRATION_FAILED to mapOf(
            "provider" to provider, "error_code" to code, "request_id" to id
        ),
        NOTIFICATION_OPENED to mapOf("notification_id" to id, "delivery_id" to id, "type" to code),
        API_ERROR to mapOf(
            "route" to Property(Kind.ROUTE), "status" to Property(Kind.INT),
            "error_code" to code, "request_id" to id
        )
    )

    const val MAX_PROPERTIES = 8

    private val CODE_PATTERN = Regex("^[A-Za-z0-9_.:-]{1,64}$")
    private val ROUTE_PATTERN = Regex("^[A-Za-z0-9_/{}.:-]{1,128}$")

    /** Whether the server would take [value] as a code or an id. */
    fun isCode(value: String?): Boolean = value != null && CODE_PATTERN.matches(value)

    /** True when [name] and every property match the schema. */
    fun isValid(name: String, properties: Map<String, Any>): Boolean {
        val schema = events[name] ?: return false
        if (properties.size > MAX_PROPERTIES) return false
        return properties.all { (key, value) ->
            val spec = schema[key] ?: return@all false
            when (spec.kind) {
                Kind.BOOL -> value is Boolean
                Kind.INT -> value is Long && value in 0..1_000_000_000L
                Kind.CODE, Kind.ID -> value is String && CODE_PATTERN.matches(value)
                Kind.ROUTE -> value is String && ROUTE_PATTERN.matches(value)
                Kind.ENUM -> value is String && value in spec.values
            }
        }
    }
}
