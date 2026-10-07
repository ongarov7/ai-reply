package kz.yerek.aireply.ui.feature.setup

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import kz.yerek.aireply.R
import kz.yerek.aireply.keyboard.input.KeyboardStatus
import kz.yerek.aireply.ui.common.ChecklistRow
import kz.yerek.aireply.ui.common.ChecklistState
import kz.yerek.aireply.ui.design.Spacing

/**
 * "AI Reply keyboard: Enabled" and "Current keyboard: AI Reply" — facts the
 * system reports, so there is no "not known yet" state as on iOS.
 */
@Composable
fun KeyboardStatusRows(status: KeyboardStatus.Snapshot) {
    Column(verticalArrangement = Arrangement.spacedBy(Spacing.s)) {
        ChecklistRow(
            title = stringResource(R.string.android_status_keyboard),
            state = if (status.isEnabled) ChecklistState.DONE else ChecklistState.MISSING,
            statusLabel = stringResource(
                if (status.isEnabled) R.string.android_status_enabled else R.string.android_status_not_enabled
            )
        )
        ChecklistRow(
            title = stringResource(R.string.android_status_current),
            state = if (status.isSelected) ChecklistState.DONE else ChecklistState.MISSING,
            statusLabel = stringResource(
                if (status.isSelected) R.string.app_name else R.string.android_status_other_keyboard
            )
        )
    }
}
