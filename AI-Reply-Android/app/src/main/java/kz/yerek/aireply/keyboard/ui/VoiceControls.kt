package kz.yerek.aireply.keyboard.ui

import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.Undo
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.ErrorOutline
import androidx.compose.material.icons.filled.Mic
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kz.yerek.aireply.R
import kz.yerek.aireply.ai.AppStrings
import kz.yerek.aireply.core.text.codePointLength
import kz.yerek.aireply.keyboard.KeyboardTheme
import kz.yerek.aireply.keyboard.voice.DictationController
import kz.yerek.aireply.keyboard.voice.DictationMessage
import kz.yerek.aireply.keyboard.voice.DictationPhase
import kz.yerek.aireply.keyboard.voice.VoiceMessage
import kz.yerek.aireply.keyboard.voice.VoiceStatusText

/** The parts of a recording that change many times a second; read only where they are drawn. */
interface VoiceMeter {
    /** Audio recorded, pauses excluded. */
    val capturedMs: Long

    /** Loudness of the latest audio, 0..1, measured by the recorder. */
    val level: Float
}

/**
 * What the voice controls draw, gathered by the service. The fast-changing
 * time and loudness stay behind [meter], so a tick redraws the timer and the
 * meter - never the panel around them.
 */
class VoiceUi(
    val phase: DictationPhase,
    val message: DictationMessage?,
    val maxMs: Long,
    val limitReached: Boolean,
    val canPause: Boolean,
    val canResume: Boolean,
    val canFinish: Boolean,
    val canUndo: Boolean,
    /** The recording's controls replace the row: it is recording, paused, recognising or failed. */
    val showsControls: Boolean,
    val meter: VoiceMeter,
    /** Words for the app's field waiting for Insert. */
    val pendingText: String? = null,
    /** The microphone can be offered at all: the server transcribes (or a DEBUG mock stands in). */
    val offered: Boolean = true
) {
    val isIdle: Boolean get() = phase == DictationPhase.IDLE

    companion object {
        private val STILL = object : VoiceMeter {
            override val capturedMs: Long = 0L
            override val level: Float = 0f
        }

        val IDLE = VoiceUi(
            phase = DictationPhase.IDLE, message = null, maxMs = 40_000L, limitReached = false,
            canPause = false, canResume = false, canFinish = false, canUndo = false,
            showsControls = false, meter = STILL
        )
    }
}

class VoiceActions(
    val onPause: () -> Unit,
    val onResume: () -> Unit,
    val onFinish: () -> Unit,
    val onCancel: () -> Unit,
    val onRetry: () -> Unit,
    val onInsert: () -> Unit,
    val onUndo: () -> Unit,
    /** A recording for the app's field that could not start: try again, as 🎙 would. */
    val onRestart: () -> Unit = {}
) {
    companion object {
        val NONE = VoiceActions({}, {}, {}, {}, {}, {}, {})
    }
}

// --------------------------------------------------------------- the host bar

/**
 * Voice input for the app's field, in the persona row's place and at its
 * height - the keys never move when a recording starts or ends:
 *
 *     [✕]  ▮▮▮ Recording        00:18 / 00:40  [⏸] [✓ Done]
 *          ━━━━━━━━━━━━━━━━━━━━━━──────────────
 *
 * Recognising swaps the timer for a spinner, a failure for its sentence and
 * Try again, words waiting for the app's field for a preview and Insert. A
 * recording that could not start says why here too, for a few seconds.
 *
 * Under [COMPACT_WIDTH] (a 360dp phone) Done is a tick and the timer reads
 * "0:18/0:40", so the status always has room to be read.
 */
@Composable
fun HostVoiceBar(
    voice: VoiceUi,
    actions: VoiceActions,
    strings: AppStrings,
    theme: KeyboardTheme,
    modifier: Modifier = Modifier
) {
    BoxWithConstraints(modifier.fillMaxWidth()) {
        val compact = maxWidth < COMPACT_WIDTH
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .height(PERSONA_ROW_HEIGHT)
                .padding(horizontal = 6.dp),
            verticalAlignment = Alignment.CenterVertically
        ) {
            VoiceControlsRow(voice, actions, strings, theme, showsStatus = true, buttonSize = 34.dp, compact = compact)
        }
    }
}

// ------------------------------------------------------- the panels' controls

