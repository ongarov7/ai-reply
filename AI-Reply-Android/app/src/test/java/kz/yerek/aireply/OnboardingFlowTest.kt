package kz.yerek.aireply

import kz.yerek.aireply.data.settings.DeviceStateStore
import kz.yerek.aireply.domain.model.GrammaticalGender
import kz.yerek.aireply.ui.feature.onboarding.OnboardingFlow
import kz.yerek.aireply.ui.feature.onboarding.OnboardingMode
import kz.yerek.aireply.ui.feature.onboarding.OnboardingPath
import kz.yerek.aireply.ui.feature.onboarding.OnboardingStatus
import kz.yerek.aireply.ui.feature.onboarding.OnboardingStep
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Which onboarding steps a run shows, where it resumes, and who sees it again.
 *
 * Онбординг қадамдары, қайта жалғастыру және нұсқа көшірісі.
 */
class OnboardingFlowTest {

    private val firstRun = OnboardingFlow(OnboardingMode.FIRST_RUN, asksGender = true)
    private val setupFirst = OnboardingFlow(OnboardingMode.FIRST_RUN, asksGender = true, path = OnboardingPath.SETUP_FIRST)

    @Test
    fun `step ids are the stable codes shared with iOS and the server`() {
        assertEquals(
            listOf("welcome", "tutorial_copy", "tutorial_recipient", "tutorial_reply", "permissions", "gender"),
            OnboardingStep.entries.map { it.id }
        )
        assertEquals(OnboardingStep.TUTORIAL_RECIPIENT, OnboardingStep.fromId("tutorial_recipient"))
        assertNull(OnboardingStep.fromId("fullAccess"))
        assertNull(OnboardingStep.fromId(null))
    }

    @Test
    fun `a run saved by the previous version comes back at the matching place`() {
        assertEquals(OnboardingStep.PERMISSIONS, OnboardingStep.fromId("keyboard"))
        assertEquals(OnboardingStep.TUTORIAL_COPY, OnboardingStep.fromId("copyReply"))
        assertEquals(OnboardingStep.TUTORIAL_REPLY, OnboardingStep.fromId("practice"))
        assertEquals(OnboardingStep.PERMISSIONS, firstRun.start("keyboard"))
    }

    /** Version 2 went keyboard, copy, practice; nothing it saved names a path. */
    @Test
    fun `a run saved by the previous version keeps the keyboard first`() {
        listOf("keyboard", "copyReply", "practice").forEach { step ->
            assertEquals(step, OnboardingPath.SETUP_FIRST, OnboardingPath.saved(null, step))
        }

        val resumed = OnboardingFlow(OnboardingMode.FIRST_RUN, asksGender = false, path = OnboardingPath.saved(null, "keyboard"))
        assertEquals("the tutorial is still ahead", OnboardingStep.TUTORIAL_COPY, resumed.next(resumed.start("keyboard")))
        assertTrue("the keyboard is not asked for twice", resumed.isLast(resumed.start("practice")))

        assertEquals("nothing saved yet", OnboardingPath.TRY_FIRST, OnboardingPath.saved(null, null))
        assertEquals("today's ids save their path", OnboardingPath.TRY_FIRST, OnboardingPath.saved(null, "tutorial_copy"))
        assertEquals("a saved path wins", OnboardingPath.TRY_FIRST, OnboardingPath.saved("try_first", "keyboard"))
    }

    @Test
    fun `try first asks the gender, then the tutorial, then the keyboard`() {
        assertEquals(
            listOf(
                OnboardingStep.WELCOME, OnboardingStep.GENDER, OnboardingStep.TUTORIAL_COPY,
                OnboardingStep.TUTORIAL_RECIPIENT, OnboardingStep.TUTORIAL_REPLY, OnboardingStep.PERMISSIONS
            ),
            firstRun.steps
        )
        assertEquals("skipping the tutorial lands on the keyboard", OnboardingStep.PERMISSIONS, firstRun.afterTutorial)
    }

