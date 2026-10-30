import Foundation

/// Error codes the backend puts in `{"error": {"code": …}}` (internal/transport/httpx).
public enum APIErrorCode {
    public static let invalidRequest = "INVALID_REQUEST"
    public static let invalidOTP = "INVALID_OTP"
    public static let otpExpired = "OTP_EXPIRED"
    public static let otpAlreadyUsed = "OTP_ALREADY_USED"
    public static let otpAttemptsExceeded = "OTP_ATTEMPTS_EXCEEDED"
    public static let otpResendCooldown = "OTP_RESEND_COOLDOWN"
    public static let invalidEmail = "INVALID_EMAIL"
    public static let emailDelivery = "EMAIL_DELIVERY_FAILED"
    public static let unauthorized = "UNAUTHORIZED"
    public static let tokenExpired = "TOKEN_EXPIRED"
    public static let accountDisabled = "ACCOUNT_DISABLED"
    public static let consentRequired = "CONSENT_REQUIRED"
    public static let dailyLimit = "DAILY_LIMIT_REACHED"
    public static let monthlyLimit = "MONTHLY_LIMIT_REACHED"
    public static let subscriptionExpired = "SUBSCRIPTION_EXPIRED"
    public static let paymentRequired = "PAYMENT_REQUIRED"
    public static let rateLimited = "RATE_LIMITED"
    public static let voiceLimit = "VOICE_LIMIT_REACHED"
    public static let providerUnavailable = "AI_PROVIDER_UNAVAILABLE"
    public static let providerTimeout = "AI_TIMEOUT"
    public static let emptyCompletion = "AI_EMPTY_RESPONSE"
    public static let notFound = "NOT_FOUND"
    public static let conflict = "CONFLICT"
    public static let internalError = "INTERNAL_ERROR"
}

/// A failed request: transport problems, or the server's error envelope.
public enum APIError: Error, Equatable, Sendable {
    /// No connection, DNS, TLS or a timeout before the server answered.
    case offline
    case cancelled
    /// The server answered with its error envelope.
    case server(status: Int, code: String, message: String, details: [String: JSONValue], retryAfter: TimeInterval?)
    /// The server answered something this client cannot read.
    case invalidResponse(status: Int)
    /// No session, or the session could not be refreshed: sign in again.
    case signedOut

    public var code: String? {
        if case let .server(_, code, _, _, _) = self { return code }
        return nil
    }

    public var status: Int? {
        switch self {
        case let .server(status, _, _, _, _), let .invalidResponse(status): return status
        default: return nil
        }
    }

    public func detail(_ key: String) -> JSONValue? {
        if case let .server(_, _, _, details, _) = self { return details[key] }
        return nil
    }
}

/// Untyped JSON for the free-form `details` object of an error.
public enum JSONValue: Codable, Equatable, Sendable {
    case string(String)
    case number(Double)
    case bool(Bool)
    case object([String: JSONValue])
    case array([JSONValue])
    case null

    public init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if container.decodeNil() { self = .null }
        else if let value = try? container.decode(Bool.self) { self = .bool(value) }
        else if let value = try? container.decode(Double.self) { self = .number(value) }
        else if let value = try? container.decode(String.self) { self = .string(value) }
        else if let value = try? container.decode([JSONValue].self) { self = .array(value) }
        else { self = .object(try container.decode([String: JSONValue].self)) }
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        switch self {
        case let .string(value): try container.encode(value)
        case let .number(value): try container.encode(value)
        case let .bool(value): try container.encode(value)
        case let .object(value): try container.encode(value)
        case let .array(value): try container.encode(value)
        case .null: try container.encodeNil()
        }
    }

    public var intValue: Int? {
        if case let .number(value) = self, value.rounded() == value { return Int(value) }
        if case let .string(value) = self { return Int(value) }
        return nil
    }

    public var stringValue: String? {
        if case let .string(value) = self { return value }
        return nil
    }
}
