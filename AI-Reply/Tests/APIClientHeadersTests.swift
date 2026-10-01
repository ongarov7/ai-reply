import XCTest
@testable import AIReply

/// What every request says about the client, and what a failed one carries
/// back: the request id that finds it in the server's log.
///
/// Сұраныс тақырыптары және сұраныс идентификаторы.
final class APIClientHeadersTests: XCTestCase {

    override func tearDown() {
        StubURLProtocol.reset()
        super.tearDown()
    }

    private let metadata = ClientMetadata(appVersion: "1.3.2", appBuild: "142", osVersion: "17.5")

    private func client(hooks: APIClientHooks, requestID: String = "req_abcdefghij012345") -> APIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [StubURLProtocol.self]
        return APIClient(baseURL: URL(string: "https://api.example.test")!,
                         session: URLSession(configuration: configuration),
                         metadata: metadata, hooks: hooks, makeRequestID: { requestID })
    }

    private func appHooks(installationID: String? = "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d",
                          sessionID: String? = "5e55a0b1-0000-4000-8000-000000000001") -> APIClientHooks {
        let hooks = APIClientHooks(allowsIdentity: true)
        hooks.install(identity: { .init(installationID: installationID, sessionID: sessionID) },
                      transportFailures: { _ in })
        return hooks
    }

    // MARK: Request id

    func testRequestIDIsReqAndSixteenLowercaseAlphanumerics() {
        var seen = Set<String>()
        for _ in 0..<200 {
            let id = RequestID.make()
            XCTAssertNotNil(id.range(of: "^req_[a-z0-9]{16}$", options: .regularExpression), id)
            XCTAssertTrue(RequestID.isValid(id))
            seen.insert(id)
        }
        XCTAssertEqual(seen.count, 200, "a fresh id per request")
    }

    func testRequestIDValidationMatchesTheServer() {
        XCTAssertTrue(RequestID.isValid("req_4f1c2a9b7d3e5f60"))
        XCTAssertTrue(RequestID.isValid("abc.DEF:1-2_3"))
        XCTAssertFalse(RequestID.isValid("short"))
        XCTAssertFalse(RequestID.isValid("req 4f1c2a9b7d3e"))
        XCTAssertFalse(RequestID.isValid(String(repeating: "a", count: 65)))
    }

    // MARK: Headers

    func testMetadataHeaders() {
        XCTAssertEqual(metadata.headers, [
            "X-Platform": "ios", "X-App-Version": "1.3.2", "X-App-Build": "142", "X-OS-Version": "17.5"
        ])
        XCTAssertEqual(ClientMetadata(appVersion: "", appBuild: "", osVersion: "18.0").headers,
                       ["X-Platform": "ios", "X-OS-Version": "18.0"], "a blank value is left out")
    }

    func testOSVersionReadsLikeUIDevice() {
        XCTAssertEqual(ClientMetadata.systemVersion(OperatingSystemVersion(majorVersion: 17, minorVersion: 5, patchVersion: 0)), "17.5")
        XCTAssertEqual(ClientMetadata.systemVersion(OperatingSystemVersion(majorVersion: 17, minorVersion: 5, patchVersion: 1)), "17.5.1")
        XCTAssertEqual(ClientMetadata.systemVersion(OperatingSystemVersion(majorVersion: 26, minorVersion: 0, patchVersion: 0)), "26.0")
    }

    func testTheAppAddsInstallationAndSession() {
        let headers = client(hooks: appHooks()).headers(requestID: "req_abcdefghij012345")
        XCTAssertEqual(headers["X-Installation-ID"], "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d")
        XCTAssertEqual(headers["X-Session-ID"], "5e55a0b1-0000-4000-8000-000000000001")
        XCTAssertEqual(headers["X-Request-ID"], "req_abcdefghij012345")
        XCTAssertEqual(headers["X-Platform"], "ios")
        XCTAssertEqual(headers["Accept"], "application/json")
    }

    /// The keyboard sends platform and versions, never who it is.
    func testAnExtensionNeverSendsInstallationOrSession() {
        let hooks = APIClientHooks(allowsIdentity: false)
        hooks.install(identity: { .init(installationID: "0b7c9a52-4f5e-4d0a", sessionID: "5e55a0b1-0000") },
                      transportFailures: { _ in XCTFail("an extension reports nothing") })
        let headers = client(hooks: hooks).headers(requestID: "req_abcdefghij012345")
        XCTAssertNil(headers["X-Installation-ID"])
        XCTAssertNil(headers["X-Session-ID"])
        XCTAssertEqual(headers["X-Request-ID"], "req_abcdefghij012345")
        XCTAssertEqual(headers["X-App-Build"], "142")
        hooks.report(APITransportFailure(path: "api/v1/me", requestID: "req_x", kind: .offline))
    }

    /// The same rule as the Android app: no installation before the legal
    /// consent, and the session only while "Share diagnostics" is on.
    func testRequestsNameNoInstallationBeforeTheConsent() {
        var wasRead = false
        let identity = ClientIdentityHolder.identity(consentGiven: false, sharesDiagnostics: true,
                                                     installationID: { wasRead = true; return "install-0001" },
                                                     sessionID: "session-0001")
        XCTAssertEqual(identity, APIClientHooks.Identity())
        XCTAssertFalse(wasRead, "the installation id is not even read (or made) before the consent")
    }

    func testTheSessionGoesOnlyWithShareDiagnostics() {
        XCTAssertEqual(ClientIdentityHolder.identity(consentGiven: true, sharesDiagnostics: false,
                                                     installationID: { "install-0001" }, sessionID: "session-0001"),
                       APIClientHooks.Identity(installationID: "install-0001", sessionID: nil),
                       "the installation stays: logout and the server's limits rely on it")
        XCTAssertEqual(ClientIdentityHolder.identity(consentGiven: true, sharesDiagnostics: true,
                                                     installationID: { "install-0001" }, sessionID: "session-0001"),
                       APIClientHooks.Identity(installationID: "install-0001", sessionID: "session-0001"))
    }

    func testNoIdentityInstalledMeansNoIdentityHeaders() {
        let headers = client(hooks: APIClientHooks(allowsIdentity: true)).headers(requestID: "req_abcdefghij012345")
        XCTAssertNil(headers["X-Installation-ID"])
        XCTAssertNil(headers["X-Session-ID"])
    }

    // MARK: Over the wire

    func testEveryRequestCarriesTheHeaders() async throws {
        StubURLProtocol.respond(status: 200, body: #"{"ok":true}"#)
        struct OK: Decodable { let ok: Bool }
        let result: OK = try await client(hooks: appHooks()).get("api/v1/me", token: "access")

        XCTAssertTrue(result.ok)
        let request = try XCTUnwrap(StubURLProtocol.lastRequest)
        XCTAssertEqual(request.url?.path, "/api/v1/me")
        XCTAssertEqual(request.value(forHTTPHeaderField: "X-Platform"), "ios")
        XCTAssertEqual(request.value(forHTTPHeaderField: "X-App-Version"), "1.3.2")
        XCTAssertEqual(request.value(forHTTPHeaderField: "X-App-Build"), "142")
        XCTAssertEqual(request.value(forHTTPHeaderField: "X-OS-Version"), "17.5")
        XCTAssertEqual(request.value(forHTTPHeaderField: "X-Request-ID"), "req_abcdefghij012345")
        XCTAssertEqual(request.value(forHTTPHeaderField: "X-Installation-ID"), "0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d")
        XCTAssertEqual(request.value(forHTTPHeaderField: "X-Session-ID"), "5e55a0b1-0000-4000-8000-000000000001")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer access")
    }

    func testAFailureCarriesTheServersRequestID() async throws {
        StubURLProtocol.respond(status: 429, headers: ["X-Request-ID": "req_server00000000001", "Retry-After": "30"],
                                body: #"{"error":{"code":"RATE_LIMITED","message":"slow down","request_id":"req_server00000000001"}}"#)
        do {
            let _: APIClient.Empty = try await client(hooks: appHooks()).perform(
                "POST", "api/v1/installations", body: ["a": "b"], token: nil)
            XCTFail("expected a failure")
        } catch let failure as APIFailure {
            XCTAssertEqual(failure.error, .rateLimited(retryAfter: 30))
            XCTAssertEqual(failure.requestID, "req_server00000000001")
            XCTAssertEqual(failure.status, 429)
            XCTAssertEqual(failure.code, "RATE_LIMITED")
            XCTAssertEqual(failure.retryAfter, 30)
        }
    }

    func testTheEnvelopeRequestIDIsUsedWithoutTheHeader() throws {
        let response = try XCTUnwrap(HTTPURLResponse(url: URL(string: "https://api.example.test")!,
                                                     statusCode: 500, httpVersion: nil, headerFields: [:]))
        let body = Data(#"{"error":{"code":"INTERNAL_ERROR","message":"x","request_id":"req_4f1c2a9b7d3e5f60"}}"#.utf8)
        XCTAssertEqual(APIClient.requestID(in: response, data: body), "req_4f1c2a9b7d3e5f60")
        XCTAssertNil(APIClient.requestID(in: response, data: Data("<html>".utf8)))
    }

    /// Screens keep matching on `APIError`; only the new calls see `APIFailure`.
    func testTheClassicCallsStillThrowAPIError() async {
        StubURLProtocol.respond(status: 401, body: #"{"error":{"code":"TOKEN_EXPIRED","message":"x"}}"#)
        do {
            let _: APIClient.Empty = try await client(hooks: appHooks()).get("api/v1/me", token: "old")
            XCTFail("expected a failure")
        } catch APIError.unauthorized {
            // expected
        } catch {
            XCTFail("unexpected \(error)")
        }
    }

    func testNoResponseAtAllIsReportedWithItsKindAndRequestID() async {
        StubURLProtocol.fail(with: URLError(.notConnectedToInternet))
        let reported = ReportedFailures()
        let hooks = APIClientHooks(allowsIdentity: true)
        hooks.install(identity: { .init() }, transportFailures: { reported.append($0) })

        do {
            let _: APIClient.Empty = try await client(hooks: hooks, requestID: "req_offline000000001")
                .perform("GET", "api/v1/me", body: Optional<APIClient.Empty>.none, token: nil)
            XCTFail("expected a failure")
        } catch let failure as APIFailure {
            XCTAssertEqual(failure.error, .offline)
            XCTAssertEqual(failure.status, 0)
            XCTAssertTrue(failure.isTransport)
            XCTAssertEqual(failure.requestID, "req_offline000000001")
        } catch {
            XCTFail("unexpected \(error)")
        }
        XCTAssertEqual(reported.all, [APITransportFailure(path: "api/v1/me", requestID: "req_offline000000001", kind: .offline)])
    }

    func testTransportFailureKinds() {
        XCTAssertEqual(APIClient.transportFailureKind(URLError(.timedOut)), .timeout)
        XCTAssertEqual(APIClient.transportFailureKind(URLError(.notConnectedToInternet)), .offline)
        XCTAssertEqual(APIClient.transportFailureKind(URLError(.cannotFindHost)), .offline)
        XCTAssertEqual(APIClient.transportFailureKind(URLError(.serverCertificateUntrusted)), .tls)
        XCTAssertEqual(APIClient.transportFailureKind(URLError(.secureConnectionFailed)), .tls)
        XCTAssertEqual(APIClient.transportFailureKind(URLError(.badServerResponse)), .io)
        XCTAssertNil(APIClient.transportFailureKind(URLError(.cancelled)), "a cancellation is not a failure")
        XCTAssertNil(APIClient.transportFailureKind(CancellationError()))
    }
}

/// Collects what an `APIClientHooks` observer was told.
final class ReportedFailures: @unchecked Sendable {
    private let lock = NSLock()
    private var items: [APITransportFailure] = []

    func append(_ failure: APITransportFailure) {
        lock.lock()
        items.append(failure)
        lock.unlock()
    }

    var all: [APITransportFailure] {
        lock.lock()
        defer { lock.unlock() }
        return items
    }
}

/// Answers `URLSession` requests from the test instead of the network.
final class StubURLProtocol: URLProtocol {

    private struct Reply {
        var status = 200
        var headers: [String: String] = [:]
        var body = Data()
        var error: Error?
    }

    private static let lock = NSLock()
    private static var reply = Reply()
    private static var requests: [URLRequest] = []

    static func respond(status: Int, headers: [String: String] = [:], body: String) {
        lock.lock()
        reply = Reply(status: status, headers: headers, body: Data(body.utf8), error: nil)
        lock.unlock()
    }

    static func fail(with error: Error) {
        lock.lock()
        reply = Reply(error: error)
        lock.unlock()
    }

    static func reset() {
        lock.lock()
        reply = Reply()
        requests = []
        lock.unlock()
    }

    static var lastRequest: URLRequest? {
        lock.lock()
        defer { lock.unlock() }
        return requests.last
    }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        Self.lock.lock()
        Self.requests.append(request)
        let reply = Self.reply
        Self.lock.unlock()

        if let error = reply.error {
            client?.urlProtocol(self, didFailWithError: error)
            return
        }
        let response = HTTPURLResponse(url: request.url!, statusCode: reply.status,
                                       httpVersion: "HTTP/1.1", headerFields: reply.headers)!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: reply.body)
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}
