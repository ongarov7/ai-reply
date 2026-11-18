# AI Reply Voice for iPhone

Implementation and release guide for iOS 1.3.0 (32), 18 November 2026.

## Implemented flow

The containing iPhone app owns microphone capture. A user starts it from the
Home toolbar microphone, an App Shortcut, or the AI Reply Voice control they
manually add to Control Center on iOS 18 or later. After first-time app setup,
the control uses an intent conforming to both `AudioRecordingIntent` and
`LiveActivityIntent` to start in the containing app process without opening its
interface. Start requires local device authentication, previously granted
microphone/provider permission, valid account/AI consent and a successfully
started Live Activity. Missing permission produces setup guidance rather than
requesting permission silently. iOS 17 supports the app and the discoverable
Open AI Reply Voice action in Shortcuts.

The intent directly awaits `VoiceAssistantController.shared.startFromControl()`
until the Activity and audio engine have started, preserving the system intent
execution context. The app-specific Swift compilation condition
`AIREPLY_MAIN_APP` selects the real controller implementation only in the app;
the widget's metadata implementation never touches audio hardware. On iOS 26,
the intent explicitly declares background supported mode. The first suggested
shortcut on iOS 18 is AI Reply Voice; Set up AI Reply Voice opens the app.

Before the first recording, a separate permission alert identifies OpenAI
(USA), explains the audio/text processing, and offers Allow and Start or Not
Now. iOS microphone permission is requested separately. Voice permission can be
withdrawn in Settings → Privacy; this stops capture and clears private session
context. Account sign-out and withdrawal of general AI consent also clear it.

Capture uses AVAudioEngine, the existing audio-session lifecycle helper, and
PCM16 mono audio at 24 kHz. Tap Done in the app, keyboard or expanded Dynamic
Island to finish and generate. Short speech pauses do not end a turn. The
40-second maximum ends capture and processes available speech; 20 seconds of
initial silence ends with a recoverable error. Cancel discards the turn; Pause
releases the microphone. Starting again is a new user-triggered turn, through
the prepared system control or app. An active recording can continue while
switching apps where iOS permits it. Audio is not written to disk; silent
playback is not used to prolong execution. Interruptions, route removal and
media-services resets release microphone resources.

Realtime transcription uses the authenticated AI Reply backend. Provider
partials update the prominent app transcript card while capture continues.
The app publishes its protected snapshot at a bounded 0.5-second interval;
the keyboard shows a two-line live caption with the newest text visible.
Before any text arrives, both interfaces prompt the user to speak. The compact
app status/timer, transcript and explicit Done/Cancel controls precede context
options, keeping capture controls near the top of the sheet. Once the final
transcript arrives, an incoming message plus voice instruction goes through the
existing AIReplyService and selected template. Without message context, the
existing ComposeService creates standalone text. The existing profile, account,
subscriptions, reply limits and idempotency workflow are retained. Russian,
Kazakh and English speech preferences are available; automatic mode supplies
multilingual hints. Output language follows the existing generation rules and
any explicit instruction. Voice quality and mixed-language accuracy need
physical-device evaluation; no accuracy guarantee is implied.

## Control Center, Dynamic Island and Live Activities

On iOS 18+, open Control Center, hold an empty area, choose Add a Control and
select AI Reply Voice. The app cannot install this control automatically. It
can also be selected as a supported Lock Screen control; the AI Reply Voice
shortcut can be assigned to the Action Button through system settings.

The control uses `AIReplyVoiceControl`, a monochrome custom symbol: AI Reply's
overlapping conversation bubbles with a microphone in the front bubble. It is a
**variable template (v.3)** with Ultralight-S, Regular-S and Black-S sources, so
Xcode derives every weight and scale. Control Center and the Dynamic Island draw
it in SpringBoard, outside the app: SpringBoard logged `No image named
'AIReplyControl' found in asset catalog` for build 31's single Regular-M (v.2)
glyph and drew an empty circle, although actool accepted it and the app itself
could render it. With the v.3 template the iOS 26.5 simulator's control gallery
and Control Center draw the branded glyph. Control Center caches a control's
template per install, so verification needs a fresh install. The Live Activity
uses the same glyph.

The public ControlWidgetButton/Toggle API has one intent action per control,
not an app-defined runtime long-press menu. Apple's configurable-control API
opens configuration on long press **in edit mode**; it does not provide a
Shazam-style custom action sheet during normal use. A second user-added
control or an edit-mode configuration option is a possible future native
alternative. Build 32 preserves tap-to-record and the existing explicit copied
message action in the keyboard; it adds no undocumented long-press handler.

