import UserNotifications
import XCTest
@testable import AIReply

/// The installation record: when it is sent, with which token, and what a
/// rejected token does - against a fake server.
///
/// Орнатуды тіркеу: қайталанбау, 401 кезінде бір рет жаңарту, шыққанда анонимді тіркеу.
final class InstallationRegistrarTests: XCTestCase {

    // MARK: Fakes

    /// Records every registration and answers from a script.
    private actor FakeServer: InstallationTransport {
        struct Call: Equatable {
            let payload: InstallationPayload
            let token: String?
        }

        private(set) var calls: [Call] = []
        /// Tokens the server rejects with 401.
        var rejectedTokens: Set<String> = []
        var failure: APIFailure?

        func reject(_ tokens: Set<String>) { rejectedTokens = tokens }
        func fail(with failure: APIFailure?) { self.failure = failure }

        func register(_ payload: InstallationPayload, accessToken: String?) async throws -> InstallationResponse {
            calls.append(Call(payload: payload, token: accessToken))
            if let failure { throw failure }
            if let accessToken, rejectedTokens.contains(accessToken) {
                throw APIFailure(error: .unauthorized, requestID: "req_rejected000000001", status: 401,
                                 code: "TOKEN_EXPIRED")
            }
            return InstallationResponse(installationID: payload.installation_id, attached: accessToken != nil,
                                        pushStatus: payload.push == nil ? "none" : "active",
                                        pushAvailable: true, notificationsEnabled: payload.notifications_enabled,
                                        preferences: accessToken == nil ? nil : ["marketing": true])
        }
    }

    private actor FakeTokens: AccessTokenProviding {
        private(set) var refreshes = 0
        var current = "token-1"
        var refreshError: APIError?

        func failRefresh(with error: APIError) { refreshError = error }

        func accessToken() async throws -> String { current }

        func refreshAccessToken() async throws -> String {
            refreshes += 1
            if let refreshError { throw refreshError }
            current = "token-\(refreshes + 1)"
            return current
        }
    }

    private final class MemoryStore: InstallationSyncStoring, @unchecked Sendable {
        private let lock = NSLock()
        private var record: InstallationSyncRecord?

        func lastSync() -> InstallationSyncRecord? {
            lock.lock()
            defer { lock.unlock() }
            return record
        }

        func setLastSync(_ record: InstallationSyncRecord?) {
            lock.lock()
            self.record = record
            lock.unlock()
        }
    }

    private final class Clock: @unchecked Sendable {
        private let lock = NSLock()
        private var date = Date(timeIntervalSince1970: 1_800_000_000)

        var now: Date {
            lock.lock()
            defer { lock.unlock() }
            return date
        }

        func advance(_ seconds: TimeInterval) {
            lock.lock()
            date = date.addingTimeInterval(seconds)
            lock.unlock()
        }
    }

    private var server: FakeServer!
    private var tokens: FakeTokens!
    private var store: MemoryStore!
    private var clock: Clock!
    private var registrar: InstallationRegistrar!

    override func setUp() {
        super.setUp()
        server = FakeServer()
        tokens = FakeTokens()
        store = MemoryStore()
        clock = Clock()
        let clock = self.clock!
        registrar = InstallationRegistrar(transport: server, tokens: tokens, store: store, now: { clock.now })
    }

    private func payload(permission: NotificationPermission = .notDetermined,
                         enabled: Bool = true,
                         token: String? = nil) -> InstallationPayload {
        ClientContext(installationID: "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d",
                      metadata: ClientMetadata(appVersion: "1.3.2", appBuild: "142", osVersion: "17.5"),
                      deviceModel: "iPhone17,1", locale: "kk", timezone: "Asia/Almaty")
            .installationPayload(permission: permission, notificationsEnabled: enabled,
                                 push: token.map { .init(provider: "apns", token: $0, environment: "sandbox") })
    }

    private func signedIn(_ payload: InstallationPayload, account: String? = "user-1") -> InstallationSnapshot {
        InstallationSnapshot(payload: payload, isSignedIn: true, accountID: account)
    }

    private func signedOut(_ payload: InstallationPayload) -> InstallationSnapshot {
        InstallationSnapshot(payload: payload, isSignedIn: false, accountID: nil)
    }

    // MARK: Wire format

