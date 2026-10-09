import Foundation

/// Keeps the account's preferred language - the language its notifications
/// and e-mails are written in - in step with the app.
///
/// Хабарламалар тілі: қосымшада таңдалған тіл аккаунтқа осы жерден жіберіледі.
///
/// Three cases, and only against a server that publishes `preferred_language`
/// (an older one rejects the whole update over an unknown field):
/// - The user picks a language in Settings while signed in: it is marked
///   pending and sent. "System" sends the language it resolves to.
/// - The account has none yet (after sign-in or an account load): the
///   language of this device's app is sent.
/// - Otherwise nothing: a choice made on another device is not overwritten.
///
/// Like the gender in `ProfileSync`, a pending change survives relaunches, is
/// retried on every return to the foreground, goes out one request at a time
/// and is dropped on sign-out.
@MainActor
struct PreferredLanguageSync {

    let defaults: UserDefaults
    /// Whether an account is signed in on this device.
    let isSignedIn: @MainActor () -> Bool
    /// The account's language as the server last sent it: "" when it has
    /// none, nil while unknown or on a server without the field.
    let serverLanguage: @MainActor () -> String?
    /// The app's language right now: en, ru, kk or uz.
    let language: @MainActor () -> String
    /// Sends one value; true once the server confirmed it.
    let send: @MainActor (String) async -> Bool

    init(defaults: UserDefaults = .standard,
         isSignedIn: @escaping @MainActor () -> Bool,
         serverLanguage: @escaping @MainActor () -> String?,
         language: @escaping @MainActor () -> String,
         send: @escaping @MainActor (String) async -> Bool) {
        self.defaults = defaults
        self.isSignedIn = isSignedIn
        self.serverLanguage = serverLanguage
        self.language = language
        self.send = send
    }

    init(account: AccountModel, settings: AppSettings) {
        self.init(
            isSignedIn: { account.isSignedIn },
            serverLanguage: {
                guard account.features?.preferredLanguage == true else { return nil }
                return account.user.map { $0.preferredLanguage ?? "" }
            },
            language: { settings.effectiveLanguage.rawValue },
            send: { code in await account.updatePreferredLanguage(code) }
        )
    }

    private var pending: PendingProfileChange { PendingProfileChange(defaults: defaults, field: .preferredLanguage) }

    var hasPendingChange: Bool { pending.isPending }

    /// The user picked a language in Settings. Signed out there is no account
    /// to tell; the next sign-in sends it only if the account has none.
    func languageChosen() {
        guard isSignedIn() else { return }
        pending.markChanged()
        startPush()
    }

    /// After the account arrives or on return to the foreground: send what
    /// the server has not confirmed, or this device's language to an account
    /// that has none.
    func reconcile() async {
        guard isSignedIn() else { return }
        if !hasPendingChange, serverLanguage()?.isEmpty == true {
            pending.markChanged()
        }
        if hasPendingChange { await pushPendingChange() }
    }

    /// Sign-out: a language this account never got must not be sent to the
    /// next account that signs in on this phone.
    static func discardPendingChange(defaults: UserDefaults = .standard) {
        PendingProfileChange(defaults: defaults, field: .preferredLanguage).discard()
    }

    // MARK: Sending

    /// The last push started for each store of the pending mark; the next
    /// push for the same store waits for it.
    private static var lastPush: [ObjectIdentifier: Task<Void, Never>] = [:]

    func pushPendingChange() async {
        await startPush().value
    }

    /// Queues a push behind the one in progress, at once, so a caller that
    /// does not wait for it still keeps the order.
    @discardableResult
    private func startPush() -> Task<Void, Never> {
        let key = ObjectIdentifier(defaults)
        let previous = Self.lastPush[key]
        let push = Task { @MainActor in
            await previous?.value
            await pushNow()
        }
        Self.lastPush[key] = push
        return push
    }

    /// Waits until every push started so far for `defaults` has finished.
    /// For tests.
    static func waitForPushes(defaults: UserDefaults = .standard) async {
        let key = ObjectIdentifier(defaults)
        while let push = lastPush[key] {
            await push.value
            if lastPush[key] == push { break }
        }
    }

    private func pushNow() async {
        let pending = self.pending
        guard pending.isPending, isSignedIn() else { return }
        let revision = pending.revision
        if await send(language()) { pending.confirm(revision: revision) }
    }
}
