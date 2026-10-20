package kz.yerek.aireply.ui.common

import android.content.Context
import android.database.ContentObserver
import android.os.Handler
import android.os.Looper
import android.provider.Settings
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.Stable
import androidx.compose.runtime.State
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalWindowInfo
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import kz.yerek.aireply.analytics.ProductEvent
import kz.yerek.aireply.analytics.ProductEvents
import kz.yerek.aireply.data.settings.DeviceStateStore
import kz.yerek.aireply.keyboard.input.KeyboardStatus
import kz.yerek.aireply.ui.LocalServices

/** What the system said about the keyboard, and when it was asked. */
data class KeyboardCheck(val status: KeyboardStatus.Snapshot, val checkedAt: Long)

/**
 * The keyboard's state as last read from the system, re-read on [refresh].
 * Every read is noted in [DeviceStateStore.lastKeyboardCheckAt], so «Последняя
 * проверка» and the diagnostics say when the answer was true.
 *
 * Пернетақта күйі: жүйеден оқылады, оқылған уақыты сақталады.
 */
@Stable
class KeyboardCheckState internal constructor(
    private val context: Context,
    private val deviceState: DeviceStateStore?
) {
    var current: KeyboardCheck by mutableStateOf(read())
        private set

    fun refresh() {
        current = read()
    }

    private fun read(): KeyboardCheck {
        val snapshot = KeyboardStatus.current(context)
        if (snapshot.isEnabled) ProductEvents.trackOnce(ProductEvent.KEYBOARD_ENABLED_DETECTED)
        val now = System.currentTimeMillis()
        deviceState?.noteKeyboardCheck(now)
        return KeyboardCheck(snapshot, now)
    }
}

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
fun rememberKeyboardCheck(): KeyboardCheckState {
    val context = LocalContext.current
    val deviceState = LocalServices.current.deviceState
    val owner = LocalLifecycleOwner.current
    val windowFocused = LocalWindowInfo.current.isWindowFocused
    val state = remember(context) { KeyboardCheckState(context, deviceState) }

    DisposableEffect(owner, state) {
        val observer = LifecycleEventObserver { _, event ->
            if (event == Lifecycle.Event.ON_RESUME) state.refresh()
        }
        owner.lifecycle.addObserver(observer)
        onDispose { owner.lifecycle.removeObserver(observer) }
    }

    LaunchedEffect(windowFocused, state) {
        if (windowFocused) state.refresh()
    }

    DisposableEffect(context, state) {
        val resolver = context.contentResolver
        val observer = object : ContentObserver(Handler(Looper.getMainLooper())) {
            override fun onChange(selfChange: Boolean) = state.refresh()
        }
        listOf(Settings.Secure.DEFAULT_INPUT_METHOD, Settings.Secure.ENABLED_INPUT_METHODS).forEach { key ->
            resolver.registerContentObserver(Settings.Secure.getUriFor(key), false, observer)
        }
        onDispose { resolver.unregisterContentObserver(observer) }
    }

    return state
}

/** Only the enabled / selected facts of [rememberKeyboardCheck]. */
@Composable
fun rememberKeyboardStatus(): State<KeyboardStatus.Snapshot> {
    val check = rememberKeyboardCheck()
    return remember(check) { derivedStateOf { check.current.status } }
}
