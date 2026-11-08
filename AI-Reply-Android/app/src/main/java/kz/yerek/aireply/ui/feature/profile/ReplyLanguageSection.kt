package kz.yerek.aireply.ui.feature.profile

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import kz.yerek.aireply.R
import kz.yerek.aireply.domain.model.ReplyLanguagePreference
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.AppSection

/**
 * Which language replies are written in. Automatic, the language of the copied
 * message, comes first and is where every profile starts.
 *
 * Part of the draft the profile screen saves on the way out, unlike
 * [GenderSection], which is applied at once because it is synced to the account.
 */
@Composable
fun ReplyLanguageSection(
    selection: ReplyLanguagePreference?,
    onSelect: (ReplyLanguagePreference?) -> Unit
) {
    AppSection(stringResource(R.string.profile_replylanguage)) {
        AppCard {
            Column(modifier = Modifier.selectableGroup()) {
                SelectableRow(
                    label = stringResource(R.string.profile_replylanguage_auto),
                    selected = selection == null
                ) { onSelect(null) }
                ReplyLanguagePreference.entries.forEach { option ->
                    SelectableRow(label = option.nativeName, selected = selection == option) {
                        onSelect(option)
                    }
                }
            }
        }
        Footnote(stringResource(R.string.profile_replylanguage_footer))
    }
}
