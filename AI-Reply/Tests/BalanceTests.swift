import XCTest
@testable import AIReply

/// The balance beyond the plan (contract §3.2): read leniently from any
/// server, and shown as the server's numbers, split by where they come from.
///
/// Жауап балансы: ескі сервер де, жаңа сервер де дұрыс оқылады.
final class BalanceTests: XCTestCase {

    private func usage(_ json: String) throws -> AccountAPI.Usage {
        try JSONDecoder().decode(AccountAPI.Usage.self, from: Data(json.utf8))
    }

    private let planOnly = """
    {"daily_limit": 7, "used_today": 7, "remaining_today": 0, "monthly_limit": 0, "used_month": 7,
     "resets_at": "2026-10-21T19:00:00Z", "timezone": "Asia/Almaty"}
    """

    private let withBalance = """
    {"daily_limit": 7, "used_today": 7, "remaining_today": 0, "monthly_limit": 0, "used_month": 7,
     "resets_at": "2026-10-21T19:00:00Z", "timezone": "Asia/Almaty",
     "welcome": {"total": 50, "remaining": 35}, "credits": 2,
     "pass": {"expires_at": "2026-10-21T15:00:00+05:00", "remaining": 40},
     "remaining_total": 77, "bucket": "welcome"}
    """

    // MARK: Parsing

    func testAnOlderServerHasNoBalance() throws {
        let plan = try usage(planOnly)
        XCTAssertNil(plan.welcome)
        XCTAssertNil(plan.credits)
        XCTAssertNil(plan.pass)
        XCTAssertNil(plan.remainingTotal)
        XCTAssertFalse(plan.hasBalance)
        XCTAssertTrue(plan.isKnown)
        XCTAssertEqual(plan.availableNow, 0, "the plan's day is all there is")
    }

    func testTheBalanceIsRead() throws {
        let full = try usage(withBalance)
        XCTAssertEqual(full.welcome, AccountAPI.Usage.Welcome(total: 50, remaining: 35))
        XCTAssertEqual(full.credits, 2)
        XCTAssertEqual(full.pass, AccountAPI.Usage.Pass(expiresAt: "2026-10-21T15:00:00+05:00", remaining: 40))
        XCTAssertEqual(full.remainingTotal, 77)
        XCTAssertEqual(full.bucket, "welcome")
        XCTAssertEqual(full.remainingToday, 0, "remaining_today stays the plan's alone")
        XCTAssertEqual(full.availableNow, 77)
    }

    /// A part the app cannot read is dropped on its own; the plan's numbers
    /// and the other parts stay.
    func testABrokenPartCostsOnlyItself() throws {
        let broken = try usage("""
        {"daily_limit": 7, "used_today": 1, "remaining_today": 6, "monthly_limit": 0, "used_month": 1,
         "resets_at": "2026-10-21T19:00:00Z", "timezone": "Asia/Almaty",
         "welcome": {"total": "fifty"}, "credits": "many", "pass": 3, "remaining_total": 56, "bucket": 1}
        """)
        XCTAssertEqual(broken.remainingToday, 6)
        XCTAssertNil(broken.welcome)
        XCTAssertNil(broken.credits)
        XCTAssertNil(broken.pass)
        XCTAssertNil(broken.bucket)
        XCTAssertEqual(broken.remainingTotal, 56)
    }

    func testTheReplyAnswerCarriesTheBucket() throws {
        let response = try JSONDecoder().decode(AccountAPI.ReplyResponse.self, from: Data("""
        {"reply": "Ok", "usage": \(withBalance)}
        """.utf8))
        XCTAssertEqual(response.usage.bucket, "welcome")
    }

    // MARK: Summary

    func testProRemainsUnlimitedWithZeroNumericBalance() throws {
        let pro = try usage("""
        {"daily_limit": 0, "used_today": 40, "remaining_today": 0,
         "unlimited": true, "remaining_total": 0, "purchased_credits": 60, "admin_credits": 5, "credits": 65}
        """)
        XCTAssertTrue(pro.isKnown)
        XCTAssertEqual(BalanceSummary.make(pro).rows, [.unlimited, .purchased(60), .admin(5)])
        let defaults = try XCTUnwrap(UserDefaults(suiteName: "ProBalanceTests.\(UUID())"))
        defer { AccountUsageCache.clear(defaults: defaults) }
        AccountUsageCache.store(pro, defaults: defaults)
        XCTAssertTrue(AccountUsageCache.snapshot(defaults: defaults).unlimited)
        XCTAssertFalse(AccountUsageCache.snapshot(defaults: defaults).isExhausted)
        AccountUsageCache.store(try usage(planOnly), defaults: defaults)
        XCTAssertFalse(AccountUsageCache.snapshot(defaults: defaults).unlimited)
        XCTAssertTrue(AccountUsageCache.snapshot(defaults: defaults).isExhausted)
    }

    func testOneTimeWelcomeIsKnownWithoutADailyReset() throws {
        let free = try usage("""
        {"daily_limit": 0, "used_today": 0, "remaining_today": 0,
         "welcome": {"total": 10, "remaining": 7}, "remaining_total": 7}
        """)
        XCTAssertTrue(free.isKnown)
        XCTAssertEqual(BalanceSummary.make(free).rows, [.today(7), .welcome(remaining: 7, total: 10)])
    }