A finite ActivityKit task shows microphone and processing status on compatible
Dynamic Island iPhones and on the Lock Screen. Expanded Island controls are:

- Checkmark: finish dictation and generate a response.
- Pause: release the microphone and pause the turn.
- Cancel (X): discard the turn or cancel processing.

Preparing and processing states show progress plus Cancel, not a false ready
microphone. Completion shows that the reply waits in the keyboard during a
brief, approximately three-second completion state. The compact Island shows
the brand glyph and a system-drawn elapsed timer; the Lock Screen shows the
glyph, status, timer, Done and Cancel. Activity text follows the app language
(`VoiceAssistantAttributes.language`), not the device language. A timer Text
must get a fixed frame: `fixedSize()` on it left the whole Lock Screen view
blank in testing.

Activity state supports ready, listening, speech detected, transcribing,
generating, response ready, paused, stopped and error. It carries session
identity, time, status, the recording start and a bounded activity level, never
message, instruction or response text (the published privacy policy promises
this; live words appear in the AI Reply keyboard and app instead). Phase transitions publish immediately; level updates are
throttled. After the brief completion state, the activity ends; the keyboard
retains the short-lived result separately. Activities disabled in
system settings do not disable ordinary keyboard input.

## Clipboard and keyboard

The keyboard never captures microphone audio or opens the containing app. Its
microphone control appears for a relevant active session; an inactive session
does not add an unusable microphone button. Voice actions and notices use
inline extension UI, with no UIAlertController or modal presentation calls.
Full Access is required for shared/network features;
ordinary typing remains available without it.

Message context comes from an explicit system Paste action in the app, typed
input, or Use copied message in the active keyboard microphone menu. Opening
the keyboard and receiving state changes do not read the clipboard. Message
length validation continues to use the existing server-published limit.

The app is the single writer of the protected App Group snapshot. Immutable,
session-bound command files and Darwin notifications carry control requests.
A bounded refresh while visible covers coalesced notifications. Private text
uses Class B file protection (`completeFileProtectionUnlessOpen`) and is
excluded from backups. This permits an atomic write of a new result during
an already-authorized locked recording, while the closed result cannot be
reopened until unlock. Text-free control commands remain usable while locked
after first unlock. A cold app launch while protected data is unavailable does
not read, purge or overwrite the snapshot. On unlock it restores a fresh ready
result, or resets an orphaned turn without resuming capture. Explicit new turns
and privacy resets cancel pending restoration. Records expire three minutes
after their last update and are removed on cleanup.

The keyboard only offers a fresh response in the response-ready state. It
supports explicit review/edit, copy, regenerate and Insert using
`textDocumentProxy.insertText`. A changed or unbound keyboard field requires
confirmation before insertion. Insertion is never automatic and does not press
Send in the host app. Regeneration uses the keyboard's existing composer
service, because a Darwin notification cannot wake a suspended containing app.
System launches clear previous conversation context; it must be supplied again.

## Backend contract and configuration

Production API base: `https://ai-reply.kz`.

`GET /api/v1/config` advertises `features.voice_realtime_transcription`. When
present and true, iOS connects to
`wss://ai-reply.kz/api/v1/ai/transcribe/realtime` with its existing bearer token.
It sends a request ID and validated language hints, waits for ready, streams
bounded base64 PCM frames, and commits one audio turn. The backend returns
filtered delta/final/error events and keeps the provider credential server-side.
The default streaming model is `gpt-live-transcribe`, which supplies partials
before commit. Explicit Done or the recording limit commits the single turn;
short pauses do not commit. No multi-turn protocol or provider rewrite is
required. No detected-language metadata is assumed.

An older server without the feature flag uses the existing authenticated
`POST /api/v1/ai/transcribe` multipart route with a bounded WAV upload. An
advertised streaming request that fails is not silently retried as a second
billable transcription. Final text then uses the existing reply/compose API.
Transcription has its own daily/burst limits; AI generation uses normal reply
quota. No database migration is required for realtime voice.

Backend configuration, wire examples, timeouts, security checks and executable
provider tests are described in
[IOS_VOICE_REALTIME.md](../ai-reply-back-end/docs/IOS_VOICE_REALTIME.md).
Deploy and verify the backend before releasing the client; record actual
deployment and upload outcomes in the release status artifact.

## Files and capabilities

