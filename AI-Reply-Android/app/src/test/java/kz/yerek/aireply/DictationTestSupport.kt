package kz.yerek.aireply

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kz.yerek.aireply.ai.AIConfiguration
import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.ai.AIReplyException
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
import kz.yerek.aireply.keyboard.voice.DictationEvent
import kz.yerek.aireply.keyboard.voice.HostDictation
import kz.yerek.aireply.keyboard.voice.MicAccess
import kz.yerek.aireply.voice.MicPermission
import kz.yerek.aireply.voice.RecordedAudio
import kz.yerek.aireply.voice.RecorderFailure
import kz.yerek.aireply.voice.TranscriptionRequest
import kz.yerek.aireply.voice.VoiceRecorder
import kz.yerek.aireply.voice.VoiceTranscriber
import java.io.File
import kotlin.math.min

/**
 * A microphone the test speaks for. It keeps the real recorder's promises:
 * time is the audio captured, nothing is captured while paused, and capture
 * stops by itself - for good - at the limit.
 */
class FakeRecorder : VoiceRecorder {
    private var listener: VoiceRecorder.Listener? = null
    private var maxMs = 0L
    private var captured = 0L

    /** The microphone is open. */
    var running = false
        private set

    var starts = 0
    var pauses = 0
    var resumes = 0
    var finishes = 0
    var discards = 0
    var startFailure: RecorderFailure? = null
    var resumeFailure: RecorderFailure? = null
    /** Holds finish() until completed: encoding in progress. */
    var encoding: CompletableDeferred<Unit>? = null
    /** finish() fails the way a full disk does. */
    var failWrite = false

    override val capturedMs: Long get() = captured

    override fun start(maxMs: Long, listener: VoiceRecorder.Listener): RecorderFailure? {
        starts++
        startFailure?.let { return it }
        this.maxMs = maxMs
        this.listener = listener
        running = true
        return null
    }

    /** The first audio arrives. */
    fun capturing() {
        check(running) { "the microphone is closed" }
        listener!!.onCapturing()
    }

    /** [ms] more of audio - all of it, or up to the limit, where capture stops by itself. Nothing while closed. */
    fun record(ms: Long, level: Float = 0.5f) {
        if (!running) return
        captured += min(ms, maxMs - captured)
        listener!!.onProgress(captured, level)
        if (captured >= maxMs) {
            running = false
            listener!!.onLimitReached()
        }
    }

    /** The microphone stops under the recording: a call, a revoked permission. */
    fun fails(reason: RecorderFailure) {
        running = false
        listener!!.onFailure(reason)
    }

    override fun pause() {
        pauses++
        running = false
    }

    override fun resume(): RecorderFailure? {
        resumes++
        resumeFailure?.let { return it }
        running = captured < maxMs
        return null
    }

    override suspend fun finish(): RecordedAudio? {
        finishes++
        running = false
        encoding?.await()
        if (failWrite) throw java.io.IOException("disk full")
        return if (captured < DictationController.MIN_RECORDING_MS) null else RecordedAudio(File("voice-test.m4a"), "audio/mp4", captured)
    }

    override fun discard() {
        discards++
        running = false
    }
}

/** The server's transcription, answered by the test. Every request is kept. */
class FakeTranscriber : VoiceTranscriber {
    override var isOffered = true
    var problem: AIReplyError? = null
    val requests = mutableListOf<TranscriptionRequest>()
    var cancelled = 0
    private var pending: CompletableDeferred<String>? = null

    override fun readiness(): AIReplyError? = problem

    override suspend fun transcribe(request: TranscriptionRequest): String {
        requests += request
        val answer = CompletableDeferred<String>()
        pending = answer
        try {
            return answer.await()
        } catch (cancellation: CancellationException) {
            cancelled++
            // The request is gone: an answer can no longer arrive.
            answer.cancel()
            throw cancellation
        }
    }

    val isWaiting: Boolean get() = pending?.isActive == true

    fun answers(text: String) {
        checkNotNull(pending) { "nothing was sent" }.complete(text)
    }

    fun fails(error: AIReplyError) {
        checkNotNull(pending) { "nothing was sent" }.completeExceptionally(AIReplyException(error))
    }
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

/** The app's text field: the words it was given, whether the keyboard is over it, and over the same app. */
class FakeHostField(var live: Boolean = true, var sameApp: Boolean = true) : HostDictation {
    val inserted = mutableListOf<String>()
    override val isLive: Boolean get() = live
    override val isSameApp: Boolean get() = sameApp
    override fun insert(text: String): Boolean {
        inserted += text
        return true
    }
}

/**
 * Both AI panels, voice input and the services behind them, the way the
 * keyboard service wires them - with fakes where the platform and the server
 * would be. Every request that would reach the network is counted.
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

    val recorders = mutableListOf<FakeRecorder>()
    /** The next microphone refuses to open, with this reason. */
    var nextStartFailure: RecorderFailure? = null
    val transcriber = FakeTranscriber()
    val mic = FakeMic()
    var now = 10_000L
    var layout = KeyboardLanguage.RUSSIAN
    var layouts: List<KeyboardLanguage> = KeyboardLanguage.CYCLE_ORDER
    val events = mutableListOf<DictationEvent>()
    val changed = mutableListOf<String>()
    private var ids = 0

    val dictation = DictationController(
        scope = scope,
        newRecorder = { FakeRecorder().also { it.startFailure = nextStartFailure; recorders += it } },
        transcriber = transcriber,
        mic = mic,
        clock = { now },
        newRequestId = { "request-${++ids}" }
    ).apply {
        onEvent = { events += it }
        onInstructionChanged = { field -> changed += field.text }
    }

    val panels = KeyboardPanels(
        replies, compose, dictation,
        layout = { layout },
        instructionLimit = { instructionLimit },
        layouts = { layouts }
    )

    /** The microphone of the recording in progress. */
    val recorder: FakeRecorder get() = recorders.last()

    val friend get() = ReplyConfiguration.INITIAL.visibleTemplates.first { it.id == "friend" }

    init {
        // The keyboard is on screen.
        dictation.shown()
    }

    fun settle() = scope.runCurrent()

    /** Lets [ms] of the test's time pass - delays included - and the clock with it. */
    fun passes(ms: Long) {
        now += ms
        scope.advanceTimeBy(ms)
        scope.runCurrent()
    }

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

    /** Mic tapped in the panel on screen, the microphone open and [ms] recorded. */
    fun startRecording(ms: Long = 2_000L) {
        panels.microphoneTapped()
        settle()
        recorder.capturing()
        recorder.record(ms)
        settle()
    }

    /** Done, and the server hearing [text]. */
    fun finishWith(text: String) {
        dictation.finish()
        settle()
        transcriber.answers(text)
        settle()
    }
}
