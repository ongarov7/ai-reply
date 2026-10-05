# iOS → Android feature parity

Source of truth: the Swift project at `~/ai-reply/AI-Reply` (app target `AIReply`,
extension target `ReplyKeyboard`, shared sources in `Shared/`).

This file is the implementation checklist. It was written from an audit of the
iOS sources before any Kotlin was written, and updated after implementation.

**Status vocabulary** (as required by the brief):

| Status | Meaning |
|---|---|
| `IMPLEMENTED_AND_VERIFIED` | Built and exercised, result observed |
| `IMPLEMENTED_NOT_DEVICE_VERIFIED` | Written and statically checked; not run on a device/emulator in this session |
| `PARTIALLY_IMPLEMENTED` | Core path present, a sub-behaviour is missing |
| `BLOCKED` | Cannot be done in this environment; reason recorded |
| `NOT_IMPLEMENTED` | Deliberately not ported; reason recorded |

> **Status update, 27.09.2026 — keyboard refactor.** The project builds now:
> AGP 8.7.3 on JDK 17+, `./gradlew assembleDebug testDebugUnitTest` — 115 JVM
> tests in 13 classes, all green. The keyboard (§5) and the reply flow (§6) were
> rebuilt to match the refactored iOS keyboard and exercised on an emulator
> (Pixel 6, API 34) through the debug mock transport (Settings ▸ Developer ▸
> Mock AI replies, debug builds only). Their rows say what was seen working and
> what only tests cover.
>
> Rows this refactor did not touch keep the status they got when the app was
> first ported: written and now compiled, but not re-checked on a device.

---

## 1. Application shell

| Feature | iOS implementation | Android equivalent | Status | Notes |
|---|---|---|---|---|
| App entry | `AIReplyApp` (`@main`, SwiftUI `App`), branches onboarding vs `HomeView` on `profile.hasCompletedOnboarding` | `MainActivity` (`ComponentActivity`) + `AppNavHost`, same branch on the same persisted flag | IMPLEMENTED_NOT_DEVICE_VERIFIED | Same single decision point, same stored field |
| DI / object graph | Swift singletons (`ProfileStore.shared`, `SharedSettings.shared`, `AIConfiguration.shared`) | `ServiceLocator` held by `AIReplyApplication`, `applicationContext` only | IMPLEMENTED_NOT_DEVICE_VERIFIED | No Hilt: an IME pays DI graph construction on every cold start |
| Observable config | `@Observable ReplyConfigurationModel` in the SwiftUI environment | `ConfigurationRepository` exposing `StateFlow<ReplyConfiguration>`, collected by Compose | IMPLEMENTED_NOT_DEVICE_VERIFIED | Same "one instance, every screen edits the same value" rule |
| Light / dark | `AppearancePreference` (system/light/dark) → `preferredColorScheme` | Same enum → `AIReplyTheme(darkTheme = …)` | IMPLEMENTED_NOT_DEVICE_VERIFIED | Dynamic colour deliberately off; the product has its own accent |
| Interface language | `AppLanguage` en/ru/kk, `nil` = follow system; drives `\.locale` | Same enum; `LocalizedContext.wrap()` in `attachBaseContext`, `recreate()` on change | IMPLEMENTED_NOT_DEVICE_VERIFIED | See §7 |

## 2. Domain model

Every type below is a direct port, field for field, including the tolerant
decoding that lets a configuration written by an older build still load.

| iOS type | Android type | Status | Notes |
|---|---|---|---|
| `UserProfile` (+ 1000/120 char clamps, `promptDescription`, `hasAnyContext`) | `UserProfile` | IMPLEMENTED_AND_VERIFIED | Clamped by code point, matching iOS's Unicode-scalar counting; covered by `TextLimitsTest` |
| `BusinessContext` (offering/summary/rules, `cleanRules`, max 8 rules) | `BusinessContext` | IMPLEMENTED_AND_VERIFIED | Rule cleaning and the cap are executed in `ConfigurationTest` / `ReplyPromptBuilderTest` |
| `ReplyTone`, `ReplyLength`, `EmojiPolicy`, `WorkingHoursBehaviour`, `RelationshipKind` | same five enums | IMPLEMENTED_NOT_DEVICE_VERIFIED | Same raw values on the wire (`mention_when_relevant` etc.) |
| `TimeOfDay`, `DaySchedule`, `WorkingHours` (+ `Context` derivation, weekday grouping) | same | IMPLEMENTED_AND_VERIFIED | 11 executed tests, including weekday grouping and the Sunday-is-1 convention |
| `ReplyTemplate` (built-ins + custom, per-template business/hours override) | `ReplyTemplate` | IMPLEMENTED_NOT_DEVICE_VERIFIED | Built-in ids stay `friend`/`client`/`business`/`work` |
| `ReplyConfiguration` + `normalized()` repair | same | IMPLEMENTED_NOT_DEVICE_VERIFIED | Same repair rules: restore missing built-ins, unhide if all hidden, re-index |
| `TemplateSummary` (first-frame chip cache) | same | IMPLEMENTED_NOT_DEVICE_VERIFIED | Same reason: draw the chip row before the full config is read |

