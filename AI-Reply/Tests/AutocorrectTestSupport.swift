import Foundation
@testable import AIReply

/// The real dictionaries the keyboard ships, read straight from the source
/// tree, and one loader for the whole test run so each language is parsed
/// once.
enum AutocorrectFixtures {

    static let dictionaries: URL = URL(fileURLWithPath: #filePath)
        .resolvingSymlinksInPath()
        .deletingLastPathComponent()
        .deletingLastPathComponent()
        .appendingPathComponent("ReplyKeyboard/Dictionaries", isDirectory: true)

    static let loader = AutocorrectLoader(directory: dictionaries)

    static func engine(_ language: KeyboardLanguage) throws -> AutocorrectEngine {
        try loader.loadEngine(for: language)
    }

    static func session(_ language: KeyboardLanguage) -> AutocorrectSession {
        AutocorrectSession(learned: LearnedWords(language: language))
    }
}
