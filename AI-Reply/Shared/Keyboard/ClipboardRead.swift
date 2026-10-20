import Foundation

/// What one read of the clipboard on the user's tap came back with.
///
/// Алмасу буферін оқу нәтижесі: мәтін, бос буфер немесе iOS рұқсат бермеген.
///
/// Kept apart from the read itself (`ContextTextProvider`, in the keyboard)
/// so the rule is unit-tested: `hasStrings` is metadata and never asks the
/// user, `string` is the read that can. When iOS says there is text and the
/// read still returns none, the paste was refused - the user tapped "Don't
/// Allow", or "Paste from Other Apps" is set to deny - which is not the same
/// as having copied nothing, and is told apart so the keyboard can say which.
enum ClipboardRead: Equatable, Sendable {
    /// Usable text, trimmed.
    case text(String)
    /// Nothing copied, or nothing but whitespace.
    case empty
    /// iOS had text and gave none.
    case denied

    static func classify(hasStrings: Bool, string: String?) -> ClipboardRead {
        guard hasStrings else { return .empty }
        guard let string else { return .denied }
        let trimmed = string.trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? .empty : .text(trimmed)
    }
}
