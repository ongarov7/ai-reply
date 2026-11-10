import SwiftUI

@main
struct AIReplyWatchApp: App {

    @State private var model = WatchVoiceModel()
    @Environment(\.scenePhase) private var scenePhase

    var body: some Scene {
        WindowGroup {
            WatchRootView()
                .environment(model)
                .environment(\.locale, model.locale)
                .onChange(of: scenePhase, initial: true) { _, phase in
                    model.scenePhaseChanged(phase)
                }
        }
    }
}

/// One screen per stage: this is an app for a glance at the wrist. While
/// nothing is being recorded or sent, the page: home, a persona, a new message.
struct WatchRootView: View {

    @Environment(WatchVoiceModel.self) private var model

    var body: some View {
        NavigationStack {
            content
                .navigationTitle(title)
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    if model.stage == .ready, model.page != .home {
                        ToolbarItem(placement: .topBarLeading) {
                            Button(action: model.goHome) {
                                Image(systemName: "chevron.backward")
                            }
                            .accessibilityLabel(Text("watch.back"))
                        }
                    }
                }
        }
    }

    private var title: Text {
        guard model.stage == .ready else { return Text(verbatim: "AI Reply") }
        switch model.page {
        case .home:                return Text(verbatim: "AI Reply")
        case .persona(let choice): return Text(verbatim: choice.name)
        case .newMessage:          return Text("watch.mode.new")
        }
    }

    @ViewBuilder
    private var content: some View {
        switch model.stage {
        case .ready:
            switch model.page {
            case .home:                HomeScreen()
            case .persona(let choice): PersonaScreen(persona: choice)
            case .newMessage:          NewMessageScreen()
            }
        case .voiceConsent:
            VoiceConsentScreen()
        case .listening:
            ListeningScreen()
        case .processing:
            ProgressScreen(title: "watch.processing", cancels: true)
        case .review:
            ReviewScreen()
        case .sending:
            ProgressScreen(title: "watch.sending", cancels: true)
        case .queuedForLater:
            MessageScreen(symbol: "clock.arrow.circlepath", title: "watch.queued", primary: nil, offersCancel: true)
        case .waitingForKeyboard:
            MessageScreen(symbol: "keyboard", title: "watch.waitingKeyboard", primary: nil, offersCancel: true)
        case .sentToPhone:
            ProgressScreen(title: "watch.sent", cancels: true)
        case .generating:
            ProgressScreen(title: "watch.generating", cancels: true)
        case .completed(let inserted):
            CompletedScreen(inserted: inserted)
        case .failed(let failure, let limit):
            FailureScreen(failure: failure, limit: limit)
        }
    }
}

// MARK: - Home

/// The personas, as on the keyboard's row, then a new message and settings.
private struct HomeScreen: View {

    @Environment(WatchVoiceModel.self) private var model

    var body: some View {
        List {
            Section {
                StatusRow()
            }

            Section {
                ForEach(model.personas) { persona in
                    Button {
                        model.openPersona(persona)
                    } label: {
                        PersonaRow(persona: persona)
                    }
                }
            } header: {
                Text("watch.home.reply")
            } footer: {
                if !model.personasAreFromPhone {
                    Text("watch.home.personasHint")
                }
            }

            Section {
                Button(action: model.openNewMessage) {
                    Label("watch.mode.new", systemImage: "square.and.pencil")
                }
            }

            Section {
                NavigationLink {
                    SettingsScreen()
                } label: {
                    Label("watch.settings", systemImage: "gearshape")
                }
                if model.recent != nil {
                    NavigationLink {
                        RecentResultScreen()
                    } label: {
                        Label("watch.recent", systemImage: "text.bubble")
                    }
                }
            }
        }
    }
}

private struct StatusRow: View {

    @Environment(WatchVoiceModel.self) private var model

    var body: some View {
        Label {
            Text(text)
                .font(.footnote)
        } icon: {
            Image(systemName: symbol)
                .foregroundStyle(model.isReachable ? Color.green : Color.orange)
        }
        .accessibilityElement(children: .combine)
    }

