package kz.yerek.aireply.ui.feature.support

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.ApiException
import kz.yerek.aireply.data.support.SupportApi
import kz.yerek.aireply.platform.ReplyLog
import java.util.UUID

/**
 * «Чат с оператором»: the conversation, sending with retries, and the unread
 * count for the badge (contract §3.7).
 *
 * Оператормен чат: тарих, жіберу, қайталау, оқылмағандар саны.
 *
 * One per process, in the service locator, so a message on its way is not
 * lost when the screen closes and the badge is the same everywhere. Nothing
 * runs while the server has no chat ([isEnabled]). The text of a message goes
 * to the server and nowhere else: it is never logged or stored on the phone.
 *
 * WHEN IT TALKS TO THE SERVER. On every return to the app, one
 * `GET /support/conversation` for the badge. While the chat is on screen, an
 * `after=` poll every [POLL_INTERVAL_MS], driven by the screen's lifecycle;
 * a `support_reply` push polls at once instead of showing a notification.
 */
class SupportChatController(
    private val api: SupportApi,
    /** Signed in and `features.support_chat`. */
    private val isEnabled: () -> Boolean,
    private val scope: CoroutineScope,
    private val clock: () -> Long = System::currentTimeMillis,
    private val newClientId: () -> String = { UUID.randomUUID().toString() }
) {

    private val _state = MutableStateFlow(SupportChatState())
    val state: StateFlow<SupportChatState> = _state.asStateFlow()

    private val _unread = MutableStateFlow(0)
    /** Operator messages not read yet; the badge in Settings and on Home. */
    val unread: StateFlow<Int> = _unread.asStateFlow()

    /** The chat is on screen (started): replies are read as they come, and their push is not shown. */
    @Volatile
    var isVisible: Boolean = false
        private set

    /** One load or poll at a time, so pages never interleave. */
    private val loading = Mutex()

    @Volatile
    private var markedReadUpTo: String? = null

    /** Back in the app: the badge. */
    fun refreshUnread() {
        if (!isEnabled()) return
        scope.launch {
            runCatching { api.conversation() }
                .onFailure { if (it is CancellationException) throw it }
                .onSuccess { conversation -> _unread.value = conversation?.unreadCount ?: 0 }
        }
    }

    /** The screen started showing the chat: the latest page, read. */
    suspend fun open() {
        isVisible = true
        if (!isEnabled()) return
        loading.withLock {
            _state.update { it.copy(loading = true, loadFailed = false) }
            try {
                val page = api.messages()
                _state.update { it.withLatest(page) }
            } catch (cancelled: CancellationException) {
                _state.update { it.copy(loading = false) }
                throw cancelled
            } catch (failure: ApiException) {
                _state.update { it.copy(loading = false, loadFailed = !it.loaded) }
                return
            }
        }
        markRead()
    }

    /** The screen stopped (left, or the app went to the background): no more polling, pushes notify again. */
    fun close() {
        isVisible = false
    }

    /** What arrived since the newest message shown. Skipped while one of ours is on its way. */
    suspend fun poll() {
        if (!isEnabled() || _state.value.isSending) return
        if (!_state.value.loaded) return open()
        loading.withLock {
            val after = _state.value.newestId
            val page = try {
                api.messages(after = after)
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (failure: ApiException) {
                // A missed poll is caught up by the next one.
                return
            }
            _state.update { if (after == null) it.withLatest(page) else it.withNewer(page) }
        }
        markRead()
    }

    /** The top of the list came into view: the page before. */
    suspend fun loadOlder() {
        val current = _state.value
        val before = current.oldestId ?: return
        if (!current.hasOlder || current.loadingOlder || !isEnabled()) return
        loading.withLock {
            _state.update { it.copy(loadingOlder = true) }
            try {
                val page = api.messages(before = before)
                _state.update { it.withOlder(page) }
            } catch (cancelled: CancellationException) {
                _state.update { it.copy(loadingOlder = false) }
                throw cancelled
            } catch (failure: ApiException) {
                _state.update { it.copy(loadingOlder = false) }
            }
        }
    }

    /** «Отправить». False when there is nothing to send or it is too long (the field says so). */
    fun send(text: String): Boolean {
        val message = text.trim()
        if (!isSendable(message) || !isEnabled()) return false
        val clientId = newClientId()
        _state.update { it.sending(clientId, message, clock()) }
        scope.launch { deliver(clientId, message) }
        return true
    }

    /** Retry under a bubble that did not go: the same client id, so the server stores it once. */
    fun retry(clientMessageId: String) {
        val message = _state.value.messages.firstOrNull {
            it.id == null && it.clientMessageId == clientMessageId && it.status == ChatMessage.Status.FAILED
        } ?: return
        _state.update { it.retrying(clientMessageId) }
        scope.launch { deliver(clientMessageId, message.text) }
    }

    /**
     * A `support_reply` push. True when the chat is on screen: it is read
     * there, and no notification is shown.
     */
    fun onReplyPush(): Boolean {
        if (!isEnabled()) return false
        if (isVisible) {
            scope.launch { poll() }
            return true
        }
        refreshUnread()
        return false
    }

    /** Signed out: nothing of this account's chat stays. */
    fun reset() {
        _state.value = SupportChatState()
        _unread.value = 0
        markedReadUpTo = null
    }

    private suspend fun deliver(clientId: String, text: String) {
        try {
            val stored = api.send(text, clientId)
            _state.update { it.sent(clientId, stored) }
        } catch (cancelled: CancellationException) {
            _state.update { it.failed(clientId, SendFailure.NETWORK) }
            throw cancelled
        } catch (failure: ApiException) {
            ReplyLog.warn(failure) { "support message not sent (HTTP ${failure.httpStatus ?: "none"})" }
            val reason = when (failure.error) {
                is ApiError.RateLimited -> SendFailure.RATE_LIMITED
                ApiError.InvalidRequest -> SendFailure.REFUSED
                else -> SendFailure.NETWORK
            }
            _state.update { it.failed(clientId, reason) }
        }
    }

    /** Read up to the operator's newest message, once per message, while the chat is on screen. */
    private suspend fun markRead() {
        if (!isVisible) return
        val upTo = _state.value.lastOperatorId ?: return
        if (upTo == markedReadUpTo) return
        try {
            _unread.value = api.markRead(upTo)
            markedReadUpTo = upTo
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (failure: ApiException) {
            // Tried again with the next poll.
        }
    }

    companion object {
        /** While the chat is on screen. */
        const val POLL_INTERVAL_MS = 5_000L

        /** The server's limit, in characters. */
        const val MAX_LENGTH = 2000

        fun isSendable(text: String): Boolean {
            val trimmed = text.trim()
            return trimmed.isNotEmpty() && trimmed.codePointCount(0, trimmed.length) <= MAX_LENGTH
        }

        fun isTooLong(text: String): Boolean = text.trim().let { it.codePointCount(0, it.length) > MAX_LENGTH }
    }
}
