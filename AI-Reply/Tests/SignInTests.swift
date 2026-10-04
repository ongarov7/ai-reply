import XCTest
@testable import AIReply

/// E-mail, Google and Apple sign-in: the rules the screens rely on, and the
/// keyboard haptics switch the app shares with the extension.
///
/// Кіру ережелері және пернетақта дірілінің баптауы — желісіз тексеріледі.
final class SignInTests: XCTestCase {

    // MARK: E-mail entry

    func testEmailIsTrimmedAndLowercasedLikeTheServerDoes() {
        XCTAssertEqual(EmailAddress.normalized("  Aigerim.S+tag@Mail.KZ \n"), "aigerim.s+tag@mail.kz")
    }

    func testContinueNeedsSomethingThatLooksLikeOneAddress() {
        for valid in ["user@example.com", " User@Example.com ", "a@b.co", "x7k2@privaterelay.appleid.com"] {
            XCTAssertTrue(EmailAddress.isPlausible(valid), valid)
        }
        for invalid in ["", "   ", "user", "user@", "@example.com", "user@localhost", "us er@example.com",
                        "user@example..com", "user@.example.com", "user@example.com.", "a@b@c.com"] {
            XCTAssertFalse(EmailAddress.isPlausible(invalid), invalid)
        }
    }

    // MARK: Code entry

    func testCodeFieldKeepsFourAsciiDigits() {
        XCTAssertEqual(OTPCode.sanitize("0384"), "0384", "leading zeros are part of the code")
        XCTAssertEqual(OTPCode.sanitize("48 21"), "4821")
        XCTAssertEqual(OTPCode.sanitize("Your code: 4821"), "4821", "a pasted sentence still yields the code")
        XCTAssertEqual(OTPCode.sanitize("123456"), "1234")
        XCTAssertEqual(OTPCode.sanitize("12a"), "12")
        XCTAssertEqual(OTPCode.sanitize("٤٨٢١"), "", "other scripts' digits are rejected like the server does")
        XCTAssertEqual(OTPCode.length, 4)
    }

    func testResendCountdownRoundsUpAndStopsAtZero() {
        let now = Date(timeIntervalSince1970: 1_000_000)
        XCTAssertEqual(ResendCountdown.secondsRemaining(until: now.addingTimeInterval(32), now: now), 32)
        XCTAssertEqual(ResendCountdown.secondsRemaining(until: now.addingTimeInterval(0.2), now: now), 1)
        XCTAssertEqual(ResendCountdown.secondsRemaining(until: now, now: now), 0)
        XCTAssertEqual(ResendCountdown.secondsRemaining(until: now.addingTimeInterval(-5), now: now), 0)
    }

    // MARK: Nonces

    func testAppleGetsTheSHA256OfTheNonce() {
        XCTAssertEqual(SignInNonce.sha256("abc"),
                       "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad")
    }

    func testEveryAttemptGetsAFreshURLSafeNonce() {
        let nonces = (0..<50).map { _ in SignInNonce.make() }
        XCTAssertEqual(Set(nonces).count, nonces.count)
        for nonce in nonces {
            XCTAssertGreaterThanOrEqual(nonce.count, 43)
            XCTAssertNil(nonce.rangeOfCharacter(from: CharacterSet(charactersIn: "+/= ")))
        }
    }

    func testAppleIsOfferedOnlyInBuildsWithTheEntitlement() {
        XCTAssertTrue(AppleSignIn.isEnabled(info: ["AIReplySignInWithApple": "YES"]))
        XCTAssertFalse(AppleSignIn.isEnabled(info: ["AIReplySignInWithApple": "NO"]),
                       "a Personal Team build has no entitlement, so no button")
        XCTAssertFalse(AppleSignIn.isEnabled(info: [:]))
        XCTAssertFalse(AppleSignIn.isEnabledInThisBuild, "Debug builds leave Sign in with Apple out by default")
    }

    func testAppleFullNameIsFormattedOnlyWhenGiven() {
        var components = PersonNameComponents()
        components.givenName = "Aigerim"
        components.familyName = "Seitkyzy"
        XCTAssertFalse(AppleSignIn.fullName(components).isEmpty)
        XCTAssertEqual(AppleSignIn.fullName(nil), "")
    }

    // MARK: Google configuration

