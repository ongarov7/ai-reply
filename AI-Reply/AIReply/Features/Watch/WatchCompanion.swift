import UIKit
import WatchConnectivity

/// The iPhone's end of WatchConnectivity.
///
/// Телефондағы сағат байланысы: хабарды қабылдайды, файлға жазады, жауап береді.
///
/// It lives in the containing app because a keyboard extension is not where
/// WatchConnectivity works. A message from the watch wakes this app in the
/// background (Apple's documented behaviour for `sendMessage` from an active
/// watch app); it writes the command to the App Group and tells the keyboard
/// with a Darwin notification. No reply is generated here: the keyboard does
/// that, through the same code as a tap on Reply.
///
/// Every delegate method hands over to one serial queue, so state here is
/// never touched from two threads.
final class WatchCompanion: NSObject, WCSessionDelegate, @unchecked Sendable {

    static let shared = WatchCompanion()

    /// The link changed; the settings screen listens.
    var onLinkChange: (@Sendable () -> Void)?

    private let store: WatchVoiceStore
    private let core: WatchCompanionCore
    private let transcriber = WatchTranscriber()
    private let queue = DispatchQueue(label: "kz.ai-reply.watch-companion", qos: .userInitiated)

    private var observer: WatchVoiceSignalObserver?
    private var lastContactAt: Date?
    private var lastContext: Data?
    /// What the watch has been told about each request.
    private var pushed: [String: WatchVoiceDeliveryStatus] = [:]

    private var backgroundTask = UIBackgroundTaskIdentifier.invalid
    private var backgroundRelease: DispatchWorkItem?

    /// How long the app stays up after a message, to relay the keyboard's
    /// progress; when iOS suspends it sooner the watch simply asks instead.
    private static let relayWindow: TimeInterval = 25

    private override init() {
        let store = WatchVoiceStore.shared
        self.store = store
        self.core = WatchCompanionCore(store: store,
                                       snapshot: { WatchPhoneSnapshot.current(store: store, now: $0) },
                                       notifyKeyboard: { WatchVoiceSignal.command.post() })
        super.init()
    }

    private var session: WCSession { WCSession.default }

    func start() {
        guard WCSession.isSupported() else {
            store.writeLink(WatchLinkSnapshot(isSupported: false, updatedAt: Date()))
            return
        }
        lastContactAt = store.readLink()?.lastWatchContactAt
        observer = WatchVoiceSignalObserver(signals: [.outcome, .session]) { [weak self] signal in
            self?.queue.async { self?.keyboardSignalled(signal) }
        }
        session.delegate = self
        session.activate()
    }

    // MARK: Link state

    private func publishLink(contact: Bool = false) {
        if contact { lastContactAt = Date() }
        guard WCSession.isSupported() else { return }
        let link = WatchLinkSnapshot(
            isSupported: true,
            isActivated: session.activationState == .activated,
            isPaired: session.isPaired,
            isWatchAppInstalled: session.isWatchAppInstalled,
            isReachable: session.isReachable,
            updatedAt: Date(),
            lastWatchContactAt: lastContactAt
        )
        store.writeLink(link)
        WatchVoiceSignal.link.post()
        onLinkChange?()
    }

    /// "Test connection" in Settings: how long the watch took to answer.
    /// Only for a watch that has the app open: the phone cannot wake it.
    func ping() async -> TimeInterval? {
        guard session.activationState == .activated, session.isReachable else { return nil }
        let started = Date()
        let envelope = WatchVoiceEnvelope.request(.ping, source: .phone)
        guard let payload = try? WatchVoiceCodec.dictionary(envelope) else { return nil }
        return await withCheckedContinuation { continuation in
            let once = OnceFlag()
            DispatchQueue.global().asyncAfter(deadline: .now() + 6) {
                if once.take() { continuation.resume(returning: nil) }
            }
            session.sendMessage(payload, replyHandler: { _ in
                if once.take() { continuation.resume(returning: Date().timeIntervalSince(started)) }
            }, errorHandler: { _ in
                if once.take() { continuation.resume(returning: nil) }
            })
        }
    }

    // MARK: Background time

