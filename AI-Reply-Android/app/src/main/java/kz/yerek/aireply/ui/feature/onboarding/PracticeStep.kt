package kz.yerek.aireply.ui.feature.onboarding

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.ContentCopy
import androidx.compose.material.icons.filled.Keyboard
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import kz.yerek.aireply.R
import kz.yerek.aireply.analytics.ProductEvent
import kz.yerek.aireply.analytics.ProductEvents
import kz.yerek.aireply.domain.model.GrammaticalGender
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.Spacing

/** Where the practice run is. Each stage has exactly one thing to tap. */
internal enum class PracticeStage {
    MESSAGE, COPIED, KEYBOARD, COMPOSER, WRITING, DRAFT, INSERTED
}

/**
 * A full reply, practised on this screen: copy, open the keyboard, pick the
 * persona, Reply, Insert. Entirely local — no request, no quota, no clipboard
 * — and the reply is the example for the app's language and the user's gender.
 */
@Composable
internal fun ColumnScope.PracticeStep(gender: GrammaticalGender?) {
    var stage by rememberSaveable { mutableStateOf(PracticeStage.MESSAGE) }

    LaunchedEffect(stage) {
        if (stage == PracticeStage.WRITING) {
            delay(WRITING_MILLIS)
            stage = PracticeStage.DRAFT
        }
    }

    StepHeader(
        stringResource(R.string.onboarding_practice_title),
        stringResource(R.string.onboarding_practice_prompt)
    )
    if (stage == PracticeStage.INSERTED) ReadyCard() else PracticeHint(stage)
    PracticeMock(stage, gender) { next ->
        if (next == PracticeStage.INSERTED) ProductEvents.track(ProductEvent.ONBOARDING_PRACTICE_COMPLETED)
        stage = next
    }
}

@Composable
private fun PracticeHint(stage: PracticeStage) {
    val hint = when (stage) {
        PracticeStage.MESSAGE -> stringResource(
            R.string.onboarding_practice_hint_copy,
            stringResource(R.string.common_copy)
        )
        PracticeStage.COPIED -> stringResource(R.string.onboarding_practice_hint_open)
        PracticeStage.KEYBOARD -> stringResource(
            R.string.onboarding_practice_hint_persona,
            stringResource(R.string.relationship_friend)
        )
        PracticeStage.COMPOSER -> stringResource(
            R.string.onboarding_practice_hint_reply,
            stringResource(R.string.kb_generate)
        )
        PracticeStage.WRITING -> stringResource(R.string.kb_generating)
        PracticeStage.DRAFT, PracticeStage.INSERTED -> stringResource(
            R.string.onboarding_practice_hint_insert,
            stringResource(R.string.kb_insert)
        )
    }
    // Read out on every change, so a screen-reader user hears what to do next.
    Text(
        hint,
        style = MaterialTheme.typography.bodyLarge,
        modifier = Modifier.semantics { liveRegion = LiveRegionMode.Polite }
    )
}

@Composable
private fun ReadyCard() {
    AppCard(modifier = Modifier.semantics(mergeDescendants = true) { liveRegion = LiveRegionMode.Polite }) {
        Row(
            horizontalArrangement = Arrangement.spacedBy(Spacing.s),
            verticalAlignment = Alignment.Top
        ) {
            Icon(
                Icons.Filled.CheckCircle,
                contentDescription = null,
                tint = LocalExtraColors.current.success,
                modifier = Modifier.size(24.dp)
            )
            Column(verticalArrangement = Arrangement.spacedBy(Spacing.xxs)) {
                Text(stringResource(R.string.onboarding_done_title), style = MaterialTheme.typography.titleLarge)
                Footnote(stringResource(R.string.onboarding_done_body))
            }
        }
    }
}

@Composable
private fun PracticeMock(stage: PracticeStage, gender: GrammaticalGender?, onStage: (PracticeStage) -> Unit) {
    val incoming = stringResource(R.string.onboarding_practice_incoming)
    val reply = stringResource(OnboardingExamples.practiceReply(gender))
    MockPhone {
        MockChat(
            incoming = incoming,
            field = reply.takeIf { stage == PracticeStage.INSERTED },
            belowMessage = when (stage) {
                PracticeStage.MESSAGE -> {
                    {
                        MockAction(
                            stringResource(R.string.common_copy),
                            icon = Icons.Filled.ContentCopy,
                            onClick = { onStage(PracticeStage.COPIED) }
                        )
                    }
                }
                PracticeStage.COPIED -> {
                    { CopiedNote() }
                }
                else -> null
            },
            fieldAction = if (stage == PracticeStage.COPIED) {
                {
                    MockAction(
                        stringResource(R.string.onboarding_practice_open),
                        icon = Icons.Filled.Keyboard,
                        onClick = { onStage(PracticeStage.KEYBOARD) }
                    )
                }
            } else {
                null
            }
        )
        when (stage) {
            PracticeStage.MESSAGE, PracticeStage.COPIED -> Unit
            // Any persona opens the panel, as on the real keyboard; the hint suggests Friend.
            PracticeStage.KEYBOARD -> MockKeyboard(
                showsPersonas = true,
                onPersona = { onStage(PracticeStage.COMPOSER) }
            )
            PracticeStage.COMPOSER -> MockKeyboard(showsPersonas = true) {
                MockComposer(source = incoming) {
                    MockAction(stringResource(R.string.kb_generate), onClick = { onStage(PracticeStage.WRITING) })
                }
            }
            PracticeStage.WRITING -> MockKeyboard(showsPersonas = true) {
                MockComposer(source = incoming) { WritingIndicator() }
            }
            PracticeStage.DRAFT -> MockKeyboard(showsPersonas = true) {
                MockComposer(source = incoming, draft = reply) {
                    MockAction(stringResource(R.string.kb_insert), onClick = { onStage(PracticeStage.INSERTED) })
                }
            }
            PracticeStage.INSERTED -> MockKeyboard(showsPersonas = true)
        }
    }
}

@Composable
private fun CopiedNote() {
    Row(
        horizontalArrangement = Arrangement.spacedBy(Spacing.xxs),
        verticalAlignment = Alignment.CenterVertically
    ) {
        Icon(
            Icons.Filled.CheckCircle,
            contentDescription = null,
            tint = LocalExtraColors.current.success,
            modifier = Modifier.size(16.dp)
        )
        Text(
            stringResource(R.string.compose_copied),
            style = MaterialTheme.typography.labelMedium,
            color = LocalExtraColors.current.textSecondary
        )
    }
}

@Composable
private fun WritingIndicator() {
    Row(
        horizontalArrangement = Arrangement.spacedBy(Spacing.xs),
        verticalAlignment = Alignment.CenterVertically
    ) {
        CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp)
        Text(
            stringResource(R.string.kb_generating),
            style = MaterialTheme.typography.labelLarge,
            color = LocalExtraColors.current.textSecondary
        )
    }
}

/** Long enough to read as "writing", short enough not to feel like waiting. */
private const val WRITING_MILLIS = 1_200L
