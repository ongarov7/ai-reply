package kz.yerek.aireply

import kz.yerek.aireply.data.settings.DeviceStateStore
import kz.yerek.aireply.ui.feature.onboarding.OnboardingFlow
import kz.yerek.aireply.ui.feature.onboarding.OnboardingMode
import kz.yerek.aireply.ui.feature.onboarding.OnboardingStep
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Which onboarding steps a run shows, where it resumes, and who sees it again.
 *
 * Онбординг қадамдары, қайта жалғастыру және нұсқа көшірісі.
 */
class OnboardingFlowTest {

    private val firstRun = OnboardingFlow(OnboardingMode.FIRST_RUN, asksGender = true)

    @Test
    fun `step ids are the stable ones shared with iOS and analytics`() {
        assertEquals(
            listOf("welcome", "gender", "keyboard", "copyReply", "practice"),
            OnboardingStep.entries.map { it.id }
        )
        assertEquals(OnboardingStep.COPY_REPLY, OnboardingStep.fromId("copyReply"))
        assertNull(OnboardingStep.fromId("fullAccess"))
    }

    @Test
    fun `a first run asks the gender only when it was never asked`() {
        assertEquals(OnboardingStep.entries, firstRun.steps)
        val answered = OnboardingFlow(OnboardingMode.FIRST_RUN, asksGender = false)
        assertFalse(OnboardingStep.GENDER in answered.steps)
    }

    @Test
    fun `the tutorial never asks the gender`() {
        val tutorial = OnboardingFlow(OnboardingMode.TUTORIAL, asksGender = true)
        assertEquals(
            listOf(OnboardingStep.WELCOME, OnboardingStep.KEYBOARD, OnboardingStep.COPY_REPLY, OnboardingStep.PRACTICE),
            tutorial.steps
        )
    }

    @Test
    fun `steps go forward and back and the last one finishes`() {
        assertEquals(OnboardingStep.GENDER, firstRun.next(OnboardingStep.WELCOME))
        assertEquals(OnboardingStep.PRACTICE, firstRun.next(OnboardingStep.COPY_REPLY))
        assertNull("after the last step the run is finished", firstRun.next(OnboardingStep.PRACTICE))
        assertTrue(firstRun.isLast(OnboardingStep.PRACTICE))

        assertNull(firstRun.previous(OnboardingStep.WELCOME))
        assertEquals(OnboardingStep.WELCOME, firstRun.previous(OnboardingStep.GENDER))

        val answered = OnboardingFlow(OnboardingMode.FIRST_RUN, asksGender = false)
        assertEquals("a skipped step is skipped both ways", OnboardingStep.WELCOME, answered.previous(OnboardingStep.KEYBOARD))
    }

    @Test
    fun `the welcome screen is not numbered`() {
        assertNull(firstRun.number(OnboardingStep.WELCOME))
        assertEquals(1, firstRun.number(OnboardingStep.GENDER))
        assertEquals(4, firstRun.number(OnboardingStep.PRACTICE))
        assertEquals(4, firstRun.numberedCount)
    }

    @Test
    fun `a first run resumes where it stopped and the tutorial starts over`() {
        assertTrue(firstRun.resumes)
        assertEquals(OnboardingStep.COPY_REPLY, firstRun.start("copyReply"))
        assertEquals(OnboardingStep.WELCOME, firstRun.start(null))
        assertEquals("an id from a newer build", OnboardingStep.WELCOME, firstRun.start("somethingNew"))

        val answered = OnboardingFlow(OnboardingMode.FIRST_RUN, asksGender = false)
        assertEquals("a step this run no longer has", OnboardingStep.WELCOME, answered.start("gender"))

        assertFalse(OnboardingFlow(OnboardingMode.TUTORIAL, asksGender = false).resumes)
    }

    @Test
    fun `a run keeps its steps and its count from start to finish`() {
        assertTrue("never asked", OnboardingFlow.asksGender(OnboardingMode.FIRST_RUN, genderAnswered = false, startedAsking = null))
        assertFalse("answered before", OnboardingFlow.asksGender(OnboardingMode.FIRST_RUN, genderAnswered = true, startedAsking = null))

        // Killed after the gender was chosen: the run comes back with the step it started with.
        val resumed = OnboardingFlow(
            OnboardingMode.FIRST_RUN,
            OnboardingFlow.asksGender(OnboardingMode.FIRST_RUN, genderAnswered = true, startedAsking = true)
        )
        assertEquals(firstRun.steps, resumed.steps)
        assertEquals(OnboardingStep.COPY_REPLY, resumed.start("copyReply"))
        assertEquals("still step 3 of 4", 3, resumed.number(OnboardingStep.COPY_REPLY))
        assertEquals(4, resumed.numberedCount)
        assertEquals("back still reaches the gender", OnboardingStep.GENDER, resumed.previous(OnboardingStep.KEYBOARD))

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
    fun `a profile that finished the old onboarding counts as version 1`() {
        assertEquals(1, OnboardingFlow.completedVersion(stored = null, legacyCompleted = true))
        assertEquals(0, OnboardingFlow.completedVersion(stored = null, legacyCompleted = false))
        assertEquals("a stored version wins", 2, OnboardingFlow.completedVersion(stored = 2, legacyCompleted = false))
    }

    @Test
    fun `everyone below the current version sees it once`() {
        assertEquals(2, OnboardingFlow.CURRENT_VERSION)
        assertTrue("new users", OnboardingFlow.needsOnboarding(0))
        assertTrue("users of the old onboarding", OnboardingFlow.needsOnboarding(1))
        assertFalse(OnboardingFlow.needsOnboarding(2))
        assertFalse("a newer build's version", OnboardingFlow.needsOnboarding(3))
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
    fun `an unsent profile change stays marked until it is cleared`() {
        val file = InMemoryPreferences()
        DeviceStateStore(file).profilePendingSync = true
        assertTrue(DeviceStateStore(file).profilePendingSync)
        DeviceStateStore(file).profilePendingSync = false
        assertFalse(DeviceStateStore(file).profilePendingSync)
    }
}
