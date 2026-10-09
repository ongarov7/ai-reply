import XCTest
@testable import AIReply

/// Shift, auto-capitalisation, the double-space full stop and word delete:
/// the typing rules users feel on every sentence.
final class KeyboardTypingTests: XCTestCase {

    // MARK: Shift

    func testSingleTapShiftsOneLetter() {
        var shift = ShiftState()
        shift.tap(at: 10)
        XCTAssertEqual(shift.mode, .once)
        XCTAssertEqual(shift.apply(to: "ә"), "Ә")
        shift.characterTyped()
        XCTAssertEqual(shift.mode, .off)
        XCTAssertEqual(shift.apply(to: "і"), "і")
    }

    func testDoubleTapLocksAndOneTapUnlocks() {
        var shift = ShiftState()
        shift.tap(at: 10)
        shift.tap(at: 10.2)
        XCTAssertEqual(shift.mode, .locked)
        shift.characterTyped()
        shift.characterTyped()
        XCTAssertEqual(shift.mode, .locked, "caps lock survives typing")
        shift.tap(at: 11)
        XCTAssertEqual(shift.mode, .off)
    }

    func testSlowSecondTapTurnsShiftOffInsteadOfLocking() {
        var shift = ShiftState()
        shift.tap(at: 10)
        shift.tap(at: 11)
        XCTAssertEqual(shift.mode, .off)
    }

    func testTypingBetweenTapsIsNotADoubleTap() {
        var shift = ShiftState()
        shift.tap(at: 10)
        shift.characterTyped()
        shift.tap(at: 10.1)
        XCTAssertEqual(shift.mode, .once)
    }

    func testAutomaticShiftComesAndGoesButManualShiftStays() {
        var shift = ShiftState()
        shift.applyAutomatic(true)
        XCTAssertEqual(shift.mode, .once)
        shift.applyAutomatic(false)
        XCTAssertEqual(shift.mode, .off, "an automatic shift leaves with the sentence start")

        shift.tap(at: 20)
        shift.applyAutomatic(false)
        XCTAssertEqual(shift.mode, .once, "a manual shift is never overridden")
    }

    func testDoubleTapFromAutomaticShiftLocks() {
        var shift = ShiftState()
        shift.applyAutomatic(true)
        shift.tap(at: 30)
        XCTAssertEqual(shift.mode, .off)
        shift.tap(at: 30.2)
        XCTAssertEqual(shift.mode, .locked)
    }

    // MARK: Auto-capitalisation

    func testSentenceStarts() {
        let sentences = UITextAutocapitalizationType.sentences
        XCTAssertTrue(AutoCapitalization.shouldCapitalize(before: nil, type: sentences))
        XCTAssertTrue(AutoCapitalization.shouldCapitalize(before: "", type: sentences))
        XCTAssertFalse(AutoCapitalization.shouldCapitalize(before: "Сәлем", type: sentences))
        XCTAssertFalse(AutoCapitalization.shouldCapitalize(before: "Сәлем.", type: sentences))
        XCTAssertTrue(AutoCapitalization.shouldCapitalize(before: "Сәлем. ", type: sentences))
        XCTAssertTrue(AutoCapitalization.shouldCapitalize(before: "Правда?  ", type: sentences))
        XCTAssertTrue(AutoCapitalization.shouldCapitalize(before: "Line\n", type: sentences))
        XCTAssertTrue(AutoCapitalization.shouldCapitalize(before: "He said \"Yes.\" ", type: sentences))
        XCTAssertFalse(AutoCapitalization.shouldCapitalize(before: "e.g, ", type: sentences))
    }

    func testOtherCapitalisationModes() {
        XCTAssertFalse(AutoCapitalization.shouldCapitalize(before: "", type: .none))
        XCTAssertTrue(AutoCapitalization.shouldCapitalize(before: "abc", type: .allCharacters))
        XCTAssertTrue(AutoCapitalization.shouldCapitalize(before: "Almaty ", type: .words))
        XCTAssertFalse(AutoCapitalization.shouldCapitalize(before: "Almaty", type: .words))
    }

