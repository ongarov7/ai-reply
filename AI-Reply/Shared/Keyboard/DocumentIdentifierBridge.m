#import "DocumentIdentifierBridge.h"

NSUUID * _Nullable AIReplyDocumentIdentifier(id<UITextDocumentProxy> _Nonnull proxy) {
    return proxy.documentIdentifier;
}
