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

/// One screen per stage: this is an app for a glance at the wrist.
struct WatchRootView: View {

    @Environment(WatchVoiceModel.self) private var model

    var body: some View {
        NavigationStack {
            content
                .navigationTitle(Text(verbatim: "AI Reply"))
                .navigationBarTitleDisplayMode(.inline)
        }
    }

    @ViewBuilder
    private var content: some View {
        switch model.stage {
        case .ready:
            HomeScreen()
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

private struct HomeScreen: View {

    @Environment(WatchVoiceModel.self) private var model

    var body: some View {
        @Bindable var model = model
        List {
            Section {
                Label {
                    Text(model.isReachable ? "watch.connection.connected" : "watch.connection.unavailable")
                        .font(.footnote)
                } icon: {
                    Image(systemName: model.isReachable ? "iphone" : "iphone.slash")
                        .foregroundStyle(model.isReachable ? Color.green : Color.orange)
                }
                .accessibilityElement(children: .combine)

                if model.canUseMicrophone {
                    Button(action: model.speakTapped) {
                        Label("watch.speak", systemImage: "mic.fill")
                            .frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.borderedProminent)
                    .tint(.accentColor)
                    .listRowBackground(Color.clear)
                }

                // Typing, or Apple's own dictation: also the way in when the
                // server's speech recognition cannot be reached.
                TextFieldLink(prompt: Text("watch.review.placeholder")) {
                    Label(typeLabel, systemImage: "keyboard")
                        .font(.footnote)
                } onSubmit: { text in
                    model.didProduceInstruction(text)
                }
            } footer: {
                if !model.canUseMicrophone, !model.language.hasSystemDictation {
                    Text("watch.kazakhNeedsServer")
                }
            }

            Section {
                Picker("watch.mode", selection: $model.mode) {
                    ForEach(WatchVoiceMode.offeredOnWatch) { Text($0.titleKey).tag($0) }
                }
                Picker("watch.template", selection: $model.templateID) {
                    Text(inheritedTemplateKey).tag(String?.none)
                    ForEach(model.phone?.templates ?? []) { Text(verbatim: $0.name).tag(String?.some($0.id)) }
                }
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
            }

            if model.recent != nil {
                Section {
                    NavigationLink {
                        RecentResultScreen()
                    } label: {
                        Label("watch.recent", systemImage: "text.bubble")
                    }
                }
            }

            if model.hasVoiceConsent {
                Section {
                    Button("watch.consent.withdraw", role: .destructive, action: model.withdrawVoiceConsent)
                        .font(.footnote)
                }
            }
        }
    }

    private var typeLabel: LocalizedStringKey {
        model.language.hasSystemDictation ? "watch.typeOrDictate" : "watch.type"
    }

    private var inheritedTemplateKey: LocalizedStringKey {
        if let name = model.currentTemplateName, model.phone?.keyboardSessionID != nil {
            return "watch.template.keyboard \(name)"
        }
        return "watch.template.auto"
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

private extension WatchVoiceMode {
    var titleKey: LocalizedStringKey {
        switch self {
        case .reply:      return "watch.mode.reply"
        case .newMessage: return "watch.mode.new"
        case .rewrite:    return "watch.mode.rewrite"
        case .shorten:    return "watch.mode.shorten"
        case .expand:     return "watch.mode.expand"
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
