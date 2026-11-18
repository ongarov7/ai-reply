import UIKit

@MainActor
enum HostDocumentIdentity {
    static func identifier(for proxy: UITextDocumentProxy) -> UUID? {
        // Reading proxy.documentIdentifier in Swift bridges to a nonoptional
        // UUID first and traps if the host actually returned nil.
        AIReplyDocumentIdentifier(proxy)
    }
}
