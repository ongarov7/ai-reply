import Foundation
import Security

/// What the app tells the server about this install.
///
/// Орнату туралы метадерек: нұсқа, құрылғы үлгісі, тіл, уақыт белдеуі.
///
/// Device and build facts only: no name, no contacts, no advertising id, no
/// vendor id, no location. The model is a hardware identifier like
/// "iPhone17,1", never the name the owner gave the phone.
struct ClientContext: Equatable, Sendable {
    let installationID: String
    let appVersion: String
    let appBuild: String
    let osVersion: String
    let deviceModel: String
    /// The app's interface language (en, ru, kk, uz), not the phone's.
    let locale: String
    /// IANA name, e.g. "Asia/Almaty".
    let timezone: String

    init(installationID: String,
         build: BuildInfo = .current,
         deviceModel: String = DeviceModel.identifier,
         locale: String,
         timezone: String = TimeZone.current.identifier) {
        self.installationID = installationID
        self.appVersion = build.appVersion
        self.appBuild = build.appBuild
        self.osVersion = build.osVersion
        self.deviceModel = deviceModel
        self.locale = locale
        self.timezone = timezone
    }

    func installationPayload(permission: NotificationPermission,
                             notificationsEnabled: Bool,
                             push: InstallationPayload.Push?) -> InstallationPayload {
        InstallationPayload(
            installation_id: installationID,
            platform: "ios",
            app_version: appVersion,
            app_build: appBuild,
            os_name: "iOS",
            os_version: osVersion,
            device_model: deviceModel,
            manufacturer: "Apple",
            locale: locale,
            timezone: timezone,
            notification_permission: permission.rawValue,
            notifications_enabled: notificationsEnabled,
            push: push
        )
    }
}

/// The build and system versions, read once.
struct BuildInfo: Equatable, Sendable {
    let appVersion: String
    let appBuild: String
    let osVersion: String

    static let current = BuildInfo(bundle: .main, processInfo: .processInfo)

    init(appVersion: String, appBuild: String, osVersion: String) {
        self.appVersion = appVersion
        self.appBuild = appBuild
        self.osVersion = osVersion
    }

    init(bundle: Bundle, processInfo: ProcessInfo) {
        self.init(
            appVersion: bundle.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "",
            appBuild: bundle.object(forInfoDictionaryKey: "CFBundleVersion") as? String ?? "",
            osVersion: Self.systemVersion(processInfo.operatingSystemVersion)
        )
    }

    /// The same text `UIDevice.current.systemVersion` gives ("17.5",
    /// "17.5.1"), without touching UIKit.
    static func systemVersion(_ version: OperatingSystemVersion) -> String {
        var text = "\(version.majorVersion).\(version.minorVersion)"
        if version.patchVersion > 0 { text += ".\(version.patchVersion)" }
        return text
    }
}

/// The hardware model identifier ("iPhone17,1").
enum DeviceModel {

    static let identifier: String = {
        // The simulator reports the Mac's architecture from uname; it names
        // the device it is simulating in its environment instead.
        if let simulated = ProcessInfo.processInfo.environment["SIMULATOR_MODEL_IDENTIFIER"], !simulated.isEmpty {
            return simulated
        }
        var info = utsname()
        uname(&info)
        return withUnsafeBytes(of: &info.machine) { raw in
            String(decoding: raw.prefix { $0 != 0 }, as: UTF8.self)
        }
    }()
}

// MARK: - Installation id

/// Where the installation id is kept.
protocol InstallationIDStorage {
    func load() -> InstallationIDLoad
    func save(_ id: String) -> Bool
}

enum InstallationIDLoad: Equatable {
    case found(String)
    case notFound
    /// The Keychain cannot be read right now (before the first unlock).
    case unavailable
}

