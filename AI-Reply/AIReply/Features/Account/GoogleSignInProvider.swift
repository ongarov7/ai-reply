internal import GoogleSignIn
import UIKit

/// Google Sign-In through Google's official SDK (GoogleSignIn-iOS).
///
/// Google арқылы кіру: SDK тек осы файлда.
///
/// Only this file knows the SDK exists; everything else sees an ID token and
/// the nonce it was issued for, and the server verifies the token against
/// Google's keys. Configuration comes from Info.plist: `GIDClientID` and the
/// reversed client id URL scheme are filled from the GOOGLE_IOS_CLIENT_ID and
/// GOOGLE_IOS_REVERSED_CLIENT_ID build settings.
@MainActor
enum GoogleSignInProvider {

    enum Failure: Error, Equatable {
        /// No iOS OAuth client id in this build.
        case notConfigured
        /// The user closed Google's sheet.
        case cancelled
        case failed
    }

    struct Token: Equatable {
        let idToken: String
        let nonce: String
    }

    /// The iOS OAuth client id, or nil while this build is not set up for Google.
    static var clientID: String? {
        clientID(info: Bundle.main.infoDictionary ?? [:])
    }

    /// The client id from an Info.plist, accepted only together with its
    /// reversed form registered as a URL scheme: the SDK raises an
    /// Objective-C exception (a crash) when that scheme is missing, so a half
    /// finished setup hides the button instead.
    static func clientID(info: [String: Any]) -> String? {
        let suffix = ".apps.googleusercontent.com"
        guard let raw = info["GIDClientID"] as? String else { return nil }
        let value = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard value.hasSuffix(suffix), value.count > suffix.count else { return nil }
        let urlTypes = info["CFBundleURLTypes"] as? [[String: Any]] ?? []
        let schemes = urlTypes.flatMap { $0["CFBundleURLSchemes"] as? [String] ?? [] }
        let expected = reversedClientID(value)
        guard schemes.contains(where: { $0.caseInsensitiveCompare(expected) == .orderedSame }) else { return nil }
        return value
    }

    /// `123-abc.apps.googleusercontent.com` → `com.googleusercontent.apps.123-abc`.
    static func reversedClientID(_ clientID: String) -> String {
        clientID.split(separator: ".").reversed().joined(separator: ".")
    }

    static var isConfigured: Bool { clientID != nil }

    static func signIn() async throws -> Token {
        guard let clientID else { throw Failure.notConfigured }
        guard let presenter = presentingViewController() else { throw Failure.failed }
        let nonce = SignInNonce.make()
        GIDSignIn.sharedInstance.configuration = GIDConfiguration(clientID: clientID)

        return try await withCheckedThrowingContinuation { continuation in
            GIDSignIn.sharedInstance.signIn(
                withPresenting: presenter, hint: nil, additionalScopes: nil, nonce: nonce
            ) { result, error in
                if let error {
                    let cancelled = (error as? GIDSignInError)?.code == .canceled
                    continuation.resume(throwing: cancelled ? Failure.cancelled : Failure.failed)
                    return
                }
                guard let idToken = result?.user.idToken?.tokenString, !idToken.isEmpty else {
                    continuation.resume(throwing: Failure.failed)
                    return
                }
                continuation.resume(returning: Token(idToken: idToken, nonce: nonce))
            }
        }
    }

    /// Google's redirect back into the app.
    @discardableResult
    static func handle(_ url: URL) -> Bool {
        GIDSignIn.sharedInstance.handle(url)
    }

    /// Forgets the Google session kept by the SDK, so the next sign-in asks
    /// which account to use instead of silently reusing the last one.
    static func signOut() {
        GIDSignIn.sharedInstance.signOut()
    }

    private static func presentingViewController() -> UIViewController? {
        let scene = UIApplication.shared.connectedScenes
            .compactMap { $0 as? UIWindowScene }
            .first { $0.activationState == .foregroundActive }
        var top = scene?.windows.first(where: \.isKeyWindow)?.rootViewController
        while let presented = top?.presentedViewController {
            top = presented
        }
        return top
    }
}
