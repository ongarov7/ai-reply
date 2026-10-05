package kz.yerek.aireply.ui.feature.onboarding

import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.RadioButtonUnchecked
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import kz.yerek.aireply.R
import kz.yerek.aireply.domain.model.GrammaticalGender
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.Radius
import kz.yerek.aireply.ui.design.Spacing
import kz.yerek.aireply.ui.feature.profile.genderLabel

/**
 * "How should replies speak for you?" Two choices, each with the Russian form
 * it produces. Skip (in the bar below) means neutral wording.
 */
@Composable
internal fun ColumnScope.GenderStep(selected: GrammaticalGender?, onSelect: (GrammaticalGender) -> Unit) {
    StepHeader(
        stringResource(R.string.onboarding_gender_title),
        stringResource(R.string.onboarding_gender_prompt)
    )
    Column(
        modifier = Modifier.selectableGroup(),
        verticalArrangement = Arrangement.spacedBy(Spacing.s)
    ) {
        GenderChoice(
            label = stringResource(genderLabel(GrammaticalGender.MALE)),
            example = stringResource(R.string.onboarding_gender_example_male),
            selected = selected == GrammaticalGender.MALE
        ) { onSelect(GrammaticalGender.MALE) }
        GenderChoice(
            label = stringResource(genderLabel(GrammaticalGender.FEMALE)),
            example = stringResource(R.string.onboarding_gender_example_female),
            selected = selected == GrammaticalGender.FEMALE
        ) { onSelect(GrammaticalGender.FEMALE) }
    }
    Footnote(stringResource(R.string.onboarding_gender_skip_note))
    Footnote(stringResource(R.string.onboarding_gender_footer))
}

@Composable
private fun GenderChoice(label: String, example: String, selected: Boolean, onSelect: () -> Unit) {
    val shape = RoundedCornerShape(Radius.large)
    val outline = if (selected) MaterialTheme.colorScheme.primary else LocalExtraColors.current.separator
    AppCard(
        modifier = Modifier
            .clip(shape)
            .border(if (selected) 2.dp else 0.5.dp, outline, shape)
            .selectable(selected = selected, role = Role.RadioButton, onClick = onSelect)
    ) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Spacing.s)
        ) {
            Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(Spacing.xxs)) {
                Text(label, style = MaterialTheme.typography.titleMedium)
                Text(
                    example,
                    style = MaterialTheme.typography.bodyMedium,
                    color = LocalExtraColors.current.textSecondary
                )
            }
            Icon(
                if (selected) Icons.Filled.CheckCircle else Icons.Filled.RadioButtonUnchecked,
                contentDescription = null,
                tint = if (selected) MaterialTheme.colorScheme.primary else LocalExtraColors.current.textTertiary,
                modifier = Modifier.size(24.dp)
            )
        }
    }
}
