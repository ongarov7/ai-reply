# AI Reply — Android

A native Kotlin port of the AI Reply iOS app: a messaging keyboard that writes
the reply for you. You copy a message in WhatsApp, Telegram, Instagram or
anywhere else, switch to the AI Reply keyboard, say who you are replying to and
what you want to say, and it drafts the message. You edit it, insert it, and
send it yourself.

The iOS project at `../AI-Reply` is the source of truth for everything the
product does. This one adds a microphone.

---

## What it is

Two things in one APK:

* **The app** — onboarding, your profile, reply templates, working hours,
  settings, and a "Try a reply" screen for checking that your setup produces the
  replies you expect.
* **The keyboard** — a full three-layout keyboard (English, Русский, Қазақша)
  with an AI reply panel above the keys.

### The reply flow

```
you copy a message          →  long press, Copy, in any chat app
switch to the AI keyboard   →  the persona row sits above the keys
tap who it is from          →  Friend · Client · Business · Work · your own
say what to write           →  type it, tap a quick intent, or use the microphone
Reply                       →  the model drafts the reply
edit it                     →  tap anywhere in it; it is your message now
Regenerate                  →  a new version; your edited one stays (‹ 1/2 ›)
Insert                      →  exactly the text on screen goes into the field
```

Nothing is ever sent for you. The messenger's own Send button stays under your
finger, and this project contains no accessibility service that could press it.

### What is different from iOS

| | iOS | Android |
|---|---|---|
| Dictating an instruction **inside the keyboard** | impossible — a keyboard extension cannot capture audio | **yes**, this is the headline addition |
| Knowing whether the keyboard is enabled | no API; the keyboard writes a timestamp and the app infers | Android answers directly, so the setup checklist is a fact |
| Opening the keyboard settings | can only open the app's own settings page | a public intent lands exactly where the user needs to be |
| Two string tables for two languages | two hand-written Swift tables | one `strings.xml`, read through a locale-configured `Context` |
| Key layouts | Apple's keyboards | Gboard's: digits as hints on the QWERTY / ЙЦУКЕН row, `?123` and `=\<` pages |

Everything else in the reply flow is the same on both, down to the state
machine. `docs/IOS_ANDROID_PARITY.md` records every difference, with reasons.

---

## Build

### Requirements

