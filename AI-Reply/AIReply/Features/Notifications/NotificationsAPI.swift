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

    struct SaveRequest: Encodable, Sendable {
        let preferences: [String: Bool]
        let expectedUserID: String?

        enum CodingKeys: String, CodingKey {
            case preferences
            case expectedUserID = "expected_user_id"
        }
    }

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
        try await save(changes, expectedUserID: nil)
    }

    func save(_ changes: [String: Bool], expectedUserID: String?) async throws -> NotificationPreferences {
        // Keep the original account in the body across token awaits/retries.
        // The server refuses a different authenticated owner before mutation.
        let request = SaveRequest(preferences: changes, expectedUserID: expectedUserID)
        return try await session.authenticated { token in
            try await NotificationsAPI.client().put("api/v1/me/notification-preferences", body: request, token: token)
        }
    }
}

/// A short-lived, single-use website login issued for the tapped offer.
/// The opaque URL is never logged or persisted by the app.
struct WebPaymentHandoff: Decodable, Equatable, Sendable {
    let url: String
    let expiresIn: Int

    enum CodingKeys: String, CodingKey {
        case url
        case expiresIn = "expires_in"
    }
}

protocol WebPaymentHandoffProviding: Sendable {
    func create(expectedUserID: String, locale: String) async throws -> WebPaymentHandoff
}

struct BackendWebPaymentHandoffProvider: WebPaymentHandoffProviding {
    struct Request: Encodable, Sendable {
        let expectedUserID: String
        let locale: String

        enum CodingKeys: String, CodingKey {
            case expectedUserID = "expected_user_id"
            case locale
        }
    }

    private let session: AccountSession

    init(session: AccountSession = .shared) { self.session = session }

    func create(expectedUserID: String, locale: String) async throws -> WebPaymentHandoff {
        let request = Request(expectedUserID: expectedUserID, locale: locale)
        return try await session.authenticated { token in
            try await NotificationsAPI.client().post("api/v1/web/payment-handoff", body: request, token: token)
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
