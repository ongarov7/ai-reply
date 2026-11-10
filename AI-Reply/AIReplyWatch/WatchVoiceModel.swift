import Foundation
import Observation
import SwiftUI
import WatchKit

/// Everything the watch app does between the microphone and "ready on iPhone".
@MainActor
@Observable
final class WatchVoiceModel {

    enum Stage: Equatable {
        case ready
        /// First use: what happens to the voice, before anything is recorded.
        case voiceConsent
        case listening
        /// The recording is on its way to text.
        case processing
        case review
        case sending
        /// In the system's queue: delivered when the phone can be reached.
        case queuedForLater
        /// On the phone, with no keyboard to take it.
        case waitingForKeyboard
        case sentToPhone
        case generating
        case completed(inserted: Bool)
        case failed(WatchVoiceFailure, limit: Int?)
    }

    struct RecentResult: Equatable {
        let requestID: String
        var inserted: Bool
        /// Only ever in memory, and only after "View result".
        var text: String?
    }

    private(set) var stage: Stage = .ready {
        didSet {
            #if DEBUG
            NSLog("[WatchVoice] stage %@", String(describing: stage))
            #endif
        }
    }
    /// The instruction being reviewed. Editable before it is sent.
    var draft = ""
    private(set) var listeningSeconds = 0
    private(set) var isReachable = false
    private(set) var phone: WatchPhoneSnapshot?
    private(set) var recent: RecentResult?
    private(set) var hasVoiceConsent: Bool

    var mode: WatchVoiceMode { didSet { save(mode.rawValue, Key.mode) } }
    var language: WatchVoiceLanguage {
        didSet {
            // The phone's language is a default, not the user's choice.
            guard !isAdoptingPhoneLanguage else { return }
            hasChosenLanguage = true
            save(language.rawValue, Key.language)
        }
    }
    var responseLanguage: WatchVoiceLanguage? { didSet { save(responseLanguage?.rawValue, Key.responseLanguage) } }
    var length: WatchVoiceLength? { didSet { save(length?.rawValue, Key.length) } }
    /// nil: inherit the persona the keyboard uses.
    var templateID: String? { didSet { save(templateID, Key.template) } }

    /// The app's language on the phone, which this app follows.
    var locale: Locale {
        phone?.appLanguage.map { Locale(identifier: $0) } ?? .autoupdatingCurrent
    }

    @ObservationIgnored private let client = WatchConnectivityClient()
    @ObservationIgnored private let recorder = WatchAudioRecorder()
    @ObservationIgnored private let defaults = UserDefaults.standard

    @ObservationIgnored private var currentRequestID: String?
    /// The phone acknowledged this request, so a new attempt is a new request.
    @ObservationIgnored private var wasAcknowledged = false
    @ObservationIgnored private var currentRecording: WatchRecording?
    @ObservationIgnored private var isForeground = true
    @ObservationIgnored private var pollTask: Task<Void, Never>?
    @ObservationIgnored private var listenTask: Task<Void, Never>?
    @ObservationIgnored private var transcriptionTimeout: Task<Void, Never>?
    @ObservationIgnored private var hasChosenLanguage: Bool
    @ObservationIgnored private var isAdoptingPhoneLanguage = false
    /// Between the tap on Speak and the recorder running: a second tap waits.
    @ObservationIgnored private var isStartingListening = false
    /// How far the current request has got, so a late message cannot move
    /// the screen back (a queued "ready" after a live "inserted").
    @ObservationIgnored private var statusRank = -1

    private enum Key {
        static let mode = "watch.mode"
        static let language = "watch.language"
        static let responseLanguage = "watch.responseLanguage"
        static let length = "watch.length"
        static let template = "watch.template"
        static let voiceConsent = "watch.voiceConsent"
    }

    init() {
        let defaults = UserDefaults.standard
        mode = defaults.string(forKey: Key.mode).flatMap(WatchVoiceMode.init(rawValue:)) ?? .reply
        let stored = defaults.string(forKey: Key.language).flatMap(WatchVoiceLanguage.init(rawValue:))
        hasChosenLanguage = stored != nil
        language = stored ?? Self.systemLanguage
        responseLanguage = defaults.string(forKey: Key.responseLanguage).flatMap(WatchVoiceLanguage.init(rawValue:))
        length = defaults.string(forKey: Key.length).flatMap(WatchVoiceLength.init(rawValue:))
        templateID = defaults.string(forKey: Key.template)
        hasVoiceConsent = defaults.string(forKey: Key.voiceConsent) == WatchVoice.voiceConsentVersion

        client.onEvent = { [weak self] event in
            Task { @MainActor in self?.handle(event) }
        }
        client.start()
        #if DEBUG
        runSmokeTestIfRequested()
        #endif
    }

