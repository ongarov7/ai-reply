import Foundation
import Testing
@testable import AirReplyCore

@Suite("Models from real server answers")
struct ModelDecodingTests {
    @Test func sessionPayload() throws {
        let payload = try JSONDecoder.api.decode(SessionPayload.self, from: Data(Fixtures.session().utf8))
        #expect(payload.deviceID == "server-device")
        #expect(payload.user.email == "aigerim@mail.kz")
        #expect(payload.usage?.remainingTotal == 37)
        #expect(payload.subscription?.plan.code == "base_monthly")
        #expect(payload.legalConsent?.termsVersion == "2026-10-08")
        let tokens = payload.tokens(issuedAt: Date(timeIntervalSince1970: 0))
        #expect(tokens.accessExpiresAt == Date(timeIntervalSince1970: 900))
    }

    @Test func meWithoutConsentAndWithPass() throws {
        let json = """
            {"user":{"id":"u","status":"active"},"subscription":\(Fixtures.subscription),
             "usage":{"daily_limit":0,"unlimited":false,"purchased_credits":0,"admin_credits":2,"used_today":0,"remaining_today":0,
                      "resets_at":"2026-10-31T19:00:00Z","pass":{"expires_at":"2026-11-02T10:00:00+05:00","remaining":5}}}
            """
        let me = try JSONDecoder.api.decode(MeResponse.self, from: Data(json.utf8))
        #expect(me.legalConsent == nil)
        #expect(me.usage?.credits == 2)
        #expect(me.usage?.pass?.remaining == 5)
        // remaining_total missing: rebuilt from the parts like the server does.
        #expect(me.usage?.remainingTotal == 7)
    }

    @Test func config() throws {
        let config = try JSONDecoder.api.decode(ServerConfig.self, from: Data(Fixtures.config.utf8))
        #expect(config.voiceTranscription)
        #expect(!config.feature("missing_flag"))
        #expect(config.maxSourceCharacters == 400)
        #expect(config.legal?.privacyVersion == "2026-10-14")
        #expect(config.legal?.termsURL?.absoluteString == "https://ai-reply.kz/offer")
    }

