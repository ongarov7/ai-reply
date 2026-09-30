import Foundation

/// The app's own notification and diagnostics state.
///
/// Хабарлама баптаулары: тек қосымшаның өзінде (пернетақтаға қажет емес).
///
/// Standard defaults of the app, not the App Group: the keyboard never reads
/// any of it. The push token is not a secret (it only works with the app's
/// own APNs key on the server) and is kept so a launch can register with the
/// token it already has instead of waiting for APNs to hand it over again.
struct NotificationSettingsStore: @unchecked Sendable {

    let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    private enum Key {
        static let enabledInApp = "notifications.enabledInApp"
        static let sharesDiagnostics = "diagnostics.shareEnabled"
        static let cardDismissed = "notifications.permissionCardDismissed"
        static let deviceToken = "push.deviceToken"
        static let registeredToken = "push.registeredToken"
        static let lastSync = "installation.lastSync"
    }

    /// Settings ▸ Notifications master switch. On by default.
    var isEnabledInApp: Bool {
        defaults.object(forKey: Key.enabledInApp) as? Bool ?? true
    }

    func setEnabledInApp(_ value: Bool) {
        defaults.set(value, forKey: Key.enabledInApp)
    }

    /// Settings ▸ Diagnostics. On by default.
    var sharesDiagnostics: Bool {
        defaults.object(forKey: Key.sharesDiagnostics) as? Bool ?? true
    }

    func setSharesDiagnostics(_ value: Bool) {
        defaults.set(value, forKey: Key.sharesDiagnostics)
    }

    /// "Not now" on Home's notification card.
    var isPermissionCardDismissed: Bool {
        defaults.bool(forKey: Key.cardDismissed)
    }

    func setPermissionCardDismissed(_ value: Bool) {
        defaults.set(value, forKey: Key.cardDismissed)
    }

    /// The APNs token iOS last handed over, lowercase hex.
    var deviceToken: String? {
        defaults.string(forKey: Key.deviceToken)
    }

    func setDeviceToken(_ value: String?) {
        defaults.set(value, forKey: Key.deviceToken)
    }

    /// The token the server last accepted, to tell a first registration from
    /// a refreshed one.
    var registeredToken: String? {
        defaults.string(forKey: Key.registeredToken)
    }

    func setRegisteredToken(_ value: String?) {
        defaults.set(value, forKey: Key.registeredToken)
    }
}

extension NotificationSettingsStore: InstallationSyncStoring {
    func lastSync() -> InstallationSyncRecord? {
        guard let data = defaults.data(forKey: Key.lastSync) else { return nil }
        return try? JSONDecoder().decode(InstallationSyncRecord.self, from: data)
    }

    func setLastSync(_ record: InstallationSyncRecord?) {
        guard let record, let data = try? JSONEncoder().encode(record) else {
            defaults.removeObject(forKey: Key.lastSync)
            return
        }
        defaults.set(data, forKey: Key.lastSync)
    }
}

/// Whether this build can receive pushes at all.
///
/// Release builds carry the `aps-environment` entitlement; Debug builds leave
/// it out so a free Personal Team can still install them. The build setting
/// AIREPLY_PUSH_NOTIFICATIONS reaches the app through Info.plist, exactly as
/// AIREPLY_SIGN_IN_WITH_APPLE does, and the app never asks APNs for a token in
/// a build that could not receive one.
enum PushBuildConfiguration {

    static var isEnabledInThisBuild: Bool {
        isEnabled(info: Bundle.main.infoDictionary ?? [:])
    }

    static func isEnabled(info: [String: Any]) -> Bool {
        (info["AIReplyPushNotifications"] as? String)?.uppercased() == "YES"
    }
}
