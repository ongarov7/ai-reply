# iOS keyboard audio lifecycle audit — 2026-10-26

## Reported symptom and evidence boundary

After installing the latest release and switching away from AI Reply, ordinary typing on Apple's keyboard produced an unpleasant distorted clicking/hissing sound. Restarting the iPhone cleared it. The complaint concerns continued native-keyboard typing, not just the globe-switch gesture.

The exact audible defect has not been reproduced on a physical iPhone in this workspace. A Simulator build or passing unit tests cannot establish that Apple's native click waveform is repaired. The defects below are demonstrated by code inspection and regression tests; their connection to the reported system-wide sound remains a hypothesis pending device verification.

## Audio surface and release history

- The keyboard extension uses four call sites of the public `UIDevice.playInputClick()` API and an input view implementing `UIInputViewAudioFeedback`. It has no `AVAudioSession`, `AudioServicesPlaySystemSound`, custom sound resource, preferred sample-rate/buffer/route override, microphone capture, or `CHHapticEngine` code.
- `KeyboardKeysView` and the emoji grid also use UIKit impact feedback generators. Keyboard click audio respects iOS's keyboard-sound setting through the documented UIKit API.
- Latest iOS release commit `9cdc6b9` changes Space/control geometry and adds tutorial playback, but does not change the four keyboard click call sites or introduce a keyboard audio session. The dictation implementation predates this release.
- `PasteTutorialView` is the sole app `AVPlayer` user. It starts only through native user controls, is muted, pauses when inactive, and removes its player/observer when leaving. All four bundled MP4s have no audio tracks, as checked by `PermissionDiagnosticsTests.testEveryLocalizedTutorialIsBundledOfflineSilentH264`.
- `VoiceReplyViewModel` is the only app code configuring/activating an audio session. An active recording session is the concrete code path capable of affecting other system audio.

## Confirmed defects and changes

### Recording could start after its sheet was dismissed

`startRecording()` created an untracked Task. The system microphone/speech prompts suspend it. `DictationSheet.onDisappear` called stop, but a later permission response continued the original Task and could activate the engine without the sheet. Rapid taps could also create concurrent startup Tasks.

Startup is now owned and cancellable. Stop, dismissal, entering the background, and a begun audio interruption cancel pending startup. Cancellation is checked after each permission result. Repeated Start while permission is pending does not create another request. The app does not stop simply because a permission alert temporarily makes it inactive.

### Recording used an unsupported category option and retained record configuration

The old combination was `.record` / `.measurement` / `.duckOthers`. Apple's `duckOthers` documentation allows it with `.playAndRecord`, `.playback`, or `.multiRoute`, not `.record`. Dictation does not play audio, so recording now uses `.record` / `.measurement` with no category options. Activation no longer incorrectly passes the deactivation-only notification option.

`DictationAudioSession` captures the previous category/mode/options immediately before recording, deactivates only a session it owns after the engine/tap stop, notifies interrupted audio, and restores the previous configuration while inactive. An idle/repeated Stop no longer deactivates an unrelated player's app session. Activation failures restore configuration; failed teardown retains ownership for retry rather than discarding the error. Final speech recognition results release the microphone immediately instead of waiting for the 60-second timer. Callback identities prevent an older recognition session's callbacks from modifying a newer session.

### Held-key work could survive extension disappearance

Key tracking previously relied on receiving `touchesCancelled`/`touchesEnded` or a later layout change. The extension now cancels key/emoji-bar tracking explicitly when disappearing and when a keys view detaches from its window. This invalidates repeat/long-press timers and clears popups/pressed/trackpad state. The input view opts out of clicks while disappearing, and keyboard haptics are disabled until the next appearance. No new audio session is activated by the extension.

This cleanup removes delayed extension work after a switch. It does not prove the original distortion was caused by that work: delete repeat itself does not play click audio on every repeat.

## Regression coverage

Nine `DictationAudioLifecycleTests` are added to the existing test target via `VoiceConfigurationParserTests.swift`:

1. Supported recording category/options; deactivate before restoring the previous configuration; repeated teardown is harmless.
2. Failed activation releases and restores configuration.
3. Failed deactivation preserves ownership and can retry.
4. Stopping an idle model does not touch another audio session.
5. Late microphone permission after Stop cannot proceed to speech/capture.
6. Late speech permission after Stop cannot alter the stopped status or capture.
7. Repeated Start during a prompt produces one request.
8. Backgrounding and begun interruptions cancel pending capture.
9. Immediate dismissal before the startup Task first runs does not request a microphone permission or change the stopped status.

These tests inject permission responses and an audio-session probe; they do not record a microphone, access personal audio, or mutate the test host's audio session. The combined project build and complete suite results are recorded in the parent audit for this task.

## Physical-device acceptance still required

Use an affected iPhone/iOS version, with keyboard clicks enabled. Record device model, exact OS/build, app build, speaker/headphone/Bluetooth route, and whether dictation/tutorial playback preceded the first incident. Use disposable text.

1. Establish the baseline by typing on Apple's keyboard before opening AI Reply.
2. Type rapidly on AI Reply, test Space/delete hold/emoji, switch using both a globe tap and the keyboard picker, then type on Apple's keyboard for at least a minute. Repeat switching without restarting.
3. Repeat after opening/closing each localized silent tutorial, including backgrounding during playback and returning from Settings.
4. Start dictation, Stop, dismiss, then type on Apple's keyboard inside the app and another app. Repeat with immediate dismissal during each permission prompt, backgrounding, and an interrupted capture. Verify the microphone indicator clears and previously playing audio resumes.
5. Repeat with the built-in speaker and any route used when the defect occurred. Include the affected current iOS version and the supported minimum where devices are available.
6. If distortion remains, capture a short external audio recording and iOS audio-service logs/sysdiagnose at the event; determine whether it persists after terminating the containing app and switching extensions. Do not classify the symptom as solved based only on a reboot or Simulator output.

No attempt is made to restart iOS audio services, alter the user's sound settings, force a system route/sample rate, or acquire an extension audio session as a workaround.

## Primary references

- [Apple — playInputClick](https://developer.apple.com/documentation/uikit/uidevice/playinputclick%28%29): system click feedback and visibility/user-setting requirements.
- [Apple — duckOthers](https://developer.apple.com/documentation/avfaudio/avaudiosession/categoryoptions-swift.struct/duckothers): supported categories and temporary lifetime.
- [Apple — Activating an Audio Session](https://developer.apple.com/library/archive/documentation/Audio/Conceptual/AudioSessionProgrammingGuide/ConfiguringanAudioSession/ConfiguringanAudioSession.html): recording sessions should be active only while recording; background release and checked activation/deactivation.
- [Apple — setActive(_:options:)](https://developer.apple.com/documentation/avfaudio/avaudiosession/setactive%28_%3Aoptions%3A%29): stop running audio objects before deactivation.
- [Apple — Handling audio interruptions](https://developer.apple.com/documentation/avfaudio/handling-audio-interruptions): explicit interruption handling for capture state.
