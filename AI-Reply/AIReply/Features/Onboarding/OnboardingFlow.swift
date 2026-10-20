import Foundation

/// Which onboarding steps a user sees, in which order, where an unfinished
/// one resumes and when it is done.
///
/// Онбординг логикасы: қай қадам, қай ретпен, қайдан жалғасады, қашан бітеді.
/// UI жоқ, сондықтан толық тестіленеді.
///
/// Version 3: a welcome screen with two ways in - «Настроить клавиатуру»
/// (connect the keyboard, then the tutorial) or «Сначала попробовать» (the
/// tutorial, then the keyboard) - a three-step tutorial on local examples,
/// and «Подключи AI Reply». The gender question, when the profile has none,
/// comes right before the tutorial, whose sample replies speak for the user.
/// Every step can be left; «Позже» on the welcome screen ends the guide.
struct OnboardingFlow: Equatable {

    /// What a finished run records. A device that finished ANY version -
    /// the first guide (1), v2 or this one - never sees the guide by itself
    /// again; Settings reopens it.
    static let currentVersion = 3

    static func needsOnboarding(completedVersion: Int) -> Bool {
        completedVersion < 1
    }

    /// Stable ids: saved for resuming and reported as the `step` of product
    /// events (the server allow-lists them).
    enum Step: String, CaseIterable, Hashable, Sendable {
        case welcome
        case gender
        case tutorialCopy = "tutorial_copy"
        case tutorialRecipient = "tutorial_recipient"
        case tutorialReply = "tutorial_reply"
        /// «Подключи AI Reply».
        case permissions

        static let tutorial: [Step] = [.tutorialCopy, .tutorialRecipient, .tutorialReply]

        var isTutorial: Bool { Self.tutorial.contains(self) }

        /// A step as saved, including the ids onboarding v2 saved: a run
        /// left on its keyboard steps resumes on «Подключи AI Reply», one
        /// left on its tutorial or practice on the tutorial.
        init?(saved raw: String) {
            if let step = Step(rawValue: raw) {
                self = step
                return
            }
            switch raw {
            case "keyboard", "fullAccess": self = .permissions
            case "copyReply":              self = .tutorialCopy
            case "practice":               self = .tutorialReply
            default:                       return nil
            }
        }

        /// v2's ids, whose run had the keyboard first.
        static func isLegacy(_ raw: String) -> Bool {
            ["keyboard", "fullAccess", "copyReply", "practice"].contains(raw)
        }
    }

    /// The welcome screen's two ways in.
    enum Path: String, Sendable {
        /// «Настроить клавиатуру»: the keyboard, then the tutorial.
        case setupFirst = "setup_first"
        /// «Сначала попробовать»: the tutorial, then the keyboard.
        case tryFirst = "try_first"
    }

    enum Mode: Equatable {
        /// Shown by itself after sign-in until it is finished or skipped.
        case firstRun
        /// Reopened from Settings or `aireply://onboarding`: no questions,
        /// and closing it changes nothing.
        case tutorial
    }

    let mode: Mode
    /// The gender question is among the steps: a first run whose profile
    /// had none when the run began.
    let asksGender: Bool
    private(set) var path: Path
    /// The steps on the navigation stack above the welcome screen; the last
    /// one is on screen.
    private(set) var stack: [Step] = []

    /// - Parameters:
    ///   - asksGender: whether the profile has never had a gender. Asked
    ///     once, and never in the tutorial.
    ///   - saved: where a first run was when the app last closed. A step
    ///     that is no longer shown resumes at the next one that is.
    init(mode: Mode, asksGender: Bool, path: Path = .tryFirst, resumingAt saved: Step? = nil) {
        self.mode = mode
        self.asksGender = mode == .firstRun && asksGender
        self.path = path
        guard mode == .firstRun, let saved, saved != .welcome else { return }
        let steps = self.steps
        let everything = Self.steps(asksGender: true, path: path)
        guard let from = everything.firstIndex(of: saved),
              let resumed = everything[from...].first(where: steps.contains),
              let position = steps.firstIndex(of: resumed) else { return }
        stack = Array(steps[1...position])
    }

    /// The first run as it stands now: a fresh one asks the gender question
    /// when the profile has none, and an unfinished one resumes where it
    /// stopped, on its path and with the steps it started with - answering
    /// the question does not take its step away.
    static func firstRun(profileHasGender: Bool, resume: OnboardingResumeStore = OnboardingResumeStore()) -> OnboardingFlow {
        OnboardingFlow(
            mode: .firstRun,
            asksGender: resume.askedGender ?? !profileHasGender,
            path: resume.path ?? .tryFirst,
            resumingAt: resume.step
        )
    }

    static func steps(asksGender: Bool, path: Path) -> [Step] {
        let tutorial = (asksGender ? [Step.gender] : []) + Step.tutorial
        switch path {
        case .setupFirst: return [.welcome, .permissions] + tutorial
        case .tryFirst:   return [.welcome] + tutorial + [.permissions]
        }
    }