    private var text: LocalizedStringKey {
        guard model.isReachable else { return "watch.connection.unavailable" }
        return model.phone?.keyboardSessionID != nil ? "watch.status.keyboard" : "watch.connection.connected"
    }

    private var symbol: String {
        guard model.isReachable else { return "iphone.slash" }
        return model.phone?.keyboardSessionID != nil ? "keyboard" : "iphone"
    }
}

private struct PersonaRow: View {

    let persona: WatchTemplateChoice

    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: persona.symbol ?? "person.crop.circle")
                .font(.footnote.weight(.semibold))
                .foregroundStyle(.white)
                .frame(width: 30, height: 30)
                .background(Circle().fill(persona.tint))
                .accessibilityHidden(true)
            Text(verbatim: persona.name)
                .font(.headline)
                .lineLimit(2)
                .minimumScaleFactor(0.8)
            Spacer(minLength: 0)
            Image(systemName: "chevron.forward")
                .font(.footnote)
                .foregroundStyle(.secondary)
                .accessibilityHidden(true)
        }
        .padding(.vertical, 4)
    }
}

// MARK: - A persona

/// The copied message, the microphone, then quick replies: what to answer,
/// and the fastest ways to say how.
private struct PersonaScreen: View {

    let persona: WatchTemplateChoice

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                VStack(spacing: 10) {
                    CopiedMessageCard()
                    SpeakControls(hint: "watch.speak.reply")
                    QuickReplies()
                        .id("quick")
                }
            }
            #if DEBUG
            // `-AIReplyWatchShowQuick`: the quick replies in view, for screenshots.
            .task {
                guard CommandLine.arguments.contains("-AIReplyWatchShowQuick") else { return }
                try? await Task.sleep(for: .seconds(4))
                withAnimation { proxy.scrollTo("quick", anchor: .bottom) }
            }
            #endif
        }
    }
}

private struct CopiedMessageCard: View {

    @Environment(WatchVoiceModel.self) private var model
    @State private var isExpanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Label("watch.preview.title", systemImage: "text.bubble.fill")
                .font(.caption2)
                .foregroundStyle(.secondary)
            switch model.preview {
            case .loading:
                HStack(spacing: 6) {
                    ProgressView()
                        .frame(width: 20, height: 20)
                    Text("watch.preview.loading")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                if model.isPreviewSlow {
                    // iOS asks before a keyboard reads what another app copied.
                    Text("watch.preview.allowPaste")
                        .font(.footnote)
                        .foregroundStyle(.orange)
                        .fixedSize(horizontal: false, vertical: true)
                }
            case .ready(let text, let isTruncated):
                // Three lines, so the microphone is on the first screen;
                // a tap shows all of it.
                Text(verbatim: text)
                    .font(.body)
                    .lineLimit(isExpanded ? nil : 3)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .contentShape(Rectangle())
                    .onTapGesture { isExpanded.toggle() }
                    .accessibilityAddTraits(.isButton)
                if isTruncated {
                    Text("watch.preview.truncated")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
            case .unavailable(let problem):
                Text(problem.message)
                    .font(.footnote)
                    .fixedSize(horizontal: false, vertical: true)
                Button(action: model.loadPreview) {
                    Label("watch.preview.refresh", systemImage: "arrow.clockwise")
                        .font(.footnote)
                }
            }
        }
        .padding(10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 14, style: .continuous).fill(Color.white.opacity(0.12)))
    }
}

/// The microphone first and biggest; typing for when speaking will not do.
private struct SpeakControls: View {

    @Environment(WatchVoiceModel.self) private var model
    let hint: LocalizedStringKey

