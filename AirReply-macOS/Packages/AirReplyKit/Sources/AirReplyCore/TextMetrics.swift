import Foundation

/// Text length exactly as the backend counts it before accepting a request:
/// Go `strings.TrimSpace`, then the number of Unicode code points (runes).
/// An emoji sequence (family, flag) is therefore several characters.
public enum TextMetrics {
    public static func serverLength(_ text: String) -> Int {
        text.trimmingCharacters(in: .whitespacesAndNewlines).unicodeScalars.count
    }

    public static func fits(_ text: String, limit: Int) -> Bool {
        serverLength(text) <= limit
    }
}
