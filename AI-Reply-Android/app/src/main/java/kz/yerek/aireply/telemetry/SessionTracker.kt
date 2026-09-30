package kz.yerek.aireply.telemetry

import android.app.Activity
import android.app.Application
import android.os.Bundle
import java.util.UUID

/**
 * The app's foreground sessions.
 *
 * Сессия: қосымша алғаш ашылғанда және фонда 30 минуттан көп тұрғаннан кейін жаңарады.
 *
 * A session id is minted the first time the app comes to the front in this
 * process, and again when it returns after [timeoutMs] or more in the
 * background. Only the app's own screens count: the keyboard lives in the same
 * process but is not "the app being open", so it never starts a session.
 *
 * Pure logic with an injected clock; [Callbacks] feeds it from the platform.
 */
class SessionTracker(
    private val clock: () -> Long = System::currentTimeMillis,
    private val newId: () -> String = { UUID.randomUUID().toString() },
    private val timeoutMs: Long = SESSION_TIMEOUT_MS,
    private val listener: Listener
) {

    interface Listener {
        fun onForeground(coldStart: Boolean)
        fun onBackground(foregroundMillis: Long)
    }

    @Volatile
    var sessionId: String? = null
        private set

    /** True while one of the app's screens is started. */
    @Volatile
    var isForeground: Boolean = false
        private set

    private var started = 0
    private var foregroundSince = 0L
    private var backgroundSince: Long? = null
    private var hasBeenForeground = false

    @Synchronized
    fun screenStarted() {
        started++
        if (started == 1 && !isForeground) enterForeground()
    }

    /**
     * [changingConfigurations]: the screen is being recreated (a language
     * change, say), and the app never really left the front.
     */
    @Synchronized
    fun screenStopped(changingConfigurations: Boolean) {
        if (started == 0) return
        started--
        if (started == 0 && !changingConfigurations && isForeground) enterBackground()
    }

    private fun enterForeground() {
        val now = clock()
        val coldStart = !hasBeenForeground
        val away = backgroundSince?.let { now - it }
        if (sessionId == null || (away != null && away >= timeoutMs)) sessionId = newId()
        hasBeenForeground = true
        isForeground = true
        foregroundSince = now
        backgroundSince = null
        listener.onForeground(coldStart)
    }

    private fun enterBackground() {
        val now = clock()
        isForeground = false
        backgroundSince = now
        listener.onBackground((now - foregroundSince).coerceAtLeast(0))
    }

    /**
     * Feeds [SessionTracker] from the activities [counts] accepts. Activity
     * callbacks rather than ProcessLifecycleOwner: lifecycle-process is not a
     * dependency of this app, and the process-wide owner would count the
     * transparent microphone-permission screen the keyboard opens.
     */
    class Callbacks(
        private val tracker: SessionTracker,
        private val counts: (Activity) -> Boolean
    ) : Application.ActivityLifecycleCallbacks {

        override fun onActivityStarted(activity: Activity) {
            if (counts(activity)) tracker.screenStarted()
        }

        override fun onActivityStopped(activity: Activity) {
            if (counts(activity)) tracker.screenStopped(activity.isChangingConfigurations)
        }

        override fun onActivityCreated(activity: Activity, savedInstanceState: Bundle?) = Unit
        override fun onActivityResumed(activity: Activity) = Unit
        override fun onActivityPaused(activity: Activity) = Unit
        override fun onActivitySaveInstanceState(activity: Activity, outState: Bundle) = Unit
        override fun onActivityDestroyed(activity: Activity) = Unit
    }

    companion object {
        const val SESSION_TIMEOUT_MS = 30L * 60 * 1000
    }
}
