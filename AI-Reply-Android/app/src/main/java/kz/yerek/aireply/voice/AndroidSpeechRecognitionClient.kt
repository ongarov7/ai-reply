package kz.yerek.aireply.voice

import android.content.Context
import android.content.Intent
import android.media.AudioManager
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.speech.RecognitionListener
import android.speech.RecognitionSupport
import android.speech.RecognitionSupportCallback
import android.speech.RecognizerIntent
import android.speech.SpeechRecognizer
import androidx.annotation.RequiresApi
import androidx.core.content.ContextCompat
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kz.yerek.aireply.platform.ReplyLog

/**
 * Dictation through Android's own recogniser.
 *
 * ON-DEVICE FIRST. From API 33 the platform exposes an on-device recogniser
 * directly, which is faster, works offline and never sends audio anywhere. It
 * is tried first, so a user dictating an instruction about a client's order is
 * not shipping that audio to a server whenever the device could have handled
 * it locally.
 *
 * ONE FALLBACK, FOR THE LANGUAGE. On-device models exist for few languages -
 * Kazakh is often missing. Before listening, the on-device recogniser is asked
 * whether the language is installed (API 33+); if it is not, or if it reports
 * the language unsupported once listening, the system recogniser (normally
 * Google's, over the network) is used for that one attempt. If that cannot do
 * the language either, the attempt ends as [VoiceFailure.LANGUAGE_UNAVAILABLE]
 * with the language named, and the keyboard keeps working.
 *
 * LIFECYCLE. [SpeechRecognizer] must be created, used and destroyed on the main
 * thread, and it holds the microphone until it is destroyed. Inside an input
 * method - which can stay alive for hours - leaking one would mean holding the
 * mic open across every app the user visits. [release] is therefore called from
 * `onFinishInputView`, not only from `onDestroy`.
 *
 * NO ENDLESS STATES. Every attempt carries a number; a callback for an attempt
 * that was cancelled or superseded is ignored. Listening is capped at
 * [SpeechRecognitionClient.MAX_DURATION_MS], and a recogniser that never
 * delivers its final result after Stop is given up on after
 * [FINALIZE_TIMEOUT_MS] - with what was already heard, if anything.
 */
class AndroidSpeechRecognitionClient(context: Context) : SpeechRecognitionClient {

    private val appContext = context.applicationContext
    private val handler = Handler(Looper.getMainLooper())
    private val audio = appContext.getSystemService(Context.AUDIO_SERVICE) as? AudioManager

    private val _state = MutableStateFlow<VoiceState>(VoiceState.Idle)
    override val state: StateFlow<VoiceState> = _state.asStateFlow()

    private enum class Engine { ON_DEVICE, SYSTEM }

    private var onDevice: SpeechRecognizer? = null
    private var system: SpeechRecognizer? = null

    /** The attempt being served. Bumped by every start, cancel and release. */
    private var attempt = 0
    private var languageTag = ""
    private var engine: Engine? = null
    private var partial: String = ""
    private var isStopping = false
    /** `startListening` has been called for this attempt: there may be audio. */
    private var isListening = false
    private var triedSystem = false
    private var retriedBusy = false
    private var modeListener: Any? = null

    private val cap = Runnable {
        ReplyLog.event { "voice: hit the 60s cap" }
        stop()
    }

    private val finalize = Runnable {
        if (_state.value !is VoiceState.Processing) return@Runnable
        ReplyLog.event { "voice: no final result in time" }
        val heard = partial.trim()
        abandonAttempt()
        _state.value = if (heard.isNotEmpty()) {
            VoiceState.Done(heard)
        } else {
            VoiceState.Failed(VoiceFailure.NO_SPEECH, languageTag)
        }
    }

    override fun start(languageTag: String) {
        if (_state.value.isActive) return

        if (!MicPermission.isGranted(appContext)) {
            _state.value = VoiceState.PermissionRequired
            return
        }
        // A call holds the microphone; the recogniser would only hear silence.
        if (audio?.mode?.let { it in CALL_MODES } == true) {
            _state.value = VoiceState.Failed(VoiceFailure.BUSY, languageTag)
            return
        }

        val useOnDevice = Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU && onDeviceAvailable()
        if (!useOnDevice && !systemAvailable()) {
            _state.value = VoiceState.Failed(VoiceFailure.UNAVAILABLE, languageTag)
            return
        }

        val mine = ++attempt
        this.languageTag = languageTag
        partial = ""
        isStopping = false
        isListening = false
        triedSystem = false
        retriedBusy = false
        engine = null
        _state.value = VoiceState.Starting
        watchForCalls()
        // The cap counts from the tap: a slow start eats into it, never extends it.
        handler.postDelayed(cap, SpeechRecognitionClient.MAX_DURATION_MS)

        if (useOnDevice && Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            chooseForLanguage(mine)
        } else {
            listen(mine, Engine.SYSTEM)
        }
    }

