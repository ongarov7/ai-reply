package kz.yerek.aireply.push

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat
import kz.yerek.aireply.MainActivity
import kz.yerek.aireply.R
import kz.yerek.aireply.platform.ReplyLog

/**
 * The two notification channels the server addresses by id.
 *
 * Хабарлама арналары: «general» және «important» (тіркелгі, жазылым, қауіпсіздік).
 *
 * Created at app start and again after a language change, which only renames
 * them: re-creating an existing channel updates its name and description and
 * nothing else, so whatever the user set for sound or importance stays theirs.
 */
object NotificationChannels {

    fun ensure(context: Context, localized: Context) {
        runCatching {
            val manager = context.getSystemService(NotificationManager::class.java) ?: return
            val general = NotificationChannel(
                PushPayload.CHANNEL_GENERAL,
                localized.getString(R.string.push_channel_general),
                NotificationManager.IMPORTANCE_DEFAULT
            ).apply { description = localized.getString(R.string.push_channel_general_description) }
            val important = NotificationChannel(
                PushPayload.CHANNEL_IMPORTANT,
                localized.getString(R.string.push_channel_important),
                NotificationManager.IMPORTANCE_HIGH
            ).apply { description = localized.getString(R.string.push_channel_important_description) }
            manager.createNotificationChannels(listOf(general, important))
        }.onFailure { ReplyLog.warn(it) { "notification channels not created" } }
    }
}

/**
 * Shows a push while the app is in the foreground, when Firebase hands it to
 * the app instead of the system. It looks and behaves exactly like the one the
 * system shows in the background: the same channel rule, the notification id
 * as the tag (a repeated delivery replaces, never stacks), and a tap that
 * starts the launcher activity with the data keys as String extras.
 */
object PushNotifier {

    /** Id used with the tag, as Firebase does for the notifications it shows itself. */
    private const val NOTIFICATION_ID = 0

    /** False when nothing was shown (no permission, switched off, or nothing to show). */
    fun show(
        context: Context,
        localized: Context,
        payload: PushPayload,
        data: Map<String, String>,
        title: String?,
        body: String?
    ): Boolean {
        if (title.isNullOrBlank() && body.isNullOrBlank()) return false
        val manager = NotificationManagerCompat.from(context)
        if (!manager.areNotificationsEnabled()) return false
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) !=
            PackageManager.PERMISSION_GRANTED
        ) {
            return false
        }
        NotificationChannels.ensure(context, localized)

        val tag = payload.notificationId ?: return false
        val notification = NotificationCompat.Builder(context, payload.channelId)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(title?.trim())
            .setContentText(body?.trim())
            .setStyle(NotificationCompat.BigTextStyle().bigText(body?.trim()))
            .setPriority(if (payload.isImportant) NotificationCompat.PRIORITY_HIGH else NotificationCompat.PRIORITY_DEFAULT)
            .setAutoCancel(true)
            .setContentIntent(tapIntent(context, tag, data))
            .build()

        return try {
            manager.notify(tag, NOTIFICATION_ID, notification)
            true
        } catch (revoked: SecurityException) {
            // The permission went away between the check and the post.
            false
        }
    }

    /**
     * The launch intent with the payload as String extras — what Firebase builds
     * for a notification the system shows — so MainActivity handles both kinds
     * of tap the same way. The request code is per notification: PendingIntents
     * that differ only in extras would otherwise be one and the same.
     */
    private fun tapIntent(context: Context, tag: String, data: Map<String, String>): PendingIntent {
        val intent = (context.packageManager.getLaunchIntentForPackage(context.packageName)
            ?: Intent(context, MainActivity::class.java))
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        data.forEach { (key, value) -> intent.putExtra(key, value) }
        return PendingIntent.getActivity(
            context,
            tag.hashCode(),
            intent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )
    }
}
