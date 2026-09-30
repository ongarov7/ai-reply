package kz.yerek.aireply.push

import android.Manifest
import android.annotation.SuppressLint
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.provider.Settings
import androidx.annotation.ChecksSdkIntAtLeast
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat

/**
 * The system's side of notifications: the Android 13+ runtime permission and
 * the app-wide notification switch in system settings.
 *
 * Хабарлама рұқсаты: Android 13-тен бастап жүйе сұрайды, одан бұрын — баптауда.
 */
object NotificationPermission {

    const val AUTHORIZED = "authorized"
    const val DENIED = "denied"
    const val NOT_DETERMINED = "not_determined"
    const val UNKNOWN = "unknown"

    /**
     * `android.permission.POST_NOTIFICATIONS`. A String constant the compiler
     * inlines, so naming it on older versions is harmless; it is only ever
     * requested where [isRuntimePermission] is true.
     */
    @SuppressLint("InlinedApi")
    const val PERMISSION: String = Manifest.permission.POST_NOTIFICATIONS

    /** POST_NOTIFICATIONS exists as a runtime permission from Android 13. */
    @get:ChecksSdkIntAtLeast(api = Build.VERSION_CODES.TIRAMISU)
    val isRuntimePermission: Boolean
        get() = Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU

    /** The runtime permission is held (always true before Android 13). */
    fun isGranted(context: Context): Boolean =
        !isRuntimePermission ||
            ContextCompat.checkSelfPermission(context, PERMISSION) == PackageManager.PERMISSION_GRANTED

    /** Notifications would actually be shown: permission held and not switched off in system settings. */
    fun canPost(context: Context): Boolean =
        isGranted(context) && NotificationManagerCompat.from(context).areNotificationsEnabled()

    /**
     * The value the installation registration reports.
     *
     * Before Android 13 there is nothing to ask, so it is the system switch.
     * From 13, a permission the app never asked for is `not_determined`; one it
     * asked for and did not get — or granted but switched off — is `denied`.
     */
    fun status(context: Context, store: PushStateStore): String = runCatching {
        val enabled = NotificationManagerCompat.from(context).areNotificationsEnabled()
        when {
            !isRuntimePermission -> if (enabled) AUTHORIZED else DENIED
            isGranted(context) -> if (enabled) AUTHORIZED else DENIED
            store.permissionRequested -> DENIED
            else -> NOT_DETERMINED
        }
    }.getOrDefault(UNKNOWN)

    /** The app's notification page in system settings (the only way back after "Don't allow"). */
    fun openSystemSettings(context: Context) {
        val intent = Intent(Settings.ACTION_APP_NOTIFICATION_SETTINGS)
            .putExtra(Settings.EXTRA_APP_PACKAGE, context.packageName)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        val fallback = Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.fromParts("package", context.packageName, null))
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        runCatching { context.startActivity(intent) }
            .onFailure { runCatching { context.startActivity(fallback) } }
    }
}
