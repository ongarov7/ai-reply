import UIKit
import XCTest
@testable import AIReply

@MainActor
final class DocumentIdentifierBridgeTests: XCTestCase {

    /// The mock returns nil from the Objective-C getter, rather than from a
    /// Swift optional helper, so this exercises the bridge that used to trap.
    func testNilFromTheNonnullUIKitGetterDoesNotCrash() {
        let proxy = DocumentIdentifierTestProxy(identifier: nil)

        XCTAssertNil(HostDocumentIdentity.identifier(for: proxy))
        XCTAssertEqual(proxy.documentIdentifierReadCount, 1)
    }

    func testARealDocumentIdentifierSurvivesTheBridge() {
        let identifier = UUID()
        let proxy = DocumentIdentifierTestProxy(identifier: identifier)

        XCTAssertEqual(HostDocumentIdentity.identifier(for: proxy), identifier)
        XCTAssertEqual(proxy.documentIdentifierReadCount, 1)
    }

    func testAnIdentifierCanDisappearAndReturnWithoutUsingAStaleValue() {
        let first = UUID()
        let second = UUID()
        let proxy = DocumentIdentifierTestProxy(identifier: first)
        XCTAssertEqual(HostDocumentIdentity.identifier(for: proxy), first)

        proxy.reportedIdentifier = nil
        XCTAssertNil(HostDocumentIdentity.identifier(for: proxy))

        proxy.reportedIdentifier = second
        XCTAssertEqual(HostDocumentIdentity.identifier(for: proxy), second)
        XCTAssertEqual(proxy.documentIdentifierReadCount, 3)
    }

    func testAMissingHostIdentifierKeepsWatchInsertionManual() {
        let proxy = DocumentIdentifierTestProxy(identifier: nil)
        let identifier = HostDocumentIdentity.identifier(for: proxy)
        let context = WatchInsertContext(
            sessionID: "keyboard-session",
            documentID: WatchInsertContext.validDocumentID(identifier),
            textBeforeCaret: "",
            textAfterCaret: ""
        )

        XCTAssertEqual(
            WatchInsertPolicy.decide(isAutoInsertEnabled: true, bound: context, current: context,
                                     alreadyInserted: false),
            .manual(.documentUnknown)
        )
    }
}
