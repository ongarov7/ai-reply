package kz.yerek.aireply.voice

import android.Manifest
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import androidx.core.content.ContextCompat
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.SharedFlow

/**
 * Microphone permission, from a place that cannot ask for one.
 *
 * A Service has no `requestPermissions`, so the keyboard cannot show the system
 * dialog itself. The supported way round this is a transparent Activity that
 * asks on its behalf and finishes — [VoicePermissionActivity]. This object is
 * the channel between the two; they are in the same process, so a shared flow
 * is all it takes.
 */
object MicPermission {

    enum class Outcome { GRANTED, DENIED, PERMANENTLY_DENIED }

    private val _results = MutableSharedFlow<Outcome>(extraBufferCapacity = 1)

    /** Emitted once per completed request. */
    val results: SharedFlow<Outcome> = _results

    fun isGranted(context: Context): Boolean =
        ContextCompat.checkSelfPermission(context, Manifest.permission.RECORD_AUDIO) ==
            PackageManager.PERMISSION_GRANTED

    /**
     * Shows the system dialog. Safe to call from the keyboard service; the
     * answer arrives on [results]. False when the dialog could not be shown,
     * in which case no answer will ever arrive.
     */
    fun request(context: Context): Boolean {
        val intent = Intent(context, VoicePermissionActivity::class.java)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            .addFlags(Intent.FLAG_ACTIVITY_NO_ANIMATION)
        return runCatching { context.startActivity(intent) }.isSuccess
    }

    internal fun publish(outcome: Outcome) {
        _results.tryEmit(outcome)
    }
}
