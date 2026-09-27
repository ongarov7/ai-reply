import SwiftUI

/// First run.
///
/// A short first-run guide for enabling and using the keyboard. Profile details
/// are collected during account registration and remain editable in Settings.
///
/// Every question screen is skippable and every answer is written as the user
/// leaves it, so quitting halfway through loses nothing and a user who skips
/// everything still gets a working keyboard with the default templates.
///
/// The interface language follows the device's preferred language on first
/// launch (kk / ru / anything else becomes English) through Apple's normal
/// localization, and Settings ▸ Language can override it afterwards.
struct OnboardingView: View {

    @Environment(ReplyConfigurationModel.self) private var model
    @Environment(AppSettings.self) private var settings

    @State private var step: Step = .welcome
    @State private var role: String = ""
    @State private var offering: String = ""
    @State private var about: String = ""
    @State private var hours: WorkingHours = .default
    @State private var isDictating = false
    @State private var suggestion: VoiceConfigurationParser.Suggestion?

    enum Step: Int, CaseIterable {
        case welcome, keyboard, usage, test

        /// The welcome screen is not numbered: "Step 1 of 6" should start at
        /// the first thing the user actually does.
        var questionIndex: Int? {
            self == .welcome ? nil : rawValue
        }
        static let questionCount = 3
    }

    var body: some View {
        VStack(spacing: 0) {
            if let index = step.questionIndex {
                ProgressView(value: Double(index), total: Double(Step.questionCount))
                    .padding(.horizontal, DS.Spacing.l)
                    .padding(.top, DS.Spacing.s)
                Text(String(format: settings.localized("onboarding.step"), index, Step.questionCount))
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .padding(.top, DS.Spacing.xxs)
            }

            ScrollView {
                VStack(alignment: .leading, spacing: DS.Spacing.l) {
                    content
                }
                .padding(DS.Spacing.l)
                .frame(maxWidth: DS.Layout.readableWidth, alignment: .leading)
                .frame(maxWidth: .infinity)
            }

            footer
        }
        .background(Color.dsBackground)
        .onAppear(perform: loadExisting)
        .sheet(isPresented: $isDictating) {
            DictationSheet(language: settings.effectiveLanguage) { transcript in
                acceptDictation(transcript)
            }
        }
        .sheet(item: $suggestion) { value in
            VoiceSuggestionSheet(suggestion: value, existingHours: hours) { confirmed in
                apply(confirmed)
            }
        }
    }

    // MARK: Steps

    @ViewBuilder
    private var content: some View {
        switch step {
        case .welcome:  welcomeStep
        case .keyboard: keyboardStep
        case .usage:    usageStep
        case .test:     testStep
        }
    }

