import UIKit

/// Whether the host field wants typing help at all (DESIGN §5.5).
///
/// Addresses, handles, codes and passwords are typed exactly: a "fixed" e-mail
/// or one-time code costs the user far more than a missed typo. The field says
/// what it is through its text traits, and the keyboard reads them when it
/// appears or the host changes the text - never on a keystroke.
enum AutocorrectFieldPolicy {

    /// Keyboards for numbers, addresses and phone numbers. `.namePhonePad` is
    /// left out on purpose: a contact name is ordinary text.
    static let exactKeyboards: Set<UIKeyboardType> = [
        .URL, .emailAddress, .numberPad, .phonePad, .decimalPad, .asciiCapableNumberPad
    ]

    static let exactContentTypes: Set<UITextContentType> = [
        .URL, .emailAddress, .username, .password, .newPassword, .oneTimeCode, .telephoneNumber
    ]

    static func allowsCorrection(
        autocorrection: UITextAutocorrectionType?,
        keyboard: UIKeyboardType?,
        contentType: UITextContentType?,
        isSecure: Bool?
    ) -> Bool {
        if autocorrection == .no || isSecure == true { return false }
        if let keyboard, exactKeyboards.contains(keyboard) { return false }
        if let contentType, exactContentTypes.contains(contentType) { return false }
        return true
    }

    /// The same decision for a live text proxy.
    static func allowsCorrection(in traits: UITextInputTraits) -> Bool {
        allowsCorrection(
            autocorrection: traits.autocorrectionType,
            keyboard: traits.keyboardType,
            contentType: traits.textContentType ?? nil,
            isSecure: traits.isSecureTextEntry
        )
    }
}
