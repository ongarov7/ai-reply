import CryptoKit
import Foundation

/// `POST /api/v1/installations`: the exact body. The server refuses unknown
/// fields, so nothing is added here that it does not know.
struct InstallationPayload: Codable, Equatable, Sendable {

    struct Push: Codable, Equatable, Sendable {
        let provider: String
        /// The APNs device token, lowercase hex.
        let token: String
        /// "sandbox" or "production": which APNs host this build's token belongs to.
        let environment: String
    }

    let installation_id: String
    let platform: String
    let app_version: String
    let app_build: String
    let os_name: String
    let os_version: String
    let device_model: String
    let manufacturer: String
    /// The app's interface language: en, ru, kk or uz.
    let locale: String
    let timezone: String
    let notification_permission: String
    /// The in-app switch in Settings ▸ Notifications.
    let notifications_enabled: Bool
    /// Left out entirely while there is no token; the server then keeps the
    /// one it has.
    let push: Push?
}

/// The server's answer.
struct InstallationResponse: Decodable, Equatable, Sendable {
    let installationID: String
    let attached: Bool
    /// none | active | invalid | replaced
    let pushStatus: String
    let pushAvailable: Bool
    let notificationsEnabled: Bool
    /// Notification categories; only present for a signed-in request.
    let preferences: [String: Bool]?

    enum CodingKeys: String, CodingKey {
        case installationID = "installation_id"
        case attached
        case pushStatus = "push_status"
        case pushAvailable = "push_available"
        case notificationsEnabled = "notifications_enabled"
        case preferences
    }

    init(installationID: String, attached: Bool, pushStatus: String, pushAvailable: Bool,
         notificationsEnabled: Bool, preferences: [String: Bool]? = nil) {
        self.installationID = installationID
        self.attached = attached
        self.pushStatus = pushStatus
        self.pushAvailable = pushAvailable
        self.notificationsEnabled = notificationsEnabled
        self.preferences = preferences
    }
}

/// What the app wants the server to know, at the moment of a sync.
struct InstallationSnapshot: Equatable, Sendable {
    let payload: InstallationPayload
    /// Whether a session exists. The account an installation belongs to comes
    /// only from the access token: with one the installation is attached to
    /// that account, without one it is anonymous.
    let isSignedIn: Bool
    /// The signed-in account's id when known. Part of what "unchanged" means,
    /// so a sign-in, a sign-out or another account always registers again.
    let accountID: String?
}

/// Sends one registration. Throws `APIFailure`.
protocol InstallationTransport: Sendable {
    func register(_ payload: InstallationPayload, accessToken: String?) async throws -> InstallationResponse
}

/// Hands out access tokens; `AccountSession` in the app.
protocol AccessTokenProviding: Sendable {
    func accessToken() async throws -> String
    func refreshAccessToken() async throws -> String
}

extension AccountSession: AccessTokenProviding {}

/// The last registration the server accepted.
struct InstallationSyncRecord: Codable, Equatable, Sendable {
    let fingerprint: String
    let syncedAt: Date
}

/// Where the last accepted registration is remembered.
protocol InstallationSyncStoring: Sendable {
    func lastSync() -> InstallationSyncRecord?
    func setLastSync(_ record: InstallationSyncRecord?)
}

/// Keeps the server's record of this install up to date, and quiet.
///
/// Орнатуды тіркеу: өзгеріс болса ғана, әйтпесе тәулігіне бір рет.
///
/// A registration is sent only when something in it changed since the last
/// one the server accepted - metadata, permission, the in-app switch, the push
/// token, or the account (signed in, signed out, another one) - and otherwise
/// at most once every 24 hours, so the server's "last seen" stays meaningful
/// without the app calling on every launch.
///
/// An access token the server rejects (401) is refreshed once and the call
/// retried once; if the refresh itself fails, `AccountSession` has already
/// cleared the session and the next pass registers anonymously.
actor InstallationRegistrar {

    enum Outcome: Equatable, Sendable {
        case registered(InstallationResponse)
        /// The server already has exactly this, from less than 24 hours ago.
        case unchanged
        case failed(APIFailure)
    }

    static let refreshInterval: TimeInterval = 24 * 60 * 60

    private let transport: InstallationTransport
    private let tokens: AccessTokenProviding
    private let store: InstallationSyncStoring
    private let now: @Sendable () -> Date
    /// A registration the server refused as invalid: sending the same one
    /// again would only be refused again.
    private var rejectedFingerprint: String?

    init(transport: InstallationTransport,
         tokens: AccessTokenProviding,
         store: InstallationSyncStoring,
         now: @escaping @Sendable () -> Date = { Date() }) {
        self.transport = transport
        self.tokens = tokens
        self.store = store
        self.now = now
    }

    /// Registers `snapshot` unless the server already has it (see the type's
    /// documentation). `force` skips that check.
    func sync(_ snapshot: InstallationSnapshot, force: Bool = false) async -> Outcome {
        let fingerprint = Self.fingerprint(snapshot)
        if !force {
            if fingerprint == rejectedFingerprint { return .unchanged }
            if let last = store.lastSync(), last.fingerprint == fingerprint,
               now() >= last.syncedAt,
               now().timeIntervalSince(last.syncedAt) < Self.refreshInterval {
                return .unchanged
            }
        }
        do {
            let response = try await send(snapshot)
            rejectedFingerprint = nil
            store.setLastSync(InstallationSyncRecord(fingerprint: fingerprint, syncedAt: now()))
            return .registered(response)
        } catch let failure as APIFailure {
            if !Self.isRetryable(failure) { rejectedFingerprint = fingerprint }
            return .failed(failure)
        } catch {
            return .failed(APIFailure(error: .server, requestID: "", status: 0))
        }
    }

    private func send(_ snapshot: InstallationSnapshot) async throws -> InstallationResponse {
        guard snapshot.isSignedIn else {
            return try await transport.register(snapshot.payload, accessToken: nil)
        }
        let token = try await withTokenFailures { try await self.tokens.accessToken() }
        do {
            return try await transport.register(snapshot.payload, accessToken: token)
        } catch let failure as APIFailure where failure.error == .unauthorized {
            // Once, never in a loop.
            let fresh = try await withTokenFailures { try await self.tokens.refreshAccessToken() }
            return try await transport.register(snapshot.payload, accessToken: fresh)
        }
    }

    /// A failure to obtain a token, in the same shape as a failed request.
    private func withTokenFailures(_ work: @Sendable () async throws -> String) async throws -> String {
        do {
            return try await work()
        } catch let failure as APIFailure {
            throw failure
        } catch let error as APIError {
            throw APIFailure(error: error, requestID: "", status: error == .unauthorized ? 401 : 0)
        }
    }

    /// Identifies a registration: every payload field plus the account.
    static func fingerprint(_ snapshot: InstallationSnapshot) -> String {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        let body = (try? encoder.encode(snapshot.payload)) ?? Data()
        var material = body
        material.append(Data("|signed-in:\(snapshot.isSignedIn)|account:\(snapshot.accountID ?? "")".utf8))
        return SHA256.hash(data: material).map { String(format: "%02x", $0) }.joined()
    }

    /// Whether a failed registration is worth retrying automatically. A 4xx
    /// other than 401, 408 and 429 would fail the same way again, so it waits
    /// for the next change instead.
    static func isRetryable(_ failure: APIFailure) -> Bool {
        switch failure.status {
        case 0, 401, 408, 429: return true
        case 500...: return true
        default: return false
        }
    }
}
