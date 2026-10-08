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
        XCTAssertNil(session.profile.gender, "a server without sender_profile sends none")
        XCTAssertNil(session.profile.onboardingVersion)
    }

    /// The sender fields are optional, and a gender value this build does not
    /// know must not fail the account.
    func testProfileDecodesTheSenderFields() throws {
        func profile(_ extra: String) throws -> AccountAPI.Profile {
            try JSONDecoder().decode(AccountAPI.Profile.self, from: Data("""
            {"display_name": "", "role": "", "description": "", "preferred_tone": "natural",
             "business_offering": "", "business_summary": "", "business_rules": [],
             "onboarding_completed": true\(extra)}
            """.utf8))
        }
        let female = try profile(#", "grammatical_gender": "female", "onboarding_version": 2"#)
        XCTAssertEqual(female.gender, .female)
        XCTAssertEqual(female.onboardingVersion, 2)
        XCTAssertNil(try profile(#", "grammatical_gender": "robot""#).gender)
    }

    /// Only what changed goes in an update; nil fields are left out entirely,
    /// so an older server never sees a field it does not know.
    func testProfileUpdateSendsOnlyWhatIsSet() throws {
        var update = AccountService.ProfileUpdate()
        update.grammatical_gender = "male"
        let json = try JSONSerialization.jsonObject(with: JSONEncoder().encode(update)) as? [String: Any]
        XCTAssertEqual(json?.count, 1)
        XCTAssertEqual(json?["grammatical_gender"] as? String, "male")
    }

    // MARK: Push and language features

    /// The push features are opt-in: a server that does not name them has
    /// none of them.
    func testPushFeaturesDecodeAndDefaultToOff() throws {
        let current = try JSONDecoder().decode(AccountAPI.Features.self, from: Data("""
        {"reply_preferences": true, "installations": true, "push_notifications": false,
         "preferred_language": true}
        """.utf8))
        XCTAssertEqual(current.installations, true)
        XCTAssertEqual(current.pushNotifications, false)
        XCTAssertEqual(current.preferredLanguage, true)

        let older = try JSONDecoder().decode(AccountAPI.Features.self, from: Data(#"{"reply_preferences": true}"#.utf8))
        XCTAssertNil(older.installations)
        XCTAssertNil(older.pushNotifications)
        XCTAssertNil(older.preferredLanguage)
    }

    /// "" is an account without a language; a missing field is a server
    /// without it.
    func testUserDecodesThePreferredLanguage() throws {
        func user(_ extra: String) throws -> AccountAPI.User {
            try JSONDecoder().decode(AccountAPI.User.self, from: Data("""
            {"id": "u1", "email": "a@example.kz", "status": "active", "locale": "ru",
             "onboarding_completed": true\(extra)}
            """.utf8))
        }
        XCTAssertEqual(try user(#", "preferred_language": "kk""#).preferredLanguage, "kk")
        XCTAssertEqual(try user(", \"preferred_language\": \"\"").preferredLanguage, "")
        XCTAssertNil(try user("").preferredLanguage)

        let confirmed = try user(", \"preferred_language\": \"\"").withPreferredLanguage("uz")
        XCTAssertEqual(confirmed.preferredLanguage, "uz")
        XCTAssertEqual(confirmed.id, "u1")
        XCTAssertEqual(confirmed.email, "a@example.kz")
    }

    /// The field goes out only when set: an older server refuses it.
    func testProfileUpdateSendsThePreferredLanguageOnlyWhenSet() throws {
        var update = AccountService.ProfileUpdate()
        update.locale = "kk"
        var json = try XCTUnwrap(JSONSerialization.jsonObject(with: JSONEncoder().encode(update)) as? [String: Any])
        XCTAssertNil(json["preferred_language"])

        update.preferred_language = "kk"
        json = try XCTUnwrap(JSONSerialization.jsonObject(with: JSONEncoder().encode(update)) as? [String: Any])
        XCTAssertEqual(json["preferred_language"] as? String, "kk")
    }

    /// The server is told one of the app's own languages, never "de".
    func testDeviceLocaleIsOneOfTheAppsLanguages() {
        XCTAssertNotNil(AppLanguage(rawValue: DeviceDescriptor.current.locale))
    }
}

/// What the release added to the account layer: plans that cannot be bought,
/// consent and quota errors, account deletion, reports, and the session's
/// guards between the app and the keyboard.
///
/// Шығарылымға қосылғандар: сатып алу, келісім, тіркелгіні жою, шағым, токен.
final class AccountReleaseTests: XCTestCase {

    private func response(_ status: Int) -> HTTPURLResponse {
        HTTPURLResponse(url: URL(string: "https://example.test")!, statusCode: status, httpVersion: nil, headerFields: nil)!
    }

    private func envelope(_ code: String) -> Data {
        Data("{\"error\": {\"code\": \"\(code)\", \"message\": \"x\"}}".utf8)
    }

    private func freshDefaults() -> UserDefaults {
        let suite = "AccountReleaseTests.\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suite)!
        addTeardownBlock { defaults.removePersistentDomain(forName: suite) }
        return defaults
    }

    private func plan(_ extra: String) throws -> AccountAPI.Plan {
        try JSONDecoder().decode(AccountAPI.Plan.self, from: Data("""
        {"id": "p2", "code": "pro", "name": {"en": "Pro"}, "description": {"en": ""}, "price": 349000,
         "price_text": "3 490 KZT", "currency": "KZT", "daily_message_limit": 50,
         "monthly_message_limit": 0, "period_days": 30, "is_free": false\(extra)}
        """.utf8))
    }

    // MARK: Plans

    func testPlanIsPurchasableOnlyWhenTheServerSaysSo() throws {
        XCTAssertTrue(try plan(#", "purchasable": true"#).purchasable)
        XCTAssertFalse(try plan(#", "purchasable": false"#).purchasable)
        XCTAssertFalse(try plan("").purchasable, "an older server does not send it, and nothing is sold then")
    }

    /// A price and Choose only next to a plan that can be bought, and only in
    /// a build that can buy at all (DEBUG here; Release has no StoreKit flow).
    @MainActor
    func testChooseNeedsABuildThatCanBuyAndAPurchasablePlan() throws {
        let model = AccountModel()
        XCTAssertTrue(AccountModel.purchasesAvailableInThisBuild, "tests run the DEBUG build")
        XCTAssertTrue(model.canPurchase(try plan(#", "purchasable": true"#)))
        XCTAssertFalse(model.canPurchase(try plan("")))
        let free = try JSONDecoder().decode(AccountAPI.Plan.self, from: Data("""
        {"id": "p1", "code": "free", "name": {"en": "Free"}, "description": {"en": ""}, "price": 0,
         "price_text": "0", "currency": "KZT", "daily_message_limit": 7, "monthly_message_limit": 0,
         "period_days": 0, "is_free": true, "purchasable": true}
        """.utf8))
        XCTAssertFalse(model.canPurchase(free))
    }

    // MARK: Config

    func testConfigCarriesSupportAndTheNewFeatures() throws {
        let legal = try JSONDecoder().decode(AccountAPI.LegalConfig.self, from: Data("""
        {"terms_version": "2026-10-08", "privacy_version": "2026-10-08", "terms_url": "https://x/offer",
         "privacy_url": "https://x/privacy", "contact_email": "", "support_url": "https://x/support",
         "account_deletion_url": "https://x/account/delete", "ai_provider": "OpenAI"}
        """.utf8))
        XCTAssertEqual(legal.resolvedSupportURL, "https://x/support")
        XCTAssertEqual(legal.aiProvider, "OpenAI")

        let older = try JSONDecoder().decode(AccountAPI.LegalConfig.self, from: Data("""
        {"terms_version": "a", "privacy_version": "b", "terms_url": "u", "privacy_url": "v"}
        """.utf8))
        XCTAssertNil(older.supportURL)
        XCTAssertEqual(older.resolvedSupportURL, "https://ai-reply.kz/support")
        XCTAssertEqual(AccountAPI.LegalConfig.production.termsVersion, "2026-10-08")

        let features = try JSONDecoder().decode(AccountAPI.Features.self, from: Data("""
        {"ai_reports": true, "account_deletion": true, "purchases": false}
        """.utf8))
        XCTAssertEqual(features.aiReports, true)
        XCTAssertEqual(features.accountDeletion, true)
        XCTAssertEqual(features.purchases, false)
    }

    /// The keyboard shows Report only for a server that takes reports.
    func testTheReportFlagReachesTheKeyboard() throws {
        let defaults = freshDefaults()
        AILimits.storeFeatures(try JSONDecoder().decode(AccountAPI.Features.self,
                                                        from: Data(#"{"ai_reports": true}"#.utf8)), defaults: defaults)
        XCTAssertTrue(defaults.bool(forKey: "ai.features.aiReports"))
        AILimits.storeFeatures(nil, defaults: defaults)
        XCTAssertFalse(defaults.bool(forKey: "ai.features.aiReports"))
    }

    // MARK: Errors

    func testMonthlyQuotaAndConsentHaveTheirOwnErrors() {
        XCTAssertEqual(APIClient.mapServerError(status: 429, data: envelope("MONTHLY_LIMIT_REACHED"), headers: response(429)),
                       .monthlyLimitReached)
        XCTAssertEqual(APIClient.mapServerError(status: 403, data: envelope("CONSENT_REQUIRED"), headers: response(403)),
                       .consentRequired)
        XCTAssertEqual(AccountReplyTransport.map(.monthlyLimitReached), .monthlyQuotaExhausted)
        XCTAssertEqual(AccountReplyTransport.map(.consentRequired), .consentRequired)
        XCTAssertEqual(AccountComposeTransport.map(.consentRequired), .consentRequired)
        for language in AppLanguage.allCases {
            let strings = AIReplyStrings.forLanguage(language)
            XCTAssertEqual(strings.message(for: .monthlyQuotaExhausted), strings.monthlyQuotaExhausted)
            XCTAssertEqual(strings.message(for: .consentRequired), strings.consentRequired)
        }
    }

    // MARK: Consent

    /// The keyboard checks the consent against the versions the server
    /// published last, before anything is sent.
    func testConsentIsCheckedAgainstThePublishedVersions() {
        let defaults = freshDefaults()
        XCTAssertFalse(LegalConsentStore.hasAcceptedCurrentVersions(defaults: defaults), "nothing accepted")

        let current = AccountAPI.LegalConfig(termsVersion: "2026-10-08", privacyVersion: "2026-10-08",
                                             termsURL: "", privacyURL: "")
        LegalConsentStore.accept(current, locale: "kk", defaults: defaults)
        XCTAssertTrue(LegalConsentStore.hasAcceptedCurrentVersions(defaults: defaults),
                      "before any config, this build's own versions count")

        LegalConsentStore.storeCurrentVersions(
            AccountAPI.LegalConfig(termsVersion: "2026-12-01", privacyVersion: "2026-10-08", termsURL: "", privacyURL: ""),
            defaults: defaults)
        XCTAssertFalse(LegalConsentStore.hasAcceptedCurrentVersions(defaults: defaults), "the terms changed")

        LegalConsentStore.storeCurrentVersions(current, defaults: defaults)
        XCTAssertTrue(LegalConsentStore.hasAcceptedCurrentVersions(defaults: defaults))
    }

    /// CONSENT_REQUIRED drops a consent the server had confirmed, but not one
    /// still waiting to be sent.
    func testAServerRefusalForgetsOnlyAConfirmedConsent() {
        let defaults = freshDefaults()
        LegalConsentStore.accept(.production, locale: "ru", defaults: defaults)
        LegalConsentStore.forgetAfterServerRefusal(defaults: defaults)
        XCTAssertNotNil(LegalConsentStore.load(defaults: defaults), "pending: the app sends it next")

        LegalConsentStore.markSynced(defaults: defaults)
        LegalConsentStore.forgetAfterServerRefusal(defaults: defaults)
        XCTAssertNil(LegalConsentStore.load(defaults: defaults))
    }

    func testRequestsStopBeforeTheNetworkWithoutASessionOrConsent() {
        XCTAssertEqual(AIReplyService.preflight(isSignedIn: false, hasConsent: false), .authenticationFailed)
        XCTAssertEqual(AIReplyService.preflight(isSignedIn: true, hasConsent: false), .consentRequired)
        XCTAssertNil(AIReplyService.preflight(isSignedIn: true, hasConsent: true))
    }

    // MARK: Account deletion

    func testDeletionIsADeleteCarryingTheAppleCodeOnlyWhenThere() throws {
        let client = APIClient(baseURL: URL(string: "https://example.test")!)
        let apple = try client.makeRequest(path: AccountService.deletionPath, method: "DELETE",
                                           body: AccountService.DeletionRequest(appleAuthorizationCode: " c0de \n"),
                                           token: "access")
        XCTAssertEqual(apple.httpMethod, "DELETE")
        XCTAssertEqual(apple.url?.absoluteString, "https://example.test/api/v1/me")
        XCTAssertEqual(apple.value(forHTTPHeaderField: "Authorization"), "Bearer access")
        XCTAssertEqual(apple.value(forHTTPHeaderField: "Content-Type"), "application/json")
        let body = try XCTUnwrap(JSONSerialization.jsonObject(with: XCTUnwrap(apple.httpBody)) as? [String: String])
        XCTAssertEqual(body, ["apple_authorization_code": "c0de"])

        let plain = try client.makeRequest(path: AccountService.deletionPath, method: "DELETE",
                                           body: AccountService.DeletionRequest(appleAuthorizationCode: ""),
                                           token: "access")
        XCTAssertEqual(String(data: try XCTUnwrap(plain.httpBody), encoding: .utf8), "{}")
    }

    func testDeletionResultDecodes() throws {
        let result = try JSONDecoder().decode(AccountService.DeletionResult.self,
                                              from: Data(#"{"deleted": true, "apple_token_revoked": false}"#.utf8))
        XCTAssertEqual(result, AccountService.DeletionResult(deleted: true, appleTokenRevoked: false))
        XCTAssertFalse(try JSONDecoder().decode(AccountService.DeletionResult.self, from: Data("{}".utf8)).deleted)
    }

    /// Everything this phone kept for the account goes; the keyboard's own
    /// settings stay.
    @MainActor
    func testDeletionWipesWhatThisDeviceKeptForTheAccount() throws {
        let group = freshDefaults()
        let app = freshDefaults()
        LegalConsentStore.accept(.production, locale: "kk", defaults: group)
        AccountUsageCache.store(try JSONDecoder().decode(AccountAPI.Usage.self, from: Data("""
        {"daily_limit": 7, "used_today": 2, "remaining_today": 5, "monthly_limit": 0, "used_month": 2,
         "resets_at": "", "timezone": "Asia/Almaty"}
        """.utf8)), defaults: group)
        AccountUsageCache.storePlanCode("pro", defaults: group)
        let words = DefaultsLearnedWordsStore(defaults: group)
        for language in [KeyboardLanguage.english, .russian, .kazakh] {
            words.saveLearnedWords(["сәлем", "hello"], for: language)
        }
        PendingProfileChange(defaults: app).markChanged()
        PendingProfileChange(defaults: app, field: .preferredLanguage).markChanged()
        group.set(false, forKey: "shared.smartCorrection")

        LocalAccountData.wipe(appGroup: group, app: app)

        XCTAssertNil(LegalConsentStore.load(defaults: group))
        XCTAssertEqual(AccountUsageCache.snapshot(defaults: group).dailyLimit, 0)
        for language in KeyboardLanguage.allCases {
            XCTAssertTrue(words.learnedWords(for: language).isEmpty, language.rawValue)
        }
        XCTAssertNotEqual(words.resetStamp, 0, "a running keyboard learns that the words went")
        XCTAssertFalse(PendingProfileChange(defaults: app).isPending)
        XCTAssertFalse(PendingProfileChange(defaults: app, field: .preferredLanguage).isPending)
        XCTAssertNotNil(group.object(forKey: "shared.smartCorrection"), "device settings stay")
    }

    // MARK: Reports

    func testAReportCarriesOnlyWhatTheUserChose() throws {
        let long = String(repeating: "ә", count: 2100)
        let report = AIReport(mode: .compose, reason: .wrongLanguage, comment: "  ", text: long)
        let json = try XCTUnwrap(JSONSerialization.jsonObject(with: JSONEncoder().encode(report)) as? [String: Any])
        XCTAssertEqual(json["mode"] as? String, "compose")
        XCTAssertEqual(json["reason"] as? String, "wrong_language")
        XCTAssertEqual(json["platform"] as? String, "ios")
        XCTAssertNotNil(json["app_version"])
        XCTAssertNil(json["comment"], "an empty comment is not sent")
        XCTAssertEqual((json["text"] as? String)?.unicodeScalars.count, AIReport.maximumTextCharacters)

        let withoutText = AIReport(mode: .reply, reason: .falseInfo, text: nil)
        let bare = try XCTUnwrap(JSONSerialization.jsonObject(with: JSONEncoder().encode(withoutText)) as? [String: Any])
        XCTAssertNil(bare["text"], "the switch was off")
        XCTAssertEqual(bare["reason"] as? String, "false_info")
        XCTAssertEqual(AIReport.Reason.allCases.map(\.rawValue),
                       ["offensive", "harmful", "false_info", "wrong_language", "other"])
    }

    // MARK: Session between the app and the keyboard

    /// A refresh answer is stored, and a refusal clears the session, only
    /// while the refresh token sent is still the stored one.
    func testARefreshOnlyTouchesItsOwnSession() {
        XCTAssertTrue(AccountSession.isSameSession(sentRefreshToken: "r1", storedRefreshToken: "r1"))
        XCTAssertFalse(AccountSession.isSameSession(sentRefreshToken: "r1", storedRefreshToken: nil), "signed out meanwhile")
        XCTAssertFalse(AccountSession.isSameSession(sentRefreshToken: "r1", storedRefreshToken: "r2"),
                       "rotated by the other process, or another account")
    }

    /// After waiting for the lock: the pair the other process stored is used
    /// instead of spending the old refresh token again.
    func testAPairRotatedElsewhereIsReused() {
        XCTAssertEqual(AccountSession.tokenRotatedElsewhere(staleRefreshToken: "r1", storedRefreshToken: "r2",
                                                            storedAccessToken: "a2", isAccessTokenFresh: true), "a2")
        XCTAssertNil(AccountSession.tokenRotatedElsewhere(staleRefreshToken: "r1", storedRefreshToken: "r1",
                                                          storedAccessToken: "a1", isAccessTokenFresh: true),
                     "nothing rotated: refresh")
        XCTAssertNil(AccountSession.tokenRotatedElsewhere(staleRefreshToken: "r1", storedRefreshToken: "r2",
                                                          storedAccessToken: "a2", isAccessTokenFresh: false))
        XCTAssertNil(AccountSession.tokenRotatedElsewhere(staleRefreshToken: "r1", storedRefreshToken: nil,
                                                          storedAccessToken: nil, isAccessTokenFresh: false))
    }

    /// Two holders of the same lock file exclude each other, like the app
    /// and the keyboard do.
    func testTheRefreshLockExcludesASecondHolder() async throws {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("lock-\(UUID().uuidString)")
        addTeardownBlock { try? FileManager.default.removeItem(at: url) }
        var other = CrossProcessLock(fileURL: url)
        other.timeout = .milliseconds(150)
        other.pollInterval = .milliseconds(20)

        let first = await CrossProcessLock(fileURL: url).acquire()
        XCTAssertTrue(first.isLocked)
        let blocked = await other.acquire()
        XCTAssertFalse(blocked.isLocked, "gave up after the timeout")

        first.release()
        first.release()
        XCTAssertFalse(first.isLocked)
        let second = await other.acquire()
        XCTAssertTrue(second.isLocked)
        second.release()

        let noContainer = await CrossProcessLock(fileURL: nil).acquire()
        XCTAssertFalse(noContainer.isLocked, "no App Group: unlocked, the actor still serialises")
    }
}