    func testExhaustedWelcomeDoesNotBecomeTenRepliesAgain() throws {
        let free = try usage("""
        {"daily_limit": 0, "used_today": 0, "remaining_today": 0,
         "welcome": {"total": 10, "remaining": 0}, "remaining_total": 0}
        """)
        XCTAssertTrue(free.isKnown)
        XCTAssertEqual(free.availableNow, 0)
        XCTAssertEqual(BalanceSummary.make(free).rows, [.today(0), .welcome(remaining: 0, total: 10)])
    }

    func testNothingIsANumberBeforeTheServerAnswers() {
        XCTAssertFalse(AccountAPI.Usage.unknown.isKnown)
        XCTAssertEqual(BalanceSummary.make(.unknown), .unknown)
    }

    func testEveryPartHasItsRow() throws {
        let now = try XCTUnwrap(BalanceSummary.date("2026-10-20T12:00:00Z"))
        let summary = BalanceSummary.make(try usage(withBalance), now: now)
        let passEnd = try XCTUnwrap(BalanceSummary.date("2026-10-21T10:00:00Z"))
        XCTAssertEqual(summary.rows, [.today(77), .welcome(remaining: 35, total: 50), .purchased(2),
                                      .pass(until: passEnd, remaining: 40)])
        XCTAssertEqual(summary.welcomeHeadline, AccountAPI.Usage.Welcome(total: 50, remaining: 35))
        XCTAssertEqual(summary.extraRows.count, 3)
    }

    /// Spent free replies stay listed («0 из 50») but no longer head the
    /// card; nothing purchased and an ended pass have no row at all.
    func testSpentAndEndedPartsAreQuiet() throws {
        var spent = try usage(planOnly)
        spent.welcome = AccountAPI.Usage.Welcome(total: 50, remaining: 0)
        spent.credits = 0
        spent.pass = AccountAPI.Usage.Pass(expiresAt: "2026-10-19T15:00:00Z", remaining: 3)
        spent.remainingTotal = 0
        let summary = BalanceSummary.make(spent, now: try XCTUnwrap(BalanceSummary.date("2026-10-20T12:00:00Z")))
        XCTAssertEqual(summary.rows, [.today(0), .welcome(remaining: 0, total: 50)])
        XCTAssertNil(summary.welcomeHeadline)
    }

    func testAnOlderServerShowsThePlansDay() throws {
        var plan = try usage(planOnly)
        XCTAssertEqual(BalanceSummary.make(plan).rows, [.today(0)])
        plan = AccountAPI.Usage(dailyLimit: 7, usedToday: 2, remainingToday: 5, monthlyLimit: 0, usedMonth: 2,
                                resetsAt: "2026-10-21T19:00:00Z", timezone: "Asia/Almaty")
        XCTAssertEqual(BalanceSummary.make(plan).rows, [.today(5)])
    }

    func testTheHeadlineReadsAsTheBriefSays() {
        let settings = AppSettings(store: SharedSettings(defaults: UserDefaults(suiteName: "BalanceTests.\(UUID())")!))
        let expected: [AppLanguage: String] = [
            .russian: "Бесплатные ответы: 35 из 50",
            .english: "Free replies: 35 of 50",
            .kazakh: "Тегін жауаптар: 35 / 50",
            .uzbek: "Bepul javoblar: 50 tadan 35 ta"
        ]
        for (language, value) in expected {
            settings.setLanguage(language)
            XCTAssertEqual(String(format: settings.localized("balance.welcomeTitle %lld %lld"), 35, 50), value,
                           language.rawValue)
        }
    }

    // MARK: Keyboard cache

    /// The keyboard's copy: spent for the day is not spent when free or
    /// purchased replies are left, and an older server's numbers still work.
    func testTheCacheCountsTheWholeBalance() throws {
        let group = try XCTUnwrap(UserDefaults(suiteName: "BalanceTests.\(UUID())"))
        AccountUsageCache.store(try usage(withBalance), defaults: group)
        var snapshot = AccountUsageCache.snapshot(defaults: group)
        XCTAssertEqual(snapshot.remainingTotal, 77)
        XCTAssertEqual(snapshot.welcomeRemaining, 35)
        XCTAssertEqual(snapshot.welcomeTotal, 50)
        XCTAssertEqual(snapshot.credits, 2)
        XCTAssertFalse(snapshot.isExhausted)

        AccountUsageCache.store(try usage(planOnly), defaults: group)
        snapshot = AccountUsageCache.snapshot(defaults: group)
        XCTAssertNil(snapshot.remainingTotal, "a part the server stopped sending is not kept from before")
        XCTAssertNil(snapshot.welcomeRemaining)
        XCTAssertTrue(snapshot.isExhausted)

        AccountUsageCache.clear(defaults: group)
        XCTAssertFalse(AccountUsageCache.snapshot(defaults: group).isKnown)
    }
}
