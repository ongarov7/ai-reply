import Foundation

// Wire models of the backend (internal/transport/api/dto.go). Decoding is
// lenient: an absent optional key never fails a whole screen.

public struct UserDTO: Decodable, Equatable, Sendable {
    public let id: String
    public let email: String?
    public let locale: String?
    public let preferredLanguage: String?

    enum CodingKeys: String, CodingKey {
        case id, email, locale
        case preferredLanguage = "preferred_language"
    }
}

public struct PlanDTO: Decodable, Equatable, Sendable {
    public let code: String
    public let name: [String: String]
    public let entitlementKind: String
    public let dailyMessageLimit: Int
    public let isFree: Bool

    enum CodingKeys: String, CodingKey {
        case code, name
        case entitlementKind = "entitlement_kind"
        case dailyMessageLimit = "daily_message_limit"
        case isFree = "is_free"
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        code = try c.decode(String.self, forKey: .code)
        name = try c.decodeIfPresent([String: String].self, forKey: .name) ?? [:]
        entitlementKind = try c.decodeIfPresent(String.self, forKey: .entitlementKind) ?? "daily"
        dailyMessageLimit = try c.decodeIfPresent(Int.self, forKey: .dailyMessageLimit) ?? 0
        isFree = try c.decodeIfPresent(Bool.self, forKey: .isFree) ?? false
    }

    /// Plan name in the given language, falling back to Russian, then the code.
    public func localizedName(_ language: String) -> String {
        for key in [language, "ru", "en"] {
            if let value = name[key], !value.isEmpty { return value }
        }
        return code
    }
}

public struct SubscriptionDTO: Decodable, Equatable, Sendable {
    public let status: String
    public let plan: PlanDTO
    public let expiresAt: Date?
    public let autoRenews: Bool?

    enum CodingKeys: String, CodingKey {
        case status, plan
        case expiresAt = "expires_at"
        case autoRenews = "auto_renews"
    }
}

public struct UsageDTO: Decodable, Equatable, Sendable {
    public struct Welcome: Decodable, Equatable, Sendable {
        public let total: Int
        public let remaining: Int
    }

    public struct Pass: Decodable, Equatable, Sendable {
        public let expiresAt: Date?
        public let remaining: Int

        enum CodingKeys: String, CodingKey {
            case expiresAt = "expires_at"
            case remaining
        }
    }

    public let dailyLimit: Int
    public let unlimited: Bool
    public let usedToday: Int
    public let remainingToday: Int
    public let credits: Int
    public let welcome: Welcome?
    public let pass: Pass?
    public let remainingTotal: Int
    public let resetsAt: Date?

    enum CodingKeys: String, CodingKey {
        case dailyLimit = "daily_limit"
        case unlimited
        case usedToday = "used_today"
        case remainingToday = "remaining_today"
        case credits
        case purchasedCredits = "purchased_credits"
        case adminCredits = "admin_credits"
        case welcome, pass
        case remainingTotal = "remaining_total"
        case resetsAt = "resets_at"
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        dailyLimit = try c.decodeIfPresent(Int.self, forKey: .dailyLimit) ?? 0
        unlimited = try c.decodeIfPresent(Bool.self, forKey: .unlimited) ?? false
        usedToday = try c.decodeIfPresent(Int.self, forKey: .usedToday) ?? 0
        remainingToday = try c.decodeIfPresent(Int.self, forKey: .remainingToday) ?? 0
        let purchased = try c.decodeIfPresent(Int.self, forKey: .purchasedCredits) ?? 0
        let admin = try c.decodeIfPresent(Int.self, forKey: .adminCredits) ?? 0
        credits = try c.decodeIfPresent(Int.self, forKey: .credits) ?? (purchased + admin)
        welcome = try c.decodeIfPresent(Welcome.self, forKey: .welcome)
        pass = try c.decodeIfPresent(Pass.self, forKey: .pass)
        resetsAt = try c.decodeIfPresent(Date.self, forKey: .resetsAt)
        remainingTotal = try c.decodeIfPresent(Int.self, forKey: .remainingTotal)
            ?? (remainingToday + (pass?.remaining ?? 0) + (welcome?.remaining ?? 0) + credits)
    }