    /// The server refuses unknown fields: exactly these keys, snake case.
    func testPayloadHasExactlyTheServersFields() throws {
        let json = try XCTUnwrap(JSONSerialization.jsonObject(
            with: JSONEncoder().encode(payload(permission: .authorized, token: String(repeating: "ab", count: 32))))
            as? [String: Any])
        XCTAssertEqual(Set(json.keys), [
            "installation_id", "platform", "app_version", "app_build", "os_name", "os_version", "device_model",
            "manufacturer", "locale", "timezone", "notification_permission", "notifications_enabled", "push"
        ])
        XCTAssertEqual(json["platform"] as? String, "ios")
        XCTAssertEqual(json["os_name"] as? String, "iOS")
        XCTAssertEqual(json["manufacturer"] as? String, "Apple")
        XCTAssertEqual(json["device_model"] as? String, "iPhone17,1")
        XCTAssertEqual(json["notification_permission"] as? String, "authorized")
        XCTAssertEqual(json["notifications_enabled"] as? Bool, true)
        let push = try XCTUnwrap(json["push"] as? [String: String])
        XCTAssertEqual(push, ["provider": "apns", "token": String(repeating: "ab", count: 32), "environment": "sandbox"])
    }

    func testNoTokenMeansNoPushObjectAtAll() throws {
        let json = try XCTUnwrap(JSONSerialization.jsonObject(with: JSONEncoder().encode(payload())) as? [String: Any])
        XCTAssertNil(json["push"], "the server keeps the token it has when push is absent")
        XCTAssertEqual(json["notification_permission"] as? String, "not_determined")
    }

    func testPermissionValuesAreTheServersWords() {
        XCTAssertEqual(NotificationPermission(.authorized).rawValue, "authorized")
        XCTAssertEqual(NotificationPermission(.denied).rawValue, "denied")
        XCTAssertEqual(NotificationPermission(.notDetermined).rawValue, "not_determined")
        XCTAssertEqual(NotificationPermission(.provisional).rawValue, "provisional")
        XCTAssertEqual(NotificationPermission(.ephemeral).rawValue, "ephemeral")
        XCTAssertEqual(NotificationPermission.unknown.rawValue, "unknown")
    }

    func testResponseDecodes() throws {
        let response = try JSONDecoder().decode(InstallationResponse.self, from: Data("""
        {"installation_id":"0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d","attached":true,"push_status":"active",
         "push_available":true,"notifications_enabled":false,
         "preferences":{"account":true,"subscription":false,"security":true,"system":true,"marketing":false}}
        """.utf8))
        XCTAssertTrue(response.attached)
        XCTAssertEqual(response.pushStatus, "active")
        XCTAssertFalse(response.notificationsEnabled)
        XCTAssertEqual(response.preferences?["subscription"], false)

        let anonymous = try JSONDecoder().decode(InstallationResponse.self, from: Data("""
        {"installation_id":"x-12345678","attached":false,"push_status":"none","push_available":false,"notifications_enabled":true}
        """.utf8))
        XCTAssertNil(anonymous.preferences)
    }

    // MARK: Dedupe

    func testAnUnchangedInstallationIsNotSentTwice() async {
        let first = await registrar.sync(signedIn(payload()))
        guard case .registered = first else { return XCTFail("expected a registration, got \(first)") }
        let second = await registrar.sync(signedIn(payload()))
        XCTAssertEqual(second, .unchanged)
        let calls = await server.calls
        XCTAssertEqual(calls.count, 1)
    }

    func testAnyChangeIsSent() async {
        _ = await registrar.sync(signedIn(payload()))
        _ = await registrar.sync(signedIn(payload(permission: .authorized)))
        _ = await registrar.sync(signedIn(payload(permission: .authorized, token: String(repeating: "0f", count: 32))))
        _ = await registrar.sync(signedIn(payload(permission: .authorized, enabled: false,
                                                  token: String(repeating: "0f", count: 32))))
        let calls = await server.calls
        XCTAssertEqual(calls.count, 4)
        XCTAssertEqual(calls.last?.payload.notifications_enabled, false)
    }

    func testTheSameInstallationIsSentAgainAfterADay() async {
        _ = await registrar.sync(signedIn(payload()))
        clock.advance(23 * 60 * 60)
        let early = await registrar.sync(signedIn(payload()))
        XCTAssertEqual(early, .unchanged)
        clock.advance(60 * 60 + 1)
        let later = await registrar.sync(signedIn(payload()))
        guard case .registered = later else { return XCTFail("expected a daily registration, got \(later)") }
        let calls = await server.calls
        XCTAssertEqual(calls.count, 2)
    }

    func testForceSendsEvenWhenUnchanged() async {
        _ = await registrar.sync(signedIn(payload()))
        _ = await registrar.sync(signedIn(payload()), force: true)
        let calls = await server.calls
        XCTAssertEqual(calls.count, 2)
    }

