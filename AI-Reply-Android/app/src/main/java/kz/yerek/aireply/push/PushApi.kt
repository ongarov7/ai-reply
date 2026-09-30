package kz.yerek.aireply.push

import kotlinx.serialization.KSerializer
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.SessionAuth
import kz.yerek.aireply.data.account.raise

/**
 * `POST /api/v1/installations`, field for field. The server refuses unknown
 * fields, so nothing is added here that it does not define. There is no user
 * id: the account comes only from the bearer token.
 */
@Serializable
data class InstallationRequest(
    @SerialName("installation_id") val installationId: String,
    val platform: String,
    @SerialName("app_version") val appVersion: String,
    @SerialName("app_build") val appBuild: String,
    @SerialName("os_name") val osName: String,
    @SerialName("os_version") val osVersion: String,
    @SerialName("device_model") val deviceModel: String,
    val manufacturer: String,
    /** The app's interface language: en, ru, kk or uz. */
    val locale: String,
    /** IANA, e.g. Asia/Almaty. */
    val timezone: String,
    /** authorized | denied | not_determined | unknown. */
    @SerialName("notification_permission") val notificationPermission: String,
    /** The in-app switch. */
    @SerialName("notifications_enabled") val notificationsEnabled: Boolean,
    /** Absent when this build or device has no token. */
    val push: PushTokenDto? = null
)

@Serializable
data class PushTokenDto(
    val provider: String,
    val token: String
)

@Serializable
data class InstallationResponse(
    @SerialName("installation_id") val installationId: String = "",
    val attached: Boolean = false,
    /** none | active | invalid | replaced. */
    @SerialName("push_status") val pushStatus: String = "none",
    @SerialName("push_available") val pushAvailable: Boolean = false,
    @SerialName("notifications_enabled") val notificationsEnabled: Boolean = true,
    /** Per-category switches; only for a signed-in registration. */
    val preferences: Map<String, Boolean>? = null
)

/** `GET · PUT /api/v1/me/notification-preferences`. */
@Serializable
data class NotificationPreferencesDto(
    val preferences: Map<String, Boolean> = emptyMap(),
    /** The categories a user may switch off; the rest (security) stay on. */
    val optional: List<String> = emptyList()
)

@Serializable
private data class NotificationPreferencesUpdate(val preferences: Map<String, Boolean>)

/** Registers this installation. Throws [kz.yerek.aireply.data.account.ApiException]. */
fun interface InstallationApi {
    /** [token] null registers anonymously, which detaches it from any account. */
    suspend fun register(request: InstallationRequest, token: String?): InstallationResponse
}

/** The signed-in user's notification categories. */
interface NotificationPreferencesApi {
    suspend fun load(): NotificationPreferencesDto
    suspend fun update(changes: Map<String, Boolean>): NotificationPreferencesDto
}

/** Both, over the app's [ApiClient]. */
class HttpPushApi(
    private val client: () -> ApiClient,
    private val session: SessionAuth
) : InstallationApi, NotificationPreferencesApi {

    override suspend fun register(request: InstallationRequest, token: String?): InstallationResponse {
        val body = json.encodeToString(InstallationRequest.serializer(), request)
        return decode(InstallationResponse.serializer(), client().request("POST", "api/v1/installations", body, token))
    }

    override suspend fun load(): NotificationPreferencesDto = session.authenticated { token ->
        decode(
            NotificationPreferencesDto.serializer(),
            client().request("GET", PREFERENCES, token = token)
        )
    }

    override suspend fun update(changes: Map<String, Boolean>): NotificationPreferencesDto {
        val body = json.encodeToString(NotificationPreferencesUpdate.serializer(), NotificationPreferencesUpdate(changes))
        return session.authenticated { token ->
            decode(NotificationPreferencesDto.serializer(), client().request("PUT", PREFERENCES, body, token))
        }
    }

    companion object {
        private const val PREFERENCES = "api/v1/me/notification-preferences"

        /** Every field goes out; a null `push` is left out, not sent as null. */
        val json = Json {
            ignoreUnknownKeys = true
            encodeDefaults = true
            explicitNulls = false
        }

        private fun <T> decode(serializer: KSerializer<T>, body: String): T =
            runCatching { json.decodeFromString(serializer, body) }
                .getOrElse { ApiError.MalformedResponse.raise() }
    }
}
