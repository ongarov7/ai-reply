# AI Reply production audit — 2026-10-23

## Scope and initial state

Requested six-issue production audit, adaptive keyboard corrections, genuine offline tutorials and safe push investigation. Initial branch `main...origin/main`; working tree clean. No AGENTS.md found in applicable ancestor/project directories. Read README, HANDOFF.md and current notification/setup documentation. HANDOFF contains historical claims and explicitly newer updates; source and fresh tests take precedence. At the start, no publication or deployment was authorized. The user later explicitly authorized commit and `git push origin main` after verification, as recorded below. No production deployment, bulk notifications, secrets inspection or external account changes were performed. Backend `.env` was not read.

Architecture: iOS UIKit custom keyboard extension (`ReplyKeyboard`) with pure shared layout/typing state and custom exclusive hit mosaic; SwiftUI containing application, App Group settings, account/onboarding services. Android InputMethodService → Compose chrome → Canvas keys with pure layout/geometry/state, shared settings and repository API. RU/KK/EN (plus existing UZ) localization: Swift typed strings + xcstrings, Android resources. Backend Go + SQLite, embedded admin JS, installation registration, encrypted tokens, outbox, FCM HTTP v1 for Android and iOS through APNs. Existing extensive Swift, Kotlin/JVM, Go integration tests.

Environment inventory: Xcode 26.6, iOS 26.5 runtime; available SE 3, iPhone 17/17e/Air/17 Pro/17 Pro Max, iPads. App supports iPhone only, portrait containing app; extension has host-driven geometry. One Android AVD `rbt_api36`; OEM Samsung/Xiaomi/Vivo images and real phones not observed. 32 GB RAM; representative simulators run sequentially to keep builds stable. Real APNs/FCM delivery requires designated physical test devices/configuration, not inferred from simulator push injection.

## Issue 1 — Space and language switching (high UX priority)

- Current/evidence: iOS `KeyboardLayout.bottomRow` gives each secondary control 1.35 units and return 2.75; standard 390-point layout with globe/language leaves about 122-point Space before optional emoji. Android `KeyboardLayout.bottomRow` allocates ?123 1.5, emoji 1, globe/comma 1, language 1.25, dot 1, return 1.5 units. Both already use mutually exclusive hit mosaics. Accidental switching is reported; not yet manually reproduced.
- Expected: wider adaptive Space (approximately 25–35% where feasible), system switch retained, visible Space entirely maps to Space.
- Root cause proven in source: excessive combined secondary-key allocation; overlapping hit regions not found.
- Files/functions: iOS Shared/Keyboard/KeyboardLayout.swift, KeyboardGeometry.swift; Android keyboard/layout/KeyboardLayout.kt, KeyboardGeometry.kt.
- Solution: rebalance secondary widths with responsive native targets; retain input-type extras and half-open hit tiles.
- Risks: compact widths, optional emoji/system globe, specialized fields, landscape. Verify geometry across widths/planes/languages and manually on representatives.
- Status: implemented; focused and full automated checks passed. See final verification below.

## Issue 2 — Recognizable AI action (medium)

- Current/evidence: iOS `PersonaRowLayout` fixed AI width 38 and height 30 inside 36-point chrome; `TemplateBarView` 14-point sparkle. Android CREATE_SIZE=30 and 17-point icon-only action in `ReplyPanel.PersonaRow`.
- Expected: visible sparkle + short AI label, wider target, neighboring personas accessible including horizontal scrolling.
- Root cause: compact fixed icon-only dimensions.
- Files: iOS persona layout/TemplateBarView/action metrics; Android PersonaRowLayout.kt and ReplyPanel.kt.
- Solution: adaptive labeled AI control and native target height, preserve persona selection/scrolling.
- Risks/verification: compact RU/KK labels, accessibility text, chrome height changes; geometry tests and simulator screenshots.
- Status: implemented; focused and full automated checks passed. See final verification below.

## Issue 3 — Another reply action (medium)

- Current/evidence: regeneration action is an icon-only circular control in both composers. Existing version history and flow prevent duplicate generation. iOS result row can hide pagination at insufficient width.
- Expected: same location, wider labeled action: RU «Другой вариант», KK «Басқа нұсқа», EN «Another reply»; preserve back/edit/insert/history.
- Root cause: icon-only constrained result-row allocation.
- Files: iOS ReplyComposerView.swift + localized AI strings; Android ReplyPanel.kt + strings.
- Solution: localized pill, responsive row wrapping/pagination rather than overlapping controls.
- Risks/verification: compact width, long labels, pending/error states, single request per tap, edited prior variants retained. Existing flow tests plus UI verification.
- Status: implemented; focused and full automated checks passed. See final verification below.

