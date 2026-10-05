import XCTest
@testable import AIReply

/// The engine cases of the design (§7.8), on the real dictionaries the
/// keyboard ships.
final class AutocorrectEngineTests: XCTestCase {

    // MARK: Russian

    func testRussianTyposAreCorrected() throws {
        let russian = try AutocorrectFixtures.engine(.russian)
        XCTAssertEqual(correction("сегодян", russian), "сегодня")
        XCTAssertEqual(correction("Сегодян", russian), "Сегодня", "the capital carries over")
        XCTAssertEqual(firstChoice("спасиб", russian), "спасибо")
        XCTAssertEqual(correction("превет", russian), "привет")
    }

    /// "привт" is one letter from both "привет" and "приют": too close to
    /// call, so it is offered first but not applied.
    func testCloseCallIsOfferedNotApplied() throws {
        let russian = try AutocorrectFixtures.engine(.russian)
        XCTAssertNil(correction("привт", russian))
        XCTAssertEqual(firstChoice("привт", russian), "привет")
    }

    /// "превт" and "привет" are two slips apart (a missing и plus a swap,
    /// 1.7): within the search radius since §10.2, past the 1.3 automatic
    /// limit. Closer words (право, прет, крест) score better, so привет is
    /// not among the three on the strip - the shared table pins that. What
    /// must hold: the word is not "fixed" into something else.
    func testTwoSlipWordIsLeftAlone() throws {
        let russian = try AutocorrectFixtures.engine(.russian)
        XCTAssertNil(correction("превт", russian))
        let reach = russian.candidates(for: "превт", limit: 20)
        XCTAssertEqual(reach.first { $0.word == "привет" }?.cost, 17, "within the 1.7 radius for five letters")
    }

    func testRussianWordsThatMustStay() throws {
        let russian = try AutocorrectFixtures.engine(.russian)
        for word in ["привет", "еще", "ещё", "созвониться", "ПРИВЕТ", "@превт", "превт123", "https://site.ru"] {
            XCTAssertNil(correction(word, russian), word)
        }
        XCTAssertTrue(russian.isKnown("еще"))
        XCTAssertTrue(russian.isKnown("Ещё"))
    }

    // MARK: Kazakh

    func testKazakhPlainSpellingGetsAHintButNoCorrection() throws {
        let kazakh = try AutocorrectFixtures.engine(.kazakh)
        XCTAssertNil(correction("кайда", kazakh), "known as a plain spelling")
        XCTAssertEqual(kazakh.suggestions(for: TypedWord(text: "кайда")).items.first, AutocorrectSuggestion(text: "қайда", kind: .word))
        XCTAssertEqual(kazakh.suggestions(for: TypedWord(text: "Кайда")).items.first?.text, "Қайда")
    }

    /// The hint is for a word the Kazakh list does not have as typed, as on
    /// Android (§10.5). A Russian word the Kazakh list has gets none; a
    /// Russian word it lacks may get one ("был" → "біл") - offered, never
    /// applied.
    func testKazakhHintOnlyForWordsTheKazakhListLacks() throws {
        let kazakh = try AutocorrectFixtures.engine(.kazakh)
        for word in ["сегодня", "қайда", "рахмет"] {
            XCTAssertNil(kazakh.kazakhLetterHint(for: word), word)
        }
        for word in ["они", "был", "куда", "сегодня"] {
            XCTAssertNil(correction(word, kazakh), word)
        }
        XCTAssertEqual(kazakh.kazakhLetterHint(for: "бугин"), "бүгін", "several letters at once")
        XCTAssertNil(try AutocorrectFixtures.engine(.russian).kazakhLetterHint(for: "кайда"), "only on the Kazakh layout")
    }

    /// The Kazakh layout corrects Russian too (§10.4): its candidates come
    /// from both lists, each word with its own list's rank.
    func testKazakhLayoutCorrectsRussianWords() throws {
        let kazakh = try AutocorrectFixtures.engine(.kazakh)
        XCTAssertEqual(correction("превет", kazakh), "привет")
        XCTAssertEqual(correction("сегодян", kazakh), "сегодня")
        XCTAssertNil(correction("қнига", kazakh), "never қ → к, whichever list has the word")
        XCTAssertFalse(kazakh.candidates(for: "қнига", limit: 10).contains { $0.word == "книга" })
    }

    func testKazakhTyposAreCorrected() throws {
        let kazakh = try AutocorrectFixtures.engine(.kazakh)
        XCTAssertEqual(firstChoice("қалайсын", kazakh), "қалайсың")
        for word in ["рахмет", "сәлем", "қалайсың", "калайсын"] {
            XCTAssertNil(correction(word, kazakh), word)
        }
    }

