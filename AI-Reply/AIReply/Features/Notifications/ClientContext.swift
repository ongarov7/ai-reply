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
    let platform: String
    let appVersion: String
    let appBuild: String
    let osName: String
    let osVersion: String
    let deviceModel: String
    let manufacturer: String
    /// The app's interface language (en, ru, kk, uz), not the phone's.
    let locale: String
    /// IANA name, e.g. "Asia/Almaty".
    let timezone: String

    init(installationID: String,
         metadata: ClientMetadata = .current,
         deviceModel: String = DeviceModel.identifier,
         locale: String,
         timezone: String = TimeZone.current.identifier) {
        self.installationID = installationID
        self.platform = metadata.platform
        self.appVersion = metadata.appVersion
        self.appBuild = metadata.appBuild
        self.osName = "iOS"
        self.osVersion = metadata.osVersion
        self.deviceModel = deviceModel
        self.manufacturer = "Apple"
        self.locale = locale
        self.timezone = timezone
    }

    func installationPayload(permission: NotificationPermission,
                             notificationsEnabled: Bool,
                             push: InstallationPayload.Push?) -> InstallationPayload {
        InstallationPayload(
            installation_id: installationID,
            platform: platform,
            app_version: appVersion,
            app_build: appBuild,
            os_name: osName,
            os_version: osVersion,
            device_model: deviceModel,
            manufacturer: manufacturer,
            locale: locale,
            timezone: timezone,
            notification_permission: permission.rawValue,
            notifications_enabled: notificationsEnabled,
            push: push
        )
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
///   in the app's OWN access group (no access group is named, so iOS uses the
///   app's application identifier): the keyboard extension cannot read it.
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
struct KeychainInstallationIDStorage: InstallationIDStorage {

    private let service = "kz.yerek.replykeyboard.installation"
    private let account = "installation.id"

    /// No `kSecAttrAccessGroup`: the item lives in the app's own group, not in
    /// the App Group the keyboard shares.
    private var query: [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account
        ]
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
}

// MARK: - Session

/// The app session: a new id at every cold start and after 30 minutes in the
/// background. It ties an API error to the events around it on the server.
@MainActor
final class SessionTracker {

    static let backgroundTimeout: TimeInterval = 30 * 60

    enum Transition: Equatable {
        /// The app came to the front. `newSession` is false after a short
        /// trip to the background.
        case opened(coldStart: Bool, newSession: Bool)
        case backgrounded(foregroundSeconds: Int)
    }

    private(set) var sessionID: String
    private var isInForeground = false
    private var isColdStart = true
    private var foregroundSince: Date?
    private var backgroundSince: Date?
    private let now: () -> Date
    private let makeID: () -> String

    init(now: @escaping () -> Date = Date.init,
         makeID: @escaping () -> String = { UUID().uuidString.lowercased() }) {
        self.now = now
        self.makeID = makeID
        self.sessionID = makeID()
    }

    /// scenePhase became `.active`. Nil when the app never left the front
    /// (a system alert or Control Center only makes it inactive).
    func didBecomeActive() -> Transition? {
        guard !isInForeground else { return nil }
        isInForeground = true
        let date = now()
        defer {
            foregroundSince = date
            backgroundSince = nil
            isColdStart = false
        }
        if isColdStart {
            return .opened(coldStart: true, newSession: true)
        }
        if let since = backgroundSince, date.timeIntervalSince(since) >= Self.backgroundTimeout {
            sessionID = makeID()
            return .opened(coldStart: false, newSession: true)
        }
        return .opened(coldStart: false, newSession: false)
    }

    /// scenePhase became `.background`.
    func didEnterBackground() -> Transition? {
        guard isInForeground else { return nil }
        isInForeground = false
        let date = now()
        backgroundSince = date
        let seconds = foregroundSince.map { Int(max(0, date.timeIntervalSince($0)).rounded()) } ?? 0
        foregroundSince = nil
        return .backgrounded(foregroundSeconds: seconds)
    }
}

/// The identity the containing app adds to its requests, readable from any
/// thread (`APIClient` runs off the main actor).
final class ClientIdentityHolder: @unchecked Sendable {
    private let lock = NSLock()
    private var identity = APIClientHooks.Identity()

    var current: APIClientHooks.Identity {
        lock.lock()
        defer { lock.unlock() }
        return identity
    }

    func update(installationID: String?, sessionID: String?) {
        lock.lock()
        identity = APIClientHooks.Identity(installationID: installationID, sessionID: sessionID)
        lock.unlock()
    }
}
