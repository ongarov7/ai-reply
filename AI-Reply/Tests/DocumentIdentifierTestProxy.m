#import "DocumentIdentifierTestProxy.h"

@implementation DocumentIdentifierTestProxy

- (instancetype)initWithIdentifier:(NSUUID * _Nullable)identifier {
    self = [super init];
    if (self) {
        _reportedIdentifier = [identifier copy];
    }
    return self;
}

- (NSUUID *)documentIdentifier {
    _documentIdentifierReadCount += 1;
    // This deliberate contract violation matches the production crash.
    // Reading the Swift UUID getter directly would trap before returning.
    return _reportedIdentifier;
}

- (NSString * _Nullable)documentContextBeforeInput { return nil; }
- (NSString * _Nullable)documentContextAfterInput { return nil; }
- (NSString * _Nullable)selectedText { return nil; }
- (UITextInputMode * _Nullable)documentInputMode { return nil; }
- (UIKeyboardType)keyboardType { return UIKeyboardTypeDefault; }
- (BOOL)hasText { return NO; }
- (void)insertText:(NSString *)text {}
- (void)deleteBackward {}
- (void)adjustTextPositionByCharacterOffset:(NSInteger)offset {}
- (void)setMarkedText:(NSString *)markedText selectedRange:(NSRange)selectedRange {}
- (void)unmarkText {}

@end