## 3. Storage

| Feature | iOS | Android | Status | Notes |
|---|---|---|---|---|
| Shared state between app and keyboard | App Group `group.kz.yerek.replykeyboard` + `UserDefaults` suite | Same process and same app → app-private `SharedPreferences` | IMPLEMENTED_NOT_DEVICE_VERIFIED | Android has no App Group problem to solve; the whole `AppGroup.isAvailable` / "container unavailable" failure mode disappears |
| Profile + templates | JSON file in the App Group container, mtime-cached decode | JSON file in `filesDir`, same mtime cache, `kotlinx.serialization` | IMPLEMENTED_NOT_DEVICE_VERIFIED | Not Room, for the same reason iOS is not Core Data: the IME must not pay store-stack setup on appearance |
| Atomic write | `Data.write(options: .atomic)` | temp file + `renameTo` | IMPLEMENTED_NOT_DEVICE_VERIFIED | A keyboard reading mid-write sees old or new, never truncated |
| API credential | Keychain, `kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly`, access group | Android Keystore AES/GCM key (`setUserAuthenticationRequired=false`), ciphertext in `SharedPreferences` | IMPLEMENTED_NOT_DEVICE_VERIFIED | Hand-rolled rather than `androidx.security:security-crypto`, which is alpha and deprecated; ~150 lines, no dependency. Key is non-exportable and device-bound, same guarantee class as the Keychain item |
| Keyboard height cache | `SharedSettings.keyboardHeight`, seeds the first frame | **NOT_IMPLEMENTED** | NOT_IMPLEMENTED | Android has no equivalent problem: the IME window sizes itself to the view, so there is no first-frame resize to pre-empt |

## 4. AI layer

| Feature | iOS | Android | Status | Notes |
|---|---|---|---|---|
| Entry point | `AIReplyService.generate(Request)` | `AIReplyService.generate(Request)` | IMPLEMENTED_NOT_DEVICE_VERIFIED | Same responsibilities, same boundaries |
| Incoming-message limit | Published by the server (`max_source_characters`, set in the admin panel, fallback 400), cached in the App Group, checked by `AIReplyService.validate` before any request | `AILimits` over `SharedPreferences`: same fallback, refreshed from `/api/v1/config` when older than 6 h; a server rejection that names `source_text` stores the server's limit | IMPLEMENTED_AND_VERIFIED | `AILimitsTest`, `AIReplyServiceValidationTest`. Checked before the network call, so an over-long paste costs nothing; the composer counts `N / limit` |
| Prompt construction | `ReplyPromptBuilder`: rules in the developer message, all user data in named blocks in the user message | same, byte-for-byte identical developer text **plus** one `INSTRUCTION` paragraph | IMPLEMENTED_AND_VERIFIED | 11 executed tests. The added paragraph is the only deliberate prompt delta; see §6 |
| Prompt-injection posture | `<incoming_message>`, `<user_profile>`, `<business_context>`, `<user_rules>`, `<template_instructions>` introduced as data | same blocks + `<user_instruction>` | IMPLEMENTED_AND_VERIFIED | A test asserts that "Ignore previous instructions" reaches the user message and never the developer message |
| Relationship guidance | Hard-coded per `RelationshipKind`, not stored per template | same | IMPLEMENTED_NOT_DEVICE_VERIFIED | Keeps it out of user data and migratable |
| Transport abstraction | `protocol ReplyTransport` + Direct / Backend | `interface ReplyTransport` + Direct / Backend | IMPLEMENTED_NOT_DEVICE_VERIFIED | |
| Direct OpenAI | `/v1/responses`, `store:false`, `max_output_tokens` 180, temp 0.7, 25 s | same, `HttpURLConnection` | IMPLEMENTED_NOT_DEVICE_VERIFIED | No OkHttp/Retrofit: fewer classes to load in the IME |
| Backend transport | Structured JSON to a self-hosted service, bearer token | same wire format | IMPLEMENTED_NOT_DEVICE_VERIFIED | Field names preserved, including the legacy `keyboard_language` |
| Error set | `AIReplyError`, 12 closed cases; a burst of requests (`rateLimited`, wait a moment) and a spent plan (`quotaExhausted`, wait until tomorrow or change plan) are separate | same sealed interface, same split | IMPLEMENTED_AND_VERIFIED | `AccountApiTest` maps every backend error. No status codes or response bodies ever reach the UI |
| Draft normalisation | `ReplyDraftNormalizer`, URL-preserving | same | IMPLEMENTED_AND_VERIFIED | 7 executed tests, including that a URL survives space collapsing |
| Response unwrapping | `ReplyNetworking.unwrapQuotes` (`"`, `“”`, `«»`) | same | IMPLEMENTED_NOT_DEVICE_VERIFIED | |
| Model / mode / backend URL settings | `AIConfiguration` in App Group defaults | `AIConfiguration` over `SettingsStore` | IMPLEMENTED_NOT_DEVICE_VERIFIED | `gpt-4o-mini` default named in exactly one place, as on iOS |
| Per-install id | Random UUID, never hardware-derived | same | IMPLEMENTED_NOT_DEVICE_VERIFIED | |

