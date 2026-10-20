package kz.yerek.aireply.ui.feature.support

import kz.yerek.aireply.data.support.SupportMessageDto
import kz.yerek.aireply.data.support.SupportMessagesPage

/** One bubble: a message the server has, or one of ours on its way. */
data class ChatMessage(
    /** Stable for the list: the server id, or `local:<client id>` until it is sent. */
    val key: String,
    val id: String?,
    val clientMessageId: String?,
    val fromOperator: Boolean,
    val text: String,
    /** The server's time (RFC 3339), or null for one not sent yet. */
    val createdAt: String?,
    /** When it was written here, for a bubble not sent yet. */
    val writtenAt: Long? = null,
    val status: Status = Status.SENT,
    val failure: SendFailure? = null
) {
    enum class Status { SENT, SENDING, FAILED }

    companion object {
        fun of(message: SupportMessageDto) = ChatMessage(
            key = message.id,
            id = message.id,
            clientMessageId = null,
            fromOperator = message.fromOperator,
            text = message.text,
            createdAt = message.createdAt
        )
    }
}

/** Why a message did not go: said under it, with Retry. */
enum class SendFailure { NETWORK, RATE_LIMITED, REFUSED }

/**
 * The chat as the screen draws it, and the pure steps that change it.
 *
 * Чат күйі: беттер, жіберілуде/жіберілмеді, қайталау.
 *
 * Server messages are in the server's order, oldest first, each once
 * (by id); ours that are not sent yet follow them. A page the server sends
 * again, a poll that overlaps, or a reply that arrives while a page loads
 * never makes a bubble twice.
 */
data class SupportChatState(
    val messages: List<ChatMessage> = emptyList(),
    /** The latest page has arrived. */
    val loaded: Boolean = false,
    val loading: Boolean = false,
    /** The latest page could not be read; Retry. */
    val loadFailed: Boolean = false,
    val hasOlder: Boolean = false,
    val loadingOlder: Boolean = false
) {
    private val confirmed: List<ChatMessage> get() = messages.filter { it.id != null }
    private val local: List<ChatMessage> get() = messages.filter { it.id == null }

    /** The `before=` cursor. */
    val oldestId: String? get() = confirmed.firstOrNull()?.id
    /** The `after=` cursor. */
    val newestId: String? get() = confirmed.lastOrNull()?.id
    /** The newest message of the operator: what «read» is marked up to. */
    val lastOperatorId: String? get() = confirmed.lastOrNull { it.fromOperator }?.id

    val isEmpty: Boolean get() = loaded && messages.isEmpty()
    val isSending: Boolean get() = messages.any { it.status == ChatMessage.Status.SENDING }

    /** The latest page: what is on screen is replaced, ours on the way stay. */
    fun withLatest(page: SupportMessagesPage): SupportChatState = copy(
        messages = merge(emptyList(), page.messages) + local,
        loaded = true,
        loading = false,
        loadFailed = false,
        hasOlder = page.hasMore
    )

    /** An older page, above what is shown. */
    fun withOlder(page: SupportMessagesPage): SupportChatState {
        val known = confirmed.mapNotNull { it.id }.toSet()
        val older = page.messages.filter { it.id !in known }.map(ChatMessage::of)
        return copy(messages = older + messages, hasOlder = page.hasMore, loadingOlder = false)
    }

    /** What arrived since [newestId]. */
    fun withNewer(page: SupportMessagesPage): SupportChatState =
        copy(messages = merge(confirmed, page.messages) + local, loaded = true)

    fun sending(clientMessageId: String, text: String, now: Long): SupportChatState = copy(
        messages = messages + ChatMessage(
            key = localKey(clientMessageId),
            id = null,
            clientMessageId = clientMessageId,
            fromOperator = false,
            text = text,
            createdAt = null,
            writtenAt = now,
            status = ChatMessage.Status.SENDING
        )
    )

    /** The server stored it: the bubble becomes the server's message, in its place among them. */
    fun sent(clientMessageId: String, message: SupportMessageDto): SupportChatState {
        val others = local.filter { it.clientMessageId != clientMessageId }
        return copy(messages = merge(confirmed, listOf(message)) + others)
    }

    fun failed(clientMessageId: String, failure: SendFailure): SupportChatState = mapLocal(clientMessageId) {
        it.copy(status = ChatMessage.Status.FAILED, failure = failure)
    }

    fun retrying(clientMessageId: String): SupportChatState = mapLocal(clientMessageId) {
        it.copy(status = ChatMessage.Status.SENDING, failure = null)
    }

    private fun mapLocal(clientMessageId: String, change: (ChatMessage) -> ChatMessage) =
        copy(messages = messages.map { if (it.id == null && it.clientMessageId == clientMessageId) change(it) else it })

    companion object {
        fun localKey(clientMessageId: String) = "local:$clientMessageId"

        /** [existing] then what is new in [incoming], each id once, in the server's order. */
        private fun merge(existing: List<ChatMessage>, incoming: List<SupportMessageDto>): List<ChatMessage> {
            val known = existing.mapNotNull { it.id }.toMutableSet()
            return existing + incoming.filter { known.add(it.id) }.map(ChatMessage::of)
        }
    }
}
