import XCTest
@testable import AIReply

/// Onboarding v2: which steps a user sees, where an unfinished one resumes,
/// when it counts as done, and how a device that finished the first guide is
/// treated.
final class OnboardingFlowTests: XCTestCase {

    private func freshDefaults() -> UserDefaults {
        UserDefaults(suiteName: "OnboardingFlowTests.\(UUID().uuidString)")!
    }

    // MARK: Steps

    func testFirstRunAsksGenderOnlyWhenNeverAsked() {
        XCTAssertEqual(OnboardingFlow(mode: .firstRun, asksGender: true).steps,
                       [.welcome, .gender, .keyboard, .fullAccess, .copyReply, .practice])
        XCTAssertEqual(OnboardingFlow(mode: .firstRun, asksGender: false).steps,
                       [.welcome, .keyboard, .fullAccess, .copyReply, .practice])
    }

    /// Reopened from Settings it is a tutorial: no question, ever.
    func testTutorialNeverAsksGender() {
        let tutorial = OnboardingFlow(mode: .tutorial, asksGender: true)
        XCTAssertFalse(tutorial.steps.contains(.gender))
        XCTAssertEqual(tutorial.current, .welcome)
        XCTAssertNil(tutorial.resumeStep, "the tutorial is never resumed")
        XCTAssertFalse(tutorial.offersSkipTutorial, "it has a close button instead")
    }

    func testAdvancingToTheEndFinishes() {
        var flow = OnboardingFlow(mode: .firstRun, asksGender: true)
        var visited = [flow.current]
        while flow.advance() { visited.append(flow.current) }
        XCTAssertEqual(visited, flow.steps)
        XCTAssertTrue(flow.isLast)
        XCTAssertFalse(flow.advance(), "nothing after the last step")

        flow.goBack()
        XCTAssertEqual(flow.current, .copyReply)
    }

    func testBackStopsAtTheFirstStep() {
        var flow = OnboardingFlow(mode: .firstRun, asksGender: false)
        flow.goBack()
        XCTAssertEqual(flow.current, .welcome)
        XCTAssertTrue(flow.isFirst)
    }

    /// "Step 1 of 5" starts at the first thing the user does, not at hello.
    func testProgressDoesNotCountTheWelcomeScreen() {
        var flow = OnboardingFlow(mode: .firstRun, asksGender: true)
        XCTAssertNil(flow.progress)
        XCTAssertTrue(flow.advance())
        XCTAssertEqual(flow.progress?.position, 1)
        XCTAssertEqual(flow.progress?.count, 5)
        while flow.advance() {}
        XCTAssertEqual(flow.progress?.position, 5)
    }

    func testSkipButtons() {
        var flow = OnboardingFlow(mode: .firstRun, asksGender: true)
        XCTAssertTrue(flow.offersSkipTutorial, "welcome")
        XCTAssertFalse(flow.canSkipStep, "the welcome button only starts")

        XCTAssertTrue(flow.advance())       // gender
        XCTAssertFalse(flow.offersSkipTutorial)
        XCTAssertTrue(flow.canSkipStep)

        XCTAssertTrue(flow.advance())       // keyboard
        XCTAssertTrue(flow.offersSkipTutorial)

        while flow.advance() {}             // practice
        XCTAssertFalse(flow.canSkipStep, "the last button already finishes")
    }

    // MARK: Resume

    func testFirstRunResumesWhereItStopped() {
        let flow = OnboardingFlow(mode: .firstRun, asksGender: true, resumingAt: .copyReply)
        XCTAssertEqual(flow.current, .copyReply)
        XCTAssertEqual(flow.resumeStep, .copyReply)
    }

    /// The gender was answered before the app closed, so that step is gone:
    /// resume at the next one rather than from the top.
    func testResumingAtAStepNoLongerShownMovesForward() {
        let flow = OnboardingFlow(mode: .firstRun, asksGender: false, resumingAt: .gender)
        XCTAssertEqual(flow.current, .keyboard)
    }

    func testTutorialIgnoresASavedStep() {
        XCTAssertEqual(OnboardingFlow(mode: .tutorial, asksGender: false, resumingAt: .practice).current, .welcome)
    }

