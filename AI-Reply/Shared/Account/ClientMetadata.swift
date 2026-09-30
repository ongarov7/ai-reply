import Foundation

/// What every request says about the build that sent it.
///
/// Әр сұраныс: платформа, қосымша нұсқасы, build, ОЖ нұсқасы. Тек метадерек.
///
/// Metadata only. The server writes these into its logs and version reports
/// and never decides access by them. This file is compiled into the keyboard
/// extension too, so nothing here identifies the install or the user: the
/// installation and session ids live behind `APIClientHooks`, which only the
/// containing app ever fills in.
struct ClientMetadata: Equatable, Sendable {

    let platform: String
    let appVersion: String
    let appBuild: String
    let osVersion: String

    /// This process's own values, read once.
    static let current = ClientMetadata(bundle: .main, processInfo: .processInfo)

    init(platform: String = "ios", appVersion: String, appBuild: String, osVersion: String) {
        self.platform = platform
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
    /// "17.5.1"), without touching UIKit from a background thread.
    static func systemVersion(_ version: OperatingSystemVersion) -> String {
        var text = "\(version.majorVersion).\(version.minorVersion)"
        if version.patchVersion > 0 { text += ".\(version.patchVersion)" }
        return text
    }

    /// Header name → value. Empty values are left out rather than sent blank.
    var headers: [String: String] {
        var headers = ["X-Platform": platform]
        if !appVersion.isEmpty { headers["X-App-Version"] = appVersion }
        if !appBuild.isEmpty { headers["X-App-Build"] = appBuild }
        if !osVersion.isEmpty { headers["X-OS-Version"] = osVersion }
        return headers
    }
}

/// Correlation id for one request: `req_` and 16 random `[a-z0-9]`.
///
/// The server accepts `^[A-Za-z0-9._:-]{8,64}$`, keeps a valid one as it is
/// and puts it in its log line, its error envelope and the `X-Request-ID`
/// response header, so a failure reported from a phone can be found on the
/// server by this value alone.
enum RequestID {

    static let prefix = "req_"
    private static let alphabet = Array("abcdefghijklmnopqrstuvwxyz0123456789")

    static func make() -> String {
        var generator = SystemRandomNumberGenerator()
        return make(using: &generator)
    }

    static func make<Generator: RandomNumberGenerator>(using generator: inout Generator) -> String {
        var value = prefix
        for _ in 0..<16 {
            value.append(alphabet[Int.random(in: 0..<alphabet.count, using: &generator)])
        }
        return value
    }

    /// The server's own pattern for a request id it will keep.
    static func isValid(_ value: String) -> Bool {
        guard (8...64).contains(value.count) else { return false }
        return value.unicodeScalars.allSatisfy { scalar in
            switch scalar {
            case "A"..."Z", "a"..."z", "0"..."9", ".", "_", ":", "-": return true
            default: return false
            }
        }
    }
}

/// A request that failed without any HTTP response, as the app reports it.
///
/// Only the kind of failure travels, never the URL's query or any body.
struct APITransportFailure: Equatable, Sendable {
    enum Kind: String, Sendable {
        case timeout, offline, tls, io
    }

    let path: String
    let requestID: String
    let kind: Kind
}

/// The containing app's additions to every request, and where it hears about
/// requests that never got an answer.
///
/// Орнату мен сессия идентификаторын тек қосымша қосады; пернетақта ешқашан.
///
/// The keyboard extension compiles this file but never installs either hook,
/// and even if something did, `isAppExtension` keeps the identity out of an
/// extension's requests: the keyboard sends platform and versions, nothing
/// that names the install.
final class APIClientHooks: @unchecked Sendable {

    static let shared = APIClientHooks()

    struct Identity: Equatable, Sendable {
        var installationID: String?
        var sessionID: String?
    }

    /// Whether this process is an app extension (the keyboard).
    static let isAppExtension = Bundle.main.bundlePath.hasSuffix(".appex")

    private let lock = NSLock()
    private let allowsIdentity: Bool
    private var identityProvider: (@Sendable () -> Identity)?
    private var failureObserver: (@Sendable (APITransportFailure) -> Void)?

    init(allowsIdentity: Bool = !APIClientHooks.isAppExtension) {
        self.allowsIdentity = allowsIdentity
    }

    /// Called once by the containing app at launch.
    func install(identity: @escaping @Sendable () -> Identity,
                 transportFailures: @escaping @Sendable (APITransportFailure) -> Void) {
        lock.lock()
        defer { lock.unlock() }
        identityProvider = identity
        failureObserver = transportFailures
    }

    var identity: Identity? {
        guard allowsIdentity else { return nil }
        lock.lock()
        let provider = identityProvider
        lock.unlock()
        return provider?()
    }

    func report(_ failure: APITransportFailure) {
        guard allowsIdentity else { return }
        lock.lock()
        let observer = failureObserver
        lock.unlock()
        observer?(failure)
    }
}
