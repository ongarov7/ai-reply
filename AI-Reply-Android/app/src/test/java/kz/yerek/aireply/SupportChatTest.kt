package kz.yerek.aireply

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kz.yerek.aireply.data.account.AccountService
import kz.yerek.aireply.data.account.ApiClient
import kz.yerek.aireply.data.account.ApiError
import kz.yerek.aireply.data.account.ApiException
import kz.yerek.aireply.data.account.DeviceDescriptor
import kz.yerek.aireply.data.support.SupportApi
import kz.yerek.aireply.data.support.SupportConversationDto
import kz.yerek.aireply.data.support.SupportMessageDto
import kz.yerek.aireply.data.support.SupportMessagesPage
import kz.yerek.aireply.push.AppLink
import kz.yerek.aireply.push.AppScreen
import kz.yerek.aireply.push.PushPayload
import kz.yerek.aireply.support.FakeCredentials
import kz.yerek.aireply.support.FakeResponse
import kz.yerek.aireply.support.FakeServer
import kz.yerek.aireply.support.sessionOn
import kz.yerek.aireply.ui.feature.support.ChatMessage
import kz.yerek.aireply.ui.feature.support.SendFailure
import kz.yerek.aireply.ui.feature.support.SupportChatController
import kz.yerek.aireply.ui.feature.support.formatChatTime
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import java.time.LocalDate
import java.time.ZoneId
import java.util.Locale

/**
 * «Чат с оператором» (contract §3.7): pages, sending with retries, unread,
 * and the support push.
 *
 * Қолдау чаты: беттер, жіберу, қайталау, оқылмағандар.
 */
class SupportChatTest {

    /** The server's chat, in memory: ids m1, m2, … in time order. */
    private class FakeSupport : SupportApi {
        val messages = mutableListOf<SupportMessageDto>()
        val calls = mutableListOf<String>()
        val sent = mutableListOf<Pair<String, String>>()
        /** Thrown before the message is stored. */
        var sendError: ApiError? = null
        /** Thrown after it is stored: the answer is lost on the way, as on a timeout. */
        var answerLost: ApiError? = null
        var sendGate: CompletableDeferred<Unit>? = null
        private val byClientId = mutableMapOf<String, SupportMessageDto>()
        var unread = 0
        var conversationExists = true
        var pageSize = 3

        fun operatorWrites(text: String) {
            messages += SupportMessageDto("m${messages.size + 1}", "operator", text, "2026-10-20T10:0${messages.size}:00+05:00")
            unread++
        }

        fun userWrote(text: String) {
            messages += SupportMessageDto("m${messages.size + 1}", "user", text, "2026-10-20T10:0${messages.size}:00+05:00")
        }

        override suspend fun conversation(): SupportConversationDto? {
            calls += "conversation"
            return if (conversationExists) SupportConversationDto(id = "c1", unreadCount = unread) else null
        }

        override suspend fun messages(before: String?, after: String?, limit: Int): SupportMessagesPage {
            calls += "messages before=$before after=$after"
            val index = { id: String -> messages.indexOfFirst { it.id == id } }
            return when {
                after != null -> messages.drop(index(after) + 1).let { SupportMessagesPage(it.take(pageSize), it.size > pageSize) }
                before != null -> messages.take(index(before)).let { SupportMessagesPage(it.takeLast(pageSize), it.size > pageSize) }
                else -> SupportMessagesPage(messages.takeLast(pageSize), messages.size > pageSize)
            }
        }

        override suspend fun send(text: String, clientMessageId: String): SupportMessageDto {
            calls += "send"
            sent += clientMessageId to text
            sendGate?.await()
            sendError?.let { throw ApiException(it, 429) }
            // A retry with the same client id returns the stored message.
            val stored = byClientId.getOrPut(clientMessageId) {
                SupportMessageDto("m${messages.size + 1}", "user", text, "2026-10-20T11:0${messages.size}:00+05:00").also { messages += it }
            }
            answerLost?.let { throw ApiException(it) }
            return stored
        }

