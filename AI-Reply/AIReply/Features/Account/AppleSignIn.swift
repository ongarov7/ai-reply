import AuthenticationServices
import CryptoKit
import Foundation

/// A fresh random value for one sign-in attempt.
///
/// Бір кіру әрекетіне арналған кездейсоқ мән (nonce).
///
/// Google gets it as is and puts it in the ID token; Apple gets its SHA-256
/// and puts that in the identity token. The server checks the token carries
/// the value this attempt made, so a token taken from somewhere else cannot
/// be replayed with a different one.
enum SignInNonce {

    /// 32 bytes from the system's cryptographic generator, URL-safe base64.
    static func make() -> String {
        var generator = SystemRandomNumberGenerator()
        let bytes = (0..<32).map { _ in UInt8.random(in: .min ... .max, using: &generator) }
        return Data(bytes).base64EncodedString()
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }

    /// Lowercase hex SHA-256 — what Apple expects in the request's nonce.
    static func sha256(_ value: String) -> String {
        SHA256.hash(data: Data(value.utf8)).map { String(format: "%02x", $0) }.joined()
    }
}

/// Sign in with Apple through AuthenticationServices.
///
/// Apple арқылы кіру: токенді сервер тексереді.
///
/// The app never decides who the user is: it hands Apple's identity token and
/// the raw nonce to the server, which checks both against Apple's keys.
enum AppleSignIn {

    struct Credential: Equatable {
        let identityToken: String
        /// Apple's stable user id for this app. Not a secret.
        let userID: String
        /// Only present on the very first authorization; empty afterwards.
        let fullName: String
    }

    /// Whether this build carries the Sign in with Apple entitlement.
    ///
    /// Release builds always do. Debug builds do not by default, because a
    /// free (Personal Team) Apple account cannot sign the entitlement and the
    /// app could not be installed on a phone at all. The build setting
    /// AIREPLY_SIGN_IN_WITH_APPLE chooses the entitlements file and reaches
    /// the app through Info.plist, so the button never appears in a build
    /// that could not complete the sign-in.
    static var isEnabledInThisBuild: Bool {
        isEnabled(info: Bundle.main.infoDictionary ?? [:])
    }

    static func isEnabled(info: [String: Any]) -> Bool {
        (info["AIReplySignInWithApple"] as? String)?.uppercased() == "YES"
    }

    static func configure(_ request: ASAuthorizationAppleIDRequest, rawNonce: String) {
        request.requestedScopes = [.fullName, .email]
        request.nonce = SignInNonce.sha256(rawNonce)
    }

    static func credential(from authorization: ASAuthorization) -> Credential? {
        guard let apple = authorization.credential as? ASAuthorizationAppleIDCredential,
              let data = apple.identityToken,
              let token = String(data: data, encoding: .utf8),
              !token.isEmpty else { return nil }
        return Credential(identityToken: token, userID: apple.user, fullName: fullName(apple.fullName))
    }

    static func fullName(_ components: PersonNameComponents?) -> String {
        guard let components else { return "" }
        return PersonNameComponentsFormatter.localizedString(from: components, style: .default)
            .trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// The user closed the Apple sheet: not an error worth a message.
    static func isCancellation(_ error: Error) -> Bool {
        (error as? ASAuthorizationError)?.code == .canceled
    }

    // MARK: Revocation

    /// Kept so the app can notice when the user stops using their Apple ID
    /// with it (Settings ▸ Apple ID ▸ Sign in with Apple ▸ Stop Using).
    private static let userIDKey = "account.appleUserID"

    static func remember(userID: String, defaults: UserDefaults = .standard) {
        defaults.set(userID, forKey: userIDKey)
    }

    static func forget(defaults: UserDefaults = .standard) {
        defaults.removeObject(forKey: userIDKey)
    }

    /// True only when Apple reports the authorization as revoked.
    static func isAuthorizationRevoked(defaults: UserDefaults = .standard) async -> Bool {
        guard let userID = defaults.string(forKey: userIDKey) else { return false }
        let state = try? await ASAuthorizationAppleIDProvider().credentialState(forUserID: userID)
        return state == .revoked
    }
}
