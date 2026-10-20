package kz.yerek.aireply.ui.feature.setup

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import kz.yerek.aireply.R
import kz.yerek.aireply.ui.common.AppScreen
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.design.ReadableColumn
import kz.yerek.aireply.ui.design.Spacing

/**
 * The permissions section as a screen of its own, for aireply://permissions
 * and aireply://keyboard (a reminder to finish the setup lands here).
 */
@Composable
fun PermissionsScreen(onBack: () -> Unit, onOpen: (String) -> Unit) {
    AppScreen(title = stringResource(R.string.permissions_title), onBack = onBack) {
        ReadableColumn(verticalArrangement = Arrangement.spacedBy(Spacing.m)) {
            Footnote(stringResource(R.string.permissions_intro))
            KeyboardPermissionsContent(onOpen)
        }
    }
}