## 5. Keyboard

| Feature | iOS | Android | Status | Notes |
|---|---|---|---|---|
| Host | `UIInputViewController` extension | `ReplyKeyboardService : InputMethodService` | IMPLEMENTED_AND_VERIFIED | Declared with `android.view.InputMethod` + `res/xml/method.xml`; enabled and used on the emulator |
| Layouts | Apple's own layouts: QWERTY 10/9/7, ЙЦУКЕН 11/11/9 (`ё`, `ъ` on long press), Kazakh = 9-letter top row `ә і ң ғ ү ұ қ ө һ` + ЙЦУКЕН | Gboard's: the same letters, digits as corner hints on the QWERTY / ЙЦУКЕН row (long press types the digit) | IMPLEMENTED_AND_VERIFIED | `KeyboardLayoutTest` (every Kazakh letter typeable, rows pinned); Kazakh typing seen on the emulator |
| Number / symbol pages | iOS `123` / `#+=` | Gboard `?123` / `=\<`; `₸` on the currency key for Kazakh and Russian, `$` for English | IMPLEMENTED_AND_VERIFIED | `KeyboardLayoutTest`; `?123` seen on the emulator |
| Bottom row | Plane key, globe (when iOS asks for it), layout key, space, return; `@`, `.`, `/` or `#` in e-mail, URL and social fields | Plane key, comma (or `@` / `/` in e-mail / URL fields), layout key, space, `.` with alternates, return; the globe replaces the comma only when `shouldOfferSwitchingToNextInputMethod()` | IMPLEMENTED_AND_VERIFIED | `KeyboardLayoutTest` |
| Metrics | Width-derived size table; every page of every enabled layout gets the height of the tallest (5 rows with Kazakh) | Same table in dp; the key area is capped at 45 % of the screen height in portrait, 50 % in landscape | IMPLEMENTED_AND_VERIFIED | `KeyboardGeometryTest`: one height for every page, keys ≥ 37 × 24 dp from 320 to 480 dp wide |
| Touch model | One view, hit frames that tile the whole key area; the letter is typed on release | One `Canvas` + one pointer handler; the same tiling, typed on release | PARTIALLY_IMPLEMENTED | Tiling is tested (`KeyboardGeometryTest`: every point belongs to exactly one key). Slide-to-correct, rollover, long-press alternates (380 ms) and the space-bar trackpad are implemented; only long press was tried on the emulator, multi-touch needs a phone |
| Theme | Trait collection first, `keyboardAppearance` only as a fallback (it goes stale after a system switch) | Resolved from `EditorInfo.IME_FLAG_*`/`Configuration.uiMode` + the user's appearance override | IMPLEMENTED_NOT_DEVICE_VERIFIED | Dark theme not tried on the emulator |
| Shift / caps lock | Tap toggles, double tap within 0.32 s locks | same, same threshold | IMPLEMENTED_AND_VERIFIED | `KeyboardTypingTest` |
| Auto-shift at sentence start | Follows the text before the caret, in the host and in the composer | same | IMPLEMENTED_AND_VERIFIED | `KeyboardTypingTest`; seen on the emulator |
| Double-space → `. ` | 0.45 s window, only after a letter or digit | same | IMPLEMENTED_AND_VERIFIED | `KeyboardTypingTest` |
| Delete repeat | 0.45 s, then 0.085 s; whole words after a while | 0.42 s, then 65 ms; whole words after 14 repeats | IMPLEMENTED_NOT_DEVICE_VERIFIED | Word length is tested (`KeyboardTypingTest`), the timing is not |
| Layout switch key | `ҚАЗ` / `РУС` / `ENG`, cycles the layouts switched on in Settings | same; a long press opens a layout picker | IMPLEMENTED_AND_VERIFIED | `KeyboardLayoutTest` |
| System keyboard switch | Globe → `handleInputModeList`, shown only when `needsInputModeSwitchKey` | Globe → `switchToNextInputMethod`; long press on the globe or the space bar → `showInputMethodPicker` | IMPLEMENTED_NOT_DEVICE_VERIFIED | |
| Return key label + prominence | Follows `returnKeyType` (send/search/go/done) | Follows `EditorInfo.imeOptions` action | IMPLEMENTED_NOT_DEVICE_VERIFIED | Same word list per language |
| Key click sound / haptics | `UIDevice.playInputClick()` | `AudioManager.playSoundEffect` + `performHapticFeedback`, both honouring the system settings | IMPLEMENTED_NOT_DEVICE_VERIFIED | Android exposes the user's own sound/vibrate-on-keypress settings; both are read, not assumed |
| Accessibility | VoiceOver labels per key | One semantics node per key over the canvas, for TalkBack | IMPLEMENTED_NOT_DEVICE_VERIFIED | TalkBack not tried |

