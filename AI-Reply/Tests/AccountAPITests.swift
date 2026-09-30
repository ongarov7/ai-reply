import XCTest
@testable import AIReply

/// The account layer's pure decisions: error mapping, localization fallbacks
/// and what the client is allowed to assume.
///
/// Тіркелгі қабатының таза логикасы — желісіз тексеріледі.
///
/// Networking itself is not mocked here. What matters, and what these cover, is
/// that a server answer always turns into exactly one value the UI can render,
/// and that nothing in that path invents a message of its own.
final class AccountAPITests: XCTestCase {

    private func response(_ status: Int, retryAfter: String? = nil) -> HTTPURLResponse {
        var headers: [String: String] = [:]
        if let retryAfter { headers["Retry-After"] = retryAfter }
        return HTTPURLResponse(url: URL(string: "https://example.test")!,
                               statusCode: status, httpVersion: nil, headerFields: headers)!
    }

    private func envelope(_ code: String, details: String = "") -> Data {
        let detailsPart = details.isEmpty ? "" : ", \"details\": {\(details)}"
        return Data("""
        {"error": {"code": "\(code)", "message": "ignored"\(detailsPart)}}
        """.utf8)
    }

    // MARK: Error mapping

    func testQuotaErrorCarriesTheNumbersTheScreenNeeds() {
        let error = APIClient.mapServerError(
            status: 429,
            data: envelope("DAILY_LIMIT_REACHED",
                           details: "\"daily_limit\": 7, \"used_today\": 7, \"resets_at\": \"2026-03-11T19:00:00Z\""),
            headers: response(429)
        )
        guard case let .dailyLimitReached(limit, used, resetsAt) = error else {
            return XCTFail("expected a quota error, got \(error)")
        }
        XCTAssertEqual(limit, 7)
        XCTAssertEqual(used, 7)
        XCTAssertEqual(resetsAt, "2026-03-11T19:00:00Z")
    }

    func testStableCodesMapToStableCases() {
        let cases: [(String, Int, APIError)] = [
            ("UNAUTHORIZED", 401, .unauthorized),
            ("TOKEN_EXPIRED", 401, .unauthorized),
            ("ACCOUNT_DISABLED", 403, .accountDisabled),
            ("INVALID_OTP", 400, .invalidOTP(attemptsRemaining: nil)),
            ("OTP_EXPIRED", 400, .otpExpired),
            ("AI_TIMEOUT", 504, .providerTimeout),
            ("AI_PROVIDER_UNAVAILABLE", 502, .providerUnavailable),
            ("INVALID_REQUEST", 400, .invalidRequest),
            ("CONFLICT", 409, .conflict)
        ]
        for (code, status, expected) in cases {
            let actual = APIClient.mapServerError(status: status, data: envelope(code), headers: response(status))
            XCTAssertEqual(actual, expected, "code \(code)")
        }
    }

    /// A proxy or load balancer can answer without our envelope. The client
    /// still has to produce something sensible rather than crash or guess.
    func testFallsBackToTheStatusWhenThereIsNoEnvelope() {
        XCTAssertEqual(APIClient.mapServerError(status: 401, data: Data("<html>".utf8), headers: response(401)),
                       .unauthorized)
        XCTAssertEqual(APIClient.mapServerError(status: 500, data: Data(), headers: response(500)), .server)
        XCTAssertEqual(APIClient.mapServerError(status: 429, data: Data(), headers: response(429, retryAfter: "30")),
                       .rateLimited(retryAfter: 30))
    }

    /// The reply flow has a closed error set; every backend failure has to land
    /// in it, because the keyboard can only show those.
    func testBackendErrorsBecomeReplyErrors() {
        XCTAssertEqual(AccountReplyTransport.map(.offline), .offline)
        XCTAssertEqual(AccountReplyTransport.map(.unauthorized), .authenticationFailed)
        XCTAssertEqual(AccountReplyTransport.map(.accountDisabled), .authenticationFailed)
        XCTAssertEqual(AccountReplyTransport.map(.dailyLimitReached(limit: 7, usedToday: 7, resetsAt: nil)), .quotaExhausted)
        XCTAssertEqual(AccountReplyTransport.map(.rateLimited(retryAfter: nil)), .rateLimited)
        XCTAssertEqual(AccountReplyTransport.map(.providerTimeout), .timedOut)
        XCTAssertEqual(AccountReplyTransport.map(.emptyResponse), .emptyResponse)
        XCTAssertEqual(AccountReplyTransport.map(.server), .serviceUnavailable)
    }

