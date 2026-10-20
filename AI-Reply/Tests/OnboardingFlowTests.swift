import XCTest
@testable import AIReply

/// Onboarding v3: the two ways in, which steps a user sees, where an
/// unfinished run resumes (a v2 one included), when it counts as done, and
/// that a device which finished any earlier guide is left alone.
final class OnboardingFlowTests: XCTestCase {

    private func freshDefaults() -> UserDefaults {
        UserDefaults(suiteName: "OnboardingFlowTests.\(UUID().uuidString)")!
    }

    // MARK: Steps

    /// «Настроить клавиатуру» connects first; «Сначала попробовать» tries
    /// first. The gender question, when asked, comes right before the
    /// tutorial whose replies speak for the user.
    func testBothWaysInHaveTheirOrder() {
        XCTAssertEqual(OnboardingFlow.steps(asksGender: true, path: .setupFirst),
                       [.welcome, .permissions, .gender, .tutorialCopy, .tutorialRecipient, .tutorialReply])
        XCTAssertEqual(OnboardingFlow.steps(asksGender: true, path: .tryFirst),
                       [.welcome, .gender, .tutorialCopy, .tutorialRecipient, .tutorialReply, .permissions])
        XCTAssertEqual(OnboardingFlow.steps(asksGender: false, path: .tryFirst),
                       [.welcome, .tutorialCopy, .tutorialRecipient, .tutorialReply, .permissions])
    }

    /// The event codes the server allow-lists.
    func testStepIDsAreTheServersCodes() {
        XCTAssertEqual(OnboardingFlow.Step.allCases.map(\.rawValue),
                       ["welcome", "gender", "tutorial_copy", "tutorial_recipient", "tutorial_reply", "permissions"])
    }

    func testWelcomeButtonsChooseThePath() {
        var setup = OnboardingFlow(mode: .firstRun, asksGender: true)
        XCTAssertEqual(setup.current, .welcome)
        setup.start(.setupFirst)
        XCTAssertEqual(setup.current, .permissions)
        XCTAssertEqual(setup.stack, [.permissions])

        var tryFirst = OnboardingFlow(mode: .firstRun, asksGender: true)
        tryFirst.start(.tryFirst)
        XCTAssertEqual(tryFirst.current, .gender)
        XCTAssertTrue(tryFirst.advance())
        XCTAssertEqual(tryFirst.current, .tutorialCopy)
    }

    /// Reopened from Settings it is a tutorial: no question, ever.
    func testTutorialNeverAsksGender() {
        var tutorial = OnboardingFlow(mode: .tutorial, asksGender: true)
        XCTAssertFalse(tutorial.steps.contains(.gender))
        XCTAssertEqual(tutorial.current, .welcome)
        XCTAssertNil(tutorial.resumeStep, "the tutorial is never resumed")
        tutorial.start(.tryFirst)
        XCTAssertEqual(tutorial.current, .tutorialCopy)
    }

    func testAdvancingToTheEndFinishes() {
        var flow = OnboardingFlow(mode: .firstRun, asksGender: true)
        flow.start(.tryFirst)
        var visited = [OnboardingFlow.Step.welcome, flow.current]
        while flow.advance() { visited.append(flow.current) }
        XCTAssertEqual(visited, flow.steps)
        XCTAssertTrue(flow.isLast(flow.current))
        XCTAssertFalse(flow.advance(), "nothing after the last step")

        flow.goBack()
        XCTAssertEqual(flow.current, .tutorialReply)
    }

    /// The system back button only ever shortens the stack.
    func testBackGoesOneStepAndStopsAtWelcome() {
        var flow = OnboardingFlow(mode: .firstRun, asksGender: false)
        flow.start(.tryFirst)
        XCTAssertTrue(flow.advance())
        flow.setStack([.tutorialCopy])
        XCTAssertEqual(flow.current, .tutorialCopy)
        flow.setStack([.tutorialCopy, .permissions])
        XCTAssertEqual(flow.current, .tutorialCopy, "a stack the user did not walk is ignored")
        flow.setStack([])
        XCTAssertEqual(flow.current, .welcome)
        flow.goBack()
        XCTAssertEqual(flow.current, .welcome)
    }

    /// «Пропустить» leaves the rest of the tutorial: to the keyboard when it
    /// is still ahead, otherwise the guide is done.
    func testSkippingTheTutorial() {
        var tryFirst = OnboardingFlow(mode: .firstRun, asksGender: false)
        tryFirst.start(.tryFirst)
        XCTAssertTrue(tryFirst.skipTutorial())
        XCTAssertEqual(tryFirst.current, .permissions)
        tryFirst.goBack()
        XCTAssertEqual(tryFirst.current, .tutorialCopy, "back returns to where the skip was")

        var setupFirst = OnboardingFlow(mode: .firstRun, asksGender: false)
        setupFirst.start(.setupFirst)
        XCTAssertTrue(setupFirst.advance())
        XCTAssertEqual(setupFirst.current, .tutorialCopy)
        XCTAssertFalse(setupFirst.skipTutorial(), "nothing after the tutorial: done")
    }

