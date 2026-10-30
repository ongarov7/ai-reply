import Foundation
import os

/// One call to the AI Reply backend.
public struct APIRequest: Sendable {
    public enum Body: Sendable {
        case none
        case json(Data)
        case multipart(MultipartForm)
    }

    public var method: String
    /// Path under the backend base, e.g. "/api/v1/me".
    public var path: String
    public var body: Body
    public var authenticated: Bool
    public var idempotencyKey: String?
    public var timeout: TimeInterval?

    public init(_ method: String, _ path: String, body: Body = .none, authenticated: Bool = true,
                idempotencyKey: String? = nil, timeout: TimeInterval? = nil) {
        self.method = method
        self.path = path
        self.body = body
        self.authenticated = authenticated
        self.idempotencyKey = idempotencyKey
        self.timeout = timeout
    }

    public static func json<T: Encodable>(_ method: String, _ path: String, _ payload: T, authenticated: Bool = true,
                                          idempotencyKey: String? = nil, timeout: TimeInterval? = nil) -> APIRequest {
        let data = (try? JSONEncoder.api.encode(payload)) ?? Data("{}".utf8)
        return APIRequest(method, path, body: .json(data), authenticated: authenticated,
                          idempotencyKey: idempotencyKey, timeout: timeout)
    }
}

/// What the client tells the app about the session.
public enum SessionEvent: Sendable {
    /// A refresh returned the account state (same shape as sign-in).
    case refreshed(SessionPayload)
    /// The session ended: refresh refused, account disabled, or local sign-out.
    case signedOut
}