## Issue 4 — Punctuation + Space → letters (medium)

- Current/evidence: iOS `keysViewDidTapSpace` has no plane transition; apostrophe alone has a separate transition in `didType`. Android `typeSpace` already unconditionally returns non-letter planes to letters; Android complaint not reproduced in source and specialized input protection is insufficient.
- Expected: common/consecutive punctuation in symbols followed by exactly one Space returns to same-language letters for ordinary text; numeric/phone/specialized contexts keep intended behavior.
- Root cause: absent iOS punctuation state; Android broad transition policy differs from requested punctuation-scoped behavior.
- Files: both KeyboardTyping and service/controller handlers; specialized input policy.
- Solution: testable explicit punctuation state, reset on edits/caret/context/language/explicit mode switch, context-aware Space transition; no view recreation beyond existing plane layout.
- Risks/verification: autocorrect, double-space punctuation, numeric-only/email/URL/password, composer edits and language persistence. Focused state tests and real key taps.
- Status: implemented; focused and full automated checks passed. See final verification below.

## Issue 5 — Paste guidance and permanent video (high onboarding priority)

- Current/evidence: iOS OnboardingConnectStep + PermissionsView/PasteHelpView already explain keyboard and clipboard via text; Settings permanently reopens onboarding. No bundled tutorial/player found. Clipboard reads are initiated by explicit actions, no grant bypass. Paste permission must remain distinct from Full Access. Android keyboard setup exists but its paste section incorrectly uses generic permission wording despite no iOS-equivalent toggle.
- Expected: optional onboarding access and permanent Settings → keyboard setup → video tutorials, offline silent authentic footage, active-language captions, unlimited replay/resume, no repeated mandatory interruption.
- Root cause: no video assets or player; prose alone insufficient for reported understanding issue.
- Files: iOS SettingsView, PermissionsView/PasteHelpView, OnboardingConnectStep, Resources/Localizable.xcstrings, new local tutorial view/resources. Android setup wording and optional actual footage access.
- Solution: inspect real simulator Settings; record only authentic behavior. Use direct permission instructions in product UI and video, with capture limitations recorded in the engineering report and physical-device recording procedure. Bundle four localized H.264 no-audio recordings per platform with burned captions and native main-app playback, persistent playback position. Never claim watching grants permission.
- Risks/verification: OS-version labels/control availability, caption readability, offline bundle lookup, app background/resume, localization and accessibility. Inspect metadata and frames, replay from Settings offline. No raw oversized footage in app.
- Status: authentic native UI footage recorded, optimized and bundled in four languages per platform with optional onboarding access and permanent local playback. Simulator lacks the actual Allow control; the engineering limitation is documented below. See tutorial-verification report.

## Issue 6 — Push recipients and delivery (high)

- Current/evidence: installations registry may include anonymous devices, but eligible audience INNER JOINs active ordinary users. Default campaign category marketing requires explicit preference opt-in. Disabled/unconfigured FCM supplies no supported platform and makes audience condition false. Thus stored tokens alone do not establish eligible recipients. These are confirmed code paths; actual production zero-count cause cannot be established without authorized sanitized production state.
- Proven defects: preview exposes matched_devices but no matched_users, recipient records or exclusion diagnostics; send confirmation can remain enabled after failed/zero/blocked preview. Queue freezes only filters; worker selects recipients later, so population can change after Send.
- Existing correct behavior: preview/fanout share `eligible()` in repository/audience.go; encrypted deduplicated token registration, account detach, invalid token and transient retry handling, provider acceptance separate from device delivery. No evidence FCM/APNs is the original failure.
- Files: backend repository/audience.go, campaign/service/admin handlers and admin-app.js; mobile notifications services and installation registrar.
- Solution: additive preview counts + bounded masked records and exclusions, meaningful zero reason, guarded Send, transactional recipient snapshot at Send using established fanout. Preserve categories, filters, idempotency and outbox/provider architecture.
- iOS inspection: entitlements and manual APNs→FCM mapping present, foreground/tap handling present, token refresh delegate and registration retry present. APNs registration failure leaves per-launch requested flag true: evaluate retry. No APNs token is sent as FCM token.
- Android inspection: real Firebase project/package config, runtime permission, channels, MessagingService/token callbacks, foreground/background paths present. No confirmed Android source root cause of zero recipients.
- Risks/verification: multi-device/users counts, preferences/auth/disabled/anonymous filters, stable snapshot, duplicate sends, concurrency, transient/permanent errors and history. Run isolated fake-provider Go integrations, mobile lifecycle tests, admin JS checks. Never send to production users. Physical iOS/Android FCM display/tap proof remains external until designated devices are available.
- Status: source repairs and isolated integrations complete; full Go and race checks pass. Per user clarification, real push testing is outside this run because no designated token/device is available.

