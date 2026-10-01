import XCTest
@testable import AIReply

/// App events: only the allowlisted ones, with typed properties, sent in
/// small batches and only when both the server and the user allow it.
///
/// Оқиғалар: рұқсат етілген тізім, топтау, қайталау және тоқтату ережелері.
final class EventReporterTests: XCTestCase {

    // MARK: Fakes

    private final class FakeTransport: EventTransport, @unchecked Sendable {
        struct Call {
            let batch: EventBatch
            let token: String?
        }

        private let lock = NSLock()
        private var recorded: [Call] = []
        /// Answers for the next calls, in order; afterwards everything is accepted.
        private var script: [Result<EventBatchResult, APIFailure>] = []

        var calls: [Call] {
            lock.lock()
            defer { lock.unlock() }
            return recorded
        }

        func answer(_ results: [Result<EventBatchResult, APIFailure>]) {
            lock.lock()
            script = results
            lock.unlock()
        }

        func send(_ batch: EventBatch, accessToken: String?) async throws -> EventBatchResult {
            let next: Result<EventBatchResult, APIFailure>? = lock.withLock {
                recorded.append(Call(batch: batch, token: accessToken))
                return script.isEmpty ? nil : script.removeFirst()
            }
            switch next {
            case .some(.success(let result)): return result
            case .some(.failure(let failure)): throw failure
            case .none: return EventBatchResult(accepted: batch.events.count, rejected: 0)
            }
        }
    }

    /// Holds every request until released, the way a slow network would.
    /// Like URLSession, a request whose task is cancelled fails as `cancelled`.
    private final class HeldTransport: EventTransport, @unchecked Sendable {
        private let lock = NSLock()
        private var sent: [EventBatch] = []
        private var held: [CheckedContinuation<Bool, Never>] = []
        private var isReleased = false

        var batches: [EventBatch] { lock.withLock { sent } }

        /// Lets every request held so far, and every later one, complete.
        func release() {
            let waiting: [CheckedContinuation<Bool, Never>] = lock.withLock {
                isReleased = true
                defer { held = [] }
                return held
            }
            waiting.forEach { $0.resume(returning: true) }
        }

        func send(_ batch: EventBatch, accessToken: String?) async throws -> EventBatchResult {
            lock.withLock { sent.append(batch) }
            let answered = await withTaskCancellationHandler {
                await withCheckedContinuation { (continuation: CheckedContinuation<Bool, Never>) in
                    let now: Bool? = lock.withLock {
                        if isReleased { return true }
                        if Task.isCancelled { return false }
                        held.append(continuation)
                        return nil
                    }
                    if let now { continuation.resume(returning: now) }
                }
            } onCancel: {
                let waiting: [CheckedContinuation<Bool, Never>] = lock.withLock {
                    defer { held = [] }
                    return held
                }
                waiting.forEach { $0.resume(returning: false) }
            }
            guard answered else { throw APIFailure(error: .cancelled, requestID: "req_cancelled00000001", status: 0) }
            return EventBatchResult(accepted: batch.events.count, rejected: 0)
        }
    }

    /// The reporter's world: a clock, a session, an account and a token the
    /// test moves.
    @MainActor
    private final class Harness {
        let transport = FakeTransport()
        var now = Date(timeIntervalSince1970: 1_800_000_000)
        var session = "session-0001"
        var account = "user-1"
        var token: String? = "access-token"
        private var counter = 0

        func reporter(userAllows: Bool = true, installationID: String? = "install-0001",
                      consent: Bool? = true) -> EventReporter {
            let reporter = EventReporter(
                transport: transport,
                userAllows: userAllows,
                installationID: { installationID },
                sessionID: { [unowned self] in self.session },
                accountKey: { [unowned self] in self.account },
                accessToken: { [unowned self] in self.token },
                now: { [unowned self] in self.now },
                makeID: { [unowned self] in
                    self.counter += 1
                    return String(format: "event-%04d", self.counter)
                }
            )
            if let consent { reporter.setConsent(consent) }
            return reporter
        }
    }

    @MainActor
    private func heldReporter(_ transport: HeldTransport) -> EventReporter {
        let reporter = EventReporter(transport: transport, userAllows: true,
                                     installationID: { "install-0001" }, sessionID: { "session-0001" },
                                     accountKey: { "user-1" }, accessToken: { "access-token" })
        reporter.setServerSupport(true)
        reporter.setConsent(true)
        return reporter
    }

