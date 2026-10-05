package kz.yerek.aireply.data.settings

import android.content.Context
import android.content.SharedPreferences

/**
 * State that belongs to THIS phone and must not follow the user to another
 * one.
 *
 * Бұл құрылғының ғана күйі: сақтық көшірмеге түспейді.
 *
 * The keyboard is enabled per device, so a finished onboarding restored from a
 * backup onto a new phone would skip exactly the step that phone still needs.
 * The file is therefore excluded from Auto Backup and device transfer
 * (`res/xml/backup_rules.xml`, `res/xml/data_extraction_rules.xml`), which also
 * makes every "once" flag in it once per install.
 *
 * Nothing secret and no text the user typed is kept here.
 */
class DeviceStateStore internal constructor(private val prefs: SharedPreferences) {

    constructor(context: Context) : this(
        context.applicationContext.getSharedPreferences(NAME, Context.MODE_PRIVATE)
    )

    /** The onboarding version this device finished; null when it never finished one. */
    val completedOnboardingVersion: Int?
        get() = if (prefs.contains(KEY_ONBOARDING_VERSION)) prefs.getInt(KEY_ONBOARDING_VERSION, 0) else null

    /**
     * The step an unfinished onboarding was on, so a process killed halfway
     * resumes there. A step id, never anything the user typed.
     */
    var onboardingResumeStep: String?
        get() = prefs.getString(KEY_RESUME_STEP, null)
        set(value) {
            prefs.edit().apply {
                if (value == null) remove(KEY_RESUME_STEP) else putString(KEY_RESUME_STEP, value)
            }.apply()
        }

    /**
     * Whether the unfinished onboarding shows the gender step, decided when it
     * started. Kept with [onboardingResumeStep] so a run that comes back has
     * the same steps, and the same "Step 2 of 4", as before it was killed.
     * Null when no run is in progress.
     */
    var onboardingAsksGender: Boolean?
        get() = if (prefs.contains(KEY_ASKS_GENDER)) prefs.getBoolean(KEY_ASKS_GENDER, false) else null
        set(value) {
            prefs.edit().apply {
                if (value == null) remove(KEY_ASKS_GENDER) else putBoolean(KEY_ASKS_GENDER, value)
            }.apply()
        }

    /** Records a finished onboarding. A version never goes down, and the run in progress is gone. */
    fun completeOnboarding(version: Int) {
        val stored = completedOnboardingVersion ?: 0
        prefs.edit()
            .putInt(KEY_ONBOARDING_VERSION, maxOf(stored, version))
            .remove(KEY_RESUME_STEP)
            .remove(KEY_ASKS_GENDER)
            .apply()
    }

    /** A profile change was saved here but has not reached the server yet. */
    var profilePendingSync: Boolean
        get() = prefs.getBoolean(KEY_PENDING_SYNC, false)
        set(value) = prefs.edit().putBoolean(KEY_PENDING_SYNC, value).apply()

    /** The backing file, for once-per-install flags such as [kz.yerek.aireply.analytics.ProductEvents]. */
    val sharedPreferences: SharedPreferences get() = prefs

    private companion object {
        const val NAME = "aireply_device"

        const val KEY_ONBOARDING_VERSION = "onboarding.completedVersion"
        const val KEY_RESUME_STEP = "onboarding.resumeStep"
        const val KEY_ASKS_GENDER = "onboarding.asksGender"
        const val KEY_PENDING_SYNC = "profile.pendingSync"
    }
}