    /// Keeps the app running a little after a message, and no longer.
    private func holdBackgroundTime(_ seconds: TimeInterval) {
        DispatchQueue.main.async { [self] in
            if backgroundTask == .invalid {
                backgroundTask = UIApplication.shared.beginBackgroundTask(withName: "watch-voice") { [weak self] in
                    self?.releaseBackgroundTime()
                }
            }
            backgroundRelease?.cancel()
            let release = DispatchWorkItem { [weak self] in self?.releaseBackgroundTime() }
            backgroundRelease = release
            DispatchQueue.main.asyncAfter(deadline: .now() + seconds, execute: release)
        }
    }

    private func releaseBackgroundTime() {
        dispatchPrecondition(condition: .onQueue(.main))
        backgroundRelease?.cancel()
        backgroundRelease = nil
        guard backgroundTask != .invalid else { return }
        UIApplication.shared.endBackgroundTask(backgroundTask)
        backgroundTask = .invalid
    }

    // MARK: Incoming

    private func incoming(_ dictionary: [String: Any], reply: (([String: Any]) -> Void)?) {
        queue.async { [self] in
            guard let envelope = WatchVoiceCodec.envelope(from: dictionary) else {
                let refusal = WatchVoiceEnvelope.refusal(requestID: WatchVoiceIdentifier.make(), failure: .unsupportedVersion)
                reply?((try? WatchVoiceCodec.dictionary(refusal)) ?? [:])
                return
            }
            publishLink(contact: true)
            holdBackgroundTime(Self.relayWindow)
            let answer = core.handle(envelope)
            if let reply {
                reply((try? WatchVoiceCodec.dictionary(answer)) ?? [:])
            } else if envelope.type == .command || envelope.type == .cancel {
                // Came through the queue: nobody is waiting for an answer,
                // but the watch still has to hear how it went.
                deliver(answer, queueIfUnreachable: answer.deliveryStatus == .failed)
            }
        }
    }

    private func incomingAudio(_ audio: Data, envelope: WatchVoiceEnvelope) {
        publishLink(contact: true)
        holdBackgroundTime(WatchTranscriber.timeout + 5)
        guard envelope.type == .transcribe else {
            sendTranscript(requestID: envelope.requestID, result: .failure(.audioRejected))
            return
        }
        if let problem = WatchVoiceValidator.problem(with: envelope) {
            sendTranscript(requestID: envelope.requestID, result: .failure(problem))
            return
        }
        guard let language = envelope.language, let milliseconds = envelope.audioMilliseconds else {
            sendTranscript(requestID: envelope.requestID, result: .failure(.audioRejected))
            return
        }
        let transcriber = self.transcriber
        Task.detached(priority: .userInitiated) { [weak self] in
            let result = await transcriber.transcribe(audio: audio, milliseconds: milliseconds,
                                                      requestID: envelope.requestID, language: language)
            self?.queue.async { self?.sendTranscript(requestID: envelope.requestID, result: result) }
        }
    }

    private func sendTranscript(requestID: String, result: Result<String, WatchVoiceFailure>) {
        var envelope = WatchVoiceEnvelope(requestID: requestID, type: .transcript, createdAt: Date(), source: .phone)
        switch result {
        case .success(let text): envelope.instruction = text
        case .failure(let failure): envelope.failure = failure
        }
        deliver(envelope, queueIfUnreachable: true)
    }

    // MARK: Outgoing

    private func deliver(_ envelope: WatchVoiceEnvelope, queueIfUnreachable: Bool) {
        guard session.activationState == .activated,
              let payload = try? WatchVoiceCodec.dictionary(envelope) else { return }
        guard session.isReachable else {
            if queueIfUnreachable { session.transferUserInfo(payload) }
            return
        }
        session.sendMessage(payload, replyHandler: nil) { [weak self] _ in
            guard queueIfUnreachable else { return }
            self?.queue.async { self?.session.transferUserInfo(payload) }
        }
    }

    private func keyboardSignalled(_ signal: WatchVoiceSignal) {
        switch signal {
        case .outcome: pushOutcomes()
        case .session: publishContext()
        case .command, .link: break
        }
    }