| | |
|---|---|
| Android Studio | Ladybug (2024.2) or newer |
| JDK | 17 (Android Studio's bundled JBR is fine) |
| Android SDK | API 35 platform, build-tools 35.0.0 |
| Gradle | 8.11.1, via the checked-in wrapper |
| AGP / Kotlin | 8.7.3 / 2.0.21 |
| Minimum device | Android 8.0 (API 26) |

Versions are pinned in `gradle/libs.versions.toml`. They are a combination known
to work together rather than the newest of each — see *Known limitations*.
Android Studio will offer to upgrade AGP and Kotlin; taking that offer is safe
as long as the Compose BOM and the Compose compiler plugin move with Kotlin.

### Android Studio

1. **File ▸ Open**, choose this folder, and let it sync. Studio writes
   `local.properties` with your SDK path on first sync; that file is
   git-ignored and must not be committed.
2. Run the `app` configuration on a device or emulator (API 26+).

### Command line

```bash
./gradlew assembleDebug           # debug APK → app/build/outputs/apk/debug/
./gradlew testDebugUnitTest       # unit tests
./gradlew lintDebug               # Android Lint
./gradlew assembleRelease         # needs a signing config; none is checked in
```

Each Play upload needs a higher `versionCode`. The defaults (1 / "1.0") stay
in `app/build.gradle.kts`; an upload overrides them instead of editing it:

```bash
./gradlew bundleRelease -Paireply.versionCode=7 -Paireply.versionName=1.0.6
```

A release build without `app/google-services.json` (no push) or without a
Google web client id (no "Continue with Google") still builds, but prints a
warning, since a store upload without them is almost always a mistake.

If `local.properties` is missing:

```bash
echo "sdk.dir=$HOME/Library/Android/sdk" > local.properties
```

`build-and-log.command` does all of the above and writes `build.log`.

### Sign-in (Google, e-mail)

The account screen offers **Continue with Google** (Credential Manager,
`GetSignInWithGoogleOption` with a fresh nonce) and **Continue with Email** (a
4-digit code sent by the server through Resend). Phone-number sign-in was
removed. The server verifies the Google ID token and the code; the app only
forwards them. Setup, in full: `../ai-reply-back-end/docs/AUTH.md`.

Google needs the OAuth client of type *Web application* at build time (it is
not a secret, but it differs per environment, so it is not checked in):

```bash
# local.properties (git-ignored) or ~/.gradle/gradle.properties
aireply.googleWebClientId=1234-abc.apps.googleusercontent.com
# or: ./gradlew assembleRelease -Paireply.googleWebClientId=…
# or: GOOGLE_WEB_CLIENT_ID=… ./gradlew assembleRelease
```

Google Cloud also needs an *Android* OAuth client for `kz.yerek.aireply` with
the SHA-1 of every signing key (debug, upload, Play App Signing). Without a web
client id, release builds hide the Google button.

Keyboard vibration: Settings ▸ Keyboard ▸ *Vibration on key press* (on by
default). The system's own keypress-vibration setting still applies on top.

### Push notifications (Firebase)

Pushes arrive through Firebase Cloud Messaging (iOS uses FCM too, through the
Firebase Messaging SDK, so the server has one provider).

1. The **Android app** `kz.yerek.aireply` is registered in Firebase project
   `ai-reply-4bf8f`, shared with iOS and the backend service account.
2. The genuine public client config `app/google-services.json` is version
   controlled at the owner's request. Server credentials must stay outside Git.
   Validate both clients with `python3 ../tools/validate_firebase_config.py`;
   see `../docs/FIREBASE_SETUP.md` for the handoff and remaining setup.
3. Build as usual. The Google Services plugin is applied only when the file is
   there; without it the app still builds and runs, and push is unavailable
   (Settings ▸ Notifications says so).

The server side (service account, `PUSH_NOTIFICATIONS_ENABLED`) lives in the
backend's configuration; the app only uses what `GET /api/v1/config`
announces (`features.installations`, `push_notifications`,
`preferred_language`) and calls nothing a server did not announce.

What the app does:

* **Nothing before the terms are accepted.** Firebase auto-init is off in the
  manifest; the token is fetched, and the installation registered, only after
  the legal consent.
* **Installation.** A random UUID in `noBackupFilesDir` (no hardware id, never
  backed up, so a reinstall or a restore is a new installation) is registered
  with `POST /api/v1/installations`: with the access token when signed in (the
  server attaches it to that account), without one when signed out. It is
  sent on start and return to the app, after sign-in, on a new FCM token, on a
  language change, and when the permission or the in-app switch changes — but
  only when the body or the account differs from the last accepted one, else
  once a day. Failures back off (30 s doubling, at most an hour); a 4xx other
  than 401/408/429 is not retried until the body changes.
* **Sign-out.** The logout request — and no other request — carries
  `X-Installation-ID`, so the server detaches the installation at once; the
  app then registers it again anonymously.
* **Permission.** Never asked at first launch. After sign-in, on Android 13+,
  Home shows a card (*Turn on* → the system dialog, *Not now* → gone for
  good). Settings ▸ Notifications shows the phone's state, the way to system
  settings when blocked, the in-app switch and, signed in, the categories
  (security always on).
* **Display and taps.** Channels `general` and `important` (account,
  subscription, security), named in the app language. In the background the
  system shows the push; in the foreground the app shows it the same way (tag
  = notification id). A tap reports `POST /api/v1/notifications/opened` (best
  effort, when the push has a delivery id), then opens the `aireply://<screen>`
  it names — after the consent, sign-in and onboarding gates, never around
  them — or an `https://ai-reply.kz` page in the browser; anything else only
  opens the app.
* **Language.** The account's `preferred_language` (the language of its
  notifications and e-mails) is sent when the user picks a language in
  Settings (System sends the language the app shows), or once when the
  account has none yet. A language set on another device is left alone.

Debug builds, Settings ▸ Developer: *Simulate a push notification* runs the
foreground path with a sample payload (it opens Plan), and *Always show the
notification card* shows the Home card and the Settings controls without
Firebase or a server that delivers.

---

## Setting the keyboard up

1. Open AI Reply and go through onboarding (or Settings ▸ Keyboard setup).
2. Tap **Open keyboard settings** — this lands on the system's on-screen
   keyboard list. Turn **AI Reply** on. Android will warn you that a keyboard
   can see what you type; that warning is shown for every third-party keyboard.
3. Tap **Switch keyboard** and pick AI Reply, or use the keyboard button in the
   navigation bar.
4. The checklist on the setup screen turns green as each step completes. It is
   read from the system, not guessed.

### AI configuration

The app ships with no credentials and contains none.

* **Settings ▸ OpenAI ▸ API key** — paste a key beginning with `sk-`. It is
  encrypted with an AES-256/GCM key held in the Android Keystore and stored in
  an app-private file that is excluded from cloud backup and device transfer.
  It is never logged, never shown again, and never leaves the device except in
  the `Authorization` header of a request you asked for.
