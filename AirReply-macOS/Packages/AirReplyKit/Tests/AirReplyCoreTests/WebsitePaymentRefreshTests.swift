import Foundation
import Testing
@testable import AirReplyCore

private func paymentMe(_ credits: Int, user: String = "u-1") -> String {
    """
    {"user":{"id":"\(user)"},"usage":{"credits":\(credits),"remaining_total":\(credits)}}
    """
}

private actor PaymentResponses {
    private var credits: [Int]
    private(set) var calls = 0
    init(_ credits: [Int]) { self.credits = credits }
    func next() -> String {
        calls += 1
        let value = credits.count > 1 ? credits.removeFirst() : (credits.first ?? 0)
        return paymentMe(value)
    }
}

private actor PaymentGate {
    private var entered = false
    private var pending: CheckedContinuation<Void, Never>?
    private var observer: CheckedContinuation<Void, Never>?
    func pause() async {
        entered = true
        observer?.resume()
        observer = nil
        await withCheckedContinuation { pending = $0 }
    }
    func waitUntilEntered() async {
        if !entered { await withCheckedContinuation { observer = $0 } }
    }
    func release() { pending?.resume(); pending = nil }
}

@MainActor
private final class PaymentClock {
    var now = Date(timeIntervalSince1970: 1_800_000_000)
    var delays: [Duration] = []
    func sleep(_ delay: Duration) async throws {
        delays.append(delay)
        now += Double(delay.components.seconds) + Double(delay.components.attoseconds) / 1e18
    }
}

@Suite("Website payment balance refresh")
@MainActor
struct WebsitePaymentRefreshTests {
    private func makeAccount(_ transport: FakeTransport, clock: PaymentClock,
                             sleep: (@MainActor @Sendable (Duration) async throws -> Void)? = nil) throws -> (AccountStore, APIClient) {
        let sessions = SessionStore(store: InMemorySecureStore())
        try sessions.save(Fixtures.tokens(expiresAt: .distantFuture))
        let client = APIClient(configuration: Fixtures.configuration, transport: transport, sessions: sessions)
        let account = AccountStore(client: client, configuration: Fixtures.configuration, now: { clock.now },
                                   paymentRefreshSleep: sleep ?? { try await clock.sleep($0) })
        account.apply(try JSONDecoder.api.decode(MeResponse.self, from: Data(paymentMe(0).utf8)))
        return (account, client)
    }