    override fun stop() {
        if (!_state.value.isActive) return
        handler.removeCallbacks(cap)
        if (!isListening) {
            // Still deciding which recogniser to use, or waiting to retry:
            // nothing has been captured, so there is nothing to deliver.
            abandonAttempt()
            _state.value = VoiceState.Idle
            return
        }
        if (isStopping) return
        isStopping = true
        _state.value = VoiceState.Processing
        runCatching { current()?.stopListening() }
        handler.removeCallbacks(finalize)
        handler.postDelayed(finalize, FINALIZE_TIMEOUT_MS)
    }

    override fun cancel() {
        abandonAttempt()
        _state.value = VoiceState.Idle
    }

    override fun reset() {
        if (_state.value is VoiceState.Done || _state.value is VoiceState.Failed) {
            _state.value = VoiceState.Idle
        }
    }

    override fun release() {
        abandonAttempt()
        runCatching { onDevice?.destroy() }
        runCatching { system?.destroy() }
        onDevice = null
        system = null
        _state.value = VoiceState.Idle
    }

    // ------------------------------------------------------------ choosing

    /**
     * Asks the on-device recogniser whether the language is installed, and
     * listens with it if so - with the system recogniser otherwise. No answer
     * in time, or a recogniser that cannot say: try on-device, and let a
     * language error fall back.
     */
    @RequiresApi(Build.VERSION_CODES.TIRAMISU)
    private fun chooseForLanguage(mine: Int) {
        val recognizer = recognizer(Engine.ON_DEVICE)
        if (recognizer == null) {
            listen(mine, Engine.SYSTEM)
            return
        }
        var decided = false
        lateinit var timeout: Runnable
        fun decide(choice: Engine) {
            if (decided || mine != attempt) return
            decided = true
            handler.removeCallbacks(timeout)
            listen(mine, choice)
        }
        timeout = Runnable { decide(Engine.ON_DEVICE) }
        handler.postDelayed(timeout, SUPPORT_CHECK_TIMEOUT_MS)

        val tag = languageTag
        runCatching {
            recognizer.checkRecognitionSupport(
                intent(tag),
                ContextCompat.getMainExecutor(appContext),
                object : RecognitionSupportCallback {
                    override fun onSupportResult(support: RecognitionSupport) {
                        val installed = support.installedOnDeviceLanguages.any { RecognitionLanguage.covers(it, tag) }
                        // Language codes only - nothing the user said.
                        ReplyLog.event {
                            "voice: on-device has the language: $installed " +
                                "(installed ${support.installedOnDeviceLanguages}, " +
                                "downloadable ${support.supportedOnDeviceLanguages}, " +
                                "online ${support.onlineLanguages})"
                        }
                        decide(if (installed || !systemAvailable()) Engine.ON_DEVICE else Engine.SYSTEM)
                    }

                    override fun onError(error: Int) {
                        ReplyLog.event { "voice: support check failed, $error" }
                        decide(Engine.ON_DEVICE)
                    }
                }
            )
        }.onFailure { decide(Engine.ON_DEVICE) }
    }

    private fun listen(mine: Int, choice: Engine) {
        if (mine != attempt) return
        val recognizer = recognizer(choice)
        if (recognizer == null) {
            finish(mine, VoiceState.Failed(VoiceFailure.UNAVAILABLE, languageTag))
            return
        }
        engine = choice
        if (choice == Engine.SYSTEM) triedSystem = true
        recognizer.setRecognitionListener(Listener(mine))
        runCatching { recognizer.startListening(intent(languageTag)) }
            .onSuccess { isListening = true }
            .onFailure {
                ReplyLog.warn(it) { "voice: startListening refused" }
                finish(mine, VoiceState.Failed(VoiceFailure.UNAVAILABLE, languageTag))
            }
    }

    private fun intent(languageTag: String) = Intent(RecognizerIntent.ACTION_RECOGNIZE_SPEECH).apply {
        putExtra(RecognizerIntent.EXTRA_LANGUAGE_MODEL, RecognizerIntent.LANGUAGE_MODEL_FREE_FORM)
        putExtra(RecognizerIntent.EXTRA_LANGUAGE, languageTag)
        putExtra(RecognizerIntent.EXTRA_LANGUAGE_PREFERENCE, languageTag)
        putExtra(RecognizerIntent.EXTRA_ONLY_RETURN_LANGUAGE_PREFERENCE, true)
        putExtra(RecognizerIntent.EXTRA_PARTIAL_RESULTS, true)
        putExtra(RecognizerIntent.EXTRA_MAX_RESULTS, 1)
        putExtra(RecognizerIntent.EXTRA_CALLING_PACKAGE, appContext.packageName)
    }

