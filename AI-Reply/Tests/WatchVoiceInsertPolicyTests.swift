import XCTest
@testable import AIReply

/// The one rule for text reaching another app's field by itself.
final class WatchVoiceInsertPolicyTests: XCTestCase {

    private let document = UUID()

    private func context(session: String = "session-1", document: UUID? = nil, before: String = "", after: String = "") -> WatchInsertContext {
        WatchInsertContext(sessionID: session, documentID: document ?? self.document, textBeforeCaret: before, textAfterCaret: after)
    }

    private func decide(enabled: Bool = true, bound: WatchInsertContext?, current: WatchInsertContext?,
                        inserted: Bool = false) -> WatchInsertDecision {
        WatchInsertPolicy.decide(isAutoInsertEnabled: enabled, bound: bound, current: current, alreadyInserted: inserted)
    }

    func testTheSameEmptyFieldInsertsByItself() {
        XCTAssertEqual(decide(bound: context(), current: context()), .automatic)
    }

    func testTheSwitchOffMeansManual() {
        XCTAssertEqual(decide(enabled: false, bound: context(), current: context()), .manual(.preferenceOff))
    }

    func testACommandNotMeantForThisFieldIsManual() {
        XCTAssertEqual(decide(bound: nil, current: context()), .manual(.notBound))
    }

    func testAKeyboardOffScreenIsManual() {
        XCTAssertEqual(decide(bound: context(), current: nil), .manual(.keyboardNotVisible))
    }

    func testAnotherSessionIsManual() {
        XCTAssertEqual(decide(bound: context(), current: context(session: "session-2")), .manual(.sessionChanged))
    }

    func testAnotherDocumentIsManual() {
        XCTAssertEqual(decide(bound: context(), current: context(document: UUID())), .manual(.documentChanged))
    }

    func testAnUnverifiableDocumentIsManual() {
        let none = WatchInsertContext(sessionID: "session-1", documentID: nil, textBeforeCaret: "", textAfterCaret: "")
        XCTAssertEqual(decide(bound: none, current: none), .manual(.documentUnknown))
    }

    func testTheZeroUUIDCountsAsNoDocument() {
        XCTAssertNil(WatchInsertContext.validDocumentID(UUID(uuidString: "00000000-0000-0000-0000-000000000000")))
        XCTAssertNil(WatchInsertContext.validDocumentID(nil))
        let real = UUID()
        XCTAssertEqual(WatchInsertContext.validDocumentID(real), real)
    }

    /// A document identifier does not prove it is the same chat.
    func testTextThatChangedIsManual() {
        XCTAssertEqual(decide(bound: context(), current: context(before: "Другой чат")), .manual(.textChanged))
        XCTAssertEqual(decide(bound: context(), current: context(after: "хвост")), .manual(.textChanged))
    }

    func testWhatTheUserWroteIsNeverOverwrittenByItself() {
        let written = context(before: "Мой текст")
        XCTAssertEqual(decide(bound: written, current: written), .manual(.fieldHasText))
    }

    /// A selection replaced by `insertText` is text the user would lose.
    func testASelectionIsNotAnEmptyField() {
        let selected = WatchInsertContext(sessionID: "session-1", documentID: document, textBeforeCaret: "",
                                          textAfterCaret: "", selectedText: "Выделено")
        XCTAssertFalse(selected.isFieldEmpty)
        XCTAssertEqual(decide(bound: selected, current: selected), .manual(.fieldHasText))
        XCTAssertEqual(decide(bound: context(), current: selected), .manual(.textChanged))
    }

    func testOnlySpacesCountAsEmpty() {
        XCTAssertTrue(context(before: "  \n").isFieldEmpty)
        XCTAssertFalse(context(after: "a").isFieldEmpty)
    }

    func testAlreadyInsertedIsNeverInsertedAgain() {
        XCTAssertEqual(decide(bound: context(), current: context(), inserted: true), .manual(.alreadyInserted))
    }

    func testTheSwitchIsOnByDefaultAndRemembered() throws {
        let suite = "watch.autoinsert.\(UUID().uuidString)"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }
        let settings = SharedSettings(defaults: defaults)
        XCTAssertTrue(settings.watchAutoInsertEnabled)
        settings.setWatchAutoInsertEnabled(false)
        XCTAssertFalse(settings.watchAutoInsertEnabled)
    }

    // MARK: Connection

    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    private func link(paired: Bool = true, installed: Bool = true, reachable: Bool = false,
                      updatedAgo: TimeInterval = 0, contactAgo: TimeInterval? = nil) -> WatchLinkSnapshot {
        WatchLinkSnapshot(isSupported: true, isActivated: true, isPaired: paired, isWatchAppInstalled: installed, isReachable: reachable,
                          updatedAt: now.addingTimeInterval(-updatedAgo),
                          lastWatchContactAt: contactAgo.map { now.addingTimeInterval(-$0) })
    }

    func testPairedIsNotInstalledAndInstalledIsNotReachable() {
        XCTAssertEqual(link(paired: false, installed: false).connectionState(now: now), .notPaired)
        XCTAssertEqual(link(installed: false).connectionState(now: now), .appNotInstalled)
        XCTAssertEqual(link().connectionState(now: now), .appNotOpen)
        XCTAssertEqual(link(reachable: true).connectionState(now: now), .connected)
    }

    func testAReachabilityFromLongAgoIsNotBelieved() {
        XCTAssertEqual(link(reachable: true, updatedAgo: 3600).connectionState(now: now), .appNotOpen,
                       "a suspended app cannot report the watch leaving")
    }

    func testARecentMessageFromTheWatchIsProofEnough() {
        XCTAssertEqual(link(contactAgo: 10).connectionState(now: now), .connected)
        XCTAssertEqual(link(installed: false, contactAgo: 10).connectionState(now: now), .connected, "installed lags behind")
        XCTAssertEqual(link(contactAgo: 120).connectionState(now: now), .appNotOpen)
    }

    func testAnUnsupportedDevice() {
        let unsupported = WatchLinkSnapshot(updatedAt: now)
        XCTAssertEqual(unsupported.connectionState(now: now), .unsupported)
    }

    // MARK: The row

    func testTheRowIsUntouchedWithoutAWatch() {
        let texts: [CGFloat] = [27, 47, 54, 58]
        let plain = PersonaRowLayout(width: 390, textWidths: texts)
        let explicit = PersonaRowLayout(width: 390, textWidths: texts, trailingActionWidth: 0)
        XCTAssertEqual(plain, explicit)
        XCTAssertNil(plain.trailingFrame)
    }

    func testTheControlDoesNotOverlapThePersonas() throws {
        let row = PersonaRowLayout(width: 390, textWidths: [27, 47, 54, 58], trailingActionWidth: PersonaRowLayout.watchActionWidth)
        let control = try XCTUnwrap(row.trailingFrame)
        XCTAssertLessThanOrEqual(row.personasFrame.maxX, control.minX + 0.5)
        XCTAssertEqual(control.maxX, 390 - PersonaRowLayout.edgeInset, accuracy: 0.5)
        XCTAssertEqual(row.createFrame.width, PersonaRowLayout.actionWidth, "✨ stays as it is")
    }

    func testFourKazakhPersonasStillFitOnA61InchPhone() {
        let row = PersonaRowLayout(width: 390, textWidths: [27, 47, 54, 58], trailingActionWidth: PersonaRowLayout.watchActionWidth)
        XCTAssertFalse(row.scrolls)
    }
}
