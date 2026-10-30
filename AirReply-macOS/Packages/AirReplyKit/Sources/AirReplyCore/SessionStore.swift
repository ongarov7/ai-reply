import Foundation

/// The signed-in session as the server issued it.
public struct SessionTokens: Codable, Equatable, Sendable {
    public var accessToken: String
    /// Single use: the server rotates it on every refresh and revokes the whole
    /// session if an old one is ever sent again.
    public var refreshToken: String
    public var accessExpiresAt: Date
    public var refreshExpiresAt: Date?
    /// The device row the session is bound to (may differ from the id we sent).
    public var deviceID: String
    public var userID: String
    public var email: String?

    public init(accessToken: String, refreshToken: String, accessExpiresAt: Date, refreshExpiresAt: Date?,
                deviceID: String, userID: String, email: String?) {
        self.accessToken = accessToken
        self.refreshToken = refreshToken
        self.accessExpiresAt = accessExpiresAt
        self.refreshExpiresAt = refreshExpiresAt
        self.deviceID = deviceID
        self.userID = userID
        self.email = email
    }

    public func needsRefresh(at date: Date, leeway: TimeInterval = 60) -> Bool {
        accessExpiresAt.timeIntervalSince(date) < leeway
    }
}

/// Keeps the session and this Mac's device id in the keychain.
public struct SessionStore: Sendable {
    private let store: any SecureStore
    private static let sessionKey = "session"
    private static let deviceKey = "device-id"

    public init(store: any SecureStore) {
        self.store = store
    }

    public func load() -> SessionTokens? {
        guard let data = try? store.read(Self.sessionKey) else { return nil }
        return try? JSONDecoder.api.decode(SessionTokens.self, from: data)
    }

    public func save(_ tokens: SessionTokens) throws {
        try store.write(try JSONEncoder.api.encode(tokens), for: Self.sessionKey)
    }

    public func clear() {
        try? store.delete(Self.sessionKey)
    }

    /// A random id for this installation, created once. Never derived from hardware.
    public func deviceID() -> String {
        if let data = try? store.read(Self.deviceKey), let id = String(data: data, encoding: .utf8), !id.isEmpty {
            return id
        }
        let id = UUID().uuidString.lowercased()
        try? store.write(Data(id.utf8), for: Self.deviceKey)
        return id
    }

    public func rememberDeviceID(_ id: String) {
        guard !id.isEmpty else { return }
        try? store.write(Data(id.utf8), for: Self.deviceKey)
    }
}

extension JSONDecoder {
    /// Server JSON: RFC 3339 dates, with or without fractional seconds.
    public static let api: JSONDecoder = {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .custom { decoder in
            let container = try decoder.singleValueContainer()
            let raw = try container.decode(String.self)
            if let date = RFC3339.parse(raw) { return date }
            throw DecodingError.dataCorruptedError(in: container, debugDescription: "Not an RFC 3339 date: \(raw)")
        }
        return decoder
    }()
}

extension JSONEncoder {
    public static let api: JSONEncoder = {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .custom { date, encoder in
            var container = encoder.singleValueContainer()
            try container.encode(RFC3339.format(date))
        }
        return encoder
    }()
}

enum RFC3339 {
    static func parse(_ raw: String) -> Date? {
        if let date = try? Date(raw, strategy: .iso8601) { return date }
        return try? Date(raw, strategy: Date.ISO8601FormatStyle(includingFractionalSeconds: true))
    }

    static func format(_ date: Date) -> String {
        date.formatted(.iso8601)
    }
}