/// Talks to the backend: adds the bearer token, refreshes it once when it
/// expires, and turns the error envelope into `APIError`. Refresh is single-flight:
/// the refresh token is single-use and a reused one revokes the whole session.
public actor APIClient {
    private let configuration: AppConfiguration
    private let transport: any HTTPTransport
    private let sessions: SessionStore
    private let now: @Sendable () -> Date
    private let languageCode: @Sendable () -> String
    private var tokens: SessionTokens?
    private var refreshTask: Task<SessionTokens, any Error>?
    private var onEvent: (@Sendable (SessionEvent) -> Void)?
    private let log = Logger(subsystem: "kz.ai-reply.desktop", category: "api")

    public init(configuration: AppConfiguration, transport: any HTTPTransport, sessions: SessionStore,
                now: @escaping @Sendable () -> Date = Date.init,
                languageCode: @escaping @Sendable () -> String = { APIClient.preferredLanguage() }) {
        self.configuration = configuration
        self.transport = transport
        self.sessions = sessions
        self.now = now
        self.languageCode = languageCode
        tokens = sessions.load()
    }

    public func setEventHandler(_ handler: @escaping @Sendable (SessionEvent) -> Void) {
        onEvent = handler
    }

    public var session: SessionTokens? { tokens }

    public var deviceDescriptor: DeviceDescriptor {
        DeviceDescriptor(deviceID: tokens?.deviceID ?? sessions.deviceID(), configuration: configuration,
                         locale: languageCode())
    }

    /// Stores a session from sign-in.
    public func install(_ payload: SessionPayload) throws {
        let issued = payload.tokens(issuedAt: now())
        try sessions.save(issued)
        sessions.rememberDeviceID(issued.deviceID)
        tokens = issued
    }

    /// Forgets the session on this Mac (the server call is the caller's business).
    public func clearSession() {
        refreshTask?.cancel()
        refreshTask = nil
        tokens = nil
        sessions.clear()
        onEvent?(.signedOut)
    }

    public func send<T: Decodable & Sendable>(_ request: APIRequest, as type: T.Type) async throws(APIError) -> T {
        let data = try await send(request)
        do {
            return try JSONDecoder.api.decode(T.self, from: data)
        } catch {
            log.error("undecodable response for \(request.path, privacy: .public)")
            throw APIError.invalidResponse(status: 200)
        }
    }

    @discardableResult
    public func send(_ request: APIRequest) async throws(APIError) -> Data {
        guard request.authenticated else { return try await perform(request, accessToken: nil) }
        guard var current = tokens else { throw APIError.signedOut }
        if current.needsRefresh(at: now()) {
            current = try await refreshed(replacing: current.accessToken)
        }
        do {
            return try await perform(request, accessToken: current.accessToken)
        } catch let error where Self.isExpiredSession(error) {
            let renewed = try await refreshed(replacing: current.accessToken)
            return try await perform(request, accessToken: renewed.accessToken)
        }
    }

    private static func isExpiredSession(_ error: APIError) -> Bool {
        guard case let .server(status, code, _, _, _) = error, status == 401 else { return false }
        return code == APIErrorCode.unauthorized || code == APIErrorCode.tokenExpired
    }

    /// Returns tokens newer than `replacing`, refreshing at most once at a time.
    private func refreshed(replacing staleAccess: String) async throws(APIError) -> SessionTokens {
        if let current = tokens, current.accessToken != staleAccess, !current.needsRefresh(at: now()) {
            return current
        }
        if refreshTask == nil {
            guard let current = tokens else { throw APIError.signedOut }
            refreshTask = Task { try await self.performRefresh(current) }
        }
        let task = refreshTask!
        do {
            let result = try await task.value
            if refreshTask == task { refreshTask = nil }
            return result
        } catch {
            if refreshTask == task { refreshTask = nil }
            throw (error as? APIError) ?? .offline
        }
    }

    private func performRefresh(_ current: SessionTokens) async throws -> SessionTokens {
        struct Body: Encodable {
            let refresh_token: String
            let device: DeviceDescriptor
        }
        let request = APIRequest.json("POST", "/api/v1/auth/refresh",
                                      Body(refresh_token: current.refreshToken, device: deviceDescriptor),
                                      authenticated: false)
        let data: Data
        do {
            data = try await perform(request, accessToken: nil)
        } catch let error {
            if case let .server(status, _, _, _, _) = error, status == 401 || status == 403 {
                log.notice("session ended by the server (\(status, privacy: .public))")
                tokens = nil
                sessions.clear()
                onEvent?(.signedOut)
                throw APIError.signedOut
            }
            throw error
        }
        guard let payload = try? JSONDecoder.api.decode(SessionPayload.self, from: data) else {
            throw APIError.invalidResponse(status: 200)
        }
        let renewed = payload.tokens(issuedAt: now(), fallbackEmail: current.email)
        // Persist before anything uses it: the old refresh token is already dead on the server.
        try? sessions.save(renewed)
        tokens = renewed
        onEvent?(.refreshed(payload))
        return renewed
    }

    private func perform(_ request: APIRequest, accessToken: String?) async throws(APIError) -> Data {
        let requestID = UUID().uuidString.lowercased()
        var urlRequest = URLRequest(url: url(for: request.path))
        urlRequest.httpMethod = request.method
        urlRequest.timeoutInterval = request.timeout ?? 30
        urlRequest.setValue("application/json", forHTTPHeaderField: "Accept")
        urlRequest.setValue(languageCode(), forHTTPHeaderField: "Accept-Language")
        urlRequest.setValue(userAgent, forHTTPHeaderField: "User-Agent")
        urlRequest.setValue(requestID, forHTTPHeaderField: "X-Request-ID")
        if let accessToken {
            urlRequest.setValue("Bearer \(accessToken)", forHTTPHeaderField: "Authorization")
        }
        if let key = request.idempotencyKey {
            urlRequest.setValue(key, forHTTPHeaderField: "Idempotency-Key")
        }
        switch request.body {
        case .none:
            break
        case let .json(data):
            urlRequest.setValue("application/json", forHTTPHeaderField: "Content-Type")
            urlRequest.httpBody = data
        case let .multipart(form):
            urlRequest.setValue(form.contentType, forHTTPHeaderField: "Content-Type")
            urlRequest.httpBody = form.encoded()
        }

        let data: Data
        let response: HTTPURLResponse
        do {
            (data, response) = try await transport.send(urlRequest)
        } catch let error as URLError where error.code == .cancelled {
            throw APIError.cancelled
        } catch is CancellationError {
            throw APIError.cancelled
        } catch {
            log.notice("\(request.method, privacy: .public) \(request.path, privacy: .public) failed: offline [\(requestID, privacy: .public)]")
            throw APIError.offline
        }
        guard (200..<300).contains(response.statusCode) else {
            let failure = Self.decodeFailure(data: data, response: response)
            log.notice("\(request.method, privacy: .public) \(request.path, privacy: .public) → \(response.statusCode, privacy: .public) \(failure.code ?? "-", privacy: .public) [\(requestID, privacy: .public)]")
            throw failure
        }
        return data
    }

    private static func decodeFailure(data: Data, response: HTTPURLResponse) -> APIError {
        struct Envelope: Decodable {
            struct Body: Decodable {
                let code: String
                let message: String?
                let details: [String: JSONValue]?
            }
            let error: Body
        }
        guard let envelope = try? JSONDecoder.api.decode(Envelope.self, from: data) else {
            return .invalidResponse(status: response.statusCode)
        }
        let details = envelope.error.details ?? [:]
        let header = response.value(forHTTPHeaderField: "Retry-After").flatMap(TimeInterval.init)
        let retryAfter = header ?? details["retry_after_seconds"]?.intValue.map(TimeInterval.init)
        return .server(status: response.statusCode, code: envelope.error.code, message: envelope.error.message ?? "",
                       details: details, retryAfter: retryAfter)
    }

    private func url(for path: String) -> URL {
        var components = URLComponents(url: configuration.backendBaseURL, resolvingAgainstBaseURL: false)!
        let base = components.path.hasSuffix("/") ? String(components.path.dropLast()) : components.path
        components.path = base + path
        return components.url!
    }

    private var userAgent: String {
        "AIReply-macOS/\(configuration.appVersion) (build \(configuration.build); macOS \(configuration.osVersion))"
    }

    /// The app's interface language as the server knows it: kk, ru, en (else ru).
    public static func preferredLanguage() -> String {
        let code = Bundle.main.preferredLocalizations.first.map { String($0.prefix(2)).lowercased() } ?? "ru"
        return ["kk", "ru", "en"].contains(code) ? code : "ru"
    }
}