    func testTypedKazakhLetterIsNeverMadePlain() throws {
        let kazakh = try AutocorrectFixtures.engine(.kazakh)
        let plain: [Character: Set<Character>] = [
            "ә": ["а"], "ғ": ["г"], "қ": ["к"], "ң": ["н"], "ө": ["о"],
            "ұ": ["у"], "ү": ["у", "ұ"], "һ": ["х"], "і": ["и", "ы"]
        ]
        for typed in ["қайдаа", "қалаймыз", "қазақстанда", "көмектесейінші", "ұйқтау", "қасқырр", "қайдп"] {
            let offered = kazakh.candidates(for: typed, limit: 5).map(\.word) + [correction(typed, kazakh)].compactMap { $0 }
            for word in offered where word.count == typed.count {
                for (letter, other) in zip(typed, word) where letter != other {
                    XCTAssertFalse(plain[letter]?.contains(other) ?? false, "\(typed) → \(word)")
                }
            }
        }
    }

    // MARK: English

    func testEnglishTyposAndContractions() throws {
        let english = try AutocorrectFixtures.engine(.english)
        XCTAssertEqual(correction("tommorow", english), "tomorrow")
        XCTAssertEqual(correction("dont", english), "don't")
        XCTAssertEqual(correction("Dont", english), "Don't")
        XCTAssertEqual(correction("im", english), "I'm")
        XCTAssertEqual(correction("i", english), "I")
        XCTAssertEqual(correction("shouldnt", english), "shouldn't")
        for word in ["hello", "McDonald", "lol", "I", "its", "ill", "well", "were", "lets", "id"] {
            XCTAssertNil(correction(word, english), word)
        }
    }

    /// The web corpus counts a few classic misspellings as words; the
    /// dictionary build drops them (`supplement/*_typos.txt`), so they are
    /// corrected like any other typo (§10.1).
    func testCorpusTyposAreCorrected() throws {
        let english = try AutocorrectFixtures.engine(.english)
        for (typo, word) in [("teh", "the"), ("thier", "their"), ("recieve", "receive")] {
            XCTAssertFalse(english.isKnown(typo), typo)
            XCTAssertEqual(correction(typo, english), word, typo)
        }
        let russian = try AutocorrectFixtures.engine(.russian)
        XCTAssertFalse(russian.isKnown("извени"))
        XCTAssertFalse(russian.isKnown("сдесь"))
        XCTAssertEqual(correction("извени", russian), "извини")
    }

    /// A contraction typed without its apostrophe has its own fix, so the
    /// listed form is never offered (`don` → `don't`, never `dont`).
    func testApostropheLessContractionsAreNeverOffered() throws {
        let english = try AutocorrectFixtures.engine(.english)
        XCTAssertFalse(english.completions(for: "don", limit: 10).contains("dont"))
        XCTAssertTrue(english.completions(for: "don", limit: 3).contains("don't"))
        XCTAssertFalse(english.candidates(for: "dint", limit: 10).contains { $0.word == "dont" })
    }

    // MARK: Completions

    func testCompletions() throws {
        XCTAssertTrue(try AutocorrectFixtures.engine(.russian).completions(for: "сего", limit: 3).contains("сегодня"))
        XCTAssertTrue(try AutocorrectFixtures.engine(.english).completions(for: "tomo", limit: 3).contains("tomorrow"))
        XCTAssertTrue(try AutocorrectFixtures.engine(.kazakh).completions(for: "рахм", limit: 3).contains("рахмет"))
        XCTAssertEqual(try AutocorrectFixtures.engine(.russian).completions(for: "с", limit: 3), [], "not for one letter")
    }

    // MARK: Candidates

    func testCandidatesAreOrderedByScore() throws {
        let russian = try AutocorrectFixtures.engine(.russian)
        let found = russian.candidates(for: "превт", limit: 5)
        XCTAssertEqual(found.count, 5)
        XCTAssertEqual(found.map(\.score), found.map(\.score).sorted())
        XCTAssertFalse(found.contains { $0.word == "превт" })
        XCTAssertTrue(found.allSatisfy { $0.cost <= 17 }, "five letters: radius 1.7")
        let best = try XCTUnwrap(russian.candidates(for: "сегодян", limit: 1).first)
        XCTAssertEqual(best.word, "сегодня")
        XCTAssertEqual(best.cost, 7)
        XCTAssertEqual(best.score, 0.7 + AutocorrectEngine.rankPenalty(best.rank), accuracy: 1e-9)
    }

    func testCaseTransfer() {
        XCTAssertEqual(AutocorrectEngine.matchingCase(of: "превт", "привет"), "привет")
        XCTAssertEqual(AutocorrectEngine.matchingCase(of: "Превт", "привет"), "Привет")
        XCTAssertEqual(AutocorrectEngine.matchingCase(of: "москва", "Москва"), "Москва", "lowercase takes the listed form")
        XCTAssertEqual(AutocorrectEngine.matchingCase(of: "Қалайсын", "қалайсың"), "Қалайсың")
        XCTAssertEqual(AutocorrectEngine.matchingCase(of: "i", "I"), "I")
    }

