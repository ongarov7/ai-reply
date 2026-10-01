import Foundation

/// The app's own notification and diagnostics state.
///
/// Хабарлама баптаулары: тек қосымшаның өзінде (пернетақтаға қажет емес).
///
/// Standard defaults of the app, not the App Group: the keyboard never reads
/// any of it. The push token is not a secret (it only works with the app's
/// own APNs key on the server) and is kept so a launch can register with the
/// token it already has instead of waiting for APNs to hand it over again.
///
/// Standard defaults travel in backups, the installation id does not (it is
/// "this device only"). So a token is kept together with the installation it
/// was handed to and read back only for that installation: a backup restored
/// onto another iPhone - a new installation - never registers the old phone's
/// token, which would make the two phones take it from each other.
struct NotificationSettingsStore: @unchecked Sendable {

    let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    private enum Key {
        static let enabledInApp = "notifications.enabledInApp"
        static let sharesDiagnostics = "diagnostics.shareEnabled"
        static let cardDismissed = "notifications.permissionCardDismissed"
        static let deviceToken = "push.deviceToken.v2"
        static let registeredToken = "push.registeredToken.v2"
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

    /// The APNs token iOS last handed to this installation, lowercase hex.
    func deviceToken(for installationID: String) -> String? {
        token(forKey: Key.deviceToken, installationID: installationID)
    }

    func setDeviceToken(_ token: String?, installationID: String) {
        setToken(token, forKey: Key.deviceToken, installationID: installationID)
    }

    /// The token the server last accepted from this installation, to tell a
    /// first registration from a refreshed one.
    func registeredToken(for installationID: String) -> String? {
        token(forKey: Key.registeredToken, installationID: installationID)
    }

    func setRegisteredToken(_ token: String?, installationID: String) {
        setToken(token, forKey: Key.registeredToken, installationID: installationID)
    }

    /// A token and the installation it belongs to.
    private struct BoundToken: Codable {
        let installationID: String
        let token: String
    }

    private func token(forKey key: String, installationID: String) -> String? {
        guard let data = defaults.data(forKey: key),
              let bound = try? JSONDecoder().decode(BoundToken.self, from: data),
              bound.installationID == installationID else { return nil }
        return bound.token
    }

    private func setToken(_ token: String?, forKey key: String, installationID: String) {
        guard let token, let data = try? JSONEncoder().encode(BoundToken(installationID: installationID, token: token)) else {
            defaults.removeObject(forKey: key)
            return
        }
        defaults.set(data, forKey: key)
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
