# Localized offline tutorials — 2026-10-23

## Final iOS assets

Four separate genuine simulator recordings are bundled in `AI-Reply/AIReply/Resources/Tutorials/`:

- `paste-permission-en.mp4`
- `paste-permission-ru.mp4`
- `paste-permission-kk.mp4`
- `paste-permission-uz.mp4`

Each contains actual AI Reply screens in its own language and localized instructions burned into a separate dark strip below the recorded screen. The original UI is not painted over. English, Russian and Kazakh footage uses the actual matching iOS Settings localization. This iOS 26.5 runtime has no Uzbek Settings localization; the Uzbek recording shows real English system labels and Uzbek app screens/instructions, without replacing or translating system UI pixels.

The player chooses the effective app language, which follows the phone unless overridden in app Settings. Position is saved separately per recording. Language changes replace the player and observer. Watching never grants a permission or completes onboarding. The containing app offers native AVKit playback, unlimited replay and permanent access from Settings, Keyboard & Permissions and Paste help; the onboarding connection step offers optional access. No video/player is placed in the keyboard extension.

Verified metadata for all four: 23.97–24.00 seconds, H.264 High/yuv420p, 720 × 1766 (1566 recorded screen + 200 caption strip), approximately 30 fps, MP4 fast start, zero audio streams. Captions are part of the pixels and therefore remain visible in fullscreen/offline playback. No duplicate shared video is bundled. Contact sheets in `docs/audits/evidence/ios-tutorial-{en,ru,kk,uz}-frames.png` were visually inspected, including cold-start and pasted-result checks; blank/late captures were replaced. Final product captions and screens contain no simulator/emulator terminology.

## Genuine capture and system limitation

Recorded with `xcrun simctl io <iPhone-17-UDID> recordVideo --codec=h264` on iPhone 17 / iOS 26.5. Footage covers setup, the actual AI Reply page in Settings, returning to the app and native manual Paste using disposable localized test text. Editorial cuts remove waiting. Static VFR captures omit unchanged frames; normalization holds the genuine unchanged screen for its measured capture wall time (4/10/10 seconds). No permission control, tap outcome or system label was fabricated. Raw MOVs stay in `/tmp/ai-reply-*.mov` and are not committed/bundled.

The actual Settings → Apps → AI Reply page did **not** expose Paste from Other Apps. The recordings cannot demonstrate selecting Allow here. This audit retains that verification limitation; the product itself gives permission steps without capture-tool diagnostics. Keyboard Full Access is a different setting. Native manual Paste is **not** evidence that automatic keyboard paste was granted. To trigger the actual clipboard request, the guide directs users to the AI Reply keyboard's reply recipient or Paste message action, then back to Settings.

`tools/tutorials/optimize_tutorials.py` provides reproducible encoding, localized caption wrapping and metadata checks. Final iOS full suite after four-asset integration: **616 tests, 0 failures**, including all four local H.264/no-audio/duration checks, exact locale selection, explicit missing-resource fallback and separate progress. Result `/tmp/ai-reply-audit-localized-approved-tests.xcresult`; log `/tmp/ai-reply-audit-localized-approved-tests.log`.

On iPhone 17 Pro Max, native playback of the initial recording, active Russian captions, back/reopen and persisted completed position (24 seconds) were observed. Final localized playback verification is recorded in the main audit. A local AVURLAsset is used; playback has no network source.

## Android

Four Android-specific recordings and a native offline player were added separately; see `2026-10-23-android-verification.md`. Android has no iOS-style Paste from Other Apps toggle. Its guide covers actual Android activation/clipboard behavior and never invents that permission.

## Physical iPhone recording procedure for the missing Allow step

1. Use a clean designated test iPhone, disposable clipboard text and the intended system/app language. Hide personal notifications/accounts.
2. Add AI Reply and Full Access through ordinary user-controlled setup. Copy test text from another app; in a normal field open AI Reply and choose a reply recipient or Paste message to trigger the real clipboard request.
3. Start iOS Screen Recording with Microphone Off. Show AI Reply setup, Open Settings, and Settings → Apps → AI Reply where applicable.
4. If the actual Paste from Other Apps row appears, show it and choose its actual Allow option. If absent, document device/OS behavior instead of drawing a replacement.
5. Return, copy new test text and invoke the keyboard paste action. Do not spend an AI generation merely to demonstrate permission.
6. Trim to 15–25 seconds, remove audio, encode H.264/MP4, retain readable captions in the correct language, inspect frames and verify local replay. Replace the appropriate language asset.


Final copy refinement: user-facing technical capture explanations were removed from all four languages, including the video pixels, while this engineering record retains the actual capture limitations. Instructions now say to allow pasting in AI Reply settings and show the normal path. No permission-granted state or missing system control was drawn into the recording.

Final copy-only re-encoding/build after removing capture diagnostics also passed the complete **616-test** iOS suite: `/tmp/ai-reply-audit-final-copy-tests.xcresult`, `/tmp/ai-reply-audit-final-copy-tests.log`. All four MP4s were decoded and their caption contact sheets regenerated. Product text was checked in all four locales for simulator/emulator terms; none remain in the tutorial copy.
