import CryptoKit
import Foundation

/// `PUT /api/v1/installations/{installation_id}/diagnostics`: the exact,
/// strict body (contract §3.4). Statuses and dates only - never any text,
/// nothing from the clipboard.
struct PermissionDiagnosticsReport: Encodable, Equatable, Sendable {
    let keyboardSetupStatus: PermissionEvidence
    let keyboardSelectedStatus: PermissionEvidence
    let fullAccessStatus: PermissionEvidence
    let clipboardStatus: PermissionEvidence
    let onboardingStatus: OnboardingStatus
    /// Always `app_check`: the app reads what the keyboard left.
    let statusSource: String
    let lastSuccessfulPasteAt: Date?
    let lastKeyboardCheckAt: Date?

    enum CodingKeys: String, CodingKey {
        case keyboardSetupStatus = "keyboard_setup_status"
        case keyboardSelectedStatus = "keyboard_selected_status"
        case fullAccessStatus = "full_access_status"
        case clipboardStatus = "clipboard_status"
        case onboardingStatus = "onboarding_status"
        case statusSource = "status_source"
        case lastSuccessfulPasteAt = "last_successful_paste_at"
        case lastKeyboardCheckAt = "last_keyboard_check_at"
    }

    /// What the keyboard was seen doing (`KeyboardStatus`) and how this
    /// device's onboarding stands, in the contract's words.
    static func make(keyboard: KeyboardStatus, onboarding: OnboardingStatus) -> PermissionDiagnosticsReport {
        PermissionDiagnosticsReport(
            keyboardSetupStatus: keyboard.keyboardSetupStatus,
            keyboardSelectedStatus: keyboard.keyboardSelectedStatus,
            fullAccessStatus: keyboard.fullAccessStatus,
            clipboardStatus: keyboard.clipboardStatus,
            onboardingStatus: onboarding,
            statusSource: "app_check",
            lastSuccessfulPasteAt: keyboard.lastSuccessfulPasteAt,
            lastKeyboardCheckAt: keyboard.lastKeyboardCheckAt
        )
    }

    /// Every key is written, a missing date as `null`.
    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(keyboardSetupStatus.rawValue, forKey: .keyboardSetupStatus)
        try container.encode(keyboardSelectedStatus.rawValue, forKey: .keyboardSelectedStatus)
        try container.encode(fullAccessStatus.rawValue, forKey: .fullAccessStatus)
        try container.encode(clipboardStatus.rawValue, forKey: .clipboardStatus)
        try container.encode(onboardingStatus.rawValue, forKey: .onboardingStatus)
        try container.encode(statusSource, forKey: .statusSource)
        try container.encode(lastSuccessfulPasteAt.map(Self.timestamp), forKey: .lastSuccessfulPasteAt)
        try container.encode(lastKeyboardCheckAt.map(Self.timestamp), forKey: .lastKeyboardCheckAt)
    }

    /// RFC 3339 in UTC, whole seconds.
    static func timestamp(_ date: Date) -> String {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]
        formatter.timeZone = TimeZone(identifier: "UTC")
        return formatter.string(from: date)
    }

    /// What "changed" means: any status, or a date that moved to another
    /// (UTC) day. A keyboard seen again on the same day is not news; the
    /// daily report carries the newer time anyway.
    var changeKey: String {
        func day(_ date: Date?) -> String { date.map { String(Self.timestamp($0).prefix(10)) } ?? "-" }
        return [keyboardSetupStatus.rawValue, keyboardSelectedStatus.rawValue, fullAccessStatus.rawValue,
                clipboardStatus.rawValue, onboardingStatus.rawValue, statusSource,
                day(lastSuccessfulPasteAt), day(lastKeyboardCheckAt)].joined(separator: "|")
    }
}

/// Sends one report. Throws `APIError`.
protocol DiagnosticsTransport: Sendable {
    func put(_ report: PermissionDiagnosticsReport, installationID: String, accessToken: String?) async throws
}

/// The last report the server accepted.
protocol DiagnosticsSyncStoring: Sendable {
    func lastSync() -> InstallationSyncRecord?
    func setLastSync(_ record: InstallationSyncRecord?)
}