## Verification ledger

Baseline iOS build started: `xcodebuild -project AI-Reply/AIReply.xcodeproj -scheme AIReply -configuration Debug -destination 'generic/platform=iOS Simulator' -derivedDataPath /tmp/ai-reply-audit-ios CODE_SIGNING_ALLOWED=NO build` (log `/tmp/ai-reply-audit-baseline-build.log`). Initial SE 3 simulator booted for compact-screen reproduction. Pending final commands/results, media paths and release readiness are deliberately not claimed yet.


## Final implementation and verification

User clarified the priority during implementation: the three keyboard controls on iOS and Android are primary; push is source investigation/repair only. User subsequently authorized an ordinary commit and `git push origin main` after checks, with ordinary repository authorship. No deployment or production notifications are authorized or performed.

- **Space:** adaptive secondary-control allocation preserves essential keyboard switching and field-specific controls. Android's ordinary emoji + three-language row grows Space by approximately 25–35% in tested 320–480 dp configurations. iOS keeps native control targets where feasible; gain is smaller on compact devices (about 15–20%) and reaches about 25% at Pro Max widths, rather than sacrificing essential controls to force a percentage. Strict adjacent hit frames share their actual rounded edges; exact-boundary regression covers 2×/3× densities. Existing default layout selection remains internal RU/KK/EN.
- **AI:** pinned sparkle + AI control is 64×44 pt on iOS and 64×48 dp on Android. Persona scrolling and selection are preserved.
- **Another reply:** labeled localized action stays beneath the response in the existing action area. Adaptive action/pagination rows preserve Insert and edited response history, with 44 pt/48 dp targets. Swift and Kotlin flow/geometry tests cover compact widths and larger fonts. Full interactive regeneration/history/insert testing in third-party host apps remains manual QA.
- **Punctuation:** both platforms return to the current language's letters after common/consecutive punctuation and one Space in ordinary text. Numeric, phone, email, URL, social and secure contexts are protected. No extra network requests or permission grants were introduced.
- **Onboarding/video:** four genuine localized 24-second silent H.264 recordings per platform, native AVKit/VideoView replay, captions burned into each language recording, optional onboarding entry and permanent Settings/keyboard setup entry. Manual Paste demo explicitly does not verify automatic paste permission. See `2026-10-23-tutorial-verification.md` for actual asset, limitation and physical-device capture procedure.
- **Push:** shared audience eligibility preserved, additive matching/exclusion counts and masked sample, guarded Send, transactional recipient snapshot (migration 0020), dispatch-time consent recheck, and campaign/channel-aware worker claims. iOS retries APNs registration after failure on the next foreground opportunity. No unconfigured-token sending, bulk tests, production DB writes or secret inspection. The original production zero-count cause remains unproven; known exclusion paths are now visible. See `2026-10-23-push-verification.md`.

### Build and test evidence

- Baseline iOS simulator build passed.
- First full iOS run: 612 tests, 139 assertions failed, all from two geometry cases: floating-point boundary overlap and compact emoji category padding. Both causes were repaired; added exact shared-edge regression, without weakening overlap checks.
- Focused keyboard rerun: 80 tests, 0 failures.
- Earlier full iOS: `xcodebuild -project AI-Reply/AIReply.xcodeproj -scheme AIReply -configuration Debug -destination 'platform=iOS Simulator,id=BCA47869-5FAA-4CB0-BEF5-005068C46720' -derivedDataPath /tmp/ai-reply-audit-ios -parallel-testing-enabled NO CODE_SIGNING_ALLOWED=NO test`: **613 tests, 0 failures, TEST SUCCEEDED**. Result bundle `/tmp/ai-reply-audit-final-tests.xcresult`; log `/tmp/ai-reply-audit-ios-final-tests.log`.
- Final media/localization verification after caption refinements: **30 tests, 0 failures, TEST SUCCEEDED**; `/tmp/ai-reply-audit-media-final-tests.log`.
- Android: bundled Android Studio JDK, `./gradlew testDebugUnitTest assembleDebug lintDebug --console=plain`: **820 tests, 72 suites, 0 failures**, APK built/installed; lint 0 errors, 182 warnings. Baseline warning count was not separately saved, so the report does not assert every warning pre-existed. Details and commands in `2026-10-23-android-verification.md`.
- Backend: `go test ./... -count=1` passed after final worker correction; focused concurrency/queued snapshot/email-only worker race suite passed. Admin JS syntax, 8 confirmation states and compiled modified Vue templates passed. No provider-send test used real recipients. Details in push report.
- Firebase client project/package validation passed. This confirms client configuration consistency, not real delivery.
- `git diff --check`, Xcode project plist lint and optimized media decode passed.

