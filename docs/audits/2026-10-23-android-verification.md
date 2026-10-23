# Android verification — 2026-10-23

This supplements `2026-10-23-ai-reply-production-audit.md`. Android changes are implemented and pass automated checks. Interactive keyboard testing on the emulator remains pending for the tooling reason described below. No physical-device push delivery is claimed.

## Scope and implementation

- `ReplyKeyboardService` is the native `InputMethodService`; `KeyboardViewHost` supplies Compose lifecycle owners. `KeyboardLayout` defines separate letters, punctuation, emoji and specialized numeric pages. `KeyboardGeometry` calculates their frames and mutually exclusive, half-open hit regions in dp. `KeySurface` draws the existing Canvas and retains its single touch controller.
- Space now gets spare bottom-row spacing and some secondary-key width. All controls remain available; the system globe, emoji and Return retain their previous widths. A regression comparison with the previous layout verifies a 25–35% Space increase in the ordinary multi-language, emoji-enabled layout across 320, 360, 393, 411 and 480 dp at densities 1, 2.625 and 3.5. Less crowded and specialized rows retain their own behavior; a uniform percentage for every input type is not claimed. Every sampled point inside visible Space resolves exclusively to Space.
- The AI entry is a 64 × 48 dp pill with a 20 dp sparkle and a short AI label. Microphone has a separate 48 dp target. Recipient chips preserve their natural width and horizontal scrolling when the larger controls leave less space.
- Regeneration keeps its position under the response and now has a 48 dp target and localized “Another reply” / “Другой вариант” / “Басқа нұсқа” labels, with short alternatives on compact screens. Edit and version navigation can occupy a second row when needed. Wide landscape layouts keep version navigation inline. Existing generation, stop, editing, insertion and version-history callbacks are retained.
- The reported missing punctuation→Space transition was **not confirmed** in the initial Android source: it already returned from both punctuation pages after every Space. The transition is now explicit and tested for `. , ! ? : ; …` and consecutive punctuation. Digits and other symbol runs stay on the chosen page. Numeric, phone, date, email, URL and password fields do not apply this transition or the double-space sentence shortcut. The selected language is unchanged and insertion still uses the existing single Space path.
- Geometry now uses the measured `BoxWithConstraints.maxWidth` of the actual IME view, rather than assuming the configuration's screen width equals the keyboard window width.
- Keyboard Setup uses the Android-specific “Copied text” title and explains Copy → AI Reply → recipient/Paste, explicit reads and no background collection. The permanent existing Paste Help page already correctly explains that Android has no separate “allow paste” switch. EN/RU/KK/UZ resources retain parity.

Following the expanded video request, four genuine Android recordings are bundled in EN/RU/KK/UZ. They show the actual localized Keyboard Setup screen, the native Android on-screen keyboard settings, and the localized Paste Help screen. Captions explain enabling/selecting the keyboard and user-initiated copy/paste. The recorded keyboard was already enabled; no permission-grant action is presented as completed. No tap, permission grant, paste success or notification delivery was fabricated. The permanent text guide remains available.

## Commands and automated results

Run from `AI-Reply-Android`:

```sh
JAVA_HOME='/Applications/Android Studio.app/Contents/jbr/Contents/Home' ./gradlew testDebugUnitTest assembleDebug lintDebug --console=plain
```

Baseline: build, unit tests and lint passed before edits. The system Java launcher initially reported that no Java runtime was installed; Android Studio's bundled JDK resolved it.

After changes: **820 tests, 72 suites, 0 failures, 0 errors, 0 skipped**. This includes four added regression tests for Space geometry, punctuation in RU/KK/EN, non-punctuation runs and specialized fields, plus a packaging test requiring four distinct local AVC tutorial assets. Existing composer/history, clipboard, installation registration, notification permissions and localization tests also pass. One first-run failure caught an emoji ABC alignment regression; its layout was corrected, then the complete suite passed.

`assembleDebug` passed; APK: `AI-Reply-Android/app/build/outputs/apk/debug/app-debug.apk`. The APK was installed successfully with `adb install -r` on emulator-5554.

