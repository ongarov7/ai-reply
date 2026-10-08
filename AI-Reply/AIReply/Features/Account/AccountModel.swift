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

    /// How an account deletion ended. `messageKey` is localized by the view.
    enum DeletionOutcome: Equatable {
        case deleted
        case failed(messageKey: String)
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
    /// The server's copy of the profile, as of the last answer that carried it.
    private(set) var profile: AccountAPI.Profile?
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
    /// Something to tell the user once, whichever screen is up - the account
    /// was deleted, say. A localization key.
    private(set) var noticeKey: String?
    /// Whether `legalConfig` came from the server in this launch, rather than
    /// from this build's own defaults.
    @ObservationIgnored private var hasServerLegalConfig = false
    /// A foreground reload of the legal versions is already out.
    @ObservationIgnored private var isReloadingLegalConfig = false

    @ObservationIgnored private let service: AccountService

    /// Runs the moment the server's profile arrives - sign-in, `/me`, adding
    /// an e-mail - before the screens switch. The app takes a gender chosen
    /// on another device here, so a returning user's onboarding never asks
    /// the question again.
    @ObservationIgnored var didReceiveProfile: (@MainActor (AccountAPI.Profile?) -> Void)?

    /// This phone's installation, named on the logout request so the server
    /// stops sending the account's notifications to it at once. Nil when
    /// there is none to name.
    @ObservationIgnored var installationIDForSignOut: (@MainActor () -> String?)?

    /// The account was deleted: the app drops what it keeps for it beyond
    /// the session (the synced profile, the events waiting to be sent).
    @ObservationIgnored var didDeleteAccount: (@MainActor () -> Void)?

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

    /// Apple is offered when this build is entitled to it and the server can
    /// verify Apple tokens.
    var offersApple: Bool { AppleSignIn.isEnabledInThisBuild && (features?.appleSignIn ?? true) }

    /// Google needs this build's client id and a server that verifies Google
    /// tokens. DEBUG builds show it regardless, so the screen can be reviewed
    /// before the client id exists; tapping it then explains it is unavailable.
    var offersGoogle: Bool {
        #if DEBUG
        let configured = true
        let isRelease = false
        #else
        let configured = GoogleSignInProvider.isConfigured
        let isRelease = true
        #endif
        return Self.offersGoogle(configured: configured, serverAllows: features?.googleSignIn ?? true,
                                 offersApple: offersApple, isRelease: isRelease)
    }

    /// App Review guideline 4.8: an app that signs in with Google must offer
    /// Sign in with Apple beside it, so a Release build without Apple hides
    /// Google too. DEBUG builds, which leave Apple out for Personal Team
    /// signing, keep Google for review.
    static func offersGoogle(configured: Bool, serverAllows: Bool, offersApple: Bool, isRelease: Bool) -> Bool {
        guard configured, serverAllows else { return false }
        return offersApple || !isRelease
    }

    /// Whether this build can take a payment at all. Release builds cannot:
    /// there is no StoreKit purchase flow yet, and a plan sold outside it
    /// would not pass App Review. DEBUG builds may, against a development
    /// server with the demo checkout.
    static var purchasesAvailableInThisBuild: Bool {
        #if DEBUG
        return true
        #else
        return false
        #endif
    }

    /// A Choose button (and a price) for this plan: the build can buy, and
    /// the server says the plan can be bought right now.
    func canPurchase(_ plan: AccountAPI.Plan) -> Bool {
        Self.purchasesAvailableInThisBuild && plan.purchasable && !plan.isFree
    }

    /// The account signs in with Apple, so deleting it revokes Apple's token.
    var signsInWithApple: Bool { user?.authProviders?.contains("apple") == true }

    // MARK: Loading

    /// Limits, features and current legal versions, needed before the first screen.
    func loadServerConfig() async {
        if let config = try? await service.serverConfig() {
            legalConfig = config.legal ?? .production
            hasServerLegalConfig = config.legal != nil
            features = config.features
            // The limits the administrator set, for the app's own composer
            // and - through the App Group - for the keyboard.
            AILimits.apply(config)
            ProductEvents.storeServerSupport(config.features)
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
        // An acceptance confirmed while this request was out is newer than
        // its answer, which must not undo it.
        let consentBefore = LegalConsentStore.load()
        do {
            let account = try await service.account()
            apply(user: account.user, subscription: account.subscription, usage: account.usage)
            receive(account.profile)
            applyLegalConsent(account.legalConsent, recordBefore: consentBefore)
            phase = .signedIn
            // The account is there: no earlier deletion went through.
            hasUnansweredDeletion = false
        } catch APIError.unauthorized {
            // A deletion whose answer was lost (the app closed since) did delete it.
            if hasUnansweredDeletion {
                await forgetDeletedAccount()
            } else {
                await signOutLocally()
            }
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
        // Before the phase flips: onboarding decides its steps from what the
        // device knows at that moment.
        receive(session.profile)
        applyLegalConsent(session.legalConsent, recordBefore: LegalConsentStore.load())
        phase = .signedIn
        pendingEmail = ""
        hasUnansweredDeletion = false
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
        receive(account.profile)
    }

    // MARK: Legal and session

    func acceptLegal(locale: String) async {
        // The versions the server and the keyboard enforce now, not the ones
        // read at launch: the screen may have come up from that older check.
        // Offline, the current ones stay.
        await loadServerConfig()
        LegalConsentStore.accept(legalConfig, locale: locale)
        hasAcceptedLegal = true
        await syncPendingLegalConsent()
    }

    /// Re-reads the consent the App Group holds: the keyboard drops it when
    /// the server answers CONSENT_REQUIRED. Called on every return to the
    /// foreground. The documents may have changed while the app was away,
    /// so their current versions are loaded too, as the keyboard has them.
    func revalidateLegalConsent() {
        guard isBootstrapComplete else { return }
        hasAcceptedLegal = LegalConsentStore.hasAccepted(legalConfig)
        guard !isReloadingLegalConfig else { return }
        isReloadingLegalConfig = true
        Task {
            await serverRequiresConsent()
            isReloadingLegalConfig = false
        }
    }

    /// An AI request in the app came back CONSENT_REQUIRED, or was refused
    /// before it was sent (the transport has already dropped an out-of-date
    /// record): the consent screen comes back, unless an acceptance is only
    /// waiting to be sent.
    func serverRequiresConsent() async {
        // The documents may have changed since launch: their current
        // versions first, for the screen and for the keyboard.
        await loadServerConfig()
        await syncPendingLegalConsent()
    }

    /// Settings ▸ Privacy ▸ Withdraw AI consent. The server hears it first:
    /// the AI endpoints refuse from then on, and the consent screen returns
    /// until the user accepts again. The account stays signed in. Returns a
    /// message key when the withdrawal did not reach the server.
    func withdrawLegalConsent() async -> String? {
        isBusy = true
        defer { isBusy = false }
        do {
            try await service.withdrawLegalConsent()
        } catch {
            return Self.message(for: error)
        }
        LegalConsentStore.clear()
        hasAcceptedLegal = false
        return nil
    }

    func signOut() async {
        isBusy = true
        await service.signOut(installationID: installationIDForSignOut?())
        await signOutLocally()
        isBusy = false
    }

    /// Settings ▸ Delete account (or the consent screen), after the user
    /// confirmed it. An account that signs in with Apple is confirmed with
    /// Apple once more, so the server can revoke Apple's token. Once the
    /// server has deleted the account, this device forgets it too and
    /// returns to sign-in.
    func deleteAccount() async -> DeletionOutcome {
        isBusy = true
        defer { isBusy = false }

        var appleCode: String?
        if signsInWithApple && AppleSignIn.isEnabledInThisBuild {
            do {
                appleCode = try await AppleSignIn.confirmForDeletion()
            } catch {
                guard Self.deletionContinues(afterAppleFailure: error) else {
                    return .failed(messageKey: "account.delete.appleRequired")
                }
                // The server deletes without a code and only skips the revocation.
                ReplyLog.event("apple confirmation for deletion failed: \(error)")
            }
        }

        do {
            _ = try await service.deleteAccount(appleAuthorizationCode: appleCode)
        } catch {
            ReplyLog.event("account deletion failed: \(error)")
            switch Self.deletionFailure(error, afterUnansweredAttempt: hasUnansweredDeletion) {
            case .alreadyDeleted:
                // The earlier attempt deleted it; only its answer was lost.
                break
            case .unanswered:
                hasUnansweredDeletion = true
                return .failed(messageKey: "account.delete.failed")
            case .sessionEnded:
                errorKey = "account.error.sessionExpired"
                await signOutLocally()
                return .failed(messageKey: "account.error.sessionExpired")
            case .refused:
                return .failed(messageKey: "account.delete.failed")
            }
        }

        await forgetDeletedAccount()
        return .deleted
    }

    /// How a failed `DELETE /me` is read.
    enum DeletionFailure: Equatable {
        /// 401 after an attempt that got no answer: that attempt deleted the
        /// account, and every token of it stopped working.
        case alreadyDeleted
        /// The request may have reached the server, but no answer came back.
        case unanswered
        /// 401 with no such attempt before: the session ended, the account
        /// may well still be there.
        case sessionEnded
        /// The server answered, and nothing was deleted.
        case refused
    }

    nonisolated static func deletionFailure(_ error: Error, afterUnansweredAttempt: Bool) -> DeletionFailure {
        switch error as? APIError {
        case .unauthorized:
            return afterUnansweredAttempt ? .alreadyDeleted : .sessionEnded
        // `offline` too: a connection lost after the request left is reported so.
        case .offline, .timedOut, .cancelled, .server, .providerTimeout:
            return .unanswered
        default:
            return .refused
        }
    }

    /// The Apple step before a deletion gave no code. The user cancelling it
    /// stops the deletion; Apple failing on this device does not.
    nonisolated static func deletionContinues(afterAppleFailure error: Error) -> Bool {
        (error as? AppleSignIn.ConfirmationFailure) != .cancelled
    }

    /// A deletion went out and no answer came back, so the account may be
    /// gone already. Kept across launches; dropped with the session.
    private var hasUnansweredDeletion: Bool {
        get { UserDefaults.standard.bool(forKey: Self.unansweredDeletionKey) }
        set { UserDefaults.standard.set(newValue, forKey: Self.unansweredDeletionKey) }
    }

    private static let unansweredDeletionKey = "account.deletionUnanswered"

    /// The server no longer has the account: this device forgets it too.
    private func forgetDeletedAccount() async {
        LocalAccountData.wipe()
        didDeleteAccount?()
        await signOutLocally()
        hasAcceptedLegal = LegalConsentStore.hasAccepted(legalConfig)
        noticeKey = "account.delete.done"
    }

    func dismissNotice() {
        noticeKey = nil
    }

    private func signOutLocally() async {
        // A gender or language change this account never received stays
        // with it: it must not be sent to whoever signs in next on this phone.
        ProfileSync.discardPendingChange()
        PreferredLanguageSync.discardPendingChange()
        hasUnansweredDeletion = false
        AccountCredentials.clear()
        AccountUsageCache.clear()
        AppleSignIn.forget()
        GoogleSignInProvider.signOut()
        user = nil
        profile = nil
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
        // The language of the account's notifications, where the server has it.
        if features?.preferredLanguage == true { update.preferred_language = locale }

        do {
            _ = try await service.updateProfile(update)
            await refresh()
            return true
        } catch {
            errorKey = Self.message(for: error)
            return false
        }
    }

    /// Sends the sender fields - the grammatical gender, the onboarding this
    /// device finished - to a server that publishes `sender_profile`.
    ///
    /// Returns false when nothing reached the server: signed out, an older
    /// server, or a failure. Nothing is shown for it; the caller keeps the
    /// change pending and it is retried on the next foreground.
    func updateSenderProfile(gender: GrammaticalGender? = nil, onboardingVersion: Int? = nil) async -> Bool {
        guard AccountCredentials.isSignedIn, AILimits.serverSupportsSenderProfile else { return false }
        var update = AccountService.ProfileUpdate()
        update.grammatical_gender = gender?.rawValue
        update.onboarding_version = onboardingVersion
        do {
            profile = try await service.updateProfile(update)
            return true
        } catch {
            ReplyLog.event("profile sync failed: \(error)")
            return false
        }
    }

    /// Sends the language of the account's notifications to a server that
    /// publishes `preferred_language`. False when nothing reached the server;
    /// the caller keeps it pending (see `PreferredLanguageSync`).
    func updatePreferredLanguage(_ code: String) async -> Bool {
        guard AccountCredentials.isSignedIn, features?.preferredLanguage == true else { return false }
        var update = AccountService.ProfileUpdate()
        update.preferred_language = code
        do {
            profile = try await service.updateProfile(update)
            user = user?.withPreferredLanguage(code)
            return true
        } catch {
            ReplyLog.event("preferred language sync failed: \(error)")
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

    /// The server's copy of the profile arrived.
    private func receive(_ profile: AccountAPI.Profile?) {
        self.profile = profile
        didReceiveProfile?(profile)
    }

    private func apply(user: AccountAPI.User, subscription: AccountAPI.Subscription, usage: AccountAPI.Usage) {
        self.user = user
        self.subscription = subscription
        self.usage = usage
        AccountUsageCache.store(usage)
        AccountUsageCache.storePlanCode(subscription.plan.code)
        AccountCredentials.setDisplayIdentifier(user.identifier)
    }

    /// The server's record decides. Without a consent there for the
    /// documents in force, one this device had confirmed is out of date -
    /// withdrawn on another device, or another account's - and the consent
    /// screen returns. An acceptance still waiting to be sent stays, and so
    /// does one that changed since `recordBefore` was read (the request was
    /// already out when it was confirmed).
    private func applyLegalConsent(_ consent: AccountAPI.LegalConsent?, recordBefore: StoredLegalConsent?) {
        guard let consent,
              consent.termsVersion == legalConfig.termsVersion,
              consent.privacyVersion == legalConfig.privacyVersion else {
            guard hasServerLegalConfig, let recordBefore, !recordBefore.isPendingSync,
                  LegalConsentStore.load() == recordBefore else { return }
            LegalConsentStore.clear()
            hasAcceptedLegal = LegalConsentStore.hasAccepted(legalConfig)
            return
        }
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

/// What this device keeps for an account besides the session, removed when
/// the account is deleted.
///
/// Тіркелгі жойылғанда құрылғыдағы оның деректері де өшеді.
///
/// The keyboard's settings (layouts, haptics, smart correction) and the app's
/// language belong to the device and stay. The keychain session, the Apple
/// and Google sign-in state and the in-memory data are dropped by
/// `AccountModel` itself.
@MainActor
enum LocalAccountData {

    static func wipe(appGroup: UserDefaults = AppGroup.defaults, app: UserDefaults = .standard) {
        LegalConsentStore.clear(defaults: appGroup)
        AccountUsageCache.clear(defaults: appGroup)
        // Words the keyboard learned from what this user typed.
        DefaultsLearnedWordsStore(defaults: appGroup).removeAll()
        // Changes this account never received must not reach the next one.
        ProfileSync.discardPendingChange(defaults: app)
        PreferredLanguageSync.discardPendingChange(defaults: app)
    }
}
