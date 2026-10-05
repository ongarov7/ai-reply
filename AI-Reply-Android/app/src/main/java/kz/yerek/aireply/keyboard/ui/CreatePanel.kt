package kz.yerek.aireply.keyboard.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.Reply
import androidx.compose.material.icons.filled.AutoAwesome
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.EditNote
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kz.yerek.aireply.R
import kz.yerek.aireply.ai.AIReplyError
import kz.yerek.aireply.ai.AppStrings
import kz.yerek.aireply.keyboard.KeyboardTheme
import kz.yerek.aireply.keyboard.reply.ComposeSession
import kz.yerek.aireply.keyboard.reply.ReplyComposerFlow
import kz.yerek.aireply.keyboard.voice.DictationNotice
import kz.yerek.aireply.keyboard.voice.VoiceStatusText
import kz.yerek.aireply.voice.VoiceState

/** Everything the Create panel draws, gathered by the service. */
class CreateModel(
    val session: ComposeSession,
    val intents: List<QuickIntent>,
    /** Lines the instruction area shows before it scrolls. */
    val instructionLines: Int,
    /** The tallest the message field may be, from the screen the keyboard is on. */
    val maxFieldLines: Int,
    /** Suggestions and the polish chip, shown in the intents' place. */
    val assist: TypingAssist = TypingAssist.NONE,
    /** The microphone, shared with the reply composer. */
    val voice: VoiceState = VoiceState.Idle,
    /** The last dictation reached the request's limit. */
    val voiceNotice: DictationNotice? = null,
    /** Lines of the dictation status: fewer on a phone on its side. */
    val voiceLines: Int = 2,
    /**
     * A short, wide window (a phone on its side): the line under the request
     * moves up into the header, where there is width to spare.
     */
    val compact: Boolean = false
)

class CreateActions(
    val onClose: () -> Unit,
    /** Start over: empty instruction, no versions. */
    val onNew: () -> Unit,
    /** A tap inside a field: focus it, caret at [Int]. */
    val onFieldTap: (PanelFocus, Int) -> Unit,
    /** Write, Stop or Try again. */
    val onPrimary: () -> Unit,
    val onIntent: (QuickIntent) -> Unit,
    val onBack: () -> Unit,
    val onRegenerate: () -> Unit,
    val onEdit: () -> Unit,
    val onInsert: () -> Unit,
    val onPreviousVersion: () -> Unit,
    val onNextVersion: () -> Unit,
    val onConflict: (ReplyComposerFlow.ConflictChoice) -> Unit,
    val assist: TypingAssistActions,
    /** The microphone: dictate the request. */
    val onMic: () -> Unit = {},
    /** Answer the copied message with this request instead. */
    val onReplyToCopied: () -> Unit = {}
)

/**
 * "Create": a small writing assistant in place of the persona row. The user
 * describes the message, the server writes it, the user inserts it.
 *
 * The same panel language as the reply composer - header, one field, one row
 * of actions - with no persona and no copied message: a title and New in the
 * header, a roomier instruction area, occasion/tone intents, the same
 * microphone. Heights change on content EVENTS (a stage, a new version, an
 * error), never on a keystroke - and not when the microphone starts or stops:
 * the line under the request is always there while it is written, holding
 * "Reply to copied" or, while the microphone has something to say, the
 * dictation status.
 */
