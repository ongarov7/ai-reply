import XCTest
@testable import AIReply

/// Character limits come from the server; the client mirrors them, falls back
/// to 400, and never believes a broken value.
final class AILimitsTests: XCTestCase {

    private func freshDefaults() -> UserDefaults {
        UserDefaults(suiteName: "AILimitsTests.\(UUID().uuidString)")!
    }

    func testFallbackIsFourHundred() {
        XCTAssertEqual(AILimits.fallback.sourceCharacters, 400)
        XCTAssertEqual(AILimits.load(from: freshDefaults()), .fallback)
    }

    func testPublishedValuesAreStoredAndBrokenOnesIgnored() {
        let defaults = freshDefaults()
        AILimits.store(sourceCharacters: 450, instructionCharacters: 350, defaults: defaults)
        XCTAssertEqual(AILimits.load(from: defaults), AILimits(sourceCharacters: 450, instructionCharacters: 350))

        AILimits.store(sourceCharacters: 0, instructionCharacters: 99_999, defaults: defaults)
        XCTAssertEqual(AILimits.load(from: defaults), AILimits(sourceCharacters: 450, instructionCharacters: 350),
                       "a broken payload keeps the last good values")
        XCTAssertNotNil(AILimits.lastSynced(defaults: defaults))
    }

    func testTheServerCorrectsTheClient() {
        let defaults = freshDefaults()
        AILimits.storeSourceLimit(300, defaults: defaults)
        XCTAssertEqual(AILimits.load(from: defaults).sourceCharacters, 300)
        XCTAssertEqual(AILimits.load(from: defaults).instructionCharacters, 400)
    }

    func testValidationUsesTheLimitItIsGiven() {
        let exact = String(repeating: "ә", count: 400)
        XCTAssertNotNil(try? AIReplyService.validate(message: exact, limit: 400).get())
        guard case .failure(.messageTooLong(let limit)) = AIReplyService.validate(message: exact + "ә", limit: 400) else {
            return XCTFail("401 characters must be rejected")
        }
        XCTAssertEqual(limit, 400)
    }

    func testInstructionShareLeavesRoomForTheLanguageRule() {
        let rule = ReplyInstruction.languageRule.unicodeScalars.count
        XCTAssertEqual(ReplyInstruction.maximumCharacters(serverLimit: 400), 280)
        XCTAssertEqual(ReplyInstruction.maximumCharacters(serverLimit: 300), 300 - rule - 1)
        XCTAssertEqual(ReplyInstruction.maximumCharacters(serverLimit: 60), 40)
    }

    /// A server that resolves the language itself gets no rule, so the user's
    /// share is the whole limit (still capped for a short prompt).
    func testWithoutTheLanguageRuleTheWholeLimitIsTheUsers() {
        XCTAssertEqual(ReplyInstruction.maximumCharacters(serverLimit: 400, reservesLanguageRule: false), 280)
        XCTAssertEqual(ReplyInstruction.maximumCharacters(serverLimit: 150, reservesLanguageRule: false), 150)
    }

    // MARK: Feature flags

    /// Request-shaping flags are false unless the server published them true:
    /// an unknown field sent to an older server fails the whole request.
    func testFeatureFlagsAreStoredAndDefaultToOff() throws {
        let defaults = freshDefaults()
        AILimits.storeFeatures(nil, defaults: defaults)
        for key in ["ai.features.replyPreferences", "ai.features.senderProfile", "ai.features.instructionPolish"] {
            XCTAssertFalse(defaults.bool(forKey: key), key)
        }

        let features = try JSONDecoder().decode(AccountAPI.Features.self, from: Data("""
        {"reply_preferences": true, "sender_profile": true, "instruction_polish": false, "compose": true}
        """.utf8))
        AILimits.storeFeatures(features, defaults: defaults)
        XCTAssertTrue(defaults.bool(forKey: "ai.features.replyPreferences"))
        XCTAssertTrue(defaults.bool(forKey: "ai.features.senderProfile"))
        XCTAssertFalse(defaults.bool(forKey: "ai.features.instructionPolish"))

        // A later config without the flags turns them off again.
        AILimits.storeFeatures(try JSONDecoder().decode(AccountAPI.Features.self, from: Data("{}".utf8)), defaults: defaults)
        XCTAssertFalse(defaults.bool(forKey: "ai.features.senderProfile"))
    }

    // MARK: Server errors

    func testTooLongCarriesTheServersLimit() {
        let body = Data("""
        {"error": {"code": "INVALID_REQUEST", "message": "x", "details": {"field": "source_text", "max_characters": 450}}}
        """.utf8)
        let response = HTTPURLResponse(url: URL(string: "https://example.test")!, statusCode: 400,
                                       httpVersion: nil, headerFields: nil)!
        XCTAssertEqual(APIClient.mapServerError(status: 400, data: body, headers: response), .sourceTooLong(limit: 450))
        XCTAssertEqual(AccountReplyTransport.map(.sourceTooLong(limit: 450)), .messageTooLong(limit: 450))
    }

    func testQuotaAndRateLimitAreDifferentErrors() {
        XCTAssertEqual(AccountReplyTransport.map(.dailyLimitReached(limit: 7, usedToday: 7, resetsAt: nil)), .quotaExhausted)
        XCTAssertEqual(AccountReplyTransport.map(.paymentRequired), .quotaExhausted)
        XCTAssertEqual(AccountReplyTransport.map(.rateLimited(retryAfter: 5)), .rateLimited)
        for language in AppLanguage.allCases {
            let strings = AIReplyStrings.forLanguage(language)
            XCTAssertNotEqual(strings.message(for: .quotaExhausted), strings.message(for: .rateLimited))
            XCTAssertTrue(strings.message(for: .messageTooLong(limit: 400)).contains("400"), language.rawValue)
        }
    }

    // MARK: Profile

    func testReplyLanguageOnlyGoesToServersThatKnowIt() {
        let profile = AccountReplyTransport.Profile(
            description: "  Florist in Almaty ", role: "", preferredTone: .friendly,
            business: .empty, replyLanguage: "kk"
        )
        let old = AccountReplyTransport.profileBlock(profile, serverSupportsPreferences: false,
                                                     serverSupportsSenderProfile: false)
        XCTAssertNil(old?.replyLanguage)
        XCTAssertEqual(old?.description, "Florist in Almaty")
        XCTAssertNil(old?.role)
        XCTAssertEqual(old?.preferredTone, "friendly")

        let current = AccountReplyTransport.profileBlock(profile, serverSupportsPreferences: true,
                                                         serverSupportsSenderProfile: false)
        XCTAssertEqual(current?.replyLanguage, "kk")
        XCTAssertNil(AccountReplyTransport.profileBlock(nil, serverSupportsPreferences: true,
                                                        serverSupportsSenderProfile: true))
    }

    func testAnEmptyProfileSendsNothing() {
        XCTAssertNil(AIReplyService.profile(from: .empty))
        var profile = UserProfile.empty
        profile.setRole("Дизайнер")
        XCTAssertEqual(AIReplyService.profile(from: profile)?.role, "Дизайнер")
    }
}