    @MainActor
    func testGoogleIsOfferedOnlyWithItsClientIDAndReversedURLScheme() {
        let id = "1234-abc.apps.googleusercontent.com"
        XCTAssertEqual(GoogleSignInProvider.reversedClientID(id), "com.googleusercontent.apps.1234-abc")

        let registered: [String: Any] = ["CFBundleURLSchemes": ["com.googleusercontent.apps.1234-abc"]]
        XCTAssertEqual(GoogleSignInProvider.clientID(info: ["GIDClientID": " \(id) ", "CFBundleURLTypes": [registered]]), id)

        XCTAssertNil(GoogleSignInProvider.clientID(info: ["GIDClientID": id]),
                     "without the URL scheme the SDK would crash the app")
        let placeholder: [String: Any] = ["CFBundleURLSchemes": ["kz.ai-reply.reply.keyboard.keyboard.google-signin"]]
        XCTAssertNil(GoogleSignInProvider.clientID(info: ["GIDClientID": id, "CFBundleURLTypes": [placeholder]]))
        XCTAssertNil(GoogleSignInProvider.clientID(info: ["GIDClientID": "", "CFBundleURLTypes": [registered]]),
                     "an empty build setting keeps the button hidden")
        XCTAssertNil(GoogleSignInProvider.clientID(info: ["GIDClientID": ".apps.googleusercontent.com",
                                                          "CFBundleURLTypes": [registered]]))
    }

    // MARK: Server answers

    private func response(_ status: Int) -> HTTPURLResponse {
        HTTPURLResponse(url: URL(string: "https://example.test")!, statusCode: status, httpVersion: nil,
                        headerFields: ["Retry-After": "32"])!
    }

    private func envelope(_ code: String, details: String = "") -> Data {
        let detailsPart = details.isEmpty ? "" : ", \"details\": {\(details)}"
        return Data("{\"error\": {\"code\": \"\(code)\", \"message\": \"x\"\(detailsPart)}}".utf8)
    }

    func testSignInErrorCodesMapToTheirOwnCases() {
        let cases: [(String, Int, String, APIError)] = [
            ("INVALID_OTP", 400, "\"attempts_remaining\": 3", .invalidOTP(attemptsRemaining: 3)),
            ("OTP_ALREADY_USED", 400, "", .otpAlreadyUsed),
            ("OTP_ATTEMPTS_EXCEEDED", 429, "", .otpAttemptsExceeded),
            ("OTP_RESEND_COOLDOWN", 429, "\"retry_after_seconds\": 17", .resendCooldown(retryAfter: 17)),
            ("INVALID_EMAIL", 400, "", .invalidEmail),
            ("EMAIL_DELIVERY_FAILED", 503, "", .emailDeliveryFailed),
            ("EMAIL_ALREADY_IN_USE", 409, "", .emailInUse),
            // A rejected Google/Apple token is a 401, but it is not an expired session.
            ("INVALID_ID_TOKEN", 401, "", .invalidIDToken),
            ("AUTH_PROVIDER_UNAVAILABLE", 503, "", .authProviderUnavailable)
        ]
        for (code, status, details, expected) in cases {
            let actual = APIClient.mapServerError(status: status, data: envelope(code, details: details),
                                                  headers: response(status))
            XCTAssertEqual(actual, expected, code)
        }
    }

    @MainActor
    func testEverySignInFailureHasItsOwnTranslatedMessage() throws {
        let path = try XCTUnwrap(Bundle.main.path(forResource: "Localizable", ofType: "strings",
                                                  inDirectory: "en.lproj"))
        let table = try XCTUnwrap(NSDictionary(contentsOfFile: path) as? [String: String])
        let errors: [APIError] = [
            .invalidOTP(attemptsRemaining: 2), .otpExpired, .otpAlreadyUsed, .otpAttemptsExceeded,
            .resendCooldown(retryAfter: 10), .invalidEmail, .emailDeliveryFailed, .emailInUse,
            .invalidIDToken, .authProviderUnavailable
        ]
        let keys = errors.map(AccountModel.message(for:))
        XCTAssertEqual(Set(keys).count, keys.count, "each failure needs its own wording: \(keys)")
        for key in keys {
            XCTAssertNotNil(table[key], "\(key) is not in the string catalog")
        }
    }

