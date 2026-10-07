import Foundation
import Security

/// Keychain storage for the account session, shared with the keyboard.
///
/// Токендер тек Keychain-де сақталады (App Group defaults-та емес).
///
/// The tokens live in the same access group as the rest of the app's keychain
/// items, which is what lets the keyboard extension generate a reply without
/// asking the user to sign in twice. App Group defaults hold only the
/// non-secret device id and the access-token expiry.
enum AccountCredentials {

    private static let service = "kz.ai-reply.account"
    private static let accessGroup = KeychainGroup.identifier

    private enum Account {
        static let access = "account.access.token"
        static let refresh = "account.refresh.token"
    }

    private enum DefaultsKey {
        static let deviceID = "account.deviceIdentifier"
        static let accessExpiry = "account.accessTokenExpiry"
        static let identifier = "account.identifier"
    }

    // MARK: Keychain

    private static func query(_ account: String) -> [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecAttrAccessGroup as String: accessGroup
        ]
    }

    private static func read(_ account: String) -> String? {
        var request = query(account)
        request[kSecReturnData as String] = true
        request[kSecMatchLimit as String] = kSecMatchLimitOne

        var item: CFTypeRef?
        guard SecItemCopyMatching(request as CFDictionary, &item) == errSecSuccess,
              let data = item as? Data,
              let value = String(data: data, encoding: .utf8),
              !value.isEmpty else {
            return nil
        }
        return value
    }

    @discardableResult
    private static func write(_ value: String?, to account: String) -> Bool {
        guard let value, !value.isEmpty else {
            let status = SecItemDelete(query(account) as CFDictionary)
            return status == errSecSuccess || status == errSecItemNotFound
        }
        guard let data = value.data(using: .utf8) else { return false }

        let attributes: [String: Any] = [
            kSecValueData as String: data,
            // After first unlock so the keyboard can read it mid-conversation;
            // this-device-only so a backup never carries a session elsewhere.
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        ]

        let updateStatus = SecItemUpdate(query(account) as CFDictionary, attributes as CFDictionary)
        if updateStatus == errSecSuccess { return true }
        guard updateStatus == errSecItemNotFound else { return false }

        var insert = query(account)
        insert.merge(attributes) { current, _ in current }
        return SecItemAdd(insert as CFDictionary, nil) == errSecSuccess
    }

    // MARK: Tokens

    static var accessToken: String? { read(Account.access) }
    static var refreshToken: String? { read(Account.refresh) }

    /// True when a session exists at all - checked before every network call so
    /// a signed-out keyboard shows a prompt instead of a failed request.
    static var isSignedIn: Bool { refreshToken != nil }

    /// Stores a freshly issued pair. Called from one place only.
    static func store(accessToken: String, refreshToken: String, expiresIn: Int) {
        write(accessToken, to: Account.access)
        write(refreshToken, to: Account.refresh)
        let expiry = Date().addingTimeInterval(TimeInterval(max(expiresIn, 60)))
        AppGroup.defaults.set(expiry.timeIntervalSince1970, forKey: DefaultsKey.accessExpiry)
    }

    /// Wipes the session. Used by sign-out and by an unrecoverable 401.
    static func clear() {
        write(nil, to: Account.access)
        write(nil, to: Account.refresh)
        AppGroup.defaults.removeObject(forKey: DefaultsKey.accessExpiry)
        AppGroup.defaults.removeObject(forKey: DefaultsKey.identifier)
    }

    /// Whether the access token is still comfortably valid.
    ///
    /// A minute of headroom, because a token that expires while the request is
    /// in flight costs the user a visible retry.
    static var isAccessTokenFresh: Bool {
        guard accessToken != nil else { return false }
        let raw = AppGroup.defaults.double(forKey: DefaultsKey.accessExpiry)
        guard raw > 0 else { return false }
        return Date(timeIntervalSince1970: raw).timeIntervalSinceNow > 60
    }

    // MARK: Non-secret state

    /// Stable per-install device id. Not the IDFV, not the IDFA, not hardware:
    /// a random value that disappears with the app.
    static var deviceID: String {
        if let existing = AppGroup.defaults.string(forKey: DefaultsKey.deviceID), !existing.isEmpty {
            return existing
        }
        let created = UUID().uuidString
        AppGroup.defaults.set(created, forKey: DefaultsKey.deviceID)
        return created
    }

    static func setDeviceID(_ value: String) {
        guard !value.isEmpty else { return }
        AppGroup.defaults.set(value, forKey: DefaultsKey.deviceID)
    }

    /// The masked phone or e-mail, shown in Settings so the user knows which
    /// account they are on. Masked server-side; stored as received.
    static var displayIdentifier: String? {
        AppGroup.defaults.string(forKey: DefaultsKey.identifier)
    }

    static func setDisplayIdentifier(_ value: String?) {
        if let value, !value.isEmpty {
            AppGroup.defaults.set(value, forKey: DefaultsKey.identifier)
        } else {
            AppGroup.defaults.removeObject(forKey: DefaultsKey.identifier)
        }
    }
}
