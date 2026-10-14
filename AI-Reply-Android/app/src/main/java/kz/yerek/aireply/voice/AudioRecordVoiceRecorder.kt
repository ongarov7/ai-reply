package kz.yerek.aireply.voice

import android.annotation.SuppressLint
import android.content.Context
import android.media.AudioFormat
import android.media.AudioManager
import android.media.AudioRecord
import android.media.AudioRecordingConfiguration
import android.media.MediaRecorder
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.os.Process
import android.os.SystemClock
import androidx.annotation.RequiresApi
import androidx.core.content.ContextCompat
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.withContext
import kz.yerek.aireply.platform.ReplyLog
import java.io.File
import java.io.FileOutputStream
import java.io.IOException

/**
 * [VoiceRecorder] on [AudioRecord]: raw PCM from the microphone into memory,
 * one M4A file at the end.
 *
 * Микрофон тек жазу кезінде ашық: кідірісте ол жабылады.
 *
 * THE MICROPHONE IS OPEN ONLY WHILE CAPTURING. Pause stops the [AudioRecord]
 * and releases it - Android's privacy indicator goes off - and resume opens a
 * fresh one in the same format. Every run appends to the same [PcmBuffer], so
 * pauses never produce a second file. The limit, a failure, a call and
 * [finish] close it the same way.
 *
 * TIME IS AUDIO. The buffer holds at most `maxMs` worth of samples at the
 * capture rate. The chunk that reaches the limit is truncated there, the
 * reader closes the microphone itself, and only then is
 * [VoiceRecorder.Listener.onLimitReached] posted: a busy main thread can delay
 * that callback, never add audio to the recording.
 *
 * NEVER ON THE MAIN THREAD. Creating an [AudioRecord] and starting it can
 * take seconds on a slow audio HAL; done on the main thread that would
 * freeze the keyboard, and Android may take the keyboard away. So [start] and
 * [resume] only check what is instant (the permission, a call) and hand the
 * opening to the run's own thread; a microphone that will not open is
 * reported through [VoiceRecorder.Listener.onFailure].
 *
 * NOTHING STALE. Every run carries a number, and stopping a run moves
 * [generation] on, so a callback from a run that was paused, failed or
 * discarded is never delivered. A run that delivers no audio within
 * [WATCHDOG_MS] of being asked for is given up on as [RecorderFailure.UNAVAILABLE].
 *
 * CALLS. A call in progress when starting or resuming refuses with
 * [RecorderFailure.INTERRUPTED]. A call starting while capturing (API 31+),
 * or Android silencing this recording for another app (API 29+), ends the run
 * the same way, keeping what was held. Reads the audio mode and this app's own
 * recordings only - no phone-state permission.
 *
 * THREADS.
 * - Main thread: every public method, every listener callback, and every field
 *   not named below.
 * - One reader thread per run: opens and starts its [AudioRecord], reads it,
 *   measures the level, appends under [lock], and releases the [AudioRecord]
 *   when the run ends.
 * - [lock] guards [pcm] and each run's `active` flag. The main thread clears
 *   the flag under it before closing the microphone, so once a run is stopped
 *   not one more of its samples is kept.
 * - [finish] encodes a copy of the samples on [Dispatchers.Default].
 *
 * Nothing heard is ever logged: sizes, durations and outcomes only.
 */
