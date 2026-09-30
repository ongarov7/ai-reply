import Foundation

/// Wire models for the AI Reply backend.
///
/// Тіркелгі мен тариф туралы серверден келетін деректер.
///
/// These are DTOs, not domain types: they mirror the JSON the server sends and
/// nothing else. Everything the app actually reasons about (a profile, a
/// template) already exists in `Shared/Model` and is not duplicated here.
enum AccountAPI {}

extension AccountAPI {

    /// One authenticated session, as returned by every sign-in endpoint and refresh.
    struct Session: Decodable, Sendable {
        let accessToken: String
        let refreshToken: String
        let expiresIn: Int
        let deviceID: String
        let isNewUser: Bool
        let user: User
        let profile: Profile
        let subscription: Subscription
        let usage: Usage
        let legalConsent: LegalConsent?

        enum CodingKeys: String, CodingKey {
            case accessToken = "access_token"
            case refreshToken = "refresh_token"
            case expiresIn = "expires_in"
            case deviceID = "device_id"
            case isNewUser = "is_new_user"
            case user, profile, subscription, usage
            case legalConsent = "legal_consent"
        }
    }

    /// The account itself. No name, no contacts, no device fingerprint.
    struct User: Decodable, Sendable, Equatable {
        let id: String
        let phone: String?
        let email: String?
        let status: String
        let locale: String
        let onboardingCompleted: Bool
        /// Ways this account signs in: "apple", "email", "google", "phone".
        /// Absent on servers from before Google and Apple sign-in.
        let authProviders: [String]?

        enum CodingKeys: String, CodingKey {
            case id, phone, email, status, locale
            case onboardingCompleted = "onboarding_completed"
            case authProviders = "auth_providers"
        }

        /// What the user recognises themselves by.
        var identifier: String { email ?? phone ?? "" }
        var isActive: Bool { status == "active" }
        /// Accounts opened with a phone number have no address to sign in
        /// with once phone sign-in is gone; Settings offers to add one.
        var needsEmail: Bool { (email ?? "").isEmpty }
    }

    /// Server-side copy of the personalisation answers.
    struct Profile: Decodable, Sendable, Equatable {
        let displayName: String
        let role: String
        let description: String
        let preferredTone: String
        let businessOffering: String
        let businessSummary: String
        let businessRules: [String]
        let onboardingCompleted: Bool

        enum CodingKeys: String, CodingKey {
            case displayName = "display_name"
            case role, description
            case preferredTone = "preferred_tone"
            case businessOffering = "business_offering"
            case businessSummary = "business_summary"
            case businessRules = "business_rules"
            case onboardingCompleted = "onboarding_completed"
        }
    }

    /// A plan as the server defines it. Limits live there, never in the app.
    struct Plan: Decodable, Sendable, Equatable, Identifiable {
        let id: String
        let code: String
        let name: [String: String]
        let description: [String: String]
        let price: Int
        let priceText: String
        let currency: String
        let dailyLimit: Int
        let monthlyLimit: Int
        let periodDays: Int
        let isFree: Bool

        enum CodingKeys: String, CodingKey {
            case id, code, name, description, price, currency
            case priceText = "price_text"
            case dailyLimit = "daily_message_limit"
            case monthlyLimit = "monthly_message_limit"
            case periodDays = "period_days"
            case isFree = "is_free"
        }

        /// Localized name with an English fallback, mirroring the server.
        func localizedName(_ language: String) -> String {
            name[language] ?? name["en"] ?? code
        }

        func localizedDescription(_ language: String) -> String {
            description[language] ?? description["en"] ?? ""
        }
    }

    struct PlanList: Decodable, Sendable {
        let plans: [Plan]
    }

    /// Which plan the account is actually on right now.
    struct Subscription: Decodable, Sendable, Equatable {
        let id: String?
        let status: String
        let plan: Plan
        let expiresAt: String?

        enum CodingKeys: String, CodingKey {
            case id, status, plan
            case expiresAt = "expires_at"
        }
    }

    /// The only counter the app trusts: the server's.
    struct Usage: Decodable, Sendable, Equatable {
        let dailyLimit: Int
        let usedToday: Int
        let remainingToday: Int
        let monthlyLimit: Int
        let usedMonth: Int
        let resetsAt: String
        let timezone: String

        enum CodingKeys: String, CodingKey {
            case dailyLimit = "daily_limit"
            case usedToday = "used_today"
            case remainingToday = "remaining_today"
            case monthlyLimit = "monthly_limit"
            case usedMonth = "used_month"
            case resetsAt = "resets_at"
            case timezone
        }