@Composable
fun CreatePanel(
    model: CreateModel,
    actions: CreateActions,
    strings: AppStrings,
    theme: KeyboardTheme,
    modifier: Modifier = Modifier
) {
    val session = model.session
    val flow = session.flow
    val stage = flow.stage
    val generating = flow.isGenerating
    val errorMessage = flow.error?.let { strings.message(it) }?.takeIf { it.isNotEmpty() }
    // "Copy a message first" is about Reply to copied, not a failed Write:
    // the button keeps saying Write.
    val writeFailed = errorMessage != null &&
        flow.error != AIReplyError.NoSourceMessage && flow.error != AIReplyError.ClipboardUnavailable

    Column(
        modifier = modifier
            .fillMaxWidth()
            .padding(horizontal = 5.dp, vertical = 3.dp)
            .clip(RoundedCornerShape(12.dp))
            .background(theme.panelBackground)
            .padding(horizontal = 8.dp, vertical = 5.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp)
    ) {
        CreateHeader(model, actions, strings, theme)

        BoxWithConstraints(Modifier.fillMaxWidth()) {
            val fieldHeight = draftFieldHeight(flow, maxWidth, model.maxFieldLines, minLines = 3)
            when {
                flow.isConflict -> Box(
                    modifier = Modifier.fillMaxWidth().height(fieldHeight).padding(horizontal = 8.dp),
                    contentAlignment = Alignment.Center
                ) {
                    Text(
                        text = strings[R.string.kb_host_field_not_empty],
                        fontSize = 14.sp,
                        fontWeight = FontWeight.Medium,
                        color = theme.primaryText,
                        textAlign = TextAlign.Center,
                        maxLines = 3
                    )
                }

                flow.showsReply -> KeyboardTextField(
                    state = session.draft,
                    placeholder = strings[R.string.kb_compose_draft_title],
                    textStyle = TextStyle(fontSize = 16.sp, lineHeight = LINE_HEIGHT_SP.sp),
                    textColor = theme.primaryText,
                    placeholderColor = theme.secondaryText,
                    caretColor = theme.accent,
                    isActive = stage == ReplyComposerFlow.Stage.Editing,
                    onTap = { offset -> actions.onFieldTap(PanelFocus.DRAFT, offset) },
                    accessibilityLabel = strings[R.string.kb_compose_draft_title],
                    modifier = Modifier
                        .fillMaxWidth()
                        .height(fieldHeight)
                        .alpha(if (generating) 0.55f else 1f)
                        .fieldFrame(theme, focused = stage == ReplyComposerFlow.Stage.Editing)
                )

                else -> KeyboardTextField(
                    state = session.instruction,
                    placeholder = strings[R.string.kb_compose_placeholder],
                    textStyle = TextStyle(fontSize = 15.5.sp, lineHeight = INSTRUCTION_LINE_SP.sp),
                    textColor = theme.primaryText,
                    placeholderColor = theme.secondaryText,
                    caretColor = theme.accent,
                    isActive = stage == ReplyComposerFlow.Stage.Composing,
                    onTap = { offset -> actions.onFieldTap(PanelFocus.INSTRUCTION, offset) },
                    accessibilityLabel = strings[R.string.kb_compose_placeholder],
                    modifier = Modifier
                        .fillMaxWidth()
                        .height((model.instructionLines * INSTRUCTION_LINE_SP + 16).dp)
                        .alpha(if (generating) 0.6f else 1f)
                        .fieldFrame(theme, focused = stage == ReplyComposerFlow.Stage.Composing)
                )
            }
        }

        if (errorMessage != null && !flow.isConflict) {
            Text(
                text = errorMessage,
                fontSize = 12.sp,
                fontWeight = FontWeight.Medium,
                color = theme.destructive,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.fillMaxWidth().padding(horizontal = 2.dp)
            )
        }

        if (!flow.showsReply && !flow.isConflict && !model.compact) CreateLine(model, actions, strings, theme)

        when {
            flow.isConflict -> ConflictRow(actions.onConflict, strings, theme)
            flow.showsReply -> ResultRow(
                flow = flow,
                draftText = session.draft.text,
                strings = strings,
                theme = theme,
                backDescription = strings[R.string.kb_compose_edit_request],
                onBack = actions.onBack,
                onRegenerate = actions.onRegenerate,
                onEdit = actions.onEdit,
                onPreviousVersion = actions.onPreviousVersion,
                onNextVersion = actions.onNextVersion,
                onInsert = actions.onInsert
            )
            else -> CreateComposingRow(model, actions, strings, theme, failed = writeFailed)
        }
    }
}

