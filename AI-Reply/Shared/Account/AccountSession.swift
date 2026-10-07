import Foundation

/// Owns the token pair and hands out a usable access token.
///
/// Access токен 15 минут жарамды; ескіргенде бір рет қана жаңартылады.
///
/// Why an actor: the app and the keyboard can both ask for a token at the same
/// moment, and two parallel refreshes would rotate the refresh token twice -
/// the server treats the second use of a rotated token as theft and kills the
/// whole session. Serialising through one actor, with a single in-flight
/// refresh task, is what makes that impossible.
actor AccountSession {

    static let shared = AccountSession()

    private var refreshTask: Task<String, Error>?

    /// Whether a session exists at all. Cheap: reads the keychain, no network.
    nonisolated var isSignedIn: Bool { AccountCredentials.isSignedIn }

    /// Base URL of our service. Nil when the app has not been pointed at one.
    nonisolated var baseURL: URL? { AIConfiguration.shared.backendBaseURL }

    /// A token that is valid right now, refreshing first when needed.
    func accessToken() async throws -> String {
        if AccountCredentials.isAccessTokenFresh, let token = AccountCredentials.accessToken {
            return token
        }
        return try await refreshAccessToken()
    }

    /// Forces a refresh - used after a 401 on a token we believed was fresh.
    func refreshAccessToken() async throws -> String {
        if let existing = refreshTask {
            return try await existing.value
        }
        let task = Task<String, Error> { [self] in
            defer { Task { await self.clearRefreshTask() } }
            return try await self.performRefresh()
        }
        refreshTask = task
        return try await task.value
    }

    private func clearRefreshTask() { refreshTask = nil }

    private func performRefresh() async throws -> String {
        guard let baseURL else { throw APIError.invalidRequest }
        guard let refreshToken = AccountCredentials.refreshToken else {
            throw APIError.unauthorized
        }

        struct Request: Encodable {
            let refresh_token: String
            let device: DeviceDescriptor
        }
        struct Response: Decodable {
            let accessToken: String
            let refreshToken: String
            let expiresIn: Int

            enum CodingKeys: String, CodingKey {
                case accessToken = "access_token"
                case refreshToken = "refresh_token"
                case expiresIn = "expires_in"
            }
        }

        let client = APIClient(baseURL: baseURL)
        do {
            let response: Response = try await client.post(
                "api/v1/auth/refresh",
                body: Request(refresh_token: refreshToken, device: .current)
            )
            AccountCredentials.store(accessToken: response.accessToken,
                                     refreshToken: response.refreshToken,
                                     expiresIn: response.expiresIn)
            return response.accessToken
        } catch APIError.unauthorized {
            // The refresh token is gone, rotated or revoked. Nothing local can
            // fix that, so the session is cleared and the UI asks for sign-in.
            AccountCredentials.clear()
            throw APIError.unauthorized
        }
    }

    /// Stores a freshly issued session.
    func adopt(_ session: AccountAPI.Session) {
        AccountCredentials.store(accessToken: session.accessToken,
                                 refreshToken: session.refreshToken,
                                 expiresIn: session.expiresIn)
        AccountCredentials.setDeviceID(session.deviceID)
        AccountCredentials.setDisplayIdentifier(session.user.identifier)
    }

    /// Ends the session. The server call is best effort: the local tokens are
    /// dropped either way, so a user on a plane can still sign out.
    ///
    /// `installationID` names this phone's installation on the logout request
    /// (and only there), so the server stops sending the account's
    /// notifications to it at once.
    func signOut(installationID: String? = nil) async {
        let refreshToken = AccountCredentials.refreshToken
        AccountCredentials.clear()
        refreshTask?.cancel()
        refreshTask = nil

        guard let baseURL, let refreshToken else { return }
        struct Request: Encodable { let refresh_token: String }
        let headers = installationID.map { ["X-Installation-ID": $0] } ?? [:]
        let client = APIClient(baseURL: baseURL, headers: headers)
        let _: APIClient.Empty? = try? await client.post("api/v1/auth/logout",
                                                         body: Request(refresh_token: refreshToken))
    }

    /// Runs an authenticated call, refreshing once if the server rejects the
    /// token. One retry, never a loop.
    func authenticated<Response: Decodable & Sendable>(
        _ work: @Sendable (String) async throws -> Response
    ) async throws -> Response {
        let token = try await accessToken()
        do {
            return try await work(token)
        } catch APIError.unauthorized {
            let fresh = try await refreshAccessToken()
            return try await work(fresh)
        }
    }
}

/// What the client tells the server about itself.
///
/// Deliberately minimal: platform, app version, OS version, locale, time zone.
/// No device name, no model identifier, no advertising id, no carrier.
struct DeviceDescriptor: Encodable, Sendable {
    let device_id: String
    let platform: String
    let app_version: String
    let os_version: String
    let locale: String
    let timezone: String

    static var current: DeviceDescriptor {
        let version = ProcessInfo.processInfo.operatingSystemVersion
        return DeviceDescriptor(
            device_id: AccountCredentials.deviceID,
            platform: "ios",
            app_version: Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "",
            os_version: "\(version.majorVersion).\(version.minorVersion).\(version.patchVersion)",
            // One of the app's own languages, never a code the server does not know.
            locale: SharedSettings.shared.effectiveAppLanguage.rawValue,
            timezone: TimeZone.current.identifier
        )
    }
}