    // ---------------------------------------------------------- recognisers

    private fun onDeviceAvailable(): Boolean =
        Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            runCatching { SpeechRecognizer.isOnDeviceRecognitionAvailable(appContext) }.getOrDefault(false)

    private fun systemAvailable(): Boolean =
        runCatching { SpeechRecognizer.isRecognitionAvailable(appContext) }.getOrDefault(false)

    private fun recognizer(choice: Engine): SpeechRecognizer? = when (choice) {
        Engine.ON_DEVICE -> onDevice ?: run {
            if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return null
            runCatching { SpeechRecognizer.createOnDeviceSpeechRecognizer(appContext) }.getOrNull()
                ?.also { onDevice = it }
        }
        Engine.SYSTEM -> system ?: run {
            if (!systemAvailable()) return null
            runCatching { SpeechRecognizer.createSpeechRecognizer(appContext) }.getOrNull()
                ?.also { system = it }
        }
    }

    private fun current(): SpeechRecognizer? = when (engine) {
        Engine.ON_DEVICE -> onDevice
        Engine.SYSTEM -> system
        null -> null
    }

    /** A busy recogniser may be a stale session of our own: the next try gets a fresh one. */
    private fun discard(choice: Engine) {
        when (choice) {
            Engine.ON_DEVICE -> {
                runCatching { onDevice?.destroy() }
                onDevice = null
            }
            Engine.SYSTEM -> {
                runCatching { system?.destroy() }
                system = null
            }
        }
    }

    /** Ends the attempt without a result: later callbacks for it are ignored. */
    private fun abandonAttempt() {
        attempt++
        handler.removeCallbacks(cap)
        handler.removeCallbacks(finalize)
        runCatching { onDevice?.cancel() }
        runCatching { system?.cancel() }
        stopWatchingForCalls()
        partial = ""
        isStopping = false
        isListening = false
        engine = null
    }

    private fun finish(mine: Int, outcome: VoiceState) {
        if (mine != attempt) return
        handler.removeCallbacks(cap)
        handler.removeCallbacks(finalize)
        stopWatchingForCalls()
        partial = ""
        isStopping = false
        isListening = false
        _state.value = outcome
    }

    // ---------------------------------------------------------------- calls