* **Settings ▸ Model** — `gpt-4o-mini` by default.

This is the *direct* mode, and it is a development mode. A key that reaches a
device is a key the device's owner can extract, and every request is billed to
it with no rate limit but the provider's own. `BackendTransport` is the
production shape, where the key never reaches a device at all;
`docs/AI_REPLY_PLATFORM_ARCHITECTURE.md` is the plan for getting there.

### Voice

The microphone is requested the first time you tap it, never on launch. An input
method cannot show a permission dialog itself, so tapping the microphone briefly
opens a transparent activity that asks on its behalf and closes again.

Recognition uses Android's on-device recogniser where the device has one
(API 33+), and the networked one otherwise. Kazakh coverage is patchy on both;
when a language has no model the keyboard says so rather than leaving you
holding a button that can never produce text.

### Languages

The app, the keyboard's product labels, and the key captions are three separate
questions:

* **App language** — Settings ▸ Language, or the system default. Drives the
  screens, the template chips, Insert, Regenerate and every error message.
* **Keyboard layout** — the `ҚАЗ`/`РУС`/`ENG` key cycles the layouts switched on
  in Settings ▸ Keyboard layouts; hold it for a list. Drives the character keys
  and the space/return captions only.
* **Reply language** — follows the incoming message, always. Russian in, Russian
  out. It is never the app's language, so a Kazakh interface can produce a
  Russian reply to a Russian message.

---

## Project structure

```
app/src/main/java/kz/yerek/aireply/
├── AIReplyApplication.kt      the object graph, built once for app and keyboard
├── ServiceLocator.kt          manual DI — no framework on the keyboard's path
├── MainActivity.kt            the only Activity
│
├── core/lang/                 AppLanguage, KeyboardLanguage, LocalizedContext
├── core/text/                 code-point-accurate length limits
│
├── domain/model/              UserProfile, BusinessContext, ReplyTemplate,
│                              WorkingHours, ReplyConfiguration — pure values
│
├── data/settings/             SharedPreferences, readable synchronously
├── data/profile/              the JSON configuration file + its observable view
├── data/secure/               Android Keystore credential storage
│
├── ai/                        AIReplyService, ReplyPromptBuilder, the two
│                              transports, the closed error set
│
├── keyboard/                  ReplyKeyboardService (the IME), theme
│   ├── layout/                layouts, key geometry, shift and auto-capitals —
│   │                          pure Kotlin, unit-tested
│   ├── input/                 clipboard, host field, local text fields, status
│   ├── reply/                 ReplyComposerFlow (the state machine + versions)
│   │                          and the session controller around it
│   └── ui/                    the key surface (one Canvas, one touch handler)
│                              and the reply panel, in Compose
│
├── voice/                     SpeechRecognitionClient + the Android one
├── push/                      installation id and registration, FCM service,
│                              channels, notification links, permission
├── platform/                  logging that never carries message text
└── ui/                        design system, navigation, and the app screens
```

Layer rules, in one line each:

* `domain` knows about nothing. `data` knows `domain`. `ai` knows `domain` and
  `data`. `keyboard` and `ui` know everything below them and nothing about each
  other.
* `ui` never touches a transport, and `keyboard` never imports from `ui/feature`.
* Nothing outside `data/secure` reads the credential; nothing outside
  `keyboard/input/ContextTextProvider` reads the clipboard.

### Decisions worth knowing before changing things

**No Hilt.** Half of this app is an input method, started at the moment the user
has switched keyboards and is staring at a blank strip. A generated component
and a reflective entry point on that path, to construct seven objects, is not a
trade worth making.

**No OkHttp or Retrofit.** Two kinds of POST, no interceptors, no converters.
`HttpURLConnection` does it in forty lines, and every class not loaded is
keyboard startup time not spent.

**SharedPreferences, not DataStore.** The keyboard's first frame must be correct
synchronously, and the three values it needs to do that are a few hundred bytes.
DataStore's choices there are `runBlocking` on the main thread or a visible
one-frame swap of the whole chip row. Preferences load once and are an in-memory
map afterwards; `AIReplyApplication` warms them. Observers still get a `Flow`.

**A JSON file, not Room.** The whole configuration is a few kilobytes, written
when a settings screen closes and read when the keyboard appears. `ProfileStore`
skips parsing when the file's modification time has not changed, so a keyboard
that opens repeatedly inside one messenger session pays a `stat`.

**Hand-rolled Keystore crypto, not `androidx.security`.**
`EncryptedSharedPreferences` does exactly this, but the library has been stuck in
alpha, is now deprecated, and pulls Tink in. `SecureCredentialStore` is ~120
lines with no dependency and no upgrade risk.

