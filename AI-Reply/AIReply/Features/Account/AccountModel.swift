import Foundation
import Observation

/// Account state for the app's UI.
///
/// Тіркелгі күйі: кірген/кірмеген, тариф, күндік квота.
///
/// One instance lives in the environment next to `AppSettings`. It owns no
/// networking of its own - `AccountService` does that - and no tokens -
/// `AccountSession` does that. What it owns is what the screens render.
@MainActor
@Observable
final class AccountModel {

    /// Where the user is in the sign-in flow.
    enum Phase: Equatable {
        case signedOut
        case awaitingCode(identifier: String, masked: String)
        case signedIn
    }

    private(set) var phase: Phase
    private(set) var user: AccountAPI.User?
    private(set) var subscription: AccountAPI.Subscription?
    private(set) var usage: AccountAPI.Usage = .unknown
    private(set) var plans: [AccountAPI.Plan] = []
    private(set) var countries: [AccountAPI.Country] = []
    private(set) var legalConfig: AccountAPI.LegalConfig = .production
    private(set) var hasAcceptedLegal: Bool
    private(set) var isBootstrapComplete = false
    private(set) var isBusy = false
    /// A key the view localizes. Never a raw server string.
    private(set) var errorKey: String?

    @ObservationIgnored private let service: AccountService

    init(service: AccountService = AccountService()) {
        self.service = service
        self.phase = AccountCredentials.isSignedIn ? .signedIn : .signedOut
        self.hasAcceptedLegal = LegalConsentStore.hasAccepted(.production)
    }

    var isSignedIn: Bool { phase == .signedIn }

    /// What the user recognises the account by, masked where the server masked it.
    var displayIdentifier: String {
        user?.identifier ?? AccountCredentials.displayIdentifier ?? ""
    }

    var remainingToday: Int { usage.remainingToday }

    // MARK: Loading

    /// Countries and current legal versions, needed before the first screen.
    func loadServerConfig() async {
        if let config = try? await service.serverConfig() {
            countries = config.countries
            legalConfig = config.legal ?? .production
            // The limits the administrator set, for the app's own composer
            // and - through the App Group - for the keyboard.
            AILimits.apply(config)
        }
        hasAcceptedLegal = LegalConsentStore.hasAccepted(legalConfig)
    }

    func bootstrap() async {
        guard !isBootstrapComplete else { return }
        await loadServerConfig()
        if AccountCredentials.isSignedIn {
            await refresh()
            await syncPendingLegalConsent()
        }
        hasAcceptedLegal = LegalConsentStore.hasAccepted(legalConfig)
        isBootstrapComplete = true
    }

    func loadPlans() async {
        if let list = try? await service.plans() {
            plans = list
        }
    }

    /// Profile, plan and quota in one call. Safe to call on every appearance.
    func refresh() async {
        guard AccountCredentials.isSignedIn else {
            phase = .signedOut
            return
        }
        do {
            let account = try await service.account()
            apply(user: account.user, subscription: account.subscription, usage: account.usage)
            applyLegalConsent(account.legalConsent)
            phase = .signedIn
        } catch APIError.unauthorized {
            await signOutLocally()
        } catch APIError.accountDisabled {
            errorKey = "account.error.disabled"
            await signOutLocally()
        } catch {
            // A refresh failing offline is not a reason to sign anyone out.
            errorKey = nil
        }
    }

    func refreshUsage() async {
        guard AccountCredentials.isSignedIn else { return }
        if let fresh = try? await service.usage() {
            usage = fresh
            AccountUsageCache.store(fresh)
        }
    }

    // MARK: Sign-in

    func requestCode(identifier: String, locale: String) async {
        isBusy = true
        errorKey = nil
        defer { isBusy = false }

        do {
            let challenge = try await service.requestCode(identifier: identifier, locale: locale)
            phase = .awaitingCode(identifier: identifier,
                                  masked: challenge.maskedIdentifier)
        } catch {
            errorKey = Self.message(for: error)
        }
    }

    /// Returns true when the account is brand new, so the caller can show the
    /// short profile step instead of dropping the user straight onto Home.
    @discardableResult
    func verify(code: String) async -> Bool {
        guard case let .awaitingCode(identifier, _) = phase else { return false }
        isBusy = true
        errorKey = nil
        defer { isBusy = false }

        do {
            let session = try await service.verifyCode(identifier: identifier, code: code)
            apply(user: session.user, subscription: session.subscription, usage: session.usage)
            applyLegalConsent(session.legalConsent)
            phase = .signedIn
            // Best effort: a failed device registration must not block sign-in.
            try? await service.registerDevice()
            await syncPendingLegalConsent()
            return session.isNewUser
        } catch {
            errorKey = Self.message(for: error)
            return false
        }
    }

