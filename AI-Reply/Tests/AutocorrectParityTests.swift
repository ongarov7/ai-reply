import XCTest
@testable import AIReply

/// The table both keyboards answer the same way (DESIGN §10.5): the same
/// typed word on the same layout gives the same correction and the same
/// strip on iOS and on Android. The table is `tools/dictionaries/
/// autocorrect_parity.json`; the Android suite reads the same file.
final class AutocorrectParityTests: XCTestCase {

    private struct Table: Decodable {
        let format: Int
        let cases: [Case]
    }

    private struct Case: Decodable {
        let layout: String
        let typed: String
        let before: String
        let correction: String?
        let strip: [Item]
    }

    private struct Item: Decodable, Equatable, CustomStringConvertible {
        let kind: String
        let text: String

        var description: String { "\(kind):\(text)" }
    }

    static let table: URL = AutocorrectFixtures.dictionaries
        .deletingLastPathComponent()
        .deletingLastPathComponent()
        .deletingLastPathComponent()
        .appendingPathComponent("tools/dictionaries/autocorrect_parity.json")

    func testEveryCaseMatchesTheSharedTable() throws {
        let table = try JSONDecoder().decode(Table.self, from: Data(contentsOf: Self.table))
        XCTAssertEqual(table.format, 1)
        XCTAssertFalse(table.cases.isEmpty)

        for entry in table.cases {
            let language = try XCTUnwrap(KeyboardLanguage(rawValue: entry.layout), entry.layout)
            let engine = try AutocorrectFixtures.engine(language)
            let word = TypedWord(text: entry.typed, prefix: Self.lastChunk(of: entry.before))
            let label = "\(entry.layout) «\(entry.typed)» after «\(entry.before)»"

            XCTAssertEqual(engine.correction(for: word)?.replacement, entry.correction, label)
            let strip = engine.suggestions(for: word)
            XCTAssertEqual(strip.correction?.replacement, entry.correction, label)
            XCTAssertEqual(strip.items.map(Self.item), entry.strip, label)
        }
    }

    /// What the guard sees of `before`: its last whitespace-free chunk.
    private static func lastChunk(of before: String) -> String {
        String(before.reversed().prefix { !$0.isWhitespace }.reversed())
    }

    private static func item(_ suggestion: AutocorrectSuggestion) -> Item {
        switch suggestion.kind {
        case .typed: return Item(kind: "typed", text: suggestion.text)
        case .correction: return Item(kind: "correction", text: suggestion.text)
        case .word: return Item(kind: "word", text: suggestion.text)
        }
    }
}
