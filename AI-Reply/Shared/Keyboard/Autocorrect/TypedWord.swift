import Foundation

/// The word right before the caret, and what comes before it in the same
/// whitespace-delimited chunk - `site.` in `site.ru`, `@` in `@name` - which
/// the token guard needs to recognise addresses and handles.
struct TypedWord: Equatable, Sendable {

    let text: String
    let prefix: String

    init(text: String, prefix: String = "") {
        self.text = text
        self.prefix = prefix
    }

    /// The word at the end of `context`, or nil when the caret is not right
    /// after one. A word is a run of letters and digits, with apostrophes and
    /// hyphens inside it (`don't`, `кто-то`).
    init?(before context: String) {
        var chunkStart = context.endIndex
        while chunkStart > context.startIndex {
            let previous = context.index(before: chunkStart)
            guard !context[previous].isWhitespace else { break }
            chunkStart = previous
        }
        var start = context.endIndex
        while start > chunkStart {
            let previous = context.index(before: start)
            guard Self.isWordCharacter(context[previous]) else { break }
            start = previous
        }
        while start < context.endIndex, Self.isJoiner(context[start]) {
            start = context.index(after: start)
        }
        guard start < context.endIndex else { return nil }
        self.init(text: String(context[start...]), prefix: String(context[chunkStart..<start]))
    }

    private static func isWordCharacter(_ character: Character) -> Bool {
        character.isLetter || character.isNumber || isJoiner(character)
    }

    private static func isJoiner(_ character: Character) -> Bool {
        character == "'" || character == "\u{2019}" || character == "-"
    }
}