    @Test
    fun `set up the keyboard first puts the gender and the tutorial after it`() {
        assertEquals(
            listOf(
                OnboardingStep.WELCOME, OnboardingStep.PERMISSIONS, OnboardingStep.GENDER, OnboardingStep.TUTORIAL_COPY,
                OnboardingStep.TUTORIAL_RECIPIENT, OnboardingStep.TUTORIAL_REPLY
            ),
            setupFirst.steps
        )
        assertEquals(OnboardingStep.PERMISSIONS, setupFirst.next(OnboardingStep.WELCOME))
        assertNull("nothing after the tutorial: skipping it finishes", setupFirst.afterTutorial)
        assertEquals(OnboardingStep.TUTORIAL_COPY, setupFirst.copy(asksGender = false).next(OnboardingStep.PERMISSIONS))
        assertEquals(OnboardingPath.SETUP_FIRST, OnboardingPath.fromId("setup_first"))
        assertEquals("an unknown path is the default", OnboardingPath.TRY_FIRST, OnboardingPath.fromId(null))
    }

    @Test
    fun `a first run asks the gender only when it was never asked`() {
        val answered = OnboardingFlow(OnboardingMode.FIRST_RUN, asksGender = false)
        assertFalse(OnboardingStep.GENDER in answered.steps)
        assertTrue(answered.isLast(OnboardingStep.PERMISSIONS))
    }

    @Test
    fun `the tutorial never asks the gender`() {
        val tutorial = OnboardingFlow(OnboardingMode.TUTORIAL, asksGender = true)
        assertFalse(OnboardingStep.GENDER in tutorial.steps)
        assertEquals(OnboardingStep.PERMISSIONS, tutorial.steps.last())
    }

    @Test
    fun `steps go forward and back and the last one finishes`() {
        assertEquals(OnboardingStep.GENDER, firstRun.next(OnboardingStep.WELCOME))
        assertEquals(OnboardingStep.TUTORIAL_COPY, firstRun.next(OnboardingStep.GENDER))
        assertNull("after the last step the run is finished", firstRun.next(OnboardingStep.PERMISSIONS))
        assertTrue(firstRun.isLast(OnboardingStep.PERMISSIONS))

        assertNull(firstRun.previous(OnboardingStep.WELCOME))
        assertEquals(OnboardingStep.GENDER, firstRun.previous(OnboardingStep.TUTORIAL_COPY))
        assertEquals("back follows the chosen path", OnboardingStep.WELCOME, setupFirst.previous(OnboardingStep.PERMISSIONS))
        assertEquals(OnboardingStep.PERMISSIONS, setupFirst.previous(OnboardingStep.GENDER))
        assertEquals(OnboardingStep.GENDER, setupFirst.previous(OnboardingStep.TUTORIAL_COPY))
    }

    @Test
    fun `only the tutorial is numbered, one to three`() {
        assertNull(firstRun.tutorialNumber(OnboardingStep.WELCOME))
        assertNull(firstRun.tutorialNumber(OnboardingStep.PERMISSIONS))
        assertEquals(1, firstRun.tutorialNumber(OnboardingStep.TUTORIAL_COPY))
        assertEquals(3, setupFirst.tutorialNumber(OnboardingStep.TUTORIAL_REPLY))
        assertEquals(3, firstRun.tutorialCount)
        assertTrue(OnboardingStep.TUTORIAL_RECIPIENT.isTutorial)
        assertFalse(OnboardingStep.GENDER.isTutorial)
    }

    @Test
    fun `a first run resumes where it stopped and the tutorial starts over`() {
        assertTrue(firstRun.resumes)
        assertEquals(OnboardingStep.TUTORIAL_RECIPIENT, firstRun.start("tutorial_recipient"))
        assertEquals(OnboardingStep.WELCOME, firstRun.start(null))
        assertEquals("an id from a newer build", OnboardingStep.WELCOME, firstRun.start("somethingNew"))

        val answered = OnboardingFlow(OnboardingMode.FIRST_RUN, asksGender = false)
        assertEquals("a step this run no longer has", OnboardingStep.WELCOME, answered.start("gender"))

        assertFalse(OnboardingFlow(OnboardingMode.TUTORIAL, asksGender = false).resumes)
    }