    var body: some View {
        VStack(spacing: 6) {
            Button(action: model.speakTapped) {
                VStack(spacing: 2) {
                    Image(systemName: "mic.fill")
                        .font(.title2)
                    Text("watch.speak")
                        .font(.headline)
                }
                .frame(maxWidth: .infinity, minHeight: 58)
            }
            .buttonStyle(.borderedProminent)
            .tint(.accentColor)
            .disabled(!model.canUseMicrophone)
            .accessibilityHint(Text(hint))

            Text(model.canUseMicrophone ? hint : microphoneUnavailable)
                .font(.caption2)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)

            // Typing, or Apple's own dictation where the watch has it.
            TextFieldLink(prompt: Text("watch.review.placeholder")) {
                Label(typeLabel, systemImage: "keyboard")
                    .font(.footnote)
                    .frame(maxWidth: .infinity)
            } onSubmit: { text in
                model.didProduceInstruction(text)
            }
        }
    }

    private var microphoneUnavailable: LocalizedStringKey {
        if !model.isReachable { return "watch.connection.unavailable" }
        return model.language.hasSystemDictation ? "watch.failure.transcriptionUnavailable" : "watch.kazakhNeedsServer"
    }

    private var typeLabel: LocalizedStringKey {
        model.language.hasSystemDictation ? "watch.typeOrDictate" : "watch.type"
    }
}

private struct QuickReplies: View {

    @Environment(WatchVoiceModel.self) private var model
    private let columns = [GridItem(.flexible(), spacing: 6), GridItem(.flexible(), spacing: 6)]

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("watch.quick.title")
                .font(.caption2)
                .foregroundStyle(.secondary)
            LazyVGrid(columns: columns, spacing: 6) {
                ForEach(WatchQuickReply.allCases) { quick in
                    Button {
                        model.useQuickReply(quick)
                    } label: {
                        Text(quick.titleKey)
                            .font(.footnote)
                            .lineLimit(2)
                            .minimumScaleFactor(0.75)
                            .multilineTextAlignment(.center)
                            .frame(maxWidth: .infinity, minHeight: 34)
                    }
                    .buttonStyle(.bordered)
                }
            }
        }
    }
}

// MARK: - A new message

private struct NewMessageScreen: View {

    var body: some View {
        ScrollView {
            SpeakControls(hint: "watch.newMessage.hint")
        }
    }
}

// MARK: - Settings

private struct SettingsScreen: View {

    @Environment(WatchVoiceModel.self) private var model

    var body: some View {
        @Bindable var model = model
        List {
            Picker("watch.language", selection: $model.language) {
                ForEach(WatchVoiceLanguage.allCases) { Text(verbatim: $0.nativeName).tag($0) }
            }
            Picker("watch.responseLanguage", selection: $model.responseLanguage) {
                Text("watch.responseLanguage.auto").tag(WatchVoiceLanguage?.none)
                ForEach(WatchVoiceLanguage.allCases) { Text(verbatim: $0.nativeName).tag(WatchVoiceLanguage?.some($0)) }
            }
            Picker("watch.length", selection: $model.length) {
                Text("watch.length.auto").tag(WatchVoiceLength?.none)
                ForEach(WatchVoiceLength.allCases) { Text($0.titleKey).tag(WatchVoiceLength?.some($0)) }
            }
            if model.hasVoiceConsent {
                Section {
                    Button("watch.consent.withdraw", role: .destructive, action: model.withdrawVoiceConsent)
                        .font(.footnote)
                }
            }
        }
        .navigationTitle("watch.settings")
    }
}

// MARK: - Consent

/// Shown once, before the first recording: where the voice goes, and a real
/// choice. "Not now" leaves typing and Apple's dictation as they are.
private struct VoiceConsentScreen: View {

    @Environment(WatchVoiceModel.self) private var model

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 8) {
                Text("watch.consent.title")
                    .font(.headline)
                Text("watch.consent.body")
                    .font(.footnote)
                Button("watch.consent.allow", action: model.acceptVoiceConsent)
                    .buttonStyle(.borderedProminent)
                Button("watch.consent.decline", action: model.declineVoiceConsent)
            }
        }
    }
}

// MARK: - Listening

private struct ListeningScreen: View {

    @Environment(WatchVoiceModel.self) private var model