    public init(dailyLimit: Int, unlimited: Bool, usedToday: Int, remainingToday: Int, credits: Int,
                welcome: Welcome?, pass: Pass?, remainingTotal: Int, resetsAt: Date?) {
        self.dailyLimit = dailyLimit
        self.unlimited = unlimited
        self.usedToday = usedToday
        self.remainingToday = remainingToday
        self.credits = credits
        self.welcome = welcome
        self.pass = pass
        self.remainingTotal = remainingTotal
        self.resetsAt = resetsAt
    }
}

public struct LegalConsentDTO: Decodable, Equatable, Sendable {
    public let termsVersion: String
    public let privacyVersion: String

    enum CodingKeys: String, CodingKey {
        case termsVersion = "terms_version"
        case privacyVersion = "privacy_version"
    }
}

/// GET /api/v1/me
public struct MeResponse: Decodable, Equatable, Sendable {
    public let user: UserDTO
    public let subscription: SubscriptionDTO?
    public let usage: UsageDTO?
    /// Absent when the account has not accepted the current documents.
    public let legalConsent: LegalConsentDTO?

    enum CodingKeys: String, CodingKey {
        case user, subscription, usage
        case legalConsent = "legal_consent"
    }

    public init(user: UserDTO, subscription: SubscriptionDTO?, usage: UsageDTO?, legalConsent: LegalConsentDTO?) {
        self.user = user
        self.subscription = subscription
        self.usage = usage
        self.legalConsent = legalConsent
    }
}

/// The session answer of email OTP verify and of refresh.
public struct SessionPayload: Decodable, Equatable, Sendable {
    public let accessToken: String
    public let refreshToken: String
    public let expiresIn: Int
    public let refreshExpiresAt: Date?
    public let deviceID: String
    public let isNewUser: Bool
    public let user: UserDTO
    public let subscription: SubscriptionDTO?
    public let usage: UsageDTO?
    public let legalConsent: LegalConsentDTO?

    enum CodingKeys: String, CodingKey {
        case accessToken = "access_token"
        case refreshToken = "refresh_token"
        case expiresIn = "expires_in"
        case refreshExpiresAt = "refresh_expires_at"
        case deviceID = "device_id"
        case isNewUser = "is_new_user"
        case user, subscription, usage
        case legalConsent = "legal_consent"
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        accessToken = try c.decode(String.self, forKey: .accessToken)
        refreshToken = try c.decode(String.self, forKey: .refreshToken)
        expiresIn = try c.decodeIfPresent(Int.self, forKey: .expiresIn) ?? 900
        refreshExpiresAt = try c.decodeIfPresent(Date.self, forKey: .refreshExpiresAt)
        deviceID = try c.decodeIfPresent(String.self, forKey: .deviceID) ?? ""
        isNewUser = try c.decodeIfPresent(Bool.self, forKey: .isNewUser) ?? false
        user = try c.decode(UserDTO.self, forKey: .user)
        subscription = try c.decodeIfPresent(SubscriptionDTO.self, forKey: .subscription)
        usage = try c.decodeIfPresent(UsageDTO.self, forKey: .usage)
        legalConsent = try c.decodeIfPresent(LegalConsentDTO.self, forKey: .legalConsent)
    }

    public var me: MeResponse {
        MeResponse(user: user, subscription: subscription, usage: usage, legalConsent: legalConsent)
    }

    func tokens(issuedAt: Date, fallbackEmail: String? = nil) -> SessionTokens {
        SessionTokens(accessToken: accessToken, refreshToken: refreshToken,
                      accessExpiresAt: issuedAt.addingTimeInterval(TimeInterval(max(expiresIn, 60))),
                      refreshExpiresAt: refreshExpiresAt, deviceID: deviceID, userID: user.id,
                      email: user.email ?? fallbackEmail)
    }
}

/// POST /api/v1/auth/email/otp/request
public struct OTPChallenge: Decodable, Equatable, Sendable {
    public let maskedEmail: String
    public let expiresIn: Int
    public let resendAfter: Int
    public let codeLength: Int

