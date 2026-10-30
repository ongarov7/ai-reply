import Foundation

/// A screen a notification can open.
///
/// Хабарлама аша алатын экрандар. Тізімнен тыс мән — басты экран.
///
/// The same list the server validates links against (`domain.LinkScreens`).
/// "notifications" is the notifications part of Settings; "keyboard" and
/// "permissions" both open «Настройка клавиатуры и разрешений»;
/// "onboarding" shows the guide over Home; "purchase" opens native StoreKit
/// purchases when available; "support" opens
/// «Чат с оператором» on a server with the chat, Home otherwise.
enum AppScreen: String, CaseIterable, Sendable {
    case home, subscription, settings, notifications, templates, profile, keyboard, compose
    case onboarding, permissions
    case pasteHelp = "paste_help"
    case support, purchase
}

/// A checked destination. Website links are information, legal or support only.
enum DeepLinkDestination: Equatable, Sendable {
    case screen(AppScreen)
    case web(URL)
    case none
}

/// Network links are data. A server cannot turn an ordinary web notification
/// into a checkout route or send this app to an arbitrary site.
enum DeepLink {
    static let scheme = "aireply"
    static let webHost = "ai-reply.kz"
    static let maximumLength = 512

    private static let informationPaths: Set<String> = [
        "/app", "/offer", "/terms", "/privacy", "/support", "/account/delete",
        "/sandbox/app", "/sandbox/offer", "/sandbox/terms", "/sandbox/privacy", "/sandbox/support", "/sandbox/account/delete"
    ]

    static func destination(for raw: String?) -> DeepLinkDestination {
        guard let components = components(for: raw), let scheme = components.scheme?.lowercased() else { return .none }
        switch scheme {
        case Self.scheme:
            return .screen(AppScreen(rawValue: (components.host ?? "").lowercased()) ?? .home)
        case "https":
            guard let url = validatedURL(components, paths: informationPaths) else { return .none }
            return .web(url)
        default:
            return .none
        }
    }

    /// Information-only page for this build's canonical backend environment.
    static func serviceInformationDestination(backendBaseURL: URL, locale: String) -> DeepLinkDestination {
        guard var components = URLComponents(url: backendBaseURL, resolvingAgainstBaseURL: false),
              components.scheme == "https", components.host == webHost,
              components.port == nil, components.user == nil, components.password == nil,
              components.query == nil, components.fragment == nil,
              ["", "/", "/sandbox", "/sandbox/"].contains(components.percentEncodedPath),
              ["ru", "kk", "en", "uz"].contains(locale) else { return .none }
        components.path = components.path.hasPrefix("/sandbox") ? "/sandbox/app" : "/app"
        components.queryItems = [URLQueryItem(name: "lang", value: locale)]
        return destination(for: components.url?.absoluteString)
    }

    static func isAllowed(_ destination: DeepLinkDestination) -> Bool {
        switch destination {
        case .none: return false
        case .screen: return true
        case .web(let url): return Self.destination(for: url.absoluteString) == destination
        }
    }

    static func isAllowedWebHost(_ host: String) -> Bool { host.lowercased() == webHost }

    private static func components(for raw: String?) -> URLComponents? {
        guard let raw = raw?.trimmingCharacters(in: .whitespacesAndNewlines),
              !raw.isEmpty, raw.count <= maximumLength else { return nil }
        return URLComponents(string: raw)
    }

    private static func validatedURL(_ components: URLComponents, paths: Set<String>) -> URL? {
        guard components.scheme?.lowercased() == "https",
              components.user == nil, components.password == nil, components.port == nil,
              let host = components.host, isAllowedWebHost(host), components.fragment == nil,
              paths.contains(components.percentEncodedPath) else { return nil }
        // The language is the sole supported argument. Redirects, embedded
        // credentials, percent-encoded paths and checkout arguments are refused.
        if let items = components.queryItems {
            guard items.count == 1, let item = items.first, item.name == "lang",
                  let value = item.value, ["ru", "kk", "en", "uz"].contains(value) else { return nil }
        }
        return components.url
    }
}

/// The part of a push the app reads when it is tapped.
///
/// FCM delivers the message's data as flat keys next to `aps`: `nid`
/// (notification), `did` (this delivery), `type`, `category`, `link`,
/// `conversation_id` (an operator's reply), plus custom ones the app
/// ignores. Anything missing or of the wrong type is simply absent.
struct NotificationPayload: Equatable, Sendable {
    let notificationID: String?
    let deliveryID: String?
    let type: String?
    let category: String?
    let link: String?
    /// The support conversation an operator answered in.
    let conversationID: String?

    static let supportReplyType = "support_reply"
    static let webTopupOfferType = "web_topup_offer"

    init(notificationID: String? = nil, deliveryID: String? = nil, type: String? = nil,
         category: String? = nil, link: String? = nil, conversationID: String? = nil) {
        self.notificationID = notificationID
        self.deliveryID = deliveryID
        self.type = type
        self.category = category
        self.link = link
        self.conversationID = conversationID
    }

    init(userInfo: [AnyHashable: Any]) {
        func text(_ key: String) -> String? {
            switch userInfo[key] {
            case let value as String:
                let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
                return trimmed.isEmpty ? nil : trimmed
            case let value as NSNumber:
                return value.stringValue
            default:
                return nil
            }
        }
        self.init(notificationID: text("nid"), deliveryID: text("did"), type: text("type"),
                  category: text("category"), link: text("link"), conversationID: text("conversation_id"))
    }

    /// An operator answered: the chat, whatever the link says.
    var isSupportReply: Bool { type == Self.supportReplyType || conversationID != nil }

    var isWebTopupOffer: Bool { type == Self.webTopupOfferType }

    var destination: DeepLinkDestination {
        // Previously delivered website-payment offers stay inert too.
        if isWebTopupOffer { return .none }
        if isSupportReply { return .screen(.support) }
        return DeepLink.destination(for: link)
    }
}
