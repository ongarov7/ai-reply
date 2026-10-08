import Foundation

/// Every way a backend call can fail, as a value the UI can localize.
///
/// Сервер қатесі — тұрақты код, аударма қосымшада.
///
/// The server sends a stable `code`; the English `message` beside it is for a
/// developer reading a log, never for a user. Mapping happens once, here, so no
/// screen has to know what an HTTP status means.
enum APIError: Error, Equatable, Sendable {
    case offline
    case timedOut
    case cancelled
    /// The session is gone: the user has to sign in again.
    case unauthorized
    case accountDisabled
    /// A wrong code. The server says how many tries this code has left.
    case invalidOTP(attemptsRemaining: Int?)
    case otpExpired
    /// The code already opened a session; a new one has to be requested.
    case otpAlreadyUsed
    /// Five wrong tries: this code is burnt, a new one has to be requested.
    case otpAttemptsExceeded
    /// A new code was asked for too soon. The server's wait, in seconds.
    case resendCooldown(retryAfter: Int?)
    case rateLimited(retryAfter: Int?)
    case invalidEmail
    /// The code e-mail could not be sent (the mail provider refused or is down).
    case emailDeliveryFailed
    /// The address belongs to another account.
    case emailInUse
    /// Google or Apple returned a token the server did not accept.
    case invalidIDToken
    /// The sign-in provider is not configured on the server, or unreachable.
    case authProviderUnavailable
    /// Daily quota is spent. `resetsAt` is ISO-8601 from the server.
    case dailyLimitReached(limit: Int, usedToday: Int, resetsAt: String?)
    /// The plan's replies for this month are spent: tomorrow does not help.
    case monthlyLimitReached
    /// The account has not accepted the current terms and privacy policy,
    /// so the AI endpoints refuse until the app records a new consent.
    case consentRequired
    case subscriptionExpired
    case paymentRequired
    case providerUnavailable
    case providerTimeout
    case emptyResponse
    case invalidRequest
    /// The incoming message is longer than the server's limit, which the
    /// server states. A plain `invalidRequest` from an older server carries no
    /// number, and stays `invalidRequest`.
    case sourceTooLong(limit: Int)
    /// Compose: the instruction is longer than the server's limit, which the
    /// server states.
    case instructionTooLong(limit: Int)
    /// Compose: the server found the instruction empty.
    case instructionMissing
    case notFound
    case conflict
    case server
    /// The client could not make sense of the response at all.
    case malformedResponse

    /// Whether retrying the same request could plausibly succeed.
    var isTransient: Bool {
        switch self {
        case .offline, .timedOut, .providerUnavailable, .providerTimeout, .server,
             .emailDeliveryFailed, .authProviderUnavailable: return true
        default: return false
        }
    }
}

/// Thin HTTP client for the AI Reply backend.
///
/// No third-party dependency, no retry loops hidden inside, no shared mutable
/// state: one method, one request, one decoded value.
struct APIClient: Sendable {

    private let baseURL: URL
    private let session: URLSession
    /// Extra headers for every request of this client. Only the logout
    /// request uses them (`X-Installation-ID`); everything else sends none.
    private let headers: [String: String]

    init(baseURL: URL, session: URLSession? = nil, headers: [String: String] = [:]) {
        self.baseURL = baseURL
        self.session = session ?? ReplyNetworking.makeSession(timeout: AIConfiguration.requestTimeout)
        self.headers = headers
    }

    /// Decoded response plus the bearer token that was used, so a caller that
    /// refreshed mid-flight does not have to ask again.
    struct Empty: Codable, Sendable {}

    // MARK: Requests

    func get<Response: Decodable>(_ path: String, token: String? = nil) async throws -> Response {
        try await send(path: path, method: "GET", body: Optional<Empty>.none, token: token)
    }

    @discardableResult
    func post<Body: Encodable, Response: Decodable>(
        _ path: String, body: Body, token: String? = nil
    ) async throws -> Response {
        try await send(path: path, method: "POST", body: body, token: token)
    }

    @discardableResult
    func patch<Body: Encodable, Response: Decodable>(
        _ path: String, body: Body, token: String? = nil
    ) async throws -> Response {
        try await send(path: path, method: "PATCH", body: body, token: token)
    }

    @discardableResult
    func put<Body: Encodable, Response: Decodable>(
        _ path: String, body: Body, token: String? = nil
    ) async throws -> Response {
        try await send(path: path, method: "PUT", body: body, token: token)
    }

    @discardableResult
    func delete<Response: Decodable>(_ path: String, token: String? = nil) async throws -> Response {
        try await send(path: path, method: "DELETE", body: Optional<Empty>.none, token: token)
    }

    /// DELETE with a JSON body: account deletion carries the Apple code.
    @discardableResult
    func delete<Body: Encodable, Response: Decodable>(
        _ path: String, body: Body, token: String? = nil
    ) async throws -> Response {
        try await send(path: path, method: "DELETE", body: body, token: token)
    }

    // MARK: Transport

