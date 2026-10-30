import Foundation
import Testing
@testable import AirReplyCore

@Suite("API client")
struct APIClientTests {
    private func makeClient(_ transport: FakeTransport, store: InMemorySecureStore = InMemorySecureStore(),
                            clock: TestClock = TestClock(), tokens: SessionTokens? = nil) throws -> (APIClient, SessionStore) {
        let sessions = SessionStore(store: store)
        if let tokens { try sessions.save(tokens) }
        let client = APIClient(configuration: Fixtures.configuration, transport: transport, sessions: sessions,
                               now: { clock.now }, languageCode: { "kk" })
        return (client, sessions)
    }

    @Test func sendsHeadersUnderTheBasePath() async throws {
        let clock = TestClock()
        let transport = FakeTransport { _ in (200, "{}", [:]) }
        let (client, _) = try makeClient(transport, clock: clock, tokens: Fixtures.tokens(expiresAt: clock.now.addingTimeInterval(600)))

        try await client.send(APIRequest("GET", "/api/v1/me", idempotencyKey: "key-12345678"))

        let request = try #require(transport.requests.first)
        #expect(request.url?.absoluteString == "https://ai-reply.kz/sandbox/api/v1/me")
        #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer access-1")
        #expect(request.value(forHTTPHeaderField: "Idempotency-Key") == "key-12345678")
        #expect(request.value(forHTTPHeaderField: "Accept-Language") == "kk")
        #expect(request.value(forHTTPHeaderField: "User-Agent") == "AIReply-macOS/1.0.0 (build 7; macOS 26.6.2)")
        let requestID = try #require(request.value(forHTTPHeaderField: "X-Request-ID"))
        #expect(requestID.range(of: "^[A-Za-z0-9-]{1,64}$", options: .regularExpression) != nil)
    }

