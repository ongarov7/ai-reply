import UIKit

/// Shift, the way the iOS keyboard behaves:
///
/// * one tap arms it for the next letter;
/// * two taps in quick succession lock it (caps lock), one more tap unlocks;
/// * the keyboard arms it by itself at the start of a sentence, and takes an
///   automatic shift back if the caret moves away from that start - but never
///   overrides a shift the user set by hand.
struct ShiftState: Equatable, Sendable {

    enum Mode: Equatable, Sendable {
        case off
        case once
        case locked
    }

    static let doubleTapInterval: TimeInterval = 0.32

    private(set) var mode: Mode = .off
    /// True when the current `.once` was set by auto-capitalization.
    private(set) var isAutomatic = false
    private var lastTap: TimeInterval = -.infinity

    var isActive: Bool { mode != .off }
    var isLocked: Bool { mode == .locked }

    mutating func tap(at time: TimeInterval) {
        if mode == .locked {
            mode = .off
            isAutomatic = false
            lastTap = -.infinity
            return
        }
        if time - lastTap <= Self.doubleTapInterval {
            mode = .locked
            isAutomatic = false
            lastTap = -.infinity
            return
        }
        mode = mode == .off ? .once : .off
        isAutomatic = false
        lastTap = time
    }

    /// A character was typed: a one-shot shift is spent.
    mutating func characterTyped() {
        lastTap = -.infinity
        if mode == .once {
            mode = .off
            isAutomatic = false
        }
    }

    /// Applies auto-capitalization for the text now before the caret.
    mutating func applyAutomatic(_ shouldCapitalize: Bool) {
        switch mode {
        case .locked:
            return
        case .off:
            if shouldCapitalize {
                mode = .once
                isAutomatic = true
            }
        case .once:
            if !shouldCapitalize, isAutomatic {
                mode = .off
                isAutomatic = false
            }
        }
    }

    mutating func reset() {
        mode = .off
        isAutomatic = false
        lastTap = -.infinity
    }

    /// The text a key types in this state.
    func apply(to text: String) -> String {
        isActive ? text.uppercased() : text
    }
}

/// Whether the next letter should be a capital, decided the way iOS does it
/// from the host field's `autocapitalizationType` and the text before the
/// caret.
enum AutoCapitalization {

    static func shouldCapitalize(before context: String?, type: UITextAutocapitalizationType) -> Bool {
        switch type {
        case .none:
            return false
        case .allCharacters:
            return true
        case .words:
            guard let last = context?.last else { return true }
            return last.isWhitespace
        case .sentences:
            return isSentenceStart(context)
        @unknown default:
            return isSentenceStart(context)
        }
    }

    /// An empty field, a new line, or sentence punctuation followed by a
    /// space. "Hello.|" is not a sentence start yet; "Hello. |" is.
    static func isSentenceStart(_ context: String?) -> Bool {
        guard let context, !context.isEmpty else { return true }
        var index = context.endIndex
        var trailingSpaces = 0
        while index > context.startIndex {
            let previous = context.index(before: index)
            let character = context[previous]
            if character.isNewline { return true }
            guard character == " " || character == "\u{00A0}" || character == "\t" else { break }
            trailingSpaces += 1
            index = previous
        }
        guard index > context.startIndex else { return true }
        guard trailingSpaces > 0 else { return false }

        // Step over closing quotes and brackets: `He said "Yes." |` starts a
        // sentence too.
        var cursor = context.index(before: index)
        while cursor > context.startIndex, "\"'»”’)]".contains(context[cursor]) {
            cursor = context.index(before: cursor)
        }
        return ".!?…".contains(context[cursor])
    }
}

/// The double-space full stop: a second space typed quickly after a word turns
/// the first one into ". ".
enum SpaceShortcut {

    static let interval: TimeInterval = 0.45

    static func shouldInsertPeriod(before context: String?, secondsSinceLastSpace: TimeInterval) -> Bool {
        guard secondsSinceLastSpace <= interval,
              let context, context.hasSuffix(" "), !context.hasSuffix("  ") else { return false }
        let beforeSpace = context.dropLast()
        guard let last = beforeSpace.last else { return false }
        return last.isLetter || last.isNumber || "\"')»”’".contains(last)
    }
}

/// Word-at-a-time deletion, used once delete has been held for a while.
enum TextDeletion {

    /// How many characters one word-delete removes from the end of `context`:
    /// the whitespace before the caret plus the word (or the run of
    /// punctuation) before that. Always at least one while there is text.
    static func wordLength(before context: String) -> Int {
        guard !context.isEmpty else { return 0 }
        var count = 0
        var index = context.endIndex

        while index > context.startIndex {
            let previous = context.index(before: index)
            guard context[previous].isWhitespace else { break }
            count += 1
            index = previous
        }
        guard index > context.startIndex else { return count }

        func isWordCharacter(_ character: Character) -> Bool {
            character.isLetter || character.isNumber || character == "'" || character == "’" || character == "-"
        }
        let deletingWord = isWordCharacter(context[context.index(before: index)])

        while index > context.startIndex {
            let previous = context.index(before: index)
            let character = context[previous]
            if character.isWhitespace || isWordCharacter(character) != deletingWord { break }
            count += 1
            index = previous
        }
        return max(count, 1)
    }
}

/// Which host field the keys type into, as far as a keyboard can tell: the
/// proxy's document identifier and the field's keyboard type.
struct HostFieldIdentity: Equatable {
    var documentIdentifier: UUID?
    var keyboardType: UIKeyboardType?
}

/// Notices the caret moving to another host field.
///
/// Return or Next in a form moves the caret to the next field right after the
/// keyboard's own keystroke - inside the window in which the keyboard trusts
/// its own copy of the text and skips re-reading the host. Without this the
/// old field's capitalisation, correction and keyboard type would stay.
struct HostFieldTracker {

    /// How long after its own keystroke the keyboard trusts its copy of the
    /// host text.
    static let ownMutationWindow: TimeInterval = 0.45

    private(set) var current: HostFieldIdentity?

    /// The keyboard appeared on a field: that one is current, not "new".
    mutating func reset(to field: HostFieldIdentity) {
        current = field
    }

    /// Records the field the host reports now. True when it is another one
    /// than before - never for the first field seen.
    mutating func update(_ field: HostFieldIdentity) -> Bool {
        defer { current = field }
        guard let current else { return false }
        return current != field
    }

    /// Whether a text change is the keyboard's own keystroke echoing back,
    /// so the host need not be re-read. Never for another field.
    static func trustsMirror(sinceOwnMutation elapsed: TimeInterval, fieldChanged: Bool) -> Bool {
        !fieldChanged && elapsed < ownMutationWindow
    }
}
