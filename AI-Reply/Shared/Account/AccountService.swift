import Foundation

/// Every call the app makes to the AI Reply backend.
///
/// Барлық сұраныс осы жерден өтеді: қайталанатын желі коды жоқ.
///
/// The service holds no state of its own. Tokens belong to `AccountSession`,
/// and the base URL to `AIConfiguration`, so a screen only has to say what it
/// wants, not how authentication works.
struct AccountService: Sendable {

    private let session: AccountSession

    init(session: AccountSession = .shared) {
        self.session = session
    }

    private func client() throws -> APIClient {
        guard let baseURL = AIConfiguration.shared.backendBaseURL else {
            throw APIError.invalidRequest
        }
        return APIClient(baseURL: baseURL)
    }

    // MARK: Public endpoints

    /// Countries, limits and current legal versions. Called before sign-in.
    func serverConfig() async throws -> AccountAPI.ServerConfig {
        try await client().get("api/v1/config")
    }

    func plans() async throws -> [AccountAPI.Plan] {
        let list: AccountAPI.PlanList = try await client().get("api/v1/plans")
        return list.plans
    }

    // MARK: Sign-in
    //
    // The server verifies everything: the e-mail code itself, and the Google
    // or Apple token against the provider's keys. The app sends only what the
    // server needs to check; it never states who the user is.

    /// Sends a 4-digit code to the address. Sign-up and sign-in are one flow.
    func requestEmailCode(email: String, locale: String) async throws -> AccountAPI.EmailChallenge {
        struct Request: Encodable {
            let email: String
            let locale: String
        }
        return try await client().post("api/v1/auth/email/otp/request",
                                       body: Request(email: email, locale: locale))
    }

    /// Verifies the e-mail code and stores the resulting session.
    @discardableResult
    func verifyEmailCode(email: String, code: String) async throws -> AccountAPI.Session {
        struct Request: Encodable {
            let email: String
            let code: String
            let device: DeviceDescriptor
        }
        return try await adopt(client().post(
            "api/v1/auth/email/otp/verify",
            body: Request(email: email, code: code, device: .current)
        ))
    }

    /// Exchanges a Google ID token for a session. `nonce` is the value the app
    /// gave Google for this attempt; the server checks the token carries it.
    @discardableResult
    func signInWithGoogle(idToken: String, nonce: String) async throws -> AccountAPI.Session {
        struct Request: Encodable {
            let id_token: String
            let nonce: String
            let device: DeviceDescriptor
        }
        return try await adopt(client().post(
            "api/v1/auth/google",
            body: Request(id_token: idToken, nonce: nonce, device: .current)
        ))
    }

    /// Exchanges an Apple identity token for a session. `rawNonce` is the
    /// unhashed value; Apple's token carries its SHA-256. `fullName` is only
    /// ever present on the first authorization.
    @discardableResult
    func signInWithApple(identityToken: String, rawNonce: String, fullName: String) async throws -> AccountAPI.Session {
        struct Request: Encodable {
            let identity_token: String
            let nonce: String
            let full_name: String
            let device: DeviceDescriptor
        }
        return try await adopt(client().post(
            "api/v1/auth/apple",
            body: Request(identity_token: identityToken, nonce: rawNonce, full_name: fullName, device: .current)
        ))
    }

    private func adopt(_ result: AccountAPI.Session) async -> AccountAPI.Session {
        await session.adopt(result)
        return result
    }

    func signOut(installationID: String? = nil) async {
        await session.signOut(installationID: installationID)
    }

    // MARK: Authenticated endpoints

    func account() async throws -> AccountAPI.Account {
        try await session.authenticated { token in
            try await client().get("api/v1/me", token: token)
        }
    }

    func usage() async throws -> AccountAPI.Usage {
        try await session.authenticated { token in
            try await client().get("api/v1/me/usage", token: token)
        }
    }

    func subscription() async throws -> AccountAPI.Subscription {
        try await session.authenticated { token in
            try await client().get("api/v1/me/subscription", token: token)
        }
    }

    /// Sends only the fields that changed - every property is optional on the
    /// wire, so an empty edit is a no-op rather than an accidental wipe.
    struct ProfileUpdate: Encodable, Sendable {
        var display_name: String?
        var role: String?
        var description: String?
        var preferred_tone: String?
        var business_offering: String?
        var business_summary: String?
        var business_rules: [String]?
        var locale: String?
        var timezone: String?
        var onboarding_completed: Bool?
        /// Only for a server that publishes `sender_profile`; an older one
        /// rejects the whole update over an unknown field.
        var grammatical_gender: String?
        var onboarding_version: Int?
        /// The language of the account's notifications. Only for a server that
        /// publishes `preferred_language`.
        var preferred_language: String?
    }