/// Kept in the app's own defaults: a device fact, never synced or shared.
struct DefaultsDiagnosticsSyncStore: DiagnosticsSyncStoring, @unchecked Sendable {
    var defaults: UserDefaults = .standard
    private static let key = "diagnostics.lastSync"

    func lastSync() -> InstallationSyncRecord? {
        defaults.data(forKey: Self.key).flatMap { try? JSONDecoder().decode(InstallationSyncRecord.self, from: $0) }
    }

    func setLastSync(_ record: InstallationSyncRecord?) {
        guard let record, let data = try? JSONEncoder().encode(record) else {
            defaults.removeObject(forKey: Self.key)
            return
        }
        defaults.set(data, forKey: Self.key)
    }
}

/// `PUT …/diagnostics` with the account's token when there is a session.
struct BackendDiagnosticsTransport: DiagnosticsTransport {
    func put(_ report: PermissionDiagnosticsReport, installationID: String, accessToken: String?) async throws {
        let _: APIClient.Empty = try await NotificationsAPI.client().put(
            "api/v1/installations/\(installationID)/diagnostics", body: report, token: accessToken)
    }
}

/// Tells the server what the app last saw of its keyboard and permissions,
/// so a reminder is sent only on fresh evidence (contract §3.4, §3.6).
///
/// Рұқсат диагностикасы: өзгерсе ғана немесе тәулігіне бір рет жіберіледі.
///
/// Sent on the way to the foreground, after the installation is registered
/// (the server needs it to exist), and only when the report changed (see
/// `changeKey`) or the last accepted one is 24 hours old. A report the
/// server refused is not sent again until something in it changes.
actor PermissionDiagnosticsReporter {

    enum Outcome: Equatable, Sendable {
        case sent
        case unchanged
        case failed(APIError)
    }

    static let refreshInterval: TimeInterval = 24 * 60 * 60

    private let transport: DiagnosticsTransport
    private let tokens: AccessTokenProviding
    private let store: DiagnosticsSyncStoring
    private let now: @Sendable () -> Date
    private var rejectedFingerprint: String?

    init(transport: DiagnosticsTransport = BackendDiagnosticsTransport(),
         tokens: AccessTokenProviding = AccountSession.shared,
         store: DiagnosticsSyncStoring = DefaultsDiagnosticsSyncStore(),
         now: @escaping @Sendable () -> Date = { Date() }) {
        self.transport = transport
        self.tokens = tokens
        self.store = store
        self.now = now
    }

    func report(_ report: PermissionDiagnosticsReport, installationID: String,
                isSignedIn: Bool, accountID: String?) async -> Outcome {
        let fingerprint = Self.fingerprint(report, installationID: installationID,
                                           account: isSignedIn ? accountID ?? "signed-in" : nil)
        if fingerprint == rejectedFingerprint { return .unchanged }
        if let last = store.lastSync(), last.fingerprint == fingerprint,
           now() >= last.syncedAt, now().timeIntervalSince(last.syncedAt) < Self.refreshInterval {
            return .unchanged
        }
        do {
            try await send(report, installationID: installationID, isSignedIn: isSignedIn)
            rejectedFingerprint = nil
            store.setLastSync(InstallationSyncRecord(fingerprint: fingerprint, syncedAt: now()))
            return .sent
        } catch {
            let failure = error as? APIError ?? .server
            if !InstallationRegistrar.isRetryable(failure) { rejectedFingerprint = fingerprint }
            return .failed(failure)
        }
    }

    private func send(_ report: PermissionDiagnosticsReport, installationID: String, isSignedIn: Bool) async throws {
        guard isSignedIn else {
            return try await transport.put(report, installationID: installationID, accessToken: nil)
        }
        let token = try await tokens.accessToken()
        do {
            try await transport.put(report, installationID: installationID, accessToken: token)
        } catch APIError.unauthorized {
            // Once, never in a loop.
            let fresh = try await tokens.refreshAccessToken()
            try await transport.put(report, installationID: installationID, accessToken: fresh)
        }
    }

    static func fingerprint(_ report: PermissionDiagnosticsReport, installationID: String, account: String?) -> String {
        let material = "\(report.changeKey)|installation:\(installationID)|account:\(account ?? "")"
        return SHA256.hash(data: Data(material.utf8)).map { String(format: "%02x", $0) }.joined()
    }
}
