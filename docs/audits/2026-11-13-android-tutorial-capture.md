# Android setup tutorials — 2026-11-13

## Genuine Kazakh and Uzbek capture

The Kazakh and Uzbek recordings replace the earlier short static guides. They
show the actual Android app, native keyboard list and warnings, input-method
picker, an explicitly copied example message, the keyboard reply panel,
microphone permission, voice recording controls, a generated reply and insertion
into the app's practice field. Sending a message is left to the user.

Capture used the existing `rbt_api36` Android 16 emulator, `emulator-5554`, at
1080 × 2424 pixels and 420 dpi. `adb shell screenrecord --size 720x1616`
recorded the complete display. The emulator ran headless during clipboard and
reply capture, avoiding desktop clipboard integration. Only disposable examples
from the app's own onboarding were copied.

The app-language override was absent. The system language was changed through
Android Settings, with `kk-KZ` first for Kazakh and `uz-Latn-UZ` first for Uzbek.
Both app interfaces followed the system automatically. Native Settings and
permission labels are the real translations supplied by this Android image;
no system label or permission outcome was drawn into the footage. Keyboard
letter layout remains independent of the app language.

## Actions verified

- Enabled AI Reply from its disabled state and confirmed both native Android
  keyboard warnings.
- Selected a real AI Reply subtype in the system keyboard picker. The app's
  enabled/selected checklist then reflected the actual system state.
- Tapped the onboarding Copy button and verified its copied state. The keyboard
  source field then showed the matching Kazakh or Uzbek example after tapping
  the recipient chip.
- Requested microphone permission from Keyboard Setup and selected the native
  option allowing access while using the app.
- Used the existing designated review account to show the actual keyboard
  recording timer and controls. Recording was canceled; no recognized speech or
  transcription success was fabricated.
- Chose a quick instruction and generated one genuine reply per language using
  the existing backend. The resulting localized reply was inserted into the
  practice field. No mock transport was enabled and no chat message was sent.

Android has no separate clipboard permission toggle. The caption explains that
the app reads copied text after the user's recipient/Paste action. Watching the
recording itself does not grant any permission.

## Packaging and evidence

`tools/tutorials/encode_keyboard_tutorial.py` assembles the genuine clips with
localized instructions from `tools/tutorials/android_caption_copy.json` in a
separate 220-pixel strip below the recorded screen. Editorial cuts remove waiting
and retain the actual action outcomes. A static final frame is held only within
the measured recording wall time where Android's variable-frame-rate output
omits unchanged frames.

The packaged files are `app/src/main/res/raw/keyboard_setup_kk.mp4` and
`keyboard_setup_uz.mp4` inside `AI-Reply-Android`. Both use local H.264/yuv420p,
720 × 1836 portrait frames, 30 fps, MP4 fast start, and no audio or subtitle
streams. Encoding also fully decodes each result before replacing the asset.

Contact sheets and source hashes, clip ranges, captions, final size and metadata
are recorded in:

- `evidence/android-2026-11-13-tutorial-kk-frames.png`
- `evidence/android-2026-11-13-tutorial-kk-metadata.json`
- `evidence/android-2026-11-13-tutorial-uz-frames.png`
- `evidence/android-2026-11-13-tutorial-uz-metadata.json`

Raw footage and assembly manifests remain outside the application resources in
`/tmp/aireply-android-capture/{kk,uz}/`. Source hashes in the metadata identify the
exact recordings used. This is emulator evidence; actual speech transcription
and manufacturer-specific device behavior remain separate device checks.

## English and Russian capture

English and Russian use the same complete sequence of genuine setup, clipboard,
microphone, reply generation and insertion actions. Capture ran on an isolated
temporary API 36 AVD, `emulator-5556`, with the same 1080 × 2424 display and
420 dpi. It ran headless. The system language was `en-US` for English; Russian
was added and moved to the first position through native Android Settings,
including the system language-change confirmation. The resulting system
configuration had `ru-RU` first. The app-language override remained absent, and
the Russian keyboard subtype and letter layout were selected normally.

The fresh isolated emulator restored only the designated review account's exact
existing server consent receipt, with `pendingSync=false`, before native email
sign-in. No new terms were accepted, no consent record was sent to the server,
and no receipt version or acceptance date was invented. The temporary session
used to retrieve that existing receipt was logged out. Tokens were not written
to capture evidence or recording metadata.

