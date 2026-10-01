import Foundation

/// `POST /api/v1/events`: one batch.
struct EventBatch: Encodable, Equatable, Sendable {
    struct Event: Encodable, Equatable, Sendable {
        let id: String
        let name: String
        let occurred_at: String
        let properties: [String: EventValue]
    }

    let installation_id: String
    let session_id: String
    let events: [Event]
}

/// The server's answer to a batch (202 Accepted).
struct EventBatchResult: Decodable, Equatable, Sendable {
    let accepted: Int
    let rejected: Int
    /// The server has telemetry switched off: stop sending.
    let disabled: Bool?

    init(accepted: Int, rejected: Int, disabled: Bool? = nil) {
        self.accepted = accepted
        self.rejected = rejected
        self.disabled = disabled
    }
}

/// Sends one batch. Throws `APIFailure`.
protocol EventTransport: Sendable {
    func send(_ batch: EventBatch, accessToken: String?) async throws -> EventBatchResult
}

/// Collects app events in memory and sends them in small batches.
///
/// Оқиғалар жадта жиналып, топпен жіберіледі; дискке ештеңе жазылмайды.
///
/// - Nothing is recorded before the legal consent (what was recorded before
///   it is dropped, never kept for later) or while the user's "Share
///   diagnostics" switch is off, and nothing is sent unless the server offers
///   telemetry (`features.telemetry`). Until the server and the consent are
///   known, events wait in memory; a "no" drops them.
/// - At most `maxQueue` events are kept; the oldest go first. Nothing is
///   written to disk: events not sent before the app is killed are lost,
///   which is the right trade for diagnostics.
/// - Up to 50 events per request, one session and one account per request,
///   every ~60 s and when the app goes to the background. A send in progress
///   is joined, never skipped, and runs in a task of its own, so stopping the
///   timer as the app leaves the screen cannot cancel a request on the wire.
/// - Each event remembers who was signed in when it happened. A batch carries
///   the access token only while that account is still the signed-in one, so
///   one account's events never land on another's.
/// - Network failures, 5xx and 429 keep the batch for later with a growing
///   pause; a cancelled request keeps it with no pause; any other 4xx means
///   the server will never take it, so it is dropped. A 401 is retried once
///   without the token, since the token is optional here.
@MainActor
final class EventReporter {

    struct Configuration: Sendable {
        var maxBatch = 50
        var maxQueue = 100
        /// Stays under the server's 64 KB body limit with room to spare.
        var maxBodyBytes = 60 * 1024
        var baseRetryDelay: TimeInterval = 60
        var maxRetryDelay: TimeInterval = 15 * 60
    }

    struct QueuedEvent: Equatable {
        let id: String
        let name: String
        let occurredAt: Date
        let properties: [String: EventValue]
        let sessionID: String
        /// Who was signed in when it happened: a user id, `signedIn` while
        /// the id is not known yet, `anonymous` for nobody.
        let accountKey: String
    }

    enum ServerSupport: Equatable {
        case unknown, supported, unsupported
    }

    /// `accountKey` values that are not a user id.
    nonisolated static let anonymous = "anon"
    nonisolated static let signedIn = "signed-in"

    private(set) var queue: [QueuedEvent] = []
    private(set) var serverSupport: ServerSupport = .unknown
    private(set) var userAllows: Bool
    /// The legal consent: nil until the app has said, false drops everything.
    private(set) var consent: Bool?

    /// Who is signed in right now; see `QueuedEvent.accountKey`.
    var accountKey: () -> String

    private let configuration: Configuration
    private let transport: EventTransport
    private let installationID: () -> String?
    private let sessionID: () -> String
    private let accessToken: () -> String?
    private let now: () -> Date
    private let makeID: () -> String

    /// The send in progress. A flush waits for it rather than racing it.
    private var inFlight: Task<Void, Never>?
    private var retryNotBefore: Date?
    private var consecutiveFailures = 0

    init(transport: EventTransport,
         userAllows: Bool,
         installationID: @escaping () -> String?,
         sessionID: @escaping () -> String,
         accountKey: @escaping () -> String = { EventReporter.anonymous },
         accessToken: @escaping () -> String?,
         configuration: Configuration = Configuration(),
         now: @escaping () -> Date = Date.init,
         makeID: @escaping () -> String = { UUID().uuidString.lowercased() }) {
        self.transport = transport
        self.userAllows = userAllows
        self.installationID = installationID
        self.sessionID = sessionID
        self.accountKey = accountKey
        self.accessToken = accessToken
        self.configuration = configuration
        self.now = now
        self.makeID = makeID
    }

    /// Whether events actually leave the phone right now.
    var isSending: Bool { serverSupport == .supported && userAllows && consent == true }

    // MARK: Switches

    func setServerSupport(_ supported: Bool?) {
        switch supported {
        case .some(true):
            serverSupport = .supported
        case .some(false):
            serverSupport = .unsupported
            queue.removeAll()
        case .none:
            // Not known yet (or the config request failed): keep waiting.
            if serverSupport != .supported { serverSupport = .unknown }
        }
    }

    func setUserAllows(_ allows: Bool) {
        userAllows = allows
        if !allows { queue.removeAll() }
    }

    /// The legal consent screen. Without it nothing is kept: events recorded
    /// before it are dropped, not sent once it is given.
    func setConsent(_ given: Bool) {
        consent = given
        if !given { queue.removeAll() }
    }

