package kz.yerek.aireply.ui.feature.onboarding

/**
 * One screen of the onboarding. [id] is stable: it is what the resume point
 * stores and what analytics reports, and the iOS app uses the same ids.
 */
enum class OnboardingStep(val id: String) {
    WELCOME("welcome"),
    GENDER("gender"),
    KEYBOARD("keyboard"),
    COPY_REPLY("copyReply"),
    PRACTICE("practice");

    companion object {
        fun fromId(id: String?): OnboardingStep? = entries.firstOrNull { it.id == id }
    }
}

enum class OnboardingMode {
    /** Shown once per device after sign-in; finishing it is recorded. */
    FIRST_RUN,

    /** "How to use AI Reply" from Settings: no gender step, nothing recorded. */
    TUTORIAL
}

/**
 * Which steps a run shows, in what order, and where it starts.
 *
 * Онбординг логикасы: экрансыз, JVM-де тексеріледі.
 *
 * Pure on purpose: no Android, no storage. The screen asks it, the tests pin
 * it. Android has no Full Access, so the iOS `fullAccess` step does not exist
 * here.
 *
 * @param asksGender whether this run has the gender step; see [asksGender]
 *   in the companion. Fixed for the whole run, so choosing a gender does not
 *   reshuffle the steps, or the "Step 2 of 4", under the user's finger.
 */
data class OnboardingFlow(val mode: OnboardingMode, val asksGender: Boolean) {

    val steps: List<OnboardingStep> = OnboardingStep.entries.filter { step ->
        step != OnboardingStep.GENDER || (asksGender && mode == OnboardingMode.FIRST_RUN)
    }

    /** A saved resume point when it is still part of this run, else the beginning. */
    fun start(resumeId: String?): OnboardingStep =
        OnboardingStep.fromId(resumeId)?.takeIf { it in steps } ?: steps.first()

    /** Null after the last step: the run is finished. */
    fun next(step: OnboardingStep): OnboardingStep? = steps.getOrNull(steps.indexOf(step) + 1)

    /** Null on the first step. */
    fun previous(step: OnboardingStep): OnboardingStep? = steps.getOrNull(steps.indexOf(step) - 1)

    fun isLast(step: OnboardingStep): Boolean = next(step) == null

    /** "Step 2 of 4": the welcome screen is not counted, so it has no number. */
    fun number(step: OnboardingStep): Int? =
        if (step == OnboardingStep.WELCOME) null else steps.indexOf(step)

    val numberedCount: Int get() = steps.size - 1

    /** Only a first run comes back to where it was; the tutorial starts over. */
    val resumes: Boolean get() = mode == OnboardingMode.FIRST_RUN

    companion object {
        /** Raised whenever existing users should see the onboarding again. */
        const val CURRENT_VERSION = 2

        /** What a profile that finished the old, unversioned onboarding counts as. */
        private const val LEGACY_VERSION = 1

        /**
         * The version this device finished. Before versions existed the
         * profile kept a flag; a device that has never stored a version
         * migrates from it.
         */
        fun completedVersion(stored: Int?, legacyCompleted: Boolean): Int =
            stored ?: if (legacyCompleted) LEGACY_VERSION else 0

        fun needsOnboarding(completedVersion: Int): Boolean = completedVersion < CURRENT_VERSION

        /**
         * Whether a run shows the gender step. A first run asks when the
         * gender was never answered. One that comes back after the process was
         * killed keeps what it started with ([startedAsking], saved with the
         * resume point), although the gender is answered by then. The tutorial
         * never asks.
         */
        fun asksGender(mode: OnboardingMode, genderAnswered: Boolean, startedAsking: Boolean?): Boolean =
            mode == OnboardingMode.FIRST_RUN && (startedAsking ?: !genderAnswered)

        /**
         * Whether opening the screen starts a run, for `onboarding_started`.
         * A first run with a saved resume point ([savedStep]) is the same run
         * coming back after the process was killed, not a new start; the
         * tutorial starts every time. The iOS rule, so both funnels count alike.
         */
        fun startsRun(mode: OnboardingMode, savedStep: String?): Boolean =
            mode == OnboardingMode.TUTORIAL || savedStep == null
    }
}
