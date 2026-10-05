import Foundation

/// Words the system lends the keyboard (`UILexicon` on iOS), as plain data so
/// the engine stays free of UIKit:
///
/// * an entry whose input is its own text (a contact's name) is a known word;
/// * any other entry is a text replacement (`omw` → `On my way!`): the top
///   suggestion for its input, applied on a separator.
struct AutocorrectLexicon: Equatable, Sendable {

    struct Entry: Equatable, Sendable {
        let userInput: String
        let documentText: String
    }

    private var known: Set<String> = []
    private var replacements: [String: String] = [:]

    init(entries: [Entry] = []) {
        for entry in entries {
            let key = WordList.key(for: entry.userInput)
            guard !key.isEmpty else { continue }
            if key == WordList.key(for: entry.documentText) {
                known.insert(key)
            } else if replacements[key] == nil {
                replacements[key] = entry.documentText
            }
        }
    }

    func knows(_ key: String) -> Bool {
        known.contains(key)
    }

    func replacement(for key: String) -> String? {
        replacements[key]
    }
}
