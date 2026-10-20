import Foundation

/// One user action's `Idempotency-Key` (contract §3.3).
///
/// Бір әрекетке бір кілт: Reply, «Создать», «Другой вариант».
///
/// The app and the keyboard make one for every tap on Reply, Create or
/// Regenerate, and send it again only when they retry that same request by
/// themselves. A tap is a new action and gets a new key, so tapping again is
/// paid again; a retry the user never asked for never is.
struct IdempotencyKey: Equatable, Hashable, Sendable {

    static let header = "Idempotency-Key"

    let value: String

    /// A UUID: 36 characters of letters, digits and dashes, inside the
    /// server's `^[A-Za-z0-9-]{8,64}$`.
    init(_ value: String = UUID().uuidString) {
        self.value = value
    }

    /// A key for one action, or nil when the server does not take keys
    /// (`features.idempotent_generation`): then nothing is retried either.
    static func forAction(serverSupportsKeys: Bool = AILimits.serverSupportsIdempotentGeneration) -> IdempotencyKey? {
        serverSupportsKeys ? IdempotencyKey() : nil
    }
}

/// Sends one generation, retrying it by itself only where the key makes
/// that safe.
///
/// Қайталау тек сол кілтпен: сервер екінші рет ақы алмайды.
///
/// With a key, a request whose answer was lost (the connection dropped) is
/// sent once more with the same key: the server either finishes the one it
/// has (a replay is not charged again) or, when the first was refunded,
/// takes it as new. While the first one is still running the server answers
/// `in_progress`, and the client waits and asks again a couple of times.
/// A key already `used` is never retried. Without a key nothing is retried,
/// because each request would be paid.
enum IdempotentGeneration {

    /// The first request and at most two automatic retries.
    static let maximumAttempts = 3

    enum Step: Equatable {
        case retry(after: Duration)
        case fail
    }

    /// What to do after attempt number `attempt` (1-based) failed.
    static func step(after error: APIError, attempt: Int, key: IdempotencyKey?) -> Step {
        guard key != nil, attempt < maximumAttempts else { return .fail }
        switch error {
        // The request may have reached the server; once is enough to find out.
        case .offline:
            return attempt == 1 ? .retry(after: .seconds(1)) : .fail
        // Our own first attempt is still being written.
        case .generationInProgress:
            return .retry(after: .seconds(2))
        default:
            return .fail
        }
    }

    static func headers(for key: IdempotencyKey?) -> [String: String] {
        key.map { [IdempotencyKey.header: $0.value] } ?? [:]
    }

    /// Runs `send` with the key's headers, again with the same headers when
    /// `step` says so. Throws the last `APIError`.
    static func run<Response: Sendable>(
        key: IdempotencyKey?,
        sleep: @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) },
        send: @Sendable ([String: String]) async throws -> Response
    ) async throws -> Response {
        let headers = headers(for: key)
        var attempt = 1
        while true {
            do {
                return try await send(headers)
            } catch let error as APIError {
                guard case .retry(let delay) = step(after: error, attempt: attempt, key: key) else { throw error }
                try await sleep(delay)
                try Task.checkCancellation()
                attempt += 1
            }
        }
    }
}