    func testAFailureIsNotRememberedAsSent() async {
        await server.fail(with: APIFailure(error: .offline, requestID: "req_offline000000001", status: 0))
        let failed = await registrar.sync(signedIn(payload()))
        XCTAssertEqual(failed, .failed(APIFailure(error: .offline, requestID: "req_offline000000001", status: 0)))
        await server.fail(with: nil)
        let retried = await registrar.sync(signedIn(payload()))
        guard case .registered = retried else { return XCTFail("expected the retry to register, got \(retried)") }
        let calls = await server.calls
        XCTAssertEqual(calls.count, 2)
    }

    func testAnInvalidPayloadIsNotResentUntilItChanges() async {
        await server.fail(with: APIFailure(error: .invalidRequest, requestID: "req_invalid000000001", status: 400,
                                           code: "INVALID_REQUEST"))
        _ = await registrar.sync(signedIn(payload()))
        let again = await registrar.sync(signedIn(payload()))
        XCTAssertEqual(again, .unchanged, "the same body would be refused again")
        _ = await registrar.sync(signedIn(payload(permission: .denied)))
        let calls = await server.calls
        XCTAssertEqual(calls.count, 2)
    }

    // MARK: Account

    func testSignedInRegistrationCarriesTheAccessToken() async {
        _ = await registrar.sync(signedIn(payload()))
        let calls = await server.calls
        XCTAssertEqual(calls.map(\.token), ["token-1"])
    }

    /// Sign-out: the tokens are gone, the same device registers without one,
    /// which detaches it from the account on the server.
    func testSignOutRegistersAnonymouslyEvenWithTheSamePayload() async {
        _ = await registrar.sync(signedIn(payload()))
        let outcome = await registrar.sync(signedOut(payload()))
        guard case .registered(let response) = outcome else { return XCTFail("expected a registration, got \(outcome)") }
        XCTAssertFalse(response.attached)
        let calls = await server.calls
        XCTAssertEqual(calls.map(\.token), ["token-1", nil])
    }

    func testAnotherAccountOnTheSamePhoneRegistersAgain() async {
        _ = await registrar.sync(signedIn(payload(), account: "user-1"))
        _ = await registrar.sync(signedIn(payload(), account: "user-2"))
        let calls = await server.calls
        XCTAssertEqual(calls.count, 2)
    }

    // MARK: Rejected token

    func testARejectedTokenIsRefreshedOnceAndRetried() async {
        await server.reject(["token-1"])
        let outcome = await registrar.sync(signedIn(payload()))
        guard case .registered(let response) = outcome else { return XCTFail("expected a registration, got \(outcome)") }
        XCTAssertTrue(response.attached)
        let calls = await server.calls
        XCTAssertEqual(calls.map(\.token), ["token-1", "token-2"])
        let refreshes = await tokens.refreshes
        XCTAssertEqual(refreshes, 1)
    }

    func testASecondRejectionStopsWithoutALoop() async {
        await server.reject(["token-1", "token-2", "token-3"])
        let outcome = await registrar.sync(signedIn(payload()))
        guard case .failed(let failure) = outcome else { return XCTFail("expected a failure, got \(outcome)") }
        XCTAssertEqual(failure.status, 401)
        let calls = await server.calls
        XCTAssertEqual(calls.count, 2, "one retry, never more")
        let refreshes = await tokens.refreshes
        XCTAssertEqual(refreshes, 1)
        XCTAssertTrue(InstallationRegistrar.isRetryable(failure),
                      "retried later: by then the session is refreshed or gone, and the pass is anonymous")
    }

    func testAFailedRefreshIsAFailureNotACrash() async {
        await server.reject(["token-1"])
        await tokens.failRefresh(with: .unauthorized)
        let outcome = await registrar.sync(signedIn(payload()))
        guard case .failed(let failure) = outcome else { return XCTFail("expected a failure, got \(outcome)") }
        XCTAssertEqual(failure.error, .unauthorized)
        let calls = await server.calls
        XCTAssertEqual(calls.count, 1)
    }

    // MARK: Retry policy

    func testWhichFailuresAreRetried() {
        func failure(_ status: Int) -> APIFailure { APIFailure(error: .server, requestID: "", status: status) }
        XCTAssertTrue(InstallationRegistrar.isRetryable(failure(0)), "no response")
        XCTAssertTrue(InstallationRegistrar.isRetryable(failure(429)))
        XCTAssertTrue(InstallationRegistrar.isRetryable(failure(500)))
        XCTAssertTrue(InstallationRegistrar.isRetryable(failure(503)))
        XCTAssertFalse(InstallationRegistrar.isRetryable(failure(400)))
        XCTAssertFalse(InstallationRegistrar.isRetryable(failure(404)))
        XCTAssertFalse(InstallationRegistrar.isRetryable(failure(409)))
    }
}