    @MainActor
    func testOnlyASpentCodeEmptiesTheField() {
        for spent: APIError in [.invalidOTP(attemptsRemaining: 1), .otpExpired, .otpAlreadyUsed, .otpAttemptsExceeded] {
            XCTAssertTrue(AccountModel.codeIsSpent(spent), "\(spent)")
        }
        for transient: APIError in [.offline, .timedOut, .server, .rateLimited(retryAfter: nil)] {
            XCTAssertFalse(AccountModel.codeIsSpent(transient), "a network failure must keep the typed code")
        }
    }

    func testEmailChallengeDecodesAndFallsBackToTheServerDefaults() throws {
        let full = try JSONDecoder().decode(AccountAPI.EmailChallenge.self, from: Data("""
        {"masked_email": "a***m@mail.kz", "expires_in": 300, "resend_after": 32, "code_length": 4}
        """.utf8))
        XCTAssertEqual(full, AccountAPI.EmailChallenge(maskedEmail: "a***m@mail.kz", expiresIn: 300,
                                                       resendAfter: 32, codeLength: 4))
        let sparse = try JSONDecoder().decode(AccountAPI.EmailChallenge.self, from: Data("{}".utf8))
        XCTAssertEqual(sparse.resendAfter, 32)
        XCTAssertEqual(sparse.codeLength, 4)
    }

    func testUserKnowsItsSignInMethods() throws {
        let appleOnly = try JSONDecoder().decode(AccountAPI.User.self, from: Data("""
        {"id": "u", "email": "x@privaterelay.appleid.com", "status": "active", "locale": "kk",
         "onboarding_completed": true, "auth_providers": ["apple"]}
        """.utf8))
        XCTAssertEqual(appleOnly.authProviders, ["apple"])
        XCTAssertFalse(appleOnly.needsEmail)

        let phoneOnly = try JSONDecoder().decode(AccountAPI.User.self, from: Data("""
        {"id": "u", "phone": "+77011234567", "status": "active", "locale": "ru", "onboarding_completed": true}
        """.utf8))
        XCTAssertTrue(phoneOnly.needsEmail, "a phone-only account is offered to add an e-mail")
        XCTAssertNil(phoneOnly.authProviders, "an older server does not send the list")
    }

    // MARK: Keyboard haptics

    private func isolatedSettings() -> (SharedSettings, UserDefaults) {
        let suite = "SignInTests.\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suite)!
        addTeardownBlock { defaults.removePersistentDomain(forName: suite) }
        return (SharedSettings(defaults: defaults), defaults)
    }

    func testKeyboardHapticsAreOnByDefault() {
        let (settings, _) = isolatedSettings()
        XCTAssertTrue(settings.keyboardHapticsEnabled, "existing users keep today's behaviour")
        XCTAssertTrue(settings.keyboardHapticsActive(hasFullAccess: true))
    }

    func testKeyboardHapticsSwitchIsPersistedAndRespected() {
        let (settings, defaults) = isolatedSettings()
        settings.setKeyboardHapticsEnabled(false)
        XCTAssertFalse(SharedSettings(defaults: defaults).keyboardHapticsEnabled, "the choice survives a relaunch")
        XCTAssertFalse(settings.keyboardHapticsActive(hasFullAccess: true), "off means no vibration")

        settings.setKeyboardHapticsEnabled(true)
        XCTAssertTrue(settings.keyboardHapticsActive(hasFullAccess: true))
        XCTAssertFalse(settings.keyboardHapticsActive(hasFullAccess: false),
                       "without Full Access iOS gives a keyboard no haptics at all")
    }

    /// The app writes, the keyboard extension reads: both go through the same
    /// App Group defaults, so the switch reaches the keyboard without a relaunch.
    func testAppSettingReachesTheKeyboardThroughSharedDefaults() {
        let (store, defaults) = isolatedSettings()
        let app = AppSettings(store: store)
        XCTAssertTrue(app.keyboardHaptics)

        app.setKeyboardHaptics(false)
        let keyboardSide = SharedSettings(defaults: defaults)
        XCTAssertFalse(keyboardSide.keyboardHapticsActive(hasFullAccess: true))
        XCTAssertFalse(AppSettings(store: keyboardSide).keyboardHaptics)
    }
}
