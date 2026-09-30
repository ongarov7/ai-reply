import Foundation

/// The APNs device token as the server stores it.
enum PushToken {

    /// Lowercase hex, two digits per byte: what the server's
    /// `^[0-9a-fA-F]{64,200}$` check and APNs itself expect.
    static func hexString(_ token: Data) -> String {
        token.map { String(format: "%02x", $0) }.joined()
    }
}

/// Which APNs host a token belongs to.
///
/// APNs токені қай ортаға тиесілі: sandbox не production.
///
/// A development-signed build gets sandbox tokens, which the production host
/// refuses (and the other way round), so the server needs to know. The build
/// cannot ask iOS; it reads what it was signed with:
///
/// - Simulator: sandbox.
/// - `embedded.mobileprovision` present (Xcode, Ad Hoc, Enterprise): its
///   `aps-environment` entitlement, "development" → sandbox, "production" →
///   production. A profile without the entitlement falls back to
///   `get-task-allow` (a debuggable build is a development build).
/// - No profile at all: App Store or TestFlight, which is production.
enum APNsEnvironment: String, Equatable, Sendable {
    case sandbox
    case production

    /// This build's environment. Reads the profile once.
    static let current: APNsEnvironment = {
        #if targetEnvironment(simulator)
        return detect(provisioningProfile: nil, isSimulator: true)
        #else
        let url = Bundle.main.url(forResource: "embedded", withExtension: "mobileprovision")
        return detect(provisioningProfile: url.flatMap { try? Data(contentsOf: $0) }, isSimulator: false)
        #endif
    }()

    static func detect(provisioningProfile: Data?, isSimulator: Bool) -> APNsEnvironment {
        if isSimulator { return .sandbox }
        guard let profile = provisioningProfile else { return .production }
        guard let entitlements = ProvisioningProfile.entitlements(in: profile) else { return .production }
        switch (entitlements["aps-environment"] as? String)?.lowercased() {
        case "development": return .sandbox
        case "production":  return .production
        default:
            return (entitlements["get-task-allow"] as? Bool) == true ? .sandbox : .production
        }
    }
}

/// Reads the property list inside a provisioning profile.
///
/// The file is a CMS (PKCS #7) signed message whose content is a plain XML
/// property list. Verifying the signature is iOS's job at install time; the
/// app only needs the entitlements, so it cuts the plist out of the envelope.
enum ProvisioningProfile {

    static func propertyList(in data: Data) -> [String: Any]? {
        guard let start = data.range(of: Data("<?xml".utf8)),
              let end = data.range(of: Data("</plist>".utf8), in: start.lowerBound..<data.endIndex) else {
            return nil
        }
        let plist = data.subdata(in: start.lowerBound..<end.upperBound)
        return (try? PropertyListSerialization.propertyList(from: plist, format: nil)) as? [String: Any]
    }

    static func entitlements(in data: Data) -> [String: Any]? {
        propertyList(in: data)?["Entitlements"] as? [String: Any]
    }
}
