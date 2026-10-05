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
@MainActor
struct ProfileSync {

    enum Source: String {
        case onboarding
        case settings
    }

    let configuration: ReplyConfigurationModel
    let account: AccountModel
    var defaults: UserDefaults = .standard

    private static let pendingKey = "profile.pendingSync"

    var hasPendingChange: Bool { defaults.bool(forKey: Self.pendingKey) }

    /// The user picked a value on this device.
    func choose(_ gender: GrammaticalGender, source: Source) {
        configuration.updateProfile { $0.grammaticalGender = gender }
        defaults.set(true, forKey: Self.pendingKey)
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
        } else if let adopted = Self.adopted(local: configuration.profile.grammaticalGender,
                                             server: account.profile?.gender) {
            configuration.updateProfile { $0.grammaticalGender = adopted }
        }
    }

    /// The value to take from the server, or nil to keep the local one.
    static func adopted(local: GrammaticalGender?, server: GrammaticalGender?) -> GrammaticalGender? {
        guard let server, server != .unspecified, server != local else { return nil }
        return server
    }

    private func pushPendingChange() async {
        guard let gender = configuration.profile.grammaticalGender else { return }
        if await account.updateSenderProfile(gender: gender) {
            defaults.removeObject(forKey: Self.pendingKey)
        }
    }
}