    private var welcomeStep: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.m) {
            AppMarkView(size: 72)
            Text("onboarding.welcome.title").font(.largeTitle.weight(.semibold))
            Text("onboarding.welcome.body").font(.body).foregroundStyle(.secondary)

            VStack(alignment: .leading, spacing: DS.Spacing.s) {
                FeatureRow(symbol: "doc.on.clipboard", text: "onboarding.welcome.point.copy")
                FeatureRow(symbol: "person.2", text: "onboarding.welcome.point.templates")
                FeatureRow(symbol: "square.and.pencil", text: "onboarding.welcome.point.edit")
            }
            .dsCard()
        }
    }

    /// Normal questions, not a prompt editor. Nothing here asks the user to
    /// write an instruction for a model; the app turns these answers into
    /// context itself.
    private var profileStep: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.m) {
            title("onboarding.profile.title", "onboarding.profile.prompt")

            VStack(alignment: .leading, spacing: DS.Spacing.xs) {
                FieldLabel("profile.role")
                TextField("profile.role.placeholder", text: $role, axis: .vertical)
                    .textFieldStyle(.plain)
                    .padding(DS.Spacing.s)
                    .background(
                        RoundedRectangle(cornerRadius: DS.Radius.medium, style: .continuous)
                            .fill(Color.dsSurface)
                    )
                    .onChange(of: role) { _, value in
                        role = String(value.prefix(UserProfile.maximumRoleCharacters))
                    }

                FieldLabel("profile.offering")
                TextField("profile.offering.placeholder", text: $offering, axis: .vertical)
                    .textFieldStyle(.plain)
                    .padding(DS.Spacing.s)
                    .background(
                        RoundedRectangle(cornerRadius: DS.Radius.medium, style: .continuous)
                            .fill(Color.dsSurface)
                    )
                    .onChange(of: offering) { _, value in
                        offering = String(value.prefix(BusinessContext.maximumOfferingCharacters))
                    }

                FieldLabel("profile.about")
                ZStack(alignment: .topLeading) {
                    if about.isEmpty {
                        Text("onboarding.profile.placeholder")
                            .foregroundStyle(.tertiary)
                            .padding(DS.Spacing.s)
                            .allowsHitTesting(false)
                    }
                    TextEditor(text: $about)
                        .frame(minHeight: 110)
                        .scrollContentBackground(.hidden)
                        .padding(DS.Spacing.xxs)
                        .onChange(of: about) { _, value in
                            if value.unicodeScalars.count > UserProfile.maximumDescriptionCharacters {
                                about = String(value.prefix(UserProfile.maximumDescriptionCharacters))
                            }
                        }
                }
                .background(
                    RoundedRectangle(cornerRadius: DS.Radius.medium, style: .continuous)
                        .fill(Color.dsSurface)
                )
            }

            HStack {
                Text(
                    String(
                        format: settings.localized("profile.counter"),
                        about.unicodeScalars.count,
                        UserProfile.maximumDescriptionCharacters
                    )
                )
                .font(.caption.monospacedDigit())
                .foregroundStyle(.secondary)
                Spacer()
                Button { isDictating = true } label: {
                    Label("profile.dictate", systemImage: "mic.fill")
                }
                .buttonStyle(.dsSecondary)
            }
        }
    }

    private var hoursStep: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.m) {
            title("onboarding.hours.title", "onboarding.hours.prompt")

            Toggle("hours.enable", isOn: $hours.isEnabled)
                .padding(DS.Spacing.m)
                .background(
                    RoundedRectangle(cornerRadius: DS.Radius.medium, style: .continuous)
                        .fill(Color.dsSurface)
                )

            if hours.isEnabled {
                QuickHoursEditor(hours: $hours)
            }

            Text("onboarding.hours.footer").font(.footnote).foregroundStyle(.secondary)
        }
    }

    private var keyboardStep: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.m) {
            title("onboarding.keyboard.title", "onboarding.keyboard.prompt")
            KeyboardSetupView(showsTitle: false)
                .frame(height: 460)
        }
    }

    private var usageStep: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.m) {
            title("onboarding.usage.title", "onboarding.usage.prompt")

            VStack(alignment: .leading, spacing: DS.Spacing.s) {
                DSStepRow(index: 1, text: "onboarding.usage.step.copy")
                DSStepRow(index: 2, text: "onboarding.usage.step.open")
                DSStepRow(index: 3, text: "onboarding.usage.step.template")
                DSStepRow(index: 4, text: "onboarding.usage.step.instruction")
                DSStepRow(index: 5, text: "onboarding.usage.step.insert")
            }
            .dsCard()

            DSSection(title: "setup.paste.title") {
                VStack(alignment: .leading, spacing: DS.Spacing.xs) {
                    Text("setup.paste.body").font(.body)
                    Text("setup.paste.footer").font(.footnote).foregroundStyle(.secondary)
                }
                .dsCard()
            }
        }
    }

    private var testStep: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.m) {
            AppMarkView(size: 64)
            Text("onboarding.done.title").font(.largeTitle.weight(.semibold))
            Text("onboarding.done.body").font(.body).foregroundStyle(.secondary)
            KeyboardTestField()
        }
    }

    private func title(_ heading: LocalizedStringKey, _ prompt: LocalizedStringKey) -> some View {
        VStack(alignment: .leading, spacing: DS.Spacing.xs) {
            Text(heading).font(.title2.weight(.semibold))
            Text(prompt).font(.body).foregroundStyle(.secondary)
        }
    }

    // MARK: Footer

    private var footer: some View {
        HStack(spacing: DS.Spacing.s) {
            if step != .welcome {
                Button("onboarding.back") { goBack() }
                    .buttonStyle(.dsSecondary)
            }
            Spacer(minLength: 0)
            if step != .welcome && step != .test {
                Button("onboarding.skip") { advance() }
                    .buttonStyle(.dsSecondary)
            }
            Button(primaryTitle) { advance() }
                .buttonStyle(.dsPrimary)
                .frame(maxWidth: 180)
        }
        .padding(DS.Spacing.l)
        .background(.bar)
    }

    private var primaryTitle: LocalizedStringKey {
        switch step {
        case .welcome: return "onboarding.start"
        case .test:    return "onboarding.finish"
        default:       return "onboarding.next"
        }
    }

    // MARK: Flow

    private func loadExisting() {
        let profile = model.profile
        role = profile.role
        offering = profile.business.offering
        about = profile.descriptionText
        hours = profile.workingHours
    }

    private func goBack() {
        guard let previous = Step(rawValue: step.rawValue - 1) else { return }
        withAnimation { step = previous }
    }

    private func advance() {
        persistCurrentStep()
        guard let next = Step(rawValue: step.rawValue + 1) else {
            model.completeOnboarding()
            return
        }
        withAnimation { step = next }
    }

    /// Each answer is written as the user leaves its screen, so quitting the
    /// app halfway through never loses what was already answered.
    private func persistCurrentStep() {
        switch step {
        case .welcome, .keyboard, .usage, .test: break
        }
    }

    // MARK: Voice

    /// The spoken sentence always lands in the profile text verbatim. Anything
    /// the app additionally RECOGNISED in it is offered separately, and only
    /// applied if the user confirms it.
    private func acceptDictation(_ transcript: String) {
        let addition = transcript.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !addition.isEmpty else { return }

        let separator = about.isEmpty ? "" : " "
        about = String((about + separator + addition).prefix(UserProfile.maximumDescriptionCharacters))

        let parsed = VoiceConfigurationParser.parse(addition, language: settings.effectiveLanguage)
        if !parsed.isEmpty { suggestion = parsed }
    }

    private func apply(_ confirmed: VoiceConfigurationParser.Suggestion) {
        if let updated = confirmed.workingHours(basedOn: hours) {
            hours = updated
            model.updateProfile { $0.workingHours = updated }
        }
        guard !confirmed.rules.isEmpty else { return }
        model.updateProfile { profile in
            for rule in confirmed.rules { profile.business.addRule(rule) }
        }
    }
}