class AudioRecordVoiceRecorder(
    context: Context,
    private val directory: File = File(context.cacheDir, "voice")
) : VoiceRecorder {

    private val appContext = context.applicationContext
    private val handler = Handler(Looper.getMainLooper())
    private val audio = appContext.getSystemService(Context.AUDIO_SERVICE) as? AudioManager

    /** The one hand-off between a reader thread and the main thread. */
    private val lock = Any()

    /** Everything captured across runs; null before the microphone first opens. Guarded by [lock]. */
    private var pcm: PcmBuffer? = null

    private var listener: VoiceRecorder.Listener? = null
    private var maxMs = 0L

    /**
     * The capture rate, fixed by the first run that opens; 0 before that.
     * Resumes ask for the same rate, so the audio stays one format. Written by
     * that run's reader thread, under [lock].
     */
    @Volatile
    private var sampleRate = 0

    /** The run capturing now, if any. */
    private var current: Run? = null

    /** Moves on with every run started and every stop; a callback from any other run is dropped. */
    private var generation = 0

    private var watchdog: Runnable? = null
    private var modeListener: Any? = null
    private var recordingCallback: Any? = null

    /** Files [finish] wrote or is writing, for [discard] to delete. */
    private val written = mutableListOf<File>()

    /** Bumped by [discard], so a [finish] that was encoding meanwhile hands back nothing. */
    private var discards = 0

    override val capturedMs: Long
        get() = PcmAudio.samplesToMs(heldSamples().toLong(), sampleRate)

    override fun start(maxMs: Long, listener: VoiceRecorder.Listener): RecorderFailure? {
        require(maxMs > 0) { "maxMs must be positive" }
        // A second start begins from nothing. Files from an earlier finish stay
        // until discard: they may still be uploading.
        stopRun()
        synchronized(lock) { pcm = null }
        sampleRate = 0
        this.maxMs = maxMs
        this.listener = listener

        val failure = open(CAPTURE_RATES)
        if (failure != null) {
            // Whatever the reader caught before the refusal is not a recording.
            synchronized(lock) { pcm = null }
            sampleRate = 0
            ReplyLog.event { "voice: recorder did not start, $failure" }
        }
        return failure
    }

    override fun pause() {
        if (current == null) return
        stopRun()
        ReplyLog.event { "voice: paused with $capturedMs ms" }
        // The reader may have filled the buffer a moment before: its report of
        // the limit belonged to the run just stopped, so it is made again.
        if (synchronized(lock) { pcm?.isFull == true }) {
            val id = generation
            handler.post { if (id == generation) listener?.onLimitReached() }
        }
    }

    override fun resume(): RecorderFailure? {
        // Never started, or discarded: there is no recording to add to.
        if (listener == null) return RecorderFailure.UNAVAILABLE
        if (current != null) return null
        if (synchronized(lock) { pcm?.isFull == true }) {
            // Already at the limit: nothing more could be kept, so the
            // microphone stays closed and the limit is reported again.
            val id = generation
            handler.post { if (id == generation) listener?.onLimitReached() }
            return null
        }
        // Paused before the microphone ever opened: no format is fixed yet.
        val failure = open(if (sampleRate == 0) CAPTURE_RATES else listOf(sampleRate))
        if (failure != null) ReplyLog.event { "voice: recorder did not resume, $failure" }
        return failure
    }

    override suspend fun finish(): RecordedAudio? {
        stopRun()
        val rate = sampleRate
        val samples = synchronized(lock) { pcm?.toShortArray() } ?: return null
        val durationMs = PcmAudio.samplesToMs(samples.size.toLong(), rate)
        if (durationMs < MIN_AUDIO_MS) {
            ReplyLog.event { "voice: $durationMs ms is too short to send" }
            return null
        }

        val m4a = VoiceFiles.newFile(directory, "m4a")
        val wav = File(directory, "${m4a.nameWithoutExtension}.wav")
        written += m4a
        written += wav
        val discardsBefore = discards

        val recorded = try {
            withContext(Dispatchers.Default) { write(samples, rate, durationMs, m4a, wav) }
        } catch (e: CancellationException) {
            // Also reached when the write finished just as the caller gave up.
            m4a.delete()
            wav.delete()
            throw e
        }
        if (recorded != null && discards != discardsBefore) {
            recorded.file.delete()
            return null
        }
        return recorded
    }

    override fun discard() {
        stopRun()
        synchronized(lock) { pcm = null }
        sampleRate = 0
        listener = null
        discards++
        written.forEach { runCatching { it.delete() } }
        written.clear()
    }

    // ----------------------------------------------------------- the file

    /**
     * Off the main thread: M4A, or WAV when no AAC encoder works. Throws
     * [IOException] when neither could be written (a full disk) - never null,
     * which means "too short" - so the audio stays for another try. A
     * cancelled coroutine stops the work between blocks and leaves no
     * partial file.
     */
    private suspend fun write(
        samples: ShortArray,
        rate: Int,
        durationMs: Long,
        m4a: File,
        wav: File
    ): RecordedAudio {
        val context = currentCoroutineContext()
        val checkpoint = { context.ensureActive() }
        val startedAt = SystemClock.elapsedRealtime()
        directory.mkdirs()

        try {
            M4aEncoder.encode(samples, samples.size, rate, m4a, checkpoint)
            ReplyLog.event {
                "voice: m4a, ${m4a.length()} bytes for $durationMs ms at $rate Hz, " +
                    "encoded in ${SystemClock.elapsedRealtime() - startedAt} ms"
            }
            return RecordedAudio(m4a, VoiceFiles.MIME_M4A, durationMs)
        } catch (e: CancellationException) {
            m4a.delete()
            throw e
        } catch (e: Exception) {
            ReplyLog.warn(e) { "voice: aac encoding failed, writing wav" }
            m4a.delete()
        }

        try {
            FileOutputStream(wav).buffered().use { out ->
                WavFile.write(out, samples, samples.size, rate, checkpoint)
            }
            ReplyLog.event { "voice: wav, ${wav.length()} bytes for $durationMs ms at $rate Hz" }
            return RecordedAudio(wav, VoiceFiles.MIME_WAV, durationMs)
        } catch (e: CancellationException) {
            wav.delete()
            throw e
        } catch (e: Exception) {
            ReplyLog.warn(e) { "voice: could not write the recording" }
            wav.delete()
            throw IOException("the recording could not be written", e)
        }
    }

    // ----------------------------------------------------------- the microphone

    private fun heldSamples(): Int = synchronized(lock) { pcm?.size ?: 0 }

    /**
     * Starts a run that opens the microphone at the first of [rates] that
     * works. Null when the run is on its way; the reason when it could not
     * even be asked for. Opening itself happens on the run's thread.
     */
    private fun open(rates: List<Int>): RecorderFailure? {
        if (!MicPermission.isGranted(appContext)) return RecorderFailure.PERMISSION
        // A call holds the microphone: there would be silence, or nothing.
        if (inCallMode()) return RecorderFailure.INTERRUPTED

        val run = Run(++generation, rates)
        current = run
        val started = runCatching { run.start() }
        if (started.isFailure) {
            ReplyLog.warn(started.exceptionOrNull()) { "voice: no reader thread" }
            current = null
            generation++
            return RecorderFailure.UNAVAILABLE
        }

        val check = Runnable {
            if (run.id != generation || run.delivered) return@Runnable
            ReplyLog.event { "voice: no audio within $WATCHDOG_MS ms" }
            stopRun()
            listener?.onFailure(RecorderFailure.UNAVAILABLE)
        }
        watchdog = check
        handler.postDelayed(check, WATCHDOG_MS)
        return null
    }

    /** On the main thread, once [run]'s microphone is capturing: calls and silencing end it. */
    private fun capturing(run: Run) {
        if (run.id != generation) return
        if (watchForInterruptions(run)) interrupted(run.id, "the microphone is silenced for another app")
    }

    /**
     * A recorder in [AudioRecord.STATE_INITIALIZED] at the first of [rates]
     * that works, from the first source that works; null when none does.
     * Throws [SecurityException] when the permission went away after all.
     */
    @SuppressLint("MissingPermission") // open() checks it just before; a revoke in between throws.
    private fun createRecord(rates: List<Int>): AudioRecord? {
        for (rate in rates) {
            val minBytes = AudioRecord.getMinBufferSize(rate, CHANNEL, ENCODING)
            // ERROR or ERROR_BAD_VALUE: the hardware does not offer this rate.
            if (minBytes <= 0) continue
            val bufferBytes = maxOf(minBytes * 2, PcmAudio.msToSamples(BUFFER_MS, rate).toInt() * 2)
            for (source in SOURCES) {
                val record = try {
                    AudioRecord(source, rate, CHANNEL, ENCODING, bufferBytes)
                } catch (e: SecurityException) {
                    throw e
                } catch (e: Exception) {
                    null
                } ?: continue
                if (record.state == AudioRecord.STATE_INITIALIZED) {
                    ReplyLog.event { "voice: microphone open, source $source at $rate Hz" }
                    return record
                }
                runCatching { record.release() }
            }
        }
        return null
    }

    /**
     * Ends the current run, if any: no more of its samples are kept, the
     * microphone closes now (its reader releases it a moment later), and none
     * of its callbacks will arrive.
     */
    private fun stopRun() {
        generation++
        watchdog?.let(handler::removeCallbacks)
        watchdog = null
        stopWatching()
        val run = current ?: return
        current = null
        synchronized(lock) { run.active = false }
        run.closeMicrophone()
    }

    // ----------------------------------------------------------- calls

    private fun inCallMode(): Boolean =
        runCatching { audio?.mode?.let { it in CALL_MODES } == true }.getOrDefault(false)

    /** Watches for a call or for silencing during [run]; true when it is silenced already. */
    private fun watchForInterruptions(run: Run): Boolean {
        val manager = audio ?: return false
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) watchMode(manager, run.id)
        return Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q && watchSilencing(manager, run)
    }

    @RequiresApi(Build.VERSION_CODES.S)
    private fun watchMode(manager: AudioManager, id: Int) {
        val watcher = AudioManager.OnModeChangedListener { mode ->
            if (mode in CALL_MODES) interrupted(id, "a call started")
        }
        runCatching { manager.addOnModeChangedListener(ContextCompat.getMainExecutor(appContext), watcher) }
            .onSuccess { modeListener = watcher }
            .onFailure { ReplyLog.warn(it) { "voice: cannot watch for calls" } }
    }

    /**
     * Android 10+ silences a recording - it keeps delivering zeros - when a
     * call or a privileged app takes the microphone. Recording on would
     * capture silence and spend the user's 40 s on it.
     */
    @RequiresApi(Build.VERSION_CODES.Q)
    private fun watchSilencing(manager: AudioManager, run: Run): Boolean {
        val callback = object : AudioManager.AudioRecordingCallback() {
            override fun onRecordingConfigChanged(configs: MutableList<AudioRecordingConfiguration>?) {
                if (isSilenced(configs, run.sessionId)) interrupted(run.id, "the microphone was silenced")
            }
        }
        runCatching { manager.registerAudioRecordingCallback(callback, handler) }
            .onSuccess { recordingCallback = callback }
            .onFailure { ReplyLog.warn(it) { "voice: cannot watch the microphone" } }
        // The change for our own start may have been reported before the
        // callback was registered.
        return runCatching { isSilenced(manager.activeRecordingConfigurations, run.sessionId) }.getOrDefault(false)
    }

    @RequiresApi(Build.VERSION_CODES.Q)
    private fun isSilenced(configs: List<AudioRecordingConfiguration>?, sessionId: Int): Boolean =
        configs.orEmpty().any { it.clientAudioSessionId == sessionId && it.isClientSilenced }

    private fun stopWatching() {
        val manager = audio
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            (modeListener as? AudioManager.OnModeChangedListener)?.let { watcher ->
                runCatching { manager?.removeOnModeChangedListener(watcher) }
            }
        }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            (recordingCallback as? AudioManager.AudioRecordingCallback)?.let { callback ->
                runCatching { manager?.unregisterAudioRecordingCallback(callback) }
            }
        }
        modeListener = null
        recordingCallback = null
    }

    /** On the main thread: a call or silencing ended run [id]. What was held stays. */
    private fun interrupted(id: Int, why: String) {
        if (id != generation) return
        ReplyLog.event { "voice: $why, stopped with $capturedMs ms" }
        stopRun()
        listener?.onFailure(RecorderFailure.INTERRUPTED)
    }

    // ----------------------------------------------------------- one run

    /**
     * One stretch of capture, from start or resume to pause, the limit, a
     * failure or finish. Its reader thread is the only one that reads
     * [record], and the one that releases it.
     */
    private inner class Run(
        val id: Int,
        private val rates: List<Int>
    ) : Runnable {

        /** The microphone's session, once it is open: how Android names this recording. */
        @Volatile
        var sessionId: Int = 0
            private set

        /** Null until the reader opened it. Guarded by this run's monitor. */
        private var record: AudioRecord? = null

        /**
         * Cleared once, under [lock], by whichever thread ends the run first.
         * Volatile so the reader's loop sees it without the lock.
         */
        @Volatile
        var active = true

        /** The first audio arrived. Written by the reader, read by the watchdog. */
        @Volatile
        var delivered = false

        private val thread = Thread(this, "voice-capture")

        // Guarded by this run's monitor: the main thread may close while the
        // reader releases.
        private var stopped = false
        private var released = false

        fun start() = thread.start()

        /**
         * Stops capturing: the microphone closes now - or, still opening, it
         * is closed the moment it opens. Any thread, any number of times.
         */
        fun closeMicrophone() {
            synchronized(this) {
                if (stopped || released) return
                stopped = true
                runCatching { record?.stop() }
            }
        }

        /** Closes the microphone and frees it for good. The reader's last act. */
        fun release() {
            synchronized(this) {
                closeMicrophone()
                if (released) return
                released = true
                runCatching { record?.release() }
            }
        }

        override fun run() {
            runCatching { Process.setThreadPriority(Process.THREAD_PRIORITY_URGENT_AUDIO) }
            val record = openMicrophone() ?: return
            val rate = record.sampleRate
            val chunk = ShortArray(PcmAudio.chunkSamples(rate))
            val meter = LevelMeter()
            var lastProgressAt = 0L
            var held = 0
            var reachedLimit = false
            var failure: RecorderFailure? = null

            try {
                while (active) {
                    val n = try {
                        record.read(chunk, 0, chunk.size)
                    } catch (e: Exception) {
                        AudioRecord.ERROR
                    }
                    if (n < 0) {
                        // ERROR_INVALID_OPERATION, ERROR_BAD_VALUE, ERROR_DEAD_OBJECT,
                        // ERROR - unless the run was stopped meanwhile, which
                        // is how a closed microphone answers.
                        if (endHere()) {
                            ReplyLog.event { "voice: read failed, $n" }
                            failure = if (MicPermission.isGranted(appContext)) {
                                RecorderFailure.UNAVAILABLE
                            } else {
                                RecorderFailure.PERMISSION
                            }
                        }
                        break
                    }
                    if (n == 0) {
                        SystemClock.sleep(EMPTY_READ_BACKOFF_MS)
                        continue
                    }

                    val level = meter.update(PcmAudio.level(chunk, 0, n))
                    var kept = false
                    synchronized(lock) {
                        val buffer = pcm
                        if (active && buffer != null) {
                            buffer.append(chunk, 0, n)
                            held = buffer.size
                            kept = true
                            if (buffer.isFull) {
                                active = false
                                reachedLimit = true
                            }
                        }
                    }
                    // Stopped by the main thread: this audio came too late.
                    if (!kept) break

                    if (!delivered) {
                        delivered = true
                        post { it.onCapturing() }
                    }
                    if (reachedLimit) break

                    val now = SystemClock.elapsedRealtime()
                    if (now - lastProgressAt >= PROGRESS_INTERVAL_MS) {
                        lastProgressAt = now
                        val ms = PcmAudio.samplesToMs(held.toLong(), rate)
                        post { it.onProgress(ms, level) }
                    }
                }
            } finally {
                // The microphone is closed before anyone hears about the limit.
                release()
            }

            if (reachedLimit) {
                val ms = PcmAudio.samplesToMs(held.toLong(), rate)
                val level = meter.level
                ReplyLog.event { "voice: limit reached at $ms ms" }
                post {
                    stopRun()
                    it.onProgress(ms, level)
                    it.onLimitReached()
                }
            } else {
                val reason = failure ?: return
                post {
                    stopRun()
                    it.onFailure(reason)
                }
            }
        }

        /**
         * Opens and starts the microphone, here on the reader thread. Null when
         * it did not - the failure is reported - or when the run was stopped
         * meanwhile, in which case the microphone is closed again at once.
         */
        private fun openMicrophone(): AudioRecord? {
            val made = try {
                createRecord(rates)
            } catch (e: SecurityException) {
                refuse(RecorderFailure.PERMISSION)
                return null
            }
            if (made == null) {
                refuse(RecorderFailure.UNAVAILABLE)
                return null
            }
            synchronized(this) {
                if (stopped) {
                    // Paused or cancelled while it was opening.
                    released = true
                    runCatching { made.release() }
                    return null
                }
                record = made
            }
            val refused = try {
                made.startRecording()
                // Before Android 10, a microphone another app holds will not start.
                if (made.recordingState == AudioRecord.RECORDSTATE_RECORDING) null else RecorderFailure.INTERRUPTED
            } catch (e: SecurityException) {
                RecorderFailure.PERMISSION
            } catch (e: Exception) {
                ReplyLog.warn(e) { "voice: startRecording refused" }
                RecorderFailure.UNAVAILABLE
            }
            if (refused != null) {
                release()
                refuse(refused)
                return null
            }
            // Stopped while starting: it is not to stay open even for a moment.
            if (synchronized(this) { stopped }) {
                release()
                return null
            }
            val rate = made.sampleRate
            synchronized(lock) {
                if (pcm == null) {
                    sampleRate = rate
                    val limit = PcmAudio.msToSamples(maxMs, rate).coerceAtMost(Int.MAX_VALUE.toLong()).toInt()
                    pcm = PcmBuffer(limit, initialCapacity = rate)
                }
            }
            sessionId = made.audioSessionId
            handler.post { capturing(this) }
            return made
        }

        /** The microphone would not open: reported on the main thread, unless the run was stopped by then. */
        private fun refuse(reason: RecorderFailure) {
            synchronized(lock) { active = false }
            ReplyLog.event { "voice: recorder did not open, $reason" }
            handler.post {
                if (id != generation) return@post
                stopRun()
                listener?.onFailure(reason)
            }
        }

        /** The reader ends the run itself; false when the main thread already had. */
        private fun endHere(): Boolean = synchronized(lock) {
            if (!active) return@synchronized false
            active = false
            true
        }

        /** Delivers on the main thread, unless this run has been stopped by then. */
        private fun post(deliver: (VoiceRecorder.Listener) -> Unit) {
            handler.post {
                if (id != generation) return@post
                listener?.let(deliver)
            }
        }
    }

    private companion object {
        /** 16 kHz first: what speech recognition wants, and the smallest file. */
        val CAPTURE_RATES = listOf(16_000, 44_100, 48_000)

        /** Tuned for speech recognition first; the plain microphone where that will not open. */
        val SOURCES = listOf(MediaRecorder.AudioSource.VOICE_RECOGNITION, MediaRecorder.AudioSource.MIC)

        const val CHANNEL = AudioFormat.CHANNEL_IN_MONO
        const val ENCODING = AudioFormat.ENCODING_PCM_16BIT

        /** The AudioRecord's own buffer: how late the reader may be without losing audio. */
        const val BUFFER_MS = 500L

        const val PROGRESS_INTERVAL_MS = 50L

        /**
         * A microphone asked for that delivers nothing for this long is not
         * going to - opening included, which a slow audio HAL can drag out.
         */
        const val WATCHDOG_MS = 5_000L

        /** Less than this is a tap, not a recording: nothing is sent. */
        const val MIN_AUDIO_MS = 300L

        /** A read that returns nothing while running: wait a little instead of spinning. */
        const val EMPTY_READ_BACKOFF_MS = 5L

        /**
         * AudioManager modes of a call - RINGTONE, IN_CALL, IN_COMMUNICATION,
         * CALL_SCREENING - as literals, like AndroidSpeechRecognitionClient.
         */
        val CALL_MODES = setOf(1, 2, 3, 4)
    }
}
