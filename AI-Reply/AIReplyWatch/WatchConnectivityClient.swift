import Foundation
import WatchConnectivity

/// WatchConnectivity on the watch: activation, the fast channel with an
/// acknowledgement, and the queue for when the phone cannot be reached.
///
/// The watch cannot generate anything by itself and holds no account: every
/// request goes to the phone, which owns the session with the server.
final class WatchConnectivityClient: NSObject, WCSessionDelegate, @unchecked Sendable {

    enum Event: Sendable {
        case activated
        case reachabilityChanged
        case envelope(WatchVoiceEnvelope)
    }

    enum ClientError: Error {
        case notReachable
        case timedOut
        case malformedReply
    }

    /// Called on an arbitrary queue.
    var onEvent: (@Sendable (Event) -> Void)?

    private var session: WCSession { WCSession.default }

    func start() {
        guard WCSession.isSupported() else { return }
        session.delegate = self
        session.activate()
    }

    /// True only while the phone can be messaged right now; the phone app
    /// itself need not be running, because a message wakes it.
    var isReachable: Bool {
        session.activationState == .activated && session.isReachable
    }

    /// The phone's last published state, available with no connection.
    var lastPhoneSnapshot: WatchPhoneSnapshot? {
        WatchVoiceCodec.envelope(from: session.receivedApplicationContext)?.phone
    }

    // MARK: Sending

    /// Sends and waits for the phone's answer. Fails on a timeout even if
    /// the message did get through, which the phone's duplicate check covers.
    func request(_ envelope: WatchVoiceEnvelope) async throws -> WatchVoiceEnvelope {
        guard isReachable else { throw ClientError.notReachable }
        let payload = try WatchVoiceCodec.dictionary(envelope)
        return try await withCheckedThrowingContinuation { continuation in
            let reply = SingleReply(continuation)
            DispatchQueue.global().asyncAfter(deadline: .now() + WatchVoiceDelivery.acknowledgementTimeout) {
                reply.resume(throwing: ClientError.timedOut)
            }
            session.sendMessage(payload, replyHandler: { answer in
                if let decoded = WatchVoiceCodec.envelope(from: answer) {
                    reply.resume(returning: decoded)
                } else {
                    reply.resume(throwing: ClientError.malformedReply)
                }
            }, errorHandler: { reply.resume(throwing: $0) })
        }
    }

    /// A recording or one part of it, on the fast channel, which wakes the
    /// phone app. The phone acknowledges at once and the text comes later.
    func sendAudio(_ packet: Data) async throws -> WatchVoiceEnvelope {
        guard isReachable else { throw ClientError.notReachable }
        return try await withCheckedThrowingContinuation { continuation in
            let reply = SingleReply(continuation)
            DispatchQueue.global().asyncAfter(deadline: .now() + WatchVoiceDelivery.audioPartTimeout) {
                reply.resume(throwing: ClientError.timedOut)
            }
            session.sendMessageData(packet, replyHandler: { answer in
                if let decoded = WatchVoiceCodec.value(WatchVoiceEnvelope.self, from: answer) {
                    reply.resume(returning: decoded)
                } else {
                    reply.resume(throwing: ClientError.malformedReply)
                }
            }, errorHandler: { reply.resume(throwing: $0) })
        }
    }

    /// Queued delivery. The system sends this when it can, in order, even
    /// if this app is no longer running. It is not real time and the screen
    /// never says it was sent.
    func enqueue(_ envelope: WatchVoiceEnvelope) throws {
        session.transferUserInfo(try WatchVoiceCodec.dictionary(envelope))
    }

    /// Takes back whatever is still waiting in the system's queue for this request.
    func cancelQueued(requestID: String) {
        for transfer in session.outstandingUserInfoTransfers {
            if WatchVoiceCodec.envelope(from: transfer.userInfo)?.requestID == requestID { transfer.cancel() }
        }
        for transfer in session.outstandingFileTransfers {
            if let metadata = transfer.file.metadata,
               WatchVoiceCodec.envelope(from: metadata)?.requestID == requestID { transfer.cancel() }
        }
    }

    // MARK: WCSessionDelegate

    func session(_ session: WCSession, activationDidCompleteWith activationState: WCSessionActivationState, error: Error?) {
        onEvent?(.activated)
    }

    func sessionReachabilityDidChange(_ session: WCSession) {
        onEvent?(.reachabilityChanged)
    }

    func session(_ session: WCSession, didReceiveMessage message: [String: Any]) {
        deliver(message)
    }

    func session(_ session: WCSession, didReceiveMessage message: [String: Any], replyHandler: @escaping ([String: Any]) -> Void) {
        // "Test connection" on the phone: answered from here, at once.
        if let envelope = WatchVoiceCodec.envelope(from: message), envelope.type == .ping {
            let pong = WatchVoiceEnvelope.request(.pong, requestID: envelope.requestID)
            replyHandler((try? WatchVoiceCodec.dictionary(pong)) ?? [:])
            return
        }
        replyHandler([:])
        deliver(message)
    }

    func session(_ session: WCSession, didReceiveUserInfo userInfo: [String: Any] = [:]) {
        deliver(userInfo)
    }

    func session(_ session: WCSession, didReceiveApplicationContext applicationContext: [String: Any]) {
        deliver(applicationContext)
    }

    private func deliver(_ dictionary: [String: Any]) {
        guard let envelope = WatchVoiceCodec.envelope(from: dictionary),
              envelope.version == WatchVoice.protocolVersion else { return }
        onEvent?(.envelope(envelope))
    }
}

/// Resumes a continuation once, whichever of the reply, the error and the
/// timeout comes first. A later one is ignored instead of crashing.
private final class SingleReply<Value: Sendable>: @unchecked Sendable {

    private let lock = NSLock()
    private var continuation: CheckedContinuation<Value, Error>?

    init(_ continuation: CheckedContinuation<Value, Error>) {
        self.continuation = continuation
    }

    func resume(returning value: Value) {
        take()?.resume(returning: value)
    }

    func resume(throwing error: Error) {
        take()?.resume(throwing: error)
    }

    private func take() -> CheckedContinuation<Value, Error>? {
        lock.lock()
        defer { lock.unlock() }
        defer { continuation = nil }
        return continuation
    }
}
