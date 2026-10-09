import Foundation

/// Owns the token pair and hands out a usable access token.
///
/// Access токен 15 минут жарамды; ескіргенде бір рет қана жаңартылады.
///
/// Why an actor: two parallel refreshes would rotate the refresh token twice -
/// the server treats the second use of a rotated token as theft and kills the
/// whole session. Within one process the actor, with a single in-flight
/// refresh task, makes that impossible. The app and the keyboard are two
/// processes, though, each with its own actor: a file lock in the App Group
/// serialises them, and the second one to get it uses the pair the first one
/// just stored instead of spending the old refresh token again.
actor AccountSession {

    static let shared = AccountSession()

    private var refreshTask: Task<String, Error>?
    private let refreshLock: CrossProcessLock

    init(refreshLock: CrossProcessLock = .accountRefresh) {
        self.refreshLock = refreshLock
    }

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
        // The token this process wanted to replace. Read before waiting: if
        // the keychain holds another one once the lock is ours, the other
        // process has already rotated it.
        let stale = AccountCredentials.refreshToken
        let lock = await refreshLock.acquire()
        defer { lock.release() }
        if let fresh = rotatedElsewhere(since: stale) {
            return fresh
        }
        return try await refreshOverNetwork()
    }

    /// The access token another process stored while this one waited for the
    /// lock, when it is safe to use instead of a refresh: the refresh token
    /// changed since this process last read it, and the new access token is
    /// still fresh. Nil means "refresh over the network".
    nonisolated static func tokenRotatedElsewhere(
        staleRefreshToken: String?,
        storedRefreshToken: String?,
        storedAccessToken: String?,
        isAccessTokenFresh: Bool
    ) -> String? {
        guard let storedRefreshToken, storedRefreshToken != staleRefreshToken,
              isAccessTokenFresh, let storedAccessToken, !storedAccessToken.isEmpty else { return nil }
        return storedAccessToken
    }

    private func refreshOverNetwork() async throws -> String {
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
        let response: Response
        do {
            response = try await client.post(
                "api/v1/auth/refresh",
                body: Request(refresh_token: refreshToken, device: .current)
            )
        } catch APIError.unauthorized {
            // The refresh token is gone, rotated or revoked. Nothing local can
            // fix that, so the session is cleared and the UI asks for sign-in -
            // unless another session was stored meanwhile, which is not this
            // refresh's to clear.
            if Self.isSameSession(sentRefreshToken: refreshToken, storedRefreshToken: AccountCredentials.refreshToken) {
                AccountCredentials.clear()
            } else if let fresh = rotatedElsewhere(since: refreshToken) {
                return fresh
            }
            throw APIError.unauthorized
        }

        // The session ended or was replaced while the request was out (a
        // sign-out, another account signing in, the other process rotating
        // it after the lock timed out). Storing this pair would sign a
        // signed-out app back in, so it is dropped; nothing is cleared,
        // whatever is stored now is not this session's.
        guard Self.isSameSession(sentRefreshToken: refreshToken, storedRefreshToken: AccountCredentials.refreshToken) else {
            if let fresh = rotatedElsewhere(since: refreshToken) { return fresh }
            throw APIError.unauthorized
        }
        AccountCredentials.store(accessToken: response.accessToken,
                                 refreshToken: response.refreshToken,
                                 expiresIn: response.expiresIn)
        return response.accessToken
    }

    /// Whether the refresh token sent is still the stored one: only then may
    /// the answer to it be stored, or the session cleared over it.
    nonisolated static func isSameSession(sentRefreshToken: String, storedRefreshToken: String?) -> Bool {
        storedRefreshToken == sentRefreshToken
    }

    /// `tokenRotatedElsewhere` against what the keychain holds now.
    private func rotatedElsewhere(since refreshToken: String?) -> String? {
        Self.tokenRotatedElsewhere(
            staleRefreshToken: refreshToken,
            storedRefreshToken: AccountCredentials.refreshToken,
            storedAccessToken: AccountCredentials.accessToken,
            isAccessTokenFresh: AccountCredentials.isAccessTokenFresh
        )
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

/// An advisory lock on a file in the App Group container, shared by the app
/// and the keyboard extension.
///
/// Ортақ файл құлпы: қосымша мен пернетақта токенді кезекпен жаңартады.
///
/// `flock` belongs to the open file, so the system releases it when the
/// holder exits - a keyboard killed mid-refresh never leaves it held.
/// Waiting polls with a short sleep instead of blocking a thread, and gives
/// up after `timeout`: a holder that long overdue is stuck, and running
/// without the lock is better than never refreshing.
///
/// iOS terminates a process that is suspended while it holds a lock on a
/// file in a shared container, so the holder asks for time to finish
/// (`performExpiringActivity`, which works in the app and the extension)
/// and gives the lock back early if that time runs out.
struct CrossProcessLock: Sendable {

    /// Nil when the App Group container is not reachable: the work then runs
    /// unlocked, serialised in-process by the caller's actor only.
    let fileURL: URL?
    var timeout: Duration = .seconds(30)
    var pollInterval: Duration = .milliseconds(50)

    static let accountRefresh = CrossProcessLock(
        fileURL: FileManager.default
            .containerURL(forSecurityApplicationGroupIdentifier: AppGroup.identifier)?
            .appendingPathComponent("account-refresh.lock")
    )

    /// The lock, once held; `release()` gives it back. Released at most
    /// once, whether by the holder or because the process is being suspended.
    final class Held: @unchecked Sendable {
        private let mutex = NSLock()
        /// -1 when running unlocked or once released.
        private var descriptor: Int32
        private let activityDone = DispatchSemaphore(value: 0)

        fileprivate init(descriptor: Int32) {
            self.descriptor = descriptor
            guard descriptor >= 0 else { return }
            ProcessInfo.processInfo.performExpiringActivity(withReason: "account-refresh") { [weak self] expired in
                guard let self else { return }
                if expired {
                    // About to be suspended: not with the lock held.
                    self.release()
                } else {
                    self.activityDone.wait()
                }
            }
        }

        var isLocked: Bool {
            mutex.lock()
            defer { mutex.unlock() }
            return descriptor >= 0
        }

        func release() {
            mutex.lock()
            let held = descriptor
            descriptor = -1
            mutex.unlock()
            guard held >= 0 else { return }
            flock(held, LOCK_UN)
            close(held)
            activityDone.signal()
        }
    }

    /// Waits for the lock: held, or unlocked when there is no container or
    /// the wait timed out.
    func acquire() async -> Held {
        guard let path = fileURL?.path else { return Held(descriptor: -1) }
        let descriptor = open(path, O_RDWR | O_CREAT | O_CLOEXEC, 0o600)
        guard descriptor >= 0 else { return Held(descriptor: -1) }
        let deadline = ContinuousClock.now.advanced(by: timeout)
        while flock(descriptor, LOCK_EX | LOCK_NB) != 0 {
            guard errno == EWOULDBLOCK || errno == EINTR, ContinuousClock.now < deadline,
                  !Task.isCancelled else {
                close(descriptor)
                return Held(descriptor: -1)
            }
            try? await Task.sleep(for: pollInterval)
        }
        return Held(descriptor: descriptor)
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