`lintDebug` passed with **0 errors and 182 warnings**. The report is `AI-Reply-Android/app/build/reports/lint-results-debug.xml`; warnings are not all independently classified as pre-existing. No new dependency was introduced.

From the repository root:

```sh
python3 tools/validate_firebase_config.py
```

Passed: local Android and iOS client configurations agree on project `ai-reply-4bf8f`; Android package `kz.yerek.aireply` matches. This does not validate provider credentials, APNs configuration or notification display on a device.

## Emulator evidence and limits

Used the existing **rbt_api36 Android Emulator (API 36), emulator-5554**, physical raster 1080 × 2424, original density 420 dpi. This generic emulator is not evidence of Samsung, Xiaomi or Vivo behavior.

The existing debug launch route opened the native guide without signing into a production account:

```sh
adb -s emulator-5554 shell am start -W -S -n kz.yerek.aireply/.MainActivity --es kz.yerek.aireply.debugScreen paste_help
```

System test configurations used `wm density 540` (320 dp), `440` (approximately 393 dp), and `360` (480 dp), at `font_scale` 1 and 1.3. A landscape capture used density 440 and rotation 1. The original density, font scale 1.0, portrait rotation and automatic rotation were restored afterwards.

Authentic read-only `adb exec-out screencap -p` files:

- `evidence/android-320dp-font1-paste-help.png`
- `evidence/android-320dp-font1.3-paste-help.png`
- `evidence/android-393dp-font1-paste-help.png`
- `evidence/android-393dp-font1.3-paste-help.png`
- `evidence/android-480dp-font1-paste-help.png`
- `evidence/android-480dp-font1.3-paste-help.png`
- `evidence/android-393dp-font1-landscape-paste-help.png`
- `evidence/android-api36-setup-default.png`

Matching `android-*-activity.txt` files contain only the relevant activity configuration record. Visually inspected representative compact/standard/large and landscape screenshots: the native guide wraps text and remains within the horizontal viewport; longer content uses the existing scroll container. These are **app guide screenshots, not keyboard interaction proof**. `android-api36-initial-screen.png` shows the launcher only and is a tooling reference.

The IME enabled/selected successfully using Android's `ime enable` and `ime set` test configuration commands. However, the standalone emulator is absent from CUA's app inventory and cannot be bound by its process name or executable path. Android Studio's Welcome screen is readable, but coordinate clicks return `noWindowsAvailable` and its More Actions accessibility click produced no change. UI input through ADB was not used. The existing Compose/Connect fields have no automatic focus on launch, so no keyboard interaction was fabricated. No test-only feature was added to production to bypass this limitation.

`adb logcat -d -s AndroidRuntime:E` returned no crash entries during these checks. This does not prove that every screen was exercised.

## Push audit and remaining QA

The existing Android path is consent → Firebase token/onNewToken → PushCoordinator → InstallationRegistrar → backend installation registration. Registration fingerprints include the body and account, refreshes authentication on 401, backs off transient errors and renews a token reported invalid. Android 13+ runtime permission, foreground notification display, `general`/`important` channels and notification tap navigation are implemented and have existing focused tests. No Android source-level cause of the reported zero-recipient count was established; backend selection/provider findings are covered by the main audit.

Still required before release:

- Manual IME typing, long presses, language switching, emoji, voice, labeled regeneration, paging and insertion at the requested widths/font sizes/orientations.
- RU/KK/EN visual keyboard QA and TalkBack checks, including split-screen/foldable configurations and OEM hardware.
- WhatsApp/Telegram/Instagram host-field testing where available.
- Designated-device notification tests with granted/denied permission, channels, foreground/background/terminated and tap handling. A token or provider acceptance alone is not proof of display. No bulk test push was sent to production users.

Android is ready for further development/internal QA, with the above manual and physical-device checks outstanding. Production submission readiness is not claimed.


## Localized Android videos and offline player

