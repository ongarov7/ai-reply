import Foundation

/// The chat with an operator, as the server sends it (contract §3.7).
///
/// Оператормен чат: сервердің деректері, оператордың аты ешқашан келмейді.
enum SupportAPI {

    /// The account's one conversation; nil until its first message.
    struct Conversation: Decodable, Equatable, Sendable {
        let id: String
        /// open | resolved
        let status: String
        /// Operator messages the user has not read.
        let unreadCount: Int

        enum CodingKeys: String, CodingKey {
            case id, status
            case unreadCount = "unread_count"
        }
    }

    struct Message: Decodable, Equatable, Sendable, Identifiable {
        let id: String
        /// user | operator. Who of the operators wrote is never said.
        let sender: String
        let text: String
        /// RFC 3339.
        let createdAt: String
        /// When the user read it; operator messages only.
        let readAt: String?

        enum CodingKeys: String, CodingKey {
            case id, sender, text
            case createdAt = "created_at"
            case readAt = "read_at"
        }

        var isFromUser: Bool { sender == "user" }
    }

    /// Always oldest first.
    struct Page: Decodable, Equatable, Sendable {
        let messages: [Message]
        /// More messages exist in the direction asked.
        let hasMore: Bool

        enum CodingKeys: String, CodingKey {
            case messages
            case hasMore = "has_more"
        }
    }

    /// Which page to read.
    enum Cursor: Hashable, Sendable {
        /// The newest page.
        case latest
        /// Older than this message.
        case before(String)
        /// Newer than this message: what arrived since.
        case after(String)
    }
}

/// What the chat needs from the server. Throws `APIError`.
protocol SupportTransport: Sendable {
    func conversation() async throws -> SupportAPI.Conversation?
    func messages(_ cursor: SupportAPI.Cursor, limit: Int) async throws -> SupportAPI.Page
    /// `clientMessageID` makes a retry of the same message idempotent.
    func send(text: String, clientMessageID: String) async throws -> SupportAPI.Message
    /// Marks operator messages up to this one read; the unread count left.
    func markRead(upTo messageID: String) async throws -> Int
}

/// `/api/v1/support/*`, with the session's refresh-once-and-retry.
struct BackendSupportTransport: SupportTransport {

    private let session: AccountSession

    init(session: AccountSession = .shared) {
        self.session = session
    }

    private static func client() throws -> APIClient {
        try NotificationsAPI.client()
    }

    func conversation() async throws -> SupportAPI.Conversation? {
        struct Envelope: Decodable, Sendable { let conversation: SupportAPI.Conversation? }
        let envelope: Envelope = try await session.authenticated { token in
            try await Self.client().get("api/v1/support/conversation", token: token)
        }
        return envelope.conversation
    }

    func messages(_ cursor: SupportAPI.Cursor, limit: Int) async throws -> SupportAPI.Page {
        var query = [URLQueryItem(name: "limit", value: String(limit))]
        switch cursor {
        case .latest:             break
        case .before(let id):     query.append(URLQueryItem(name: "before", value: id))
        case .after(let id):      query.append(URLQueryItem(name: "after", value: id))
        }
        let items = query
        return try await session.authenticated { token in
            try await Self.client().get("api/v1/support/messages", query: items, token: token)
        }
    }

    func send(text: String, clientMessageID: String) async throws -> SupportAPI.Message {
        struct Request: Encodable, Sendable {
            let text: String
            let client_message_id: String
        }
        struct Response: Decodable, Sendable { let message: SupportAPI.Message }
        let request = Request(text: text, client_message_id: clientMessageID)
        let response: Response = try await session.authenticated { token in
            try await Self.client().post("api/v1/support/messages", body: request, token: token)
        }
        return response.message
    }

    func markRead(upTo messageID: String) async throws -> Int {
        struct Request: Encodable, Sendable { let up_to_message_id: String }
        struct Response: Decodable, Sendable {
            let unreadCount: Int
            enum CodingKeys: String, CodingKey { case unreadCount = "unread_count" }
        }
        let request = Request(up_to_message_id: messageID)
        let response: Response = try await session.authenticated { token in
            try await Self.client().post("api/v1/support/read", body: request, token: token)
        }
        return response.unreadCount
    }
}