        override suspend fun markRead(upToMessageId: String): Int {
            calls += "read $upToMessageId"
            unread = 0
            return unread
        }
    }

    private val server = FakeSupport()
    private var enabled = true
    private var ids = 0
    private val chat = SupportChatController(
        api = server,
        isEnabled = { enabled },
        scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined),
        clock = { 1_000L },
        newClientId = { "client-${++ids}" }
    )

    private val texts: List<String> get() = chat.state.value.messages.map { it.text }

    @Test
    fun `the latest page first, older pages above it, each message once`() = runBlocking {
        listOf("a", "b", "c", "d", "e").forEach(server::userWrote)
        chat.open()
        assertEquals(listOf("c", "d", "e"), texts)
        assertTrue(chat.state.value.hasOlder)

        chat.loadOlder()
        assertEquals(listOf("a", "b", "c", "d", "e"), texts)
        assertFalse(chat.state.value.hasOlder)
        assertTrue(server.calls.contains("messages before=m3 after=null"))

        chat.loadOlder()
        assertEquals("nothing older: not asked again", 1, server.calls.count { it.startsWith("messages before=m") })
    }

    @Test
    fun `a poll brings what is new after the newest message, and only that`() = runBlocking {
        server.userWrote("Здравствуйте")
        chat.open()
        server.operatorWrites("Добрый день! Чем помочь?")
        chat.poll()
        chat.poll()
        assertEquals(listOf("Здравствуйте", "Добрый день! Чем помочь?"), texts)
        assertEquals(listOf("messages before=null after=m1", "messages before=null after=m2"), server.calls.filter { "after=m" in it })
    }

    @Test
    fun `an empty chat polls without a cursor and shows the greeting state`() = runBlocking {
        server.conversationExists = false
        chat.open()
        assertTrue(chat.state.value.isEmpty)
        server.operatorWrites("Здравствуйте!")
        chat.poll()
        assertEquals(listOf("Здравствуйте!"), texts)
    }

    @Test
    fun `a message shows while it is sent and becomes the server's`() = runBlocking {
        chat.open()
        server.sendGate = CompletableDeferred()
        assertTrue(chat.send("  Не приходит ответ  "))

        val sending = chat.state.value.messages.single()
        assertEquals(ChatMessage.Status.SENDING, sending.status)
        assertEquals("Не приходит ответ", sending.text)
        assertEquals(listOf("client-1" to "Не приходит ответ"), server.sent)

        val asked = server.calls.size
        val polling = launch(Dispatchers.Unconfined) { chat.poll() }
        assertEquals("no page is read while ours is on its way", asked, server.calls.size)

        server.sendGate!!.complete(Unit)
        polling.join()
        val stored = chat.state.value.messages.single()
        assertEquals(ChatMessage.Status.SENT, stored.status)
        assertEquals("m1", stored.id)
        assertEquals("the poll waited and ran after it", "messages before=null after=null", server.calls.last())

        chat.poll()
        assertEquals("the poll after it does not repeat it", listOf("Не приходит ответ"), texts)
    }

    @Test
    fun `a failed message keeps its place and retries with the same client id`() = runBlocking {
        chat.open()
        server.sendError = ApiError.Offline
        chat.send("Первое")
        server.sendError = ApiError.RateLimited(60)
        chat.send("Второе")

        val (first, second) = chat.state.value.messages
        assertEquals(ChatMessage.Status.FAILED, first.status)
        assertEquals(SendFailure.NETWORK, first.failure)
        assertEquals(SendFailure.RATE_LIMITED, second.failure)

        server.sendError = null
        chat.retry("client-1")
        assertEquals(listOf("client-1", "client-2", "client-1"), server.sent.map { it.first })
        assertEquals(listOf("Первое", "Второе"), texts)
        assertEquals(ChatMessage.Status.SENT, chat.state.value.messages.first().status)
        assertEquals("ours not sent yet stay after the server's", ChatMessage.Status.FAILED, chat.state.value.messages.last().status)
    }

    @Test
    fun `an operator reply written just before ours is not skipped`() = runBlocking {
        server.userWrote("hello")
        chat.open()
        server.operatorWrites("operator answer")
        chat.send("follow up")
        assertEquals("ours waits after what the pages brought", listOf("hello", "follow up"), texts)
        assertEquals("the cursor stays on the last paged message", "m1", chat.state.value.afterId)

        chat.poll()
        assertEquals(listOf("hello", "operator answer", "follow up"), texts)
        assertTrue(server.calls.contains("messages before=null after=m1"))
        assertEquals("each message once", 3, chat.state.value.messages.map { it.key }.toSet().size)
        assertTrue("read up to the reply", server.calls.contains("read m2"))
        assertEquals(0, chat.unread.value)

        chat.poll()
        assertEquals(listOf("hello", "operator answer", "follow up"), texts)
        assertEquals("messages before=null after=m3", server.calls.last { it.startsWith("messages") })
    }

    @Test
    fun `a reply push while ours is on its way is polled after it`() = runBlocking {
        server.userWrote("hello")
        chat.open()
        server.sendGate = CompletableDeferred()
        chat.send("follow up")
        server.operatorWrites("operator answer")
        assertTrue("on screen: no notification", chat.onReplyPush())

        server.sendGate!!.complete(Unit)
        assertEquals("in the server's order", listOf("hello", "operator answer", "follow up"), texts)
        assertEquals(0, chat.unread.value)
    }

    @Test
    fun `a poll reads every page the server has after the cursor`() = runBlocking {
        server.userWrote("Здравствуйте")
        chat.open()
        (1..5).forEach { server.operatorWrites("Ответ $it") }
        chat.poll()
        assertEquals(listOf("Здравствуйте") + (1..5).map { "Ответ $it" }, texts)
        assertEquals(
            listOf("messages before=null after=m1", "messages before=null after=m4"),
            server.calls.filter { "after=m" in it }
        )
        assertTrue(server.calls.contains("read m6"))
    }

    @Test
    fun `a send stored without an answer shows once`() = runBlocking {
        chat.open()
        server.answerLost = ApiError.TimedOut
        chat.send("Не приходит ответ")
        assertEquals(SendFailure.NETWORK, chat.state.value.messages.single().failure)

        server.answerLost = null
        chat.poll()
        val shown = chat.state.value.messages.single()
        assertEquals("Не приходит ответ", shown.text)
        assertEquals(ChatMessage.Status.SENT, shown.status)
        assertEquals("m1", shown.id)
        assertEquals("asked again with the same client id", listOf("client-1", "client-1"), server.sent.map { it.first })
        assertEquals("stored once", 1, server.messages.size)

        chat.poll()
        assertEquals(listOf("Не приходит ответ"), texts)
    }

    @Test
    fun `a send stored without an answer shows once when the chat is opened again`() = runBlocking {
        server.userWrote("Здравствуйте")
        chat.open()
        chat.close()
        server.answerLost = ApiError.TimedOut
        chat.send("Ещё вопрос")
        server.answerLost = null
        server.operatorWrites("Слушаю")

        chat.open()
        assertEquals(listOf("Здравствуйте", "Ещё вопрос", "Слушаю"), texts)
        assertTrue(chat.state.value.messages.all { it.status == ChatMessage.Status.SENT })
        assertEquals(2, server.sent.size)
    }

    @Test
    fun `a failed send is not taken for an older message with the same text`() = runBlocking {
        server.userWrote("Спасибо")
        chat.open()
        server.sendError = ApiError.Offline
        chat.send("Спасибо")
        server.sendError = null
        server.operatorWrites("Пожалуйста")
        chat.poll()

        assertEquals(listOf("Спасибо", "Пожалуйста", "Спасибо"), texts)
        assertEquals(ChatMessage.Status.FAILED, chat.state.value.messages.last().status)
        assertEquals("nothing sent without Retry", 1, server.sent.size)

        chat.retry("client-1")
        chat.poll()
        assertEquals(listOf("Спасибо", "Пожалуйста", "Спасибо"), texts)
        assertTrue(chat.state.value.messages.all { it.status == ChatMessage.Status.SENT })
    }

    @Test
    fun `a rate limited send is not sent again on its own`() = runBlocking {
        chat.open()
        server.sendError = ApiError.RateLimited(60)
        chat.send("Первое")
        server.sendError = null
        server.userWrote("Первое")
        chat.poll()
        assertEquals(1, server.sent.size)
        assertEquals(SendFailure.RATE_LIMITED, chat.state.value.messages.last().failure)
    }

    @Test
    fun `the draft stays in memory and goes with the account`() {
        chat.draft = "Не приходит ответ"
        assertEquals("Не приходит ответ", chat.draft)
        chat.reset()
        assertEquals("", chat.draft)

        val screen = File("src/main/java/kz/yerek/aireply/ui/feature/support/SupportChatScreen.kt").readText()
        assertFalse("never in saved instance state", "rememberSaveable" in screen)
    }

    /** Material's Badge defaults to the error red; an unread reply is not an error. */
    @Test
    fun `unread badges are drawn in the accent colour`() {
        val call = Regex("""(?<![A-Za-z])Badge\(""")
        val drawing = File("src/main/java").walkTopDown()
            .filter { it.isFile && it.extension == "kt" && call.containsMatchIn(it.readText()) }
            .map { it.name }
            .toList()
        assertEquals("one badge for the app", listOf("Rows.kt"), drawing)
        val rows = File("src/main/java/kz/yerek/aireply/ui/common/Rows.kt")
        val badges = rows.readLines().filter { call.containsMatchIn(it) }
        assertEquals(2, badges.size)
        badges.forEach { assertTrue(it, "containerColor = container" in it) }
        assertTrue(rows.readText().contains("val container = MaterialTheme.colorScheme.primary"))
    }

    @Test
    fun `nothing empty or too long is sent`() {
        assertFalse(chat.send("   "))
        assertFalse(chat.send("я".repeat(SupportChatController.MAX_LENGTH + 1)))
        assertTrue(SupportChatController.isTooLong("я".repeat(2001)))
        assertTrue(SupportChatController.isSendable("😀".repeat(2000)))
        assertTrue(server.sent.isEmpty())
    }

    @Test
    fun `unread comes from the conversation and is read when the chat is open`() = runBlocking {
        server.operatorWrites("Ответ 1")
        server.operatorWrites("Ответ 2")
        chat.refreshUnread()
        assertEquals(2, chat.unread.value)

        chat.open()
        assertEquals(0, chat.unread.value)
        assertTrue(server.calls.contains("read m2"))

        chat.poll()
        assertEquals("marked once per message", 1, server.calls.count { it.startsWith("read") })
    }

    @Test
    fun `a closed chat reads nothing, and a reply push notifies instead`() = runBlocking {
        chat.open()
        chat.close()
        server.operatorWrites("Ответ")
        assertFalse("not on screen: the notification shows", chat.onReplyPush())
        assertEquals(1, chat.unread.value)
        assertFalse(server.calls.any { it.startsWith("read") })

        chat.open()
        server.operatorWrites("Ещё ответ")
        assertTrue("on screen: read there, no notification", chat.onReplyPush())
        assertEquals(listOf("Ответ", "Ещё ответ"), texts)
    }

    @Test
    fun `without the chat feature nothing is asked`() = runBlocking {
        enabled = false
        chat.refreshUnread()
        chat.open()
        chat.poll()
        assertFalse(chat.send("Привет"))
        assertFalse(chat.onReplyPush())
        assertTrue(server.calls.isEmpty())
    }

    @Test
    fun `signing out forgets the chat`() = runBlocking {
        server.operatorWrites("Ответ")
        chat.refreshUnread()
        chat.open()
        chat.reset()
        assertTrue(chat.state.value.messages.isEmpty())
        assertEquals(0, chat.unread.value)
    }

    @Test
    fun `a support push opens the chat`() {
        val reply = PushPayload.from(
            mapOf("nid" to "n1", "type" to "support_reply", "category" to "account", "link" to "aireply://support", "conversation_id" to "c1")
        )!!
        assertTrue(reply.isSupportReply)
        assertEquals("c1", reply.conversationId)
        assertEquals(AppLink.Screen(AppScreen.SUPPORT), reply.link)
        assertEquals("account replies use the important channel", PushPayload.CHANNEL_IMPORTANT, reply.channelId)
        assertTrue("the tap intent carries it", PushPayload.KEY_CONVERSATION_ID in PushPayload.KEYS)

        val noLink = PushPayload.from(mapOf("nid" to "n2", "conversation_id" to "c1", "link" to ""))!!
        assertEquals(AppLink.Screen(AppScreen.SUPPORT), noLink.link)
        assertNull(PushPayload.from(mapOf("nid" to "n3", "conversation_id" to "../x"))!!.conversationId)
    }

    @Test
    fun `times read as the phone's clock`() {
        val zone = ZoneId.of("Asia/Almaty")
        assertEquals("10:05", formatChatTime("2026-10-20T10:05:00+05:00", Locale.ENGLISH, zone, LocalDate.of(2026, 10, 20)))
        assertTrue(formatChatTime("2026-10-19T10:05:00+05:00", Locale.ENGLISH, zone, LocalDate.of(2026, 10, 20))!!.contains("19"))
        assertNull(formatChatTime("yesterday", Locale.ENGLISH, zone))
        assertNull(formatChatTime(null, Locale.ENGLISH, zone))
    }

    @Test
    fun `the chat's calls are the server's documented ones`() = runBlocking {
        val http = FakeServer { request ->
            when (request.path) {
                "/api/v1/support/conversation" -> FakeResponse(200, """{"conversation":null}""")
                "/api/v1/support/messages" -> if (request.method == "POST") {
                    FakeResponse(201, """{"message":{"id":"m9","sender":"user","text":"Привет","created_at":"2026-10-20T10:00:00Z","read_at":null}}""")
                } else {
                    FakeResponse(200, """{"messages":[{"id":"m8","sender":"operator","text":"Да","created_at":"2026-10-20T09:00:00Z"}],"has_more":true}""")
                }
                "/api/v1/support/read" -> FakeResponse(200, """{"unread_count":0}""")
                else -> FakeResponse(404)
            }
        }
        val service = AccountService(
            session = sessionOn(http, FakeCredentials(access = "a", refresh = "r")),
            baseUrlProvider = { "https://example.test" },
            deviceDescriptor = { DeviceDescriptor("device-1", "android", "1.0", "15", "en", "Asia/Almaty") },
            clientFactory = { baseUrl -> ApiClient(baseUrl, openConnection = http::open) }
        )

        assertNull(service.supportConversation())
        val page = service.supportMessages(before = "m10", after = null, limit = 30)
        assertTrue(page.hasMore)
        assertTrue(page.messages.single().fromOperator)
        assertEquals("before=m10&limit=30", http.requests[1].query)

        val sent = service.sendSupportMessage("Привет", "6b0b2f3e-0000-4000-8000-000000000001")
        assertEquals("m9", sent.id)
        val body = Json.parseToJsonElement(http.requests[2].body).jsonObject
        assertEquals(setOf("text", "client_message_id"), body.keys)
        assertEquals("6b0b2f3e-0000-4000-8000-000000000001", body["client_message_id"]!!.jsonPrimitive.content)

        assertEquals(0, service.markSupportRead("m8"))
        assertEquals("""{"up_to_message_id":"m8"}""", http.requests[3].body)
    }
}