    func resendCode(locale: String) async {
        guard case let .awaitingCode(identifier, _) = phase else { return }
        await requestCode(identifier: identifier, locale: locale)
    }

    func cancelCodeEntry() {
        phase = AccountCredentials.isSignedIn ? .signedIn : .signedOut
        errorKey = nil
    }

    func acceptLegal(locale: String) async {
        LegalConsentStore.accept(legalConfig, locale: locale)
        hasAcceptedLegal = true
        await syncPendingLegalConsent()
    }

    func signOut() async {
        isBusy = true
        await service.signOut()
        await signOutLocally()
        isBusy = false
    }

    private func signOutLocally() async {
        AccountCredentials.clear()
        AccountUsageCache.clear()
        user = nil
        subscription = nil
        usage = .unknown
        phase = .signedOut
    }

    // MARK: Profile

    /// Sends the minimal registration answers and marks onboarding complete.
    func completeRegistration(displayName: String, role: String, description: String,
                              tone: String, locale: String) async -> Bool {
        isBusy = true
        errorKey = nil
        defer { isBusy = false }

        var update = AccountService.ProfileUpdate()
        update.display_name = displayName
        update.role = role
        update.description = description
        update.preferred_tone = tone
        update.locale = locale
        update.timezone = TimeZone.current.identifier
        update.onboarding_completed = true

        do {
            _ = try await service.updateProfile(update)
            await refresh()
            return true
        } catch {
            errorKey = Self.message(for: error)
            return false
        }
    }

    // MARK: Plans

    /// Demo checkout: create a payment and confirm it in one step. With a real
    /// acquirer this becomes create → redirect → webhook, and only this method
    /// changes.
    func choosePlan(_ plan: AccountAPI.Plan) async -> Bool {
        isBusy = true
        errorKey = nil
        defer { isBusy = false }

        do {
            if plan.isFree {
                errorKey = nil
                return false
            }
            let checkout = try await service.startCheckout(planID: plan.id)
            let usage = try await service.confirmCheckout(paymentID: checkout.paymentID)
            self.usage = usage
            AccountUsageCache.store(usage)
            await refresh()
            return true
        } catch {
            errorKey = Self.message(for: error)
            return false
        }
    }

    // MARK: Helpers

    private func apply(user: AccountAPI.User, subscription: AccountAPI.Subscription, usage: AccountAPI.Usage) {
        self.user = user
        self.subscription = subscription
        self.usage = usage
        AccountUsageCache.store(usage)
        AccountUsageCache.storePlanCode(subscription.plan.code)
        AccountCredentials.setDisplayIdentifier(user.identifier)
    }

    private func applyLegalConsent(_ consent: AccountAPI.LegalConsent?) {
        guard let consent,
              consent.termsVersion == legalConfig.termsVersion,
              consent.privacyVersion == legalConfig.privacyVersion else { return }
        LegalConsentStore.restore(consent)
        hasAcceptedLegal = true
    }

    private func syncPendingLegalConsent() async {
        guard AccountCredentials.isSignedIn,
              let record = LegalConsentStore.load(),
              record.isPendingSync,
              record.termsVersion == legalConfig.termsVersion,
              record.privacyVersion == legalConfig.privacyVersion else { return }
        do {
            let saved = try await service.recordLegalConsent(record)
            LegalConsentStore.restore(saved)
            hasAcceptedLegal = true
        } catch {
            // The local acceptance remains pending and is retried on refresh.
        }
    }

    /// Maps a failure onto a localization key. The server's English message is
    /// never shown to a user.
    static func message(for error: Error) -> String {
        guard let apiError = error as? APIError else { return "account.error.generic" }
        switch apiError {
        case .offline:              return "error.offline"
        case .timedOut:             return "error.timedOut"
        case .invalidOTP:           return "account.error.invalidCode"
        case .otpExpired:           return "account.error.codeExpired"
        case .rateLimited:          return "account.error.tooManyAttempts"
        case .unauthorized:         return "account.error.sessionExpired"
        case .accountDisabled:      return "account.error.disabled"
        case .invalidRequest:       return "account.error.invalidNumber"
        case .dailyLimitReached:    return "account.error.limitReached"
        case .paymentRequired, .subscriptionExpired: return "account.error.paymentRequired"
        default:                    return "account.error.generic"
        }
    }
}
