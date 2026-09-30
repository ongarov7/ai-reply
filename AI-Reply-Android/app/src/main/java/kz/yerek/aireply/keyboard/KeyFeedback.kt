package kz.yerek.aireply.keyboard

import android.content.Context
import android.media.AudioManager
import android.provider.Settings
import android.view.HapticFeedbackConstants
import android.view.View

/**
 * Key-press click and vibration.
 *
 * Both are read from the user's own system settings rather than assumed. Android
 * exposes "Sound on keypress" and "Vibrate on keypress" as real preferences, and
 * a third-party keyboard that clicks when the user switched clicking off is one
 * of the fastest ways to get uninstalled. iOS gets the same behaviour free from
 * `UIDevice.playInputClick()`; here it has to be asked for.
 */
class KeyFeedback(context: Context) {

    /**
     * AI Reply's own "Vibration on key press" switch. On top of it, the
     * system's keypress-vibration setting still applies (see below).
     */
    var hapticsEnabled: Boolean = true

    private val audio = context.getSystemService(Context.AUDIO_SERVICE) as? AudioManager
    private val resolver = context.contentResolver

    private val soundEnabled: Boolean
        get() = runCatching {
            Settings.System.getInt(resolver, Settings.System.SOUND_EFFECTS_ENABLED, 0) != 0
        }.getOrDefault(false)

    fun onKeyPress(view: View?) {
        if (soundEnabled) {
            audio?.playSoundEffect(AudioManager.FX_KEYPRESS_STANDARD, VOLUME)
        }
        // No FLAG_IGNORE_GLOBAL_SETTING: if the user turned keypress haptics
        // off, this must stay silent.
        if (hapticsEnabled) view?.performHapticFeedback(HapticFeedbackConstants.KEYBOARD_TAP)
    }

    private companion object {
        /** Slightly under the system default; a keyboard should not be the loudest thing on the phone. */
        const val VOLUME = 0.6f
    }
}