- Bundled assets: `AI-Reply-Android/app/src/main/res/raw/keyboard_setup_en.mp4`, `keyboard_setup_ru.mp4`, `keyboard_setup_kk.mp4`, `keyboard_setup_uz.mp4`.
- Actual application language was selected using the existing `shared.appLanguage` preference on the isolated debug emulator, before cold-launching the guide. Native Settings was separately configured through `adb shell cmd locale set-app-locales com.android.settings --user 0 --locales <tag>` so it also shows its actual EN/RU/KK/UZ translations where provided by Android. No system labels were painted over the recording.
- Sources used `adb shell screenrecord --bit-rate 1500000 --size 720x1616 --time-limit 25 <temporary-file>` with actual app lifecycle launches, plus separate 11-second recordings of the fully rendered localized Paste Help screen. Preflight screenshots confirm that the app UI was rendered before each recording. EN retains the recorded sequence. RU/KK/UZ use normal editing of genuine stable scenes: 4 seconds Keyboard Setup, 10 seconds native on-screen keyboard settings and 10 seconds Paste Help, removing slow application-start transitions. No UI is synthesized.
- Android screenrecord emits VFR frames only when the screen changes. The capture genuinely ran for 25 seconds, but file metadata may end after the last UI change. `tools/tutorials/optimize_tutorials.py --platform android --recorded-seconds 25` preserves all recorded transitions and holds only that last unchanged, genuine frame until 24 seconds. It does not create UI, taps, permission changes or success states.
- Final assets have H.264/yuv420p, 720 × 1816 pixels, 30 fps and approximately 24-second duration (23.97–24.00 seconds), with no audio or subtitle streams. Captions are burned into a separate 200 px dark strip below the complete recorded screen, using localized Android resource strings. `fps=30` occurs before subtitle rendering so captions change on schedule even over an unchanged VFR frame. Original footage is stored outside the application bundle under `/tmp/ai-reply-keyboard-<language>-raw.mp4` and `/tmp/ai-reply-android-guide-<language>.mp4`; contact sheets are in `docs/audits/evidence/android-tutorial-<language>-frames.png`.
- `KeyboardTutorialScreen.kt` uses native `VideoView` and `MediaController`, with no new dependency or network URL. It selects an asset from the resolved app language, offers play/pause and unlimited replay, and saves a separate playback position for each language when the screen pauses or closes.
- The optional Watch Video entry is present in onboarding's keyboard-connection step and permanently in Settings and Keyboard Setup. Leaving the video returns to the existing flow; watching never marks a system permission as granted or forces onboarding to reopen.

Final Android tutorial sizes: EN 519,544 bytes; RU 355,125 bytes; KK 346,441 bytes; UZ 373,375 bytes. All four contact sheets were visually checked for actual localized application/settings/guide UI, readable captions and complete source framing. RU/KK/UZ metadata and SHA-256 records are `evidence/android-tutorial-<language>-metadata.json`. Stable real frames are held for reading where Android VFR captures contain only a single frame; the original 11-second capture wall times and this editing are disclosed in those records. Product captions contain only user setup/paste instructions, with capture limitations restricted to engineering documentation.


## Final offline playback result

The final APK was installed successfully after the last complete build. On the API 36 emulator, airplane mode was enabled and Wi-Fi/mobile data were disabled. The native VideoView opened and played the correct local recording in **EN, RU, KK and UZ**; all four screenshots were visually inspected. Captions and the complete recorded screen remained within the player, and the play/pause/replay controls were visible. Evidence: `evidence/android-video-offline-en.png`, `android-video-offline-ru.png`, `android-video-offline-kk.png`, and `android-video-offline-uz.png`.

Normal backgrounding saved the UZ playback position at **1,830 ms** in `tutorial_playback.xml`. The UZ tutorial was reopened with that saved value and produced the resumed native-player screenshot `evidence/android-video-offline-resumed-uz.png`; position persistence is also recorded in `evidence/android-video-playback-progress.xml`. Forced process termination during the language-switch fixture is not treated as a normal lifecycle pause or proof of saving every language position. The shared production code stores positions separately by language. No playback error was found in these completed checks; manual tapping of replay and full IME interaction remain outside the verified CUA scope.

Final source/assets check: **820 tests / 72 suites, 0 failures, 0 errors, 0 skipped; assembleDebug passed; lintDebug passed with 0 errors and 182 warnings; git diff --check passed**. No further source changes followed that build. The device's original network availability and app-language override were restored after offline QA. This is ready for internal QA with the previously listed physical-device/push and interactive-keyboard checks outstanding.
