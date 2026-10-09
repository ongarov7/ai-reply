import Foundation

/// The app's own notification state.
///
/// Хабарлама баптаулары: тек қосымшаның өзінде (пернетақтаға қажет емес).
///
/// Standard defaults of the app, not the App Group: the keyboard never reads
/// any of it. The FCM token is not a secret (only the server's own Firebase
/// service account can send to it) and is kept so a launch can register with
/// the token it already has.
///
/// Standard defaults travel in backups, the installation id does not (it is
/// "this device only"). So the token is kept together with the installation
/// it was handed to and read back only for that installation: a backup
/// restored onto another iPhone - a new installation - never registers the
/// old phone's token, which would make the two phones take it from each other.
struct NotificationSettingsStore: @unchecked Sendable {

    let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    private enum Key {
        static let enabledInApp = "notifications.enabledInApp"
        static let cardDismissed = "notifications.permissionCardDismissed"
        /// New names for FCM: a hex APNs token an earlier build kept under
        /// another key is never sent as an FCM token.
        static let pushToken = "push.fcmToken.v1"
        static let lastSync = "installation.lastSync.v1"
    }

    /// Settings ▸ Notifications master switch. On by default.
    var isEnabledInApp: Bool {
        defaults.object(forKey: Key.enabledInApp) as? Bool ?? true
    }

    func setEnabledInApp(_ value: Bool) {
        defaults.set(value, forKey: Key.enabledInApp)
    }

    /// "Not now" on Home's notification card.
    var isPermissionCardDismissed: Bool {
        defaults.bool(forKey: Key.cardDismissed)
    }

    func setPermissionCardDismissed(_ value: Bool) {
        defaults.set(value, forKey: Key.cardDismissed)
    }

    /// The FCM token Firebase last handed to this installation.
    func pushToken(for installationID: String) -> String? {
        guard let data = defaults.data(forKey: Key.pushToken),
              let bound = try? JSONDecoder().decode(BoundToken.self, from: data),
              bound.installationID == installationID else { return nil }
        return bound.token
    }

    func setPushToken(_ token: String?, installationID: String) {
        guard let token, let data = try? JSONEncoder().encode(BoundToken(installationID: installationID, token: token)) else {
            defaults.removeObject(forKey: Key.pushToken)
            return
        }
        defaults.set(data, forKey: Key.pushToken)
    }

    /// A token and the installation it belongs to.
    private struct BoundToken: Codable {
        let installationID: String
        let token: String
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

/// Whether this build asks for push at all.
///
/// AIREPLY_PUSH_NOTIFICATIONS reaches the app through Info.plist, exactly as
/// AIREPLY_SIGN_IN_WITH_APPLE does. Set it to NO for a build signed without
/// the `aps-environment` entitlement (a free Personal Team): the app then
/// never configures Firebase or asks for a token.
enum PushBuildConfiguration {

    static var isEnabledInThisBuild: Bool {
        isEnabled(info: Bundle.main.infoDictionary ?? [:])
    }

    static func isEnabled(info: [String: Any]) -> Bool {
        (info["AIReplyPushNotifications"] as? String)?.uppercased() == "YES"
    }
}
