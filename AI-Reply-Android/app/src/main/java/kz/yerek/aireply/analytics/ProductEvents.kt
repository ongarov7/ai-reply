package kz.yerek.aireply.analytics

import android.content.SharedPreferences
import kz.yerek.aireply.platform.ReplyLog

/**
 * The product events the APP records. The keyboard records nothing.
 *
 * Өнім оқиғалары: тек атау мен шағын мәндер, ешқашан мәтін емес.
 *
 * The names are the wire names the events endpoint allow-lists; the iOS app
 * uses the same ones. A property is a number, a flag or a short code — never
 * a message, an instruction, a reply, a gender or anything else the user
 * typed or chose about themselves.
 */
enum class ProductEvent(val wireName: String) {
    /** `version`, `trigger` (`auto` | `settings`). */
    ONBOARDING_STARTED("onboarding_started"),
    /** `step`: the step id. */
    ONBOARDING_STEP_VIEWED("onboarding_step_viewed"),
    ONBOARDING_KEYBOARD_STEP_VIEWED("onboarding_keyboard_step_viewed"),
    /** Once per install: the system reported the keyboard enabled. */
    KEYBOARD_ENABLED_DETECTED("keyboard_enabled_detected"),
    PASTE_TUTORIAL_VIEWED("paste_tutorial_viewed"),
    ONBOARDING_PRACTICE_COMPLETED("onboarding_practice_completed"),
    /** `version`, `skipped`. */
    ONBOARDING_COMPLETED("onboarding_completed"),
    ONBOARDING_REOPENED("onboarding_reopened"),
    /** `source` (`onboarding` | `settings`), `skipped`. Never the value chosen. */
    GENDER_SELECTED("gender_selected"),
    AUTOCORRECT_ENABLED("autocorrect_enabled"),
    AUTOCORRECT_DISABLED("autocorrect_disabled")
}

/** The only shapes a property can take. */
sealed interface ProductEventValue {
    data class Number(val value: Int) : ProductEventValue
    data class Flag(val value: Boolean) : ProductEventValue
    /** A short machine code such as a step id, never free text. */
    data class Code(val value: String) : ProductEventValue
}

/** Where recorded events go. */
fun interface ProductEventSink {
    fun record(name: String, properties: Map<String, ProductEventValue>)
}

/**
 * The facade every screen calls.
 *
 * Where events go is the [sink]'s business, not the screens': the Application
 * installs [ProductEventReporter], which sends them to the server's
 * allow-listed events endpoint. Until then — and in tests — [sink] writes to
 * the debug log only, and a release build records nothing.
 */
object ProductEvents {

    @Volatile
    var sink: ProductEventSink = ProductEventSink { name, properties ->
        ReplyLog.event { "event $name $properties" }
    }

    @Volatile
    private var onceStore: SharedPreferences? = null

    /**
     * Called once by the Application with a file that is not backed up, so
     * "once" means once per install.
     */
    fun install(preferences: SharedPreferences) {
        onceStore = preferences
    }

    fun track(event: ProductEvent, properties: Map<String, ProductEventValue> = emptyMap()) {
        sink.record(event.wireName, properties)
    }

    /** Records [event] the first time only. Without a store (tests) nothing is recorded. */
    fun trackOnce(event: ProductEvent, properties: Map<String, ProductEventValue> = emptyMap()) {
        val store = onceStore ?: return
        val key = ONCE_PREFIX + event.wireName
        if (store.getBoolean(key, false)) return
        store.edit().putBoolean(key, true).apply()
        track(event, properties)
    }

    private const val ONCE_PREFIX = "analytics.once."
}
