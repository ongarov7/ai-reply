#import <AVFoundation/AVFoundation.h>

NS_ASSUME_NONNULL_BEGIN

// Keep NSError explicit in Swift. These calls contain Objective-C engine
// exceptions inside Objective-C; no exception is unwound through Swift code.
FOUNDATION_EXPORT BOOL ARVoiceInstallTap(AVAudioEngine *engine,
                                         AVAudioFormat *format,
                                         AVAudioNodeTapBlock block,
                                         NSError * _Nullable * _Nullable error) NS_SWIFT_NOTHROW;
FOUNDATION_EXPORT BOOL ARVoiceStartEngine(AVAudioEngine *engine,
                                          NSError * _Nullable * _Nullable error) NS_SWIFT_NOTHROW;
FOUNDATION_EXPORT BOOL ARVoiceStopEngine(AVAudioEngine *engine,
                                         BOOL removeTap,
                                         NSError * _Nullable * _Nullable error) NS_SWIFT_NOTHROW;

NS_ASSUME_NONNULL_END