    // MARK: Double space

    func testDoubleSpaceFullStop() {
        XCTAssertTrue(SpaceShortcut.shouldInsertPeriod(before: "Рахмет ", secondsSinceLastSpace: 0.2))
        XCTAssertTrue(SpaceShortcut.shouldInsertPeriod(before: "at 15 ", secondsSinceLastSpace: 0.2))
        XCTAssertFalse(SpaceShortcut.shouldInsertPeriod(before: "Рахмет ", secondsSinceLastSpace: 1.2))
        XCTAssertFalse(SpaceShortcut.shouldInsertPeriod(before: "Рахмет. ", secondsSinceLastSpace: 0.2))
        XCTAssertFalse(SpaceShortcut.shouldInsertPeriod(before: "Рахмет  ", secondsSinceLastSpace: 0.2))
        XCTAssertFalse(SpaceShortcut.shouldInsertPeriod(before: " ", secondsSinceLastSpace: 0.2))
        XCTAssertFalse(SpaceShortcut.shouldInsertPeriod(before: nil, secondsSinceLastSpace: 0.2))
    }

    // MARK: Word delete

    func testWordDeleteTakesTheWordAndTheSpaceAfterIt() {
        XCTAssertEqual(TextDeletion.wordLength(before: "Hello world"), 5)
        XCTAssertEqual(TextDeletion.wordLength(before: "Hello world  "), 7)
        XCTAssertEqual(TextDeletion.wordLength(before: "Қайырлы таң"), 3)
        XCTAssertEqual(TextDeletion.wordLength(before: "don't"), 5)
        XCTAssertEqual(TextDeletion.wordLength(before: "Сәлем, "), 2)
        XCTAssertEqual(TextDeletion.wordLength(before: "ok 👍"), 1)
        XCTAssertEqual(TextDeletion.wordLength(before: ""), 0)
    }

    // MARK: Host field

    /// Return or Next moves the caret to another field right after the
    /// keyboard's own keystroke: that field is read at once, while an echo of
    /// a keystroke in the same field still is not.
    func testAnotherFieldIsNoticedInsideTheOwnKeystrokeWindow() {
        let message = HostFieldIdentity(documentIdentifier: UUID(), keyboardType: .default)
        var tracker = HostFieldTracker()
        tracker.reset(to: message)

        XCTAssertFalse(tracker.update(message), "the same field")
        XCTAssertTrue(HostFieldTracker.trustsMirror(sinceOwnMutation: 0.1, fieldChanged: false))
        XCTAssertFalse(HostFieldTracker.trustsMirror(sinceOwnMutation: 0.5, fieldChanged: false))

        let email = HostFieldIdentity(documentIdentifier: UUID(), keyboardType: .emailAddress)
        XCTAssertTrue(tracker.update(email), "Next moved to the e-mail field")
        XCTAssertFalse(HostFieldTracker.trustsMirror(sinceOwnMutation: 0.1, fieldChanged: true))
        XCTAssertFalse(tracker.update(email), "and stays there")
    }

    /// A host that keeps one document but swaps the keyboard type (a form
    /// reusing its field) is a new field too.
    func testAKeyboardTypeChangeAloneIsANewField() {
        let document = UUID()
        var tracker = HostFieldTracker()
        tracker.reset(to: HostFieldIdentity(documentIdentifier: document, keyboardType: .default))
        XCTAssertTrue(tracker.update(HostFieldIdentity(documentIdentifier: document, keyboardType: .numberPad)))
    }

    /// The first field seen is where the keyboard appeared, not a move.
    func testTheFirstFieldIsNotAChange() {
        var tracker = HostFieldTracker()
        XCTAssertFalse(tracker.update(HostFieldIdentity(documentIdentifier: UUID(), keyboardType: nil)))
        XCTAssertNotNil(tracker.current)
    }
}