    /// The first tutorial step promises the keyboard only when it is ahead.
    func testWhatComesLater() {
        let tryFirst = OnboardingFlow(mode: .firstRun, asksGender: false, path: .tryFirst)
        XCTAssertTrue(tryFirst.comes(.permissions, after: .tutorialCopy))
        let setupFirst = OnboardingFlow(mode: .firstRun, asksGender: false, path: .setupFirst)
        XCTAssertFalse(setupFirst.comes(.permissions, after: .tutorialCopy))
        XCTAssertTrue(setupFirst.isLast(.tutorialReply))
        XCTAssertFalse(tryFirst.isLast(.tutorialReply))
        XCTAssertEqual(OnboardingFlow.tutorialPosition(of: .tutorialRecipient), 2)
        XCTAssertNil(OnboardingFlow.tutorialPosition(of: .permissions))
    }

    // MARK: Resume

    func testFirstRunResumesWhereItStopped() {
        let flow = OnboardingFlow(mode: .firstRun, asksGender: true, path: .tryFirst, resumingAt: .tutorialRecipient)
        XCTAssertEqual(flow.current, .tutorialRecipient)
        XCTAssertEqual(flow.stack, [.gender, .tutorialCopy, .tutorialRecipient], "back walks the earlier steps")
        XCTAssertEqual(flow.resumeStep, .tutorialRecipient)
    }

    /// The gender was answered before the app closed, so that step is gone:
    /// resume at the next one rather than from the top.
    func testResumingAtAStepNoLongerShownMovesForward() {
        XCTAssertEqual(OnboardingFlow(mode: .firstRun, asksGender: false, path: .tryFirst, resumingAt: .gender).current,
                       .tutorialCopy)
        XCTAssertEqual(OnboardingFlow(mode: .firstRun, asksGender: false, path: .setupFirst, resumingAt: .gender).stack,
                       [.permissions, .tutorialCopy])
    }

    func testTutorialIgnoresASavedStep() {
        XCTAssertEqual(OnboardingFlow(mode: .tutorial, asksGender: false, resumingAt: .tutorialReply).current, .welcome)
    }

    func testResumeStoreRoundTrip() {
        let store = OnboardingResumeStore(defaults: freshDefaults())
        XCTAssertNil(store.step)
        XCTAssertNil(store.askedGender)
        XCTAssertNil(store.path)
        store.save(OnboardingFlow(mode: .firstRun, asksGender: true, path: .setupFirst, resumingAt: .tutorialCopy))
        XCTAssertEqual(store.step, .tutorialCopy)
        XCTAssertEqual(store.askedGender, true)
        XCTAssertEqual(store.path, .setupFirst)
        store.clear()
        XCTAssertNil(store.step)
        XCTAssertNil(store.askedGender)
        XCTAssertNil(store.path)

        store.save(OnboardingFlow(mode: .tutorial, asksGender: false, resumingAt: .tutorialReply))
        XCTAssertNil(store.step, "the tutorial is never resumed")

        let defaults = freshDefaults()
        defaults.set("someStepFromTheFuture", forKey: "onboarding.resumeStep")
        XCTAssertNil(OnboardingResumeStore(defaults: defaults).step)
    }

    /// A run v2 left unfinished goes on in v3, on the keyboard-first path
    /// v2 had: its keyboard steps are «Подключи AI Reply», its tutorial and
    /// practice the new tutorial.
    func testAnUnfinishedV2RunResumes() {
        let expectations: [String: OnboardingFlow.Step] = [
            "keyboard": .permissions, "fullAccess": .permissions, "copyReply": .tutorialCopy,
            "practice": .tutorialReply, "gender": .gender, "welcome": .welcome
        ]
        for (saved, step) in expectations {
            let defaults = freshDefaults()
            defaults.set(saved, forKey: "onboarding.resumeStep")
            defaults.set(true, forKey: "onboarding.resumeAskedGender")
            let store = OnboardingResumeStore(defaults: defaults)
            let flow = OnboardingFlow.firstRun(profileHasGender: false, resume: store)
            XCTAssertEqual(flow.current, step, saved)
            if OnboardingFlow.Step.isLegacy(saved) {
                XCTAssertEqual(flow.path, .setupFirst, saved)
            }
        }
    }