    /// Tells the watch whatever moved on since it was last told.
    private func pushOutcomes() {
        for outcome in store.outcomes() where pushed[outcome.id] != outcome.status {
            pushed[outcome.id] = outcome.status
            let update = WatchVoiceEnvelope.status(requestID: outcome.id, status: outcome.status,
                                                   failure: outcome.failure, failureLimit: outcome.failureLimit,
                                                   phone: WatchPhoneSnapshot.current(store: store))
            deliver(update, queueIfUnreachable: outcome.status.isFinal || outcome.status == .ready)
        }
        if pushed.count > 32 { pushed = pushed.filter { store.outcome(for: $0.key) != nil } }
    }

    /// The phone's state as application context: only the latest value
    /// matters, and the system delivers it without a live connection.
    private func publishContext() {
        guard session.activationState == .activated, session.isPaired, session.isWatchAppInstalled else { return }
        let snapshot = WatchPhoneSnapshot.current(store: store)
        guard let encoded = try? WatchVoiceCodec.data(snapshot), encoded != lastContext else { return }
        let envelope = WatchVoiceEnvelope.status(requestID: WatchVoiceIdentifier.make(), status: nil, phone: snapshot)
        guard let payload = try? WatchVoiceCodec.dictionary(envelope) else { return }
        lastContext = encoded
        try? session.updateApplicationContext(payload)
    }

    // MARK: WCSessionDelegate

    func session(_ session: WCSession, activationDidCompleteWith activationState: WCSessionActivationState, error: Error?) {
        queue.async { [self] in
            publishLink()
            publishContext()
        }
    }

    func sessionDidBecomeInactive(_ session: WCSession) {}

    /// The user switched to another watch: a new session for it.
    func sessionDidDeactivate(_ session: WCSession) {
        session.activate()
    }

    func sessionWatchStateDidChange(_ session: WCSession) {
        queue.async { [self] in
            publishLink()
            publishContext()
        }
    }

    func sessionReachabilityDidChange(_ session: WCSession) {
        queue.async { [self] in publishLink() }
    }

    func session(_ session: WCSession, didReceiveMessage message: [String: Any], replyHandler: @escaping ([String: Any]) -> Void) {
        incoming(message, reply: replyHandler)
    }

    func session(_ session: WCSession, didReceiveMessage message: [String: Any]) {
        incoming(message, reply: nil)
    }

    func session(_ session: WCSession, didReceiveUserInfo userInfo: [String: Any] = [:]) {
        incoming(userInfo, reply: nil)
    }

    /// A short recording on the fast channel. Answered at once: the text
    /// comes as its own message when the server has it.
    func session(_ session: WCSession, didReceiveMessageData messageData: Data, replyHandler: @escaping (Data) -> Void) {
        queue.async { [self] in
            guard let packet = WatchVoiceAudioPacket.decode(messageData) else {
                let refusal = WatchVoiceEnvelope.refusal(requestID: WatchVoiceIdentifier.make(), failure: .audioRejected)
                replyHandler((try? WatchVoiceCodec.data(refusal)) ?? Data())
                return
            }
            let accepted = WatchVoiceEnvelope.status(requestID: packet.envelope.requestID, status: .received,
                                                     phone: WatchPhoneSnapshot.current(store: store))
            replyHandler((try? WatchVoiceCodec.data(accepted)) ?? Data())
            incomingAudio(packet.audio, envelope: packet.envelope)
        }
    }

    /// A longer recording. The system deletes the file when this returns, so
    /// it is read here, before anything else.
    func session(_ session: WCSession, didReceive file: WCSessionFile) {
        guard let metadata = file.metadata, let envelope = WatchVoiceCodec.envelope(from: metadata) else { return }
        let audio = try? Data(contentsOf: file.fileURL)
        queue.async { [self] in
            guard let audio, audio.count <= WatchVoice.maximumAudioBytes else {
                sendTranscript(requestID: envelope.requestID, result: .failure(.audioRejected))
                return
            }
            incomingAudio(audio, envelope: envelope)
        }
    }
}

/// True for the first caller only.
private final class OnceFlag: @unchecked Sendable {
    private let lock = NSLock()
    private var taken = false

    func take() -> Bool {
        lock.lock()
        defer { lock.unlock() }
        defer { taken = true }
        return !taken
    }
}