    // MARK: Recording

    func record(_ event: AppEvent) {
        guard userAllows, consent != false, serverSupport != .unsupported else { return }
        queue.append(QueuedEvent(id: makeID(), name: event.name, occurredAt: now(),
                                 properties: event.properties, sessionID: sessionID(),
                                 accountKey: accountKey()))
        if queue.count > configuration.maxQueue {
            queue.removeFirst(queue.count - configuration.maxQueue)
        }
    }

    // MARK: Sending

    /// Sends what is queued, batch by batch, until the queue is empty or a
    /// batch has to wait. Never throws and never blocks the caller's UI.
    func flush() async {
        // A send already in progress (the minute timer's, say) is joined, not
        // skipped: what was recorded meanwhile - app_backgrounded - still goes.
        while let running = inFlight {
            await running.value
        }
        guard isSending, !queue.isEmpty else { return }
        if let retryNotBefore, now() < retryNotBefore { return }
        guard let installationID = installationID() else { return }

        // A task of its own: cancelling whoever called (the timer, as the app
        // leaves the screen) must not cancel a request already on the wire.
        let task = Task { [weak self] in
            guard let self else { return }
            await self.drain(installationID: installationID)
            self.inFlight = nil
        }
        inFlight = task
        await task.value
    }

    private func drain(installationID: String) async {
        while isSending, !queue.isEmpty {
            let next = nextBatch(installationID: installationID)
            guard !next.ids.isEmpty else { return }

            switch await deliver(next.batch, accountKey: next.accountKey) {
            case .delivered(let disabled):
                consecutiveFailures = 0
                retryNotBefore = nil
                queue.removeAll { next.ids.contains($0.id) }
                if disabled {
                    serverSupport = .unsupported
                    queue.removeAll()
                }
            case .dropped:
                queue.removeAll { next.ids.contains($0.id) }
            case .interrupted:
                // Cancelled, not refused: the batch stays, with no pause.
                return
            case .retryLater(let after):
                consecutiveFailures += 1
                let backoff = min(configuration.baseRetryDelay * pow(2, Double(consecutiveFailures - 1)),
                                  configuration.maxRetryDelay)
                retryNotBefore = now().addingTimeInterval(max(backoff, TimeInterval(after ?? 0)))
                return
            }
        }
    }

    private enum Delivery {
        case delivered(disabled: Bool)
        case dropped
        case interrupted
        case retryLater(after: Int?)
    }

    private func deliver(_ batch: EventBatch, accountKey batchAccount: String) async -> Delivery {
        // Attributed to an account only while it is still the one signed in.
        var token = batchAccount != Self.anonymous && batchAccount == accountKey() ? accessToken() : nil
        for _ in 0..<2 {
            do {
                let result = try await transport.send(batch, accessToken: token)
                return .delivered(disabled: result.disabled == true)
            } catch let failure as APIFailure {
                if failure.error == .cancelled { return .interrupted }
                if failure.status == 401, token != nil {
                    // The token is optional here; send the batch without it.
                    token = nil
                    continue
                }
                return Self.isRetryable(failure) ? .retryLater(after: failure.retryAfter) : .dropped
            } catch is CancellationError {
                return .interrupted
            } catch {
                return .retryLater(after: nil)
            }
        }
        return .dropped
    }

    /// No response, a server error or rate limiting: try again later.
    /// Any other answer is final for this batch.
    nonisolated static func isRetryable(_ failure: APIFailure) -> Bool {
        failure.status == 0 || failure.status == 408 || failure.status == 429 || failure.status >= 500
    }

    /// The oldest events that share a session and an account, up to 50 and
    /// under the body limit.
    private func nextBatch(installationID: String) -> (batch: EventBatch, ids: [String], accountKey: String) {
        guard let first = queue.first else {
            return (EventBatch(installation_id: installationID, session_id: "", events: []), [], Self.anonymous)
        }
        var picked = Array(queue.filter { $0.sessionID == first.sessionID && $0.accountKey == first.accountKey }
            .prefix(configuration.maxBatch))
        var batch = Self.batch(picked, installationID: installationID, sessionID: first.sessionID)
        while picked.count > 1,
              let size = try? JSONEncoder().encode(batch).count, size > configuration.maxBodyBytes {
            picked.removeLast(picked.count / 2)
            batch = Self.batch(picked, installationID: installationID, sessionID: first.sessionID)
        }
        return (batch, picked.map(\.id), first.accountKey)
    }

    private static func batch(_ events: [QueuedEvent], installationID: String, sessionID: String) -> EventBatch {
        EventBatch(
            installation_id: installationID,
            session_id: sessionID,
            events: events.map {
                EventBatch.Event(id: $0.id, name: $0.name,
                                 occurred_at: timestamp($0.occurredAt), properties: $0.properties)
            }
        )
    }

    /// RFC 3339 in UTC with milliseconds ("2026-10-01T09:30:00.125Z"), which
    /// Go's RFC3339Nano parser reads. A few events a minute: a formatter per
    /// call costs nothing worth sharing one across threads for.
    nonisolated static func timestamp(_ date: Date) -> String {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        formatter.timeZone = TimeZone(secondsFromGMT: 0)
        return formatter.string(from: date)
    }
}