### Simulator/emulator coverage and limits

Booted iPhone SE 3, iPhone 17, iPhone 17 Pro, iPhone 17 Pro Max on iOS 26.5, and Android `rbt_api36` API 36. SE/Pro setup and Settings reproduction, iPhone 17 actual custom keyboard screenshot (large Space and AI control) and authentic tutorial recorded. File `evidence/ios-17-keyboard-ru.png` shows the real extension. Pure geometry/state regressions cover compact/standard/large widths, all layouts and specialized contexts. App is iPhone-only; no iPad support is claimed.

Android app guide screenshots cover 320/393/480 dp, font scales 1/1.3 and landscape. IME enabled/selected and final APK installed. The emulator's unbundled window could not be controlled by available native accessibility tooling; no interactive keyboard tap results were invented. iOS custom key controls likewise do not all appear in the simulator's accessibility automation tree, and coordinate input sometimes failed with `noWindowsAvailable`; actual screenshot and deterministic tests are distinguished from complete manual interaction proof. Native keyboard language switching was used to reach AI Reply on iPhone 17.

Still required for release: complete manual RU/KK/EN generation/regeneration/edit/insert, voice/emoji/long presses, accessibility, OEM hardware, third-party hosts and physical permission behavior. No actual FCM/APNs delivery was tested, as requested in the clarified scope. **Ready for development/internal QA; production submission readiness is not claimed.**


### Subsequent user requests included in the same change

- Free daily allowance: live public plans API reports 20, while the repository seed is already 7. Additive migration 0021 changes only the canonical free plan to 7 on backend startup. Known exact multilingual boilerplate mentioning 20 is updated; customized copy, paid plans, monthly quota, usage and one-time welcome bonus are preserved. A later admin edit remains effective because the migration runs once. Full Go regression passed; production still returns 20 until an authorized server deployment. See free-limit-restoration report.
- Tutorial language: replaced the initial shared Russian footage with four genuine localized recordings per platform, with instructions burned into the video itself. The resource follows the effective app language (system default unless overridden). iOS Simulator has genuine Kazakh Settings; Uzbek uses real English system labels because this runtime has no Uzbek Settings resources, with explicit localized guidance. Android instructions remain platform-specific. Final asset/test counts follow in the language verification addendum.


### Final localized tutorial verification

The final four-resource iOS build and full suite passed: **616 tests, 0 failures, TEST SUCCEEDED**, `/tmp/ai-reply-audit-localized-approved-tests.xcresult`. A compile check during the async player refactor identified a missing `await` on AVPlayer seek; it was corrected with a cancellation guard before installing the new player. Resource selection, fallback, progress separation and all four H.264/no-audio files are tested. English/Kazakh/Uzbek contact-sheet review verified actual language and pasted results; late/blank recording segments were recaptured before delivery. The previous shared Russian MP4 was removed from app resources.


Final iPhone 17 check with system/app Kazakh: the permanent Paste help link opened the Kazakh recording, native Watch switched to Replay during playback, and both the actual app footage and embedded captions were Kazakh. Evidence `evidence/ios-17-final-kazakh-player.png`. Simulator system language was left at Kazakh with English fallback.


Product-copy refinement: removed simulator/emulator wording and capture diagnostics from the tutorial UI and burned captions in all four languages on both platforms. The normal permission instructions remain; the engineering limitations above remain recorded for QA. Final four-video re-encoding and asset checks were repeated after this copy change.

Final product-copy verification: **616 iOS tests, 0 failures, TEST SUCCEEDED**, result `/tmp/ai-reply-audit-final-copy-tests.xcresult`. Four final assets are H.264/24 seconds/no audio, with updated permission instructions inside the video. Source localization check found zero capture-tool terms in tutorial strings.

Final Android source and four-asset verification: **820 tests across 72 suites, 0 failures/errors/skips**, `assembleDebug` and `lintDebug` passed (0 lint errors, 182 warnings). Final product tutorial strings contain no simulator/emulator wording. Independent final metadata inspection confirmed all eight bundled recordings are H.264, 23.97–24.00 seconds, with no audio/subtitle streams.