    @Test
    fun `a run keeps its steps from start to finish`() {
        assertTrue("never asked", OnboardingFlow.asksGender(OnboardingMode.FIRST_RUN, genderAnswered = false, startedAsking = null))
        assertFalse("answered before", OnboardingFlow.asksGender(OnboardingMode.FIRST_RUN, genderAnswered = true, startedAsking = null))

        // Killed after the gender was chosen: the run comes back with the step it started with.
        val resumed = OnboardingFlow(
            OnboardingMode.FIRST_RUN,
            OnboardingFlow.asksGender(OnboardingMode.FIRST_RUN, genderAnswered = true, startedAsking = true)
        )
        assertEquals(firstRun.steps, resumed.steps)
        assertEquals(OnboardingStep.GENDER, resumed.start("gender"))

        assertFalse(
            "a run that started without it does not grow one",
            OnboardingFlow.asksGender(OnboardingMode.FIRST_RUN, genderAnswered = false, startedAsking = false)
        )
        assertFalse(
            "the tutorial never asks",
            OnboardingFlow.asksGender(OnboardingMode.TUTORIAL, genderAnswered = false, startedAsking = true)
        )
    }

    @Test
    fun `skipping the gender never erases an answer already known`() {
        assertEquals(GrammaticalGender.UNSPECIFIED, OnboardingFlow.skippedGender(null))
        assertEquals(GrammaticalGender.UNSPECIFIED, OnboardingFlow.skippedGender(GrammaticalGender.UNSPECIFIED))
        assertNull(OnboardingFlow.skippedGender(GrammaticalGender.MALE))
        assertNull(OnboardingFlow.skippedGender(GrammaticalGender.FEMALE))
    }

    @Test
    fun `a profile that finished the old onboarding counts as version 1`() {
        assertEquals(1, OnboardingFlow.completedVersion(stored = null, legacyCompleted = true))
        assertEquals(0, OnboardingFlow.completedVersion(stored = null, legacyCompleted = false))
        assertEquals("a stored version wins", 2, OnboardingFlow.completedVersion(stored = 2, legacyCompleted = false))
    }

    @Test
    fun `someone who finished any version never sees the new one by itself`() {
        assertEquals(3, OnboardingFlow.CURRENT_VERSION)
        assertTrue("new users", OnboardingFlow.needsOnboarding(0))
        assertFalse("users of the unversioned onboarding", OnboardingFlow.needsOnboarding(1))
        assertFalse("users of version 2", OnboardingFlow.needsOnboarding(2))
        assertFalse(OnboardingFlow.needsOnboarding(3))
    }

    @Test
    fun `the status says where the device is`() {
        assertEquals(OnboardingStatus.NOT_STARTED, OnboardingFlow.status(0, storedStatus = null, resumeStep = null))
        assertEquals(OnboardingStatus.IN_PROGRESS, OnboardingFlow.status(0, null, resumeStep = "tutorial_copy"))
        assertEquals(OnboardingStatus.COMPLETED, OnboardingFlow.status(3, "completed", null))
        assertEquals(OnboardingStatus.SKIPPED, OnboardingFlow.status(3, "skipped", null))
        assertEquals("finished before the status was kept", OnboardingStatus.COMPLETED, OnboardingFlow.status(2, null, null))
        assertEquals(listOf("not_started", "in_progress", "completed", "skipped"), OnboardingStatus.entries.map { it.wire })
    }

    @Test
    fun `opened again it is always the tutorial`() {
        val route = File("src/main/java/kz/yerek/aireply/ui/navigation/AppNavHost.kt").readText()
            .substringAfter("composable(Routes.Tutorial)")
            .substringBefore("composable(Routes.Home)")
        assertTrue(route, "OnboardingMode.TUTORIAL" in route)
        assertFalse(route, "FIRST_RUN" in route)
    }