    enum CodingKeys: String, CodingKey {
        case maskedEmail = "masked_email"
        case expiresIn = "expires_in"
        case resendAfter = "resend_after"
        case codeLength = "code_length"
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        maskedEmail = try c.decodeIfPresent(String.self, forKey: .maskedEmail) ?? ""
        expiresIn = try c.decodeIfPresent(Int.self, forKey: .expiresIn) ?? 300
        resendAfter = try c.decodeIfPresent(Int.self, forKey: .resendAfter) ?? 30
        codeLength = try c.decodeIfPresent(Int.self, forKey: .codeLength) ?? 4
    }

    public init(maskedEmail: String, expiresIn: Int, resendAfter: Int, codeLength: Int) {
        self.maskedEmail = maskedEmail
        self.expiresIn = expiresIn
        self.resendAfter = resendAfter
        self.codeLength = codeLength
    }
}

/// GET /api/v1/config (only what the desktop app uses).
public struct ServerConfig: Decodable, Equatable, Sendable {
    public struct Legal: Decodable, Equatable, Sendable {
        public let termsVersion: String
        public let privacyVersion: String
        public let termsURL: URL?
        public let privacyURL: URL?
        public let supportURL: URL?
        public let accountDeletionURL: URL?
        public let aiProvider: String

        enum CodingKeys: String, CodingKey {
            case termsVersion = "terms_version"
            case privacyVersion = "privacy_version"
            case termsURL = "terms_url"
            case privacyURL = "privacy_url"
            case supportURL = "support_url"
            case accountDeletionURL = "account_deletion_url"
            case aiProvider = "ai_provider"
        }

        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            termsVersion = try c.decodeIfPresent(String.self, forKey: .termsVersion) ?? ""
            privacyVersion = try c.decodeIfPresent(String.self, forKey: .privacyVersion) ?? ""
            termsURL = Self.url(try c.decodeIfPresent(String.self, forKey: .termsURL))
            privacyURL = Self.url(try c.decodeIfPresent(String.self, forKey: .privacyURL))
            supportURL = Self.url(try c.decodeIfPresent(String.self, forKey: .supportURL))
            accountDeletionURL = Self.url(try c.decodeIfPresent(String.self, forKey: .accountDeletionURL))
            aiProvider = try c.decodeIfPresent(String.self, forKey: .aiProvider) ?? "OpenAI"
        }

        public init(termsVersion: String, privacyVersion: String, termsURL: URL?, privacyURL: URL?,
                    supportURL: URL?, accountDeletionURL: URL?, aiProvider: String) {
            self.termsVersion = termsVersion
            self.privacyVersion = privacyVersion
            self.termsURL = termsURL
            self.privacyURL = privacyURL
            self.supportURL = supportURL
            self.accountDeletionURL = accountDeletionURL
            self.aiProvider = aiProvider
        }

        private static func url(_ raw: String?) -> URL? {
            guard let raw, let url = URL(string: raw), url.scheme == "https" || url.scheme == "http" else { return nil }
            return url
        }
    }

    public let maxSourceCharacters: Int
    public let maxInstructionLength: Int
    public let features: [String: Bool]
    public let legal: Legal?

    enum CodingKeys: String, CodingKey {
        case maxSourceCharacters = "max_source_characters"
        case maxInstructionLength = "max_instruction_length"
        case features, legal
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        maxSourceCharacters = try c.decodeIfPresent(Int.self, forKey: .maxSourceCharacters) ?? 400
        maxInstructionLength = try c.decodeIfPresent(Int.self, forKey: .maxInstructionLength) ?? 400
        features = try c.decodeIfPresent([String: Bool].self, forKey: .features) ?? [:]
        legal = try c.decodeIfPresent(Legal.self, forKey: .legal)
    }

    public init(maxSourceCharacters: Int, maxInstructionLength: Int, features: [String: Bool], legal: Legal?) {
        self.maxSourceCharacters = maxSourceCharacters
        self.maxInstructionLength = maxInstructionLength
        self.features = features
        self.legal = legal
    }

    /// A missing flag counts as off.
    public func feature(_ name: String) -> Bool { features[name] ?? false }

    public var voiceTranscription: Bool { feature("voice_transcription") }

    public static let fallback = ServerConfig(maxSourceCharacters: 400, maxInstructionLength: 400, features: [:], legal: nil)
}