    @MainActor
    private func waitUntil(_ condition: () -> Bool) async {
        for _ in 0..<400 {
            if condition() { return }
            try? await Task.sleep(for: .milliseconds(5))
        }
    }

    private func failure(_ status: Int, retryAfter: Int? = nil) -> APIFailure {
        APIFailure(error: .server, requestID: "req_failure000000001", status: status, retryAfter: retryAfter)
    }

    // MARK: Allowlist

    func testEventNamesAreTheServersAllowlist() {
        let cases: [(AppEvent, String)] = [
            (.appOpened(coldStart: true), "app_opened"),
            (.appBackgrounded(foregroundSeconds: 12), "app_backgrounded"),
            (.logout, "logout"),
            (.loginFailed(method: .apple, errorCode: "failed"), "login_failed"),
            (.pushPermissionRequested, "push_permission_requested"),
            (.pushPermissionGranted(.authorized), "push_permission_granted"),
            (.pushPermissionDenied, "push_permission_denied"),
            (.pushTokenRegistered, "push_token_registered"),
            (.pushTokenRefreshed, "push_token_refreshed"),
            (.pushTokenRegistrationFailed(errorCode: "apns_3010", requestID: nil), "push_token_registration_failed"),
            (.notificationOpened(notificationID: "n1", deliveryID: "d1", type: "t"), "notification_opened"),
            (.apiError(route: "/api/v1/me", errorCode: "offline", requestID: "req_1"), "api_error")
        ]
        // internal/telemetry/events.go
        let allowlist: Set<String> = [
            "app_opened", "app_backgrounded", "login_success", "login_failed", "registration_success", "logout",
            "push_permission_requested", "push_permission_granted", "push_permission_denied",
            "push_token_registered", "push_token_refreshed", "push_token_registration_failed",
            "notification_opened", "payment_started", "payment_success", "payment_failed",
            "api_error", "unexpected_app_error"
        ]
        for (event, name) in cases {
            XCTAssertEqual(event.name, name)
            XCTAssertTrue(allowlist.contains(event.name), event.name)
            XCTAssertLessThanOrEqual(event.properties.count, 8)
        }
    }

    func testPropertiesAreTypedLikeTheServerExpects() {
        XCTAssertEqual(AppEvent.appOpened(coldStart: false).properties, ["cold_start": .bool(false)])
        XCTAssertEqual(AppEvent.appBackgrounded(foregroundSeconds: 95).properties, ["foreground_seconds": .int(95)])
        XCTAssertEqual(AppEvent.appBackgrounded(foregroundSeconds: -3).properties, ["foreground_seconds": .int(0)])
        XCTAssertEqual(AppEvent.logout.properties, [:])
        XCTAssertEqual(AppEvent.loginFailed(method: .google, errorCode: "sdk_error").properties,
                       ["method": .string("google"), "error_code": .string("sdk_error")])
        XCTAssertEqual(AppEvent.pushPermissionGranted(.provisional).properties, ["status": .string("provisional")])
        XCTAssertEqual(AppEvent.pushPermissionGranted(.denied).properties, [:], "only granted statuses are sent")
        XCTAssertEqual(AppEvent.pushTokenRegistered.properties, ["provider": .string("apns")])
        XCTAssertEqual(
            AppEvent.pushTokenRegistrationFailed(errorCode: "INVALID_REQUEST", requestID: "req_4f1c2a9b7d3e5f60").properties,
            ["provider": .string("apns"), "error_code": .string("INVALID_REQUEST"),
             "request_id": .string("req_4f1c2a9b7d3e5f60")])
        XCTAssertEqual(
            AppEvent.apiError(route: "/api/v1/me", errorCode: "timeout", requestID: "req_4f1c2a9b7d3e5f60").properties,
            ["route": .string("/api/v1/me"), "status": .int(0), "error_code": .string("timeout"),
             "request_id": .string("req_4f1c2a9b7d3e5f60")])
    }

    func testValuesTheServerWouldRefuseAreLeftOut() {
        XCTAssertEqual(AppEvent.loginFailed(method: .apple, errorCode: "Ошибка входа").properties,
                       ["method": .string("apple")])
        XCTAssertEqual(AppEvent.apiError(route: "/api/v1/me?x=1", errorCode: "offline", requestID: "").properties,
                       ["status": .int(0), "error_code": .string("offline")])
    }

