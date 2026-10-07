package kz.yerek.aireply.ui.feature.onboarding

import androidx.annotation.StringRes
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ContentCopy
import androidx.compose.material.icons.filled.Keyboard
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalAccessibilityManager
import androidx.compose.ui.res.stringResource
import kotlinx.coroutines.delay
import kz.yerek.aireply.R
import kz.yerek.aireply.domain.model.GrammaticalGender
import kz.yerek.aireply.domain.model.RelationshipKind
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.common.rememberReducedMotion
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.Radius
import kz.yerek.aireply.ui.design.Spacing
import kz.yerek.aireply.ui.design.StepRow

/** The five moments of a reply, each with the line that explains it. */
internal enum class TutorialStage(@StringRes val caption: Int) {
    COPY(R.string.onboarding_usage_step_copy),
    SWITCH(R.string.onboarding_usage_step_open),
    PERSONA(R.string.onboarding_usage_step_template),
    INSTRUCTION(R.string.onboarding_usage_step_instruction),
    INSERT(R.string.onboarding_usage_step_insert);

    /** The tutorial loops, so the last stage leads back to the first. */
    val next: TutorialStage get() = entries[(ordinal + 1) % entries.size]
}

/**
 * Copy → switch keyboard → persona → instruction → Reply → Insert, as a small
 * animation that moves on by itself and on a tap. With animations switched
 * off it is a plain numbered list instead.
 */
@Composable
internal fun ColumnScope.CopyReplyStep(gender: GrammaticalGender?) {
    StepHeader(
        stringResource(R.string.onboarding_usage_title),
        stringResource(R.string.onboarding_usage_prompt)
    )
    if (rememberReducedMotion()) {
        AppCard {
            Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
                TutorialStage.entries.forEach { stage ->
                    StepRow(stage.ordinal + 1, stringResource(stage.caption))
                }
            }
        }
    } else {
        AnimatedTutorial(gender)
    }
    Footnote(stringResource(R.string.onboarding_switch_note))
    Footnote(stringResource(R.string.onboarding_clipboard_note))
}

@Composable
private fun ColumnScope.AnimatedTutorial(gender: GrammaticalGender?) {
    var stage by rememberSaveable { mutableStateOf(TutorialStage.COPY) }
    // Longer when a screen reader is on, so the caption can be read out in full.
    val accessibility = LocalAccessibilityManager.current
    val stageMillis = accessibility?.calculateRecommendedTimeoutMillis(
        STAGE_MILLIS,
        containsIcons = true,
        containsText = true
    ) ?: STAGE_MILLIS

    LaunchedEffect(stage) {
        delay(stageMillis)
        stage = stage.next
    }

    val next = stringResource(R.string.cd_tutorial_next)
    // Fade through: the old picture is gone before the new one appears, so two
    // stages of different heights never show on top of each other.
    AnimatedContent(
        targetState = stage,
        transitionSpec = {
            fadeIn(tween(FADE_IN_MILLIS, delayMillis = FADE_OUT_MILLIS)) togetherWith
                fadeOut(tween(FADE_OUT_MILLIS))
        },
        label = "tutorial",
        modifier = Modifier
            .clip(RoundedCornerShape(Radius.large))
            .clickable(onClickLabel = next) { stage = stage.next }
    ) { shown ->
        TutorialMock(shown, gender)
    }
    StepRow(stage.ordinal + 1, stringResource(stage.caption))
}

@Composable
private fun TutorialMock(stage: TutorialStage, gender: GrammaticalGender?) {
    val incoming = stringResource(R.string.onboarding_tutorial_incoming)
    val reply = stringResource(OnboardingExamples.tutorialReply(gender))
    MockPhone {
        MockChat(
            incoming = incoming,
            field = reply.takeIf { stage == TutorialStage.INSERT },
            belowMessage = if (stage == TutorialStage.COPY) {
                { MockAction(stringResource(R.string.common_copy), icon = Icons.Filled.ContentCopy) }
            } else {
                null
            },
            fieldAction = if (stage == TutorialStage.SWITCH) {
                { MockAction(stringResource(R.string.app_name), icon = Icons.Filled.Keyboard) }
            } else {
                null
            }
        )
        when (stage) {
            TutorialStage.COPY -> MockKeyboard(showsPersonas = false)
            TutorialStage.SWITCH -> MockKeyboard(showsPersonas = true)
            TutorialStage.PERSONA -> MockKeyboard(showsPersonas = true, selected = RelationshipKind.CLIENT)
            TutorialStage.INSTRUCTION -> MockKeyboard(showsPersonas = true) {
                MockComposer(
                    source = incoming,
                    instruction = stringResource(OnboardingExamples.tutorialInstruction(gender))
                ) { MockAction(stringResource(R.string.kb_generate)) }
            }
            TutorialStage.INSERT -> MockKeyboard(showsPersonas = true) {
                MockComposer(source = incoming, draft = reply) {
                    MockAction(stringResource(R.string.kb_insert))
                }
            }
        }
    }
}

private const val STAGE_MILLIS = 3_000L
private const val FADE_OUT_MILLIS = 120
private const val FADE_IN_MILLIS = 240
