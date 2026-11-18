#import <UIKit/UIKit.h>

// Some host fields return nil despite UIKit's nonnull declaration. Keep the
// Objective-C result nullable so Swift checks it before bridging NSUUID to UUID.
NSUUID * _Nullable AIReplyDocumentIdentifier(id<UITextDocumentProxy> _Nonnull proxy);