// MARK: - Pieces

/// `sheet(item:)` needs identity, and a suggestion is a value with no natural
/// id. Conforming here rather than on the parser keeps that presentation detail
/// out of the shared logic.
extension VoiceConfigurationParser.Suggestion: Identifiable {
    public var id: String {
        "\(start?.minutes ?? -1)-\(end?.minutes ?? -1)-\(rules.count)-\(weekdays?.count ?? 0)"
    }
}

private struct FeatureRow: View {
    let symbol: String
    let text: LocalizedStringKey

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: DS.Spacing.s) {
            Image(systemName: symbol)
                .foregroundStyle(Color.accentColor)
                .frame(width: 22)
            Text(text).font(.subheadline)
        }
    }
}

struct FieldLabel: View {
    let key: LocalizedStringKey

    init(_ key: LocalizedStringKey) { self.key = key }

    var body: some View {
        Text(key)
            .font(.subheadline.weight(.medium))
            .foregroundStyle(.secondary)
            .padding(.top, DS.Spacing.xs)
    }
}

/// Start and end time plus a weekday row, for the common case. The full
/// per-day editor lives in Settings ▸ Working hours.
struct QuickHoursEditor: View {

    @Binding var hours: WorkingHours

    private let weekdayOrder = [2, 3, 4, 5, 6, 7, 1]

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.m) {
            HStack {
                Text("hours.from").foregroundStyle(.secondary)
                Spacer()
                TimeOfDayPicker(time: startBinding)
                Text("hours.to").foregroundStyle(.secondary)
                TimeOfDayPicker(time: endBinding)
            }

            HStack(spacing: DS.Spacing.xxs) {
                ForEach(weekdayOrder, id: \.self) { weekday in
                    Button {
                        toggle(weekday)
                    } label: {
                        Text(verbatim: symbol(weekday))
                            .font(.footnote.weight(.medium))
                            .frame(maxWidth: .infinity, minHeight: 34)
                            .background(
                                RoundedRectangle(cornerRadius: DS.Radius.small, style: .continuous)
                                    .fill(isEnabled(weekday) ? Color.accentColor : Color.dsBackground)
                            )
                            .foregroundStyle(isEnabled(weekday) ? Color.white : Color.primary)
                    }
                    .buttonStyle(.plain)
                }
            }
        }
        .dsCard()
    }

    private func symbol(_ weekday: Int) -> String {
        String(Calendar.current.shortStandaloneWeekdaySymbols[weekday - 1].prefix(2))
    }

    private func isEnabled(_ weekday: Int) -> Bool {
        hours.schedule(for: weekday)?.isEnabled ?? false
    }

    private func toggle(_ weekday: Int) {
        guard let index = hours.days.firstIndex(where: { $0.weekday == weekday }) else { return }
        hours.days[index].isEnabled.toggle()
    }

    /// Editing one time applies it to every enabled day, which is what a
    /// "from / to" control implies. Per-day differences stay possible in the
    /// full editor.
    private var startBinding: Binding<TimeOfDay> {
        Binding(
            get: { hours.days.first(where: \.isEnabled)?.start ?? TimeOfDay(hour: 10, minute: 0) },
            set: { value in
                for index in hours.days.indices where hours.days[index].isEnabled {
                    hours.days[index].start = value
                }
            }
        )
    }

    private var endBinding: Binding<TimeOfDay> {
        Binding(
            get: { hours.days.first(where: \.isEnabled)?.end ?? TimeOfDay(hour: 18, minute: 0) },
            set: { value in
                for index in hours.days.indices where hours.days[index].isEnabled {
                    hours.days[index].end = value
                }
            }
        )
    }
}

