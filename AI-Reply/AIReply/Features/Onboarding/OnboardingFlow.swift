import Foundation

/// Which onboarding steps a user sees, where an unfinished one resumes and
/// when it is done.
///
/// Онбординг логикасы: қай қадам көрсетіледі, қайдан жалғасады, қашан бітеді.
/// UI жоқ, сондықтан толық тестіленеді.
///
/// Every step can be skipped, and the whole guide can be left from the welcome
/// and keyboard steps; reaching the end and leaving early both complete it.
struct OnboardingFlow: Equatable {

    /// Raised when the guide changes enough that every device should see it
    /// once more. A device that finished the first guide has version 1.
    static let currentVersion = 2

    /// Stable ids: saved for resuming and reported in analytics.
    enum Step: String, CaseIterable, Sendable {
        case welcome
        case gender
        case keyboard
        case fullAccess
        case copyReply
        case practice

        fileprivate var order: Int { Self.allCases.firstIndex(of: self) ?? 0 }
    }

    enum Mode: Equatable {
        /// Shown by itself after sign-in until it is finished or skipped.
        case firstRun
        /// Reopened from Settings: no questions, and nothing is reset or
        /// recorded - Done just closes it.
        case tutorial
    }

    let mode: Mode
    let steps: [Step]
    private(set) var current: Step

    /// - Parameters:
    ///   - asksGender: whether the profile has never had a gender. The
    ///     question is asked once, and never in the tutorial.
    ///   - saved: where a first run was when the app last closed. A step
    ///     that is no longer shown resumes at the next one that is.
    init(mode: Mode, asksGender: Bool, resumingAt saved: Step? = nil) {
        self.mode = mode
        let steps = Step.allCases.filter { $0 != .gender || (mode == .firstRun && asksGender) }
        self.steps = steps
        let resumable = mode == .firstRun ? saved : nil
        current = resumable.flatMap { saved in steps.first { $0.order >= saved.order } } ?? steps[0]
    }

    /// The first run as it stands now: a fresh one asks the gender question
    /// when the profile has none, and an unfinished one resumes where it
    /// stopped with the steps it started with - so answering the question
    /// does not take its step away and "Step 3 of 5" never becomes "of 4".
    static func firstRun(profileHasGender: Bool, resume: OnboardingResumeStore = OnboardingResumeStore()) -> OnboardingFlow {
        OnboardingFlow(
            mode: .firstRun,
            asksGender: resume.askedGender ?? !profileHasGender,
            resumingAt: resume.step
        )
    }

    static func needsOnboarding(completedVersion: Int) -> Bool {
        completedVersion < currentVersion
    }

    var index: Int { steps.firstIndex(of: current) ?? 0 }
    var isFirst: Bool { index == 0 }
    var isLast: Bool { index == steps.count - 1 }

    /// "Step 2 of 5". The welcome screen is not counted: numbering starts at
    /// the first thing the user actually does.
    var progress: (position: Int, count: Int)? {
        guard current != .welcome else { return nil }
        let counted = steps.filter { $0 != .welcome }
        guard let position = counted.firstIndex(of: current) else { return nil }
        return (position + 1, counted.count)
    }

    /// The small "Skip tutorial" button: on the first screens of a first run,
    /// where someone who has done this before decides.
    var offersSkipTutorial: Bool {
        mode == .firstRun && (current == .welcome || current == .keyboard)
    }

    /// Skip moves past one step. Not on the welcome screen, whose button only
    /// starts, and not on the last step, whose button already finishes.
    var canSkipStep: Bool { current != .welcome && !isLast }

    /// What to save so a relaunch resumes here. Nothing for the tutorial,
    /// which starts from the top every time it is opened.
    var resumeStep: Step? { mode == .firstRun ? current : nil }

    /// Moves to the next step. False when there is none: the flow is done.
    mutating func advance() -> Bool {
        guard !isLast else { return false }
        current = steps[index + 1]
        return true
    }

    mutating func goBack() {
        guard !isFirst else { return }
        current = steps[index - 1]
    }
}

/// Where an unfinished first-run onboarding resumes after the app was closed.
/// Device-local, and cleared once the onboarding completes.
struct OnboardingResumeStore {

    var defaults: UserDefaults = .standard

    private static let stepKey = "onboarding.resumeStep"
    private static let askedGenderKey = "onboarding.resumeAskedGender"

    var step: OnboardingFlow.Step? {
        defaults.string(forKey: Self.stepKey).flatMap(OnboardingFlow.Step.init(rawValue:))
    }

    /// Whether the unfinished run has the gender question among its steps.
    /// nil when nothing was saved.
    var askedGender: Bool? {
        defaults.object(forKey: Self.askedGenderKey) as? Bool
    }

    /// Remembers the current step of a first run. The tutorial is never
    /// resumed, so it saves nothing.
    func save(_ flow: OnboardingFlow) {
        guard let step = flow.resumeStep else { return }
        defaults.set(step.rawValue, forKey: Self.stepKey)
        defaults.set(flow.steps.contains(.gender), forKey: Self.askedGenderKey)
    }

    func clear() {
        defaults.removeObject(forKey: Self.stepKey)
        defaults.removeObject(forKey: Self.askedGenderKey)
    }
}