/**
 * The recording's controls in an AI panel, in place of the intents, the mic
 * and Reply / Write - at the row's height, so the panel does not move. The
 * voice line above says what is happening; this row carries the time.
 */
@Composable
internal fun PanelVoiceControls(
    voice: VoiceUi,
    actions: VoiceActions,
    strings: AppStrings,
    theme: KeyboardTheme,
    modifier: Modifier = Modifier
) {
    BoxWithConstraints(modifier.fillMaxWidth()) {
        val compact = maxWidth < COMPACT_WIDTH - 40.dp
        Row(
            modifier = Modifier.fillMaxWidth().height(ROW_HEIGHT),
            verticalAlignment = Alignment.CenterVertically
        ) {
            VoiceControlsRow(voice, actions, strings, theme, showsStatus = false, buttonSize = 32.dp, compact = compact)
        }
    }
}

/** Cancel, the state in the middle, and the one or two actions the state allows. */
@Composable
private fun androidx.compose.foundation.layout.RowScope.VoiceControlsRow(
    voice: VoiceUi,
    actions: VoiceActions,
    strings: AppStrings,
    theme: KeyboardTheme,
    showsStatus: Boolean,
    buttonSize: Dp,
    compact: Boolean
) {
    CircleIcon(
        icon = Icons.Filled.Close,
        description = strings[R.string.voice_rec_cancel],
        theme = theme,
        size = buttonSize,
        iconSize = 17.dp,
        onClick = actions.onCancel
    )
    Spacer(Modifier.width(8.dp))
    Box(Modifier.weight(1f).fillMaxHeight(), contentAlignment = Alignment.CenterStart) {
        when (voice.phase) {
            DictationPhase.FINALIZING, DictationPhase.TRANSCRIBING, DictationPhase.RECOVERING ->
                WorkingState(voice, strings, theme)
            DictationPhase.ERROR, DictationPhase.IDLE -> ProblemState(voice, strings, theme)
            DictationPhase.REVIEWING -> PendingState(voice, strings, theme)
            else -> RecordingState(voice, strings, theme, showsStatus, compact)
        }
    }
    Spacer(Modifier.width(8.dp))
    when (voice.phase) {
        DictationPhase.STARTING, DictationPhase.RECORDING, DictationPhase.PAUSED -> {
            val paused = voice.phase == DictationPhase.PAUSED
            CircleIcon(
                icon = if (paused) Icons.Filled.PlayArrow else Icons.Filled.Pause,
                description = strings[if (paused) R.string.voice_rec_resume else R.string.voice_rec_pause],
                theme = theme,
                size = buttonSize,
                iconSize = 19.dp,
                enabled = if (paused) voice.canResume else voice.canPause,
                onClick = if (paused) actions.onResume else actions.onPause
            )
            Spacer(Modifier.width(6.dp))
            DoneButton(voice, strings, theme, compact, actions.onFinish)
        }
        // The app's field: a recording that could not start, tried again as 🎙 would.
        DictationPhase.IDLE -> ActionPill(
            label = strings[R.string.kb_retry],
            enabled = true,
            theme = theme,
            quiet = true,
            modifier = Modifier.widthIn(min = 64.dp),
            onClick = actions.onRestart
        )
        DictationPhase.ERROR -> ActionPill(
            label = strings[R.string.kb_retry],
            enabled = true,
            theme = theme,
            modifier = Modifier.widthIn(min = 72.dp),
            onClick = actions.onRetry
        )
        DictationPhase.REVIEWING -> ActionPill(
            label = strings[R.string.kb_insert],
            enabled = voice.pendingText != null,
            theme = theme,
            modifier = Modifier.widthIn(min = 72.dp),
            onClick = actions.onInsert
        )
        // Recognising: only Cancel, on the left.
        else -> Unit
    }
}

/**
 * Done, enabled once there is something worth recognising - a stray tap
 * the moment the microphone opens is not a recording.
 */
