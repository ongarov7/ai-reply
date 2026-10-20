package kz.yerek.aireply.ui.feature.onboarding

/**
 * One screen of the onboarding. [id] is stable: it is what the resume point
 * stores and what analytics reports (the server's allow-list has these
 * codes), and the iOS app uses the same ids.
 *
 * @param legacyIds the ids of the version 2 screens this one replaces, so a
 *   run saved by an older build comes back at the matching place.
 */
enum class OnboardingStep(val id: String, private val legacyIds: Set<String> = emptySet()) {
    WELCOME("welcome"),
    TUTORIAL_COPY("tutorial_copy", setOf("copyReply")),
    TUTORIAL_RECIPIENT("tutorial_recipient"),
    TUTORIAL_REPLY("tutorial_reply", setOf("practice")),
    /** «Подключи AI Reply»: turn the keyboard on and choose it. */
    PERMISSIONS("permissions", setOf("keyboard")),
    GENDER("gender");

    val isTutorial: Boolean get() = this in TUTORIAL

    companion object {
        /** «Как пользоваться · 1 из 3», in this order. */
        val TUTORIAL: List<OnboardingStep> = listOf(TUTORIAL_COPY, TUTORIAL_RECIPIENT, TUTORIAL_REPLY)

        fun fromId(id: String?): OnboardingStep? =
            entries.firstOrNull { it.id == id } ?: entries.firstOrNull { id in it.legacyIds }
    }
}

/**
 * Which way the welcome screen sent the user: «Сначала попробовать» shows the
 * tutorial and then the keyboard setup; «Настроить клавиатуру» the other way
 * round. Saved with the resume point.
 */
enum class OnboardingPath(val id: String) {
    TRY_FIRST("try_first"),
    SETUP_FIRST("setup_first");

    companion object {
        fun fromId(id: String?): OnboardingPath = entries.firstOrNull { it.id == id } ?: TRY_FIRST
    }
}

enum class OnboardingMode {
    /** Shown once per device after sign-in; finishing it is recorded. */
    FIRST_RUN,

    /** "How to use AI Reply" from Settings after it was finished: no gender step, nothing recorded. */
    TUTORIAL
}

/** Where this device is with the onboarding, as the permission diagnostics report it. */
enum class OnboardingStatus(val wire: String) {
    NOT_STARTED("not_started"),
    IN_PROGRESS("in_progress"),
    COMPLETED("completed"),
    /** Left with «Позже»: not shown again by itself, offered from Settings and reminders. */
    SKIPPED("skipped");

    companion object {
        fun fromWire(raw: String?): OnboardingStatus? = entries.firstOrNull { it.wire == raw }
    }
}

/**
 * Which steps a run shows, in what order, and where it starts.
 *
 * Онбординг логикасы: экрансыз, JVM-де тексеріледі.
 *
 * Pure on purpose: no Android, no storage. The screen asks it, the tests pin
 * it. Android has no Full Access, so the iOS `fullAccess` step does not exist
 * here. The gender comes last: short, skippable, and only in a first run that
 * never had it answered.
 *
 * @param asksGender whether this run has the gender step; see [asksGender]
 *   in the companion. Fixed for the whole run, so choosing a gender does not
 *   reshuffle the steps under the user's finger.
 */
data class OnboardingFlow(
    val mode: OnboardingMode,
    val asksGender: Boolean,
    val path: OnboardingPath = OnboardingPath.TRY_FIRST
) {

    val steps: List<OnboardingStep> = buildList {
        add(OnboardingStep.WELCOME)
        when (path) {
            OnboardingPath.TRY_FIRST -> {
                addAll(OnboardingStep.TUTORIAL)
                add(OnboardingStep.PERMISSIONS)
            }
            OnboardingPath.SETUP_FIRST -> {
                add(OnboardingStep.PERMISSIONS)
                addAll(OnboardingStep.TUTORIAL)
            }
        }
        if (asksGender && mode == OnboardingMode.FIRST_RUN) add(OnboardingStep.GENDER)
    }

    /** A saved resume point when it is still part of this run, else the beginning. */
    fun start(resumeId: String?): OnboardingStep =
        OnboardingStep.fromId(resumeId)?.takeIf { it in steps } ?: steps.first()

    /** Null after the last step: the run is finished. */
    fun next(step: OnboardingStep): OnboardingStep? = steps.getOrNull(steps.indexOf(step) + 1)

    /** Null on the first step. */
    fun previous(step: OnboardingStep): OnboardingStep? = steps.getOrNull(steps.indexOf(step) - 1)

    fun isLast(step: OnboardingStep): Boolean = next(step) == null

    /** Where «Пропустить» in the tutorial leads: the first step after it; null finishes the run. */
    val afterTutorial: OnboardingStep?
        get() = next(OnboardingStep.TUTORIAL.last())

    /** «1 из 3»: only the tutorial is numbered. */
    fun tutorialNumber(step: OnboardingStep): Int? =
        OnboardingStep.TUTORIAL.indexOf(step).takeIf { it >= 0 }?.plus(1)

    val tutorialCount: Int get() = OnboardingStep.TUTORIAL.size

    /** Only a first run comes back to where it was; the tutorial starts over. */
    val resumes: Boolean get() = mode == OnboardingMode.FIRST_RUN

    companion object {
        /**
         * The version a finished run records. Version 3 is the welcome, the
         * three-step tutorial and «Подключи AI Reply».
         */
        const val CURRENT_VERSION = 3

        /**
         * A device that finished ANY version is not shown a newer one by
         * itself: the redesign is offered from Settings, never forced on
         * someone who already uses the keyboard.
         */
        private const val FIRST_VERSION = 1

        /** What a profile that finished the old, unversioned onboarding counts as. */
        private const val LEGACY_VERSION = 1

        /**
         * The version this device finished. Before versions existed the
         * profile kept a flag; a device that has never stored a version
         * migrates from it.
         */
        fun completedVersion(stored: Int?, legacyCompleted: Boolean): Int =
            stored ?: if (legacyCompleted) LEGACY_VERSION else 0

        fun needsOnboarding(completedVersion: Int): Boolean = completedVersion < FIRST_VERSION

        /**
         * Where the device is. A finished run is completed unless it was left
         * with «Позже»; one finished before the status existed counts as
         * completed. An unfinished one with a saved step is in progress.
         */
        fun status(completedVersion: Int, storedStatus: String?, resumeStep: String?): OnboardingStatus = when {
            completedVersion > 0 ->
                if (OnboardingStatus.fromWire(storedStatus) == OnboardingStatus.SKIPPED) OnboardingStatus.SKIPPED
                else OnboardingStatus.COMPLETED
            resumeStep != null -> OnboardingStatus.IN_PROGRESS
            else -> OnboardingStatus.NOT_STARTED
        }

        /**
         * Opened again - from Settings or `aireply://onboarding`: someone who
         * finished it sees the tutorial, which records nothing; someone who
         * left it with «Позже» gets the real run, so finishing it now counts.
         */
        fun reopenMode(status: OnboardingStatus): OnboardingMode =
            if (status == OnboardingStatus.COMPLETED) OnboardingMode.TUTORIAL else OnboardingMode.FIRST_RUN

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
         * coming back after the process was killed (`onboarding_resumed`),
         * not a new start; the tutorial starts every time. The iOS rule, so
         * both funnels count alike.
         */
        fun startsRun(mode: OnboardingMode, savedStep: String?): Boolean =
            mode == OnboardingMode.TUTORIAL || savedStep == null
    }
}