    private func backend(_ responses: PaymentResponses) -> FakeTransport {
        FakeTransport { request in
            if request.url!.path.hasSuffix("/payment-handoff") {
                return (200, #"{"url":"https://ai-reply.kz/sandbox/payment#handoff=test"}"#, [:])
            }
            return (200, await responses.next(), [:])
        }
    }

    @Test func lateBankNotificationAppearsAfterReturningWithoutAnotherSignIn() async throws {
        let responses = PaymentResponses([0, 0, 500])
        let transport = backend(responses)
        let clock = PaymentClock()
        let (account, _) = try makeAccount(transport, clock: clock)
        #expect(await account.paymentHandoffURL(locale: "ru") != nil)
        #expect(account.paymentRefreshTask == nil, "opening the browser does not start background polling")
        await account.refreshForForeground(maxAge: 10)
        await account.paymentRefreshTask?.value
        #expect(account.balance?.credits == 500)
        #expect(await responses.calls == 3)
        #expect(clock.delays == [.seconds(3), .seconds(3)])
        #expect(account.paymentRefreshTask == nil)
        #expect(transport.requests(to: "/auth/refresh").isEmpty)
    }

    @Test func ordinaryForegroundRefreshDoesNotStartPolling() async throws {
        let responses = PaymentResponses([0])
        let clock = PaymentClock()
        let (account, _) = try makeAccount(backend(responses), clock: clock)
        await account.refreshForForeground()
        #expect(await responses.calls == 1)
        #expect(clock.delays.isEmpty)
        #expect(account.paymentRefreshTask == nil)
    }

    @Test func unchangedBalanceStopsAfterTwoMinutes() async throws {
        let responses = PaymentResponses([0])
        let clock = PaymentClock()
        let (account, _) = try makeAccount(backend(responses), clock: clock)
        #expect(await account.paymentHandoffURL(locale: "ru") != nil)
        await account.refreshForForeground()
        await account.paymentRefreshTask?.value
        #expect(await responses.calls == 41)
        #expect(clock.delays.count == 40)
        #expect(clock.delays.allSatisfy { $0 == .seconds(3) })
        #expect(account.paymentRefreshTask == nil)
        await account.refreshForForeground()
        #expect(await responses.calls == 42, "a later foreground does not restart the expired window")
        #expect(clock.delays.count == 40)
    }

    @Test(arguments: ["logout", "switch", "stop"])
    func cancellationStopsScheduledRequests(_ action: String) async throws {
        let responses = PaymentResponses([0])
        let gate = PaymentGate()
        let (account, _) = try makeAccount(backend(responses), clock: PaymentClock(), sleep: { _ in await gate.pause() })
        #expect(await account.paymentHandoffURL(locale: "ru") != nil)
        await account.refreshForForeground()
        let run = account.paymentRefreshTask
        await gate.waitUntilEntered()
        switch action {
        case "logout": account.clear()
        case "switch": account.apply(try JSONDecoder.api.decode(MeResponse.self, from: Data(paymentMe(7, user: "u-2").utf8)))
        default: account.cancelWebsitePaymentRefresh()
        }
        await gate.release()
        await run?.value
        #expect(await responses.calls == 1)
        #expect(account.paymentRefreshTask == nil)
        if action == "switch" { #expect(account.me?.user.id == "u-2") }
        if action == "logout" { #expect(account.me == nil) }
    }

    @Test(arguments: [false, true])
    func oldAccountResponseCannotRestoreSignedOutOrDifferentAccount(_ switching: Bool) async throws {
        let gate = PaymentGate()
        let transport = FakeTransport { _ in
            await gate.pause()
            return (200, paymentMe(500), [:])
        }
        let (account, client) = try makeAccount(transport, clock: PaymentClock())
        let request = Task { try await account.refreshMe() }
        await gate.waitUntilEntered()
        account.clear()
        if switching {
            let payload = try JSONDecoder.api.decode(SessionPayload.self, from: Data(Fixtures.session().replacingOccurrences(of: "u-1", with: "u-2").utf8))
            try await client.install(payload)
            account.apply(try JSONDecoder.api.decode(MeResponse.self, from: Data(paymentMe(7, user: "u-2").utf8)))
        } else { await client.clearSession() }
        await gate.release()
        try await request.value
        #expect(account.me?.user.id == (switching ? "u-2" : nil))
        #expect(account.balance?.credits == (switching ? 7 : nil))
    }

    @Test func olderConcurrentResponseCannotOverwriteTheFreshBalance() async throws {
        let gate = PaymentGate()
        let calls = PaymentResponses([0])
        let transport = FakeTransport { _ in
            _ = await calls.next()
            if await calls.calls == 1 {
                await gate.pause()
                return (200, paymentMe(0), [:])
            }
            return (200, paymentMe(500), [:])
        }
        let (account, _) = try makeAccount(transport, clock: PaymentClock())
        let old = Task { try await account.refreshMe() }
        await gate.waitUntilEntered()
        try await account.refreshMe()
        await gate.release()
        try await old.value
        #expect(account.balance?.credits == 500)
    }

    @Test(arguments: ["logout", "stop"])
    func canceledHandoffCannotArmAnotherRefreshWindow(_ action: String) async throws {
        let gate = PaymentGate()
        let transport = FakeTransport { _ in
            await gate.pause()
            return (200, #"{"url":"https://ai-reply.kz/sandbox/payment#handoff=old"}"#, [:])
        }
        let (account, _) = try makeAccount(transport, clock: PaymentClock())
        let request = Task { await account.paymentHandoffURL(locale: "ru") }
        await gate.waitUntilEntered()
        if action == "logout" { account.clear() }
        else { account.cancelWebsitePaymentRefresh() }
        await gate.release()
        #expect(await request.value == nil)
        #expect(account.paymentRefreshTask == nil)
    }
}