@Composable
private fun DoneButton(voice: VoiceUi, strings: AppStrings, theme: KeyboardTheme, compact: Boolean, onClick: () -> Unit) {
    val ready = voice.canFinish && voice.meter.capturedMs >= DictationController.MIN_RECORDING_MS
    Row(
        modifier = Modifier
            .height(32.dp)
            .widthIn(min = if (compact) 44.dp else 72.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(if (ready) theme.accent else theme.accent.copy(alpha = 0.35f))
            .clickable(enabled = ready, role = Role.Button, onClick = onClick)
            .semantics { contentDescription = strings[R.string.voice_rec_done_description] }
            .padding(horizontal = 12.dp),
        horizontalArrangement = Arrangement.Center,
        verticalAlignment = Alignment.CenterVertically
    ) {
        Icon(Icons.Filled.Check, contentDescription = null, tint = Color.White, modifier = Modifier.size(if (compact) 18.dp else 15.dp))
        if (!compact) {
            Spacer(Modifier.width(4.dp))
            Text(
                text = strings[R.string.voice_rec_done],
                fontSize = 15.sp,
                fontWeight = FontWeight.SemiBold,
                color = Color.White,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis
            )
        }
    }
}

// ------------------------------------------------------------------ states

/**
 * Recording or paused: the meter, what is happening, the time and the
 * progress towards the limit. The time is announced by the service at 10 s,
 * 5 s and the limit; here it is only drawn.
 */
@Composable
private fun RecordingState(voice: VoiceUi, strings: AppStrings, theme: KeyboardTheme, showsStatus: Boolean, compact: Boolean) {
    val captured = voice.meter.capturedMs
    val left = voice.maxMs - captured
    val urgent = voice.phase == DictationPhase.RECORDING && left <= VoiceStatusText.URGENT_MS
    val counting = voice.phase == DictationPhase.RECORDING && left <= VoiceStatusText.COUNTDOWN_MS
    val timerDescription = strings.get(
        R.string.voice_rec_timer,
        VoiceStatusText.seconds(captured),
        VoiceStatusText.seconds(voice.maxMs)
    )
    Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.Center) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            LevelMeter(
                level = if (voice.phase == DictationPhase.RECORDING) voice.meter.level else 0f,
                active = voice.phase == DictationPhase.RECORDING,
                theme = theme
            )
            Spacer(Modifier.width(7.dp))
            if (showsStatus) {
                val status = VoiceStatusText.status(
                    voice.phase, captured, voice.maxMs, voice.limitReached, voice.message, forHost = true
                )
                Text(
                    text = status?.let { strings.text(it) }.orEmpty(),
                    fontSize = 12.5.sp,
                    fontWeight = if (counting) FontWeight.SemiBold else FontWeight.Medium,
                    color = when {
                        urgent -> theme.destructive
                        voice.phase == DictationPhase.PAUSED -> theme.secondaryText
                        else -> theme.primaryText
                    },
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f)
                )
            } else {
                Spacer(Modifier.weight(1f))
            }
            Text(
                text = if (compact) VoiceStatusText.shortTimer(captured, voice.maxMs) else VoiceStatusText.timer(captured, voice.maxMs),
                fontSize = 12.sp,
                fontFamily = FontFamily.Monospace,
                fontWeight = if (counting) FontWeight.SemiBold else FontWeight.Normal,
                color = if (urgent) theme.destructive else theme.secondaryText,
                maxLines = 1,
                modifier = Modifier.padding(start = 6.dp).clearAndSetSemantics { contentDescription = timerDescription }
            )
        }
        Spacer(Modifier.height(5.dp))
        RecordingProgress(
            fraction = if (voice.maxMs > 0) captured.toFloat() / voice.maxMs else 0f,
            urgent = counting,
            paused = voice.phase != DictationPhase.RECORDING,
            theme = theme
        )
    }
}

/** Recognising, or waiting for the one automatic retry. Cancel stays on the left. */
@Composable
private fun WorkingState(voice: VoiceUi, strings: AppStrings, theme: KeyboardTheme) {
    val status = VoiceStatusText.status(voice.phase, voice.meter.capturedMs, voice.maxMs, voice.limitReached, voice.message, forHost = true)
    // No live region: the service announces each step once.
    Row(verticalAlignment = Alignment.CenterVertically) {
        CircularProgressIndicator(modifier = Modifier.size(15.dp), strokeWidth = 2.dp, color = theme.accent)
        Spacer(Modifier.width(9.dp))
        Text(
            text = status?.let { strings.text(it) }.orEmpty(),
            fontSize = 13.sp,
            fontWeight = FontWeight.Medium,
            color = theme.primaryText,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis
        )
    }
}

