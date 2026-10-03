import Foundation

/// Diagnostics never reach the UI and never carry message content, instruction
/// text, profile text or draft text - only lengths and outcomes, and only in a
/// debug build. Shared, so the Create flow logs the same way as replies.
enum ReplyLog {
    static func event(_ message: @autoclosure () -> String) {
        #if DEBUG
        NSLog("[ReplyKeyboard] %@", message())
        #endif
    }
}
