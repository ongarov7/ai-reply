package kz.yerek.aireply.push

import android.content.Context
import com.google.firebase.FirebaseApp
import com.google.firebase.messaging.FirebaseMessaging
import kotlinx.coroutines.suspendCancellableCoroutine
import kz.yerek.aireply.platform.ReplyLog
import kotlin.coroutines.resume

/**
 * Whether this BUILD can receive pushes at all.
 *
 * Бұл жинақта Firebase бар ма: google-services.json болмаса, push жоқ.
 *
 * Firebase initialises itself at startup only when the build was made with
 * app/google-services.json. Without it FirebaseApp has no instance and
 * FirebaseMessaging.getInstance() throws, so nothing here touches Firebase
 * Messaging unless [isAvailable] said yes.
 */
object PushSupport {

    fun isAvailable(context: Context): Boolean =
        runCatching { FirebaseApp.getApps(context).isNotEmpty() }
            .onFailure { ReplyLog.warn(it) { "Firebase not available in this build" } }
            .getOrDefault(false)

    sealed interface TokenResult {
        data class Token(val value: String) : TokenResult
        /** Firebase could not produce one (no Play services, no network…). */
        data object Failed : TokenResult
        data object Unsupported : TokenResult
    }

    /**
     * The current FCM registration token. Called only after the legal consent:
     * it also switches on Firebase's automatic token handling, which the
     * manifest keeps off until then (`firebase_messaging_auto_init_enabled`).
     */
    suspend fun fetchToken(context: Context): TokenResult {
        if (!isAvailable(context)) return TokenResult.Unsupported
        val task = runCatching {
            val messaging = FirebaseMessaging.getInstance()
            if (!messaging.isAutoInitEnabled) messaging.isAutoInitEnabled = true
            messaging.token
        }.onFailure { ReplyLog.warn(it) { "FCM token request not started" } }
            .getOrNull() ?: return TokenResult.Failed
        return suspendCancellableCoroutine { continuation ->
            task.addOnCompleteListener { completed ->
                val token = if (completed.isSuccessful) completed.result?.takeIf(String::isNotBlank) else null
                if (token == null) ReplyLog.warn(completed.exception) { "FCM token unavailable" }
                if (continuation.isActive) {
                    continuation.resume(if (token != null) TokenResult.Token(token) else TokenResult.Failed)
                }
            }
        }
    }
}
