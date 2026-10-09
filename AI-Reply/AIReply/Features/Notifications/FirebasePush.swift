internal import FirebaseCore
internal import FirebaseMessaging
import Foundation

/// Firebase Cloud Messaging through Google's official SDK.
///
/// FCM: SDK тек осы файлда; баптаусыз құрастыруда push жай ғана жоқ.
///
/// Only this file knows the SDK exists; everything else sees an APNs token
/// going in and an FCM registration token coming out. APNs still carries the
/// notifications: FCM matches the token to the sandbox or production host
/// from the provisioning profile.
///
/// The SDK raises an Objective-C exception (a crash) for a missing or
/// malformed option, so nothing is configured unless all four values are
/// present and well formed - from a bundled `GoogleService-Info.plist`, or
/// else from the FIREBASE_* build settings through Info.plist. Without them
/// push is simply unavailable in this build. `AppServices` never configures
/// it inside unit tests.
///
/// Info.plist turns off the SDK's app delegate swizzling (the app hands it the
/// APNs token itself) and its automatic token at launch, so no token exists
/// before the legal consent.
@MainActor
enum FirebasePush {

    struct Options: Equatable {
        let googleAppID: String
        let gcmSenderID: String
        let apiKey: String
        let projectID: String
    }

    private(set) static var isConfigured = false
    private static let delegate = TokenDelegate()

    /// Configures Firebase once. False when this build has no usable options.
    static func configureIfPossible(bundle: Bundle = .main) -> Bool {
        if isConfigured { return true }
        guard let options = bundledOptions(bundle: bundle) ?? options(info: bundle.infoDictionary ?? [:]) else {
            ReplyLog.event("push: Firebase is not configured in this build")
            return false
        }
        let firebaseOptions = FirebaseOptions(googleAppID: options.googleAppID, gcmSenderID: options.gcmSenderID)
        firebaseOptions.apiKey = options.apiKey
        firebaseOptions.projectID = options.projectID
        if let bundleID = bundle.bundleIdentifier { firebaseOptions.bundleID = bundleID }
        FirebaseApp.configure(options: firebaseOptions)
        isConfigured = true
        return true
    }

    /// The options from the Info.plist keys the FIREBASE_* build settings
    /// fill, or nil unless all four are there and well formed.
    nonisolated static func options(info: [String: Any]) -> Options? {
        func value(_ key: String) -> String? {
            guard let raw = (info[key] as? String)?.trimmingCharacters(in: .whitespacesAndNewlines),
                  !raw.isEmpty, !raw.hasPrefix("$(") else { return nil }
            return raw
        }
        return validated(googleAppID: value("AIReplyFirebaseAppID"), gcmSenderID: value("AIReplyFirebaseSenderID"),
                         apiKey: value("AIReplyFirebaseAPIKey"), projectID: value("AIReplyFirebaseProjectID"))
    }

    /// Accepts the four values only in the shape the SDK insists on.
    nonisolated static func validated(googleAppID: String?, gcmSenderID: String?, apiKey: String?, projectID: String?) -> Options? {
        guard let googleAppID, isAppID(googleAppID),
              let gcmSenderID, isNumber(gcmSenderID),
              let apiKey, isAPIKey(apiKey),
              let projectID, !projectID.isEmpty else { return nil }
        return Options(googleAppID: googleAppID, gcmSenderID: gcmSenderID, apiKey: apiKey, projectID: projectID)
    }

    /// `1:<project number>:ios:<hex>`, as the Firebase console issues it.
    nonisolated static func isAppID(_ value: String) -> Bool {
        let parts = value.split(separator: ":", omittingEmptySubsequences: false)
        return parts.count == 4 && parts[0] == "1" && parts[2] == "ios"
            && isNumber(parts[1]) && !parts[3].isEmpty
            && parts[3].unicodeScalars.allSatisfy { scalar in
                ("0"..."9").contains(scalar) || ("a"..."f").contains(scalar) || ("A"..."F").contains(scalar)
            }
    }

    /// Firebase Installations raises unless the key is 39 URL-safe characters
    /// starting with "A".
    nonisolated static func isAPIKey(_ value: String) -> Bool {
        value.count == 39 && value.hasPrefix("A") && value.unicodeScalars.allSatisfy { scalar in
            switch scalar {
            case "A"..."Z", "a"..."z", "0"..."9", "-", "_": return true
            default: return false
            }
        }
    }

    private nonisolated static func isNumber<Text: StringProtocol>(_ value: Text) -> Bool {
        !value.isEmpty && value.unicodeScalars.allSatisfy { ("0"..."9").contains($0) }
    }

    private static func bundledOptions(bundle: Bundle) -> Options? {
        guard let path = bundle.path(forResource: "GoogleService-Info", ofType: "plist"),
              let file = FirebaseOptions(contentsOfFile: path) else { return nil }
        return validated(googleAppID: file.googleAppID, gcmSenderID: file.gcmSenderID,
                         apiKey: file.apiKey, projectID: file.projectID)
    }

    // MARK: Tokens

    /// Calls `handler` with every FCM token the SDK hands out later on its
    /// own: a refresh, or the cached one at launch.
    static func observeTokens(_ handler: @escaping @MainActor (String) -> Void) {
        guard isConfigured else { return }
        delegate.handler = handler
        Messaging.messaging().delegate = delegate
    }

    /// From `didRegisterForRemoteNotificationsWithDeviceToken`. FCM asks for
    /// it before it hands out a token.
    static func setAPNSToken(_ deviceToken: Data) {
        guard isConfigured else { return }
        Messaging.messaging().apnsToken = deviceToken
    }

    /// The FCM registration token, made on first use. Only after the APNs
    /// token was set.
    static func token() async throws -> String {
        guard isConfigured else { throw FirebasePushError.notConfigured }
        let tokens: RegistrationTokens = Messaging.messaging()
        return try await withCheckedThrowingContinuation { continuation in
            tokens.token { token, error in
                if let token, !token.isEmpty {
                    continuation.resume(returning: token)
                } else {
                    continuation.resume(throwing: error ?? FirebasePushError.noToken)
                }
            }
        }
    }

    /// Drops the current token and makes a new one: what the server asks for
    /// when FCM has stopped accepting the old one.
    static func replaceToken() async throws -> String {
        guard isConfigured else { throw FirebasePushError.notConfigured }
        let tokens: RegistrationTokens = Messaging.messaging()
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            tokens.deleteToken { error in
                if let error {
                    continuation.resume(throwing: error)
                } else {
                    continuation.resume()
                }
            }
        }
        return try await token()
    }
}

enum FirebasePushError: Error, Equatable {
    case notConfigured
    case noToken
}

/// FCM registration tokens: what the server sends to (`message.token`), the
/// same kind the Android app registers.
///
/// Firebase 12.18 deprecated these calls in favour of its opt-in registration
/// by installation id, which the server does not use; they keep working.
/// Reaching them through this protocol says so once, here.
private protocol RegistrationTokens: AnyObject {
    func token(completion: @escaping @Sendable (String?, (any Error)?) -> Void)
    func deleteToken(completion: @escaping @Sendable ((any Error)?) -> Void)
}

extension Messaging: RegistrationTokens {}

/// The SDK calls this on the main thread.
private final class TokenDelegate: NSObject, MessagingDelegate {

    var handler: (@MainActor (String) -> Void)?

    func messaging(_ messaging: Messaging, didReceiveRegistrationToken fcmToken: String?) {
        guard let fcmToken, !fcmToken.isEmpty else { return }
        MainActor.assumeIsolated { handler?(fcmToken) }
    }
}
