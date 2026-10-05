import XCTest
@testable import AIReply

/// Typing must never wait on the dictionaries: they load in the background,
/// and a lookup is a few milliseconds even in a Debug build.
final class AutocorrectPerformanceTests: XCTestCase {

    /// §7.8: under 15 ms in Debug is the goal; only over 100 ms fails.
    func testSevenLetterRussianCandidatesAreFast() throws {
        let russian = try AutocorrectFixtures.engine(.russian)
        _ = russian.candidates(for: "сегодян", limit: 3)

        var slowest: TimeInterval = 0
        for word in ["сегодян", "спасибл", "пожалуц", "звоните", "встреиа"] {
            let start = Date()
            _ = russian.candidates(for: word, limit: AutocorrectEngine.stripLimit)
            slowest = max(slowest, Date().timeIntervalSince(start))
        }
        print(String(format: "Autocorrect: slowest 7-letter Russian candidates %.1f ms", slowest * 1000))
        XCTAssertLessThan(slowest, 0.1)
    }

    func testEngineQueriesFromManyQueuesAgree() throws {
        let kazakh = try AutocorrectFixtures.engine(.kazakh)
        let expected = kazakh.suggestions(for: TypedWord(text: "қалайсын"))
        var results = [AutocorrectSuggestions](repeating: .none, count: 8)
        let lock = NSLock()
        DispatchQueue.concurrentPerform(iterations: results.count) { index in
            let strip = kazakh.suggestions(for: TypedWord(text: "қалайсын"))
            lock.lock()
            results[index] = strip
            lock.unlock()
        }
        XCTAssertTrue(results.allSatisfy { $0 == expected })
    }

    func testLoaderAnswersAtOnceAndLoadsInTheBackground() throws {
        let loader = AutocorrectLoader(directory: AutocorrectFixtures.dictionaries)
        XCTAssertNil(loader.engine(for: .english), "not loaded yet: no suggestions rather than a wait")
        let loaded = expectation(description: "English loaded")
        DispatchQueue.global().async {
            while loader.engine(for: .english) == nil { usleep(10_000) }
            loaded.fulfill()
        }
        wait(for: [loaded], timeout: 30)
    }

    func testMissingDictionariesTurnAutocorrectOff() throws {
        let loader = AutocorrectLoader(directory: FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString))
        XCTAssertThrowsError(try loader.loadEngine(for: .russian))
        loader.prepare(.russian)
        XCTAssertNil(loader.engine(for: .russian))
    }
}
