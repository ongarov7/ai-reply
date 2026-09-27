# AI Reply — migration pass 1

Scope agreed before starting: **performance and foundation**. No new product
features. AI replies stay **simulated locally** — there is no backend and none
was invented.

`README.md` documents the previous (proof-of-concept) pass. Where the two
disagree, this file is newer. The keyboard refactor of September 2026 is newer
than both: see `../HANDOFF.md` and `../docs/keyboard-refactor-report.md`.

---

## 1. What the project actually was

An audit of every file, not a summary of the README:

| Area | State before this pass |
| --- | --- |
| Technology | 100% native Swift. No Flutter, no React Native, no third-party packages. |
| Main app | 4 files. A setup-instructions screen plus a speech-to-text test harness. |
| Keyboard extension | 11 files, ~2,500 lines. `UIInputViewController`, hand-built EN/RU/KK layouts, page cache, touch-down key firing, delete repeat, caps lock, auto-shift, host-driven dark theme. Genuinely good work. |
| App Groups | **None.** No entitlements file existed. |
| Keychain | **None.** |
| Networking | **None.** No `URLSession` anywhere. |
| Authentication | **None.** |
| Backend endpoints | **None.** |
| AI | `ReplySimulator.swift` — 28 lines of keyword→string rules. |
| Localization | No `.strings`, `.xcstrings` or `.lproj`. The keyboard had a hand-rolled `KeyboardStrings` table; **the main app was hardcoded English throughout**. |
| Microphone | Native `Speech` + `AVAudioEngine`, app only, hardcoded to `en-US`. |
| App ↔ keyboard communication | **None.** The two shared nothing at all. |
| Analytics / deep links | None. |

The product gap was therefore much larger than "polish an existing app": almost
everything in the target spec (auth, backend, templates, quota, onboarding,
clipboard history, account) does not exist yet.

### Signing landmine found before anything was touched

`project.yml` declared `DEVELOPMENT_TEAM: VMF4X4NF22`. The checked-in
`project.pbxproj` uses **`JXM8N66QWU` at target level**, which overrides the
project-level value. Target level wins, so the app ships signed with
`JXM8N66QWU`.

Running `xcodegen generate` would have silently re-signed both targets with a
different team. `project.yml` has been corrected to match what actually ships,
and the divergence is documented in a comment at the top of that file. **No
signing configuration, certificate, bundle identifier or provisioning setting
was changed.**

---

## 2. Keyboard performance — root causes and fixes

The complaint was that switching to this keyboard stutters and typing feels
behind. Five distinct causes were found by reading the layout and lifecycle
paths. All five are fixed at the cause, not masked.

### 2.1 Hidden pages still paid full Auto Layout cost

`rowsContainer` was a plain `UIView`, and **every cached page's stack stayed
pinned to all four of its edges**. `isHidden` excludes a view from *drawing*,
not from *constraint solving* — only `UIStackView` arranged subviews get that
treatment. With EN + RU + KK + numbers + symbols cached, roughly **200 buttons'
constraints were solved on every layout pass** instead of the ~40 on screen.

**Fix.** Only the active page is in the view hierarchy. `install(_:)` /
`detach(_:)` add and remove it, keeping the four pin constraints on the page so
re-attaching costs four constraints. Cached pages that are not visible are fully
detached and cost nothing until displayed.

### 2.2 All three language layouts were built on the main thread at appear time

`scheduleKeyboardPagePrewarm()` ran from every `rebuild()` and built EN, RU and
KK in a **single `DispatchQueue.main.async` block** — ~120 `UIButton`s, their SF
Symbol image lookups and their constraints — at the exact moment the user had
just switched to the keyboard and was waiting for it to appear. This was the
single largest contributor to switching lag.

**Fix.** `scheduleIdlePrewarm()`:
- nothing is prewarmed until `viewDidAppear`;
- **one** page per idle tick (0.35s apart), so no runloop turn is long enough to
  drop a frame;
- the *next* language is built first, so the first language switch is instant;
- prewarmed pages are built **detached**, so they cost construction only and
  never enter a layout pass;
- cancelled on width or theme invalidation via a `DispatchWorkItem`.

### 2.3 Every key had an unbounded shadow

