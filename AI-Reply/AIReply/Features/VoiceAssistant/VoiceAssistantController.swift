import ActivityKit
import AVFoundation
import Observation
import OSLog
import UIKit

/// The main app is the sole microphone and snapshot owner. Extensions can
/// finish/stop a turn. Only the user-triggered AudioRecordingIntent can start
/// capture without presenting the app after foreground permission setup.
@MainActor @Observable
final class VoiceAssistantController {
    static let shared = VoiceAssistantController()
    var isPresented = false
    var snapshot = VoiceAssistantSnapshot(sessionID: UUID().uuidString, phase: .stopped)
    var level = 0.0
    var elapsed = 0.0
    var sourceMessage = ""
    var templateID: String?
    var languageCode = "auto"
    private(set) var isStarting = false
    var copy: VoiceAssistantCopy { .init(language: SharedSettings.shared.effectiveAppLanguage) }
    var isCapturing: Bool { snapshot.phase == .listening || snapshot.phase == .speechDetected }
    var isBusy: Bool { isStarting || isCapturing || snapshot.phase == .transcribing || snapshot.phase == .generating }
    var hasVoiceConsent: Bool { UserDefaults.standard.bool(forKey: "voiceAssistant.audioConsent.v1") }

    @ObservationIgnored private let capture = VoiceAudioCapture()
    @ObservationIgnored private let store: VoiceAssistantStore
    @ObservationIgnored private let protectedDataAvailable: () -> Bool
    @ObservationIgnored private var needsRestore = false
    @ObservationIgnored private var client: VoiceTranscriptionClient?
    @ObservationIgnored private var startTask: Task<Void, Never>?
    @ObservationIgnored private var captureTask: Task<Void, Never>?
    @ObservationIgnored private var recognitionTask: Task<String, Error>?
    @ObservationIgnored private var processingTask: Task<Void, Never>?
    @ObservationIgnored private var timer: Timer?
    @ObservationIgnored private var observers: [NSObjectProtocol] = []
    @ObservationIgnored private var signal: VoiceAssistantSignalObserver?
    @ObservationIgnored private var activity: Activity<VoiceAssistantAttributes>?
    @ObservationIgnored private var activityTask: Task<Void, Never>?
    @ObservationIgnored private var activityUpdateTask: Task<Void, Never>?
    @ObservationIgnored private let logger = Logger(subsystem: "kz.ai-reply", category: "VoiceAssistant")
    @ObservationIgnored private var lastActivityUpdate = Date.distantPast
    @ObservationIgnored private var startedAt = Date()
    @ObservationIgnored private var lastSpeechAt: Date?
    @ObservationIgnored private var processingDeadline: Date?
    @ObservationIgnored private var pcm = Data()
    @ObservationIgnored private var backgroundTask = UIBackgroundTaskIdentifier.invalid