## 6. AI reply flow

| Feature | iOS | Android | Status | Notes |
|---|---|---|---|---|
| Context acquisition | selection first (`proxy.selectedText`), then clipboard, only on a persona tap or Paste | `getSelectedText()` first, then `ClipboardManager`, only on a persona tap or Paste | IMPLEMENTED_AND_VERIFIED | No polling, no read on appearance, no background access — identical posture |
| Clipboard permission | Requires "Allow Full Access"; `fullAccessRequired` error | No equivalent gate; the current IME may read the clipboard | IMPLEMENTED_NOT_DEVICE_VERIFIED | The error case is kept in the model but is unreachable on Android |
| Sensitive clipboard | n/a | Clips flagged `EXTRA_IS_SENSITIVE` (API 33+) are refused | IMPLEMENTED_NOT_DEVICE_VERIFIED | Android-only hardening, matching iOS's "secure fields are never processed" promise |
| Password fields | iOS keyboards are simply not shown a secure field's content | AI panel disabled when `EditorInfo.inputType` is any password variation | IMPLEMENTED_NOT_DEVICE_VERIFIED | Typing still works normally |
| Persona row | `✨` (write a new message with AI) pinned at the leading edge, then compact chips filling the rest, the selected one highlighted, the last one remembered; while a word is typed the suggestions take exactly the chips' place and `✨` stays. No `+`: templates are created in the app | same | IMPLEMENTED_AND_VERIFIED | Selection and the remembered persona were seen on the emulator; `PersonaRowLayoutTest` pins the order and the chips' width |
| Composer | Persona, source preview or Paste, `N / limit` counter, close; instruction field with quick intents; Reply | same, plus a microphone | IMPLEMENTED_AND_VERIFIED | `ReplyComposerFlow` is the same state machine on both: composing → generating → result ⇄ editing → conflict |
| Generation trigger | **Only** Reply, Regenerate or Try again | same | IMPLEMENTED_AND_VERIFIED | Opening the keyboard, copying text or picking a persona never starts a request |
| Draft editing | The composer edits its own text view without becoming first responder; tap anywhere in the reply to put the caret there | `KeyboardTextFieldState` with its own caret; the same tap-to-place | IMPLEMENTED_AND_VERIFIED | Grapheme-safe (emoji, Kazakh letters); seen on the emulator |
| Versions | Regenerate adds a version (up to 6) and never overwrites an edit; `‹ 1/2 ›` | same `ReplyDraftHistory` | IMPLEMENTED_AND_VERIFIED | `ReplyComposerFlowTest`; on the emulator an edited 1/2 survived a Regenerate to 2/2 |
| Keys during generation | Dimmed and dropped, never leak into the host | same | IMPLEMENTED_AND_VERIFIED | |
| Stop / Back / errors | Stop keeps everything and a late answer is ignored; Back returns to the instruction; a failure keeps the instruction and offers Try again | same | IMPLEMENTED_AND_VERIFIED | `ReplyComposerFlowTest`; `#offline`, `#quota`, `#slow` tags in the mock transport |
| Insert | `insertText` of the text on screen, edits included, trimmed; never sends | `commitText` of the same; never sends | IMPLEMENTED_AND_VERIFIED | No Accessibility service anywhere in the project |
| Host field not empty | Replace / Add / Cancel inside the composer, which keeps its height | same | IMPLEMENTED_AND_VERIFIED | Replace was seen inserting the edited text. Android clears with one `deleteSurroundingText` |
| Append separator | Space unless the text already ends in whitespace | same | IMPLEMENTED_NOT_DEVICE_VERIFIED | |
| Teardown | Keyboard hidden: the request stops, the session is kept in memory for 10 minutes, then dropped | same, in `onFinishInputView` / `onStartInputView` | IMPLEMENTED_NOT_DEVICE_VERIFIED | Nothing is written to disk |

