import XCTest
@testable import AIReply

/// Taking a correction back, keeping a typed word, and the learned words
/// that remember both (§7.7).
final class AutocorrectLearningTests: XCTestCase {

    // MARK: Learned words

    func testLearnedWordsAreLowercasedAndMostRecentLast() {
        var learned = LearnedWords(language: .russian)
        learned.learn("Превт")
        learned.learn("алматыда")
        learned.learn("превт")
        XCTAssertEqual(learned.words, ["алматыда", "превт"])
        XCTAssertTrue(learned.contains("ПРЕВТ"))
        learned.learn("123")
        learned.learn("")
        XCTAssertEqual(learned.words.count, 2, "only words with letters")
    }

    func testCapacityKeepsTheMostRecent() {
        var learned = LearnedWords(language: .english, words: (0..<LearnedWords.capacity).map { "word\($0)x" })
        XCTAssertEqual(learned.words.count, LearnedWords.capacity)
        learned.learn("word0x")
        learned.learn("brandnew")
        XCTAssertEqual(learned.words.count, LearnedWords.capacity)
        XCTAssertFalse(learned.contains("word1x"), "the oldest goes first")
        XCTAssertTrue(learned.contains("word0x"), "used again, so kept")
        XCTAssertEqual(learned.words.last, "brandnew")

        let trimmed = LearnedWords(language: .english, words: (0..<600).map { "w\($0)" })
        XCTAssertEqual(trimmed.words.first, "w100")
        XCTAssertEqual(trimmed.words.count, LearnedWords.capacity)
    }

    func testStoreRoundTripPerLanguage() throws {
        let suite = "AutocorrectLearningTests.\(UUID().uuidString)"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }
        let store = DefaultsLearnedWordsStore(defaults: defaults)

        var russian = LearnedWords(language: .russian, store: store)
        XCTAssertEqual(russian.words, [])
        russian.learn("превт")
        russian.save(to: store)

        XCTAssertEqual(defaults.stringArray(forKey: "autocorrect.learned.ru"), ["превт"])
        XCTAssertEqual(LearnedWords(language: .russian, store: store).words, ["превт"])
        XCTAssertEqual(LearnedWords(language: .kazakh, store: store).words, [])
    }

    // MARK: Undo

    func testBackspaceAfterACorrectionRestoresAndLearns() throws {
        let russian = try AutocorrectFixtures.engine(.russian)
        var session = AutocorrectFixtures.session(.russian)
        let word = TypedWord(text: "Сегодян")
        let correction = try XCTUnwrap(russian.correction(for: word, session: session))
        XCTAssertEqual(correction.replacement, "Сегодня")

        let applied = AppliedAutocorrection(correction: correction, separator: " ")
        XCTAssertEqual(applied.charactersToDelete, 8)
        XCTAssertEqual(applied.restoredText, "Сегодян")

        session.undo(applied)
        XCTAssertTrue(session.rejected.contains(original: "сегодян", replacement: "сегодня"))
        XCTAssertTrue(session.learned.contains("сегодян"))
        XCTAssertNil(russian.correction(for: word, session: session))
        XCTAssertTrue(russian.isKnown("сегодян", session: session))
    }

    func testRejectedCorrectionIsNotOfferedAgainThisSession() throws {
        let english = try AutocorrectFixtures.engine(.english)
        var session = AutocorrectFixtures.session(.english)
        session.rejected.insert(AutocorrectCorrection(original: "tommorow", replacement: "tomorrow"))
        XCTAssertNil(english.correction(for: TypedWord(text: "tommorow"), session: session))
        XCTAssertFalse(english.suggestions(for: TypedWord(text: "tommorow"), session: session).items.contains { $0.kind == .correction })

        session.rejected.insert(AutocorrectCorrection(original: "dont", replacement: "don't"))
        XCTAssertNil(english.correction(for: TypedWord(text: "dont"), session: session))
    }

    func testKeepingTheTypedWordTeachesIt() throws {
        let english = try AutocorrectFixtures.engine(.english)
        var session = AutocorrectFixtures.session(.english)
        XCTAssertNotNil(english.correction(for: TypedWord(text: "dont"), session: session))
        session.keep("dont")
        XCTAssertNil(english.correction(for: TypedWord(text: "dont"), session: session))
        XCTAssertNil(english.correction(for: TypedWord(text: "Dont"), session: session))
    }

    func testLearnedWordsOfAnotherLayoutAreIgnored() throws {
        let russian = try AutocorrectFixtures.engine(.russian)
        var session = AutocorrectFixtures.session(.kazakh)
        session.keep("сегодян")
        XCTAssertEqual(russian.correction(for: TypedWord(text: "сегодян"), session: session)?.replacement, "сегодня")
    }
}
