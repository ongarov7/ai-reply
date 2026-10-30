import Foundation
@testable import AirReplyCore

/// Scripted backend: each request is answered by the handler and recorded.
final class FakeTransport: HTTPTransport, @unchecked Sendable {
    typealias Handler = @Sendable (URLRequest) async throws -> (Int, String, [String: String])

    private let lock = NSLock()
    private var recorded: [URLRequest] = []
    private let handler: Handler

    init(_ handler: @escaping Handler) {
        self.handler = handler
    }

    var requests: [URLRequest] { lock.withLock { recorded } }

    func requests(to path: String) -> [URLRequest] {
        requests.filter { $0.url?.path.hasSuffix(path) == true }
    }

    func send(_ request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        lock.withLock { recorded.append(request) }
        let (status, body, headers) = try await handler(request)
        let response = HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: "HTTP/1.1", headerFields: headers)!
        return (Data(body.utf8), response)
    }
}

enum Fixtures {
    static let configuration = AppConfiguration(backendBaseURL: URL(string: "https://ai-reply.kz/sandbox")!,
                                                appVersion: "1.0.0", build: "7", osVersion: "26.6.2")

    static func session(access: String = "access-1", refresh: String = "refresh-1", expiresIn: Int = 900,
                        consent: Bool = true) -> String {
        """
        {"access_token":"\(access)","refresh_token":"\(refresh)","token_type":"Bearer","expires_in":\(expiresIn),
         "refresh_expires_at":"2026-11-30T09:15:02Z","device_id":"server-device","is_new_user":false,
         "user":{"id":"u-1","email":"aigerim@mail.kz","status":"active","locale":"ru","created_at":"2026-10-31T09:15:02Z",
                 "onboarding_completed":false,"auth_providers":["email"],"preferred_language":""},
         "subscription":\(subscription),
         "usage":\(usage())\(consent ? ",\"legal_consent\":\(legalConsent)" : "")}
        """
    }

    static let subscription = """
        {"status":"active","plan":{"id":"p","code":"base_monthly","name":{"en":"Base","kk":"Base","ru":"Base","uz":"Base"},
         "description":{"en":"","kk":"","ru":"","uz":""},"price":59900,"price_text":"599 KZT","currency":"KZT",
         "daily_message_limit":30,"monthly_message_limit":0,"period_days":30,"is_free":false,"sort_order":20,
         "entitlement_kind":"daily","purchasable":false},"started_at":"2026-10-20T09:00:00Z","source":"payment"}
        """

    static func usage(remainingTotal: Int = 37, unlimited: Bool = false) -> String {
        """
        {"daily_limit":30,"unlimited":\(unlimited),"purchased_credits":30,"admin_credits":0,"used_today":23,"remaining_today":7,
         "monthly_limit":0,"used_month":40,"resets_at":"2026-10-31T19:00:00Z","timezone":"Asia/Almaty",
         "welcome":{"total":10,"remaining":0},"credits":30,"remaining_total":\(remainingTotal)}
        """
    }

    static let legalConsent = """
        {"terms_version":"2026-10-08","privacy_version":"2026-10-14","accepted_at":"2026-10-20T09:01:00Z","locale":"ru","platform":"ios"}
        """

    static let config = """
        {"default_locale":"en","demo_mode":false,"features":{"compose":true,"voice_transcription":true,"credits":true},
         "legal":{"account_deletion_url":"https://ai-reply.kz/account/delete","ai_provider":"OpenAI","contact_email":"",
                  "privacy_url":"https://ai-reply.kz/privacy","privacy_version":"2026-10-14","support_url":"https://ai-reply.kz/support",
                  "terms_url":"https://ai-reply.kz/offer","terms_version":"2026-10-08"},
         "locales":["kk","ru","en","uz"],"max_instruction_length":400,"max_source_characters":400,"payment_mode":"off",
         "timezone":"Asia/Almaty"}
        """

    static func error(_ code: String, details: String = "{}") -> String {
        "{\"error\":{\"code\":\"\(code)\",\"message\":\"x\",\"details\":\(details)}}"
    }

    static func tokens(access: String = "access-1", refresh: String = "refresh-1", expiresAt: Date) -> SessionTokens {
        SessionTokens(accessToken: access, refreshToken: refresh, accessExpiresAt: expiresAt, refreshExpiresAt: nil,
                      deviceID: "device-1", userID: "u-1", email: "aigerim@mail.kz")
    }

    static func bodyJSON(_ request: URLRequest) -> [String: Any] {
        guard let data = request.httpBody, let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            return [:]
        }
        return object
    }
}

/// A clock tests can move.
final class TestClock: @unchecked Sendable {
    private let lock = NSLock()
    private var current: Date

    init(_ start: Date = Date(timeIntervalSince1970: 1_800_000_000)) {
        current = start
    }

    var now: Date { lock.withLock { current } }

    func advance(_ seconds: TimeInterval) {
        lock.withLock { current = current.addingTimeInterval(seconds) }
    }
}

/// Collects session events from the client.
final class EventLog: @unchecked Sendable {
    private let lock = NSLock()
    private var events: [String] = []

    func record(_ event: SessionEvent) {
        lock.withLock {
            switch event {
            case .refreshed: events.append("refreshed")
            case .signedOut: events.append("signedOut")
            }
        }
    }

    var all: [String] { lock.withLock { events } }
}
