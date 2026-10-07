package kz.yerek.aireply.ui.common

import android.provider.Settings
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext

/**
 * True when the user switched animations off (Settings ▸ Accessibility ▸
 * Remove animations, or a zero animator scale in Developer options).
 *
 * Android's equivalent of iOS Reduce Motion: anything that moves by itself
 * shows a still version instead.
 */
@Composable
fun rememberReducedMotion(): Boolean {
    val context = LocalContext.current
    return remember(context) {
        Settings.Global.getFloat(context.contentResolver, Settings.Global.ANIMATOR_DURATION_SCALE, 1f) == 0f
    }
}
