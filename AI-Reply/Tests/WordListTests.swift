import XCTest
@testable import AIReply

/// The packed word list: ranks, listed forms, the length buckets the
/// candidate search scans and the prefix index completions use.
final class WordListTests: XCTestCase {

    private let sample = """
    # comment lines do not take a rank
    и
    привет
    Москва
    сегодня
    сего
    сегодняшний
    кто-то
    """

    func testRanksSkipCommentsAndKeepListedForms() throws {
        let list = try WordList(data: Data(sample.utf8))
        XCTAssertEqual(list.count, 7)
        XCTAssertEqual(list.word(at: 0), "и")
        XCTAssertEqual(list.word(at: 2), "Москва")
        XCTAssertEqual(list.key(at: 2), "москва")
        XCTAssertEqual(list.word(at: 6), "кто-то")
    }

    func testExactLookupIsByLowercasedWord() throws {
        let list = try WordList(data: Data(sample.utf8))
        XCTAssertEqual(list.rank(of: "москва"), 2)
        XCTAssertEqual(list.rank(of: "сегодня"), 3)
        XCTAssertEqual(list.rank(of: "кто-то"), 6)
        XCTAssertNil(list.rank(of: "сегод"))
        XCTAssertNil(list.rank(of: "сегодняш"))
        XCTAssertNil(list.rank(of: "hello"), "letters the list never uses")
    }

    func testCompletionsAreLongerWordsMostFrequentFirst() throws {
        let list = try WordList(data: Data(sample.utf8))
        XCTAssertEqual(list.completionRanks(of: "сего", limit: 3), [3, 5], "never the prefix itself")
        XCTAssertEqual(list.completionRanks(of: "сего", limit: 1), [3])
        XCTAssertEqual(list.completionRanks(of: "мо", limit: 3), [2])
        XCTAssertEqual(list.completionRanks(of: "ю", limit: 3), [])
        XCTAssertEqual(list.completionRanks(of: "xy", limit: 3), [])
    }

    func testBucketsHoldEveryWordOfOneLengthInRankOrder() throws {
        let list = try WordList(data: Data(sample.utf8))
        let six: [Int] = list.withWords(length: 6) { ranks, codes in
            XCTAssertEqual(codes.count, ranks.count * 6)
            return Array(ranks).map(Int.init)
        } ?? []
        XCTAssertEqual(six.map(list.word(at:)), ["привет", "Москва", "кто-то"])
        XCTAssertNil(list.withWords(length: 9) { _, _ in true })
    }

    func testEnglishListKeepsCapitalI() throws {
        let list = try WordList(words: ["the", "I", "don't"])
        XCTAssertEqual(list.word(at: 1), "I")
        XCTAssertEqual(list.rank(of: "i"), 1)
        XCTAssertEqual(list.rank(of: WordList.key(for: "Don\u{2019}t")), 2, "typographic apostrophe")
    }

    func testRealListsLoad() throws {
        for (language, count) in [(KeyboardLanguage.russian, 80_000), (.kazakh, 80_000), (.english, 50_000)] {
            let list = try AutocorrectFixtures.engine(language).words
            XCTAssertEqual(list.count, count, language.rawValue)
        }
        let english = try AutocorrectFixtures.engine(.english).words
        XCTAssertEqual(english.word(at: 0), "the")
        XCTAssertEqual(english.word(at: english.rank(of: "i")!), "I")
    }
}