    @Test func planNameFallsBackToRussian() throws {
        let plan = try JSONDecoder.api.decode(PlanDTO.self, from: Data(#"{"code":"pro_monthly","name":{"ru":"Про","kk":""}}"#.utf8))
        #expect(plan.localizedName("kk") == "Про")
        #expect(plan.entitlementKind == "daily")
    }
}

@Suite("Generation")
struct GenerationTests {
    private func generator(_ transport: FakeTransport) throws -> ServerGenerator {
        let clock = TestClock()
        let sessions = SessionStore(store: InMemorySecureStore())
        try sessions.save(Fixtures.tokens(expiresAt: clock.now.addingTimeInterval(600)))
        let client = APIClient(configuration: Fixtures.configuration, transport: transport, sessions: sessions,
                               now: { clock.now }, languageCode: { "ru" })
        return ServerGenerator(client: client, configuration: Fixtures.configuration)
    }

    @Test func replySendsOnlyKnownFields() async throws {
        let transport = FakeTransport { _ in
            (200, "{\"reply\":\"Конечно, завтра в 10.\",\"detected_language\":\"ru\",\"usage\":\(Fixtures.usage(remainingTotal: 36))}", [:])
        }
        let result = try await generator(transport).reply(to: "Встретимся завтра?", recipient: .work, instruction: "  ",
                                                          inputLanguage: "ru-KZ", idempotencyKey: "k-1234567890")
        #expect(result.text == "Конечно, завтра в 10.")
        #expect(result.usage?.remainingTotal == 36)
        let request = try #require(transport.requests.first)
        #expect(request.url?.path == "/sandbox/api/v1/ai/reply")
        #expect(request.value(forHTTPHeaderField: "Idempotency-Key") == "k-1234567890")
        let body = Fixtures.bodyJSON(request)
        #expect(Set(body.keys) == ["source_text", "language", "input_language", "template_id", "platform", "app_version"])
        #expect(body["template_id"] as? String == "work")
        #expect(body["input_language"] as? String == "ru")
        #expect(body["platform"] as? String == "macos")
    }

    @Test func composeBody() async throws {
        let transport = FakeTransport { _ in (200, "{\"text\":\"Привет! Давай завтра погуляем?\"}", [:]) }
        let result = try await generator(transport).compose("напиши другу, что завтра давай погуляем", regenerate: true,
                                                            inputLanguage: "de", idempotencyKey: "k-0987654321")
        #expect(result.text == "Привет! Давай завтра погуляем?")
        let body = Fixtures.bodyJSON(try #require(transport.requests.first))
        #expect(Set(body.keys) == ["instruction", "language", "regenerate", "platform", "app_version"])
        #expect(body["regenerate"] as? Bool == true)
    }

    @Test(arguments: [
        (400, "INVALID_REQUEST", #"{"field":"source_text","max_characters":400}"#, GenerationError.sourceTooLong(limit: 400)),
        (400, "INVALID_REQUEST", #"{"field":"instruction","max_characters":600}"#, GenerationError.instructionTooLong(limit: 600)),
        (400, "INVALID_REQUEST", #"{"field":"instruction"}"#, GenerationError.instructionMissing),
        (403, "CONSENT_REQUIRED", "{}", GenerationError.consentRequired),
        (403, "ACCOUNT_DISABLED", "{}", GenerationError.accountDisabled),
        (429, "RATE_LIMITED", #"{"retry_after_seconds":20}"#, GenerationError.rateLimited(retryAfter: 20)),
        (504, "AI_TIMEOUT", "{}", GenerationError.aiUnavailable),
        (502, "AI_EMPTY_RESPONSE", "{}", GenerationError.aiUnavailable),
        (500, "INTERNAL_ERROR", "{}", GenerationError.server),
    ])
    func errorMapping(status: Int, code: String, details: String, expected: GenerationError) async throws {
        let transport = FakeTransport { _ in (status, Fixtures.error(code, details: details), [:]) }
        await #expect(throws: expected) {
            try await generator(transport).compose("x", regenerate: false, inputLanguage: nil, idempotencyKey: "k-1234567890")
        }
    }

    @Test func dailyLimitCarriesTheResetTime() async throws {
        let transport = FakeTransport { _ in
            (429, Fixtures.error("DAILY_LIMIT_REACHED", details: #"{"daily_limit":30,"resets_at":"2026-10-31T19:00:00Z"}"#), [:])
        }
        await #expect(throws: GenerationError.limitReached(resetsAt: RFC3339.parse("2026-10-31T19:00:00Z"))) {
            try await generator(transport).reply(to: "a", recipient: .friend, instruction: nil, inputLanguage: nil,
                                                 idempotencyKey: "k-1234567890")
        }
    }
}

@Suite("Sign-in and account")
@MainActor
struct AuthAndAccountTests {
    private func environment(_ transport: FakeTransport) -> (APIClient, SessionStore, AuthSession, AccountStore) {
        let sessions = SessionStore(store: InMemorySecureStore())
        let client = APIClient(configuration: Fixtures.configuration, transport: transport, sessions: sessions,
                               languageCode: { "ru" })
        let auth = AuthSession(client: client, restoredEmail: nil, isSignedIn: false)
        let account = AccountStore(client: client, configuration: Fixtures.configuration)
        return (client, sessions, auth, account)
    }

    @Test func emailCodeSignIn() async throws {
        let transport = FakeTransport { request in
            switch request.url!.path {
            case let path where path.hasSuffix("/otp/request"):
                return (200, #"{"masked_email":"a*****m@mail.kz","expires_in":300,"resend_after":32,"code_length":4}"#, [:])
            case let path where path.hasSuffix("/otp/verify"):
                return (200, Fixtures.session(), [:])
            default:
                return (404, Fixtures.error("NOT_FOUND"), [:])
            }
        }
        let (client, sessions, auth, _) = environment(transport)
        var signedIn: SessionPayload?
        auth.onSignedIn = { signedIn = $0 }

        await auth.requestCode(email: "  Aigerim@Mail.kz ")
        guard case let .awaitingCode(challenge) = auth.state else { Issue.record("\(auth.state)"); return }
        #expect(challenge.codeLength == 4)
        #expect(challenge.maskedEmail == "a*****m@mail.kz")
        #expect(Fixtures.bodyJSON(transport.requests[0])["email"] as? String == "Aigerim@Mail.kz")

        #expect(await auth.verify(code: "12") == false)
        #expect(auth.lastError == .invalidCode)
        #expect(await auth.verify(code: "0384"))
        #expect(auth.state == .signedIn(email: "aigerim@mail.kz"))
        #expect(signedIn?.usage?.remainingTotal == 37)
        #expect(sessions.load()?.accessToken == "access-1")
        #expect(await client.session?.deviceID == "server-device")

        let verify = Fixtures.bodyJSON(try #require(transport.requests(to: "/otp/verify").first))
        #expect(verify["code"] as? String == "0384")
        let device = try #require(verify["device"] as? [String: Any])
        #expect(Set(device.keys) == ["device_id", "platform", "app_version", "os_version", "model", "locale", "timezone"])
    }

    @Test(arguments: [
        ("INVALID_OTP", 400, AuthError.invalidCode),
        ("OTP_EXPIRED", 400, AuthError.codeExpired),
        ("OTP_ATTEMPTS_EXCEEDED", 429, AuthError.tooManyAttempts),
        ("ACCOUNT_DISABLED", 403, AuthError.accountDisabled),
    ])
    func verifyErrors(code: String, status: Int, expected: AuthError) async throws {
        let transport = FakeTransport { request in
            request.url!.path.hasSuffix("/otp/request")
                ? (200, #"{"masked_email":"a@b.kz","expires_in":300,"resend_after":32,"code_length":4}"#, [:])
                : (status, Fixtures.error(code), [:])
        }
        let (_, _, auth, _) = environment(transport)
        await auth.requestCode(email: "a@b.kz")
        #expect(await auth.verify(code: "1234") == false)
        #expect(auth.lastError == expected)
        if case .awaitingCode = auth.state {} else { Issue.record("left the code step") }
    }

    @Test func cooldownUsesRetryAfter() async {
        let transport = FakeTransport { _ in (429, Fixtures.error("OTP_RESEND_COOLDOWN"), ["Retry-After": "17"]) }
        let (_, _, auth, _) = environment(transport)
        await auth.requestCode(email: "a@b.kz")
        #expect(auth.lastError == .cooldown(seconds: 17))
        #expect(auth.state == .signedOut)
    }

    @Test func malformedEmailNeverLeavesTheMac() async {
        let transport = FakeTransport { _ in (200, "{}", [:]) }
        let (_, _, auth, _) = environment(transport)
        await auth.requestCode(email: "not-an-email")
        #expect(auth.lastError == .invalidEmail)
        #expect(transport.requests.isEmpty)
    }

    @Test func signOutRevokesThisSessionAndForgetsTokens() async throws {
        let transport = FakeTransport { _ in (200, #"{"ok":true}"#, [:]) }
        let (client, sessions, _, _) = environment(transport)
        try await client.install(try JSONDecoder.api.decode(SessionPayload.self, from: Data(Fixtures.session().utf8)))
        let auth = AuthSession(client: client, restoredEmail: "aigerim@mail.kz", isSignedIn: true)
        var ended = false
        auth.onSignedOut = { ended = true }

        await auth.signOut()

        let logout = try #require(transport.requests(to: "/auth/logout").first)
        #expect(Fixtures.bodyJSON(logout)["refresh_token"] as? String == "refresh-1")
        #expect(logout.value(forHTTPHeaderField: "Authorization") == nil)
        #expect(sessions.load() == nil)
        #expect(auth.state == .signedOut)
        #expect(ended)
    }

    @Test func consentIsSentAsMacOSWithTheServerVersions() async throws {
        let transport = FakeTransport { request in
            switch request.url!.path {
            case let path where path.hasSuffix("/config"): return (200, Fixtures.config, [:])
            case let path where path.hasSuffix("/me/consents"): return (200, Fixtures.legalConsent, [:])
            case let path where path.hasSuffix("/me"):
                return (200, "{\"user\":{\"id\":\"u-1\"},\"usage\":\(Fixtures.usage()),\"legal_consent\":\(Fixtures.legalConsent)}", [:])
            default: return (404, Fixtures.error("NOT_FOUND"), [:])
            }
        }
        let (client, _, _, account) = environment(transport)
        try await client.install(try JSONDecoder.api.decode(SessionPayload.self, from: Data(Fixtures.session(consent: false).utf8)))

        try await account.acceptConsent()

        let body = Fixtures.bodyJSON(try #require(transport.requests(to: "/me/consents").first))
        #expect(body["platform"] as? String == "macos")
        #expect(body["terms_version"] as? String == "2026-10-08")
        #expect(body["privacy_version"] as? String == "2026-10-14")
        #expect(body["app_version"] as? String == "1.0.0")
        #expect(account.hasConsent == true)
        #expect(account.balance?.remainingTotal == 37)
    }

    @Test func balanceSummary() throws {
        let (_, _, _, account) = environment(FakeTransport { _ in (200, "{}", [:]) })
        #expect(account.balance == nil)
        let me = try JSONDecoder.api.decode(MeResponse.self, from: Data(
            "{\"user\":{\"id\":\"u\"},\"subscription\":\(Fixtures.subscription),\"usage\":\(Fixtures.usage(remainingTotal: 0))}".utf8))
        account.apply(me)
        #expect(account.balance?.isExhausted == true)
        #expect(account.balance?.planName == "Base")
        #expect(account.hasConsent == false)
        account.apply(usage: try JSONDecoder.api.decode(UsageDTO.self, from: Data(Fixtures.usage(unlimited: true).utf8)))
        #expect(account.balance?.isUnlimited == true)
        #expect(account.balance?.isExhausted == false)
    }
}
