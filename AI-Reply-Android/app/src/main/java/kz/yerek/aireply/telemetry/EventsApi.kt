package kz.yerek.aireply.telemetry

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.raise
import java.time.Instant

/** One `POST /api/v1/events` request: events of one session, oldest first. */
data class EventBatch(
    val installationId: String,
    val sessionId: String?,
    val events: List<TelemetryEvent>
) {
    /** The exact request body. */
    fun toJson(): String = buildJsonObject {
        put("installation_id", installationId)
        sessionId?.let { put("session_id", it) }
        put("events", buildJsonArray {
            events.forEach { event ->
                add(buildJsonObject {
                    put("id", event.id)
                    put("name", event.name)
                    put("occurred_at", Instant.ofEpochMilli(event.occurredAtMillis).toString())
                    put("properties", buildJsonObject {
                        event.properties.forEach { (key, value) ->
                            when (value) {
                                is Boolean -> put(key, JsonPrimitive(value))
                                is Long -> put(key, JsonPrimitive(value))
                                is String -> put(key, JsonPrimitive(value))
                            }
                        }
                    })
                })
            }
        })
    }.toString()
}

/** The server's answer: `202 {accepted, rejected, rejections?, disabled?}`. */
@Serializable
data class EventsResult(
    val accepted: Int = 0,
    val rejected: Int = 0,
    /** The server switched app events off: stop sending until the next launch. */
    val disabled: Boolean = false
)

/** Where batches go. Throws [kz.yerek.aireply.data.account.ApiException]. */
fun interface EventsApi {
    suspend fun send(batch: EventBatch, token: String?): EventsResult
}

/** The real endpoint. */
class HttpEventsApi(private val client: () -> ApiClient) : EventsApi {

    override suspend fun send(batch: EventBatch, token: String?): EventsResult {
        val body = client().request("POST", ROUTE, batch.toJson(), token)
        return runCatching { json.decodeFromString(EventsResult.serializer(), body) }
            .getOrElse { ApiError.MalformedResponse.raise() }
    }

    companion object {
        const val ROUTE = "api/v1/events"
        private val json = Json { ignoreUnknownKeys = true }
    }
}
