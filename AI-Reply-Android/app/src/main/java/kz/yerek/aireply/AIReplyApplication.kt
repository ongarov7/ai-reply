package kz.yerek.aireply

import android.app.Application
import kz.yerek.aireply.ai.AILimits
import kz.yerek.aireply.analytics.ProductEvents

/**
 * Holds the object graph.
 *
 * The keyboard service and the app's Activity run in the same process, so this
 * is created once for both. That is the Android answer to the problem iOS
 * solves with an App Group: there is no container to share, because there is
 * nothing separate to share it between.
 */
class AIReplyApplication : Application() {

    lateinit var services: ServiceLocator
        private set

    override fun onCreate() {
        super.onCreate()
        services = ServiceLocator(this)
        // Pull the preferences file into memory now. Everything that reads it
        // later — including the keyboard's first frame — is then an in-memory
        // map lookup rather than a disk read on whatever thread asked.
        services.settings.warmUp()
        AILimits.install(services.settings.sharedPreferences)
        ProductEvents.install(services.deviceState.sharedPreferences)
        ProductEvents.sink = services.productEvents
        instance = this
    }

    companion object {
        @Volatile
        private var instance: AIReplyApplication? = null

        /**
         * The graph, from anywhere with a Context.
         *
         * Falls back to constructing from the application context when the
         * process was started for a component that did not go through
         * [onCreate] first — which should not happen, but a keyboard that
         * crashes inside WhatsApp is a much worse failure than one extra
         * construction.
         */
        fun services(context: android.content.Context): ServiceLocator {
            (context.applicationContext as? AIReplyApplication)?.let { return it.services }
            return instance?.services ?: ServiceLocator(context.applicationContext)
        }
    }
}
