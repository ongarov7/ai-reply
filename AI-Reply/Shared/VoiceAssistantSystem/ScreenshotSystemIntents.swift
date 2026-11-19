import AppIntents
import Foundation

extension Notification.Name {
    static let screenshotReplyOpenRequested = Notification.Name("AIReply.screenshot.open")
}

/// A short-lived, content-free marker that survives a cold app launch: the
/// person opened the screenshot screen (Control Center, a shortcut).
enum ScreenshotLaunchRequest {
    private static var url: URL? {
        FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: AppGroup.identifier)?
            .appendingPathComponent("screenshot-open.json")
    }

    static func request(now: Date = Date()) {
        guard let url, let data = try? JSONEncoder().encode(now) else { return }
        try? data.write(to: url, options: [.atomic, .completeFileProtectionUnlessOpen])
    }

    static func consume(now: Date = Date()) -> Bool {
        guard let url, let data = try? Data(contentsOf: url) else { return false }
        try? FileManager.default.removeItem(at: url)
        guard let date = try? JSONDecoder().decode(Date.self, from: data) else { return false }
        return now.timeIntervalSince(date) >= -5 && now.timeIntervalSince(date) <= 60
    }
}

enum ScreenshotDestination: String, AppEnum {
    case screenshot

    static var typeDisplayRepresentation: TypeDisplayRepresentation = "AI Reply screen"
    static var caseDisplayRepresentations: [Self: DisplayRepresentation] = [
        .screenshot: "Screenshot reply"
    ]
}

/// Opens AI Reply's screenshot screen: the latest screenshot, or a choice of
/// one from Photos. Target membership is both app and widget (the Control
/// Center button). It never captures the screen: iOS gives no app that power;
/// the capture is the person's own shortcut (Take Screenshot → Reply to
/// Screenshot).
struct OpenScreenshotReplyIntent: OpenIntent {
    static var title: LocalizedStringResource = "Open AI Reply Screenshot"
    static var description = IntentDescription("Open AI Reply to answer a screenshot of a chat.")

    @Parameter(title: "Screen")
    var target: ScreenshotDestination

    init() { target = .screenshot }

    @MainActor
    func perform() async throws -> some IntentResult {
        ScreenshotLaunchRequest.request()
        NotificationCenter.default.post(name: .screenshotReplyOpenRequested, object: nil)
        return .result()
    }
}