    #if DEBUG
    /// `-AIReplyWatchSmokeTest`: the real microphone path with no taps,
    /// because a Watch simulator cannot be driven from the command line.
    private func runSmokeTestIfRequested() {
        guard CommandLine.arguments.contains("-AIReplyWatchSmokeTest") else { return }
        defaults.set(WatchVoice.voiceConsentVersion, forKey: Key.voiceConsent)
        hasVoiceConsent = true
        // Slower with `-AIReplyWatchDemo`, for screenshots of each stage.
        let pause: TimeInterval = CommandLine.arguments.contains("-AIReplyWatchDemo") ? 6 : 3
        // `-AIReplyWatchListen 20`: a longer recording, sent in several parts.
        let listen = CommandLine.arguments.firstIndex(of: "-AIReplyWatchListen")
            .flatMap { CommandLine.arguments.indices.contains($0 + 1) ? TimeInterval(CommandLine.arguments[$0 + 1]) : nil } ?? pause
        Task {
            try? await Task.sleep(for: .seconds(pause + 1))
            startListening()
            try? await Task.sleep(for: .seconds(listen))
            stopListening()
            for _ in 0..<40 where stage != .review {
                try? await Task.sleep(for: .milliseconds(500))
            }
            try? await Task.sleep(for: .seconds(pause / 3 + (pause > 3 ? 4 : 0)))
            NSLog("[WatchVoice] smoke: send, session %@", phone?.keyboardSessionID ?? "none")
            send()
        }
    }
    #endif

    private static var systemLanguage: WatchVoiceLanguage {
        for identifier in Locale.preferredLanguages {
            if let match = WatchVoiceLanguage(rawValue: String(identifier.prefix(2)).lowercased()) { return match }
        }
        return .english
    }

    private func save(_ value: String?, _ key: String) {
        defaults.set(value, forKey: key)
    }

    // MARK: What the screen offers

    /// The microphone: the server path, unless the phone said it has none
    /// or cannot be reached right now.
    var canUseMicrophone: Bool {
        isReachable && phone?.transcriptionAvailable != false
    }

    var currentTemplateName: String? {
        guard let phone else { return nil }
        let id = templateID ?? phone.currentTemplateID ?? phone.templates.first?.id
        return phone.templates.first { $0.id == id }?.name
    }

    // MARK: Lifecycle

    func scenePhaseChanged(_ phase: ScenePhase) {
        isForeground = phase == .active
        guard phase == .active else {
            pollTask?.cancel()
            pollTask = nil
            // Lowering the wrist is `.inactive` and keeps recording. Only
            // really leaving the app ends it, keeping what was said: it is
            // still reviewed before anything is sent.
            if phase == .background, stage == .listening { stopListening() }
            return
        }
        isReachable = client.isReachable
        Task { await refreshStatus() }
        resumePollingIfNeeded()
    }

    private func handle(_ event: WatchConnectivityClient.Event) {
        switch event {
        case .activated, .reachabilityChanged:
            isReachable = client.isReachable
            if phone == nil, let snapshot = client.lastPhoneSnapshot { apply(snapshot) }
            if isReachable { Task { await refreshStatus() } }
        case .envelope(let envelope):
            receive(envelope)
        }
    }

    /// Asks the phone what is going on, which wakes its app if need be: the
    /// only way to learn about a keyboard on screen. Bounded by its callers.
    func refreshStatus() async {
        guard client.isReachable else { return }
        let request = WatchVoiceEnvelope.request(.statusRequest, requestID: currentRequestID ?? WatchVoiceIdentifier.make())
        if let reply = try? await client.request(request) { receive(reply) }
    }

    private func apply(_ snapshot: WatchPhoneSnapshot) {
        phone = snapshot
        if !hasChosenLanguage, let code = snapshot.appLanguage, let match = WatchVoiceLanguage(rawValue: code) {
            isAdoptingPhoneLanguage = true
            language = match
            isAdoptingPhoneLanguage = false
        }
    }

    // MARK: Microphone

    /// Speak: the disclosure comes first, then the system's microphone prompt.
    func speakTapped() {
        guard hasVoiceConsent else {
            stage = .voiceConsent
            return
        }
        startListening()
    }