| Area | Implementation |
| --- | --- |
| Recording and orchestration | `AI-Reply/AIReply/Features/VoiceAssistant/` |
| Home entry and lifecycle | `AI-Reply/AIReply/Features/Home/HomeView.swift`, `AI-Reply/AIReply/App/AIReplyApp.swift` |
| Shared state and IPC | `AI-Reply/Shared/VoiceAssistant/` |
| Activity attributes and system intents | `AI-Reply/Shared/VoiceAssistantSystem/` |
| Control and Live Activity extension | `AI-Reply/VoiceWidgets/` |
| Keyboard integration | `KeyboardVoiceAssistant.swift`, `VoiceAssistantStripView.swift`, existing keyboard controller/composer |
| Consent withdrawal and disclosure | existing `SettingsView.swift`, localized resources and microphone purpose strings |
| Go streaming and privacy | realtime transcription provider/handler/tests, `voice_privacy_texts.go`, public legal text |

The app keeps iOS 17 as its deployment minimum. The new
`AIReplyVoiceWidgets` extension uses bundle ID
`kz.ai-reply.reply.keyboard.keyboard.voicewidgets` and App Group
`group.kz.ai-reply.shared`. It does not receive the shared account keychain
entitlement. The app adds `NSSupportsLiveActivities`, `UIBackgroundModes=audio`
and the `aireply` URL scheme. Existing app, keyboard and Watch bundle identities,
signing team `2PK6339Q47`, purchases and entitlements are preserved.
Both `project.yml` and the checked-in Xcode project include the target and new
sources. Widget required-reason APIs are declared in its privacy manifest.
`VoiceAudioEngineBridge.h/.m` contain Objective-C engine exceptions inside
Objective-C and return explicit NSError results to Swift. Tap installation is
tracked so teardown never removes an uninstalled tap. This is an additional
audio safety measure, separate from the keyboard alert-presentation fix.

## Privacy and App Review

The public policy now distinguishes optional iPhone Voice from Apple dictation
and Watch voice. It explains the named processor, explicit consent, background
recording controls, deliberate clipboard use, protected local text, expiry and
withdrawal. Only the displayed privacy-policy edition moved to 2026-11-18;
general legal-consent versions were not reset because enabling this optional
audio path requires its own explicit consent.

AI Reply does not persist microphone recordings, but that alone is insufficient
to call audio uncollected for App Privacy. OpenAI's standard Realtime policy
allows abuse-monitoring retention. The current app privacy manifest declares
Audio Data linked to the user for App Functionality, without tracking. The
published Kazakhstan App Store card was independently checked and already
lists that category. Draft App Store Connect answers must remain consistent;
a manifest does not update them automatically.

Authoritative references checked during implementation:

