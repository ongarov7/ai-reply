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
        /// Apple, Google and e-mail buttons.
        case signedOut
        case enteringEmail
        case awaitingCode(EmailCodeChallenge)
        case signedIn
    }

    /// A code on its way to an address.
    struct EmailCodeChallenge: Equatable {
        /// Normalized, as typed: it is the user's own address, shown in full.
        let email: String
        /// When the server will accept a request for a new code.
        var resendAvailableAt: Date
    }

    /// How a sign-in attempt ended, for the screen that started it.
    enum SignInOutcome: Equatable {
        case signedIn(isNewUser: Bool)
        /// `clearCode`: the code itself was wrong, expired or used up. After
        /// a network failure the typed code stays so the user can just retry.
        case failed(clearCode: Bool)
        case cancelled
    }

    private(set) var phase: Phase
    private(set) var user: AccountAPI.User?
    private(set) var subscription: AccountAPI.Subscription?
    private(set) var usage: AccountAPI.Usage = .unknown
    private(set) var plans: [AccountAPI.Plan] = []
    private(set) var legalConfig: AccountAPI.LegalConfig = .production
    private(set) var features: AccountAPI.Features?
    private(set) var hasAcceptedLegal: Bool
    private(set) var isBootstrapComplete = false
    private(set) var isBusy = false
    /// The address typed last, so going back from the code screen keeps it.
    private(set) var pendingEmail = ""
    /// A key the view localizes. Never a raw server string.
    private(set) var errorKey: String?

    @ObservationIgnored private let service: AccountService

    init(service: AccountService = AccountService()) {
        self.service = service
        self.phase = AccountCredentials.isSignedIn ? .signedIn : .signedOut
        self.hasAcceptedLegal = LegalConsentStore.hasAccepted(.production)
    }

    var isSignedIn: Bool { phase == .signedIn }

    /// What the user recognises the account by: the address, the phone, or
    /// the provider for an account that has neither.
    var displayIdentifier: String {
        let identifier = user?.identifier ?? AccountCredentials.displayIdentifier ?? ""
        if !identifier.isEmpty { return identifier }
        let providers = user?.authProviders ?? []
        if providers.contains("apple") { return "Apple ID" }
        if providers.contains("google") { return "Google" }
        return ""
    }

    var remainingToday: Int { usage.remainingToday }

    /// Apple is offered unless the server says it cannot verify Apple tokens.
    var offersApple: Bool { features?.appleSignIn ?? true }

    /// Google needs this build's client id and a server that verifies Google
    /// tokens. DEBUG builds show it regardless, so the screen can be reviewed
    /// before the client id exists; tapping it then explains it is unavailable.
    var offersGoogle: Bool {
        #if DEBUG
        let configured = true
        #else
        let configured = GoogleSignInProvider.isConfigured
        #endif
        return configured && (features?.googleSignIn ?? true)
    }

    // MARK: Loading

    /// Limits, features and current legal versions, needed before the first screen.
    func loadServerConfig() async {
        if let config = try? await service.serverConfig() {
            legalConfig = config.legal ?? .production
            features = config.features
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
            // Someone who stopped using their Apple ID with this app has signed
            // out of it, as far as Apple and the user are concerned.
            if await AppleSignIn.isAuthorizationRevoked() {
                await signOut()
            } else {
                await refresh()
                await syncPendingLegalConsent()
            }
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

    // MARK: Sign-in: e-mail

    func startEmailSignIn() {
        errorKey = nil
        phase = .enteringEmail
    }

    /// Back from the e-mail screen to the three sign-in buttons.
    func cancelEmailSignIn() {
        errorKey = nil
        phase = AccountCredentials.isSignedIn ? .signedIn : .signedOut
    }

    /// Back from the code screen to the address, which stays filled in.
    func editEmail() {
        errorKey = nil
        phase = .enteringEmail
    }

    /// Asks the server to e-mail a code, then shows the code screen.
    func requestEmailCode(email rawEmail: String, locale: String) async {
        let email = EmailAddress.normalized(rawEmail)
        pendingEmail = email
        isBusy = true
        errorKey = nil
        defer { isBusy = false }

        do {
            let challenge = try await service.requestEmailCode(email: email, locale: locale)
            phase = .awaitingCode(EmailCodeChallenge(
                email: email, resendAvailableAt: Date().addingTimeInterval(TimeInterval(challenge.resendAfter))))
        } catch APIError.resendCooldown(let retryAfter) {
            // A code went to this address moments ago and is still valid:
            // go to the code screen and count down the server's wait.
            phase = .awaitingCode(EmailCodeChallenge(
                email: email, resendAvailableAt: Date().addingTimeInterval(TimeInterval(retryAfter ?? 32))))
        } catch {
            errorKey = Self.message(for: error)
        }
    }

    /// A new code for the address on the code screen. The previous code stops
    /// working the moment the server issues this one.
    func resendEmailCode(locale: String) async {
        guard case let .awaitingCode(challenge) = phase else { return }
        isBusy = true
        errorKey = nil
        defer { isBusy = false }

        do {
            let fresh = try await service.requestEmailCode(email: challenge.email, locale: locale)
            phase = .awaitingCode(EmailCodeChallenge(
                email: challenge.email, resendAvailableAt: Date().addingTimeInterval(TimeInterval(fresh.resendAfter))))
        } catch APIError.resendCooldown(let retryAfter) {
            // The server's clock wins; the countdown follows it.
            phase = .awaitingCode(EmailCodeChallenge(
                email: challenge.email,
                resendAvailableAt: Date().addingTimeInterval(TimeInterval(retryAfter ?? 32))))
        } catch {
            errorKey = Self.message(for: error)
        }
    }

    func verifyEmailCode(_ code: String) async -> SignInOutcome {
        guard case let .awaitingCode(challenge) = phase else { return .cancelled }
        isBusy = true
        errorKey = nil
        defer { isBusy = false }

        do {
            let session = try await service.verifyEmailCode(email: challenge.email, code: code)
            return completeSignIn(session)
        } catch {
            errorKey = Self.message(for: error)
            return .failed(clearCode: Self.codeIsSpent(error))
        }
    }

    // MARK: Sign-in: Apple and Google

    func signInWithApple(_ credential: AppleSignIn.Credential, rawNonce: String) async -> SignInOutcome {
        isBusy = true
        errorKey = nil
        defer { isBusy = false }

        do {
            let session = try await service.signInWithApple(
                identityToken: credential.identityToken, rawNonce: rawNonce, fullName: credential.fullName)
            AppleSignIn.remember(userID: credential.userID)
            return completeSignIn(session)
        } catch {
            errorKey = Self.message(for: error)
            return .failed(clearCode: false)
        }
    }

    func signInWithGoogle() async -> SignInOutcome {
        isBusy = true
        errorKey = nil
        defer { isBusy = false }

        let token: GoogleSignInProvider.Token
        do {
            token = try await GoogleSignInProvider.signIn()
        } catch GoogleSignInProvider.Failure.cancelled {
            return .cancelled
        } catch GoogleSignInProvider.Failure.notConfigured {
            errorKey = "account.error.providerUnavailable"
            return .failed(clearCode: false)
        } catch {
            errorKey = "account.error.providerFailed"
            return .failed(clearCode: false)
        }

        do {
            let session = try await service.signInWithGoogle(idToken: token.idToken, nonce: token.nonce)
            return completeSignIn(session)
        } catch {
            errorKey = Self.message(for: error)
            return .failed(clearCode: false)
        }
    }

    /// Apple's own sheet failed before our server was involved.
    func reportProviderFailure() {
        errorKey = "account.error.providerFailed"
    }

    /// The session is stored and the screens switch now. Device registration
    /// and the consent sync follow on their own, so a new account goes straight
    /// to the short profile step instead of flashing the home screen first.
    private func completeSignIn(_ session: AccountAPI.Session) -> SignInOutcome {
        apply(user: session.user, subscription: session.subscription, usage: session.usage)
        applyLegalConsent(session.legalConsent)
        phase = .signedIn
        pendingEmail = ""
        Task {
            // Best effort: a failed device registration must not block sign-in.
            try? await service.registerDevice()
            await syncPendingLegalConsent()
        }
        return .signedIn(isNewUser: session.isNewUser)
    }

    // MARK: Adding an e-mail to an account

    /// Sends a code to an address the signed-in account wants to add.
    func requestLinkEmailCode(email rawEmail: String, locale: String) async throws -> EmailCodeChallenge {
        let email = EmailAddress.normalized(rawEmail)
        do {
            let challenge = try await service.requestLinkEmailCode(email: email, locale: locale)
            return EmailCodeChallenge(
                email: email, resendAvailableAt: Date().addingTimeInterval(TimeInterval(challenge.resendAfter)))
        } catch APIError.resendCooldown(let retryAfter) {
            return EmailCodeChallenge(
                email: email, resendAvailableAt: Date().addingTimeInterval(TimeInterval(retryAfter ?? 32)))
        }
    }

    /// Verifies the code; from then on the account signs in with the address.
    func verifyLinkEmailCode(email: String, code: String) async throws {
        let account = try await service.verifyLinkEmailCode(email: email, code: code)
        apply(user: account.user, subscription: account.subscription, usage: account.usage)
    }

    // MARK: Legal and session

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
        AppleSignIn.forget()
        GoogleSignInProvider.signOut()
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

    /// Whether a failed verification used up the code the user typed, so the
    /// field should empty; a network failure keeps it for a plain retry.
    static func codeIsSpent(_ error: Error) -> Bool {
        switch error as? APIError {
        case .invalidOTP, .otpExpired, .otpAlreadyUsed, .otpAttemptsExceeded: return true
        default: return false
        }
    }

    /// Maps a failure onto a localization key. The server's English message is
    /// never shown to a user.
    static func message(for error: Error) -> String {
        guard let apiError = error as? APIError else { return "account.error.generic" }
        switch apiError {
        case .offline:                  return "error.offline"
        case .timedOut:                 return "error.timedOut"
        case .invalidOTP:               return "account.error.invalidCode"
        case .otpExpired:               return "account.error.codeExpired"
        case .otpAlreadyUsed:           return "account.error.codeUsed"
        case .otpAttemptsExceeded:      return "account.error.codeAttempts"
        case .resendCooldown:           return "account.error.resendCooldown"
        case .rateLimited:              return "account.error.tooManyAttempts"
        case .invalidEmail:             return "account.error.invalidEmail"
        case .emailDeliveryFailed:      return "account.error.emailDelivery"
        case .emailInUse:               return "account.error.emailInUse"
        case .invalidIDToken:           return "account.error.providerFailed"
        case .authProviderUnavailable:  return "account.error.providerUnavailable"
        case .unauthorized:             return "account.error.sessionExpired"
        case .accountDisabled:          return "account.error.disabled"
        case .dailyLimitReached:        return "account.error.limitReached"
        case .paymentRequired, .subscriptionExpired: return "account.error.paymentRequired"
        default:                        return "account.error.generic"
        }
    }
}
