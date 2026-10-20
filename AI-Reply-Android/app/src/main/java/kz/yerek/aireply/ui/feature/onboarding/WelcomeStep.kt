package kz.yerek.aireply.ui.feature.onboarding

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Group
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import kz.yerek.aireply.R
import kz.yerek.aireply.domain.model.GrammaticalGender
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.Spacing

/**
 * What AI Reply is, in one screen: the promise, one exchange as an example,
 * and the three moves - copy, choose, get the reply. The reply speaks with
 * the gender the user already chose, if any.
 */
@Composable
internal fun ColumnScope.WelcomeStep(gender: GrammaticalGender?) {
    // A narrow phone gets a step smaller title, so the example and the three moves stay in view.
    val compact = LocalConfiguration.current.screenWidthDp < COMPACT_WIDTH_DP
    Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
        Text(
            stringResource(R.string.onboarding_welcome_headline),
            style = if (compact) MaterialTheme.typography.headlineLarge else MaterialTheme.typography.displaySmall,
            modifier = Modifier.semantics { heading() }
        )
        Text(
            stringResource(R.string.onboarding_welcome_subtitle),
            style = MaterialTheme.typography.bodyLarge,
            color = LocalExtraColors.current.textSecondary
        )
    }

    AppCard {
        Column(verticalArrangement = Arrangement.spacedBy(Spacing.xs)) {
            BubbleLabel(stringResource(R.string.onboarding_example))
            IncomingBubble(stringResource(R.string.onboarding_welcome_incoming))
            Box(modifier = Modifier.fillMaxWidth().padding(top = Spacing.xs), contentAlignment = Alignment.CenterEnd) {
                BubbleLabel(stringResource(R.string.onboarding_reply_variant), accent = true)
            }
            ReplyBubble(stringResource(OnboardingExamples.welcomeReply(gender)))
        }
    }

    FlowSteps()
}

/** «Скопируй сообщение.» › «Выбери собеседника.» › «Получи ответ.» */
@Composable
private fun FlowSteps() {
    Row(
        modifier = Modifier.fillMaxWidth(),
        verticalAlignment = Alignment.Top
    ) {
        FlowStep(Icons.Outlined.ContentCopy, stringResource(R.string.onboarding_flow_copy), Modifier.weight(1f))
        Chevron()
        FlowStep(Icons.Outlined.Group, stringResource(R.string.onboarding_flow_choose), Modifier.weight(1f))
        Chevron()
        FlowStep(Icons.Outlined.AutoAwesome, stringResource(R.string.onboarding_flow_reply), Modifier.weight(1f))
    }
}

@Composable
private fun FlowStep(icon: ImageVector, label: String, modifier: Modifier) {
    Column(
        modifier = modifier,
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(Spacing.xs)
    ) {
        Box(
            modifier = Modifier
                .size(44.dp)
                .clip(CircleShape)
                .background(MaterialTheme.colorScheme.primaryContainer),
            contentAlignment = Alignment.Center
        ) {
            Icon(icon, contentDescription = null, tint = MaterialTheme.colorScheme.primary, modifier = Modifier.size(22.dp))
        }
        Text(label, style = MaterialTheme.typography.bodySmall, textAlign = TextAlign.Center)
    }
}

@Composable
private fun Chevron() {
    Icon(
        Icons.AutoMirrored.Filled.KeyboardArrowRight,
        contentDescription = null,
        tint = LocalExtraColors.current.textTertiary,
        modifier = Modifier.padding(top = 12.dp).size(20.dp)
    )
}

private const val COMPACT_WIDTH_DP = 380
