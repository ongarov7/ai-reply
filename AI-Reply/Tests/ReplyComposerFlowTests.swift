import XCTest
@testable import AIReply

/// The composer's state machine: Generate, Edit, Regenerate, Insert, Stop,
/// failures - and the promises that no tap loses the user's work.
final class ReplyComposerFlowTests: XCTestCase {

    private func flowWithReply(_ text: String = "Иә, ертең жеткіземіз 🚚") -> ReplyComposerFlow {
        var flow = ReplyComposerFlow()
        XCTAssertTrue(flow.startGeneration())
        flow.receive(reply: text)
        return flow
    }

    // MARK: Generate

    func testGenerateShowsTheReply() {
        var flow = ReplyComposerFlow()
        XCTAssertTrue(flow.startGeneration())
        XCTAssertEqual(flow.stage, .generating(from: .composing))
        flow.receive(reply: "Hello")
        XCTAssertEqual(flow.stage, .result)
        XCTAssertEqual(flow.draftText, "Hello")
        XCTAssertEqual(flow.drafts.count, 1)
    }

    /// Rapid taps cannot start a second request.
    func testOnlyOneRequestAtATime() {
        var flow = ReplyComposerFlow()
        XCTAssertTrue(flow.startGeneration())
        XCTAssertFalse(flow.startGeneration())
        XCTAssertFalse(flow.startGeneration())
        XCTAssertEqual(flow.stage, .generating(from: .composing))
    }

    func testFailureReturnsToWhereTheRequestStarted() {
        var composing = ReplyComposerFlow()
        composing.startGeneration()
        composing.fail(.offline)
        XCTAssertEqual(composing.stage, .composing)
        XCTAssertEqual(composing.error, .offline)

        var result = flowWithReply()
        result.startGeneration()
        result.fail(.quotaExhausted)
        XCTAssertEqual(result.stage, .result)
        XCTAssertEqual(result.draftText, "Иә, ертең жеткіземіз 🚚", "the reply on screen survives a failed regeneration")
        XCTAssertEqual(result.error, .quotaExhausted)
    }

    func testStopKeepsEverythingAndIgnoresTheLateAnswer() {
        var flow = flowWithReply("First")
        flow.startGeneration()
        flow.cancelGeneration()
        XCTAssertEqual(flow.stage, .result)
        XCTAssertNil(flow.error)
        flow.receive(reply: "Late")
        XCTAssertEqual(flow.drafts.count, 1, "an answer to a stopped request must not appear")
        XCTAssertEqual(flow.draftText, "First")
    }

    // MARK: Regenerate

    /// The heart of it: Regenerate never destroys an edit.
    func testRegenerateAddsAVersionAndKeepsTheEditedOne() {
        var flow = flowWithReply("Сәлем! Ертең болады.")
        XCTAssertTrue(flow.beginEditingForTyping())
        flow.editDraft("Сәлем, Айгүл! Ертең болады.")
        flow.endEditing()

        XCTAssertTrue(flow.startGeneration())
        XCTAssertEqual(flow.stage, .generating(from: .result))
        XCTAssertTrue(flow.showsReply, "the reply stays on screen while the new one is made")
        flow.receive(reply: "Second version")

        XCTAssertEqual(flow.drafts.count, 2)
        XCTAssertEqual(flow.draftText, "Second version")
        flow.showPreviousVersion()
        XCTAssertEqual(flow.draftText, "Сәлем, Айгүл! Ертең болады.")
        XCTAssertTrue(flow.drafts.current?.isEdited ?? false)
    }

    func testVersionNavigationStopsAtTheEnds() {
        var flow = flowWithReply("A")
        flow.startGeneration()
        flow.receive(reply: "B")
        flow.showNextVersion()
        XCTAssertEqual(flow.draftText, "B")
        flow.showPreviousVersion()
        flow.showPreviousVersion()
        XCTAssertEqual(flow.draftText, "A")
        XCTAssertEqual(flow.drafts.position, 1)
    }

    // MARK: Edit

    func testTypingOnTheReplyStartsEditing() {
        var flow = flowWithReply()
        XCTAssertTrue(flow.beginEditingForTyping())
        XCTAssertEqual(flow.stage, .editing)
        var composing = ReplyComposerFlow()
        XCTAssertFalse(composing.beginEditingForTyping())
    }

    func testBackKeepsTheVersions() {
        var flow = flowWithReply("A")
        flow.back()
        XCTAssertEqual(flow.stage, .composing)
        flow.startGeneration()
        flow.receive(reply: "B")
        XCTAssertEqual(flow.drafts.count, 2)
    }

    // MARK: Insert

    func testInsertTakesTheEditedTextExactly() {
        var flow = flowWithReply("Original")
        _ = flow.beginEditingForTyping()
        let edited = "Қайырлы кеш! 😊\nЕртең 15:00-де."
        flow.editDraft(edited)
        XCTAssertEqual(flow.requestInsert(hostHasText: false), .insert(edited))
    }

    func testNonEmptyHostFieldAsksFirstAndCancelGoesBack() {
        var flow = flowWithReply("Reply")
        _ = flow.beginEditingForTyping()
        XCTAssertEqual(flow.requestInsert(hostHasText: true), .askAboutExistingText)
        XCTAssertEqual(flow.stage, .conflict(returnTo: .result, wasEditing: true))
        XCTAssertEqual(flow.resolveConflict(.cancel), .cancelled)
        XCTAssertEqual(flow.stage, .editing, "cancel returns to exactly where the user was")

        _ = flow.requestInsert(hostHasText: true)
        XCTAssertEqual(flow.resolveConflict(.append), .append("Reply"))
        _ = flow.requestInsert(hostHasText: true)
        XCTAssertEqual(flow.resolveConflict(.replace), .replace("Reply"))
    }

    func testNothingToInsert() {
        var composing = ReplyComposerFlow()
        XCTAssertEqual(composing.requestInsert(hostHasText: false), .nothing)
        var blank = flowWithReply("Text")
        _ = blank.beginEditingForTyping()
        blank.editDraft("   \n ")
        XCTAssertEqual(blank.requestInsert(hostHasText: false), .nothing)
    }

    // MARK: Resume

    func testResumeAfterChangingPersona() {
        var flow = flowWithReply("A")
        _ = flow.requestInsert(hostHasText: true)
        flow.resume()
        XCTAssertEqual(flow.stage, .result)

        var generating = ReplyComposerFlow()
        generating.startGeneration()
        generating.resume()
        XCTAssertEqual(generating.stage, .composing)
    }

    // MARK: History

    func testHistoryDropsTheOldestUneditedVersionWhenFull() {
        var history = ReplyDraftHistory()
        history.append(generated: "0")
        history.edit("0 edited")
        for index in 1...ReplyDraftHistory.capacity {
            history.append(generated: "\(index)")
        }
        XCTAssertEqual(history.count, ReplyDraftHistory.capacity)
        XCTAssertEqual(history.versions.first?.text, "0 edited", "an edited version is kept")
        XCTAssertEqual(history.currentText, "\(ReplyDraftHistory.capacity)")
    }
}