        static let unknown = Usage(dailyLimit: 0, usedToday: 0, remainingToday: 0,
                                   monthlyLimit: 0, usedMonth: 0, resetsAt: "", timezone: "")
    }

    /// Everything /api/v1/me returns in one call.
    struct Account: Decodable, Sendable {
        let user: User
        let profile: Profile
        let subscription: Subscription
        let usage: Usage
        let legalConsent: LegalConsent?

        enum CodingKeys: String, CodingKey {
            case user, profile, subscription, usage
            case legalConsent = "legal_consent"
        }
    }

    /// The e-mail code challenge. The code itself never travels back to the
    /// client, and the answer is the same whether the address has an account.
    struct EmailChallenge: Decodable, Sendable, Equatable {
        let maskedEmail: String
        let expiresIn: Int
        /// Seconds until the server accepts a request for a new code.
        let resendAfter: Int
        let codeLength: Int

        enum CodingKeys: String, CodingKey {
            case maskedEmail = "masked_email"
            case expiresIn = "expires_in"
            case resendAfter = "resend_after"
            case codeLength = "code_length"
        }

        init(maskedEmail: String, expiresIn: Int, resendAfter: Int, codeLength: Int) {
            self.maskedEmail = maskedEmail
            self.expiresIn = expiresIn
            self.resendAfter = resendAfter
            self.codeLength = codeLength
        }

        init(from decoder: Decoder) throws {
            let container = try decoder.container(keyedBy: CodingKeys.self)
            maskedEmail = try container.decodeIfPresent(String.self, forKey: .maskedEmail) ?? ""
            expiresIn = try container.decodeIfPresent(Int.self, forKey: .expiresIn) ?? 300
            resendAfter = try container.decodeIfPresent(Int.self, forKey: .resendAfter) ?? 32
            codeLength = try container.decodeIfPresent(Int.self, forKey: .codeLength) ?? 4
        }
    }

    struct LegalConsent: Decodable, Sendable, Equatable {
        let termsVersion: String
        let privacyVersion: String
        let acceptedAt: String
        let locale: String
        let platform: String
        let appVersion: String?

        enum CodingKeys: String, CodingKey {
            case termsVersion = "terms_version"
            case privacyVersion = "privacy_version"
            case acceptedAt = "accepted_at"
            case locale, platform
            case appVersion = "app_version"
        }
    }

    struct LegalConfig: Decodable, Sendable, Equatable {
        let termsVersion: String
        let privacyVersion: String
        let termsURL: String
        let privacyURL: String

        enum CodingKeys: String, CodingKey {
            case termsVersion = "terms_version"
            case privacyVersion = "privacy_version"
            case termsURL = "terms_url"
            case privacyURL = "privacy_url"
        }

        static let production = LegalConfig(
            termsVersion: "2026-09-19",
            privacyVersion: "2026-09-19",
            termsURL: "https://api.meily.kz/offer",
            privacyURL: "https://api.meily.kz/privacy"
        )
    }

    /// Non-secret server configuration the client is allowed to know.
    struct ServerConfig: Decodable, Sendable {
        let locales: [String]
        let timezone: String
        /// Set by the administrator; the server enforces it on every request.
        let maxSourceCharacters: Int
        let maxInstructionLength: Int
        let paymentMode: String
        let legal: LegalConfig?
        /// What this server version understands. Absent on older servers.
        let features: Features?

        enum CodingKeys: String, CodingKey {
            case locales, timezone, legal, features
            case maxSourceCharacters = "max_source_characters"
            case maxInstructionLength = "max_instruction_length"
            case paymentMode = "payment_mode"
        }
    }

    struct Features: Decodable, Sendable, Equatable {
        /// The reply request's `profile` block accepts `reply_language`.
        let replyPreferences: Bool?
        /// Sign-in methods the server accepts right now. nil on older servers.
        let emailOTP: Bool?
        let googleSignIn: Bool?
        let appleSignIn: Bool?

        enum CodingKeys: String, CodingKey {
            case replyPreferences = "reply_preferences"
            case emailOTP = "email_otp"
            case googleSignIn = "google_sign_in"
            case appleSignIn = "apple_sign_in"
        }
    }

    /// The reply endpoint's response.
    struct ReplyResponse: Decodable, Sendable {
        let reply: String
        let detectedLanguage: String?
        let usage: Usage

        enum CodingKeys: String, CodingKey {
            case reply, usage
            case detectedLanguage = "detected_language"
        }
    }
}