/** A failure, in its own words; the audio waits for Try again. */
@Composable
private fun ProblemState(voice: VoiceUi, strings: AppStrings, theme: KeyboardTheme) {
    val status = VoiceStatusText.status(voice.phase, voice.meter.capturedMs, voice.maxMs, voice.limitReached, voice.message, forHost = true)
    Row(verticalAlignment = Alignment.CenterVertically) {
        Icon(Icons.Filled.ErrorOutline, contentDescription = null, tint = theme.destructive, modifier = Modifier.size(16.dp))
        Spacer(Modifier.width(7.dp))
        Text(
            text = status?.let { strings.text(it) }.orEmpty(),
            fontSize = 12.sp,
            lineHeight = 14.sp,
            fontWeight = FontWeight.Medium,
            color = theme.primaryText,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis
        )
    }
}

/** Words for the app's field, recognised while the keyboard was away: the newest of them, and Insert. */
@Composable
private fun PendingState(voice: VoiceUi, strings: AppStrings, theme: KeyboardTheme) {
    val text = voice.pendingText.orEmpty()
    val style = TextStyle(fontSize = 12.sp, lineHeight = 14.sp)
    val measurer = rememberTextMeasurer()
    Column(verticalArrangement = Arrangement.Center) {
        Text(
            text = strings[R.string.voice_rec_ready],
            fontSize = 12.sp,
            fontWeight = FontWeight.SemiBold,
            color = theme.primaryText,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis
        )
        BoxWithConstraints(Modifier.fillMaxWidth()) {
            val width = constraints.maxWidth
            val preview = remember(text, width) {
                VoiceStatusText.tail(text) { candidate ->
                    measurer.measure(candidate, style, constraints = Constraints(maxWidth = width)).lineCount <= 1
                }
            }
            Text(text = preview, style = style, color = theme.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}

// ------------------------------------------------------------------ pieces

/**
 * Five bars moving with the measured loudness - the recorder's real level,
 * so silence looks like silence. Flat and grey while paused.
 */
@Composable
internal fun LevelMeter(level: Float, active: Boolean, theme: KeyboardTheme) {
    val shown by animateFloatAsState(
        targetValue = if (active) level.coerceIn(0f, 1f) else 0f,
        animationSpec = tween(90, easing = LinearEasing),
        label = "level"
    )
    val color = if (active) theme.destructive else theme.secondaryText
    Row(
        modifier = Modifier.height(18.dp).clearAndSetSemantics { },
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(2.dp)
    ) {
        LEVEL_SHAPE.forEach { weight ->
            val fraction = (MIN_BAR + (1f - MIN_BAR) * shown * weight).coerceIn(MIN_BAR, 1f)
            Box(
                Modifier
                    .width(3.dp)
                    .height(18.dp * fraction)
                    .clip(RoundedCornerShape(1.5.dp))
                    .background(color)
            )
        }
    }
}

/** The share of the 40 seconds used, red over the last ten. */
@Composable
private fun RecordingProgress(fraction: Float, urgent: Boolean, paused: Boolean, theme: KeyboardTheme) {
    val shown by animateFloatAsState(
        targetValue = fraction.coerceIn(0f, 1f),
        animationSpec = tween(120, easing = LinearEasing),
        label = "progress"
    )
    Box(
        Modifier
            .fillMaxWidth()
            .height(3.dp)
            .clip(RoundedCornerShape(1.5.dp))
            .background(theme.divider)
            .clearAndSetSemantics { }
    ) {
        Box(
            Modifier
                .fillMaxWidth(shown)
                .fillMaxHeight()
                .clip(RoundedCornerShape(1.5.dp))
                .background(
                    when {
                        urgent -> theme.destructive
                        paused -> theme.secondaryText
                        else -> theme.accent
                    }
                )
        )
    }
}

/**
 * 🎙 on the persona row, at the trailing edge - ✨'s twin: a plain disc, the
 * same size, so the row stays balanced.
 */
@Composable
internal fun HostMicButton(strings: AppStrings, theme: KeyboardTheme, onClick: () -> Unit) {
    CircleIcon(
        icon = Icons.Filled.Mic,
        description = strings[R.string.voice_kb_dictate],
        theme = theme,
        size = kz.yerek.aireply.keyboard.layout.PersonaRowLayout.MIC_SIZE.dp,
        iconSize = 18.dp,
        onClick = onClick
    )
}

/**
 * The line under an instruction while voice has something to say: the state
 * of the recording, or what came of it, and the instruction's characters
 * against its limit - with Undo while the dictated words are fresh. Fixed at
 * [lines] lines, so nothing moves while it changes.
 */
@Composable
internal fun VoiceLine(
    voice: VoiceUi,
    instruction: String,
    limit: Int,
    actions: VoiceActions,
    strings: AppStrings,
    theme: KeyboardTheme,
    modifier: Modifier = Modifier,
    lines: Int = 2,
    /**
     * Drawn in place of "Added from voice" while the words are only being
     * reviewed: Create keeps "Reply to copied" within reach.
     */
    whileReviewing: (@Composable () -> Unit)? = null
) {
    val status = VoiceStatusText.status(
        voice.phase, voice.meter.capturedMs, voice.maxMs, voice.limitReached, voice.message, forHost = false
    ) ?: overLimitMessage(instruction, limit)
    val count = instruction.codePointLength()
    val over = count > limit
    val quietReview = voice.phase == DictationPhase.REVIEWING && voice.message == null && !over
    // At least a pill's height: Undo is never squeezed, even on one line.
    val height = voiceLineHeight(lines).coerceAtLeast(if (voice.canUndo || whileReviewing != null) 28.dp else 0.dp)
    Row(
        modifier = modifier.fillMaxWidth().height(height),
        verticalAlignment = Alignment.CenterVertically
    ) {
        if (quietReview && whileReviewing != null) {
            Box(Modifier.weight(1f), contentAlignment = Alignment.CenterStart) { whileReviewing() }
        } else {
            // No live region: the service announces the steps once each,
            // never the countdown second by second.
            Text(
                text = status?.let { strings.text(it) }.orEmpty(),
                fontSize = 11.5.sp,
                lineHeight = 14.sp,
                color = when {
                    voice.phase == DictationPhase.ERROR || (over && voice.message !is DictationMessage.Failed) -> theme.destructive
                    voice.phase == DictationPhase.RECORDING -> theme.primaryText
                    else -> theme.secondaryText
                },
                maxLines = lines,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f)
            )
        }
        if (voice.canUndo) {
            Spacer(Modifier.width(6.dp))
            AssistPill(
                text = strings[R.string.voice_rec_undo],
                description = strings[R.string.voice_rec_undo_description],
                icon = {
                    Icon(Icons.AutoMirrored.Filled.Undo, null, tint = theme.primaryText, modifier = Modifier.size(14.dp))
                },
                theme = theme,
                onClick = actions.onUndo
            )
        }
        if (showsCounter(voice, count, limit)) {
            Text(
                text = "$count / $limit",
                fontSize = 11.sp,
                fontFamily = FontFamily.Monospace,
                fontWeight = if (over || count * 10 >= limit * 9) FontWeight.SemiBold else FontWeight.Normal,
                color = if (over) theme.destructive else theme.secondaryText,
                maxLines = 1,
                modifier = Modifier
                    .padding(start = 8.dp)
                    .clearAndSetSemantics { contentDescription = strings.get(R.string.kb_character_count, count, limit) }
            )
        }
    }
}

/**
 * Whether the voice line has anything to show for [voice] and an instruction
 * of [instruction]. Reads no time: the panel around it must not redraw with
 * every tick of a recording.
 */
internal fun voiceLineShows(voice: VoiceUi, instruction: String, limit: Int): Boolean =
    !voice.isIdle || voice.message != null || overLimitMessage(instruction, limit) != null

/** The counter shows while voice is involved, and whenever the instruction is close to its limit or past it. */
private fun showsCounter(voice: VoiceUi, count: Int, limit: Int): Boolean =
    !voice.isIdle || count * 10 >= limit * 9

private fun overLimitMessage(instruction: String, limit: Int): VoiceMessage? =
    if (instruction.codePointLength() > limit) VoiceMessage.Text(R.string.kb_compose_err_too_long, limit) else null

internal fun AppStrings.text(message: VoiceMessage): String = when (message) {
    is VoiceMessage.Text -> message.argument?.let { get(message.id, it) } ?: get(message.id)
    is VoiceMessage.Error -> message(message.error)
}

/** Narrower than this, the host bar shows a tick for Done and the short timer. */
private val COMPACT_WIDTH = 400.dp

/** Silence still shows a sliver of each bar. */
private const val MIN_BAR = 0.18f

/** The bars' relative heights at full loudness: a small, symmetric peak. */
private val LEVEL_SHAPE = listOf(0.55f, 0.85f, 1f, 0.8f, 0.5f)