`KeyButton.init` set `shadowOpacity = 1` with **no `shadowPath`**. Core
Animation then derives the shadow shape from the layer's own alpha channel — one
offscreen render pass per key, per bounds change.

**Fix.** `KeyButton.layoutSubviews` sets an explicit `shadowPath`, cached against
bounds and corner radius. The drawn result is identical: `shadowRadius` is 0 and
the offset is 1pt down, so this is a hard bottom edge, not a blur.

### 2.4 A cross-process read on every keystroke

`textDidChange` fires after every character. It called `refreshThemeIfNeeded()`,
which reads `textDocumentProxy.keyboardAppearance` — **an XPC round-trip to the
host app, in the typing hot path**. The README claimed proxy reads had been
removed from the hot path; that was only true of `documentContextBeforeInput`.

**Fix.** The appearance probe is skipped entirely while the change was our own
insertion, and throttled to at most once every 0.5s otherwise. Our own
insertions cannot change the host's appearance, and half a second is soon enough
for a colour swap.

### 2.5 The keyboard resized itself on every appearance

The height constraint was created inside the *first* `updateGeometry()`, i.e.
during the first layout pass. Until then iOS sized the input view itself, so
every appearance began with a visible resize from the system default height to
ours.

**Fix.** The idle height is cached in the App Group and the constraint is
installed in `viewDidLoad`, before anything is laid out, so the first frame is
already the right size. Only the **idle** height is cached — seeding a launch
with the taller composing height would open the keyboard oversized. A stale
value self-corrects on the first real layout.

### 2.6 Bonus: theme changes no longer destroy the page cache

`applyTheme(rerender: true)` used to call `invalidateKeyboardPages()` and rebuild
every layout from scratch — a visible hitch whenever the host app flipped between
light and dark while the keyboard was up. Pages now carry an `allButtons` list
and are restyled in place. A colour change invalidates no geometry.

### What did NOT change

Everything the keyboard already did well was left alone: the globe key wiring
(`handleInputModeList(from:with:)` on `.allTouchEvents`), touch-down key firing,
delete repeat timings, caps lock, auto-shift, double-tap space, the EN/RU/KK
layouts and their key geometry, the composer, the clipboard policy, and the
`KeyboardStrings` design where labels follow the *layout* rather than the system
language.

---

## 3. Shared state: App Group

`group.kz.yerek.replykeyboard`, declared in `Config/AIReply.entitlements` and
`Config/ReplyKeyboard.entitlements` and in `project.yml` so both the checked-in
project file and a future `xcodegen generate` agree.

`Shared/` is compiled into **both** targets rather than built as a framework: a
dynamic framework would add load time to the keyboard, which is the one place
this app cannot afford it. Everything in `Shared/` is Foundation-only and
extension-safe (`APPLICATION_EXTENSION_API_ONLY` stays `YES`).

| File | Holds |
| --- | --- |
| `AppGroup.swift` | The identifier, the shared `UserDefaults`, and an honest availability probe. |
| `AppLanguage.swift` | The app's interface language (EN/RU/KK), separate from the keyboard's layout language. |
| `SharedSettings.swift` | Keyboard layout, app language, appearance, cached keyboard height, keyboard status. |

Two deliberate decisions:

**Fail-safe fallback.** If the App Group suite cannot be opened, `AppGroup.defaults`
falls back to this process's own container. A keyboard that quietly keeps its
settings to itself because a provisioning profile has not been regenerated yet
is a far better failure than one that crashes inside WhatsApp. `AppGroup.isAvailable`
uses `FileManager.containerURL(forSecurityApplicationGroupIdentifier:)`, which is
the only reliable probe — `UserDefaults(suiteName:)` succeeds even without the
entitlement — and Settings surfaces it when the group is not yet live.

