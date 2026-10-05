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
    /// Android (§10.5). A word either list has gets none: "рахмет" is Kazakh
    /// as typed, and a listed Russian word is meant as written ("был" is not
    /// "біл", "куда" is not "құда", "они" is not "өңі").
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

    /// Regression: Russian words missing from kk.words used to get a Kazakh
    /// hint in the first slot (был → біл, куда → құда, они → өңі).
    func testListedRussianWordGetsNoKazakhHint() throws {
        let kazakh = try AutocorrectFixtures.engine(.kazakh)
        for word in ["был", "Был", "куда", "они", "их", "тут", "кому"] {
            XCTAssertTrue(kazakh.isListedInOtherLanguage(WordList.key(for: word)), word)
            XCTAssertNil(kazakh.kazakhLetterHint(for: word), word)
            let strip = kazakh.suggestions(for: TypedWord(text: word))
            XCTAssertNil(strip.correction, word)
            for wrong in ["біл", "Біл", "құда", "өңі", "іх", "тұт", "көму"] {
                XCTAssertFalse(strip.items.contains { $0.text == wrong }, "\(word) → \(wrong)")
            }
        }
        XCTAssertFalse(try AutocorrectFixtures.engine(.russian).isListedInOtherLanguage("был"), "Russian searches one list")
    }

    /// Kazakh typed with plain letters keeps its hint, whether or not a
    /// Russian web text ever contained that spelling: only the Russian word
    /// list (not the Russian known filter, which has "кайда" and "биз")
    /// takes the hint away.
    func testKazakhPlainSpellingsKeepTheirHint() throws {
        let kazakh = try AutocorrectFixtures.engine(.kazakh)
        let expected = ["кайда": "қайда", "калайсын": "қалайсың", "бугин": "бүгін", "биз": "біз", "сиз": "сіз"]
        for (typed, hint) in expected {
            XCTAssertFalse(kazakh.isListedInOtherLanguage(typed), typed)
            XCTAssertEqual(kazakh.suggestions(for: TypedWord(text: typed)).items.first, AutocorrectSuggestion(text: hint, kind: .word), typed)
            XCTAssertNil(correction(typed, kazakh), typed)
        }
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

    /// Regression (parity with Android): a word typed with a Kazakh letter
    /// is Kazakh, so the Russian list is not searched for it at all. Before,
    /// iOS let Russian words compete: "қном" became "гном", "душі" became
    /// "душу", and Russian "арену" crowded out the fix "үрену" → "үйрену".
    func testKazakhLetterWordsGetNothingFromTheRussianList() throws {
        let kazakh = try AutocorrectFixtures.engine(.kazakh)
        XCTAssertEqual(correction("үрену", kazakh), "үйрену")
        let russianOnly: [String: String] = ["қном": "гном", "душі": "душу", "бғды": "беды", "кағое": "какое", "важғости": "важности", "үрену": "арену"]
        for (typed, russian) in russianOnly {
            XCTAssertFalse(kazakh.candidates(for: typed, limit: 10).contains { $0.word == russian }, "\(typed) → \(russian)")
            XCTAssertNotEqual(correction(typed, kazakh), russian, typed)
        }
        for typed in ["қном", "душі", "бғды", "кағое", "важғости"] {
            XCTAssertNil(correction(typed, kazakh), typed)
        }
        XCTAssertTrue(kazakh.candidates(for: "превет", limit: 3).contains { $0.word == "привет" }, "plain letters still search Russian")
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

    // MARK: Instructions

    /// What people type into ✨ Create (and a reply's instruction) to tell
    /// the AI what to write - short requests, often one word - stays exactly
    /// as typed: no word of it is changed by a space, a comma or Return, on
    /// any layout, lower-case or capitalised at the start as the composer
    /// types it. (Russian words on the Kazakh layout included.)
    func testInstructionsAreLeftAsTyped() throws {
        let cases: [(KeyboardLanguage, [String])] = [
            (.russian, ["вежливо откажи", "да", "согласен", "скажи, что согласен", "ответь, что приду завтра",
                        "поблагодари и откажи", "нет"]),
            (.kazakh, ["иә", "жоқ", "сыпайы бас тарт", "келісемін", "келісетінімді айт", "рахмет айт",
                       "вежливо откажи", "да", "скажи, что согласен"]),
            (.english, ["yes", "no", "politely decline", "say I agree", "tell him I'm busy", "thank her and say no"])
        ]
        for (language, instructions) in cases {
            let engine = try AutocorrectFixtures.engine(language)
            for instruction in instructions {
                for typed in [instruction, instruction.prefix(1).uppercased() + instruction.dropFirst()] {
                    for end in Self.wordEnds(in: typed) {
                        let before = String(typed[..<end])
                        XCTAssertNil(correction(before, engine), "\(language.rawValue): «\(before)» in «\(typed)»")
                    }
                }
            }
        }
    }

    /// Where each word of `text` ends: where a separator would be typed.
    private static func wordEnds(in text: String) -> [String.Index] {
        var ends: [String.Index] = []
        var index = text.startIndex
        while index < text.endIndex {
            let next = text.index(after: index)
            if text[index].isLetter, next == text.endIndex || !(text[next].isLetter || text[next] == "'") {
                ends.append(next)
            }
            index = next
        }
        return ends
    }

    private func correction(_ text: String, _ engine: AutocorrectEngine, session: AutocorrectSession? = nil) -> String? {
        guard let word = TypedWord(before: text) else { return nil }
        return engine.correction(for: word, session: session)?.replacement
    }

    /// The automatic correction, or else the first entry of the strip.
    private func firstChoice(_ text: String, _ engine: AutocorrectEngine) -> String? {
        correction(text, engine) ?? engine.suggestions(for: TypedWord(text: text)).items.first?.text
    }
}
