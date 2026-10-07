import Foundation

/// The notification endpoints, over the same `APIClient`.
///
/// Орнату, хабарлама баптаулары және ашылған хабарламалар.
///
/// One URL session for all of them, rather than one per request: these run
/// in the background of the app's life, several times a session. Every call
/// throws `APIError`.
enum NotificationsAPI {

    static let session = ReplyNetworking.makeSession(timeout: 20)

    static func client() throws -> APIClient {
        guard let baseURL = AIConfiguration.shared.backendBaseURL else { throw APIError.invalidRequest }
        return APIClient(baseURL: baseURL, session: session)
    }
}

/// `POST /api/v1/installations`, with the account's access token when there
/// is a session.
struct BackendInstallationTransport: InstallationTransport {
    func register(_ payload: InstallationPayload, accessToken: String?) async throws -> InstallationResponse {
        try await NotificationsAPI.client().post("api/v1/installations", body: payload, token: accessToken)
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
            try await NotificationsAPI.client().get("api/v1/me/notification-preferences", token: token)
        }
    }

    func save(_ changes: [String: Bool]) async throws -> NotificationPreferences {
        struct Request: Encodable, Sendable { let preferences: [String: Bool] }
        let request = Request(preferences: changes)
        return try await session.authenticated { token in
            try await NotificationsAPI.client().put("api/v1/me/notification-preferences", body: request, token: token)
        }
    }
}

/// Tells the server a notification was opened.
protocol NotificationOpenedReporting: Sendable {
    func reportOpened(installationID: String, deliveryID: String) async throws
}

/// `POST /api/v1/notifications/opened`. No access token: the server checks
/// the delivery against the installation, which is all it needs, and an
/// expired token would only turn the call into a 401.
struct BackendNotificationOpenedReporter: NotificationOpenedReporting {
    func reportOpened(installationID: String, deliveryID: String) async throws {
        struct Request: Encodable, Sendable {
            let installation_id: String
            let delivery_id: String
        }
        let _: APIClient.Empty = try await NotificationsAPI.client().post(
            "api/v1/notifications/opened",
            body: Request(installation_id: installationID, delivery_id: deliveryID))
    }
}