    @discardableResult
    func updateProfile(_ update: ProfileUpdate) async throws -> AccountAPI.Profile {
        try await session.authenticated { token in
            try await client().patch("api/v1/me", body: update, token: token)
        }
    }

    /// Registers this device so a push token has somewhere to live later.
    func registerDevice(pushToken: String? = nil) async throws {
        struct Request: Encodable {
            let device_id: String
            let platform: String
            let app_version: String
            let os_version: String
            let locale: String
            let push_token: String?
            let push_enabled: Bool
        }
        let descriptor = DeviceDescriptor.current
        let request = Request(
            device_id: descriptor.device_id,
            platform: descriptor.platform,
            app_version: descriptor.app_version,
            os_version: descriptor.os_version,
            locale: descriptor.locale,
            push_token: pushToken,
            push_enabled: pushToken != nil
        )
        let _: APIClient.Empty = try await session.authenticated { token in
            try await client().post("api/v1/devices", body: request, token: token)
        }
    }

    /// Sends a code to an address the signed-in account wants to add, so an
    /// account opened with a phone number keeps a way to sign in.
    func requestLinkEmailCode(email: String, locale: String) async throws -> AccountAPI.EmailChallenge {
        struct Request: Encodable, Sendable {
            let email: String
            let locale: String
        }
        let request = Request(email: email, locale: locale)
        return try await session.authenticated { token in
            try await client().post("api/v1/me/email/otp/request", body: request, token: token)
        }
    }

    /// Verifies the code and attaches the address to the signed-in account.
    func verifyLinkEmailCode(email: String, code: String) async throws -> AccountAPI.Account {
        struct Request: Encodable, Sendable {
            let email: String
            let code: String
        }
        let request = Request(email: email, code: code)
        return try await session.authenticated { token in
            try await client().post("api/v1/me/email/otp/verify", body: request, token: token)
        }
    }

    @discardableResult
    func recordLegalConsent(_ consent: StoredLegalConsent) async throws -> AccountAPI.LegalConsent {
        struct Request: Encodable {
            let terms_version: String
            let privacy_version: String
            let locale: String
            let platform: String
            let app_version: String
        }
        let request = Request(
            terms_version: consent.termsVersion,
            privacy_version: consent.privacyVersion,
            locale: consent.locale,
            platform: consent.platform,
            app_version: consent.appVersion
        )
        return try await session.authenticated { token in
            try await client().post("api/v1/me/consents", body: request, token: token)
        }
    }

    /// Withdraws the AI consent: until the app records a new one, the AI
    /// endpoints answer CONSENT_REQUIRED. The account itself stays.
    func withdrawLegalConsent() async throws {
        struct Response: Decodable, Sendable { let ok: Bool? }
        let _: Response = try await session.authenticated { token in
            try await client().delete("api/v1/me/consents", token: token)
        }
    }

    // MARK: Account deletion

    /// The body of `DELETE /api/v1/me`. Apple's one-time code lets the server
    /// revoke the Sign in with Apple token along with the account; nil for an
    /// account without Apple, and then the body is `{}`.
    struct DeletionRequest: Encodable, Equatable, Sendable {
        let apple_authorization_code: String?

        init(appleAuthorizationCode: String?) {
            let code = appleAuthorizationCode?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
            apple_authorization_code = code.isEmpty ? nil : code
        }
    }

    struct DeletionResult: Decodable, Sendable, Equatable {
        let deleted: Bool
        let appleTokenRevoked: Bool

        enum CodingKeys: String, CodingKey {
            case deleted
            case appleTokenRevoked = "apple_token_revoked"
        }

        init(deleted: Bool, appleTokenRevoked: Bool) {
            self.deleted = deleted
            self.appleTokenRevoked = appleTokenRevoked
        }

        init(from decoder: Decoder) throws {
            let container = try decoder.container(keyedBy: CodingKeys.self)
            deleted = try container.decodeIfPresent(Bool.self, forKey: .deleted) ?? false
            appleTokenRevoked = try container.decodeIfPresent(Bool.self, forKey: .appleTokenRevoked) ?? false
        }
    }

