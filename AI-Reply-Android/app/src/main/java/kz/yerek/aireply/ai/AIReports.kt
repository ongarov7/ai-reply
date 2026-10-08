package kz.yerek.aireply.ai

import androidx.annotation.StringRes
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kz.yerek.aireply.R
import kz.yerek.aireply.core.text.clampToCodePoints
import kz.yerek.aireply.data.account.AIReportRequest

/** Which AI flow wrote the text a report is about. */
enum class AIReportMode(val raw: String) {
    REPLY("reply"),
    COMPOSE("compose")
}

/** Why the user reports it: the server's codes, in display order. */
enum class AIReportReason(val raw: String, @StringRes val label: Int) {
    OFFENSIVE("offensive", R.string.report_reason_offensive),
    HARMFUL("harmful", R.string.report_reason_harmful),
    FALSE_INFO("false_info", R.string.report_reason_false_info),
    WRONG_LANGUAGE("wrong_language", R.string.report_reason_wrong_language),
    OTHER("other", R.string.report_reason_other)
}

/** Where a report goes: `POST /api/v1/ai/reports`, or a test double. */
fun interface AIReportSender {
    suspend fun send(request: AIReportRequest)
}

object AIReports {
    /** The server's limits, in characters. */
    const val MAX_TEXT = 2000
    const val MAX_COMMENT = 500

    /**
     * The body of one report. The generated [text] travels only with
     * [includeText]; nothing else about the conversation is ever sent.
     */
    fun request(
        mode: AIReportMode,
        reason: AIReportReason,
        text: String,
        includeText: Boolean,
        appVersion: String,
        comment: String = ""
    ): AIReportRequest = AIReportRequest(
        mode = mode.raw,
        reason = reason.raw,
        comment = comment.trim().clampToCodePoints(MAX_COMMENT).ifEmpty { null },
        text = text.trim().clampToCodePoints(MAX_TEXT).takeIf { includeText && it.isNotEmpty() },
        platform = "android",
        appVersion = appVersion
    )
}

/**
 * One report being written about the text on screen: the reason, whether the
 * text goes with it (on until the user says otherwise), and how sending went.
 */
class AIReportDraft(val mode: AIReportMode, val text: String) {

    enum class Status { EDITING, SENDING, SENT, FAILED }

    var reason: AIReportReason? by mutableStateOf(null)
    var includeText: Boolean by mutableStateOf(true)
    var status: Status by mutableStateOf(Status.EDITING)
        internal set

    val canSend: Boolean get() = reason != null && status != Status.SENDING && status != Status.SENT
}

/**
 * Reporting AI output, the same in the keyboard's inline panel and in the
 * app's dialog: pick a reason, keep or drop the text, Send.
 *
 * Шағым: себеп, мәтінді қосу/қоспау, жіберу. Ештеңе өздігінен жіберілмейді.
 */
class AIReportController(
    private val scope: CoroutineScope,
    private val sender: AIReportSender,
    private val appVersion: String,
    /** How long the thanks shows before the report closes by itself; 0 keeps it. */
    private val thanksMs: Long = THANKS_MS
) {
    var draft: AIReportDraft? by mutableStateOf(null)
        private set

    val isOpen: Boolean get() = draft != null

    /** Report was tapped under [text]. */
    fun open(mode: AIReportMode, text: String) {
        draft = AIReportDraft(mode, text)
    }

    /** Cancel, or the text it was about went away. A report already sent still arrives. */
    fun close() {
        draft = null
    }

    fun send() {
        val current = draft ?: return
        val reason = current.reason ?: return
        if (!current.canSend) return
        current.status = AIReportDraft.Status.SENDING
        val request = AIReports.request(current.mode, reason, current.text, current.includeText, appVersion)
        scope.launch {
            val sent = try {
                sender.send(request)
                true
            } catch (cancellation: CancellationException) {
                throw cancellation
            } catch (failure: Throwable) {
                false
            }
            current.status = if (sent) AIReportDraft.Status.SENT else AIReportDraft.Status.FAILED
            if (sent && thanksMs > 0) {
                delay(thanksMs)
                if (draft === current) draft = null
            }
        }
    }

    companion object {
        const val THANKS_MS = 1_800L
    }
}