**The reply panel never takes height from the keys.** The keyboard window grows
instead. This is the rule that keeps typing comfortable in every state, and the
layout in `KeyboardRoot` is arranged so breaking it would take effort.

**The keys are one Canvas with one touch handler.** `KeyboardGeometry` gives
every key a hit box, and the hit boxes tile the whole key area — no gaps between
keys, so no dead zones (a test checks every point). A letter is typed on
release, which is what makes long press (`е` → `ё`) and sliding to the right key
possible; a second finger commits the first letter at once, so fast typing does
not wait. The surface reads only `KeySurfaceState`, so a generation in flight —
spinner, partial voice transcript, growing draft — touches only the panel.

---

## Privacy

These are properties of the code, not intentions:

* The clipboard is read in exactly one function, which runs only when you tap a
  persona or Paste. No polling, no timer, no read on appearance, no background
  access.
* Clips another app has flagged sensitive — a password manager entry, say — are
  refused outright.
* The AI panel is disabled entirely in password fields.
* The copied message, your instruction and the draft versions exist only in
  memory. When the keyboard closes with a reply unfinished they are kept — still
  only in memory — for ten minutes, so you can copy one more message and come
  back, and then dropped.
* `ReplyLog` records lengths and outcomes, never text, and only in debug builds.
* AI requests carry the message, your profile and the selected template. They
  carry no device identifier, no contacts, no chat history, no location, no
  advertising ID, no device model and no OS version. The per-install identifier
  is a random UUID generated locally and discarded on uninstall.
* The push installation registration (only after the terms are accepted) sends
  a random installation id, the app version and build, the OS version, the
  device model and manufacturer, the interface language, the time zone, the
  notification permission, the in-app switch and the FCM token. The account is
  never in it: the server takes it from the access token. Only the logout
  request names the installation in a header.
* Working hours send a wall-clock time and two booleans. No timezone, no city,
  no coordinates.
* There is no accessibility service in this project, and no code that reads
  another app's screen. The only way a message becomes context is that you
  copied it.

---

## Tests

```bash
./gradlew testDebugUnitTest
```

Thirteen classes, 115 tests, covering: the message limit (published by the
server, 400 when it has not said) and code-point counting, backend error
mapping, prompt construction (including that user text never reaches the
developer message), working-hours derivation and weekday grouping, draft
normalization with URLs preserved, configuration repair and tolerant decoding of
older files, the three keyboard layouts and their geometry (no dead zones, one
height for every page), shift, auto-capitals, the double-space full stop and word
delete, the composer's state machine and its versions (Regenerate never loses an
edit, Insert takes the edited text), and **localization parity** — that every
string exists in every language with matching format specifiers, which is the
failure that is otherwise silent until a Kazakh user sees an English sentence.

---

## Known limitations

**Tried on an emulator, not yet on a phone.** `assembleDebug` and
`testDebugUnitTest` pass on macOS, and the keyboard was used on a Pixel 6
emulator (API 34) with the debug mock transport: Kazakh typing, the `?123` page,
long press, and the whole Reply → edit → Regenerate → Insert flow. Not tried
yet: multi-touch rollover, delete repeat, the space-bar trackpad, TalkBack, the
dark theme and landscape.

**Debug builds can answer without the network.** Settings ▸ Developer ▸ Mock AI
replies (debug builds only) returns canned Kazakh, Russian and English replies;
`#offline`, `#quota` and `#slow` in the instruction simulate failures. Release
builds neither show the switch nor use the mock.

**Other gaps, deliberate:**

* `VoiceConfigurationParser` was not ported. On iOS it proposes working hours and
  rules from dictated speech, applied only on confirmation. It is a heuristic
  phrase parser with its own test suite, orthogonal to the port. Dictated text
  still lands verbatim in the profile, which is the behaviour that matters.
* Template reordering is up/down buttons rather than drag-and-drop. Compose has
  no built-in equivalent of `List` + `EditButton`, and a hand-rolled drag in a
  list this short would be more code than the rest of the screen and worse with
  a screen reader.
* Release signing is not configured, on purpose: a keystore in a repository is a
  shipped secret.
* The backend the production transport talks to does not exist yet. See
  `docs/AI_REPLY_PLATFORM_ARCHITECTURE.md`.

---

## Documentation

* `docs/IOS_ANDROID_PARITY.md` — every feature, its iOS implementation, its
  Android equivalent, and its status.
* `docs/AI_REPLY_PLATFORM_ARCHITECTURE.md` — the design for the production
  backend, admin panel, subscriptions, usage metering, payments and analytics.
  **Design only. None of it is implemented, and none of it should be started
  before Phase 2 of the roadmap in that document.**
