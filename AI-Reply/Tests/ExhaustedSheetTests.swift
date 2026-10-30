import XCTest
@testable import AIReply

/// «Лимит ответов исчерпан» (contract §3.9): when the app shows it, and
/// whether it offers to buy more.
///
/// Лимит біткенде шығатын парақ: қашан және не ұсынады.
@MainActor
final class ExhaustedSheetTests: XCTestCase {

    private let canBuy = LimitRefusal(canBuy: true, welcomeRemaining: 0, creditsRemaining: 0, passActive: false)

    // MARK: Decision

    func testServerPurchaseFlagsCannotOfferCommerce() {
        for enabled in [true, false] {
            let offer = ExhaustedOffer.decide(failure: .quotaExhausted, refusal: canBuy, purchasesEnabled: enabled)
            XCTAssertEqual(offer, ExhaustedOffer())
            XCTAssertEqual(offer?.offersPurchase, false)
        }
        XCTAssertEqual(ExhaustedOffer.decide(failure: .monthlyQuotaExhausted, refusal: .unknown, purchasesEnabled: true),
                       ExhaustedOffer())
    }

    /// No sheet for anything but a spent limit - and not for a refusal that
    /// only reads like one (an expired subscription leaves no limit refusal).
    func testNothingElseOpensTheSheet() {
        XCTAssertNil(ExhaustedOffer.decide(failure: .quotaExhausted, refusal: nil, purchasesEnabled: true))
        for failure: AIReplyError in [.offline, .timedOut, .rateLimited, .serviceUnavailable, .consentRequired] {
            XCTAssertNil(ExhaustedOffer.decide(failure: failure, refusal: canBuy, purchasesEnabled: true), "\(failure)")
        }
        XCTAssertNil(ExhaustedOffer.decide(failure: nil, refusal: canBuy, purchasesEnabled: true))
    }

    func testExhaustionHasOnlyNeutralBalanceCopy() {
        let offer = ExhaustedOffer()
        for gender: GrammaticalGender? in [nil, .male, .female, .unspecified] {
            XCTAssertEqual(offer.bodyKey(gender: gender), "exhausted.body.balance")
        }
        for language in AppLanguage.allCases {
            for key in ["exhausted.body.balance", "exhausted.balance"] {
                XCTAssertNotEqual(AppSettings.localized(key, language: language), key)
            }
        }
    }

    // MARK: The server's refusal

    private func refusal(_ code: String, details: String) -> APIError {
        let data = Data("""
        {"error": {"code": "\(code)", "message": "x", "details": {\(details)}}}
        """.utf8)
        let response = HTTPURLResponse(url: URL(string: "https://example.test")!, statusCode: 429,
                                       httpVersion: nil, headerFields: nil)!
        return APIClient.mapServerError(status: 429, data: data, headers: response)
    }

    func testTheRefusalSaysWhetherMoreCanBeBought() {
        let daily = refusal("DAILY_LIMIT_REACHED", details: """
        "daily_limit": 7, "used_today": 7, "resets_at": "2026-10-21T19:00:00Z",
        "welcome_remaining": 0, "credits_remaining": 0, "pass_active": true, "can_buy": true
        """)
        XCTAssertEqual(daily.limitRefusal, LimitRefusal(canBuy: true, welcomeRemaining: 0, creditsRemaining: 0, passActive: true))
        XCTAssertEqual(AccountReplyTransport.map(daily), .quotaExhausted, "the keyboard's message is unchanged")

        let monthly = refusal("MONTHLY_LIMIT_REACHED", details: "\"can_buy\": false")
        XCTAssertEqual(monthly.limitRefusal, .unknown)
        XCTAssertEqual(AccountReplyTransport.map(monthly), .monthlyQuotaExhausted)

        let older = refusal("DAILY_LIMIT_REACHED", details: "\"daily_limit\": 7, \"used_today\": 7")
        XCTAssertEqual(older.limitRefusal, .unknown, "an older server: nothing to buy")
        XCTAssertNil(APIError.paymentRequired.limitRefusal)
    }

    func testTheRefusalIsKeptOnlyForTheRequestThatFailed() throws {
        let group = try XCTUnwrap(UserDefaults(suiteName: "ExhaustedSheetTests.\(UUID())"))
        let now = Date(timeIntervalSince1970: 1_800_000_000)
        XCTAssertNil(AccountUsageCache.lastLimitRefusal(now: now, defaults: group))

        AccountUsageCache.storeLimitRefusal(canBuy, now: now, defaults: group)
        XCTAssertEqual(AccountUsageCache.lastLimitRefusal(now: now.addingTimeInterval(5), defaults: group), canBuy)
        XCTAssertNil(AccountUsageCache.lastLimitRefusal(now: now.addingTimeInterval(600), defaults: group),
                     "yesterday's refusal does not answer today's request")

        AccountUsageCache.clear(defaults: group)
        XCTAssertNil(AccountUsageCache.lastLimitRefusal(now: now, defaults: group))
    }

    // MARK: Create a reply

    /// A spent limit in «Создать ответ» opens the sheet; the error line stays,
    /// and the next attempt starts without it.
    func testCreateAReplyOpensTheSheetForASpentLimit() async throws {
        let service = AIReplyService(transportOverride: { _, _ in
            MockReplyTransport(delay: .milliseconds(1), result: .failure(.quotaExhausted))
        })
        let defaults = try XCTUnwrap(UserDefaults(suiteName: "ExhaustedSheetTests.\(UUID())"))
        let model = ComposeViewModel(settings: SharedSettings(defaults: defaults), service: service,
                                     limitRefusal: { self.canBuy })
        model.restoreRecipient(visible: ReplyTemplate.defaults)
        model.message = "Здравствуйте! Сколько стоит доставка?"

        model.generate(configuration: .initial, language: .russian, purchasesEnabled: true)
        for _ in 0..<200 where model.isGenerating {
            try await Task.sleep(for: .milliseconds(10))
        }
        XCTAssertEqual(model.failure, .quotaExhausted)
        XCTAssertNotNil(model.errorMessage)
        XCTAssertEqual(model.exhausted, ExhaustedOffer())

        model.dismissExhausted()
        XCTAssertNil(model.exhausted)
        XCTAssertNotNil(model.errorMessage, "the line under the reply stays")
    }
}