## 7. Localization

| Feature | iOS | Android | Status | Notes |
|---|---|---|---|---|
| Languages | en, ru, kk (249 keys in `Localizable.xcstrings`) | same three, `values/`, `values-ru/`, `values-kk/` | IMPLEMENTED_NOT_DEVICE_VERIFIED | Converted from the catalogue, not retyped |
| UI language ≠ AI reply language | Reply language follows the incoming message, always | same rule, same developer-message wording | IMPLEMENTED_NOT_DEVICE_VERIFIED | UI ru + reply kk is expressible, as required |
| UI language ≠ key-cap language | `AIReplyStrings` (app language) vs `KeyboardStrings` (layout language) — two hard-coded Swift tables, because an extension cannot see the app's chosen language | Two `createConfigurationContext` contexts over the same `strings.xml` | IMPLEMENTED_NOT_DEVICE_VERIFIED | **Android is better here**: the duplication iOS needed disappears, and no UI string is written in Kotlin |
| Language override | Settings picker, `nil` = system | same | IMPLEMENTED_NOT_DEVICE_VERIFIED | |

## 8. Voice (Android addition)

| Feature | iOS | Android | Status | Notes |
|---|---|---|---|---|
| Dictation in the app | `SFSpeechRecognizer` + `AVAudioEngine`, 60 s cap, editable transcript | `SpeechRecognitionClient` over `android.speech.SpeechRecognizer`, 60 s cap, editable transcript | IMPLEMENTED_NOT_DEVICE_VERIFIED | Same states, same "recording stops when you start typing" rule |
| Dictation in the keyboard | **Impossible** — an iOS keyboard extension cannot capture audio | **Implemented** — an Android IME can, with `RECORD_AUDIO` | IMPLEMENTED_NOT_DEVICE_VERIFIED | The headline Android-only feature |
| Permission from the IME | n/a | `VoicePermissionActivity`, a transparent Activity, because a Service cannot request a runtime permission | IMPLEMENTED_NOT_DEVICE_VERIFIED | Handles denied, denied-permanently and return-from-Settings |
| Provider abstraction | n/a | `SpeechRecognitionClient` interface; `AndroidSpeechRecognitionClient` is one implementation | IMPLEMENTED_NOT_DEVICE_VERIFIED | Swapping to server-side transcription is a new class, not a keyboard rewrite |
| On-device recognition | `supportsOnDeviceRecognition` | `createOnDeviceSpeechRecognizer` on API 33+, network recogniser below | IMPLEMENTED_NOT_DEVICE_VERIFIED | |
| Unsupported language | Named plainly ("not available for Қазақша on this device") | same, from `ERROR_LANGUAGE_NOT_SUPPORTED` / `ERROR_LANGUAGE_UNAVAILABLE` | IMPLEMENTED_NOT_DEVICE_VERIFIED | Kazakh coverage is genuinely patchy on both platforms |
| Voice → structured config | `VoiceConfigurationParser` proposes working hours / rules from dictation, applied only on confirmation | **NOT_IMPLEMENTED** | NOT_IMPLEMENTED | Deliberate: it is a heuristic Russian/Kazakh/English phrase parser with its own test suite, orthogonal to the port, and no Android surface needs it yet. Dictated text still lands verbatim in the profile, which is the behaviour that matters. Recorded as a follow-up in the README |