For each language, the actual Copy indicator and matching keyboard source text
were checked. Microphone access was granted using the native while-in-use
option. The recording timer and controls were captured, then recording was
canceled without claiming a recognized transcript. A quick instruction produced
one real backend reply, and the actual Insert button placed it into the app's
practice field. The English result was “Good afternoon! Yes, of course.”; the
Russian result was “Добрый день! Конечно, расскажу подробнее.” No chat message
was sent. `debug.mockReplies` was false throughout.

English is 78 seconds. Russian is 79 seconds and ends with the complete inserted
text visible after dismissing the keyboard and scrolling the real practice
screen. Both use the same silent H.264/yuv420p, 720 × 1836, 30 fps, fast-start
encoding and full decode verification described above. Their evidence is:

- `evidence/android-2026-11-13-tutorial-en-frames.png`
- `evidence/android-2026-11-13-tutorial-en-metadata.json`
- `evidence/android-2026-11-13-tutorial-ru-frames.png`
- `evidence/android-2026-11-13-tutorial-ru-metadata.json`

The metadata also records the actual system configuration, absent app-language
override and disabled mock setting. Raw sources, action snapshots and manifests
remain in `/tmp/aireply-android-capture/{en,ru}/`. Historical October contact
sheets were preserved. An emulator graphics-process failure interrupted an
early Russian picker take; that incomplete take was discarded, the AVD was
restarted with SwiftShader and Vulkan disabled, and the remaining complete
scenes were recorded again.

## Final offline playback and guide QA

The final debug APK, version `1.2.1` / code `5`, was installed only after a
successful build. On `emulator-5556`, airplane mode was enabled and Wi-Fi and
mobile data were disabled. Each of `en-US`, `ru-RU`, `kk-KZ` and `uz-Latn-UZ`
was selected first through native system language settings. The app-language
override remained absent. All four actual localized recordings played locally.

The complete recorded display and caption strip fit in expanded mode. Both
controls remained inside the visible screen and accepted taps. Native player
diagnostics confirmed Pause (`renderer paused=1`), stable paused frames, then
Play (`paused=0`) with increasing decoded frames. Replay restarted each
recording at approximately one second. Back first returned to the written
guide, then to Keyboard Setup. Closing retained the playback position before
the native view released its player; reopening resumed that language's video.
Other language positions stayed unchanged during each check.

| System language | Saved on close | Observed after reopening |
| --- | ---: | ---: |
| English | 13,553 ms | 15,910 ms |
| Russian | 14,667 ms | 15,262 ms |
| Kazakh | 15,329 ms | 18,519 ms |
| Uzbek | 18,567 ms | 17,728 ms |

Native VideoView seeking may land on a nearby keyframe. Per-language JSON checks
record the actual system configuration, cleared override, offline settings,
native player states, control bounds and observed positions:
`evidence/android-2026-11-13-playback-{en,ru,kk,uz}-checks.json`.
The matching `-expanded`, `-collapsed` and `-resumed` PNG/XML files contain only
the confirmed final UI. Intermediate diagnostic images are outside Git in
`outputs/android-tutorial-qa-intermediate/`.

All nine written actions and the optional voice guidance were reached by
scrolling. Evidence is `android-2026-11-13-playback-en-guide-{2,6,9}.png` and
`android-2026-11-13-playback-written-guide-checks.json` in `evidence/`.
The onboarding connection step's Watch Video entry opened the player and
returned to exactly the same page and saved onboarding state; see the
`android-2026-11-13-playback-onboarding-{before,after}.png` pair and
`android-2026-11-13-playback-onboarding-checks.json`.

After QA, English was restored first, airplane mode was disabled, Wi-Fi and
mobile data were enabled, and all three animation scales were restored to 1.
Native app-language overrides for AI Reply and Settings were empty. The
designated review session was retained. Final settings are recorded in
`evidence/android-2026-11-13-playback-restored-settings.json`.

The final source passed 1,021 unit tests in 83 suites (no failures or skips),
`lintRelease` with 0 errors and 199 warnings, debug assembly and unsigned
release-bundle generation. Signing and Play submission remain the upload Mac's
separate steps.
