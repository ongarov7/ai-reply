package kz.yerek.aireply

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.runCurrent
import kz.yerek.aireply.ai.AIConfiguration
import kz.yerek.aireply.ai.AIReplyService
import kz.yerek.aireply.ai.ComposeService
import kz.yerek.aireply.ai.ComposeTransport
import kz.yerek.aireply.ai.GeneratedReply
import kz.yerek.aireply.ai.ReplyDraftNormalizer
import kz.yerek.aireply.ai.ReplyPromptBuilder
import kz.yerek.aireply.ai.ReplyTransport
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.core.lang.KeyboardLanguage
import kz.yerek.aireply.domain.model.ReplyConfiguration
import kz.yerek.aireply.keyboard.reply.ComposeSessionController
import kz.yerek.aireply.keyboard.reply.KeyboardPanels
import kz.yerek.aireply.keyboard.reply.ReplySessionController
import kz.yerek.aireply.keyboard.voice.DictationController
import kz.yerek.aireply.keyboard.voice.MicAccess
import kz.yerek.aireply.keyboard.voice.VoiceAnnouncement
import kz.yerek.aireply.voice.MicPermission
import kz.yerek.aireply.voice.SpeechRecognitionClient
import kz.yerek.aireply.voice.VoiceFailure
import kz.yerek.aireply.voice.VoiceState

/**
 * A recogniser the test speaks for: it records what the keyboard asked of it,
 * and moves through the states the platform one does when told to.
 */
class FakeSpeechClient : SpeechRecognitionClient {
    private val flow = MutableStateFlow<VoiceState>(VoiceState.Idle)
    override val state: StateFlow<VoiceState> = flow

    val languages = mutableListOf<String>()
    var stops = 0
    var cancels = 0
    var releases = 0

    override fun start(languageTag: String) {
        if (flow.value.isActive) return
        languages += languageTag
        flow.value = VoiceState.Starting
    }

    override fun stop() {
        stops++
        if (flow.value.isActive) flow.value = VoiceState.Processing
    }

    override fun cancel() {
        cancels++
        flow.value = VoiceState.Idle
    }

    override fun reset() {
        if (flow.value is VoiceState.Done || flow.value is VoiceState.Failed) flow.value = VoiceState.Idle
    }

    override fun release() {
        releases++
        flow.value = VoiceState.Idle
    }

    // What the platform recogniser would report.
    fun ready() { flow.value = VoiceState.Listening() }
    fun hears(partial: String) { flow.value = VoiceState.Listening(partial) }
    fun finalResult(text: String) { flow.value = VoiceState.Done(text) }
    fun fails(reason: VoiceFailure) { flow.value = VoiceState.Failed(reason, languages.lastOrNull()) }
}

/** The microphone permission, answered by the test. */
class FakeMic(var granted: Boolean = true) : MicAccess {
    var requests = 0
    var settingsOpened = 0
    private var answer: CompletableDeferred<MicPermission.Outcome?>? = null

    override fun isGranted(): Boolean = granted

    override suspend fun request(): MicPermission.Outcome? {
        requests++
        val pending = CompletableDeferred<MicPermission.Outcome?>()
        answer = pending
        return pending.await()
    }

    override fun openSettings() {
        settingsOpened++
    }

    fun answers(outcome: MicPermission.Outcome) {
        if (outcome == MicPermission.Outcome.GRANTED) granted = true
        checkNotNull(answer) { "no permission dialog is up" }.complete(outcome)
        answer = null
    }
}

/**
 * Both AI panels, the microphone and the services behind them, the way the
 * keyboard service wires them - with fakes where the platform would be.
 * Every request that would reach the network is counted.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class PanelsHarness(instructionLimit: Int = 400) {
    val dispatcher = StandardTestDispatcher()
    val scope = TestScope(dispatcher)

    val composeRequests = mutableListOf<ComposeService.Request>()
    val replyRequests = mutableListOf<AIReplyService.Request>()

    private val composeService = ComposeService(
        configuration = AIConfiguration { true },
        accountTransport = { error("the account transport must not be used in tests") },
        transportOverride = { request ->
            composeRequests += request
            ComposeTransport { GeneratedReply("Готовое сообщение") }
        }
    )
    private val replyService = AIReplyService(
        configuration = AIConfiguration { true },
        nameTemplate = { template, _ -> template.id },
        accountTransport = { _, _ -> error("the account transport must not be used in tests") },
        transportOverride = { request, _ ->
            replyRequests += request
            object : ReplyTransport {
                override suspend fun generate(prompt: ReplyPromptBuilder.Prompt) = GeneratedReply("Готовый ответ")
            }
        }
    )

    val compose = ComposeSessionController(scope, composeService, ReplyDraftNormalizer()).apply {
        uiLanguage = AppLanguage.RUSSIAN
    }
    val replies = ReplySessionController(scope, replyService, ReplyDraftNormalizer()).apply {
        configuration = ReplyConfiguration.INITIAL
        uiLanguage = AppLanguage.RUSSIAN
    }

    val clients = mutableListOf<FakeSpeechClient>()
    val mic = FakeMic()
    var now = 10_000L
    var layout = KeyboardLanguage.RUSSIAN
    val announcements = mutableListOf<VoiceAnnouncement>()
    val inserted = mutableListOf<String>()

    val dictation = DictationController(
        scope = scope,
        newClient = { FakeSpeechClient().also { clients += it } },
        mic = mic,
        clock = { now }
    ).apply {
        onAnnounce = { announcements += it }
        onInserted = { field -> inserted += field.text }
    }

    val panels = KeyboardPanels(replies, compose, dictation, layout = { layout }, instructionLimit = { instructionLimit })

    /** The recogniser in use. */
    val client: FakeSpeechClient get() = clients.last()

    val friend get() = ReplyConfiguration.INITIAL.visibleTemplates.first { it.id == "friend" }

    init {
        // The keyboard is on screen.
        dictation.shown()
    }

    fun settle() = scope.runCurrent()

    /** Create open, with [text] typed in the request. */
    fun openCreate(text: String = "") {
        panels.openCreate()
        compose.session!!.instruction.set(text)
    }

    /** The reply composer open on [message], with [text] in the instruction. */
    fun openReply(message: String = "Ты завтра свободен?", text: String = "") {
        replies.open(friend, message, null, null)
        replies.session!!.instruction.set(text)
    }

    /** Mic tapped, the recogniser ready and hearing [partial]. */
    fun startListening(partial: String = "") {
        panels.microphoneTapped()
        settle()
        client.ready()
        if (partial.isNotEmpty()) client.hears(partial)
        settle()
    }

    /** Mic tapped again and the recogniser delivering [text]. */
    fun stopWith(text: String) {
        panels.microphoneTapped()
        settle()
        client.finalResult(text)
        settle()
    }
}