    /// The request `send` puts on the wire; separate so its shape is testable.
    func makeRequest<Body: Encodable>(path: String, method: String, body: Body?, token: String?) throws -> URLRequest {
        var request = URLRequest(url: baseURL.appendingPathComponent(path))
        request.httpMethod = method
        for (name, value) in headers {
            request.setValue(value, forHTTPHeaderField: name)
        }
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        if let token {
            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
        if let body {
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = try JSONEncoder().encode(body)
        }
        return request
    }

    private func send<Body: Encodable, Response: Decodable>(
        path: String, method: String, body: Body?, token: String?
    ) async throws -> Response {
        let request = try makeRequest(path: path, method: method, body: body, token: token)

        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await session.data(for: request)
        } catch {
            throw Self.mapTransportError(error)
        }

        guard let http = response as? HTTPURLResponse else { throw APIError.malformedResponse }

        guard (200..<300).contains(http.statusCode) else {
            throw Self.mapServerError(status: http.statusCode, data: data, headers: http)
        }

        if Response.self == Empty.self { return Empty() as! Response }

        do {
            return try JSONDecoder().decode(Response.self, from: data)
        } catch {
            throw APIError.malformedResponse
        }
    }

    // MARK: Error mapping

    /// The error envelope every endpoint uses.
    private struct ErrorEnvelope: Decodable {
        struct Payload: Decodable {
            let code: String
            let message: String?
            let details: Details?
        }
        struct Details: Decodable {
            let dailyLimit: Int?
            let usedToday: Int?
            let resetsAt: String?
            let retryAfterSeconds: Int?
            /// Which request field an INVALID_REQUEST is about, when the
            /// server says.
            let field: String?
            let maxCharacters: Int?
            let attemptsRemaining: Int?

            enum CodingKeys: String, CodingKey {
                case dailyLimit = "daily_limit"
                case usedToday = "used_today"
                case resetsAt = "resets_at"
                case retryAfterSeconds = "retry_after_seconds"
                case field
                case maxCharacters = "max_characters"
                case attemptsRemaining = "attempts_remaining"
            }
        }
        let error: Payload
    }

    static func mapServerError(status: Int, data: Data, headers: HTTPURLResponse) -> APIError {
        let envelope = try? JSONDecoder().decode(ErrorEnvelope.self, from: data)
        let details = envelope?.error.details
        let retryAfter = details?.retryAfterSeconds
            ?? Int(headers.value(forHTTPHeaderField: "Retry-After") ?? "")

        switch envelope?.error.code {
        case "UNAUTHORIZED", "TOKEN_EXPIRED":   return .unauthorized
        case "ACCOUNT_DISABLED":                return .accountDisabled
        case "INVALID_OTP":                     return .invalidOTP(attemptsRemaining: details?.attemptsRemaining)
        case "OTP_EXPIRED":                     return .otpExpired
        case "OTP_ALREADY_USED":                return .otpAlreadyUsed
        case "OTP_ATTEMPTS_EXCEEDED":           return .otpAttemptsExceeded
        case "OTP_RESEND_COOLDOWN":             return .resendCooldown(retryAfter: retryAfter)
        case "RATE_LIMITED":                    return .rateLimited(retryAfter: retryAfter)
        case "INVALID_EMAIL":                   return .invalidEmail
        case "EMAIL_DELIVERY_FAILED":           return .emailDeliveryFailed
        case "EMAIL_ALREADY_IN_USE":            return .emailInUse
        case "INVALID_ID_TOKEN":                return .invalidIDToken
        case "AUTH_PROVIDER_UNAVAILABLE":       return .authProviderUnavailable
        case "DAILY_LIMIT_REACHED":
            return .dailyLimitReached(limit: details?.dailyLimit ?? 0,
                                      usedToday: details?.usedToday ?? 0,
                                      resetsAt: details?.resetsAt)
        case "MONTHLY_LIMIT_REACHED":           return .monthlyLimitReached
        case "CONSENT_REQUIRED":                return .consentRequired
        case "SUBSCRIPTION_EXPIRED":            return .subscriptionExpired
        case "PAYMENT_REQUIRED":                return .paymentRequired
        case "AI_PROVIDER_UNAVAILABLE":         return .providerUnavailable
        case "AI_TIMEOUT":                      return .providerTimeout
        case "AI_EMPTY_RESPONSE":               return .emptyResponse
        case "INVALID_REQUEST":
            if details?.field == "source_text", let limit = details?.maxCharacters, limit > 0 {
                return .sourceTooLong(limit: limit)
            }
            if details?.field == "instruction" {
                if let limit = details?.maxCharacters, limit > 0 { return .instructionTooLong(limit: limit) }
                return .instructionMissing
            }
            return .invalidRequest
        case "NOT_FOUND":                       return .notFound
        case "CONFLICT":                        return .conflict
        default: break
        }

        // No envelope (a proxy error page, say): fall back to the status.
        switch status {
        case 401, 403: return .unauthorized
        case 404:      return .notFound
        case 408, 504: return .providerTimeout
        case 409:      return .conflict
        case 429:      return .rateLimited(retryAfter: retryAfter)
        case 400...499: return .invalidRequest
        default:       return .server
        }
    }

    static func mapTransportError(_ error: Error) -> APIError {
        if error is CancellationError { return .cancelled }
        let nsError = error as NSError
        guard nsError.domain == NSURLErrorDomain else { return .server }
        switch nsError.code {
        case NSURLErrorCancelled: return .cancelled
        case NSURLErrorTimedOut:  return .timedOut
        case NSURLErrorNotConnectedToInternet,
             NSURLErrorNetworkConnectionLost,
             NSURLErrorCannotFindHost,
             NSURLErrorCannotConnectToHost,
             NSURLErrorDataNotAllowed,
             NSURLErrorInternationalRoamingOff:
            return .offline
        default: return .server
        }
    }
}
