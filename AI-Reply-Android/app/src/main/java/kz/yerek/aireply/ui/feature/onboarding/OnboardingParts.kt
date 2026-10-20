package kz.yerek.aireply.ui.feature.onboarding

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.Spacing

/*
 * The onboarding's small pieces: chat bubbles drawn from the app's tokens (so
 * they follow the dark theme) with the app's strings (so they follow the
 * language), the step header and the page dots. Nothing here reads the
 * clipboard or sends anything.
 */

/** A step's title and the sentence under it. */
@Composable
internal fun StepHeader(heading: String, prompt: String) {
    Column(verticalArrangement = Arrangement.spacedBy(Spacing.xs)) {
        Text(heading, style = MaterialTheme.typography.headlineMedium, modifier = Modifier.semantics { heading() })
        Text(prompt, style = MaterialTheme.typography.bodyLarge, color = LocalExtraColors.current.textSecondary)
    }
}

/** The other person's message, on the leading side, in the page's grey. */
@Composable
internal fun IncomingBubble(text: String, modifier: Modifier = Modifier) {
    Bubble(
        text = text,
        shape = RoundedCornerShape(18.dp, 18.dp, 18.dp, 6.dp),
        color = MaterialTheme.colorScheme.background,
        textColor = MaterialTheme.colorScheme.onBackground,
        alignment = Alignment.CenterStart,
        modifier = modifier
    )
}

/** A suggested reply, on the trailing side, in the light accent fill. */
@Composable
internal fun ReplyBubble(text: String, modifier: Modifier = Modifier) {
    Bubble(
        text = text,
        shape = RoundedCornerShape(18.dp, 18.dp, 6.dp, 18.dp),
        color = MaterialTheme.colorScheme.primaryContainer,
        textColor = MaterialTheme.colorScheme.onPrimaryContainer,
        alignment = Alignment.CenterEnd,
        modifier = modifier
    )
}

/** As wide as its text, at most most of the row, like a messenger's. */
@Composable
private fun Bubble(
    text: String,
    shape: RoundedCornerShape,
    color: androidx.compose.ui.graphics.Color,
    textColor: androidx.compose.ui.graphics.Color,
    alignment: Alignment,
    modifier: Modifier
) {
    Box(modifier = modifier.fillMaxWidth(), contentAlignment = alignment) {
        Box(modifier = Modifier.fillMaxWidth(BUBBLE_WIDTH), contentAlignment = alignment) {
            Text(
                text,
                style = MaterialTheme.typography.bodyLarge,
                color = textColor,
                modifier = Modifier.wrapBubble(shape, color)
            )
        }
    }
}

private fun Modifier.wrapBubble(shape: RoundedCornerShape, color: androidx.compose.ui.graphics.Color): Modifier =
    clip(shape).background(color).padding(horizontal = Spacing.s, vertical = Spacing.xs + 2.dp)

/** A small label over a bubble: «Например», «Вариант ответа». */
@Composable
internal fun BubbleLabel(text: String, accent: Boolean = false) {
    Text(
        text,
        style = MaterialTheme.typography.labelMedium,
        color = if (accent) MaterialTheme.colorScheme.primary else LocalExtraColors.current.textSecondary
    )
}

/**
 * Where the tutorial is, as dots. Decoration for a screen reader: the title
 * already says «1 из 3».
 */
@Composable
internal fun ProgressDots(count: Int, current: Int, modifier: Modifier = Modifier) {
    Row(
        modifier = modifier.clearAndSetSemantics { },
        horizontalArrangement = Arrangement.spacedBy(Spacing.xs),
        verticalAlignment = Alignment.CenterVertically
    ) {
        repeat(count) { index ->
            Box(
                modifier = Modifier
                    .size(if (index + 1 == current) 8.dp else 6.dp)
                    .clip(CircleShape)
                    .background(
                        if (index + 1 == current) MaterialTheme.colorScheme.primary
                        else LocalExtraColors.current.textTertiary
                    )
            )
        }
    }
}

private const val BUBBLE_WIDTH = 0.86f