/// A field to try the keyboard in without leaving the app.
struct KeyboardTestField: View {

    @State private var text = ""
    @FocusState private var isFocused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            Text("onboarding.test.prompt").font(.subheadline).foregroundStyle(.secondary)
            TextField("onboarding.test.placeholder", text: $text, axis: .vertical)
                .textFieldStyle(.plain)
                .lineLimit(2...4)
                .padding(DS.Spacing.s)
                .background(
                    RoundedRectangle(cornerRadius: DS.Radius.medium, style: .continuous)
                        .fill(Color.dsBackground)
                )
                .focused($isFocused)
            Text("onboarding.test.hint").font(.footnote).foregroundStyle(.secondary)
        }
        .dsCard()
    }
}

/// Hour and minute wheels, kept as a `TimeOfDay` rather than a `Date` so the
/// value stays a wall-clock fact (see `TimeOfDay`).
struct TimeOfDayPicker: View {

    @Binding var time: TimeOfDay

    var body: some View {
        DatePicker(
            "",
            selection: Binding(
                get: {
                    Calendar.current.date(
                        bySettingHour: time.hour, minute: time.minute, second: 0, of: Date()
                    ) ?? Date()
                },
                set: { date in
                    let parts = Calendar.current.dateComponents([.hour, .minute], from: date)
                    time = TimeOfDay(hour: parts.hour ?? 0, minute: parts.minute ?? 0)
                }
            ),
            displayedComponents: .hourAndMinute
        )
        .labelsHidden()
    }
}

/// The steps for adding the keyboard in iOS Settings.
struct KeyboardSetupSteps: View {
    private let steps: [LocalizedStringKey] = [
        "home.setup.step.settings",
        "home.setup.step.general",
        "home.setup.step.keyboard",
        "home.setup.step.keyboards",
        "home.setup.step.addNew",
        "home.setup.step.choose",
        "home.setup.step.fullAccess"
    ]

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Spacing.s) {
            ForEach(Array(steps.enumerated()), id: \.offset) { index, step in
                DSStepRow(index: index + 1, text: step)
            }
        }
        .dsCard()
    }
}
