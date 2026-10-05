import SwiftUI

/// First run, and the "How to use AI Reply" tutorial Settings reopens.
///
/// Алғашқы іске қосу нұсқаулығы: әр қадамды өткізіп жіберуге болады, қолданба
/// жабылса, сол қадамнан жалғасады.
///
/// Which steps there are, where a first run resumes and when it is done is
/// `OnboardingFlow`'s business; this view shows the current step and moves
/// the flow. Every step can be skipped. A first run is complete when the user
/// reaches Done or leaves with "Skip tutorial"; the tutorial records nothing
/// and Done just closes it.
///
/// The interface language follows the device's preferred language on first
/// launch (kk / ru / uz / anything else becomes English), and Settings ▸
/// Language can override it afterwards.
struct OnboardingView: View {

    @Environment(ReplyConfigurationModel.self) private var model
    @Environment(AccountModel.self) private var account
    @Environment(AppSettings.self) private var settings
    @Environment(\.dismiss) private var dismiss

    @State private var flow: OnboardingFlow
    /// The answer on the gender step, recorded when the user moves on.
    @State private var gender: GrammaticalGender?
    @State private var hasStarted = false

    private let resumeStore = OnboardingResumeStore()

    init(flow: OnboardingFlow) {
        _flow = State(initialValue: flow)
    }

    var body: some View {
        VStack(spacing: 0) {
            header

            ScrollView {
                step
                    .padding(DS.Spacing.l)
                    .frame(maxWidth: DS.Layout.readableWidth, alignment: .leading)
                    .frame(maxWidth: .infinity)
            }
            // A new step starts at its top, not where the last one was scrolled.
            .id(flow.current)

            footer
        }
        .background(Color.dsBackground)
        .onAppear(perform: start)
        .onChange(of: flow.current) { _, step in show(step) }
    }

    // MARK: Steps

    @ViewBuilder
    private var step: some View {
        let samples = OnboardingSamples(settings: settings, gender: model.profile.grammaticalGender)
        switch flow.current {
        case .welcome:    OnboardingWelcomeStep(samples: samples)
        case .gender:     OnboardingGenderStep(selection: $gender)
        case .keyboard:   OnboardingKeyboardStep()
        case .fullAccess: OnboardingFullAccessStep()
        case .copyReply:  OnboardingCopyReplyStep(samples: samples)
        case .practice:   OnboardingPracticeStep(samples: samples)
        }
    }

    // MARK: Header

    /// Progress on the left, the way out on the right; when the text is too
    /// large for both on one line, the way out moves under the progress.
    private var header: some View {
        ViewThatFits(in: .horizontal) {
            HStack(alignment: .center, spacing: DS.Spacing.s) {
                progress
                Spacer(minLength: 0)
                exit.fixedSize()
            }
            VStack(alignment: .leading, spacing: DS.Spacing.xxs) {
                progress
                exit
            }
        }
        .padding(.horizontal, DS.Spacing.l)
        .padding(.top, DS.Spacing.xs)
        .frame(minHeight: DS.Layout.minimumTouchTarget)
    }