    func testKeyboardUsesAccountWording() {
        for language in AppLanguage.allCases {
            let strings = AIReplyStrings.forLanguage(language)
            XCTAssertEqual(strings.message(for: .authenticationFailed), strings.signInRequired)
            XCTAssertEqual(strings.message(for: .quotaExhausted), strings.quotaExhausted)
            XCTAssertEqual(strings.message(for: .rateLimited), strings.rateLimited)
            XCTAssertFalse(strings.signInRequired.isEmpty)
            XCTAssertFalse(strings.quotaExhausted.isEmpty)
        }
    }

    @MainActor
    func testEveryFailureHasALocalizationKey() {
        let errors: [APIError] = [.offline, .timedOut, .invalidOTP(attemptsRemaining: nil), .otpExpired,
                                  .rateLimited(retryAfter: nil), .unauthorized, .accountDisabled,
                                  .invalidRequest, .dailyLimitReached(limit: 0, usedToday: 0, resetsAt: nil),
                                  .paymentRequired, .server, .malformedResponse]
        for error in errors {
            let key = AccountModel.message(for: error)
            XCTAssertFalse(key.isEmpty)
            XCTAssertTrue(key.contains("."), "\(key) does not look like a localization key")
        }
    }

    // MARK: Models

    func testPlanNameFallsBackToEnglish() {
        let plan = AccountAPI.Plan(
            id: "1", code: "pro",
            name: ["en": "Pro", "kk": "Pro KK"],
            description: ["en": "Description"],
            price: 349_000, priceText: "3 490 KZT", currency: "KZT",
            dailyLimit: 50, monthlyLimit: 0, periodDays: 30, isFree: false
        )
        XCTAssertEqual(plan.localizedName("kk"), "Pro KK")
        XCTAssertEqual(plan.localizedName("ru"), "Pro", "a missing translation must not show a key")
        XCTAssertEqual(plan.localizedDescription("kk"), "Description")
    }

    /// The session decoder has to accept exactly what the server sends, snake
    /// case and all - a rename on either side should fail here, not on a phone.
    func testSessionDecodesTheServerPayload() throws {
        let payload = Data("""
        {
          "access_token": "a", "refresh_token": "r", "token_type": "Bearer",
          "expires_in": 900, "refresh_expires_at": "2026-04-09T09:00:00Z",
          "device_id": "device", "is_new_user": true,
          "user": {"id": "u1", "phone": "+77011234567", "status": "active",
                   "locale": "kk", "created_at": "2026-03-10T09:00:00Z", "onboarding_completed": false},
          "profile": {"display_name": "", "role": "", "description": "", "preferred_tone": "natural",
                      "business_offering": "", "business_summary": "", "business_rules": [],
                      "onboarding_completed": false, "updated_at": "2026-03-10T09:00:00Z"},
          "subscription": {"status": "active", "plan": {"id": "p1", "code": "free",
                           "name": {"en": "Free"}, "description": {"en": ""}, "price": 0,
                           "price_text": "0", "currency": "KZT", "daily_message_limit": 7,
                           "monthly_message_limit": 0, "period_days": 0, "is_free": true, "sort_order": 10}},
          "usage": {"daily_limit": 7, "used_today": 0, "remaining_today": 7, "monthly_limit": 0,
                    "used_month": 0, "resets_at": "2026-03-11T19:00:00Z", "timezone": "Asia/Almaty"}
        }
        """.utf8)

        let session = try JSONDecoder().decode(AccountAPI.Session.self, from: payload)
        XCTAssertEqual(session.accessToken, "a")
        XCTAssertEqual(session.expiresIn, 900)
        XCTAssertTrue(session.isNewUser)
        XCTAssertEqual(session.user.identifier, "+77011234567")
        XCTAssertEqual(session.subscription.plan.dailyLimit, 7)
        XCTAssertEqual(session.usage.remainingToday, 7)
    }
}
