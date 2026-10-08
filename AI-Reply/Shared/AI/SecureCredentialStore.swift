import Foundation
import Security

/// Removes credentials and overrides written by builds that supported direct
/// provider access. Account access and refresh tokens use a different service.
enum LegacyCredentialMigration {
    private static let migrationKey = "migration.backendOnly.v1"
    private static let service = "kz.yerek.replykeyboard.openai"
    private static let legacyAccounts = ["openai.api.key", "backend.client.token"]
    private static let legacyDefaults = [
        "ai.transportMode",
        "ai.model",
        "ai.backendBaseURL",
        "ai.installIdentifier"
    ]

    static func runOnce(defaults: UserDefaults = AppGroup.defaults) {
        guard !defaults.bool(forKey: migrationKey) else { return }

        for account in legacyAccounts {
            let query: [String: Any] = [
                kSecClass as String: kSecClassGenericPassword,
                kSecAttrService as String: service,
                kSecAttrAccount as String: account,
                kSecAttrAccessGroup as String: KeychainGroup.identifier
            ]
            SecItemDelete(query as CFDictionary)
        }
        legacyDefaults.forEach(defaults.removeObject(forKey:))
        defaults.set(true, forKey: migrationKey)
    }
}

struct StoredLegalConsent: Codable, Equatable, Sendable {
    let termsVersion: String
    let privacyVersion: String
    let acceptedAt: String
    let locale: String
    let platform: String
    let appVersion: String
    var isPendingSync: Bool
}

/// Non-secret acceptance state shared by the host app and its keyboard.
///
/// Келісім күйі App Group-та: пернетақта AI сұранысынан бұрын тексереді.
enum LegalConsentStore {
    private static let key = "account.legalConsent.v1"
    /// The versions the server publishes now, from the last config read by
    /// the app or the keyboard.
    private static let currentTermsKey = "account.legalVersions.terms"
    private static let currentPrivacyKey = "account.legalVersions.privacy"

    static func load(defaults: UserDefaults = AppGroup.defaults) -> StoredLegalConsent? {
        guard let data = defaults.data(forKey: key) else { return nil }
        return try? JSONDecoder().decode(StoredLegalConsent.self, from: data)
    }

    static func hasAccepted(_ config: AccountAPI.LegalConfig,
                            defaults: UserDefaults = AppGroup.defaults) -> Bool {
        guard let record = load(defaults: defaults) else { return false }
        return record.termsVersion == config.termsVersion
            && record.privacyVersion == config.privacyVersion
    }

    @discardableResult
    static func accept(_ config: AccountAPI.LegalConfig, locale: String,
                       defaults: UserDefaults = AppGroup.defaults) -> StoredLegalConsent {
        let record = StoredLegalConsent(
            termsVersion: config.termsVersion,
            privacyVersion: config.privacyVersion,
            acceptedAt: ISO8601DateFormatter().string(from: Date()),
            locale: locale,
            platform: "ios",
            appVersion: Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "",
            isPendingSync: true
        )
        save(record, defaults: defaults)
        return record
    }

    static func restore(_ consent: AccountAPI.LegalConsent,
                        defaults: UserDefaults = AppGroup.defaults) {
        save(StoredLegalConsent(
            termsVersion: consent.termsVersion,
            privacyVersion: consent.privacyVersion,
            acceptedAt: consent.acceptedAt,
            locale: consent.locale,
            platform: consent.platform,
            appVersion: consent.appVersion ?? "",
            isPendingSync: false
        ), defaults: defaults)
    }

    // MARK: Current versions

    static func storeCurrentVersions(_ config: AccountAPI.LegalConfig,
                                     defaults: UserDefaults = AppGroup.defaults) {
        defaults.set(config.termsVersion, forKey: currentTermsKey)
        defaults.set(config.privacyVersion, forKey: currentPrivacyKey)
    }

    /// The published versions; this build's own until a config was read.
    static func currentVersions(defaults: UserDefaults = AppGroup.defaults) -> (terms: String, privacy: String) {
        let terms = defaults.string(forKey: currentTermsKey) ?? ""
        let privacy = defaults.string(forKey: currentPrivacyKey) ?? ""
        guard !terms.isEmpty, !privacy.isEmpty else {
            return (AccountAPI.LegalConfig.production.termsVersion, AccountAPI.LegalConfig.production.privacyVersion)
        }
        return (terms, privacy)
    }

    /// Whether the documents in force now were accepted on this device.
    /// Checked before every AI request, in the app and in the keyboard: the
    /// server refuses anyway, this only spares the request.
    static func hasAcceptedCurrentVersions(defaults: UserDefaults = AppGroup.defaults) -> Bool {
        guard let record = load(defaults: defaults) else { return false }
        let current = currentVersions(defaults: defaults)
        return record.termsVersion == current.terms && record.privacyVersion == current.privacy
    }

    // MARK: Removal

    /// Withdrawal and account deletion.
    static func clear(defaults: UserDefaults = AppGroup.defaults) {
        defaults.removeObject(forKey: key)
    }

    /// The server answered CONSENT_REQUIRED. A record it had confirmed is out
    /// of date there (withdrawn on another device, or older documents) and is
    /// dropped; one still waiting to be sent is kept, the app sends it on its
    /// next start.
    static func forgetAfterServerRefusal(defaults: UserDefaults = AppGroup.defaults) {
        guard let record = load(defaults: defaults), !record.isPendingSync else { return }
        clear(defaults: defaults)
    }

    static func markSynced(defaults: UserDefaults = AppGroup.defaults) {
        guard var record = load(defaults: defaults) else { return }
        record.isPendingSync = false
        save(record, defaults: defaults)
    }

    private static func save(_ record: StoredLegalConsent, defaults: UserDefaults) {
        guard let data = try? JSONEncoder().encode(record) else { return }
        defaults.set(data, forKey: key)
    }
}
