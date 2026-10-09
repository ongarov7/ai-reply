import Foundation

/// A screen a notification can open.
///
/// Хабарлама аша алатын экрандар. Тізімнен тыс мән — басты экран.
///
/// The same list the server validates links against (`domain.LinkScreens`).
/// "notifications" is the notifications part of Settings, "keyboard" the
/// keyboard setup guide.
enum AppScreen: String, CaseIterable, Sendable {
    case home, subscription, settings, notifications, templates, profile, keyboard, compose
}

/// Where a notification's `link` leads, after the app's own check.
enum DeepLinkDestination: Equatable, Sendable {
    /// One of the app's own screens.
    case screen(AppScreen)
    /// A page on ai-reply.kz, shown in an in-app browser.
    case web(URL)
    /// Nothing to open: the app just comes to the front.
    case none
}

/// Validates a link before the app acts on it.
///
/// Сілтеме тексеріледі: тек aireply:// экрандары және ai-reply.kz беттері.
///
/// The server already refuses anything else, but the app checks again and
/// never opens an arbitrary URL: a push is a message from the network, and a
/// link in it is data, not an instruction.
///
/// - `aireply://<screen>`: a known screen opens it; an unknown or missing
///   screen opens Home.
/// - `https://ai-reply.kz/...` or a subdomain, with no user info and no port:
///   the in-app browser.
/// - anything else, or no link at all: the app simply opens.
enum DeepLink {

    static let scheme = "aireply"
    static let webHost = "ai-reply.kz"
    /// The server's own limit on a link.
    static let maximumLength = 512

    static func destination(for raw: String?) -> DeepLinkDestination {
        guard let raw = raw?.trimmingCharacters(in: .whitespacesAndNewlines),
              !raw.isEmpty, raw.count <= maximumLength,
              let components = URLComponents(string: raw),
              let scheme = components.scheme?.lowercased() else {
            return .none
        }
        switch scheme {
        case Self.scheme:
            let screen = (components.host ?? "").lowercased()
            return .screen(AppScreen(rawValue: screen) ?? .home)
        case "https":
            guard components.user == nil, components.password == nil, components.port == nil,
                  let host = components.host?.lowercased(), isAllowedWebHost(host),
                  let url = components.url else {
                return .none
            }
            return .web(url)
        default:
            return .none
        }
    }

    /// ai-reply.kz itself or any of its subdomains. A look-alike such as
    /// `ai-reply.kz.example.com` or `evil-ai-reply.kz` is not one.
    static func isAllowedWebHost(_ host: String) -> Bool {
        host == webHost || host.hasSuffix("." + webHost)
    }
}

/// The part of a push the app reads when it is tapped.
///
/// FCM delivers the message's data as flat keys next to `aps`: `nid`
/// (notification), `did` (this delivery), `type`, `category`, `link`,
/// plus custom ones the app ignores. Anything missing or of the wrong type is
/// simply absent.
struct NotificationPayload: Equatable, Sendable {
    let notificationID: String?
    let deliveryID: String?
    let type: String?
    let category: String?
    let link: String?

    init(notificationID: String? = nil, deliveryID: String? = nil, type: String? = nil,
         category: String? = nil, link: String? = nil) {
        self.notificationID = notificationID
        self.deliveryID = deliveryID
        self.type = type
        self.category = category
        self.link = link
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
                  category: text("category"), link: text("link"))
    }

    var destination: DeepLinkDestination { DeepLink.destination(for: link) }
}