/// The install's own random id.
///
/// Орнату идентификаторы: кездейсоқ UUID, бір рет жасалады, Keychain-де.
///
/// - A random UUID made once. Never the advertising id or the vendor id, and
///   never derived from them or from hardware.
/// - Kept in the Keychain as `kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly`
///   in the app's own access group (see `KeychainInstallationIDStorage`): the
///   keyboard extension cannot read it.
/// - It survives deleting and reinstalling the app on the same iPhone, because
///   iOS currently keeps an app's Keychain items after the app is deleted; the
///   server then recognises the same install instead of counting a new one.
/// - "ThisDeviceOnly": it is never restored onto another device from a backup,
///   so a new phone is a new installation.
/// - It is not a credential. It names this install to the server and grants
///   nothing: the account always comes from the access token.
final class InstallationIdentity {

    private let storage: InstallationIDStorage
    private let makeID: () -> String
    private var cached: String?

    init(storage: InstallationIDStorage = KeychainInstallationIDStorage(),
         makeID: @escaping () -> String = { UUID().uuidString.lowercased() }) {
        self.storage = storage
        self.makeID = makeID
    }

    /// The id, created on first use. Nil only while the Keychain is locked or
    /// refuses the write; the next call tries again.
    var id: String? {
        if let cached { return cached }
        switch storage.load() {
        case .found(let value) where Self.isValid(value):
            cached = value
        case .found, .notFound:
            let created = makeID()
            if storage.save(created) { cached = created }
        case .unavailable:
            return nil
        }
        return cached
    }

    /// The server's pattern for an installation id, `^[A-Za-z0-9-]{8,64}$`.
    static func isValid(_ value: String) -> Bool {
        guard (8...64).contains(value.count) else { return false }
        return value.unicodeScalars.allSatisfy { scalar in
            switch scalar {
            case "A"..."Z", "a"..."z", "0"..."9", "-": return true
            default: return false
            }
        }
    }
}

/// The Keychain item behind `InstallationIdentity`.
///
/// The app's entitlements list the shared keychain group the keyboard also
/// has, and an item saved without an access group lands in the first group
/// listed - the shared one. So the group is named: the app's own application
/// identifier, which every app may use and no extension of it can read.
struct KeychainInstallationIDStorage: InstallationIDStorage {

    private let service = "kz.ai-reply.installation"
    private let account = "installation.id"
    private let accessGroup: String?

    init(accessGroup: String? = KeychainInstallationIDStorage.applicationIdentifier) {
        self.accessGroup = accessGroup
    }

    private var query: [String: Any] {
        var query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account
        ]
        if let accessGroup { query[kSecAttrAccessGroup as String] = accessGroup }
        return query
    }

    func load() -> InstallationIDLoad {
        var request = query
        request[kSecReturnData as String] = true
        request[kSecMatchLimit as String] = kSecMatchLimitOne
        var item: CFTypeRef?
        switch SecItemCopyMatching(request as CFDictionary, &item) {
        case errSecSuccess:
            guard let data = item as? Data, let value = String(data: data, encoding: .utf8) else {
                return .notFound
            }
            return .found(value)
        case errSecItemNotFound:
            return .notFound
        default:
            return .unavailable
        }
    }

    func save(_ id: String) -> Bool {
        let attributes: [String: Any] = [
            kSecValueData as String: Data(id.utf8),
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        ]
        let update = SecItemUpdate(query as CFDictionary, attributes as CFDictionary)
        if update == errSecSuccess { return true }
        guard update == errSecItemNotFound else { return false }
        var insert = query
        insert.merge(attributes) { current, _ in current }
        return SecItemAdd(insert as CFDictionary, nil) == errSecSuccess
    }

    /// "<team id>.<bundle id>". Xcode fills `AIReplyAppIdentifierPrefix`
    /// ("<team id>.") when it signs the app; an unsigned build has none and
    /// keeps the item in the default group.
    static var applicationIdentifier: String? {
        applicationIdentifier(info: Bundle.main.infoDictionary ?? [:], bundleID: Bundle.main.bundleIdentifier)
    }

    static func applicationIdentifier(info: [String: Any], bundleID: String?) -> String? {
        guard let prefix = info["AIReplyAppIdentifierPrefix"] as? String,
              prefix.count > 1, prefix.hasSuffix("."), !prefix.contains("$("),
              let bundleID, !bundleID.isEmpty else { return nil }
        return prefix + bundleID
    }
}
