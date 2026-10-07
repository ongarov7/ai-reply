import XCTest
@testable import AIReply

/// Key neighbours and the weighted edit distance (§7.4), measured on small
/// hand-made lists so every cost is visible.
final class AutocorrectDistanceTests: XCTestCase {

    // MARK: Key proximity

    func testEnglishNeighboursFollowTheStaggeredRows() {
        let keys = KeyProximity(layout: .english)
        for neighbour in ["w", "e", "a", "d", "z", "x"] {
            XCTAssertTrue(keys.areAdjacent("s", neighbour), neighbour)
        }
        for stranger in ["q", "r", "c", "f"] {
            XCTAssertFalse(keys.areAdjacent("s", stranger), stranger)
        }
        XCTAssertTrue(keys.areAdjacent("e", "r"))
        XCTAssertFalse(keys.areAdjacent("e", "y"))
    }

    func testCyrillicNeighboursOnTheElevenColumnGrid() {
        let russian = KeyProximity(layout: .russian)
        XCTAssertTrue(russian.areAdjacent("е", "к"))
        XCTAssertTrue(russian.areAdjacent("е", "п"))
        XCTAssertTrue(russian.areAdjacent("я", "ы"))
        XCTAssertFalse(russian.areAdjacent("е", "и"), "two rows apart")

        let kazakh = KeyProximity(layout: .kazakh)
        XCTAssertTrue(kazakh.areAdjacent("ә", "ц"), "the Kazakh row sits one key in")
        XCTAssertTrue(kazakh.areAdjacent("қ", "ш"))
        XCTAssertTrue(kazakh.areAdjacent("ә", "і"))
        XCTAssertFalse(kazakh.areAdjacent("қ", "к"))
        XCTAssertFalse(russian.areAdjacent("ә", "ц"), "no Kazakh row on the Russian layout")
    }

    func testRowsAreCentredOnTheWidestRow() {
        let keys = KeyProximity(rows: [["a", "b", "c"], ["x"]])
        XCTAssertTrue(keys.areAdjacent("x", "a"))
        XCTAssertTrue(keys.areAdjacent("x", "b"))
        XCTAssertTrue(keys.areAdjacent("x", "c"))
        XCTAssertTrue(keys.areAdjacent("a", "b"))
        XCTAssertFalse(keys.areAdjacent("a", "c"))
    }

    // MARK: Costs

    func testKazakhLettersAreCheapOneWayAndForbiddenTheOther() throws {
        let measure = try Measurer(["қайда", "кайда", "сәлем"], language: .kazakh)
        XCTAssertEqual(measure("кайда", "қайда"), 3)
        XCTAssertEqual(measure("салем", "сәлем"), 3)
        XCTAssertEqual(measure("қайда", "кайда"), 20, "never the cheap substitution: only a deletion plus an insertion")
        XCTAssertNil(measure("қайда", "кайда", limit: 19))
    }

    /// The costs do not depend on the layout, as on Android: a Kazakh letter
    /// that reached the Russian layout (typed on ҚАЗ before a switch) is
    /// protected there too. The Russian list has no Kazakh letters, so the
    /// cheap direction never applies to Russian words.
    func testKazakhLettersCostTheSameOnEveryLayout() throws {
        let measure = try Measurer(["сәлем", "салем", "кайда"], language: .russian)
        XCTAssertEqual(measure("салем", "сәлем"), 3)
        XCTAssertEqual(measure("қайда", "кайда"), 20)
    }

    /// A typed letter the list never uses (Kazakh against the Russian words
    /// searched on the Kazakh layout) costs what it would cost on the keys.
    func testLetterOutsideTheListIsMeasuredByTheKeys() throws {
        let measure = try Measurer(["книга", "шнига"], language: .kazakh)
        XCTAssertEqual(measure("қнига", "книга", limit: 30), 20, "never қ → к: a deletion plus an insertion")
        XCTAssertEqual(measure("қнига", "шнига"), 6, "қ sits next to ш")
    }

    func testLongPressLettersShareTheirKey() {
        let russian = KeyProximity(layout: .russian)
        XCTAssertTrue(russian.areAdjacent("ъ", "ь"))
        XCTAssertTrue(russian.areAdjacent("ъ", "б"))
        XCTAssertTrue(russian.areAdjacent("ё", "к"))
        XCTAssertFalse(KeyProximity(layout: .english).areAdjacent("ъ", "ь"))
    }

    func testYoIsAlmostFree() throws {
        let measure = try Measurer(["ещё", "еще"], language: .russian)
        XCTAssertEqual(measure("еще", "ещё"), 1)
        XCTAssertEqual(measure("ещё", "еще"), 1)
    }

    func testTyposCostWhatTheSpecSays() throws {
        let measure = try Measurer(["сегодня", "привет", "мыло"], language: .russian)
        XCTAssertEqual(measure("сегодян", "сегодня"), 7, "swap")
        XCTAssertEqual(measure("сеггодня", "сегодня"), 6, "doubled letter")
        XCTAssertEqual(measure("привт", "привет"), 10, "missing letter")
        XCTAssertEqual(measure("приывет", "привет"), 10, "extra letter")
        XCTAssertEqual(measure("прмвет", "привет"), 6, "neighbouring key")
        XCTAssertEqual(measure("прлвет", "привет"), 10, "far key")
        XCTAssertEqual(measure("превт", "привет"), 17, "two slips: a missing и plus a swap")
    }

    func testCutoffDropsFarWords() throws {
        let measure = try Measurer(["привет", "сегодня"], language: .russian)
        XCTAssertNil(measure("привет", "сегодня"))
        XCTAssertEqual(measure("превт", "привет", limit: 17), 17)
        XCTAssertNil(measure("превт", "привет", limit: 16))
    }
}

/// Measures typed → listed word, in tenths, with the costs of a layout.
private struct Measurer {

    let list: WordList
    let costs: EditCosts

    init(_ words: [String], language: KeyboardLanguage) throws {
        list = try WordList(words: words)
        costs = EditCosts(alphabet: list.alphabet, proximity: KeyProximity(layout: language))
    }

    func callAsFunction(_ typed: String, _ word: String, limit: Int32 = 20) -> Int32? {
        let distance = EditDistance(typed: list.encode(typed), units: Array(WordList.key(for: typed).utf16), costs: costs, longest: 30)
        return list.encode(word).withUnsafeBufferPointer { distance.distance(to: $0.baseAddress!, count: $0.count, limit: limit) }
    }
}
