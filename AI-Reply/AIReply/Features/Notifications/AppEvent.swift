import Foundation

/// The app events this client sends, and nothing else.
///
/// Жіберілетін оқиғалар тізімі: хабарлама мәтіні, пернелер, буфер — ешқашан.
///
/// A closed enum rather than a name and a dictionary, so the only events that
/// can exist are the ones listed here, each with the typed properties the
/// server's allowlist expects (`internal/telemetry/events.go`). There is no
/// case that could carry message text, anything typed or the clipboard, and
/// the keyboard extension does not compile this file at all.
///
/// Deliberately minimal: sign-in, registration and payment outcomes are
/// recorded by the server itself, so they are not sent from here.
enum AppEvent: Equatable, Sendable {
    case appOpened(coldStart: Bool)
    case appBackgrounded(foregroundSeconds: Int)
    case logout
    /// Only for a failure inside Apple's or Google's own sheet, which the
    /// server never saw.
    case loginFailed(method: LoginMethod, errorCode: String)
    case pushPermissionRequested
    case pushPermissionGranted(NotificationPermission)
    case pushPermissionDenied
    case pushTokenRegistered
    case pushTokenRefreshed
    case pushTokenRegistrationFailed(errorCode: String, requestID: String?)
    case notificationOpened(notificationID: String?, deliveryID: String?, type: String?)
    /// A request that got no HTTP response at all: `status` is always 0.
    case apiError(route: String, errorCode: String, requestID: String)

    enum LoginMethod: String, Sendable {
        case email, google, apple
    }

    var name: String {
        switch self {
        case .appOpened:                   return "app_opened"
        case .appBackgrounded:             return "app_backgrounded"
        case .logout:                      return "logout"
        case .loginFailed:                 return "login_failed"
        case .pushPermissionRequested:     return "push_permission_requested"
        case .pushPermissionGranted:       return "push_permission_granted"
        case .pushPermissionDenied:        return "push_permission_denied"
        case .pushTokenRegistered:         return "push_token_registered"
        case .pushTokenRefreshed:          return "push_token_refreshed"
        case .pushTokenRegistrationFailed: return "push_token_registration_failed"
        case .notificationOpened:          return "notification_opened"
        case .apiError:                    return "api_error"
        }
    }

    /// Properties as the server accepts them. A value that would fail the
    /// server's check is left out rather than sent: one bad property makes
    /// the server refuse the whole event.
    var properties: [String: EventValue] {
        var properties: [String: EventValue] = [:]
        func code(_ key: String, _ value: String?) {
            if let value, Self.isCode(value) { properties[key] = .string(value) }
        }
        switch self {
        case .appOpened(let coldStart):
            properties["cold_start"] = .bool(coldStart)
        case .appBackgrounded(let seconds):
            properties["foreground_seconds"] = .int(min(max(seconds, 0), 1_000_000_000))
        case .logout, .pushPermissionRequested, .pushPermissionDenied:
            break
        case .loginFailed(let method, let errorCode):
            properties["method"] = .string(method.rawValue)
            code("error_code", errorCode)
        case .pushPermissionGranted(let permission):
            switch permission {
            case .authorized, .provisional, .ephemeral:
                properties["status"] = .string(permission.rawValue)
            default:
                break
            }
        case .pushTokenRegistered, .pushTokenRefreshed:
            properties["provider"] = .string("apns")
        case .pushTokenRegistrationFailed(let errorCode, let requestID):
            properties["provider"] = .string("apns")
            code("error_code", errorCode)
            code("request_id", requestID)
        case .notificationOpened(let notificationID, let deliveryID, let type):
            code("notification_id", notificationID)
            code("delivery_id", deliveryID)
            code("type", type)
        case .apiError(let route, let errorCode, let requestID):
            if Self.isRoute(route) { properties["route"] = .string(route) }
            properties["status"] = .int(0)
            code("error_code", errorCode)
            code("request_id", requestID)
        }
        return properties
    }

    // MARK: The server's value patterns

    /// `^[A-Za-z0-9_.:-]{1,64}$` - codes and ids.
    static func isCode(_ value: String) -> Bool {
        matches(value, maxLength: 64, extra: "_.:-")
    }

    /// `^[A-Za-z0-9_/{}.:-]{1,128}$` - an API route.
    static func isRoute(_ value: String) -> Bool {
        matches(value, maxLength: 128, extra: "_/{}.:-")
    }

    private static func matches(_ value: String, maxLength: Int, extra: String) -> Bool {
        guard (1...maxLength).contains(value.count) else { return false }
        return value.unicodeScalars.allSatisfy { scalar in
            switch scalar {
            case "A"..."Z", "a"..."z", "0"..."9": return true
            default: return extra.unicodeScalars.contains(scalar)
            }
        }
    }

    /// A request path as a route: ids become `{id}`, so no identifier of a
    /// payment, installation or anything else ends up in an event.
    /// "api/v1/payments/8f1c…/confirm" → "/api/v1/payments/{id}/confirm".
    static func route(forPath path: String) -> String {
        let segments = path.split(separator: "/", omittingEmptySubsequences: true).map { segment -> String in
            let text = String(segment)
            let isVersion = text.first == "v" && text.dropFirst().allSatisfy(\.isNumber) && text.count > 1
            if isVersion { return text }
            return text.contains(where: \.isNumber) ? "{id}" : text
        }
        return "/" + segments.joined(separator: "/")
    }
}

/// One property value on the wire.
enum EventValue: Equatable, Sendable, Encodable {
    case bool(Bool)
    case int(Int)
    case string(String)

    func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        switch self {
        case .bool(let value):   try container.encode(value)
        case .int(let value):    try container.encode(value)
        case .string(let value): try container.encode(value)
        }
    }
}