    init(store: VoiceAssistantStore = .shared,
         protectedDataAvailable: @escaping () -> Bool = { UIApplication.shared.isProtectedDataAvailable }) {
        self.store = store
        self.protectedDataAvailable = protectedDataAvailable
        templateID = SharedSettings.shared.lastTemplateID
        if protectedDataAvailable() {
            if let saved = store.readSnapshot(), saved.phase == .responseReady {
                snapshot = saved
                sourceMessage = saved.sourceMessage
                templateID = saved.templateID
            } else {
                store.writeSnapshot(snapshot)
            }
            store.purge()
        } else {
            // Class B files cannot be reopened while locked. A failed read is
            // not evidence of an empty store: defer restore and all cleanup.
            needsRestore = true
        }
        // A terminated app never silently resumes microphone capture.
        let oldActivities = Activity<VoiceAssistantAttributes>.activities
        Task {
            for old in oldActivities { await old.end(nil, dismissalPolicy: .immediate) }
        }
        signal = VoiceAssistantSignalObserver(signals: [.command]) { [weak self] _ in
            Task { @MainActor in self?.readCommands() }
        }
        observers.append(NotificationCenter.default.addObserver(forName: UIApplication.protectedDataDidBecomeAvailableNotification,
                                                               object: nil, queue: .main) { [weak self] _ in
            Task { @MainActor in self?.refreshPublishedState() }
        })
        observers.append(NotificationCenter.default.addObserver(forName: .voiceAssistantConsentWithdrawn,
                                                               object: nil, queue: .main) { [weak self] _ in
            Task { @MainActor in self?.stop(clearPrivateContext: true) }
        })
        for name in [AVAudioSession.interruptionNotification, AVAudioSession.mediaServicesWereResetNotification,
                     AVAudioSession.routeChangeNotification] {
            observers.append(NotificationCenter.default.addObserver(forName: name, object: nil, queue: .main) { [weak self] note in
                let began = (note.userInfo?[AVAudioSessionInterruptionTypeKey] as? UInt) == AVAudioSession.InterruptionType.began.rawValue
                let removed = (note.userInfo?[AVAudioSessionRouteChangeReasonKey] as? UInt) == AVAudioSession.RouteChangeReason.oldDeviceUnavailable.rawValue
                if began || removed || note.name == AVAudioSession.mediaServicesWereResetNotification {
                    Task { @MainActor in
                        guard let self, self.isCapturing || self.isStarting else { return }
                        self.stop(phase: .paused, message: self.copy.interrupted)
                    }
                }
            })
        }
    }

    func open(startWhenPossible: Bool = false) {
        isPresented = true
        if startWhenPossible && !isBusy {
            // A system shortcut is a new conversation context. Never reuse a
            // message pasted for a preceding turn without another paste.
            sourceMessage = ""
            templateID = SharedSettings.shared.lastTemplateID
        }
        if startWhenPossible && hasVoiceConsent && !isBusy { start() }
    }

    func start(consent: Bool = false) {
        guard !isBusy, UIApplication.shared.applicationState == .active else { return }
        if consent { UserDefaults.standard.set(true, forKey: "voiceAssistant.audioConsent.v1") }
        guard hasVoiceConsent else { return }
        let id = prepareSession()
        startTask = Task { [weak self] in await self?.begin(id: id) }
    }

    /// Awaited directly by AudioRecordingIntent.perform(), within its system
    /// authorization context. No keyboard mailbox or generic background task
    /// can call this path or grant permission on the user's behalf.
    func startFromControl() async throws {
        guard hasVoiceConsent, AVAudioApplication.shared.recordPermission == .granted,
              AIReplyService.preflight(isSignedIn: AccountCredentials.isSignedIn,
                                       hasConsent: LegalConsentStore.hasAcceptedCurrentVersions()) == nil else {
            throw VoiceAssistantControlStartError(message: String(localized: "Open AI Reply once to allow microphone access and voice processing."))
        }
        if isCapturing { finish(); return }
        guard !isBusy else { return }
        sourceMessage = ""
        templateID = SharedSettings.shared.lastTemplateID
        let id = prepareSession()
        await begin(id: id, systemControl: true)
        guard snapshot.sessionID == id, isCapturing else {
            throw VoiceAssistantControlStartError(message: snapshot.error ?? copy.interrupted)
        }
    }

    private func prepareSession() -> String {
        needsRestore = false
        stop()
        let id = UUID().uuidString
        snapshot = VoiceAssistantSnapshot(sessionID: id, phase: .ready, requestID: UUID().uuidString,
                                          sourceMessage: sourceMessage, templateID: templateID)
        isStarting = true
        startedAt = Date()
        publish(.ready)
        return id
    }

