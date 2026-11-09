# Apple Watch integration

Voice instructions on Apple Watch for the AI Reply keyboard: the user speaks on the
wrist, and the reply appears in the keyboard on the iPhone, inside the messaging app.

Status of each part is in [Verification](#verification). Nothing below claims more
than that section does.

## Architecture

```
Apple Watch (AIReplyWatchApp)
   │  WatchConnectivity
   ▼
iPhone app (WatchCompanion)  ──  recording  ──▶  POST /api/v1/ai/transcribe  ──▶  OpenAI
   │  App Group files + Darwin notification
   ▼
Keyboard (WatchVoiceCoordinator)  ──  /api/v1/ai/reply, /api/v1/ai/compose  ──▶  OpenAI
   │  textDocumentProxy.insertText
   ▼
Text field of the messaging app. The user reviews it and sends it.
```

| Decision | Why |
|---|---|
| WatchConnectivity ends in the containing app | The keyboard extension is not where `WCSession` works; a message from an active watch app wakes the iPhone app. |
| One file per command, no shared queue file | iOS kills a process suspended while holding a lock in a shared container (0xdead10cc). Exclusive `open` and atomic `link` need no lock. |
| Generation runs in the keyboard | It is the existing pipeline, the existing quota, and the only process that may read the clipboard and insert text. |
| The watch has no account | Everything goes through the iPhone's session. The server never sees a watch, so it uses no device slot. |
| Speech goes to the server for every language | The owner's decision on 2026-11-09: the same OpenAI microphone as Android, for Russian, English and the chosen language. Apple's dictation is the fallback for typing and has no Kazakh. |

## Targets

| Target | Bundle ID | Notes |
|---|---|---|
| `AIReply` | `kz.ai-reply.reply.keyboard.keyboard` | Unchanged. Embeds the watch app in `Watch/`. |
| `ReplyKeyboard` | `….extension` | Unchanged. |
| `AIReplyWatchApp` | `….watchkitapp` | New. Single-target SwiftUI app, watchOS 10.0+, `WKRunsIndependentlyOfCompanionApp = false`. |

The watch target needs no entitlements: no App Group, no Keychain. Version and build must
match the iPhone app (`1.2.0` / `24` today) or App Store Connect refuses the upload.

`project.pbxproj` is the source of truth, because `xcodegen` is not installed on the
development Mac. `project.yml` describes the same target but has not been run.

## Files

| Area | Files |
|---|---|
| Wire format, all targets | `Shared/WatchVoice/WatchVoiceProtocol.swift` |
| App Group store, signals | `Shared/WatchVoice/WatchVoiceStore.swift` |
| Session, link state, insert rule | `Shared/WatchVoice/WatchVoiceSession.swift` |
| Routing to the existing pipelines | `Shared/WatchVoice/WatchVoiceRouting.swift` |
| Keyboard logic | `Shared/WatchVoice/WatchVoiceCoordinator.swift`, `WatchVoiceStrings.swift` |
| Keyboard UI | `ReplyKeyboard/WatchVoiceStripView.swift`, `TemplateBarView`, `KeyboardActionBar`, `KeyboardViewController` |
| iPhone | `AIReply/Features/Watch/` |
| Watch | `AIReplyWatch/` |

## Protocol

Everything is a `WatchVoiceEnvelope` (`version`, `requestID`, `type`, `createdAt`, `source`,
`keyboardSessionID`, `language`, `instruction`, `deliveryStatus`, …). WatchConnectivity sees a
one-key dictionary holding its JSON. Identifiers must match `^[A-Za-z0-9-]{8,64}$`: they become
file names and the server's `Idempotency-Key`.

| Type | Direction | Channel |
|---|---|---|
| `command` | watch → phone | `sendMessage`, acknowledged; after 2 failed attempts `transferUserInfo` |
| `cancel` | watch → phone | as above |
| `statusRequest`, `resultRequest` | watch → phone | `sendMessage` |
| `statusUpdate` | phone → watch | `sendMessage`, or `transferUserInfo` for final states |
| `transcribe` | watch → phone | `sendMessageData` up to 58 KB, otherwise `transferFile` |
| `transcript` | phone → watch | as `statusUpdate` |
| `ping` / `pong` | phone → watch | `sendMessage` (Settings ▸ Test connection) |

The phone's state goes in every answer and in `updateApplicationContext`.

`transferUserInfo` is not real time, and the watch screen says so. A queued command that
arrives after its 5 minutes is refused as `expired`.

Delivery states: `queued`, `received`, `generating`, `ready`, `inserted`, `dismissed`, `failed`,
`expired`, `cancelled`.

### Duplicates

A repeated delivery is stopped three times over:

1. `store.enqueue` refuses a known request ID (an atomic `link`, then the claim marker).
2. `store.claim` is an exclusive file creation: one keyboard wins.
3. The request ID is the `Idempotency-Key`, so the server charges once even if a keyboard died
   and another re-ran the command.

An `.inserted` marker makes text reach a field at most once.

## App Group

Folder `WatchVoice/` in `group.kz.ai-reply.shared`, excluded from backups.

| Path | Writer | Contents |
|---|---|---|
| `inbox/<id>.json` | app | The instruction. `completeFileProtectionUnlessOpen`: can be written while locked, read only after unlock. |
| `outcomes/<id>.json` | keyboard | Status; the generated text only while `ready`. |
| `markers/<id>.claimed`, `.inserted`, `.cancelled` | both | Empty files. |
| `keyboard-session.json` | keyboard | IDs and flags, never field content. Written only with a paired watch. |
| `watch-link.json` | app | What `WCSession` reports. |

Lifetimes: instruction 5 minutes, result 10 minutes, markers 24 hours. The copied message is
never written anywhere: it stays in the keyboard's memory.

Signals are Darwin notifications with no payload: `kz.ai-reply.watch.command`, `.link`,
`.outcome`, `.session`. A process hears them only while running, so each side also reads the
store when it comes to life.

## Keyboard

Nothing runs on a keystroke. While on screen the keyboard listens for the signal. The only
timer is a re-read every 2 seconds for at most 3 minutes after the user taps Watch Voice, as a
safety net for a lost notification.

A session is one appearance on one field: another field means another session.

A command starts by itself only when it names the live session, the composer has no user work
in it, and Full Access is on. Otherwise it waits in the strip for a tap on Generate, which
costs nothing until then.

Auto insert (`WatchInsertPolicy`) needs all of: the Auto Insert switch on, a command bound to
this session, the keyboard on screen, the same session, a known and unchanged document
identifier, unchanged text around the caret, and an empty field. Anything else is a manual
Insert Response. `documentIdentifier` is not trusted to mean the same chat, which is why the
text is compared too.

If the keyboard goes away during generation, the request continues and the text is saved for
Insert Response. If the process dies, the command is taken over after 2 minutes and re-run with
the same key, so it is not charged twice.

The control is hidden without a paired watch, and `PersonaRowLayout` is then unchanged.

## Generation

`WatchVoiceRouter` picks one of the two existing pipelines. No new model or endpoint.

| Mode | Path | Input |
|---|---|---|
| Reply | `/ai/reply` | Copied message, instruction, persona |
| Reply, nothing copied | `/ai/compose` | Instruction |
| Reply, paste refused | no request | Reply composer with the instruction and an empty message card |
| New message | `/ai/compose` | Instruction |
| Rewrite, shorten, expand | `/ai/compose` | Selection or field text, inside the instruction |

Length: `reply_length` has only `short` and `medium`, so `long` is `medium` plus a sentence in
the instruction. Response language is `profile.reply_language` for a reply and a sentence in
the instruction for a new message.

An instruction over the limit is refused, never cut (280 for a reply, the published limit for a
new message).

Quota, plans and packs are the server's existing rules, because the requests are the existing
requests from the existing session.

## Voice and languages

Speech goes: `AVAudioRecorder` on the watch (AAC, 16 kHz mono, 24 kbit/s, 40 s at most) →
iPhone → `POST /api/v1/ai/transcribe`, `purpose=instruction`, `languages=<chosen>,…` → text,
editable on the watch before sending.

- Hints: the chosen language first, then Russian, English, Kazakh, at most three.
- `features.voice_transcription` off, or no iPhone: the microphone is hidden and Type or
  dictate remains.
- The endpoint spends no replies. It has its own daily limit (`VOICE_LIMIT_REACHED`).
- Apple's watchOS dictation lists English and Russian and does not list Kazakh
  ([feature availability](https://www.apple.com/watchos/feature-availability/)).

## Privacy

- A first-use screen on the watch discloses that the recording goes to OpenAI and asks for
  permission before anything is recorded. It can be withdrawn on the watch.
- The microphone is on only between Speak and Stop. The audio file is deleted when the text
  arrives or the user cancels.
- Nothing is logged but lengths and states. No API key on the watch or the phone.
- See [APPLE_WATCH_APP_REVIEW.md](APPLE_WATCH_APP_REVIEW.md).

## Apple platform limits

- The iPhone cannot open the watch app or its microphone. The user opens it.
- Without an active watch app the phone cannot wake it. `isReachable` on the phone means the
  watch app is in the foreground.
- A suspended iPhone app cannot push progress, so the watch asks, every 3 seconds for 45
  seconds at most, in the foreground only.
- Delivery times are not guaranteed by Apple.
- A keyboard cannot read another app's chat, or know which conversation is open.
- iOS may show "Allow Paste" when the keyboard reads the copied message. Settings ▸ AI Reply ▸
  Paste from Other Apps ▸ Allow removes it.
- A force-quit iPhone app may not be woken. This has to be checked on a device.

## Build

```bash
xcodebuild build -project AI-Reply/AIReply.xcodeproj -scheme AIReply -destination 'generic/platform=iOS Simulator'
```

```bash
xcodebuild build -project AI-Reply/AIReply.xcodeproj -scheme AIReplyWatchApp -destination 'generic/platform=watchOS Simulator'
```

The iPhone scheme builds and embeds the watch app. In Xcode, automatic signing will register
`….watchkitapp` the first time.

## Testing

```bash
xcodebuild test -project AI-Reply/AIReply.xcodeproj -scheme AIReply -destination 'platform=iOS Simulator,name=iPhone 17'
```

Debug launch arguments: `-AIReplyMockReplies` on the iPhone app gives canned recognition and
replies, and `-AIReplyWatchSmokeTest` on the watch app runs record, send and confirm with no
taps. Both are compiled out of release builds.

## Verification

| Scenario from the brief | Status |
|---|---|
| 1 Active keyboard, text inserted | Simulator pair, canned AI |
| 2 Instruction with the keyboard closed | Unit test |
| 3 Another chat or field | Unit test |
| 4 Watch disconnected | Delivery rule unit-tested; needs a device |
| 5 Repeated delivery | Unit test, including concurrent claims |
| 6 Cancel | Unit test |
| 7 Nothing copied | Unit test |
| 8 Generation fails | Unit test |
| 9 Limit reached | Unit test of the mapping; needs a real account |
| 10 Russian and English | **Needs real speech and the real server** |
| 11 Kazakh | **Needs real speech and the real server** |
| 12 iPhone app in the background | Needs a device |
| 13 Install and onboarding | Screens built; needs a device |
| 14 No watch paired | Unit test |

Physical tests still required: real microphone capture, Bluetooth loss, locked iPhone,
suspended and force-quit iPhone app, WhatsApp and Telegram as hosts, battery, and all watch
screen sizes.