/// The `device` object sent on sign-in and refresh.
public struct DeviceDescriptor: Encodable, Sendable, Equatable {
    public let device_id: String
    public let platform: String
    public let app_version: String
    public let os_version: String
    public let model: String
    public let locale: String
    public let timezone: String

    public init(deviceID: String, configuration: AppConfiguration, locale: String, model: String = DeviceDescriptor.hardwareModel(),
                timezone: String = TimeZone.current.identifier) {
        device_id = deviceID
        platform = configuration.platform
        app_version = configuration.appVersion
        os_version = configuration.osVersion
        self.model = model
        self.locale = locale
        self.timezone = timezone
    }

    /// e.g. "Mac15,3"; only the model family, nothing that identifies the machine.
    public static func hardwareModel() -> String {
        var size = 0
        sysctlbyname("hw.model", nil, &size, nil, 0)
        guard size > 0 else { return "" }
        var buffer = [UInt8](repeating: 0, count: size)
        sysctlbyname("hw.model", &buffer, &size, nil, 0)
        return String(decoding: buffer.prefix { $0 != 0 }, as: UTF8.self)
    }
}

/// multipart/form-data body.
public struct MultipartForm: Sendable {
    public struct File: Sendable {
        public let name: String
        public let fileName: String
        public let contentType: String
        public let data: Data
    }

    public let boundary: String
    public private(set) var fields: [(String, String)] = []
    public private(set) var files: [File] = []

    public init(boundary: String = "AIReply-" + UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()) {
        self.boundary = boundary
    }

    public var contentType: String { "multipart/form-data; boundary=\(boundary)" }

    public mutating func add(_ name: String, _ value: String) {
        fields.append((name, value))
    }

    public mutating func addFile(_ name: String, fileName: String, contentType: String, data: Data) {
        files.append(File(name: name, fileName: fileName, contentType: contentType, data: data))
    }

    public func encoded() -> Data {
        var body = Data()
        func line(_ string: String) { body.append(Data((string + "\r\n").utf8)) }
        for (name, value) in fields {
            line("--\(boundary)")
            line("Content-Disposition: form-data; name=\"\(name)\"")
            line("Content-Type: text/plain; charset=utf-8")
            line("")
            line(value)
        }
        for file in files {
            line("--\(boundary)")
            line("Content-Disposition: form-data; name=\"\(file.name)\"; filename=\"\(file.fileName)\"")
            line("Content-Type: \(file.contentType)")
            line("")
            body.append(file.data)
            line("")
        }
        line("--\(boundary)--")
        return body
    }
}