    var body: some View {
        VStack(spacing: 8) {
            Image(systemName: "waveform")
                .font(.title2)
                .foregroundStyle(.red)
                .accessibilityHidden(true)
            Text("watch.listening")
                .font(.headline)
            Text(verbatim: "\(model.listeningSeconds) / \(WatchVoice.maximumAudioSeconds)")
                .font(.footnote.monospacedDigit())
                .foregroundStyle(.secondary)
                .accessibilityLabel("watch.listening.seconds \(model.listeningSeconds)")
            Button("watch.stop", action: model.stopListening)
                .buttonStyle(.borderedProminent)
                .tint(.red)
            Button("watch.cancel", role: .cancel, action: model.cancelListening)
        }
    }
}

// MARK: - Review

private struct ReviewScreen: View {

    @Environment(WatchVoiceModel.self) private var model

    var body: some View {
        @Bindable var model = model
        ScrollView {
            VStack(spacing: 8) {
                // All of it, to read before it goes anywhere.
                Text(verbatim: model.draft)
                    .font(.footnote)
                    .frame(maxWidth: .infinity, alignment: .leading)
                if model.isDraftTooLong, let limit = model.instructionLimit {
                    Text("watch.tooLong \(limit)")
                        .font(.footnote)
                        .foregroundStyle(.orange)
                }
                Button(action: model.send) {
                    Label("watch.send", systemImage: "paperplane.fill")
                        .frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent)
                .disabled(model.draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || model.isDraftTooLong)

                // The system's editor, opened on this text.
                Text("watch.edit")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .leading)
                TextField("watch.review.placeholder", text: $model.draft)

                Button("watch.clear", action: model.clear)
                Button("watch.cancel", role: .cancel, action: model.cancel)
            }
        }
    }
}

// MARK: - Progress and results

private struct ProgressScreen: View {

    @Environment(WatchVoiceModel.self) private var model
    let title: LocalizedStringKey
    let cancels: Bool

    var body: some View {
        VStack(spacing: 10) {
            ProgressView()
            Text(title)
                .font(.footnote)
                .multilineTextAlignment(.center)
            if cancels {
                Button("watch.cancel", role: .cancel, action: model.cancel)
            }
        }
    }
}

private struct MessageScreen: View {

    @Environment(WatchVoiceModel.self) private var model
    let symbol: String
    let title: LocalizedStringKey
    let primary: LocalizedStringKey?
    let offersCancel: Bool

    var body: some View {
        ScrollView {
            VStack(spacing: 8) {
                Image(systemName: symbol)
                    .font(.title3)
                    .foregroundStyle(.secondary)
                    .accessibilityHidden(true)
                Text(title)
                    .font(.footnote)
                    .multilineTextAlignment(.center)
                if offersCancel {
                    Button("watch.cancel", role: .cancel, action: model.cancel)
                }
            }
        }
    }
}

private struct CompletedScreen: View {

    @Environment(WatchVoiceModel.self) private var model
    let inserted: Bool

    var body: some View {
        ScrollView {
            VStack(spacing: 8) {
                Image(systemName: "checkmark.circle.fill")
                    .font(.title2)
                    .foregroundStyle(.green)
                    .accessibilityHidden(true)
                Text(inserted ? "watch.completed.inserted" : "watch.completed.ready")
                    .font(.footnote)
                    .multilineTextAlignment(.center)
                if !inserted {
                    NavigationLink("watch.viewResult") { RecentResultScreen() }
                }
                Button("watch.done", action: model.done)
                    .buttonStyle(.borderedProminent)
            }
        }
    }
}

private struct RecentResultScreen: View {

    @Environment(WatchVoiceModel.self) private var model