    func testRoutesNeverCarryIdentifiers() {
        XCTAssertEqual(AppEvent.route(forPath: "api/v1/me"), "/api/v1/me")
        XCTAssertEqual(AppEvent.route(forPath: "api/v1/payments/8f1c2a9b/confirm"), "/api/v1/payments/{id}/confirm")
        XCTAssertEqual(AppEvent.route(forPath: "api/v1/installations/0b7c9a52-4f5e/detach"),
                       "/api/v1/installations/{id}/detach")
        XCTAssertEqual(AppEvent.route(forPath: "/api/v1/me/notification-preferences"),
                       "/api/v1/me/notification-preferences")
    }

    // MARK: Switches

    @MainActor
    func testNothingLeavesUntilTheServerOffersTelemetry() async {
        let world = Harness()
        let reporter = world.reporter()
        reporter.record(.appOpened(coldStart: true))
        await reporter.flush()
        XCTAssertTrue(world.transport.calls.isEmpty, "the server has not answered yet: events wait")
        XCTAssertEqual(reporter.queue.count, 1)

        reporter.setServerSupport(true)
        await reporter.flush()
        XCTAssertEqual(world.transport.calls.count, 1)
        XCTAssertTrue(reporter.queue.isEmpty)
    }

    @MainActor
    func testAServerWithoutTelemetryGetsNothing() async {
        let world = Harness()
        let reporter = world.reporter()
        reporter.record(.appOpened(coldStart: true))
        reporter.setServerSupport(false)
        XCTAssertTrue(reporter.queue.isEmpty)
        reporter.record(.logout)
        await reporter.flush()
        XCTAssertTrue(reporter.queue.isEmpty)
        XCTAssertTrue(world.transport.calls.isEmpty)
    }

    @MainActor
    func testNothingIsKeptFromBeforeTheConsent() async {
        let world = Harness()
        let reporter = world.reporter(consent: nil)
        reporter.setServerSupport(true)
        reporter.record(.appOpened(coldStart: true))
        XCTAssertEqual(reporter.queue.count, 1, "waits while the consent is not known yet")
        await reporter.flush()
        XCTAssertTrue(world.transport.calls.isEmpty, "nothing leaves before the consent")

        reporter.setConsent(false)
        XCTAssertTrue(reporter.queue.isEmpty, "dropped, not kept for later")
        reporter.record(.logout)
        XCTAssertTrue(reporter.queue.isEmpty)

        reporter.setConsent(true)
        await reporter.flush()
        XCTAssertTrue(world.transport.calls.isEmpty, "what came before the consent never goes")
        reporter.record(.appBackgrounded(foregroundSeconds: 3))
        await reporter.flush()
        XCTAssertEqual(world.transport.calls.map { $0.batch.events.map(\.name) }, [["app_backgrounded"]])
    }

    @MainActor
    func testABatchCarriesOnlyTheTokenOfTheAccountItHappenedUnder() async {
        let world = Harness()
        let reporter = world.reporter()
        reporter.setServerSupport(true)
        world.account = EventReporter.anonymous
        reporter.record(.appOpened(coldStart: true))
        world.account = "user-1"
        reporter.record(.logout)
        world.account = "user-2"
        reporter.record(.appBackgrounded(foregroundSeconds: 4))
        await reporter.flush()

        let calls = world.transport.calls
        XCTAssertEqual(calls.map { $0.batch.events.map(\.name) }, [["app_opened"], ["logout"], ["app_backgrounded"]])
        XCTAssertEqual(calls.map(\.token), [nil, nil, "access-token"],
                       "user-1's events never go with user-2's token")
    }

    @MainActor
    func testShareDiagnosticsOffStopsEverything() async {
        let world = Harness()
        let reporter = world.reporter()
        reporter.setServerSupport(true)
        reporter.record(.appOpened(coldStart: true))
        reporter.setUserAllows(false)
        XCTAssertTrue(reporter.queue.isEmpty, "turning it off drops what was waiting")
        reporter.record(.logout)
        await reporter.flush()
        XCTAssertTrue(world.transport.calls.isEmpty)

        let optedOut = world.reporter(userAllows: false)
        optedOut.setServerSupport(true)
        optedOut.record(.appOpened(coldStart: true))
        XCTAssertTrue(optedOut.queue.isEmpty)
    }