## 9. Main app screens

| Screen | iOS | Android | Status |
|---|---|---|---|
| Onboarding (7 steps: welcome, profile, hours, key, keyboard, usage, test) | `OnboardingView` | `OnboardingScreen` | IMPLEMENTED_NOT_DEVICE_VERIFIED |
| — per-step persistence, skip, back | yes | yes | IMPLEMENTED_NOT_DEVICE_VERIFIED |
| Home | `HomeView` (header, missing-key card, try-it, setup links, keyboard status, how it works, privacy) | `HomeScreen`, same sections in the same order | IMPLEMENTED_NOT_DEVICE_VERIFIED |
| Keyboard setup guide | `KeyboardSetupView` + checklist with a real "not known yet" state | `KeyboardSetupScreen`; the checklist is *actually knowable* on Android | IMPLEMENTED_NOT_DEVICE_VERIFIED |
| Profile editor | role, business, rules, about + counter + dictate, tone | same | IMPLEMENTED_NOT_DEVICE_VERIFIED |
| Working hours | full per-day editor | same | IMPLEMENTED_NOT_DEVICE_VERIFIED |
| Template list | reorder, hide, delete custom, add | same | IMPLEMENTED_NOT_DEVICE_VERIFIED |
| Template editor | basic, business, hours override, rules, style, instructions | same | IMPLEMENTED_NOT_DEVICE_VERIFIED |
| Settings | profile links, setup, API key, model, appearance, language, privacy, re-run onboarding | same | IMPLEMENTED_NOT_DEVICE_VERIFIED |
| Try a reply (`ComposeView`) | paste/dictate, template, generate, copy, regenerate | same, plus the instruction field | IMPLEMENTED_NOT_DEVICE_VERIFIED |
| API key editor | paste-first, `sk-` shape check, never redisplayed | same | IMPLEMENTED_NOT_DEVICE_VERIFIED |
| Debug screen launcher (`-AIReplyDebugScreen`) | DEBUG only | **NOT_IMPLEMENTED** | NOT_IMPLEMENTED — Android Studio previews and deep links cover it |

## 10. Cross-cutting

| Concern | iOS | Android | Status | Notes |
|---|---|---|---|---|
| No third-party dependencies | Apple frameworks only | AndroidX + Compose + `kotlinx.serialization` only; no networking, DI or crypto library | IMPLEMENTED_NOT_DEVICE_VERIFIED | Matches the stated project rule |
| Config read off the main thread | Once per appearance, `qos: .userInitiated` | Once per `onStartInputView`, `Dispatchers.IO`, throttled to 1 s | IMPLEMENTED_NOT_DEVICE_VERIFIED | Same throttle, same reason |
| Nothing expensive at keyboard start | Prewarm only after `viewDidAppear` | Transport, recogniser and JSON parser all lazy; nothing touches disk or network in `onCreateInputView` | IMPLEMENTED_NOT_DEVICE_VERIFIED | |
| No Activity/Context leaks | n/a | `ServiceLocator` holds `applicationContext`; the IME's scope dies with the service | IMPLEMENTED_NOT_DEVICE_VERIFIED | |
| Logging never carries message text | `ReplyLog`, lengths and outcomes only, DEBUG only | `ReplyLog`, same rule, `BuildConfig.DEBUG` only | IMPLEMENTED_NOT_DEVICE_VERIFIED | |
| Secrets out of the repo | Key typed at runtime, nothing in the IPA | Same; nothing in the APK, `local.properties` untouched, `.gitignore` covers it | IMPLEMENTED_NOT_DEVICE_VERIFIED | |
| Responsive layout | Portrait-locked, width-derived metrics | Portrait + landscape, width- and height-derived; `readableWidth` cap; font-scale respected in the app, pinned in the key grid | IMPLEMENTED_NOT_DEVICE_VERIFIED | Pinning the key grid's font scale stops a 2× accessibility font from breaking key geometry |
| Unit tests | XCTest, 138 tests | 13 JVM test classes, 115 tests | IMPLEMENTED_AND_VERIFIED | `./gradlew testDebugUnitTest`, all green |
| `./gradlew assembleDebug` | n/a | builds on the Mac with JDK 17+ | IMPLEMENTED_AND_VERIFIED | AGP 8.7.3 needs JDK 17+; Android Studio Electric Eel's bundled JBR 11 is too old |
