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

/// A failed call together with what ties it to the server's log.
///
/// Сәтсіз сұраныс: қате, сұраныс идентификаторы және HTTP күйі.
///
/// `APIError` stays the value screens switch on; this wraps it with the
/// request id (the `X-Request-ID` the server answered with, else the one this
/// client sent) and the HTTP status, 0 when no response arrived at all.
struct APIFailure: Error, Equatable, Sendable {
    let error: APIError
    let requestID: String
    let status: Int
    /// The server's stable code ("RATE_LIMITED"), when it sent an envelope.
    let code: String?
    /// Seconds the server asked the client to wait, from the envelope or
    /// `Retry-After`.
    let retryAfter: Int?

    init(error: APIError, requestID: String, status: Int, code: String? = nil, retryAfter: Int? = nil) {
        self.error = error
        self.requestID = requestID
        self.status = status
        self.code = code
        self.retryAfter = retryAfter
    }

    /// No HTTP response at all: offline, timed out, TLS, cancelled.
    var isTransport: Bool { status == 0 }
}

/// Thin HTTP client for the AI Reply backend.
///
/// No third-party dependency, no retry loops hidden inside, no shared mutable
/// state: one method, one request, one decoded value.
///
/// Every request carries the client headers the server logs (`X-Platform`,
/// `X-App-Version`, `X-App-Build`, `X-OS-Version`) and a fresh `X-Request-ID`.
/// Only the containing app adds `X-Installation-ID` and `X-Session-ID`,
/// through `APIClientHooks`; the keyboard never does.
struct APIClient: Sendable {

    private let baseURL: URL
    private let session: URLSession
    private let metadata: ClientMetadata
    private let hooks: APIClientHooks
    private let makeRequestID: @Sendable () -> String

    init(baseURL: URL,
         session: URLSession? = nil,
         metadata: ClientMetadata = .current,
         hooks: APIClientHooks = .shared,
         makeRequestID: @escaping @Sendable () -> String = { RequestID.make() }) {
        self.baseURL = baseURL
        self.session = session ?? ReplyNetworking.makeSession(timeout: AIConfiguration.requestTimeout)
        self.metadata = metadata
        self.hooks = hooks
        self.makeRequestID = makeRequestID
    }

    /// Decoded response plus the bearer token that was used, so a caller that
    /// refreshed mid-flight does not have to ask again.
    struct Empty: Codable, Sendable {}

    // MARK: Requests
    //
    // These throw `APIError`, which is what every screen matches on. The
    // `perform` family below throws `APIFailure` for callers that also need
    // the request id.

    func get<Response: Decodable>(_ path: String, token: String? = nil) async throws -> Response {
        try await unwrapped { try await perform("GET", path, body: Optional<Empty>.none, token: token) }
    }

    @discardableResult
    func post<Body: Encodable, Response: Decodable>(
        _ path: String, body: Body, token: String? = nil
    ) async throws -> Response {
        try await unwrapped { try await perform("POST", path, body: body, token: token) }
    }

    @discardableResult
    func patch<Body: Encodable, Response: Decodable>(
        _ path: String, body: Body, token: String? = nil
    ) async throws -> Response {
        try await unwrapped { try await perform("PATCH", path, body: body, token: token) }
    }

    @discardableResult
    func put<Body: Encodable, Response: Decodable>(
        _ path: String, body: Body, token: String? = nil
    ) async throws -> Response {
        try await unwrapped { try await perform("PUT", path, body: body, token: token) }
    }

    private func unwrapped<Response>(_ work: () async throws -> Response) async throws -> Response {
        do {
            return try await work()
        } catch let failure as APIFailure {
            throw failure.error
        }
    }

    // MARK: Transport

    /// One request. Throws `APIFailure`, never anything else.
    func perform<Body: Encodable, Response: Decodable>(
        _ method: String, _ path: String, body: Body?, token: String?
    ) async throws -> Response {
        let requestID = makeRequestID()
        var request = URLRequest(url: baseURL.appendingPathComponent(path))
        request.httpMethod = method
        for (name, value) in headers(requestID: requestID) {
            request.setValue(value, forHTTPHeaderField: name)
        }
        if let token {
            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
        if let body {
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            do {
                request.httpBody = try JSONEncoder().encode(body)
            } catch {
                throw APIFailure(error: .invalidRequest, requestID: requestID, status: 0)
            }
        }

        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await session.data(for: request)
        } catch {
            if let kind = Self.transportFailureKind(error) {
                hooks.report(APITransportFailure(path: path, requestID: requestID, kind: kind))
            }
            throw APIFailure(error: Self.mapTransportError(error), requestID: requestID, status: 0)
        }

        guard let http = response as? HTTPURLResponse else {
            throw APIFailure(error: .malformedResponse, requestID: requestID, status: 0)
        }
        let answeredID = Self.requestID(in: http, data: data) ?? requestID

        guard (200..<300).contains(http.statusCode) else {
            let envelope = Self.envelope(from: data)
            throw APIFailure(
                error: Self.mapServerError(status: http.statusCode, data: data, headers: http),
                requestID: answeredID,
                status: http.statusCode,
                code: envelope?.error.code,
                retryAfter: envelope?.error.details?.retryAfterSeconds
                    ?? Int(http.value(forHTTPHeaderField: "Retry-After") ?? "")
            )
        }

        if Response.self == Empty.self { return Empty() as! Response }

        do {
            return try JSONDecoder().decode(Response.self, from: data)
        } catch {
            throw APIFailure(error: .malformedResponse, requestID: answeredID, status: http.statusCode)
        }
    }