    var steps: [Step] { Self.steps(asksGender: asksGender, path: path) }
    var current: Step { stack.last ?? .welcome }

    /// Nothing comes after `step`: its main button finishes.
    func isLast(_ step: Step) -> Bool { steps.last == step }

    /// Whether `later` is still ahead of `step` (the tutorial's first step
    /// promises the keyboard only when it is).
    func comes(_ later: Step, after step: Step) -> Bool {
        guard let index = steps.firstIndex(of: step) else { return false }
        return steps[(index + 1)...].contains(later)
    }

    /// "1 из 3" for the tutorial's steps.
    static func tutorialPosition(of step: Step) -> Int? {
        Step.tutorial.firstIndex(of: step).map { $0 + 1 }
    }

    /// What to save so a relaunch resumes here. Nothing for the tutorial,
    /// which starts from the top every time it is opened.
    var resumeStep: Step? { mode == .firstRun ? current : nil }

    // MARK: Moving

    /// One of the welcome screen's buttons: the path, and its first step.
    mutating func start(_ path: Path) {
        self.path = path
        stack = [steps[1]]
    }

    /// Moves to the next step. False when there is none: the flow is done.
    mutating func advance() -> Bool {
        guard let index = steps.firstIndex(of: current), index + 1 < steps.count else { return false }
        stack.append(steps[index + 1])
        return true
    }

    /// «Пропустить» in the tutorial: past its remaining steps. False when
    /// nothing follows them: the flow is done.
    mutating func skipTutorial() -> Bool {
        guard let index = steps.firstIndex(of: current),
              let next = steps[(index + 1)...].first(where: { !$0.isTutorial }) else { return false }
        stack.append(next)
        return true
    }

    mutating func goBack() {
        if !stack.isEmpty { stack.removeLast() }
    }

    /// The navigation stack after the system's back button or swipe: only
    /// ever a shorter stack, so anything else is ignored.
    mutating func setStack(_ next: [Step]) {
        guard next.count <= stack.count, Array(stack.prefix(next.count)) == next else { return }
        stack = next
    }
}

/// How a device's onboarding stands, as the diagnostics API spells it
/// (contract §3.4).
enum OnboardingStatus: String, Sendable {
    case notStarted = "not_started"
    case inProgress = "in_progress"
    case completed
    case skipped

    /// A device that finished an earlier version, before outcomes were
    /// kept, counts as completed.
    static func current(completedVersion: Int, resume: OnboardingResumeStore = OnboardingResumeStore()) -> OnboardingStatus {
        if completedVersion > 0 { return resume.outcome == .skipped ? .skipped : .completed }
        return resume.step == nil ? .notStarted : .inProgress
    }
}

/// Where an unfinished first-run onboarding resumes after the app was
/// closed, and how the last one ended. Device-local.
struct OnboardingResumeStore {

    enum Outcome: String {
        /// Gone through to the end.
        case completed
        /// Left early: «Позже», or a skip that ended it.
        case skipped
    }

    var defaults: UserDefaults = .standard

    private static let stepKey = "onboarding.resumeStep"
    private static let askedGenderKey = "onboarding.resumeAskedGender"
    private static let pathKey = "onboarding.resumePath"
    private static let outcomeKey = "onboarding.outcome"

    var step: OnboardingFlow.Step? {
        defaults.string(forKey: Self.stepKey).flatMap(OnboardingFlow.Step.init(saved:))
    }

    /// The saved run's path. A run saved by v2 had the keyboard first.
    var path: OnboardingFlow.Path? {
        if let raw = defaults.string(forKey: Self.pathKey) { return OnboardingFlow.Path(rawValue: raw) }
        guard let step = defaults.string(forKey: Self.stepKey), OnboardingFlow.Step.isLegacy(step) else { return nil }
        return .setupFirst
    }

    /// Whether the unfinished run has the gender question among its steps.
    /// nil when nothing was saved.
    var askedGender: Bool? {
        defaults.object(forKey: Self.askedGenderKey) as? Bool
    }

    var outcome: Outcome? {
        defaults.string(forKey: Self.outcomeKey).flatMap(Outcome.init(rawValue:))
    }

    /// Remembers the current step of a first run. The tutorial is never
    /// resumed, so it saves nothing.
    func save(_ flow: OnboardingFlow) {
        guard let step = flow.resumeStep else { return }
        defaults.set(step.rawValue, forKey: Self.stepKey)
        defaults.set(flow.asksGender, forKey: Self.askedGenderKey)
        defaults.set(flow.path.rawValue, forKey: Self.pathKey)
    }

    func record(_ outcome: Outcome) {
        defaults.set(outcome.rawValue, forKey: Self.outcomeKey)
    }

    func clear() {
        for key in [Self.stepKey, Self.askedGenderKey, Self.pathKey] {
            defaults.removeObject(forKey: key)
        }
    }
}