@Composable
private fun CreateHeader(model: CreateModel, actions: CreateActions, strings: AppStrings, theme: KeyboardTheme) {
    val flow = model.session.flow
    Row(
        modifier = Modifier.fillMaxWidth().height(30.dp),
        verticalAlignment = Alignment.CenterVertically
    ) {
        Icon(Icons.Filled.AutoAwesome, contentDescription = null, tint = theme.accent, modifier = Modifier.padding(start = 2.dp).size(17.dp))
        Spacer(Modifier.width(6.dp))
        val lineInHeader = model.compact && !flow.showsReply && !flow.isConflict
        Text(
            text = strings[R.string.kb_compose_title],
            fontSize = 14.sp,
            fontWeight = FontWeight.SemiBold,
            color = theme.primaryText,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = (if (lineInHeader) Modifier else Modifier.weight(1f)).semantics { heading() }
        )
        if (lineInHeader) {
            Spacer(Modifier.width(12.dp))
            Box(Modifier.weight(1f), contentAlignment = Alignment.CenterStart) {
                CreateLineContent(model, actions, strings, theme)
            }
            Spacer(Modifier.width(6.dp))
        }
        if (model.session.hasContent && !flow.isConflict) {
            Row(
                modifier = Modifier
                    .height(28.dp)
                    .clip(RoundedCornerShape(14.dp))
                    .background(theme.fieldBackground)
                    .clickable(role = Role.Button, onClick = actions.onNew)
                    .semantics { contentDescription = strings[R.string.kb_compose_new_description] }
                    .padding(start = 9.dp, end = 11.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(4.dp)
            ) {
                Icon(Icons.Filled.EditNote, contentDescription = null, tint = theme.primaryText, modifier = Modifier.size(16.dp))
                Text(
                    text = strings[R.string.kb_compose_new],
                    fontSize = 13.sp,
                    fontWeight = FontWeight.SemiBold,
                    color = theme.primaryText,
                    maxLines = 1
                )
            }
            Spacer(Modifier.width(6.dp))
        }
        CircleIcon(
            icon = Icons.Filled.Close,
            description = strings[R.string.kb_close],
            theme = theme,
            size = 28.dp,
            iconSize = 15.dp,
            onClick = actions.onClose
        )
    }
}

/**
 * The line under the request, at one height whatever it holds: the dictation
 * status while the microphone is on or has something to say (exactly as in the
 * reply composer), otherwise "Reply to copied".
 */
@Composable
private fun CreateLine(model: CreateModel, actions: CreateActions, strings: AppStrings, theme: KeyboardTheme) {
    // At least a pill's height, so swapping the status for the pill never moves anything.
    val status = voiceLineHeight(model.voiceLines)
    Box(
        modifier = Modifier.fillMaxWidth().height(if (status > 28.dp) status else 28.dp),
        contentAlignment = Alignment.CenterStart
    ) {
        CreateLineContent(model, actions, strings, theme)
    }
}

/** The dictation status when there is one, otherwise "Reply to copied". */
@Composable
private fun CreateLineContent(model: CreateModel, actions: CreateActions, strings: AppStrings, theme: KeyboardTheme) {
    if (VoiceStatusText.message(model.voice, model.voiceNotice) != null) {
        VoiceStatusLine(model.voice, model.voiceNotice, strings, theme, lines = model.voiceLines)
    } else {
        AssistPill(
            text = strings[R.string.kb_reply_to_copied],
            description = strings[R.string.kb_reply_to_copied_description],
            icon = {
                Icon(Icons.AutoMirrored.Filled.Reply, null, tint = theme.primaryText, modifier = Modifier.size(15.dp))
            },
            theme = theme,
            enabled = model.session.flow.stage == ReplyComposerFlow.Stage.Composing,
            onClick = actions.onReplyToCopied
        )
    }
}

@Composable
private fun CreateComposingRow(
    model: CreateModel,
    actions: CreateActions,
    strings: AppStrings,
    theme: KeyboardTheme,
    failed: Boolean
) {
    val flow = model.session.flow
    val generating = flow.isGenerating
    // While the microphone is on, Write waits for the words being spoken.
    val canWrite = !generating && model.session.instruction.text.isNotBlank() && !model.voice.isActive
    Row(
        modifier = Modifier.fillMaxWidth().height(ROW_HEIGHT),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp)
    ) {
        IntentSlot(
            intents = model.intents,
            enabled = flow.stage == ReplyComposerFlow.Stage.Composing,
            assist = model.assist,
            assistActions = actions.assist,
            strings = strings,
            theme = theme,
            onIntent = actions.onIntent,
            modifier = Modifier.weight(1f)
        )
        MicButton(model.voice, strings, theme, enabled = !generating, onClick = actions.onMic)
        ActionPill(
            label = when {
                generating -> strings[R.string.kb_stop]
                failed -> strings[R.string.kb_retry]
                else -> strings[R.string.kb_compose_write]
            },
            enabled = canWrite || generating,
            busy = generating,
            icon = if (generating || failed) null else Icons.Filled.AutoAwesome,
            theme = theme,
            modifier = Modifier.widthIn(min = 96.dp),
            onClick = actions.onPrimary
        )
    }
}

private const val INSTRUCTION_LINE_SP = 20
