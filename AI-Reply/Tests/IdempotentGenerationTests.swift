import XCTest
@testable import AIReply

/// One Idempotency-Key per tap on Reply, Create or Regenerate, sent again
/// only on the client's own retry of that request (contract §3.3).
///
/// Бір әрекетке бір кілт; автоматты қайталау сол кілтпен ғана.
final class IdempotentGenerationTests: XCTestCase {

    /// What each attempt sent, and the answers to give in turn.
    private actor Server {
        private(set) var headers: [[String: String]] = []
        private(set) var pauses: [Duration] = []
        private var answers: [Result<String, APIError>]

        init(_ answers: [Result<String, APIError>]) { self.answers = answers }

        func receive(_ sent: [String: String]) throws -> String {
            headers.append(sent)
            let answer = answers.isEmpty ? .failure(.server) : answers.removeFirst()
            return try answer.get()
        }

        func pause(_ duration: Duration) { pauses.append(duration) }
    }

    private func run(_ server: Server, key: IdempotencyKey?) async throws -> String {
        try await IdempotentGeneration.run(key: key, sleep: { await server.pause($0) }) { headers in
            try await server.receive(headers)
        }
    }

    // MARK: Keys

    func testEveryActionGetsItsOwnKeyInTheServersFormat() throws {
        let first = try XCTUnwrap(IdempotencyKey.forAction(serverSupportsKeys: true))
        let second = try XCTUnwrap(IdempotencyKey.forAction(serverSupportsKeys: true))
        XCTAssertNotEqual(first, second, "Regenerate is a new action")
        let pattern = try NSRegularExpression(pattern: "^[A-Za-z0-9-]{8,64}$")
        for key in [first, second] {
            XCTAssertNotNil(pattern.firstMatch(in: key.value, range: NSRange(key.value.startIndex..., in: key.value)))
        }
        XCTAssertNil(IdempotencyKey.forAction(serverSupportsKeys: false), "no key for a server that takes none")
    }

    func testTheTransportsCarryTheKeyOfTheirAction() {
        let key = IdempotencyKey("reply-key-1234")
        XCTAssertEqual(AccountComposeTransport(idempotencyKey: key).idempotencyKey, key)
        XCTAssertNil(AccountComposeTransport(idempotencyKey: nil).idempotencyKey)
    }

    func testTheRequestCarriesTheHeader() throws {
        let key = IdempotencyKey("0b7c9a52-4f5e-4d0a-9c1e-1d2f3a4b5c6d")
        let client = APIClient(baseURL: try XCTUnwrap(URL(string: "https://example.test")),
                               headers: IdempotentGeneration.headers(for: key))
        let request = try client.makeRequest(path: "api/v1/ai/reply", method: "POST", body: ["source_text": "x"], token: "t")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Idempotency-Key"), key.value)
        XCTAssertTrue(IdempotentGeneration.headers(for: nil).isEmpty)
    }

    // MARK: Retries

    /// The answer was lost on the way: asked once more, with the same key,
    /// so the server does not charge twice.
    func testALostAnswerIsAskedForAgainWithTheSameKey() async throws {
        let key = IdempotencyKey()
        let server = Server([.failure(.offline), .success("reply")])
        let reply = try await run(server, key: key)
        XCTAssertEqual(reply, "reply")
        let headers = await server.headers
        XCTAssertEqual(headers.count, 2)
        XCTAssertEqual(Set(headers.map { $0["Idempotency-Key"] }), [key.value])
        let pauses = await server.pauses
        XCTAssertEqual(pauses, [.seconds(1)])
    }

    func testOfflineIsTriedOnceMoreOnly() async {
        let server = Server([.failure(.offline), .failure(.offline), .success("never")])
        do {
            _ = try await run(server, key: IdempotencyKey())
            XCTFail("expected offline")
        } catch {
            XCTAssertEqual(error as? APIError, .offline)
        }
        let count = await server.headers.count
        XCTAssertEqual(count, 2)
    }

    /// The server is still writing our first attempt: wait and ask again.
    func testInProgressIsWaitedOut() async throws {
        let key = IdempotencyKey()
        let server = Server([.failure(.offline), .failure(.generationInProgress), .success("reply")])
        let reply = try await run(server, key: key)
        XCTAssertEqual(reply, "reply")
        let pauses = await server.pauses
        XCTAssertEqual(pauses, [.seconds(1), .seconds(2)])
        let keys = await server.headers.map { $0["Idempotency-Key"] }
        XCTAssertEqual(keys, [key.value, key.value, key.value])
    }

    func testInProgressGivesUpAfterThreeAttempts() async {
        let server = Server(Array(repeating: .failure(.generationInProgress), count: 5))
        do {
            _ = try await run(server, key: IdempotencyKey())
            XCTFail("expected in progress")
        } catch {
            XCTAssertEqual(error as? APIError, .generationInProgress)
            XCTAssertEqual(AccountReplyTransport.map(.generationInProgress), .timedOut)
        }
        let count = await server.headers.count
        XCTAssertEqual(count, IdempotentGeneration.maximumAttempts)
    }

    /// A key already paid and replayed is never sent again by itself: only a
    /// new tap generates again.
    func testAUsedKeyIsNotRetried() async {
        let server = Server([.failure(.generationKeyUsed), .success("never")])
        do {
            _ = try await run(server, key: IdempotencyKey())
            XCTFail("expected used")
        } catch {
            XCTAssertEqual(error as? APIError, .generationKeyUsed)
            XCTAssertEqual(AccountReplyTransport.map(.generationKeyUsed), .serviceUnavailable)
            XCTAssertEqual(AccountComposeTransport.map(.generationKeyUsed), .serviceUnavailable)
        }
        let count = await server.headers.count
        XCTAssertEqual(count, 1)
    }

    /// Without a key a retry would be paid again: nothing is retried and no
    /// header is sent.
    func testWithoutAKeyNothingIsRetried() async {
        let server = Server([.failure(.offline), .success("never")])
        do {
            _ = try await run(server, key: nil)
            XCTFail("expected offline")
        } catch {
            XCTAssertEqual(error as? APIError, .offline)
        }
        let headers = await server.headers
        XCTAssertEqual(headers.count, 1)
        XCTAssertNil(headers.first?["Idempotency-Key"])
    }

    func testOtherFailuresAreNotRetried() {
        let key = IdempotencyKey()
        let failures: [APIError] = [.dailyLimitReached(limit: 7, usedToday: 7, resetsAt: nil), .server, .timedOut,
                                    .rateLimited(retryAfter: 3), .consentRequired, .unauthorized, .conflict]
        for failure in failures {
            XCTAssertEqual(IdempotentGeneration.step(after: failure, attempt: 1, key: key), .fail, "\(failure)")
        }
    }

    // MARK: Server answers

    func testTheConflictsReasonIsRead() {
        func conflict(_ details: String) -> APIError {
            let data = Data("""
            {"error": {"code": "CONFLICT", "message": "x", "details": {\(details)}}}
            """.utf8)
            let response = HTTPURLResponse(url: URL(string: "https://example.test")!, statusCode: 409,
                                           httpVersion: nil, headerFields: nil)!
            return APIClient.mapServerError(status: 409, data: data, headers: response)
        }
        XCTAssertEqual(conflict("\"field\": \"idempotency_key\", \"reason\": \"in_progress\""), .generationInProgress)
        XCTAssertEqual(conflict("\"field\": \"idempotency_key\", \"reason\": \"used\""), .generationKeyUsed)
        XCTAssertEqual(conflict("\"field\": \"payment\", \"reason\": \"failed\""), .conflict)
    }
}
