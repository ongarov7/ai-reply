import SwiftUI

/// First run, and the «Как пользоваться AI Reply» guide Settings reopens.
///
/// Алғашқы іске қосу нұсқаулығы: әр қадамды өткізіп жіберуге болады, қолданба
/// жабылса, сол қадамнан жалғасады.
///
/// Which steps there are, in which order, where a first run resumes and when
/// it is done is `OnboardingFlow`'s business; this view puts the steps on a
/// navigation stack (the system back button and swipe go one step back) and
/// moves the flow. A first run is complete when the user reaches its end or
/// leaves with «Позже»; reopened, the guide records nothing but that it was
/// gone through, and closing it changes nothing. Credits and quota are never
/// touched: the tutorial runs on local examples.
///
/// The interface language follows the device's preferred language on first
/// launch (kk / ru / uz / anything else becomes English), and Settings ▸
/// Language can override it afterwards.
struct OnboardingView: View {

    @Environment(ReplyConfigurationModel.self) private var model
    @Environment(AccountModel.self) private var account
    @Environment(AppSettings.self) private var settings
    @Environment(AppRouter.self) private var router
    @Environment(\.dismiss) private var dismiss

    @State private var flow: OnboardingFlow
    /// The answer on the gender step, recorded when the user moves on.
    @State private var gender: GrammaticalGender?
    /// Who the tutorial's example is answered for; shared by its steps 2 and 3.
    @State private var recipient: RelationshipKind?
    @State private var hasStarted = false

    private let resumeStore = OnboardingResumeStore()

    init(flow: OnboardingFlow) {
        _flow = State(initialValue: flow)
    }

    var body: some View {
        NavigationStack(path: stack) {
            OnboardingWelcomeStep(
                samples: samples,
                isTutorial: flow.mode == .tutorial,
                onSetup: { flow.start(.setupFirst) },
                onTry: { flow.start(.tryFirst) },
                onLater: { leave(from: .welcome) }
            )
            .navigationDestination(for: OnboardingFlow.Step.self) { step in
                page(step)
                    .toolbar {
                        if flow.mode == .tutorial {
                            ToolbarItem(placement: .topBarTrailing) {
                                Button { dismiss() } label: { Image(systemName: "xmark") }
                                    .accessibilityLabel("common.close")
                            }
                        }
                    }
            }
        }
        .onAppear(perform: start)
        .onChange(of: flow.current) { _, step in show(step) }
        // A choice that arrives from the account after the step was built -
        // a slow `/me` - shows as selected instead of an empty question.
        .onChange(of: model.profile.grammaticalGender) { _, new in
            if gender == nil, let new, new != .unspecified { gender = new }
        }
    }

    private var stack: Binding<[OnboardingFlow.Step]> {
        Binding(get: { flow.stack }, set: { flow.setStack($0) })
    }

    private var samples: OnboardingSamples {
        OnboardingSamples(settings: settings, gender: model.profile.grammaticalGender)
    }

    // MARK: Steps

    @ViewBuilder
    private func page(_ step: OnboardingFlow.Step) -> some View {
        switch step {
        case .welcome:
            EmptyView()
        case .gender:
            OnboardingGenderStep(selection: $gender, onNext: answerGender, onSkip: skipGender)
        case .tutorialCopy:
            TutorialCopyStep(samples: samples, mentionsKeyboard: flow.comes(.permissions, after: step),
                             onNext: next, onSkip: skipTutorial)
        case .tutorialRecipient:
            TutorialRecipientStep(selection: $recipient, onNext: next, onSkip: skipTutorial)
        case .tutorialReply:
            TutorialReplyStep(samples: samples, selection: $recipient, onDone: next)
        case .permissions:
            OnboardingConnectStep(isLast: flow.isLast(step), onDone: next, onLater: { leave(from: .permissions) })
        }
    }

    // MARK: Flow

    private func start() {
        guard !hasStarted else { return }
        hasStarted = true
        if flow.mode == .firstRun, resumeStore.step != nil {
            ProductEvents.track(.onboardingResumed)
        } else {
            ProductEvents.track(.onboardingStarted, [
                "version": .int(OnboardingFlow.currentVersion),
                "trigger": .code(flow.mode == .tutorial ? "settings" : "auto")
            ])
        }
        // A resumed run keeps its gender step; going back to it shows the
        // answer already given. Skipping is not an answer to show.
        if let chosen = model.profile.grammaticalGender, chosen != .unspecified {
            gender = chosen
        }
        show(flow.current)
    }

    private func show(_ step: OnboardingFlow.Step) {
        resumeStore.save(flow)
        ProductEvents.track(.onboardingStepViewed, ["step": .code(step.rawValue)])
    }

    private func next() {
        if !flow.advance() { finish(skipped: false) }
    }

    private func answerGender() {
        if let gender, gender != model.profile.grammaticalGender {
            ProfileSync(configuration: model, account: account).choose(gender, source: .onboarding)
        }
        next()
    }

    /// Skip on the gender step is an answer too: neutral wording - unless
    /// the profile already holds a male or female answer (given before, or
    /// brought from another device), which Skip never erases.
    private func skipGender() {
        if let answer = ProfileSync.skippedAnswer(current: model.profile.grammaticalGender) {
            ProfileSync(configuration: model, account: account).choose(answer, source: .onboarding)
        }
        next()
    }

    /// «Пропустить» in the tutorial: on to what follows it, or the end.
    private func skipTutorial() {
        ProductEvents.track(.onboardingSkipped, ["step": .code(flow.current.rawValue)])
        if !flow.skipTutorial() { finish(skipped: true) }
    }

    /// «Позже» on the welcome screen ends the guide; «Настроить позже» on
    /// the keyboard step moves past it.
    private func leave(from step: OnboardingFlow.Step) {
        if flow.mode == .tutorial, step == .welcome {
            dismiss()
            return
        }
        ProductEvents.track(.onboardingSkipped, ["step": .code(step.rawValue)])
        if step == .welcome || !flow.advance() { finish(skipped: true) }
    }

    private func finish(skipped: Bool) {
        guard flow.mode == .firstRun else {
            // Gone through to the end: a guide once skipped counts as done.
            if !skipped { resumeStore.record(.completed) }
            dismiss()
            return
        }
        resumeStore.clear()
        resumeStore.record(skipped ? .skipped : .completed)
        ProductEvents.track(.onboardingCompleted, [
            "version": .int(OnboardingFlow.currentVersion),
            "skipped": .bool(skipped)
        ])
        // The server keeps an informational copy; it decides nothing, so a
        // failure is not retried.
        let account = self.account
        Task { _ = await account.updateSenderProfile(onboardingVersion: OnboardingFlow.currentVersion) }
        // A link to this guide that waited behind it has been answered.
        router.firstRunOnboardingDidFinish()
        // Last: this swaps the root over to Home.
        model.completeOnboarding()
    }
}
