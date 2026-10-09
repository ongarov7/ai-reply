#if DEBUG
import Foundation

/// DEBUG ONLY. Launch arguments that put the notification features into a
/// state a Simulator cannot reach on its own: nobody can tap the permission
/// alert or a banner there. Compiled out of Release builds entirely.
///
/// - `-AIReplyForcePushCard YES` shows Home's notification card and the
///   Settings sections whatever the build, server, account or permission.
/// - `-AIReplyOpenLink <link>` opens a link at launch exactly as a tapped
///   notification would, gates included. Accepts a full link or, because the
///   build watcher strips ":" and "/", a bare screen name ("subscription")
///   or "web" for https://ai-reply.kz/.
/// - `-AIReplyDebugProvisionalPush YES` asks for provisional authorization,
///   which needs no alert, so `simctl push` notifications are delivered.
/// - `-AIReplyDebugAutoOpenPush YES` treats a notification arriving in the
///   foreground as tapped, so routing can be checked with `simctl push`.
enum DebugLaunchOptions {

    static var forcesPushUI: Bool { flag("-AIReplyForcePushCard") }
    static var requestsProvisionalPush: Bool { flag("-AIReplyDebugProvisionalPush") }
    static var autoOpensPushes: Bool { flag("-AIReplyDebugAutoOpenPush") }

    static var openLink: String? {
        value(after: "-AIReplyOpenLink").map(expandLink)
    }

    /// "subscription" → "aireply://subscription", "web" → the site.
    static func expandLink(_ raw: String) -> String {
        if raw.contains(":") { return raw }
        if raw.lowercased() == "web" { return "https://ai-reply.kz/" }
        return "aireply://" + raw
    }

    private static func flag(_ name: String, arguments: [String] = CommandLine.arguments) -> Bool {
        guard let index = arguments.firstIndex(of: name) else { return false }
        let next = index + 1 < arguments.count ? arguments[index + 1] : "YES"
        return next.uppercased() != "NO"
    }

    private static func value(after name: String, arguments: [String] = CommandLine.arguments) -> String? {
        guard let index = arguments.firstIndex(of: name), index + 1 < arguments.count else { return nil }
        let value = arguments[index + 1]
        return value.hasPrefix("-") ? nil : value
    }
}
#endif
