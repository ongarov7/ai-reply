import CoreGraphics
import Foundation

/// Who wrote a recognised message. The app decides it from the screenshot's
/// layout and lets the person correct it; `unknown` is an honest "could not
/// tell", never a guess dressed up as a fact.
enum ScreenshotSenderRole: String, Codable, CaseIterable, Sendable {
    case me = "self"
    case other
    case unknown
}

/// One chat bubble read from a screenshot.
struct ScreenshotMessage: Codable, Equatable, Identifiable, Sendable {
    var id: String
    var role: ScreenshotSenderRole
    /// A name shown with the bubble (group chats), when one was read.
    var sender: String?
    var text: String
    /// The earlier message this bubble quotes, when it shows one.
    var quote: String?
    var time: String?
    /// The bubble on the screenshot, normalised to 0…1 with a top-left
    /// origin. Local only: it lets the composer highlight the bubble.
    var frame: CGRect?
    /// How sure the recogniser is of the text and of the sender, 0…1.
    var textConfidence: Double = 1
    var roleConfidence: Double = 1
    /// The person changed the text or the sender by hand.
    var isEdited: Bool = false

    /// Below this a sender or a text is shown as "please check".
    static let reviewThreshold = 0.6

    var needsReview: Bool {
        !isEdited && (roleConfidence < Self.reviewThreshold || textConfidence < 0.5 || role == .unknown)
    }
}

/// The visible part of a chat, oldest message first.
struct ScreenshotConversation: Codable, Equatable, Sendable {
    /// whatsapp | telegram | imessage | instagram | messenger | viber, only
    /// when the screenshot's look says so clearly.
    var app: String?
    /// The chat's name from the navigation bar, when it was read.
    var title: String?
    var messages: [ScreenshotMessage]
    /// The message the person chose to answer; nil — the other side's latest.
    var targetID: String?

    init(app: String? = nil, title: String? = nil, messages: [ScreenshotMessage] = [], targetID: String? = nil) {
        self.app = app
        self.title = title
        self.messages = messages
        self.targetID = targetID
    }

    var isEmpty: Bool { messages.allSatisfy { $0.text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty } }

    /// The message a reply answers: the chosen one, else the other side's
    /// latest, else the latest whose sender is unknown. Never the person's
    /// own message unless they chose it — the server applies the same rule.
    var target: ScreenshotMessage? {
        if let targetID, let chosen = messages.first(where: { $0.id == targetID }) { return chosen }
        for role in [ScreenshotSenderRole.other, .unknown] {
            if let last = messages.last(where: { $0.role == role }) { return last }
        }
        return nil
    }

    var needsReview: Bool { messages.contains(where: \.needsReview) }

    /// Characters of every text and quote (what the server limits).
    var characterCount: Int {
        messages.reduce(0) { $0 + $1.text.count + ($1.quote?.count ?? 0) }
    }

    /// A readable app name, or nil.
    var appName: String? {
        switch app {
        case "whatsapp":  return "WhatsApp"
        case "telegram":  return "Telegram"
        case "imessage":  return "Messages"
        case "instagram": return "Instagram"
        case "messenger": return "Messenger"
        case "viber":     return "Viber"
        default:          return nil
        }
    }

    /// The request body's `conversation`: what the server needs, no frames
    /// and no confidences. Empty texts are left out.
    var wire: Wire {
        Wire(app: app, title: title?.isEmpty == true ? nil : title,
             messages: messages.compactMap { message in
                 let text = message.text.trimmingCharacters(in: .whitespacesAndNewlines)
                 guard !text.isEmpty else { return nil }
                 let quote = message.quote?.trimmingCharacters(in: .whitespacesAndNewlines)
                 return Wire.Message(id: message.id, role: message.role.rawValue,
                                     sender: message.sender?.isEmpty == true ? nil : message.sender,
                                     text: text, quote: quote?.isEmpty == true ? nil : quote, time: message.time)
             },
             target_id: targetID)
    }

    struct Wire: Encodable, Equatable, Sendable {
        struct Message: Encodable, Equatable, Sendable {
            let id: String
            let role: String
            let sender: String?
            let text: String
            let quote: String?
            let time: String?
        }
        let app: String?
        let title: String?
        let messages: [Message]
        let target_id: String?
    }

    /// The server's limits (`config.screenshot`); a longer chat is trimmed
    /// from the oldest end, where the least relevant context is.
    static let maximumMessages = 60
    static let maximumMessageCharacters = 1500
    static let maximumTotalCharacters = 8000

    func trimmedToServerLimits() -> ScreenshotConversation {
        var out = self
        out.messages = out.messages.map { message in
            var message = message
            if message.text.count > Self.maximumMessageCharacters {
                message.text = String(message.text.prefix(Self.maximumMessageCharacters))
            }
            if let quote = message.quote, quote.count > 500 { message.quote = String(quote.prefix(500)) }
            return message
        }
        while out.messages.count > Self.maximumMessages || out.characterCount > Self.maximumTotalCharacters,
              out.messages.count > 1 {
            let removed = out.messages.removeFirst()
            if removed.id == out.targetID { out.targetID = nil }
        }
        return out
    }
}

/// How the screenshot reached AI Reply.
enum ScreenshotSource: String, Codable, Sendable {
    /// The AI Reply Screenshot shortcut (Control Center, Back Tap, Action button).
    case shortcut
    /// Picked in the app with the system photo picker.
    case photos
}

/// A reply written in the app for the latest screenshot, waiting in the
/// keyboard to be inserted.
struct ScreenshotDraft: Codable, Equatable, Sendable {
    var requestID: String
    var text: String
    var createdAt: Date
    /// Replies the server charged for it.
    var units: Int
}

/// The latest screenshot, shared by the app and the keyboard through the App
/// Group. One at a time: a new one replaces it.
struct ScreenshotContext: Codable, Equatable, Sendable {
    var id: String
    var createdAt: Date
    var source: ScreenshotSource
    /// Pixel size of the stored image (after a crop).
    var imageWidth: Int
    var imageHeight: Int
    var conversation: ScreenshotConversation
    /// The recogniser found no readable chat text.
    var hasNoText: Bool = false
    /// An instruction that came with it (the shortcut's own dictation).
    var instruction: String?
    var draft: ScreenshotDraft?

    /// How long a screenshot and its text stay on the iPhone (the privacy
    /// policy says "at most one hour").
    static let lifetime: TimeInterval = 60 * 60

    var expiresAt: Date { createdAt.addingTimeInterval(Self.lifetime) }

    func isFresh(now: Date = Date()) -> Bool {
        now < expiresAt && createdAt <= now.addingTimeInterval(60)
    }
}
