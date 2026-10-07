package kz.yerek.aireply.ui.common

import android.content.Context
import android.database.ContentObserver
import android.os.Handler
import android.os.Looper
import android.provider.Settings
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.State
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalWindowInfo
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import kz.yerek.aireply.analytics.ProductEvent
import kz.yerek.aireply.analytics.ProductEvents
import kz.yerek.aireply.keyboard.input.KeyboardStatus

/**
 * Whether the keyboard is enabled and selected, kept current while the screen
 * is up.
 *
 * Read on events rather than polled. The two values change in three places,
 * and each has its own signal:
 *
 *  * system Settings takes the user out of the app: the screen resumes;
 *  * the input-method picker is a dialog over this window, so nothing
 *    resumes — but the window gets its focus back when the dialog closes;
 *  * either value can also change while the screen is visible (the picker,
 *    a quick-settings tile), which the secure settings report through a
 *    content observer.
 */
@Composable
fun rememberKeyboardStatus(): State<KeyboardStatus.Snapshot> {
    val context = LocalContext.current
    val owner = LocalLifecycleOwner.current
    val windowFocused = LocalWindowInfo.current.isWindowFocused
    val state = remember { mutableStateOf(read(context)) }

    DisposableEffect(owner) {
        val observer = LifecycleEventObserver { _, event ->
            if (event == Lifecycle.Event.ON_RESUME) state.refresh(context)
        }
        owner.lifecycle.addObserver(observer)
        onDispose { owner.lifecycle.removeObserver(observer) }
    }

    LaunchedEffect(windowFocused) {
        if (windowFocused) state.refresh(context)
    }

    DisposableEffect(context) {
        val resolver = context.contentResolver
        val observer = object : ContentObserver(Handler(Looper.getMainLooper())) {
            override fun onChange(selfChange: Boolean) = state.refresh(context)
        }
        listOf(Settings.Secure.DEFAULT_INPUT_METHOD, Settings.Secure.ENABLED_INPUT_METHODS).forEach { key ->
            resolver.registerContentObserver(Settings.Secure.getUriFor(key), false, observer)
        }
        onDispose { resolver.unregisterContentObserver(observer) }
    }

    return state
}

private fun MutableState<KeyboardStatus.Snapshot>.refresh(context: Context) {
    value = read(context)
}

private fun read(context: Context): KeyboardStatus.Snapshot {
    val snapshot = KeyboardStatus.current(context)
    if (snapshot.isEnabled) ProductEvents.trackOnce(ProductEvent.KEYBOARD_ENABLED_DETECTED)
    return snapshot
}
