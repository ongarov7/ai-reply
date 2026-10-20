package kz.yerek.aireply.data.support

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * The support chat's wire models (contract §3.7, `features.support_chat`).
 *
 * Қолдау чаты: бір тіркелгіге бір чат, оператор аты көрсетілмейді.
 */
@Serializable
data class SupportConversationDto(
    val id: String = "",
    /** `open` or `resolved`; writing to a resolved one opens it again. */
    val status: String = "open",
    @SerialName("unread_count") val unreadCount: Int = 0,
    @SerialName("created_at") val createdAt: String = "",
    @SerialName("updated_at") val updatedAt: String = ""
)

/** `GET /api/v1/support/conversation`: null until the first message. */
@Serializable
data class SupportConversationEnvelope(val conversation: SupportConversationDto? = null)

@Serializable
data class SupportMessageDto(
    val id: String,
    /** `user` or `operator`; which operator is never said. */
    val sender: String = "",
    val text: String = "",
    /** RFC 3339; strictly increasing within the chat. */
    @SerialName("created_at") val createdAt: String = "",
    @SerialName("read_at") val readAt: String? = null
) {
    val fromOperator: Boolean get() = sender == SENDER_OPERATOR

    companion object {
        const val SENDER_OPERATOR = "operator"
    }
}

/** A page, always in ascending time; [hasMore]: more in the same direction. */
@Serializable
data class SupportMessagesPage(
    val messages: List<SupportMessageDto> = emptyList(),
    @SerialName("has_more") val hasMore: Boolean = false
)

/** `POST /api/v1/support/messages`. [clientMessageId] makes a retry return the stored message. */
@Serializable
data class SupportSendRequest(
    val text: String,
    @SerialName("client_message_id") val clientMessageId: String
)

@Serializable
data class SupportMessageEnvelope(val message: SupportMessageDto)

@Serializable
data class SupportReadRequest(@SerialName("up_to_message_id") val upToMessageId: String)

@Serializable
data class SupportReadResultDto(@SerialName("unread_count") val unreadCount: Int = 0)

/** The chat's four calls over the account's session. Throws [kz.yerek.aireply.data.account.ApiException]. */
interface SupportApi {
    suspend fun conversation(): SupportConversationDto?

    /** At most one of [before] / [after]; neither gives the latest page. */
    suspend fun messages(before: String? = null, after: String? = null, limit: Int = PAGE_SIZE): SupportMessagesPage

    suspend fun send(text: String, clientMessageId: String): SupportMessageDto

    /** Marks the operator's messages up to [upToMessageId] read; answers what is still unread. */
    suspend fun markRead(upToMessageId: String): Int

    companion object {
        const val PAGE_SIZE = 30
    }
}