    var body: some View {
        ScrollView {
            if let text = model.recent?.text {
                Text(verbatim: text)
                    .font(.footnote)
            } else {
                Text(model.recent?.inserted == true ? "watch.completed.inserted" : "watch.result.unavailable")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
        .navigationTitle("watch.recent")
        .task { await model.loadRecentResult() }
    }
}

private struct FailureScreen: View {

    @Environment(WatchVoiceModel.self) private var model
    let failure: WatchVoiceFailure
    let limit: Int?

    var body: some View {
        ScrollView {
            VStack(spacing: 8) {
                Image(systemName: "exclamationmark.triangle.fill")
                    .font(.title3)
                    .foregroundStyle(.orange)
                    .accessibilityHidden(true)
                Text(failure.titleKey(limit: limit))
                    .font(.footnote)
                    .multilineTextAlignment(.center)
                if failure.offersRetry {
                    Button("watch.retry", action: model.retry)
                        .buttonStyle(.borderedProminent)
                }
                Button("watch.done", action: model.done)
            }
        }
    }
}

// MARK: - Words

private extension WatchTemplateChoice {
    /// The circle behind a persona's icon: one colour per kind, so the row
    /// reads at a glance.
    var tint: Color {
        switch symbol {
        case "person.2":                  return .green
        case "person":                    return .blue
        case "briefcase":                 return .orange
        case "person.line.dotted.person": return .purple
        default:                          return .teal
        }
    }
}

private extension WatchQuickReply {
    var titleKey: LocalizedStringKey {
        switch self {
        case .polite:  return "watch.quick.polite"
        case .short:   return "watch.quick.short"
        case .agree:   return "watch.quick.agree"
        case .decline: return "watch.quick.decline"
        case .clarify: return "watch.quick.clarify"
        case .thank:   return "watch.quick.thank"
        }
    }
}

private extension WatchVoiceModel.PreviewProblem {
    var message: LocalizedStringKey {
        switch self {
        case .noPhone:            return "watch.connection.unavailable"
        case .noKeyboard:         return "watch.preview.noKeyboard"
        case .nothingCopied:      return "watch.preview.nothingCopied"
        case .pasteDenied:        return "watch.preview.pasteDenied"
        case .fullAccessRequired: return "watch.preview.fullAccess"
        case .tooLong:            return "watch.preview.tooLong"
        case .failed:             return "watch.preview.failed"
        }
    }
}

private extension WatchVoiceLength {
    var titleKey: LocalizedStringKey {
        switch self {
        case .short:  return "watch.length.short"
        case .medium: return "watch.length.medium"
        case .long:   return "watch.length.long"
        }
    }
}

private extension WatchVoiceFailure {
    func titleKey(limit: Int?) -> LocalizedStringKey {
        switch self {
        case .instructionTooLong:
            if let limit { return "watch.tooLong \(limit)" }
            return "watch.failure.instructionTooLong"
        case .unsupportedVersion:       return "watch.failure.update"
        case .invalidRequest, .instructionEmpty, .audioRejected: return "watch.failure.invalid"
        case .expired:                  return "watch.failure.expired"
        case .storageUnavailable:       return "watch.failure.storage"
        case .fullAccessRequired:       return "watch.failure.fullAccess"
        case .notSignedIn:              return "watch.failure.signIn"
        case .consentRequired:          return "watch.failure.consent"
        case .offline:                  return "watch.failure.offline"
        case .timedOut:                 return "watch.failure.timedOut"
        case .rateLimited:              return "watch.failure.rateLimited"
        case .quotaExhausted:           return "watch.failure.quota"
        case .monthlyQuotaExhausted:    return "watch.failure.monthlyQuota"
        case .serviceUnavailable:       return "watch.failure.service"
        case .nothingToRewrite:         return "watch.failure.nothingToRewrite"
        case .copiedMessageUnreadable:  return "watch.failure.copiedUnreadable"
        case .copiedMessageTooLong:     return "watch.failure.copiedTooLong"
        case .transcriptionUnavailable: return "watch.failure.transcriptionUnavailable"
        case .transcriptionFailed:      return "watch.failure.transcription"
        case .nothingHeard:             return "watch.failure.nothingHeard"
        case .voiceLimitReached:        return "watch.failure.voiceLimit"
        case .microphoneDenied:         return "watch.failure.microphone"
        case .phoneUnreachable:         return "watch.failure.phone"
        }
    }
}