    /// Explicit permission before a voice goes to a third-party AI.
    func acceptVoiceConsent() {
        defaults.set(WatchVoice.voiceConsentVersion, forKey: Key.voiceConsent)
        hasVoiceConsent = true
        stage = .ready
        startListening()
    }

    func declineVoiceConsent() {
        stage = .ready
    }

    /// Nothing recorded before this may leave the watch afterwards.
    func withdrawVoiceConsent() {
        if stage == .listening { cancelListening() }
        if stage == .processing {
            transcriptionTimeout?.cancel()
            reset()
        }
        discardRecording()
        defaults.removeObject(forKey: Key.voiceConsent)
        hasVoiceConsent = false
    }

    /// The user's tap on the microphone. Nothing listens before or after.
    func startListening() {
        guard stage == .ready || isFailed, !isStartingListening else { return }
        guard client.isReachable else {
            fail(.phoneUnreachable)
            return
        }
        // A new recording: whatever the last request was, it is over here.
        discardRecording()
        currentRequestID = nil
        statusRank = -1
        isStartingListening = true
        Task {
            defer { isStartingListening = false }
            guard await recorder.requestPermission() else {
                fail(.microphoneDenied)
                return
            }
            do {
                try recorder.start()
            } catch {
                fail(.transcriptionFailed)
                return
            }
            listeningSeconds = 0
            stage = .listening
            WKInterfaceDevice.current().play(.start)
            listenTask?.cancel()
            listenTask = Task { [weak self] in
                while let self, self.stage == .listening, !Task.isCancelled {
                    try? await Task.sleep(for: .milliseconds(250))
                    guard !Task.isCancelled else { return }
                    self.listeningSeconds = Int(self.recorder.elapsed)
                    if !self.recorder.isRecording {
                        // The 40-second limit: the recorder stopped by itself.
                        self.stopListening()
                        return
                    }
                }
            }
        }
    }

    func stopListening() {
        guard stage == .listening else { return }
        listenTask?.cancel()
        listenTask = nil
        guard let recording = recorder.stop() else {
            fail(.nothingHeard)
            return
        }
        WKInterfaceDevice.current().play(.stop)
        currentRecording = recording
        stage = .processing
        Task { await upload(recording) }
    }

    func cancelListening() {
        listenTask?.cancel()
        listenTask = nil
        recorder.cancel()
        stage = .ready
    }

    /// Sends the recording in parts on the channel that wakes the phone app.
    /// Every await is followed by a check that this recording is still the
    /// one on screen: Cancel or a new recording ends it.
    private func upload(_ recording: WatchRecording) async {
        func isCurrent() -> Bool { currentRecording?.requestID == recording.requestID && stage == .processing }
        guard client.isReachable else {
            fail(.phoneUnreachable)
            return
        }
        guard let audio = try? Data(contentsOf: recording.file) else {
            discardRecording()
            fail(.nothingHeard)
            return
        }
        let parts = WatchVoiceAudioAssembly.split(audio)
        for (index, part) in parts.enumerated() {
            let envelope = WatchVoiceEnvelope(requestID: recording.requestID, type: .transcribe, createdAt: Date(), source: .watch,
                                              language: language, audioMilliseconds: recording.milliseconds,
                                              audioPart: index, audioParts: parts.count)
            var attempt = 1
            while true {
                do {
                    let answer = try await client.sendAudio(try WatchVoiceAudioPacket.encode(envelope, audio: part))
                    guard isCurrent() else { return }
                    if let failure = answer.failure {
                        if !failure.offersRetry { discardRecording() }
                        fail(failure)
                        return
                    }
                    break
                } catch {
                    #if DEBUG
                    NSLog("[WatchVoice] part %d of %d (%d bytes) not delivered: %@", index + 1, parts.count, part.count, String(describing: error))
                    #endif
                    guard isCurrent() else { return }
                    guard attempt < WatchVoiceDelivery.audioPartAttempts, client.isReachable else {
                        fail(client.isReachable ? .transcriptionFailed : .phoneUnreachable)
                        return
                    }
                    attempt += 1
                    try? await Task.sleep(for: .seconds(1))
                    guard isCurrent() else { return }
                }
            }
        }
        // Upload and speech recognition for 40 seconds of audio.
        transcriptionTimeout?.cancel()
        transcriptionTimeout = Task { [weak self] in
            try? await Task.sleep(for: .seconds(60))
            guard !Task.isCancelled, let self, self.currentRecording?.requestID == recording.requestID,
                  self.stage == .processing else { return }
            self.fail(.transcriptionFailed)
        }
    }

