package kz.yerek.aireply.push

import com.google.firebase.messaging.FirebaseMessagingService
import com.google.firebase.messaging.RemoteMessage
import kz.yerek.aireply.AIReplyApplication
import kz.yerek.aireply.platform.ReplyLog

/**
 * Firebase's entry points: a new registration token, and a message that
 * arrived while the app was in the foreground.
 *
 * In the background the system shows our notification+data messages itself
 * and this service is not called; a tap then opens MainActivity with the data
 * as extras. In the foreground Firebase hands the message here and
 * [PushCoordinator.onMessageReceived] shows it the same way.
 */
class AppMessagingService : FirebaseMessagingService() {

    override fun onNewToken(token: String) {
        runCatching { AIReplyApplication.services(this).push.onNewToken(token) }
            .onFailure { ReplyLog.warn(it) { "new FCM token not handled" } }
    }

    override fun onMessageReceived(message: RemoteMessage) {
        runCatching {
            val notification = message.notification
            AIReplyApplication.services(this).push.onMessageReceived(
                data = message.data,
                title = notification?.title,
                body = notification?.body
            )
        }.onFailure { ReplyLog.warn(it) { "push message not shown" } }
    }
}