    /// Answering the gender question gives the profile a gender, which would
    /// leave the question out of a fresh run. A run resumed after the answer
    /// keeps the step, so going back still finds it.
    func testResumedRunKeepsItsSteps() {
        let store = OnboardingResumeStore(defaults: freshDefaults())
        var flow = OnboardingFlow.firstRun(profileHasGender: false, resume: store)
        flow.start(.tryFirst)               // gender, answered
        XCTAssertTrue(flow.advance())       // tutorial
        store.save(flow)

        let resumed = OnboardingFlow.firstRun(profileHasGender: true, resume: store)
        XCTAssertEqual(resumed.current, .tutorialCopy)
        XCTAssertEqual(resumed.stack, [.gender, .tutorialCopy])

        // A run that never asked keeps not asking, whatever the profile says now.
        let other = OnboardingResumeStore(defaults: freshDefaults())
        other.save(OnboardingFlow.firstRun(profileHasGender: true, resume: other))
        XCTAssertFalse(OnboardingFlow.firstRun(profileHasGender: false, resume: other).steps.contains(.gender))

        // Nothing saved: the profile decides.
        let fresh = OnboardingResumeStore(defaults: freshDefaults())
        XCTAssertTrue(OnboardingFlow.firstRun(profileHasGender: false, resume: fresh).steps.contains(.gender))
        XCTAssertFalse(OnboardingFlow.firstRun(profileHasGender: true, resume: fresh).steps.contains(.gender))
    }

    // MARK: Versions and outcome

    /// Only a device that never finished a guide sees this one by itself:
    /// the first guide (1) and v2 both count.
    func testWhoNeedsTheOnboarding() {
        XCTAssertTrue(OnboardingFlow.needsOnboarding(completedVersion: 0))
        XCTAssertFalse(OnboardingFlow.needsOnboarding(completedVersion: 1))
        XCTAssertFalse(OnboardingFlow.needsOnboarding(completedVersion: 2))
        XCTAssertFalse(OnboardingFlow.needsOnboarding(completedVersion: OnboardingFlow.currentVersion))
        XCTAssertEqual(OnboardingFlow.currentVersion, 3)
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

    /// «Позже» ends the guide as skipped; a full run later counts as done.
    func testOutcomeAsTheDiagnosticsSayIt() {
        let store = OnboardingResumeStore(defaults: freshDefaults())
        XCTAssertEqual(OnboardingStatus.current(completedVersion: 0, resume: store), .notStarted)

        store.save(OnboardingFlow(mode: .firstRun, asksGender: false, path: .tryFirst, resumingAt: .tutorialCopy))
        XCTAssertEqual(OnboardingStatus.current(completedVersion: 0, resume: store), .inProgress)

        store.clear()
        store.record(.skipped)
        XCTAssertEqual(OnboardingStatus.current(completedVersion: 3, resume: store), .skipped)
        store.record(.completed)
        XCTAssertEqual(OnboardingStatus.current(completedVersion: 3, resume: store), .completed)

        let legacy = OnboardingResumeStore(defaults: freshDefaults())
        XCTAssertEqual(OnboardingStatus.current(completedVersion: 2, resume: legacy), .completed,
                       "a guide finished before outcomes were kept")
        XCTAssertEqual([OnboardingStatus.notStarted, .inProgress, .completed, .skipped].map(\.rawValue),
                       ["not_started", "in_progress", "completed", "skipped"])
    }

    // MARK: Examples

    /// The tutorial's replies are local and differ by recipient; the
    /// welcome reply says «рада» / «рад» / neither.
    @MainActor
    func testExamplesAreLocalAndFollowRecipientAndGender() {
        let settings = AppSettings(store: SharedSettings(defaults: freshDefaults()))
        settings.setLanguage(.russian)
        let female = OnboardingSamples(settings: settings, gender: .female)
        XCTAssertEqual(female.welcomeIncoming, "Привет! Давно не общались. Как ты?")
        XCTAssertTrue(female.welcomeReply.contains("Рада"))
        XCTAssertTrue(OnboardingSamples(settings: settings, gender: .male).welcomeReply.contains("Рад "))
        let neutral = OnboardingSamples(settings: settings, gender: nil).welcomeReply
        XCTAssertFalse(neutral.contains("Рад"), neutral)
        XCTAssertEqual(female.demoMessage, "Добрый день! Можно узнать подробнее?")

        let replies = RelationshipKind.builtIns.map(female.demoReply(for:))
        XCTAssertEqual(Set(replies).count, 4, "each recipient has its own reply")
        XCTAssertEqual(female.demoReply(for: .custom), female.demoReply(for: .client))
        for language in AppLanguage.allCases {
            settings.setLanguage(language)
            for kind in RelationshipKind.builtIns {
                let reply = OnboardingSamples(settings: settings, gender: nil).demoReply(for: kind)
                XCTAssertFalse(reply.hasPrefix("onboarding."), "\(kind) in \(language)")
            }
        }
    }
}
