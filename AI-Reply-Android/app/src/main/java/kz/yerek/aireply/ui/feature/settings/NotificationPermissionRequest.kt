package kz.yerek.aireply.ui.feature.settings

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.core.app.ActivityCompat
import kz.yerek.aireply.push.NotificationPermission
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.feature.account.findActivity

/**
 * Shows the system notification dialog and reports the answer, for the Home
 * card and Settings ▸ Notifications alike.
 *
 * The rationale flag is read just before the dialog and right after it:
 * together they tell a real "Don't allow" from a dialog the user only
 * dismissed, which must not be taken as "never ask again". Call the returned
 * function only where the dialog can still appear
 * ([kz.yerek.aireply.push.PushUiState.canAskSystem]).
 *
 * @param fromPrompt the Home card: it stays hidden after any answer but yes.
 */
@Composable
fun rememberNotificationPermissionRequest(fromPrompt: Boolean): () -> Unit {
    val push = LocalServices.current.push
    val context = LocalContext.current
    var rationaleBefore by rememberSaveable { mutableStateOf(false) }

    val launcher = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        val rationaleAfter = context.findActivity()?.let { activity ->
            ActivityCompat.shouldShowRequestPermissionRationale(activity, NotificationPermission.PERMISSION)
        } ?: false
        push.onPermissionResult(granted, rationaleBefore, rationaleAfter, fromPrompt)
    }

    return {
        rationaleBefore = context.findActivity()?.let { activity ->
            ActivityCompat.shouldShowRequestPermissionRationale(activity, NotificationPermission.PERMISSION)
        } ?: false
        push.onPermissionRequested()
        launcher.launch(NotificationPermission.PERMISSION)
    }
}
