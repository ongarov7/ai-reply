import Foundation

/// The notification and telemetry endpoints, over the same `APIClient`.
///
/// Push, орнату және оқиға сұраныстары.
///
/// One URL session for all of them, rather than one per request: these run
/// in the background of the app's life, several times a session.
enum NotificationsAPI {

    static let session = ReplyNetworking.makeSession(timeout: 20)

    static func client() throws -> APIClient {
        guard let baseURL = AIConfiguration.shared.backendBaseURL else {
            throw APIFailure(error: .invalidRequest, requestID: "", status: 0)
        }
        return APIClient(baseURL: baseURL, session: session)
    }
}

struct BackendInstallationTransport: InstallationTransport {
    func register(_ payload: InstallationPayload, accessToken: String?) async throws -> InstallationResponse {
        try await NotificationsAPI.client().perform("POST", "api/v1/installations", body: payload, token: accessToken)
    }
}

struct BackendEventTransport: EventTransport {
    func send(_ batch: EventBatch, accessToken: String?) async throws -> EventBatchResult {
        try await NotificationsAPI.client().perform("POST", "api/v1/events", body: batch, token: accessToken)
    }
}

/// `GET` and `PUT /api/v1/me/notification-preferences`, with the session's
/// usual refresh-once-and-retry.
struct BackendNotificationPreferencesService: NotificationPreferencesService {

    private let session: AccountSession

    init(session: AccountSession = .shared) {
        self.session = session
    }

    func load() async throws -> NotificationPreferences {
        try await session.authenticated { token in
            try await Self.client().get("api/v1/me/notification-preferences", token: token)
        }
    }

    func save(_ changes: [String: Bool]) async throws -> NotificationPreferences {
        struct Request: Encodable, Sendable { let preferences: [String: Bool] }
        let request = Request(preferences: changes)
        return try await session.authenticated { token in
            try await Self.client().put("api/v1/me/notification-preferences", body: request, token: token)
        }
    }

    /// Errors as `APIError`, which is what `AccountSession.authenticated`
    /// recognises a rejected token by.
    private static func client() throws -> APIClient {
        guard let baseURL = AIConfiguration.shared.backendBaseURL else { throw APIError.invalidRequest }
        return APIClient(baseURL: baseURL, session: NotificationsAPI.session)
    }
}