    // MARK: Batching

    @MainActor
    func testAtMostOneHundredWaitAndFiftyGoPerRequest() async throws {
        let world = Harness()
        let reporter = world.reporter()
        for second in 0..<120 {
            world.now = Date(timeIntervalSince1970: 1_800_000_000 + Double(second))
            reporter.record(.appBackgrounded(foregroundSeconds: second))
        }
        XCTAssertEqual(reporter.queue.count, 100)
        XCTAssertEqual(reporter.queue.first?.id, "event-0021", "the oldest are dropped first")

        reporter.setServerSupport(true)
        await reporter.flush()
        let calls = world.transport.calls
        XCTAssertEqual(calls.map { $0.batch.events.count }, [50, 50])
        XCTAssertTrue(reporter.queue.isEmpty)

        let first = try XCTUnwrap(calls.first?.batch)
        XCTAssertEqual(first.installation_id, "install-0001")
        XCTAssertEqual(first.session_id, "session-0001")
        XCTAssertEqual(first.events.first?.id, "event-0021")
        XCTAssertEqual(first.events.first?.name, "app_backgrounded")
        XCTAssertEqual(calls.first?.token, "access-token")
    }

    @MainActor
    func testOneSessionPerBatch() async {
        let world = Harness()
        let reporter = world.reporter()
        reporter.record(.appOpened(coldStart: true))
        reporter.record(.appBackgrounded(foregroundSeconds: 5))
        world.session = "session-0002"
        reporter.record(.appOpened(coldStart: false))
        reporter.setServerSupport(true)
        await reporter.flush()
        let calls = world.transport.calls
        XCTAssertEqual(calls.map(\.batch.session_id), ["session-0001", "session-0002"])
        XCTAssertEqual(calls.map { $0.batch.events.count }, [2, 1])
    }

    func testBatchBodyShape() throws {
        let batch = EventBatch(installation_id: "install-0001", session_id: "session-0001", events: [
            .init(id: "event-0001", name: "app_opened",
                  occurred_at: EventReporter.timestamp(Date(timeIntervalSince1970: 1_800_000_000)),
                  properties: ["cold_start": .bool(true)])
        ])
        let json = try XCTUnwrap(JSONSerialization.jsonObject(with: JSONEncoder().encode(batch)) as? [String: Any])
        XCTAssertEqual(Set(json.keys), ["installation_id", "session_id", "events"])
        let event = try XCTUnwrap((json["events"] as? [[String: Any]])?.first)
        XCTAssertEqual(Set(event.keys), ["id", "name", "occurred_at", "properties"])
        XCTAssertEqual((event["properties"] as? [String: Any])?["cold_start"] as? Bool, true)
        XCTAssertEqual(event["occurred_at"] as? String, "2027-01-15T08:00:00.000Z")
    }

    func testTimestampsAreRFC3339InUTC() {
        XCTAssertEqual(EventReporter.timestamp(Date(timeIntervalSince1970: 1_791_000_000.25)),
                       "2026-10-03T04:00:00.250Z")
    }

    // MARK: Failures

    @MainActor
    func testNoResponseKeepsTheEventsAndWaitsBeforeTryingAgain() async {
        let world = Harness()
        let reporter = world.reporter()
        reporter.setServerSupport(true)
        reporter.record(.appOpened(coldStart: true))
        world.transport.answer([.failure(failure(0))])

        await reporter.flush()
        XCTAssertEqual(reporter.queue.count, 1, "kept for later")
        await reporter.flush()
        XCTAssertEqual(world.transport.calls.count, 1, "not again straight away")

        world.now = world.now.addingTimeInterval(61)
        await reporter.flush()
        XCTAssertEqual(world.transport.calls.count, 2)
        XCTAssertTrue(reporter.queue.isEmpty)
    }

    @MainActor
    func testRateLimitWaitsAsLongAsTheServerAsks() async {
        let world = Harness()
        let reporter = world.reporter()
        reporter.setServerSupport(true)
        reporter.record(.logout)
        world.transport.answer([.failure(failure(429, retryAfter: 300))])
        await reporter.flush()
        world.now = world.now.addingTimeInterval(120)
        await reporter.flush()
        XCTAssertEqual(world.transport.calls.count, 1)
        world.now = world.now.addingTimeInterval(181)
        await reporter.flush()
        XCTAssertEqual(world.transport.calls.count, 2)
    }

