# Build 29 keyboard crash diagnosis — 18 November 2026

Xcode Organizer was refreshed against App Store Connect and version 1.3.0 was
selected. The newly downloaded TestFlight crash group contains four occurrences
on iPhone 16 Pro / iOS 27.2.1 (24B156): 18:00:12, 18:01:50, 18:04:27 and
18:04:40 Asia/Almaty. All identify **ReplyKeyboardExtension**, build 29, and
`EXC_CRASH (SIGABRT)` / `SIGNAL 6 Abort trap: 6`.

The exception backtrace is consistent across all four reports. Three contain
symbols; the remaining report's extension offsets match the same frames:

```text
CoreFoundation __exceptionPreprocess
libobjc objc_exception_throw
Foundation -[NSAssertionHandler handleFailureInFunction:file:lineNumber:description:]
UIKitCore _UIApplicationAssertForExtensionType
UIKitCore _UIAlertControllerCommonInit
UIKitCore -[UIAlertController initWithNibName:bundle:]
UIKitCore +[UIAlertController alertControllerWithTitle:message:preferredStyle:]
ReplyKeyboardExtension UIAlertController.__allocating_init(...) [KeyboardVoiceAssistant.swift:172]
ReplyKeyboardExtension KeyboardVoiceAssistant.showNotice(_:from:)
ReplyKeyboardExtension KeyboardVoiceAssistant.menu(from:) [KeyboardVoiceAssistant.swift:127]
ReplyKeyboardExtension KeyboardViewController.actionBarDidTapVoiceAssistant(_:) [KeyboardViewController.swift:1355]
ReplyKeyboardExtension KeyboardActionBar.templateBarDidTapVoiceAssistant(_:) [KeyboardActionBar.swift:343]
ReplyKeyboardExtension TemplateBarView.microphoneTapped() [TemplateBarView.swift:405]
```

This establishes a UIKit extension-type assertion during creation of the voice
notice `UIAlertController`. It does not implicate AVAudioEngine, AVAudioConverter
or the backend provider. The downloaded text reports do not include a separate
exception reason string, so no such string is asserted here.

All four keyboard voice alert constructors need keyboard-owned inline views:
conversation insertion confirmation, voice menu, notices, and replacement of an
existing draft. The last constructor is in KeyboardViewController, the others in
KeyboardVoiceAssistant. Merely suppressing the generic error status does not
remove the crashing alert-construction path.

The backend's crash notifications intentionally contain only exception/signal,
OS/build and grouped count; they contain neither stack traces nor a raw crash
payload. Their `source=app` describes telemetry origin, not the crashed process,
and their timestamps are report delivery times. No user message, microphone
recording, identifier or credential was read to obtain this diagnosis.

Separately, filtered production metadata shows successful audio transcriptions
at 18:01:09 (5.285 seconds) and 18:01:34 (2.993 seconds). The first completed
normal compose generation with HTTP 200 at 18:01:16. A later socket ended after
11.990 seconds, consistent with the client's 12-second no-speech timeout; the
server's generic client-close error code alone does not prove its cause.

Sanitized report excerpts and original local-cache source paths are retained in
`outputs/release-1.3.0-29/crash-diagnostics/` (Git-ignored). Four distinct
occurrences are preserved; three have symbolized application frames. Raw report
contents were not exported: reporter/device identifiers and installation paths
are omitted from excerpts.

A read-only audit of the subsequent background-start source found one direct
background capture caller: StartVoiceAssistantIntent, conforming to public
AudioRecordingIntent and LiveActivityIntent. It requires local device
authentication and existing app-side microphone/provider consent, signed-in
account and current legal consent. Its app-only body awaits activation through
the shared controller. Generic keyboard commands have no start operation;
foreground manual capture remains active-app gated. A required Live Activity is
created before the system-control capture and dismissal stops recording. This
source audit does not substitute for physical-device execution of the new flow.