    private func begin(id: String, systemControl: Bool = false) async {
        defer {
            if snapshot.sessionID == id {
                isStarting = false
                startTask = nil
                // A cancelled system intent can stop awaiting setup without
                // delivering a keyboard Stop command. Close its preparation
                // activity and socket instead of leaving a stale ready task.
                if snapshot.phase == .ready { stop() }
            }
        }
        guard AIReplyService.preflight(isSignedIn: AccountCredentials.isSignedIn,
                                       hasConsent: LegalConsentStore.hasAcceptedCurrentVersions()) == nil else {
            fail(copy.signIn, id: id); return
        }
        guard AILimits.serverSupportsVoiceTranscription || AILimits.serverSupportsVoiceRealtimeTranscription else {
            fail(copy.unavailable, id: id); return
        }
        let permitted: Bool
        if systemControl { permitted = AVAudioApplication.shared.recordPermission == .granted }
        else { permitted = await AVAudioApplication.requestRecordPermission() }
        guard !Task.isCancelled, snapshot.sessionID == id, isStarting else { return }
        guard permitted else { fail(copy.permission, id: id); return }
        // A permission dialog may outlive foreground activation.
        guard systemControl || UIApplication.shared.applicationState == .active else { stop(); return }
        // AudioRecordingIntent requires a Live Activity throughout capture.
        // Request it BEFORE audio activation and before perform() returns.
        if !beginActivity() && systemControl { fail(copy.liveActivityRequired, id: id); return }
        if AILimits.serverSupportsVoiceRealtimeTranscription {
            let connection = VoiceTranscriptionClient()
            client = connection
            do {
                try await connection.connect(requestID: snapshot.requestID!, languages: languages)
                guard !Task.isCancelled, snapshot.sessionID == id, isStarting else { await connection.close(); return }
                recognitionTask = Task {
                    do {
                        return try await connection.events { [weak self] delta in
                            await self?.received(delta: delta, id: id)
                        }
                    } catch {
                        if !Task.isCancelled { self.fail(self.message(for: error), id: id) }
                        throw error
                    }
                }
            } catch {
                await connection.close()
                client = nil
                // Streaming failures are not retried as paid batch work. Only
                // an older server without the advertised feature uses WAV.
                if isStarting { fail(message(for: error), id: id) }
                return
            }
        }
        guard !Task.isCancelled, snapshot.sessionID == id, isStarting else { return }
        guard systemControl || UIApplication.shared.applicationState == .active else { stop(); return }
        do {
            let stream = try capture.start()
            pcm = Data(); elapsed = 0; startedAt = Date(); lastSpeechAt = nil
            publish(.listening)
            startTimer()
            captureTask = Task { [weak self] in
                guard let self else { return }
                do {
                    for try await chunk in stream {
                        guard snapshot.sessionID == id, !Task.isCancelled else { return }
                        let remaining = 40 * 48_000 - pcm.count
                        guard remaining > 0 else { break }
                        let audio = Data(chunk.pcm.prefix(remaining))
                        pcm.append(audio)
                        level = chunk.level
                        if chunk.level > 0.10 {
                            lastSpeechAt = Date()
                            if isCapturing { publish(.speechDetected, write: false) }
                        }
                        if let client { try await client.append(audio) }
                    }
                } catch {
                    if !Task.isCancelled { fail(message(for: error), id: id) }
                }
            }
            // The Control Center action must not return with the old
            // Preparing state still displayed while capture is already live.
            await activityUpdateTask?.value
        } catch { fail(copy.recordingFailed, id: id) }
    }

    private var languages: [String] {
        let allowed = ["ru", "kk", "en"]
        return allowed.contains(languageCode) ? [languageCode] + allowed.filter { $0 != languageCode } : allowed
    }