    // MARK: Suggestion strip

    func testPendingCorrectionStrip() throws {
        let russian = try AutocorrectFixtures.engine(.russian)
        let strip = russian.suggestions(for: TypedWord(text: "сегодян"))
        XCTAssertEqual(strip.correction, AutocorrectCorrection(original: "сегодян", replacement: "сегодня"))
        XCTAssertEqual(strip.items.count, 3)
        XCTAssertEqual(strip.items[0], AutocorrectSuggestion(text: "сегодян", kind: .typed))
        XCTAssertEqual(strip.items[1], AutocorrectSuggestion(text: "сегодня", kind: .correction))
        XCTAssertEqual(strip.items[2].kind, .word)
    }

    func testContractionStrip() throws {
        let english = try AutocorrectFixtures.engine(.english)
        let strip = english.suggestions(for: TypedWord(text: "dont"))
        XCTAssertEqual(strip.correction?.replacement, "don't")
        XCTAssertEqual(strip.items.prefix(2).map(\.text), ["dont", "don't"])
        XCTAssertEqual(strip.items.prefix(2).map(\.kind), [.typed, .correction])
    }

    /// §10.3: completions of what was typed come first, then candidates.
    func testCompletionsComeBeforeCandidates() throws {
        let kazakh = try AutocorrectFixtures.engine(.kazakh)
        XCTAssertEqual(kazakh.suggestions(for: TypedWord(text: "рахм")).items.first, AutocorrectSuggestion(text: "рахмет", kind: .word))
        let english = try AutocorrectFixtures.engine(.english)
        XCTAssertEqual(english.suggestions(for: TypedWord(text: "tomo")).items.first?.text, "tomorrow")
    }

    func testKnownWordStripShowsCompletions() throws {
        let russian = try AutocorrectFixtures.engine(.russian)
        let strip = russian.suggestions(for: TypedWord(text: "Сего"))
        XCTAssertNil(strip.correction)
        XCTAssertTrue(strip.items.contains(AutocorrectSuggestion(text: "Сегодня", kind: .word)))
        XCTAssertFalse(strip.items.contains { $0.text.lowercased() == "сего" })
        XCTAssertLessThanOrEqual(strip.items.count, 3)
    }

    func testUnknownWordWithoutACorrectionShowsCandidates() throws {
        let russian = try AutocorrectFixtures.engine(.russian)
        let strip = russian.suggestions(for: TypedWord(text: "превт"))
        XCTAssertNil(strip.correction)
        XCTAssertEqual(strip.items.count, 3)
        XCTAssertTrue(strip.items.allSatisfy { $0.kind == .word })
    }

    func testNoStripForGuardedOrMissingWords() throws {
        let russian = try AutocorrectFixtures.engine(.russian)
        XCTAssertEqual(russian.suggestions(for: nil), .none)
        XCTAssertEqual(russian.suggestions(for: TypedWord(before: "@превт")), .none)
        XCTAssertEqual(russian.suggestions(for: TypedWord(text: "ПРИВЕТ")), .none)
        XCTAssertEqual(russian.suggestions(for: TypedWord(text: "hello")), .none)
    }

    // MARK: Lexicon

    func testTextReplacementIsAppliedAndContactsAreKnown() throws {
        let english = try AutocorrectFixtures.engine(.english)
        var session = AutocorrectFixtures.session(.english)
        session.lexicon = AutocorrectLexicon(entries: [
            .init(userInput: "omw", documentText: "On my way!"),
            .init(userInput: "Yerek", documentText: "Yerek")
        ])
        XCTAssertEqual(english.correction(for: TypedWord(text: "omw"), session: session)?.replacement, "On my way!")
        let strip = english.suggestions(for: TypedWord(text: "omw"), session: session)
        XCTAssertEqual(strip.items.prefix(2).map(\.text), ["omw", "On my way!"])
        XCTAssertTrue(english.isKnown("yerek", session: session))
        XCTAssertNil(english.correction(for: TypedWord(text: "Yerek"), session: session))
    }

    // MARK: Helpers

    private func correction(_ text: String, _ engine: AutocorrectEngine, session: AutocorrectSession? = nil) -> String? {
        guard let word = TypedWord(before: text) else { return nil }
        return engine.correction(for: word, session: session)?.replacement
    }

    /// The automatic correction, or else the first entry of the strip.
    private func firstChoice(_ text: String, _ engine: AutocorrectEngine) -> String? {
        correction(text, engine) ?? engine.suggestions(for: TypedWord(text: text)).items.first?.text
    }
}
