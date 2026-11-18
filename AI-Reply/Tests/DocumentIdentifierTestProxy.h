#import <UIKit/UIKit.h>

NS_ASSUME_NONNULL_BEGIN

/// Reproduces a host returning nil from UIKit's nominally nonnull getter.
@interface DocumentIdentifierTestProxy : NSObject <UITextDocumentProxy>

- (instancetype)initWithIdentifier:(NSUUID * _Nullable)identifier;

@property (nonatomic, copy, nullable) NSUUID *reportedIdentifier;
@property (nonatomic, readonly) NSUInteger documentIdentifierReadCount;

@end

NS_ASSUME_NONNULL_END