    @Test func decodesTheErrorEnvelope() async throws {
        let transport = FakeTransport { _ in
            (429, Fixtures.error("RATE_LIMITED", details: "{\"retry_after_seconds\":12}"), [:])
        }
        let (client, _) = try makeClient(transport)
        await #expect(throws: APIError.server(status: 429, code: "RATE_LIMITED", message: "x",
                                              details: ["retry_after_seconds": .number(12)], retryAfter: 12)) {
            try await client.send(APIRequest("GET", "/api/v1/config", authenticated: false))
        }
    }

    @Test func retryAfterHeaderWins() async throws {
        let transport = FakeTransport { _ in (429, Fixtures.error("RATE_LIMITED"), ["Retry-After": "30"]) }
        let (client, _) = try makeClient(transport)
        do {
            try await client.send(APIRequest("GET", "/api/v1/config", authenticated: false))
            Issue.record("expected an error")
        } catch {
            guard case let .server(_, _, _, _, retryAfter) = error else { Issue.record("\(error)"); return }
            #expect(retryAfter == 30)
        }
    }

    @Test func unreadableErrorAndTransportFailures() async throws {
        let html = FakeTransport { _ in (502, "<html>Bad gateway</html>", [:]) }
        let (client, _) = try makeClient(html)
        await #expect(throws: APIError.invalidResponse(status: 502)) {
            try await client.send(APIRequest("GET", "/api/v1/config", authenticated: false))
        }

        let offline = FakeTransport { _ in throw URLError(.notConnectedToInternet) }
        let (offlineClient, _) = try makeClient(offline)
        await #expect(throws: APIError.offline) {
            try await offlineClient.send(APIRequest("GET", "/api/v1/config", authenticated: false))
        }
    }

    @Test func withoutSessionAuthenticatedCallsFailLocally() async throws {
        let transport = FakeTransport { _ in (200, "{}", [:]) }
        let (client, _) = try makeClient(transport)
        await #expect(throws: APIError.signedOut) { try await client.send(APIRequest("GET", "/api/v1/me")) }
        #expect(transport.requests.isEmpty)
    }

    @Test func expiredAccessTokenIsRefreshedOnceAndTheCallRetried() async throws {
        let clock = TestClock()
        let transport = FakeTransport { request in
            if request.url!.path.hasSuffix("/auth/refresh") {
                return (200, Fixtures.session(access: "access-2", refresh: "refresh-2"), [:])
            }
            let auth = request.value(forHTTPHeaderField: "Authorization")
            return auth == "Bearer access-2" ? (200, "{}", [:]) : (401, Fixtures.error("UNAUTHORIZED"), [:])
        }
        let store = InMemorySecureStore()
        let (client, sessions) = try makeClient(transport, store: store, clock: clock,
                                                tokens: Fixtures.tokens(expiresAt: clock.now.addingTimeInterval(600)))
        let events = EventLog()
        await client.setEventHandler { events.record($0) }

        try await client.send(APIRequest("GET", "/api/v1/me"))

        #expect(transport.requests(to: "/auth/refresh").count == 1)
        let refreshBody = Fixtures.bodyJSON(try #require(transport.requests(to: "/auth/refresh").first))
        #expect(refreshBody["refresh_token"] as? String == "refresh-1")
        #expect((refreshBody["device"] as? [String: Any])?["platform"] as? String == "macos")
        // The rotated refresh token is persisted at once: the old one is dead on the server.
        #expect(sessions.load()?.refreshToken == "refresh-2")
        #expect(sessions.load()?.deviceID == "server-device")
        #expect(events.all == ["refreshed"])
    }

    @Test func concurrentCallsShareOneRefresh() async throws {
        let clock = TestClock()
        let transport = FakeTransport { request in
            if request.url!.path.hasSuffix("/auth/refresh") {
                try await Task.sleep(for: .milliseconds(50))
                return (200, Fixtures.session(access: "access-2", refresh: "refresh-2"), [:])
            }
            return request.value(forHTTPHeaderField: "Authorization") == "Bearer access-2"
                ? (200, "{}", [:]) : (401, Fixtures.error("TOKEN_EXPIRED"), [:])
        }
        let (client, _) = try makeClient(transport, clock: clock,
                                         tokens: Fixtures.tokens(expiresAt: clock.now.addingTimeInterval(10)))
        try await withThrowingTaskGroup(of: Void.self) { group in
            for _ in 0..<8 {
                group.addTask { try await client.send(APIRequest("GET", "/api/v1/me")) }
            }
            try await group.waitForAll()
        }
        #expect(transport.requests(to: "/auth/refresh").count == 1)
        #expect(transport.requests(to: "/api/v1/me").allSatisfy { $0.value(forHTTPHeaderField: "Authorization") == "Bearer access-2" })
    }

    @Test func refusedRefreshSignsOut() async throws {
        let clock = TestClock()
        let transport = FakeTransport { request in
            request.url!.path.hasSuffix("/auth/refresh") ? (401, Fixtures.error("UNAUTHORIZED"), [:]) : (401, Fixtures.error("UNAUTHORIZED"), [:])
        }
        let (client, sessions) = try makeClient(transport, clock: clock,
                                                tokens: Fixtures.tokens(expiresAt: clock.now.addingTimeInterval(600)))
        let events = EventLog()
        await client.setEventHandler { events.record($0) }

        await #expect(throws: APIError.signedOut) { try await client.send(APIRequest("GET", "/api/v1/me")) }
        #expect(sessions.load() == nil)
        #expect(await client.session == nil)
        #expect(events.all == ["signedOut"])
    }

    @Test func offlineRefreshKeepsTheSession() async throws {
        let clock = TestClock()
        let transport = FakeTransport { _ in throw URLError(.timedOut) }
        let (client, sessions) = try makeClient(transport, clock: clock,
                                                tokens: Fixtures.tokens(expiresAt: clock.now.addingTimeInterval(5)))
        await #expect(throws: APIError.offline) { try await client.send(APIRequest("GET", "/api/v1/me")) }
        #expect(sessions.load()?.refreshToken == "refresh-1")
    }

    @Test func otherServerErrorsAreNotRetried() async throws {
        let clock = TestClock()
        let transport = FakeTransport { _ in (403, Fixtures.error("CONSENT_REQUIRED"), [:]) }
        let (client, _) = try makeClient(transport, clock: clock, tokens: Fixtures.tokens(expiresAt: clock.now.addingTimeInterval(600)))
        await #expect(throws: (any Error).self) { try await client.send(APIRequest("POST", "/api/v1/ai/compose")) }
        #expect(transport.requests.count == 1)
    }

    @Test func multipartBodyIsWellFormed() throws {
        var form = MultipartForm(boundary: "B")
        form.add("request_id", "abc-12345")
        form.addFile("audio", fileName: "voice.m4a", contentType: "audio/mp4", data: Data([0, 1, 2]))
        let text = String(decoding: form.encoded(), as: UTF8.self)
        #expect(form.contentType == "multipart/form-data; boundary=B")
        #expect(text.hasPrefix("--B\r\nContent-Disposition: form-data; name=\"request_id\"\r\n"))
        #expect(text.contains("name=\"audio\"; filename=\"voice.m4a\"\r\nContent-Type: audio/mp4\r\n\r\n"))
        #expect(text.hasSuffix("\r\n--B--\r\n"))
    }
}