    /**
     * A phone or VoIP call starting while the user dictates ends the
     * dictation the way Stop does: what was heard is kept. Reads the audio
     * mode only - no phone-state permission.
     */
    private fun watchForCalls() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.S || modeListener != null) return
        val manager = audio ?: return
        val listener = AudioManager.OnModeChangedListener { mode ->
            if (mode in CALL_MODES || mode == MODE_IN_COMMUNICATION) handler.post { callStarted() }
        }
        runCatching { manager.addOnModeChangedListener(ContextCompat.getMainExecutor(appContext), listener) }
            .onSuccess { modeListener = listener }
            .onFailure { ReplyLog.warn(it) { "voice: cannot watch for calls" } }
    }

    private fun stopWatchingForCalls() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.S) return
        val listener = modeListener as? AudioManager.OnModeChangedListener ?: return
        runCatching { audio?.removeOnModeChangedListener(listener) }
        modeListener = null
    }

    private fun callStarted() {
        if (!_state.value.isActive) return
        ReplyLog.event { "voice: a call started" }
        if (isListening) stop() else cancel()
    }

    // ------------------------------------------------------------- listener

    private inner class Listener(private val mine: Int) : RecognitionListener {

        override fun onReadyForSpeech(params: Bundle?) {
            if (mine != attempt || isStopping) return
            _state.value = VoiceState.Listening(partial)
        }

        override fun onBeginningOfSpeech() {
            if (mine != attempt || isStopping) return
            _state.value = VoiceState.Listening(partial)
        }

        override fun onRmsChanged(rmsdB: Float) = Unit

        override fun onBufferReceived(buffer: ByteArray?) = Unit

        override fun onEndOfSpeech() {
            if (mine != attempt) return
            handler.removeCallbacks(cap)
            if (_state.value.isActive && _state.value !is VoiceState.Processing) {
                _state.value = VoiceState.Processing
                handler.removeCallbacks(finalize)
                handler.postDelayed(finalize, FINALIZE_TIMEOUT_MS)
            }
        }

        override fun onPartialResults(partialResults: Bundle?) {
            if (mine != attempt) return
            val text = firstResult(partialResults) ?: return
            partial = text
            if (_state.value is VoiceState.Listening) _state.value = VoiceState.Listening(text)
        }

        override fun onResults(results: Bundle?) {
            if (mine != attempt) return
            val text = firstResult(results)?.trim().orEmpty().ifEmpty { partial.trim() }
            finish(
                mine,
                if (text.isEmpty()) VoiceState.Failed(VoiceFailure.NO_SPEECH, languageTag) else VoiceState.Done(text)
            )
        }

        override fun onEvent(eventType: Int, params: Bundle?) = Unit

        override fun onError(error: Int) {
            if (mine != attempt) return

            // Stopping normally can surface as NO_MATCH after a valid partial
            // result. Keeping what was heard is better than discarding it and
            // telling the user nothing was said when they watched it appear.
            if (isStopping && partial.isNotBlank()) {
                finish(mine, VoiceState.Done(partial.trim()))
                return
            }

            if (error in LANGUAGE_ERRORS && engine == Engine.ON_DEVICE && !triedSystem && !isStopping &&
                systemAvailable()
            ) {
                ReplyLog.event { "voice: no on-device model for the language, using the system recogniser" }
                isListening = false
                partial = ""
                listen(mine, Engine.SYSTEM)
                return
            }

            if (error == SpeechRecognizer.ERROR_RECOGNIZER_BUSY && !retriedBusy && !isStopping) {
                ReplyLog.event { "voice: recogniser busy, one retry" }
                retriedBusy = true
                isListening = false
                val choice = engine ?: Engine.SYSTEM
                discard(choice)
                handler.postDelayed({ listen(mine, choice) }, BUSY_RETRY_MS)
                return
            }

            ReplyLog.event { "voice: error $error" }
            finish(mine, outcomeFor(error))
        }

        private fun firstResult(bundle: Bundle?): String? =
            bundle?.getStringArrayList(SpeechRecognizer.RESULTS_RECOGNITION)
                ?.firstOrNull()
                ?.takeIf { it.isNotBlank() }
    }

    private fun outcomeFor(error: Int): VoiceState = when (error) {
        SpeechRecognizer.ERROR_NO_MATCH,
        SpeechRecognizer.ERROR_SPEECH_TIMEOUT -> VoiceState.Failed(VoiceFailure.NO_SPEECH, languageTag)

        SpeechRecognizer.ERROR_NETWORK,
        SpeechRecognizer.ERROR_NETWORK_TIMEOUT -> VoiceState.Failed(VoiceFailure.NETWORK, languageTag)

        SpeechRecognizer.ERROR_INSUFFICIENT_PERMISSIONS ->
            if (MicPermission.isGranted(appContext)) {
                VoiceState.Failed(VoiceFailure.GENERIC, languageTag)
            } else {
                VoiceState.PermissionRequired
            }

        // Added in API 33. Referenced as literals so this file compiles and
        // behaves identically whatever the compileSdk is, and so a pre-33
        // recogniser that happens to report them is handled too.
        ERROR_LANGUAGE_NOT_SUPPORTED,
        ERROR_LANGUAGE_UNAVAILABLE -> VoiceState.Failed(VoiceFailure.LANGUAGE_UNAVAILABLE, languageTag)

        // Another app has the microphone (a call, a recorder), or the
        // recognition service stayed busy after a fresh retry.
        SpeechRecognizer.ERROR_AUDIO,
        SpeechRecognizer.ERROR_RECOGNIZER_BUSY,
        ERROR_TOO_MANY_REQUESTS -> VoiceState.Failed(VoiceFailure.BUSY, languageTag)

        else -> VoiceState.Failed(VoiceFailure.GENERIC, languageTag)
    }

    private companion object {
        const val ERROR_TOO_MANY_REQUESTS = 10
        const val ERROR_LANGUAGE_NOT_SUPPORTED = 12
        const val ERROR_LANGUAGE_UNAVAILABLE = 13
        val LANGUAGE_ERRORS = setOf(ERROR_LANGUAGE_NOT_SUPPORTED, ERROR_LANGUAGE_UNAVAILABLE)

        /** AudioManager modes of a phone call, as literals for the same reason. */
        const val MODE_RINGTONE = 1
        const val MODE_IN_CALL = 2
        const val MODE_IN_COMMUNICATION = 3
        const val MODE_CALL_SCREENING = 4
        val CALL_MODES = setOf(MODE_RINGTONE, MODE_IN_CALL, MODE_CALL_SCREENING)

        /** How long the on-device recogniser may take to say which languages it has. */
        const val SUPPORT_CHECK_TIMEOUT_MS = 1_500L

        /** After Stop or the end of speech, how long the final result may take. */
        const val FINALIZE_TIMEOUT_MS = 6_000L

        const val BUSY_RETRY_MS = 400L
    }
}
