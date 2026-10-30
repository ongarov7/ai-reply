import Foundation
import Observation

/// Why a sign-in step failed, in terms the interface can phrase.
public enum AuthError: Error, Equatable, Sendable {
    case invalidEmail
    case invalidCode
    case codeExpired
    case codeAlreadyUsed
    case tooManyAttempts
    /// Wait this many seconds before asking for another code.
    case cooldown(seconds: Int)
    case rateLimited
    case emailDelivery
    case accountDisabled
    case offline
    case server

    init(_ error: APIError) {
        switch error.code {
        case APIErrorCode.invalidEmail: self = .invalidEmail
        case APIErrorCode.invalidOTP: self = .invalidCode
        case APIErrorCode.otpExpired: self = .codeExpired
        case APIErrorCode.otpAlreadyUsed: self = .codeAlreadyUsed
        case APIErrorCode.otpAttemptsExceeded: self = .tooManyAttempts
        case APIErrorCode.otpResendCooldown:
            if case let .server(_, _, _, _, retryAfter) = error, let retryAfter {
                self = .cooldown(seconds: Int(retryAfter.rounded(.up)))
            } else {
                self = .cooldown(seconds: 30)
            }
        case APIErrorCode.rateLimited: self = .rateLimited
        case APIErrorCode.emailDelivery: self = .emailDelivery
        case APIErrorCode.accountDisabled: self = .accountDisabled
        default: self = error == .offline ? .offline : .server
        }
    }
}

/// Email sign-in with a one-time code, and sign-out. Tokens never leave the
/// keychain-backed `APIClient`.
@MainActor
@Observable
public final class AuthSession {
    public struct Challenge: Equatable, Sendable {
        public let email: String
        public let maskedEmail: String
        public let codeLength: Int
        public let resendAvailableAt: Date
    }

    public enum State: Equatable, Sendable {
        case signedOut
        case awaitingCode(Challenge)
        case signedIn(email: String?)
    }

    public private(set) var state: State
    public private(set) var isWorking = false
    public private(set) var lastError: AuthError?
    /// The address typed last, kept to prefill the field after "another email".
    public private(set) var lastEmail = ""

    private let client: APIClient
    private let now: @MainActor () -> Date
    /// Called after a successful sign-in with the account state from the server.
    public var onSignedIn: ((SessionPayload) -> Void)?
    /// Called after sign-out, local or forced by the server.
    public var onSignedOut: (() -> Void)?

    public init(client: APIClient, restoredEmail: String?, isSignedIn: Bool, now: @escaping @MainActor () -> Date = Date.init) {
        self.client = client
        self.now = now
        state = isSignedIn ? .signedIn(email: restoredEmail) : .signedOut
    }

    public var isSignedIn: Bool {
        if case .signedIn = state { return true }
        return false
    }

    public func requestCode(email raw: String) async {
        let email = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        lastEmail = email
        guard email.contains("@"), email.count <= 254 else {
            lastError = .invalidEmail
            return
        }
        await sendCode(to: email)
    }

    public func resendCode() async {
        guard case let .awaitingCode(challenge) = state else { return }
        await sendCode(to: challenge.email)
    }

    private func sendCode(to email: String) async {
        struct Body: Encodable { let email: String; let locale: String }
        isWorking = true
        defer { isWorking = false }
        do {
            let challenge = try await client.send(
                .json("POST", "/api/v1/auth/email/otp/request", Body(email: email, locale: APIClient.preferredLanguage()),
                      authenticated: false),
                as: OTPChallenge.self)
            lastError = nil
            state = .awaitingCode(Challenge(email: email, maskedEmail: challenge.maskedEmail, codeLength: challenge.codeLength,
                                            resendAvailableAt: now().addingTimeInterval(TimeInterval(challenge.resendAfter))))
        } catch {
            lastError = AuthError(error)
        }
    }

    /// Returns true when signed in.
    @discardableResult
    public func verify(code raw: String) async -> Bool {
        guard case let .awaitingCode(challenge) = state else { return false }
        let code = raw.filter(\.isASCII).filter(\.isNumber)
        guard code.count == challenge.codeLength else {
            lastError = .invalidCode
            return false
        }
        struct Body: Encodable { let email: String; let code: String; let device: DeviceDescriptor }
        isWorking = true
        defer { isWorking = false }
        do {
            let device = await client.deviceDescriptor
            let payload = try await client.send(
                .json("POST", "/api/v1/auth/email/otp/verify", Body(email: challenge.email, code: code, device: device),
                      authenticated: false),
                as: SessionPayload.self)
            try await client.install(payload)
            lastError = nil
            state = .signedIn(email: payload.user.email ?? challenge.email)
            onSignedIn?(payload)
            return true
        } catch let error as APIError {
            lastError = AuthError(error)
        } catch {
            lastError = .server
        }
        return false
    }

    /// Back to the email field.
    public func useAnotherEmail() {
        lastError = nil
        state = .signedOut
    }

    public func clearError() {
        lastError = nil
    }

    /// Ends this Mac's session on the server (other devices stay signed in), then locally.
    public func signOut() async {
        struct Body: Encodable { let refresh_token: String }
        if let session = await client.session {
            _ = try? await client.send(.json("POST", "/api/v1/auth/logout", Body(refresh_token: session.refreshToken),
                                             authenticated: false, timeout: 10))
        }
        await client.clearSession()
        sessionEnded()
    }

    /// The client reported that the server ended the session.
    public func sessionEnded() {
        guard state != .signedOut else { return }
        state = .signedOut
        onSignedOut?()
    }
}