    @MainActor
    func testARefusedBatchIsDropped() async {
        let world = Harness()
        let reporter = world.reporter()
        reporter.setServerSupport(true)
        reporter.record(.logout)
        world.transport.answer([.failure(failure(400))])
        await reporter.flush()
        XCTAssertTrue(reporter.queue.isEmpty, "the server will never take it")
        XCTAssertEqual(world.transport.calls.count, 1)
    }

    @MainActor
    func testARejectedTokenIsDroppedNotTheEvents() async {
        let world = Harness()
        let reporter = world.reporter()
        reporter.setServerSupport(true)
        reporter.record(.logout)
        world.transport.answer([.failure(failure(401))])
        await reporter.flush()
        XCTAssertEqual(world.transport.calls.map(\.token), ["access-token", nil])
        XCTAssertTrue(reporter.queue.isEmpty)
    }

    @MainActor
    func testTheServerCanSwitchTelemetryOff() async {
        let world = Harness()
        let reporter = world.reporter()
        reporter.setServerSupport(true)
        reporter.record(.logout)
        world.transport.answer([.success(EventBatchResult(accepted: 0, rejected: 0, disabled: true))])
        await reporter.flush()
        XCTAssertFalse(reporter.isSending)
        reporter.record(.appOpened(coldStart: false))
        XCTAssertTrue(reporter.queue.isEmpty)
    }

    @MainActor
    func testACancelledRequestKeepsTheBatchWithoutAPause() async {
        let world = Harness()
        let reporter = world.reporter()
        reporter.setServerSupport(true)
        reporter.record(.logout)
        world.transport.answer([.failure(APIFailure(error: .cancelled, requestID: "req_cancelled00000001", status: 0))])
        await reporter.flush()
        XCTAssertEqual(reporter.queue.count, 1, "kept")
        await reporter.flush()
        XCTAssertEqual(world.transport.calls.count, 2, "sent again at once: a cancellation is not a failure")
        XCTAssertTrue(reporter.queue.isEmpty)
    }

    // MARK: A send in progress

    /// The background flush must not give up because the minute timer's send
    /// is still on the wire: it waits, then sends what came since.
    @MainActor
    func testAFlushWaitsForTheSendInProgressThenSendsWhatCameMeanwhile() async {
        let transport = HeldTransport()
        let reporter = heldReporter(transport)
        reporter.record(.appOpened(coldStart: true))
        let timer = Task { await reporter.flush() }
        await waitUntil { transport.batches.count == 1 }

        reporter.record(.appBackgrounded(foregroundSeconds: 30))
        let background = Task { await reporter.flush() }
        try? await Task.sleep(for: .milliseconds(50))
        XCTAssertEqual(transport.batches.count, 1, "no second request racing the first")

        transport.release()
        await timer.value
        await background.value
        XCTAssertEqual(transport.batches.map { $0.events.map(\.name) }, [["app_opened"], ["app_backgrounded"]])
        XCTAssertTrue(reporter.queue.isEmpty)
    }

    /// Going to the background stops the timer; the request it started must
    /// still finish, or its events stay behind with a failure pause.
    @MainActor
    func testStoppingTheTimerDoesNotCancelARequestOnTheWire() async {
        let transport = HeldTransport()
        let reporter = heldReporter(transport)
        reporter.record(.appOpened(coldStart: true))
        let timer = Task { await reporter.flush() }
        await waitUntil { transport.batches.count == 1 }

        timer.cancel()
        transport.release()
        await timer.value
        XCTAssertTrue(reporter.queue.isEmpty, "the request finished and its events were delivered")

        reporter.record(.appBackgrounded(foregroundSeconds: 1))
        await reporter.flush()
        XCTAssertEqual(transport.batches.count, 2, "and no failure pause holds the next send back")
    }

    @MainActor
    func testWithoutAnInstallationIDNothingIsSent() async {
        let world = Harness()
        let reporter = world.reporter(installationID: nil)
        reporter.setServerSupport(true)
        reporter.record(.logout)
        await reporter.flush()
        XCTAssertTrue(world.transport.calls.isEmpty)
        XCTAssertEqual(reporter.queue.count, 1)
    }
}