**Migration.** The keyboard's previously private layout preference
(`ReplyKeyboard.selectedLanguage` in the extension's own container) is read once
and migrated, so an existing install does not silently reset to English.

**Nothing sensitive goes here.** App Group defaults are a plain plist both
processes can read. When authentication arrives, tokens belong in the Keychain
with a shared access group; only derived non-sensitive flags surface here.

### Keyboard status, honestly

iOS exposes no API that tells a containing app whether its own keyboard has been
enabled — `UITextInputMode` lists languages, not extension identifiers. Instead
the extension writes a timestamp and a Full Access flag when it runs, and Home
reads it: if the keyboard has run, the user enabled it. The write is throttled
(5 minutes, or on a Full Access change) and dispatched off the main thread so it
never sits in the appearance path. It records **no message content**.

---

## 4. Localization

`AIReply/Resources/Localizable.xcstrings` — **57 keys × EN / RU / KK, all three
complete**, hand-written, checked in context rather than machine-translated.

The main app was previously hardcoded English end to end, including every
microphone string. All of it is now localized: setup steps, keyboard status,
privacy copy, settings, and all 18 voice status messages.

Two design points:

- **The app language and the keyboard language stay separate types.** The app
  follows the system language (or an explicit override); the keyboard's own
  labels follow the layout the user selected on the keyboard. Someone with a
  Russian phone typing Kazakh must see `Жауап беру`. Merging them would force one
  of the two to be wrong. `KeyboardStrings` is unchanged for exactly this reason.
- **Status is a value, not a string.** `VoiceStatus` is an enum rendered through
  the current locale on demand, so switching language re-renders the message that
  is currently on screen instead of leaving a stale sentence in the old language.

Language override is applied with `.environment(\.locale, …)`, so it takes effect
without an app restart. **This needs verification on device** — see §8.

### Speech recognition bug fixed

`SFSpeechRecognizer` was hardcoded to `Locale(identifier: "en-US")`, so dictation
did not work at all for a Russian or Kazakh user. It now follows the app
language, and `SFSpeechRecognizer.supportedLocales()` is checked first: if Apple
does not support the language on that device (Kazakh may well not be), the user
is told so plainly in their own language instead of being handed a button that
can never produce text.

---

## 5. Main app structure and design system

```
AIReply/
  App/            AIReplyApp.swift, AppSettings.swift
  DesignSystem/   DesignSystem.swift
  Features/
    Home/         HomeView.swift
    Settings/     SettingsView.swift
    Voice/        VoiceReplyView.swift, VoiceReplyViewModel.swift
  Resources/      Localizable.xcstrings
Shared/           AppGroup.swift, AppLanguage.swift, SharedSettings.swift
Config/           AIReply.entitlements, ReplyKeyboard.entitlements
ReplyKeyboard/    (unchanged layout, 11 files)
```

The design system is deliberately ~150 lines: spacing, radius, two surfaces, a
card, a section header, a numbered step row and two button styles. Semantic
system colours and Dynamic Type text styles do the rest — reproducing them behind
custom names would only make the app drift away from the platform.

`ObservableObject`/`@Published` were replaced with `@Observable` (iOS 17), and
Settings now offers Appearance (System / Light / Dark) and interface Language,
both persisted to the App Group.

One subtle bug avoided along the way: `Text(condition ? "a" : "b")` can resolve
the ternary as `String` rather than `LocalizedStringKey`, which compiles happily
and then ships the raw key to the user. Those are explicitly typed.

---

## 6. Privacy

Unchanged where it was already right, and it was:

- The clipboard is read **only** inside the Reply tap handler. No polling, no
  timers, no read on appear.
- `UIPasteboard.hasStrings` is checked before `.string`, so the system paste
  prompt is not triggered unnecessarily.
- Message content is never logged. `ReplyLog` is `#if DEBUG` only and emits
  lengths, never text.
- The source message is never seeded into the reply draft.
- Nothing leaves the device, because there is no networking.

Added: the App Group carries a timestamp and a permission flag, nothing else.

**Full Access** remains genuinely required, for reading copied text and nothing
else. Typing, `selectedText`, layout switching and system keyboard switching all
work with it off. When AI moves to a backend it will also be required for
networking from the extension — there is no public way around that, and the
onboarding copy must say so plainly rather than calling it optional.

---

## 7. Project file

`AIReply.xcodeproj/project.pbxproj` was regenerated for the new folder structure
by `.migration-backup/sync_pbxproj.py`, which rebuilds only the file-bearing
sections and preserves targets, build configurations, signing, product
references and the Embed Foundation Extensions phase byte for byte. The original
is kept at `.migration-backup/project.pbxproj.original`.

Verified after generation:

- 83 id definitions, 83 referenced, **0 undefined, 0 duplicated**
- all 12 sections present and balanced; braces balanced
- 20 Swift files on disk, all in a Sources phase; the 3 `Shared/` files in **both**
- `Localizable.xcstrings` in a new Resources phase on the app target
- `CODE_SIGN_ENTITLEMENTS` on all four target configurations
- `knownRegions` = Base, en, kk, ru
- bundle identifiers, `DEVELOPMENT_TEAM`, `CODE_SIGN_STYLE`, `PRODUCT_NAME`,
  `APPLICATION_EXTENSION_API_ONLY` and `INFOPLIST_FILE` **identical to the original**

`project.yml` is now faithful, so `xcodegen generate` is the canonical path and
produces an equivalent project.

---

## 8. Build validation — NOT DONE

**Nothing here has been compiled.** The shell available to this session on the
Mac is a Linux VM with no Xcode and no Swift compiler, so the code was written
and reviewed carefully but never fed to a compiler.

```sh
xcodegen generate            # optional; project.yml is now correct
xcodebuild -project AIReply.xcodeproj -scheme AIReply \
  -destination 'generic/platform=iOS Simulator' \
  CODE_SIGNING_ALLOWED=NO build
```

Two items to watch specifically, because they are behavioural and not
compile-time:

1. **Live language switching.** `.environment(\.locale, …)` is the native way to
   switch localization without a restart. If a screen does not re-render in the
   new language, the fallback is an explicit bundle lookup.
2. **App Group provisioning.** The first build after this change needs Xcode to
   regenerate the provisioning profiles with the App Group capability. If it has
   not, Settings shows the warning and the keyboard still works — it just keeps
   its state to itself.

### Device tests worth running for this pass

- **Switching.** Apple keyboard → AI Reply → Apple → AI Reply, repeatedly, in
  Telegram and WhatsApp. No stutter, no size jump on appearance, and no
  degradation as the session goes on.
- **Typing.** Hold a letter row and type fast in all three layouts. Delete repeat,
  double-tap space, shift and caps lock all unchanged.
- **Language cycling.** EN → RU → KK → EN twice. The first switch should be
  instant (prewarmed); none should stutter.
- **Host theme flip.** Change Telegram between light and dark with the keyboard
  up. Colours change with no rebuild hitch.
- **App language.** System Russian, then Kazakh, then English. Then override in
  Settings and confirm it applies live.
- **Dictation.** Try each language, and confirm the unsupported-language message
  appears rather than a dead button.

---

## 9. What is deliberately still missing

Not started, by agreement — these are product features, not foundation:

backend and networking · authentication and Keychain · daily quota · onboarding ·
personalized and custom templates · clipboard history · home dashboard · account
screen · keyboard AI toolbar and editor mode · app icon and asset catalog
(`ASSETCATALOG_COMPILER_APPICON_NAME` is set but no `Assets.xcassets` exists —
this blocks App Store submission).

AI replies still come from `ReplySimulator`. It is 28 lines of deterministic
keyword rules, it is clearly named, and it produces no network traffic. Before
anything ships it must be moved behind a `ReplyProviding` seam with the mock
strictly separated from a production provider — `ReplyActionCoordinator` is
already the single place that calls it.

## 10. Risks

| Risk | Mitigation |
| --- | --- |
| Nothing compiled | Build before anything else; the changes are localized and reviewed, but this is the real gap. |
| App Group not yet provisioned | Fail-safe fallback plus a visible warning in Settings. |
| Hand-generated `project.pbxproj` | Structurally verified; `xcodegen generate` regenerates from a now-correct `project.yml`; original backed up. |
| Live language switching may need a restart | Verify on device; fallback is an explicit bundle lookup. |
| Two signing teams in one project | Preserved exactly as found and documented. Worth deciding which is correct. |

## 11. Next

1. Build both targets and fix whatever the compiler finds.
2. Run the device tests above and confirm the switching lag is actually gone.
3. Decide the backend contract — even with mocks, `AIReplyService` should be
   defined now so the mock and a future real provider are interchangeable.
4. Templates and the personalized template system.
5. Clipboard history (local, 10–20 items, dedupe, clear).
6. Onboarding, then auth and quota once a backend exists.