    private func receiveTranscript(_ envelope: WatchVoiceEnvelope) {
        guard envelope.requestID == currentRecording?.requestID, stage == .processing else { return }
        transcriptionTimeout?.cancel()
        if let failure = envelope.failure {
            if !failure.offersRetry { discardRecording() }
            fail(failure)
            return
        }
        let text = (envelope.instruction ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else {
            discardRecording()
            fail(.nothingHeard)
            return
        }
        discardRecording()
        didProduceInstruction(text)
    }

    private func discardRecording() {
        if let currentRecording {
            client.cancelQueued(requestID: currentRecording.requestID)
            try? FileManager.default.removeItem(at: currentRecording.file)
        }
        currentRecording = nil
    }

    /// Text from the server, or from typing or Apple's dictation.
    func didProduceInstruction(_ text: String) {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        draft = trimmed
        currentRequestID = nil
        wasAcknowledged = false
        statusRank = -1
        stage = .review
        // So the command names the keyboard on screen now.
        Task { await refreshStatus() }
    }

    // MARK: Sending

    /// The longest instruction this request can carry, when known. "Long"
    /// adds a sentence of up to 41 characters on the iPhone.
    var instructionLimit: Int? {
        guard mode == .reply, let limit = phone?.replyInstructionLimit else { return nil }
        return length == .long ? max(40, limit - 42) : limit
    }

    var isDraftTooLong: Bool {
        guard let limit = instructionLimit else { return false }
        return draft.trimmingCharacters(in: .whitespacesAndNewlines).unicodeScalars.count > limit
    }

    func send() {
        let text = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty, !isDraftTooLong else { return }
        // An attempt the phone may not have seen keeps its identifier, so
        // a duplicate is recognised. Anything else is a new action.
        let id = (currentRequestID != nil && !wasAcknowledged) ? currentRequestID! : WatchVoiceIdentifier.make()
        currentRequestID = id
        wasAcknowledged = false
        stage = .sending
        let options = WatchVoiceOptions(mode: mode, templateID: templateID, responseLanguage: responseLanguage, length: length)
        let envelope = WatchVoiceEnvelope.command(requestID: id, instruction: text, language: language, options: options,
                                                  keyboardSessionID: phone?.keyboardSessionID)
        Task { await deliver(envelope) }
    }

    private func deliver(_ envelope: WatchVoiceEnvelope) async {
        var attempt = 1
        while client.isReachable {
            do {
                let reply = try await client.request(envelope)
                guard currentRequestID == envelope.requestID else { return }
                wasAcknowledged = true
                WKInterfaceDevice.current().play(.success)
                receive(reply)
                startPolling()
                return
            } catch {
                guard currentRequestID == envelope.requestID else { return }
                guard case .retry(let delay) = WatchVoiceDelivery.step(afterAttempt: attempt, isReachable: client.isReachable) else { break }
                try? await Task.sleep(for: .seconds(delay))
                attempt += 1
            }
        }
        guard currentRequestID == envelope.requestID else { return }
        do {
            try client.enqueue(envelope)
            stage = .queuedForLater
            WKInterfaceDevice.current().play(.click)
        } catch {
            fail(.invalidRequest)
        }
    }

    private func receive(_ envelope: WatchVoiceEnvelope) {
        if let snapshot = envelope.phone { apply(snapshot) }
        if envelope.type == .transcript {
            receiveTranscript(envelope)
            return
        }
        guard envelope.type == .statusUpdate, envelope.requestID == currentRequestID,
              let status = envelope.deliveryStatus else { return }
        // A new instruction is being recorded or reviewed: the old one's news
        // must not take its screen away.
        switch stage {
        case .voiceConsent, .listening, .processing, .review: return
        default: break
        }
        let rank = Self.rank(status)
        guard rank >= statusRank else { return }
        statusRank = rank
        switch status {
        case .queued:
            guard stage == .sending || stage == .queuedForLater || stage == .sentToPhone
                    || stage == .waitingForKeyboard else { return }
            stage = phone?.keyboardSessionID == nil ? .waitingForKeyboard : .sentToPhone
        case .received, .generating:
            if stage != .generating {
                stage = .generating
                WKInterfaceDevice.current().play(.click)
            }
        case .ready:
            finishSuccessfully(requestID: envelope.requestID, inserted: false)
        case .inserted:
            finishSuccessfully(requestID: envelope.requestID, inserted: true)
        case .dismissed, .cancelled:
            pollTask?.cancel()
            reset()
        case .expired:
            wasAcknowledged = true
            fail(.expired)
        case .failed:
            wasAcknowledged = true
            fail(envelope.failure ?? .serviceUnavailable, limit: envelope.failureLimit)
        }
    }

    private static func rank(_ status: WatchVoiceDeliveryStatus) -> Int {
        switch status {
        case .queued:     return 0
        case .received:   return 1
        case .generating: return 2
        case .ready:      return 3
        case .inserted, .dismissed, .failed, .expired, .cancelled: return 4
        }
    }

    private func finishSuccessfully(requestID: String, inserted: Bool) {
        guard stage != .completed(inserted: inserted) else { return }
        pollTask?.cancel()
        recent = RecentResult(requestID: requestID, inserted: inserted, text: nil)
        stage = .completed(inserted: inserted)
        WKInterfaceDevice.current().play(.success)
    }

    private var isFailed: Bool {
        if case .failed = stage { return true }
        return false
    }

    private func fail(_ failure: WatchVoiceFailure, limit: Int? = nil) {
        pollTask?.cancel()
        transcriptionTimeout?.cancel()
        stage = .failed(failure, limit: limit)
        WKInterfaceDevice.current().play(.failure)
    }

    // MARK: Status while waiting

    /// A suspended phone app cannot push, so the watch asks - every three
    /// seconds, and for 45 without any news, only on screen. News starts the
    /// count again. Running out of patience invents no failure: the phone
    /// still has the request, and a Retry would pay for it twice.
    private func startPolling() {
        pollTask?.cancel()
        pollTask = Task { [weak self] in
            var quiet: TimeInterval = 0
            var seen = self?.stage
            while quiet < WatchVoiceDelivery.statusPollLimit {
                try? await Task.sleep(for: .seconds(WatchVoiceDelivery.statusPollInterval))
                quiet += WatchVoiceDelivery.statusPollInterval
                guard !Task.isCancelled, let self else { return }
                guard self.isForeground else {
                    self.pollTask = nil
                    return
                }
                await self.refreshStatus()
                guard !Task.isCancelled else { return }
                guard self.isAwaitingPhone else {
                    self.pollTask = nil
                    return
                }
                if self.stage != seen {
                    seen = self.stage
                    quiet = 0
                }
            }
            guard !Task.isCancelled, let self else { return }
            self.pollTask = nil
            if self.stage == .sentToPhone { self.stage = .waitingForKeyboard }
        }
    }

    private func resumePollingIfNeeded() {
        if isAwaitingPhone, pollTask == nil || pollTask?.isCancelled == true { startPolling() }
    }

    private var isAwaitingPhone: Bool {
        switch stage {
        case .sentToPhone, .generating, .waitingForKeyboard: return true
        default: return false
        }
    }

    // MARK: The user's other buttons

    /// Stops what is going on. An instruction not yet taken is taken back.
    func cancel() {
        if let id = currentRequestID, stage != .review {
            client.cancelQueued(requestID: id)
            let envelope = WatchVoiceEnvelope.request(.cancel, requestID: id)
            let client = self.client
            if client.isReachable {
                // Not answered: queued, so the phone still hears it.
                Task {
                    do { _ = try await client.request(envelope) } catch { try? client.enqueue(envelope) }
                }
            } else {
                try? client.enqueue(envelope)
            }
        }
        if stage == .processing {
            transcriptionTimeout?.cancel()
            discardRecording()
        }
        pollTask?.cancel()
        reset()
    }

    func clear() {
        draft = ""
    }

    func retry() {
        guard case .failed(let failure, _) = stage else { return }
        if let recording = currentRecording, failure.offersRetry {
            stage = .processing
            Task { await upload(recording) }
        } else if !draft.isEmpty {
            send()
        } else {
            reset()
        }
    }

    /// Done also lets go of a recording kept for Retry.
    func done() {
        pollTask?.cancel()
        transcriptionTimeout?.cancel()
        discardRecording()
        reset()
    }

    private func reset() {
        draft = ""
        currentRequestID = nil
        wasAcknowledged = false
        statusRank = -1
        stage = .ready
    }

    /// The text of the recent result, when it is still on the phone.
    func loadRecentResult() async {
        guard let recent, recent.text == nil, !recent.inserted, client.isReachable else { return }
        let request = WatchVoiceEnvelope.request(.resultRequest, requestID: recent.requestID)
        if let reply = try? await client.request(request), let text = reply.result {
            self.recent?.text = text
        }
    }
}