    /// Everything this client says about itself on one request.
    func headers(requestID: String) -> [String: String] {
        var headers = metadata.headers
        headers["Accept"] = "application/json"
        headers["X-Request-ID"] = requestID
        if let identity = hooks.identity {
            if let installationID = identity.installationID, !installationID.isEmpty {
                headers["X-Installation-ID"] = installationID
            }
            if let sessionID = identity.sessionID, !sessionID.isEmpty {
                headers["X-Session-ID"] = sessionID
            }
        }
        return headers
    }

    // MARK: Error mapping

    /// The error envelope every endpoint uses.
    struct ErrorEnvelope: Decodable {
        struct Payload: Decodable {
            let code: String
            let message: String?
            let details: Details?
            let requestID: String?

            enum CodingKeys: String, CodingKey {
                case code, message, details
                case requestID = "request_id"
            }
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

    static func envelope(from data: Data) -> ErrorEnvelope? {
        try? JSONDecoder().decode(ErrorEnvelope.self, from: data)
    }

    /// The id the server filed the request under: its `X-Request-ID` header,
    /// else the envelope's `request_id`. Nil when neither is a valid id.
    static func requestID(in response: HTTPURLResponse, data: Data) -> String? {
        if let header = response.value(forHTTPHeaderField: "X-Request-ID"), RequestID.isValid(header) {
            return header
        }
        if let fromBody = Self.envelope(from: data)?.error.requestID, RequestID.isValid(fromBody) {
            return fromBody
        }
        return nil
    }

    static func mapServerError(status: Int, data: Data, headers: HTTPURLResponse) -> APIError {
        let envelope = Self.envelope(from: data)
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
        case "DAILY_LIMIT_REACHED", "MONTHLY_LIMIT_REACHED":
            return .dailyLimitReached(limit: details?.dailyLimit ?? 0,
                                      usedToday: details?.usedToday ?? 0,
                                      resetsAt: details?.resetsAt)
        case "SUBSCRIPTION_EXPIRED":            return .subscriptionExpired
        case "PAYMENT_REQUIRED":                return .paymentRequired
        case "AI_PROVIDER_UNAVAILABLE":         return .providerUnavailable
        case "AI_TIMEOUT":                      return .providerTimeout
        case "AI_EMPTY_RESPONSE":               return .emptyResponse
        case "INVALID_REQUEST":
            if details?.field == "source_text", let limit = details?.maxCharacters, limit > 0 {
                return .sourceTooLong(limit: limit)
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

    /// How a request that got no response at all failed, for the app's
    /// `api_error` event. Nil for a cancellation, which is not a failure.
    static func transportFailureKind(_ error: Error) -> APITransportFailure.Kind? {
        if error is CancellationError { return nil }
        let nsError = error as NSError
        guard nsError.domain == NSURLErrorDomain else { return .io }
        switch nsError.code {
        case NSURLErrorCancelled:
            return nil
        case NSURLErrorTimedOut:
            return .timeout
        case NSURLErrorNotConnectedToInternet,
             NSURLErrorNetworkConnectionLost,
             NSURLErrorCannotFindHost,
             NSURLErrorCannotConnectToHost,
             NSURLErrorDNSLookupFailed,
             NSURLErrorDataNotAllowed,
             NSURLErrorInternationalRoamingOff,
             NSURLErrorCallIsActive:
            return .offline
        case NSURLErrorSecureConnectionFailed,
             NSURLErrorServerCertificateHasBadDate,
             NSURLErrorServerCertificateUntrusted,
             NSURLErrorServerCertificateHasUnknownRoot,
             NSURLErrorServerCertificateNotYetValid,
             NSURLErrorClientCertificateRejected,
             NSURLErrorClientCertificateRequired,
             NSURLErrorAppTransportSecurityRequiresSecureConnection:
            return .tls
        default:
            return .io
        }
    }
}