    @ViewBuilder
    private var progress: some View {
        if let progress = flow.progress {
            VStack(alignment: .leading, spacing: DS.Spacing.xxs) {
                ProgressView(value: Double(progress.position), total: Double(progress.count))
                    .frame(minWidth: 120)
                Text(String(format: settings.localized("onboarding.step"), progress.position, progress.count))
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            .accessibilityElement(children: .combine)
        }
    }

    /// Close for the tutorial; "Skip tutorial" where a first run offers it.
    @ViewBuilder
    private var exit: some View {
        if flow.mode == .tutorial {
            Button { dismiss() } label: {
                Image(systemName: "xmark")
                    .font(.body.weight(.semibold))
                    .frame(width: DS.Layout.minimumTouchTarget, height: DS.Layout.minimumTouchTarget)
            }
            .accessibilityLabel("common.close")
        } else if flow.offersSkipTutorial {
            Button { finish(skipped: true) } label: {
                // Under the progress at the largest sizes, where it may take
                // two lines: both start where the progress starts.
                Text("onboarding.skipTutorial").multilineTextAlignment(.leading)
            }
            .font(.subheadline)
            .frame(minHeight: DS.Layout.minimumTouchTarget)
        }
    }

    // MARK: Footer

    /// One row while the buttons fit; at large text sizes the main button
    /// takes its own row, and at the largest the quiet ones stack too. In a
    /// row they never wrap: a word broken over two lines reads as two buttons.
    private var footer: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: DS.Spacing.s) {
                backButton.fixedSize()
                Spacer(minLength: 0)
                skipButton.fixedSize()
                primaryButton.frame(maxWidth: 180)
            }
            VStack(spacing: DS.Spacing.s) {
                primaryButton
                ViewThatFits(in: .horizontal) {
                    HStack(spacing: DS.Spacing.s) {
                        backButton.fixedSize()
                        Spacer(minLength: 0)
                        skipButton.fixedSize()
                    }
                    VStack(spacing: DS.Spacing.xs) {
                        skipButton
                        backButton
                    }
                }
            }
        }
        .padding(DS.Spacing.l)
        .background(.bar)
    }

    @ViewBuilder
    private var backButton: some View {
        if !flow.isFirst {
            Button("onboarding.back") { withAnimation { flow.goBack() } }
                .buttonStyle(.dsSecondary)
        }
    }

    @ViewBuilder
    private var skipButton: some View {
        if flow.canSkipStep {
            Button("onboarding.skip", action: skip)
                .buttonStyle(.dsSecondary)
        }
    }

    private var primaryButton: some View {
        Button(primaryTitle, action: next)
            .buttonStyle(.dsPrimary)
            // The gender step moves on with an answer, or with Skip.
            .disabled(flow.current == .gender && gender == nil)
    }

    private var primaryTitle: LocalizedStringKey {
        if flow.current == .welcome { return "onboarding.start" }
        guard flow.isLast else { return "onboarding.next" }
        return flow.mode == .tutorial ? "common.done" : "onboarding.finish"
    }

    // MARK: Flow

    private func start() {
        guard !hasStarted else { return }
        hasStarted = true
        // A first run resumed after a relaunch already started once.
        if flow.mode == .tutorial || resumeStore.step == nil {
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
        if flow.current == .gender, let gender, gender != model.profile.grammaticalGender {
            ProfileSync(configuration: model, account: account).choose(gender, source: .onboarding)
        }
        moveOn()
    }

    /// Skip on the gender step is an answer too: neutral wording.
    private func skip() {
        if flow.current == .gender {
            ProfileSync(configuration: model, account: account).choose(.unspecified, source: .onboarding)
        }
        moveOn()
    }

    private func moveOn() {
        var next = flow
        if next.advance() {
            withAnimation { flow = next }
        } else {
            finish(skipped: false)
        }
    }

    private func finish(skipped: Bool) {
        guard flow.mode == .firstRun else {
            dismiss()
            return
        }
        resumeStore.clear()
        ProductEvents.track(.onboardingCompleted, [
            "version": .int(OnboardingFlow.currentVersion),
            "skipped": .bool(skipped)
        ])
        // The server keeps an informational copy; it decides nothing, so a
        // failure is not retried.
        let account = self.account
        Task { _ = await account.updateSenderProfile(onboardingVersion: OnboardingFlow.currentVersion) }
        // Last: this swaps the root over to Home.
        model.completeOnboarding()
    }
}

/// A step's heading and the sentence under it.
struct OnboardingStepTitle: View {
    let heading: LocalizedStringKey
    let prompt: LocalizedStringKey

    init(_ heading: LocalizedStringKey, _ prompt: LocalizedStringKey) {
        self.heading = heading
        self.prompt = prompt
    }

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.xs) {
            Text(heading)
                .font(.title2.weight(.semibold))
                .accessibilityAddTraits(.isHeader)
            Text(prompt)
                .font(.body)
                .foregroundStyle(.secondary)
        }
    }
}