- [Apple Review Guidelines](https://developer.apple.com/app-store/review/guidelines/): intended background use, recording consent and indicators, keyboard restrictions, privacy disclosure and third-party AI permission.
- [Apple controls](https://developer.apple.com/documentation/widgetkit/creating-controls-to-perform-actions-across-the-system): user-added system controls.
- [Control configuration](https://developer.apple.com/documentation/widgetkit/adding-refinements-and-configuration-to-controls): one control action and edit-mode configuration.
- [Custom SF Symbols](https://developer.apple.com/documentation/technologyoverviews/creating-custom-sf-symbols): vector symbol templates and Xcode asset validation.
- [AudioRecordingIntent](https://developer.apple.com/documentation/appintents/audiorecordingintent): explicit recording actions with an active Live Activity.
- [LiveActivityIntent](https://developer.apple.com/documentation/appintents/liveactivityintent): app process execution without opening its interface.
- [Live Activity guidance](https://developer.apple.com/design/human-interface-guidelines/live-activities): finite task status and essential interactions.
- [Apple App Privacy](https://developer.apple.com/app-store/app-privacy-details/): collection includes retention by partners beyond real-time request processing.
- [OpenAI data controls](https://developers.openai.com/api/docs/guides/your-data): endpoint-specific provider retention and account controls.
- [Published app card](https://apps.apple.com/kz/app/ai-reply/id6818570150): existing Audio Data disclosure.

This review checks the implementation against the cited public requirements;
App Review makes the approval decision.

## Validation recorded for this source

Build 31 passed the full simulator suite: 966 tests passed, no failures, and
one file-protection-metadata check was skipped because the simulator does not
expose that metadata. The locked cold-launch controller regression group also
passed 11 targeted tests. Widget and intent sources passed standalone iOS 17 simulator
application-extension typechecking, including the three state preview
configurations. These checks do not establish physical microphone behavior.

The compact main view built successfully for the iPhone 17 Pro simulator.
A temporary DEBUG-only synthetic listening/transcript fixture was rendered and
visually checked; no microphone or provider result was used. The fixture was
removed from source immediately afterward. Screenshot:
`outputs/release-1.3.0-31/qa/voice-main-listening-ru.png`. This captures the actual
SwiftUI layout in dark mode; it does not exercise a real recording or the
medium-sheet presentation. Final archive/test results belong in the release
status artifact, not inferred from this preview.

A synthetic public-TTS provider experiment against the existing backend
observed 29 delta events before commit: first delta about 0.95 seconds after
start, commit about 16.06 seconds and final about 16.81 seconds. Only event
types, timing and text lengths were recorded. This validates the existing
provider transport's pre-commit partial behavior, not iPhone recognition
quality. No segmented-audio runtime change was necessary.

The four-language privacy-route regression passes with explicit Done, the
40-second maximum and 20-second initial-silence wording. The keyboard
presentation source guard passes; a separate native fake-engine harness
verified NSError/exception cleanup without microphone hardware. A real-helper
macOS harness with UIKit stubs verified direct insertion in the same keyboard
appearance and mandatory review after reopening. Its receipt is under
`outputs/release-1.3.0-31/qa/appearance-binding/`.

The release archive and distribution IPA were verified for all four bundles,
their version/build, signatures, team and profiles. App Intent metadata retains
AudioRecording/SessionStarting and `openAppWhenRun=false`; the archived widget
asset catalog contains the branded vector glyph. Apple accepted build 31 on
2026-11-18 at 19:00:21 +05:00. TestFlight processing completed; the build is available to the existing
AI-Reply Internal group (two testers), with Russian testing instructions saved.
App Review accepted one submitted item: version 1.3.0, build 31, submission
`a203f5b6-cd07-4e4c-9b80-2cfabc3b86c9`. That submission was subsequently withdrawn
and confirmed Removed after the blank-icon report, for replacement with build 32.
Receipts are tracked in
`outputs/release-1.3.0-31/release-status.json`. Build-31 physical
microphone/Control Center validation is still pending. The future April 2027
SDK warning did not prevent this upload.

## Physical iPhone test checklist

1. Install the build, sign in, accept current general AI terms, and enable the
   AI Reply keyboard. Confirm normal typing with Full Access off, then enable
   Full Access for network/shared features.
2. Open Home's microphone. Paste a short copied incoming message, select a
   template and speech language, and start. Verify first-use OpenAI disclosure,
   Not Now, allowed/denied microphone permission and retry after Settings change.
3. Say a Russian, Kazakh and English instruction in separate turns, then mixed
   speech, names, numbers, short/long commands and background noise. Confirm
   the result is an AI-written response, not a verbatim instruction.
4. During capture, switch to a messenger and open AI Reply keyboard. Use its
   microphone menu to deliberately supply copied context if needed. Speak only
   after context is supplied. Finish with the keyboard or expanded Island
   checkmark; review and Insert. Verify recognized text appears in the keyboard
   while speaking, grows over several phrases and remains active through short
   pauses. Confirm nothing is sent automatically.
5. Add the iOS 18 Control Center control manually after initial app permission
   setup. Repeat cold/warm starts while a messenger is visible. Verify capture
   and the Live Activity start without opening AI Reply. Before setup, verify
   clear setup guidance and no capture. Verify the monochrome reply-microphone
   symbol at small/large control sizes and on the Lock Screen. If an
   existing control retains cached artwork, remove and re-add it. Do not expect
   a custom long-press action menu; normal tap starts recording. On iOS 17 use
   the app or Open action.
6. Test Island compact/expanded and Lock Screen status, Done, Pause and Cancel;
   disable Live Activities and verify app/keyboard behavior. Confirm no private
   text appears on the Lock Screen. After Pause, explicitly start a new turn.
7. Test 40-second limit, short speech pauses, 20-second initial-silence timeout, loss of network,
   provider timeout, phone/audio interruption, headphone removal, lock/unlock
   and consent withdrawal while recording. Confirm microphone resources stop.
   Finish an authorized recording while locked, unlock and verify the result
   appears. Test an unrelated locked cold launch and ensure a previous result
   survives until unlock; sign-out must cancel deferred restoration.
8. Dismiss/reopen the keyboard, change fields/chats before results arrive,
   regenerate, insert once, dismiss old results and wait for expiry. Verify old
   context and duplicate results do not enter another conversation.
9. Test empty/multiline/over-limit copied messages, standalone requests,
   expired subscription, exhausted transcription quota and normal reply limits.
   Confirm Restore Purchases and Manage Subscription remain available under
   Balance and Access and previously purchased balances remain intact.

Record device model, OS, app build, backend version, each observed result and
remaining failures. Do not mark this checklist passed from simulator results.
