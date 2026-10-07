import Foundation

/// Words the keyboard never corrects, whatever the dictionary says: anything
/// that looks like an address, a handle, a code, a brand or a deliberate
/// spelling. Wrongly "fixing" these costs the user more than a missed typo.
///
/// Whether the host field allows correction at all (secure entry, URL and
/// e-mail fields, the setting) is decided by the keyboard before it asks.
enum TokenGuard {

    enum Reason: Equatable, Sendable {
        /// Part of an address, handle or path: `site.ru`, `@name`, `a_b`.
        case technical
        case digits
        /// Fewer than two letters (English `i` excepted).
        case tooShort
        case allCaps
        /// A capital after the first letter: `iPhone`, `McDonald`.
        case innerCapital
        case mixedScript
        /// Latin on a Cyrillic layout, Cyrillic on the English one.
        case otherScript
        /// The user taught the keyboard this word.
        case learned
    }

    /// `://` and `www.` are caught by `:`, `/` and `.`.
    private static let technicalCharacters: Set<Character> = ["@", "#", "/", "\\", ":", ".", "_", "=", "&"]

    /// Why `word` must stay as typed, or nil when it may be corrected.
    static func reason(for word: TypedWord, language: KeyboardLanguage, learned: LearnedWords) -> Reason? {
        let text = word.text
        if text.contains(where: technicalCharacters.contains) || word.prefix.contains(where: technicalCharacters.contains) {
            return .technical
        }
        if text.contains(where: \.isNumber) { return .digits }

        let letters = text.filter(\.isLetter)
        if letters.count < 2, !(language == .english && text.lowercased() == "i") { return .tooShort }
        if letters.count >= 2, letters.allSatisfy(\.isUppercase) { return .allCaps }
        if letters.dropFirst().contains(where: \.isUppercase) { return .innerCapital }

        // Combining marks (a decomposed `й`) belong to their letter.
        let scripts = Set(letters.unicodeScalars.filter(\.properties.isAlphabetic).map(Script.init))
        if scripts.contains(.latin), scripts.contains(.cyrillic) { return .mixedScript }
        let expected: Script = language == .english ? .latin : .cyrillic
        if scripts != [expected] { return .otherScript }

        if learned.language == language, learned.contains(text) { return .learned }
        return nil
    }

    private enum Script {
        case latin
        case cyrillic
        case other

        init(_ scalar: Unicode.Scalar) {
            switch scalar.value {
            case 0x41...0x5A, 0x61...0x7A, 0xC0...0x24F, 0x1E00...0x1EFF:
                self = .latin
            case 0x400...0x52F:
                self = .cyrillic
            default:
                self = .other
            }
        }
    }
}