    private func received(delta: String, id: String) {
        guard snapshot.sessionID == id, isCapturing || snapshot.phase == .transcribing else { return }
        snapshot.transcript = String((snapshot.transcript + delta).prefix(4000))
        if isCapturing && !delta.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            // Real recognition also counts as speech when a quiet microphone
            // falls below the local level threshold.
            lastSpeechAt = Date()
            if snapshot.phase == .listening { publish(.speechDetected, write: false) }
        }
        // Live Activity deliberately contains no recognized or copied text.
    }

    func finish() {
        guard isCapturing else { return }
        let id = snapshot.sessionID
        // Obtain processing time before releasing the background-audio reason
        // for execution; otherwise iOS can suspend between these operations.
        beginBackgroundWork()
        capture.stop()
        processingDeadline = Date().addingTimeInterval(70)
        publish(.transcribing)
        let pendingCapture = captureTask
        processingTask = Task { [weak self] in
            guard let self else { return }
            await pendingCapture?.value
            guard !Task.isCancelled, snapshot.sessionID == id, snapshot.phase == .transcribing else { return }
            do {
                guard pcm.count >= 24_000 else { throw VoiceTranscriptionClient.Failure.empty }
                let text: String
                if let client, let recognitionTask {
                    try await client.commit()
                    text = try await recognitionTask.value
                    await client.close()
                } else {
                    text = try await VoiceTranscriptionClient.transcribe(pcm: pcm, requestID: snapshot.requestID!, languages: languages)
                }
                pcm.removeAll(keepingCapacity: false)
                guard !Task.isCancelled, snapshot.sessionID == id else { return }
                snapshot.transcript = text
                try await generate(id: id, regeneration: false)
            } catch {
                if !Task.isCancelled { fail(message(for: error), id: id) }
            }
        }
    }

    func regenerate() {
        guard !isBusy, !snapshot.transcript.isEmpty else { return }
        processingTask?.cancel()
        releaseWork()
        let id = snapshot.sessionID
        snapshot.requestID = UUID().uuidString
        snapshot.response = ""
        beginBackgroundWork()
        processingDeadline = Date().addingTimeInterval(45)
        startTimer()
        publish(.generating)
        processingTask = Task { [weak self] in
            guard let self else { return }
            do { try await generate(id: id, regeneration: true) }
            catch { if !Task.isCancelled { fail(message(for: error), id: id) } }
        }
    }

    private func generate(id: String, regeneration: Bool) async throws {
        publish(.generating)
        let configuration = ProfileStore.shared.load()
        let language = SharedSettings.shared.effectiveAppLanguage
        let reply: GeneratedReply
        if snapshot.sourceMessage.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            reply = try await ComposeService().compose(.init(instruction: snapshot.transcript, uiLanguage: language,
                isRegeneration: regeneration, inputLanguage: language.keyboardLanguage,
                grammaticalGender: configuration.profile.grammaticalGender, actionID: snapshot.requestID))
        } else {
            guard let template = configuration.templates.first(where: { $0.id == snapshot.templateID }) ?? configuration.visibleTemplates.first else {
                throw AIReplyError.serviceUnavailable
            }
            reply = try await AIReplyService().generate(.init(message: snapshot.sourceMessage, template: template,
                configuration: configuration, uiLanguage: language, instruction: snapshot.transcript,
                inputLanguage: language.keyboardLanguage, actionID: snapshot.requestID))
        }
        guard !Task.isCancelled, snapshot.sessionID == id else { return }
        snapshot.response = reply.text
        publish(.responseReady)
        timer?.invalidate(); timer = nil
        processingDeadline = nil
        let completedRequest = snapshot.requestID
        await activityUpdateTask?.value
        // Brief, finite delivery confirmation. The processing background task
        // stays owned until this ends; the microphone is already released.
        try await Task.sleep(for: .seconds(3))
        guard !Task.isCancelled, snapshot.sessionID == id,
              snapshot.requestID == completedRequest, snapshot.phase == .responseReady else { return }
        releaseWork()
        endActivity()
    }

    func stop(phase: VoiceAssistantPhase = .stopped, message: String? = nil, clearPrivateContext: Bool = false) {
        startTask?.cancel(); startTask = nil; isStarting = false
        capture.stop()
        captureTask?.cancel(); captureTask = nil
        recognitionTask?.cancel(); recognitionTask = nil
        processingTask?.cancel(); processingTask = nil
        if let client { Task { await client.close() } }
        client = nil
        pcm.removeAll(keepingCapacity: false)
        level = 0
        if clearPrivateContext {
            needsRestore = false
            sourceMessage = ""
            templateID = nil
            snapshot = VoiceAssistantSnapshot(sessionID: UUID().uuidString, phase: phase)
        }
        snapshot.error = message
        publish(phase)
        releaseWork()
        endActivity()
    }

    private func fail(_ message: String, id: String) {
        guard snapshot.sessionID == id else { return }
        stop(phase: .error, message: message)
    }

    private func readCommands() {
        for command in store.pendingCommands(sessionID: snapshot.sessionID) {
            defer { store.removeCommand(command) }
            switch command.kind {
            case .stop: stop()
            case .pause: stop(phase: .paused)
            case .finish: finish()
            case .regenerate:
                guard command.requestID == snapshot.requestID,
                      command.keyboardSessionID == snapshot.keyboardSessionID else { continue }
                regenerate()
            case .request, .context:
                guard isCapturing else { continue }
                snapshot.keyboardSessionID = command.keyboardSessionID
                if let source = command.sourceMessage { snapshot.sourceMessage = source; sourceMessage = source }
                if let template = command.templateID { snapshot.templateID = template; templateID = template }
                publish(snapshot.phase)
            }
        }
    }

    private func startTimer() {
        timer?.invalidate()
        let pulse = Timer(timeInterval: 0.5, repeats: true) { [weak self] _ in
            Task { @MainActor in self?.tick() }
        }
        RunLoop.main.add(pulse, forMode: .common)
        timer = pulse
    }

    private func tick() {
        readCommands()
        if isCapturing {
            elapsed = Date().timeIntervalSince(startedAt)
            switch VoiceAssistantRecordingPolicy.decision(elapsed: elapsed, hasSpeech: lastSpeechAt != nil) {
            case .finish: finish(); return
            case .noSpeech: fail(copy.nothingHeard, id: snapshot.sessionID); return
            case .keepRecording: break
            }
            publish(snapshot.phase)
        }
        if let processingDeadline, Date() > processingDeadline { fail(copy.timedOut, id: snapshot.sessionID) }
    }

    private func publish(_ phase: VoiceAssistantPhase, write: Bool = true) {
        let changed = snapshot.phase != phase
        if changed { logger.info("voice phase \(phase.rawValue, privacy: .public)") }
        snapshot.phase = phase
        snapshot.updatedAt = Date()
        snapshot.expiresAt = Date().addingTimeInterval(180)
        if write && !needsRestore { store.writeSnapshot(snapshot) }
        guard changed || Date().timeIntervalSince(lastActivityUpdate) >= 1, let activity else { return }
        lastActivityUpdate = Date()
        let state = activityState
        let previous = activityUpdateTask
        activityUpdateTask = Task { [weak self] in
            await previous?.value
            guard !Task.isCancelled, self?.activity?.id == activity.id else { return }
            await activity.update(ActivityContent(state: state, staleDate: state.deadline))
        }
    }

    private var activityState: VoiceAssistantAttributes.ContentState {
        .init(phase: snapshot.phase.rawValue, level: level,
              deadline: isCapturing ? startedAt.addingTimeInterval(40) : (processingDeadline ?? Date().addingTimeInterval(45)))
    }

    func refreshPublishedState() {
        guard protectedDataAvailable() else { return }
        if needsRestore {
            needsRestore = false
            if let saved = store.readSnapshot(), saved.phase == .responseReady {
                snapshot = saved
                sourceMessage = saved.sourceMessage
                templateID = saved.templateID
            } else {
                // An interrupted/orphaned recording is never resumed on unlock.
                snapshot = VoiceAssistantSnapshot(sessionID: UUID().uuidString, phase: .stopped)
                sourceMessage = ""
                templateID = SharedSettings.shared.lastTemplateID
            }
        }
        store.purge()
        readCommands()
        guard snapshot.isFresh() else { stop(clearPrivateContext: true); return }
        store.writeSnapshot(snapshot)
    }

    @discardableResult
    private func beginActivity() -> Bool {
        guard ActivityAuthorizationInfo().areActivitiesEnabled else { return false }
        activity = try? Activity.request(attributes: VoiceAssistantAttributes(sessionID: snapshot.sessionID, startedAt: startedAt),
            content: ActivityContent(state: activityState, staleDate: startedAt.addingTimeInterval(115)), pushType: nil)
        guard let activity else { return false }
        lastActivityUpdate = .distantPast
        let id = snapshot.sessionID
        activityTask = Task { [weak self] in
            for await state in activity.activityStateUpdates {
                guard !Task.isCancelled, let self, self.snapshot.sessionID == id else { return }
                if state == .dismissed || state == .ended {
                    if self.isCapturing || self.isStarting { self.stop(phase: .paused, message: self.copy.interrupted) }
                    return
                }
            }
        }
        return true
    }

    private func endActivity() {
        activityTask?.cancel(); activityTask = nil
        activityUpdateTask?.cancel(); activityUpdateTask = nil
        guard let activity else { return }
        self.activity = nil
        let state = activityState
        Task { await activity.end(ActivityContent(state: state, staleDate: Date()), dismissalPolicy: .immediate) }
    }

    private func beginBackgroundWork() {
        guard backgroundTask == .invalid else { return }
        backgroundTask = UIApplication.shared.beginBackgroundTask(withName: "Voice reply") { [weak self] in
            Task { @MainActor in
                guard let self else { return }
                if self.snapshot.phase == .responseReady {
                    self.releaseWork()
                    self.endActivity()
                } else {
                    self.stop(phase: .error, message: self.copy.timedOut)
                }
            }
        }
    }

    private func releaseWork() {
        timer?.invalidate(); timer = nil
        processingDeadline = nil
        if backgroundTask != .invalid { UIApplication.shared.endBackgroundTask(backgroundTask); backgroundTask = .invalid }
    }

    private func message(for error: Error) -> String {
        let failure = error as NSError
        logger.error("voice failure domain=\(failure.domain, privacy: .public) code=\(failure.code) audioBytes=\(self.pcm.count)")
        if let error = error as? AIReplyError {
            return AIReplyStrings.forLanguage(SharedSettings.shared.effectiveAppLanguage).message(for: error)
        }
        if let error = error as? APIError {
            return WatchVoiceStrings.forLanguage(SharedSettings.shared.effectiveAppLanguage).message(for: WatchTranscriber.failure(for: error))
        }
        if case VoiceTranscriptionClient.Failure.empty = error { return copy.nothingHeard }
        if case VoiceTranscriptionClient.Failure.provider(let code) = error {
            if code == "VOICE_LIMIT_REACHED" { return copy.voiceLimit }
            if code == "UNAUTHORIZED" || code == "CONSENT_REQUIRED" { return copy.signIn }
        }
        if (error as? URLError)?.code == .notConnectedToInternet { return copy.offline }
        return copy.recordingFailed
    }
}

struct VoiceAssistantControlStartError: LocalizedError {
    let message: String
    var errorDescription: String? { message }
}

/// Silence is an utterance boundary, never permission to end the user's
/// recording session. Done/Cancel and the bounded safety limits own that flow.
enum VoiceAssistantRecordingPolicy {
    enum Decision: Equatable { case keepRecording, finish, noSpeech }
    static let maximumDuration: TimeInterval = 40
    static let initialSilenceLimit: TimeInterval = 20

    static func decision(elapsed: TimeInterval, hasSpeech: Bool) -> Decision {
        if !hasSpeech && elapsed >= initialSilenceLimit { return .noSpeech }
        if elapsed >= maximumDuration { return .finish }
        return .keepRecording
    }
}