    @Test
    fun `going through the tutorial counts only for a guide once skipped`() {
        assertTrue(OnboardingFlow.tutorialCompletes(OnboardingStatus.SKIPPED, skipped = false))
        assertFalse("closed or skipped: nothing changes", OnboardingFlow.tutorialCompletes(OnboardingStatus.SKIPPED, skipped = true))
        assertFalse("already done", OnboardingFlow.tutorialCompletes(OnboardingStatus.COMPLETED, skipped = false))
    }

    // ------------------------------------------------------- device storage

    @Test
    fun `a fresh install has finished nothing and has nothing pending`() {
        val store = DeviceStateStore(InMemoryPreferences())
        assertNull(store.completedOnboardingVersion)
        assertNull(store.onboardingResumeStep)
        assertFalse(store.profilePendingSync)
    }

    @Test
    fun `finishing records the version and drops the resume point`() {
        val file = InMemoryPreferences()
        DeviceStateStore(file).onboardingResumeStep = "keyboard"
        assertEquals("keyboard", DeviceStateStore(file).onboardingResumeStep)

        DeviceStateStore(file).completeOnboarding(2)
        assertEquals(2, DeviceStateStore(file).completedOnboardingVersion)
        assertNull(DeviceStateStore(file).onboardingResumeStep)

        DeviceStateStore(file).completeOnboarding(1)
        assertEquals("a version never goes down", 2, DeviceStateStore(file).completedOnboardingVersion)
    }

    @Test
    fun `the run in progress remembers whether it asks the gender until it finishes`() {
        val file = InMemoryPreferences()
        assertNull(DeviceStateStore(file).onboardingAsksGender)

        DeviceStateStore(file).apply {
            onboardingResumeStep = "keyboard"
            onboardingAsksGender = true
        }
        assertEquals(true, DeviceStateStore(file).onboardingAsksGender)

        DeviceStateStore(file).completeOnboarding(2)
        assertNull(DeviceStateStore(file).onboardingAsksGender)
        assertNull(DeviceStateStore(file).onboardingResumeStep)
    }

    @Test
    fun `a first run that comes back after a restart is not started again`() {
        val file = InMemoryPreferences()
        assertTrue(
            "a fresh first run starts",
            OnboardingFlow.startsRun(OnboardingMode.FIRST_RUN, DeviceStateStore(file).onboardingResumeStep)
        )
        // The run saved its step, then the process was killed.
        DeviceStateStore(file).onboardingResumeStep = "keyboard"
        assertFalse(OnboardingFlow.startsRun(OnboardingMode.FIRST_RUN, DeviceStateStore(file).onboardingResumeStep))

        assertTrue("the tutorial starts every time", OnboardingFlow.startsRun(OnboardingMode.TUTORIAL, "keyboard"))

        DeviceStateStore(file).completeOnboarding(2)
        assertTrue("a finished run left nothing to resume", OnboardingFlow.startsRun(OnboardingMode.FIRST_RUN, DeviceStateStore(file).onboardingResumeStep))
    }

    @Test
    fun `later is recorded as skipped, finishing it later as completed`() {
        val file = InMemoryPreferences()
        DeviceStateStore(file).apply {
            onboardingResumeStep = "welcome"
            onboardingPath = "setup_first"
        }
        assertEquals("setup_first", DeviceStateStore(file).onboardingPath)

        DeviceStateStore(file).completeOnboarding(3, skipped = true)
        val store = DeviceStateStore(file)
        assertEquals("skipped", store.onboardingStatus)
        assertEquals(3, store.completedOnboardingVersion)
        assertNull("the path goes with the run", store.onboardingPath)
        assertFalse("not shown again by itself", OnboardingFlow.needsOnboarding(store.completedOnboardingVersion ?: 0))

        DeviceStateStore(file).completeOnboarding(3)
        assertEquals("completed", DeviceStateStore(file).onboardingStatus)
    }

    @Test
    fun `an unsent profile change stays marked until it is cleared`() {
        val file = InMemoryPreferences()
        DeviceStateStore(file).profilePendingSync = true
        assertTrue(DeviceStateStore(file).profilePendingSync)
        DeviceStateStore(file).profilePendingSync = false
        assertFalse(DeviceStateStore(file).profilePendingSync)
    }
}
