import Foundation

/// Keeps the sender's grammatical gender the same on this device and on the
/// server.
///
/// Жынысы осы құрылғыда сақталады, серверге көшірмесі кетеді; басқа құрылғыда
/// таңдалғаны мұнда тек нақты таңдау болса ғана қабылданады.
///
/// The device is the source of truth for what the user picked here: a change
/// is written locally first and marked pending until the server confirms it,
/// so a failed request is retried on the next foreground instead of lost.
/// Without a pending change, a choice made on another device wins - but only an
/// actual choice: `unspecified` is the server's default for every account and
/// would otherwise erase what the user picked.
///
/// Changes go out one at a time, and each choice gets a new revision: the
/// server's answer to an older choice never clears a newer one that is still
/// on its way, and so never lets the older value come back from `/me`.
@MainActor
struct ProfileSync {

    enum Source: String {
        case onboarding
        case settings
    }

    let configuration: ReplyConfigurationModel
    let account: AccountModel
    var defaults: UserDefaults = .standard
    /// Sends one value; true once the server confirmed it. Tests stand in for
    /// the server here; nil means the account's real PATCH.
    var send: (@MainActor (GrammaticalGender) async -> Bool)?

    private var pending: PendingProfileChange { PendingProfileChange(defaults: defaults) }

    var hasPendingChange: Bool { pending.isPending }

    /// The user picked a value on this device.
    func choose(_ gender: GrammaticalGender, source: Source) {
        configuration.updateProfile { $0.grammaticalGender = gender }
        pending.markChanged()
        ProductEvents.track(.genderSelected, [
            "source": .code(source.rawValue),
            "skipped": .bool(gender == .unspecified)
        ])
        Task { await pushPendingChange() }
    }

    /// After a `/me` refresh or on return to the foreground: retry what the
    /// server has not confirmed, or else take a choice made on another device.
    func reconcile() async {
        if hasPendingChange {
            await pushPendingChange()
        } else {
            adoptServerChoice(account.profile?.gender)
        }
    }

    /// Takes a male or female answer the account already has, when nothing
    /// picked here is still waiting to be sent. Synchronous, so it can run
    /// the moment a profile arrives - before onboarding decides whether to
    /// ask the question at all.
    func adoptServerChoice(_ server: GrammaticalGender?) {
        guard !hasPendingChange,
              let adopted = Self.adopted(local: configuration.profile.grammaticalGender, server: server) else { return }
        configuration.updateProfile { $0.grammaticalGender = adopted }
    }

    /// The value to take from the server, or nil to keep the local one.
    static func adopted(local: GrammaticalGender?, server: GrammaticalGender?) -> GrammaticalGender? {
        guard let server, server != .unspecified, server != local else { return nil }
        return server
    }

    /// Whether onboarding already has its answer to "how should replies speak
    /// for you": one given on this device, or a choice the account brought
    /// from another device. Either way the question is not asked again.
    static func knowsGender(local: GrammaticalGender?, server: GrammaticalGender?) -> Bool {
        local != nil || adopted(local: nil, server: server) != nil
    }

    /// What Skip on the gender step records: neutral wording - unless a male
    /// or female answer is already known, which Skip never erases.
    static func skippedAnswer(current: GrammaticalGender?) -> GrammaticalGender? {
        switch current {
        case .male, .female: return nil
        case .unspecified, nil: return .unspecified
        }
    }

    /// Sign-out: a change this account never got must not be sent to the
    /// next account that signs in on this phone.
    static func discardPendingChange(defaults: UserDefaults = .standard) {
        PendingProfileChange(defaults: defaults).discard()
    }

    // MARK: Sending

    /// The last push started for each store of the pending mark (the app has
    /// one); the next push for the same store waits for it.
    private static var lastPush: [ObjectIdentifier: Task<Void, Never>] = [:]

    /// Sends the current value if a change is pending. One request at a time.
    func pushPendingChange() async {
        let key = ObjectIdentifier(defaults)
        let previous = Self.lastPush[key]
        let push = Task { @MainActor in
            await previous?.value
            await pushNow()
        }
        Self.lastPush[key] = push
        await push.value
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
        guard pending.isPending, let gender = configuration.profile.grammaticalGender else { return }
        let revision = pending.revision
        let confirmed: Bool
        if let send {
            confirmed = await send(gender)
        } else {
            confirmed = await account.updateSenderProfile(gender: gender)
        }
        if confirmed { pending.confirm(revision: revision) }
    }
}

/// The marker for a profile change the server has not confirmed yet: a
/// pending flag, plus a revision that every new choice raises. One pair of
/// keys per field. Device-local, in the app's own defaults.
struct PendingProfileChange {

    enum Field {
        /// `profile.pendingSync`, the grammatical gender.
        case gender
        /// The account's preferred language (`PreferredLanguageSync`).
        case preferredLanguage
    }

    var defaults: UserDefaults = .standard
    var field: Field = .gender

    private var pendingKey: String {
        switch field {
        case .gender:            return "profile.pendingSync"
        case .preferredLanguage: return "profile.language.pendingSync"
        }
    }

    private var revisionKey: String {
        switch field {
        case .gender:            return "profile.pendingRevision"
        case .preferredLanguage: return "profile.language.pendingRevision"
        }
    }

    var isPending: Bool { defaults.bool(forKey: pendingKey) }
    var revision: Int { defaults.integer(forKey: revisionKey) }

    /// A new choice was made: pending, under a new revision.
    @discardableResult
    func markChanged() -> Int {
        let next = revision &+ 1
        defaults.set(next, forKey: revisionKey)
        defaults.set(true, forKey: pendingKey)
        return next
    }

    /// The server confirmed the value sent under `revision`. Only the latest
    /// choice's confirmation clears the marker.
    func confirm(revision sent: Int) {
        guard sent == revision else { return }
        defaults.removeObject(forKey: pendingKey)
    }

    func discard() {
        defaults.removeObject(forKey: pendingKey)
    }
}