    func testResumeStoreRoundTrip() {
        let store = OnboardingResumeStore(defaults: freshDefaults())
        XCTAssertNil(store.step)
        XCTAssertNil(store.askedGender)
        store.save(OnboardingFlow(mode: .firstRun, asksGender: true, resumingAt: .fullAccess))
        XCTAssertEqual(store.step, .fullAccess)
        XCTAssertEqual(store.askedGender, true)
        store.clear()
        XCTAssertNil(store.step)
        XCTAssertNil(store.askedGender)

        store.save(OnboardingFlow(mode: .tutorial, asksGender: false, resumingAt: .practice))
        XCTAssertNil(store.step, "the tutorial is never resumed")

        let defaults = freshDefaults()
        defaults.set("someStepFromTheFuture", forKey: "onboarding.resumeStep")
        XCTAssertNil(OnboardingResumeStore(defaults: defaults).step)
    }

    /// Answering the gender question gives the profile a gender, which would
    /// leave the question out of a fresh run. A run resumed after the answer
    /// keeps the step, so its count stays the same.
    func testResumedRunKeepsItsStepCount() {
        let store = OnboardingResumeStore(defaults: freshDefaults())
        var flow = OnboardingFlow.firstRun(profileHasGender: false, resume: store)
        XCTAssertTrue(flow.advance())       // gender, answered
        XCTAssertTrue(flow.advance())       // keyboard
        store.save(flow)
        XCTAssertEqual(flow.progress?.position, 2)
        XCTAssertEqual(flow.progress?.count, 5)

        let resumed = OnboardingFlow.firstRun(profileHasGender: true, resume: store)
        XCTAssertEqual(resumed.current, .keyboard)
        XCTAssertEqual(resumed.progress?.position, 2)
        XCTAssertEqual(resumed.progress?.count, 5)

        // A run that never asked keeps not asking, whatever the profile says now.
        let other = OnboardingResumeStore(defaults: freshDefaults())
        other.save(OnboardingFlow.firstRun(profileHasGender: true, resume: other))
        XCTAssertFalse(OnboardingFlow.firstRun(profileHasGender: false, resume: other).steps.contains(.gender))

        // Nothing saved: the profile decides.
        let fresh = OnboardingResumeStore(defaults: freshDefaults())
        XCTAssertTrue(OnboardingFlow.firstRun(profileHasGender: false, resume: fresh).steps.contains(.gender))
        XCTAssertFalse(OnboardingFlow.firstRun(profileHasGender: true, resume: fresh).steps.contains(.gender))
    }

    // MARK: Versions

    /// Everyone sees v2 once: nobody yet, the first guide, but not v2 itself
    /// or anything newer.
    func testWhoNeedsTheOnboarding() {
        XCTAssertTrue(OnboardingFlow.needsOnboarding(completedVersion: 0))
        XCTAssertTrue(OnboardingFlow.needsOnboarding(completedVersion: 1))
        XCTAssertFalse(OnboardingFlow.needsOnboarding(completedVersion: OnboardingFlow.currentVersion))
        XCTAssertFalse(OnboardingFlow.needsOnboarding(completedVersion: OnboardingFlow.currentVersion + 1))
    }

    @MainActor
    func testCompletingNeverLowersTheVersion() {
        let settings = SharedSettings(defaults: freshDefaults())
        let model = ReplyConfigurationModel(store: ProfileStore(containerURL: nil, settings: settings))
        XCTAssertTrue(model.needsOnboarding)

        model.completeOnboarding()
        XCTAssertEqual(model.profile.completedOnboardingVersion, OnboardingFlow.currentVersion)
        XCTAssertFalse(model.needsOnboarding)

        model.completeOnboarding(version: 1)
        XCTAssertEqual(model.profile.completedOnboardingVersion, OnboardingFlow.currentVersion)
    }

    // MARK: Tutorial and practice stages

    func testTutorialStagesLoop() {
        var stage = CopyReplyStage.copy
        var seen: [CopyReplyStage] = []
        for _ in CopyReplyStage.allCases {
            seen.append(stage)
            stage = stage.next
        }
        XCTAssertEqual(seen, CopyReplyStage.allCases)
        XCTAssertEqual(stage, .copy, "the last stage starts over")
        XCTAssertEqual(CopyReplyStage.copy.number, 1)
    }

    func testPracticeSaysWhatToDoUntilTheReplyIsIn() {
        for stage in PracticeStage.allCases where stage != .inserted {
            XCTAssertNotNil(stage.hint, "\(stage)")
        }
        XCTAssertNil(PracticeStage.inserted.hint)
    }
}