    /// `DELETE /api/v1/me`; the server also takes `POST /api/v1/me/delete`
    /// for clients that cannot send a body with DELETE.
    static let deletionPath = "api/v1/me"

    /// Deletes the account and everything the server keeps for it. Every
    /// token of the account stops working; the caller wipes this device.
    func deleteAccount(appleAuthorizationCode: String?) async throws -> DeletionResult {
        let request = DeletionRequest(appleAuthorizationCode: appleAuthorizationCode)
        let result: DeletionResult = try await session.authenticated { token in
            try await client().delete(Self.deletionPath, body: request, token: token)
        }
        // A 200 that does not say so is not a deletion.
        guard result.deleted else { throw APIError.malformedResponse }
        return result
    }

    // MARK: Reports

    /// Sends a report about a generated text. Returns the report's id.
    @discardableResult
    func reportAIOutput(_ report: AIReport) async throws -> String {
        struct Receipt: Decodable, Sendable { let id: String? }
        let receipt: Receipt = try await session.authenticated { token in
            try await client().post("api/v1/ai/reports", body: report, token: token)
        }
        return receipt.id ?? ""
    }

    // MARK: Subscription changes (demo payment adapter)

    struct CheckoutResult: Decodable, Sendable {
        let paymentID: String
        let provider: String
        let status: String
        let demo: Bool

        enum CodingKeys: String, CodingKey {
            case paymentID = "payment_id"
            case provider, status, demo
        }
    }

    func startCheckout(planID: String) async throws -> CheckoutResult {
        struct Request: Encodable { let plan_id: String }
        return try await session.authenticated { token in
            try await client().post("api/v1/payments/checkout",
                                    body: Request(plan_id: planID), token: token)
        }
    }

    /// Confirms a payment. With the demo adapter this is what actually moves
    /// the account onto the chosen plan; with a real acquirer the confirmation
    /// arrives from the provider instead and this call just reads the result.
    @discardableResult
    func confirmCheckout(paymentID: String) async throws -> AccountAPI.Usage {
        struct Response: Decodable, Sendable {
            let subscription: AccountAPI.Subscription
            let usage: AccountAPI.Usage
        }
        let response: Response = try await session.authenticated { token in
            try await client().post("api/v1/payments/\(paymentID)/confirm",
                                    body: APIClient.EmptyBody(), token: token)
        }
        return response.usage
    }
}

extension APIClient {
    /// A body for endpoints that take none but still expect JSON.
    struct EmptyBody: Encodable, Sendable {}
}

/// A report about a generated reply or message (`POST /api/v1/ai/reports`).
///
/// Жауапқа шағым: себеп, қаласа — мәтіннің өзі.
///
/// The text goes only when the user left "send the text" on; nothing else
/// of the conversation is sent - no copied message, no instruction.
struct AIReport: Encodable, Equatable, Sendable {

    enum Mode: String, Encodable, Sendable {
        case reply
        case compose
    }

    /// The reasons the server takes, in the order they are offered.
    enum Reason: String, Encodable, CaseIterable, Sendable {
        case offensive
        case harmful
        case falseInfo = "false_info"
        case wrongLanguage = "wrong_language"
        case other
    }

    /// The server's limits, in characters.
    static let maximumCommentCharacters = 500
    static let maximumTextCharacters = 2000

    let mode: Mode
    let reason: Reason
    let comment: String?
    let text: String?
    let platform: String
    let appVersion: String

    enum CodingKeys: String, CodingKey {
        case mode, reason, comment, text, platform
        case appVersion = "app_version"
    }

    init(mode: Mode, reason: Reason, comment: String? = nil, text: String?,
         descriptor: DeviceDescriptor = .current) {
        self.mode = mode
        self.reason = reason
        self.comment = Self.clamped(comment, to: Self.maximumCommentCharacters)
        self.text = Self.clamped(text, to: Self.maximumTextCharacters)
        self.platform = descriptor.platform
        self.appVersion = descriptor.app_version
    }

    /// Trimmed and cut to the server's limit; nil when nothing is left.
    private static func clamped(_ value: String?, to limit: Int) -> String? {
        let trimmed = value?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        guard !trimmed.isEmpty else { return nil }
        guard trimmed.unicodeScalars.count > limit else { return trimmed }
        return String(String.UnicodeScalarView(trimmed.unicodeScalars.prefix(limit)))
    }
}
