import Foundation

/// Every version of the reply one session has produced, and the one on screen.
///
/// WHY IT EXISTS. Regenerate used to overwrite the draft in place, so a user
/// who had corrected a name in the first answer and then tapped Regenerate
/// "just to see" lost the correction. Now a regeneration ADDS a version: the
/// edited one is still there, one tap back, exactly as the user left it. There
/// is no "are you sure?" to answer, because nothing is ever destroyed.
///
/// Lives in memory only, for as long as the composer session does.
struct ReplyDraftHistory: Equatable, Sendable {

    struct Version: Equatable, Sendable {
        /// What the model returned.
        let generated: String
        /// What the user has made of it. Starts equal to `generated`.
        var text: String

        var isEdited: Bool { text != generated }
    }

    /// Enough to compare a few takes; old unedited ones go first when full.
    static let capacity = 6

    private(set) var versions: [Version] = []
    private(set) var index = 0

    var isEmpty: Bool { versions.isEmpty }
    var count: Int { versions.count }
    var current: Version? { versions.indices.contains(index) ? versions[index] : nil }
    var currentText: String { current?.text ?? "" }

    /// 1-based position for display ("2/3").
    var position: Int { isEmpty ? 0 : index + 1 }

    var canSelectPrevious: Bool { index > 0 }
    var canSelectNext: Bool { index < versions.count - 1 }

    /// Adds a freshly generated reply and shows it.
    mutating func append(generated text: String) {
        versions.append(Version(generated: text, text: text))
        if versions.count > Self.capacity {
            // Drop the oldest version the user has not touched; if every one
            // is edited, the oldest of all.
            let victim = versions.firstIndex { !$0.isEdited } ?? 0
            versions.remove(at: victim)
        }
        index = versions.count - 1
    }

    /// The user edited the version on screen.
    mutating func edit(_ text: String) {
        guard versions.indices.contains(index) else {
            // Typing into an empty result: the user is writing their own reply.
            versions = [Version(generated: "", text: text)]
            index = 0
            return
        }
        versions[index].text = text
    }

    mutating func selectPrevious() {
        if canSelectPrevious { index -= 1 }
    }

    mutating func selectNext() {
        if canSelectNext { index += 1 }
    }

    mutating func removeAll() {
        versions.removeAll()
        index = 0
    }
}
