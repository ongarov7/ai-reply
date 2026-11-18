#import "VoiceAudioEngineBridge.h"
#import <math.h>

static NSString *const ARVoiceEngineErrorDomain = @"kz.ai-reply.voice.audio-engine";

static NSError *ARVoiceEngineError(NSInteger code, NSString *operation, NSException *exception) {
    NSMutableDictionary *details = [@{NSLocalizedDescriptionKey: operation} mutableCopy];
    if (exception) {
        // Diagnostic metadata only: callers show their own localized generic
        // recording error. Never attach buffers, transcripts or message text.
        details[@"exception_name"] = exception.name ?: @"NSException";
        NSString *reason = exception.reason ?: @"";
        details[@"exception_reason"] = [reason substringToIndex:MIN(reason.length, 512)];
    }
    return [NSError errorWithDomain:ARVoiceEngineErrorDomain code:code userInfo:details];
}

BOOL ARVoiceInstallTap(AVAudioEngine *engine, AVAudioFormat *format,
                       AVAudioNodeTapBlock block, NSError **error) {
    if (error) *error = nil;
    @try {
        const AudioStreamBasicDescription *description = format.streamDescription;
        if (!engine || !format || !block || !description ||
            !isfinite(format.sampleRate) || format.sampleRate <= 0 ||
            format.channelCount == 0 || format.commonFormat == AVAudioOtherFormat ||
            description->mChannelsPerFrame == 0 || description->mBytesPerFrame == 0) {
            if (error) *error = ARVoiceEngineError(1, @"Invalid microphone input format", nil);
            return NO;
        }
        [engine.inputNode installTapOnBus:0 bufferSize:4096 format:format block:block];
        return YES;
    } @catch (NSException *exception) {
        if (error) *error = ARVoiceEngineError(2, @"Microphone tap installation failed", exception);
        return NO;
    }
}

BOOL ARVoiceStartEngine(AVAudioEngine *engine, NSError **error) {
    if (error) *error = nil;
    if (!engine) {
        if (error) *error = ARVoiceEngineError(1, @"Missing audio engine", nil);
        return NO;
    }
    @try {
        [engine prepare];
        return [engine startAndReturnError:error];
    } @catch (NSException *exception) {
        if (error) *error = ARVoiceEngineError(3, @"Microphone engine start failed", exception);
        return NO;
    }
}

BOOL ARVoiceStopEngine(AVAudioEngine *engine, BOOL removeTap, NSError **error) {
    if (error) *error = nil;
    if (!engine) return YES;
    NSError *failure = nil;
    @try {
        [engine stop];
    } @catch (NSException *exception) {
        failure = ARVoiceEngineError(4, @"Microphone engine stop failed", exception);
    }
    // Even a stop failure must not prevent the separately guarded tap cleanup.
    // The caller tracks successful installation and passes NO otherwise.
    if (removeTap) {
        @try {
            [engine.inputNode removeTapOnBus:0];
        } @catch (NSException *exception) {
            if (!failure) failure = ARVoiceEngineError(5, @"Microphone tap removal failed", exception);
        }
    }
    if (error) *error = failure;
    return failure == nil;
}
