import XCTest
@testable import AIReply

/// Which host fields smart correction leaves alone (DESIGN §5.5).
final class AutocorrectFieldPolicyTests: XCTestCase {

    private func allows(
        _ autocorrection: UITextAutocorrectionType? = .default,
        keyboard: UIKeyboardType? = .default,
        content: UITextContentType? = nil,
        secure: Bool? = false
    ) -> Bool {
        AutocorrectFieldPolicy.allowsCorrection(autocorrection: autocorrection, keyboard: keyboard, contentType: content, isSecure: secure)
    }

    func testOrdinaryTextFieldsAreCorrected() {
        XCTAssertTrue(allows())
        XCTAssertTrue(allows(.yes))
        XCTAssertTrue(allows(nil, keyboard: nil, content: nil, secure: nil), "a host that says nothing is ordinary text")
        XCTAssertTrue(allows(keyboard: .namePhonePad), "a contact name is text")
        XCTAssertTrue(allows(keyboard: .twitter))
        XCTAssertTrue(allows(content: .givenName))
    }

    func testExactFieldsAreLeftAlone() {
        XCTAssertFalse(allows(.no), "the field asked for no correction")
        XCTAssertFalse(allows(secure: true))
        for keyboard: UIKeyboardType in [.URL, .emailAddress, .numberPad, .phonePad, .decimalPad, .asciiCapableNumberPad] {
            XCTAssertFalse(allows(keyboard: keyboard), "\(keyboard.rawValue)")
        }
        for content: UITextContentType in [.URL, .emailAddress, .username, .password, .newPassword, .oneTimeCode, .telephoneNumber] {
            XCTAssertFalse(allows(content: content), content.rawValue)
        }
    }
}
