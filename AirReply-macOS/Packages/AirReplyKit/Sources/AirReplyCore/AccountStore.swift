import Foundation
import Observation

/// What the interface shows about the balance.
public struct BalanceSummary: Equatable, Sendable {
    public let planCode: String
    public let planName: String
    public let isUnlimited: Bool
    /// Everything that can still be spent now: today's plan replies, pass, welcome and packs.
    public let remainingTotal: Int
    public let remainingToday: Int
    public let dailyLimit: Int
    public let welcomeRemaining: Int?
    public let welcomeTotal: Int?
    public let credits: Int
    public let resetsAt: Date?

    public var isExhausted: Bool { !isUnlimited && remainingTotal <= 0 }
}

/// Account state from the server: plan, balance, consent and server settings.
/// Refreshed on sign-in, when the panel opens (throttled) and after each reply.
@MainActor
@Observable
public final class AccountStore {
    public private(set) var me: MeResponse?
    public private(set) var config: ServerConfig = .fallback
    public private(set) var isConsentSaving = false

    private let client: APIClient
    private let configuration: AppConfiguration
    private let now: @MainActor () -> Date
    private var meLoadedAt: Date?
    private var configLoadedAt: Date?

    public init(client: APIClient, configuration: AppConfiguration, now: @escaping @MainActor () -> Date = Date.init) {
        self.client = client
        self.configuration = configuration
        self.now = now
    }

    /// nil until /me has been read once after sign-in.
    public var hasConsent: Bool? {
        guard let me else { return nil }
        return me.legalConsent != nil
    }

    public var balance: BalanceSummary? {
        guard let me, let usage = me.usage else { return nil }
        let plan = me.subscription?.plan
        return BalanceSummary(
            planCode: plan?.code ?? "free",
            planName: plan?.localizedName(APIClient.preferredLanguage()) ?? "",
            isUnlimited: usage.unlimited,
            remainingTotal: usage.remainingTotal,
            remainingToday: usage.remainingToday,
            dailyLimit: usage.dailyLimit,
            welcomeRemaining: usage.welcome?.remaining,
            welcomeTotal: usage.welcome?.total,
            credits: usage.credits,
            resetsAt: usage.resetsAt
        )
    }

    public func apply(_ me: MeResponse) {
        self.me = me
        meLoadedAt = now()
    }

    /// AI answers carry the fresh usage; keep it without another /me call.
    public func apply(usage: UsageDTO) {
        guard let me else { return }
        self.me = MeResponse(user: me.user, subscription: me.subscription, usage: usage, legalConsent: me.legalConsent)
    }

    public func clear() {
        me = nil
        meLoadedAt = nil
    }

    public func refreshConfig(maxAge: TimeInterval = 300) async {
        if let configLoadedAt, now().timeIntervalSince(configLoadedAt) < maxAge { return }
        if let fresh = try? await client.send(APIRequest("GET", "/api/v1/config", authenticated: false), as: ServerConfig.self) {
            config = fresh
            configLoadedAt = now()
        }
    }

    public func refreshMe(maxAge: TimeInterval = 0) async throws(APIError) {
        if maxAge > 0, let meLoadedAt, now().timeIntervalSince(meLoadedAt) < maxAge { return }
        apply(try await client.send(APIRequest("GET", "/api/v1/me"), as: MeResponse.self))
    }

    /// A one-time link (60 s) that opens the website's payment page signed in as
    /// this account, so a purchase cannot land on another website account.
    /// Nil when signed out or unavailable: the caller opens the plain page.
    public func paymentHandoffURL(locale: String) async -> URL? {
        struct Body: Encodable {
            let expected_user_id: String
            let locale: String
        }
        struct Answer: Decodable { let url: URL }
        guard let userID = me?.user.id, !userID.isEmpty else { return nil }
        guard let answer = try? await client.send(.json("POST", "/api/v1/web/payment-handoff",
                                                        Body(expected_user_id: userID, locale: locale)), as: Answer.self)
        else { return nil }
        // Only our own website, over https: never a link the answer could point elsewhere.
        guard answer.url.scheme == "https", answer.url.host == configuration.backendBaseURL.host else { return nil }
        return answer.url
    }

    /// Accepts the current terms and privacy policy for AI processing.
    public func acceptConsent() async throws(APIError) {
        struct Body: Encodable {
            let terms_version: String
            let privacy_version: String
            let locale: String
            let platform: String
            let app_version: String
        }
        await refreshConfig(maxAge: 60)
        guard let legal = config.legal, !legal.termsVersion.isEmpty, !legal.privacyVersion.isEmpty else {
            throw APIError.invalidResponse(status: 0)
        }
        isConsentSaving = true
        defer { isConsentSaving = false }
        try await client.send(.json("POST", "/api/v1/me/consents",
                                    Body(terms_version: legal.termsVersion, privacy_version: legal.privacyVersion,
                                         locale: APIClient.preferredLanguage(), platform: configuration.platform,
                                         app_version: configuration.appVersion)))
        try await refreshMe()
    }
}
