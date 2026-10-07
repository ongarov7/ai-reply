import Foundation

/// Identifiers shared by the containing app and the keyboard extension.
///
/// Adding the App Group is an ADDITIVE entitlement change. Bundle identifiers,
/// the keyboard extension point and the signing team are untouched by it, but
/// the provisioning profiles do have to pick the capability up - with automatic
/// signing Xcode regenerates them on the next build.
enum AppGroup {

    static let identifier = "group.kz.ai-reply.shared"

    /// Defaults shared between the app and the keyboard.
    ///
    /// Falls back to this process's own container if the suite cannot be
    /// opened. That fallback is deliberate: a keyboard that loses its settings
    /// because a provisioning profile has not been regenerated yet is a much
    /// better failure than a keyboard that crashes on launch inside WhatsApp.
    static let defaults: UserDefaults = UserDefaults(suiteName: identifier) ?? .standard

    /// Whether the shared container is really reachable from this process.
    ///
    /// `UserDefaults(suiteName:)` succeeds even when the entitlement is missing,
    /// so it is not a usable probe. Asking for the container URL is - it returns
    /// nil unless the App Group is actually granted to this binary.
    static var isAvailable: Bool {
        FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: identifier) != nil
    }
}

/// Keychain access group shared by the containing app and its keyboard.
/// This value must match the expanded `keychain-access-groups` entitlement.
enum KeychainGroup {

    static let identifier = "2PK6339Q47.kz.ai-reply.shared"
}
