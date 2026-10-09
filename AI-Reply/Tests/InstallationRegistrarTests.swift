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
        var failure: APIError?

        func reject(_ tokens: Set<String>) { rejectedTokens = tokens }
        func fail(with failure: APIError?) { self.failure = failure }

        func register(_ payload: InstallationPayload, accessToken: String?) async throws -> InstallationResponse {
            calls.append(Call(payload: payload, token: accessToken))
            if let failure { throw failure }
            if let accessToken, rejectedTokens.contains(accessToken) {
                throw APIError.unauthorized
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

    /// Shaped like an FCM registration token, and plainly not a real one.
    private static let fcmToken = "fcm-test-token:" + String(repeating: "Ab1_-", count: 30)
    private static let otherFCMToken = "fcm-test-token:" + String(repeating: "Cd2.-", count: 30)

    private func payload(permission: NotificationPermission = .notDetermined,
                         enabled: Bool = true,
                         token: String? = nil) -> InstallationPayload {
        ClientContext(installationID: "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d",
                      build: BuildInfo(appVersion: "1.3.2", appBuild: "142", osVersion: "17.5"),
                      deviceModel: "iPhone17,1", locale: "kk", timezone: "Asia/Almaty")
            .installationPayload(permission: permission, notificationsEnabled: enabled,
                                 push: token.map(InstallationPayload.Push.fcm))
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
            with: JSONEncoder().encode(payload(permission: .authorized, token: Self.fcmToken)))
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
        XCTAssertEqual(push, ["provider": "fcm", "token": Self.fcmToken], "FCM picks the APNs host; no environment")
    }

    /// The server's own check on an FCM token: 20 to 4096 characters of
    /// `[A-Za-z0-9_:.-]`.
    func testTheFixtureLooksLikeAnFCMToken() {
        XCTAssertNotNil(Self.fcmToken.range(of: "^[A-Za-z0-9_:.-]{20,4096}$", options: .regularExpression))
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
        _ = await registrar.sync(signedIn(payload(permission: .authorized, token: Self.fcmToken)))
        _ = await registrar.sync(signedIn(payload(permission: .authorized, enabled: false, token: Self.fcmToken)))
        _ = await registrar.sync(signedIn(payload(permission: .authorized, enabled: false, token: Self.otherFCMToken)))
        let calls = await server.calls
        XCTAssertEqual(calls.count, 5)
        XCTAssertEqual(calls.last?.payload.notifications_enabled, false)
        XCTAssertEqual(calls.last?.payload.push?.token, Self.otherFCMToken, "a refreshed token is sent at once")
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
        await server.fail(with: .offline)
        let failed = await registrar.sync(signedIn(payload()))
        XCTAssertEqual(failed, .failed(.offline))
        await server.fail(with: nil)
        let retried = await registrar.sync(signedIn(payload()))
        guard case .registered = retried else { return XCTFail("expected the retry to register, got \(retried)") }
        let calls = await server.calls
        XCTAssertEqual(calls.count, 2)
    }

    func testAnInvalidPayloadIsNotResentUntilItChanges() async {
        await server.fail(with: .invalidRequest)
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
        XCTAssertEqual(failure, .unauthorized)
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
        XCTAssertEqual(failure, .unauthorized)
        let calls = await server.calls
        XCTAssertEqual(calls.count, 1)
    }

    // MARK: When to register at all

    private func inputs() -> InstallationSnapshot.Inputs {
        InstallationSnapshot.Inputs(
            isBootstrapComplete: true, hasAcceptedLegal: true, serverOffersInstallations: true,
            hasReadPermission: true, installationID: "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d",
            permission: .authorized, notificationsEnabled: true, pushToken: Self.fcmToken,
            locale: "kk", hasSession: true, accountID: "user-1")
    }

    private func snapshot(_ inputs: InstallationSnapshot.Inputs) -> InstallationSnapshot? {
        InstallationSnapshot.make(inputs, build: BuildInfo(appVersion: "1.3.2", appBuild: "142", osVersion: "17.5"),
                                  deviceModel: "iPhone17,1", timezone: "Asia/Almaty")
    }

    func testNothingIsRegisteredBeforeTheLegalConsent() {
        var beforeConsent = inputs()
        beforeConsent.hasAcceptedLegal = false
        XCTAssertNil(snapshot(beforeConsent))
    }

    func testNothingIsRegisteredUntilWhatItNeedsIsKnown() {
        var input = inputs()
        input.isBootstrapComplete = false
        XCTAssertNil(snapshot(input), "the account bootstrap has not finished")
        input = inputs()
        input.serverOffersInstallations = nil
        XCTAssertNil(snapshot(input), "the server has not answered")
        input.serverOffersInstallations = false
        XCTAssertNil(snapshot(input), "an older server without installations")
        input = inputs()
        input.hasReadPermission = false
        XCTAssertNil(snapshot(input), "iOS has not been asked about the permission yet")
        input = inputs()
        input.installationID = nil
        XCTAssertNil(snapshot(input), "the Keychain is locked")
    }

    func testASnapshotCarriesTheTokenAndTheAccountFromTheSession() throws {
        let signedIn = try XCTUnwrap(snapshot(inputs()))
        XCTAssertEqual(signedIn.payload.push, .init(provider: "fcm", token: Self.fcmToken))
        XCTAssertEqual(signedIn.payload.locale, "kk")
        XCTAssertEqual(signedIn.payload.app_build, "142")
        XCTAssertEqual(signedIn.payload.notification_permission, "authorized")
        XCTAssertTrue(signedIn.isSignedIn)
        XCTAssertEqual(signedIn.accountID, "user-1")

        var signedOutInputs = inputs()
        signedOutInputs.hasSession = false
        signedOutInputs.pushToken = nil
        let signedOut = try XCTUnwrap(snapshot(signedOutInputs))
        XCTAssertFalse(signedOut.isSignedIn)
        XCTAssertNil(signedOut.accountID, "no session: anonymous, whatever the screen still shows")
        XCTAssertNil(signedOut.payload.push)
    }

    // MARK: Retry policy

    func testWhichFailuresAreRetried() {
        XCTAssertTrue(InstallationRegistrar.isRetryable(.offline), "no response")
        XCTAssertTrue(InstallationRegistrar.isRetryable(.timedOut))
        XCTAssertTrue(InstallationRegistrar.isRetryable(.rateLimited(retryAfter: 60)))
        XCTAssertTrue(InstallationRegistrar.isRetryable(.server))
        XCTAssertTrue(InstallationRegistrar.isRetryable(.providerTimeout), "a gateway timeout")
        XCTAssertTrue(InstallationRegistrar.isRetryable(.unauthorized))
        XCTAssertFalse(InstallationRegistrar.isRetryable(.invalidRequest))
        XCTAssertFalse(InstallationRegistrar.isRetryable(.notFound))
        XCTAssertFalse(InstallationRegistrar.isRetryable(.conflict))
        XCTAssertFalse(InstallationRegistrar.isRetryable(.malformedResponse))
    }

    func testTheServersWaitIsKept() {
        XCTAssertEqual(InstallationRegistrar.retryAfter(.rateLimited(retryAfter: 120)), 120)
        XCTAssertNil(InstallationRegistrar.retryAfter(.rateLimited(retryAfter: nil)))
        XCTAssertNil(InstallationRegistrar.retryAfter(.server))
    }

    /// The status codes behind those cases, as `APIClient` maps them.
    func testStatusesMapToTheRetryPolicy() {
        func failure(_ status: Int) -> APIError {
            APIClient.mapServerError(status: status, data: Data(), headers: HTTPURLResponse(
                url: URL(string: "https://example.test")!, statusCode: status, httpVersion: nil, headerFields: nil)!)
        }
        for status in [401, 429, 500, 503, 504] {
            XCTAssertTrue(InstallationRegistrar.isRetryable(failure(status)), "\(status)")
        }
        for status in [400, 404, 409, 422] {
            XCTAssertFalse(InstallationRegistrar.isRetryable(failure(status)), "\(status)")
        }
    }
}
