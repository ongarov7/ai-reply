# iOS voice UI polish — 18 November 2026

Review of the TestFlight voice build (1.3.0, builds 31–32) against the real
code and the simulator, and the fixes that followed. Screenshots (Git-ignored):
`outputs/voice-ui-polish-2026-11-18/`.

## Control Center icon (blank circle)

Root cause, reproduced in the iOS 26.5 simulator's control gallery and logged by
SpringBoard: `No image named 'AIReplyControl' found in asset catalog for
…/AIReplyVoiceWidgets.appex`. SpringBoard renders controls outside the app and
does not resolve a single-variant (Regular-M, template v.2) custom symbol, even
though actool accepts it and the app renders it. Even `mic.fill` looked blank in
that test because Control Center caches a control template per install; a clean
install is needed between attempts.

Fix: `AIReplyVoiceControl`, a variable template v.3 (Ultralight-S, Regular-S,
Black-S) of AI Reply's bubbles with a microphone. The gallery, Control Center
and the Lock Screen Live Activity draw it. The old `AIReplyControl` asset and
build 32's system-symbol fallback are gone. Needs one check on a physical
iPhone after install (remove and re-add the control if an old one is cached).

## App voice sheet

Rebuilt from the app's design system (grouped background, cards, 50 pt button
pairs). Fixed: an empty template picker and unlabeled pickers (menu pickers
outside a Form show no label; the template now shows the first visible one
when none matches), "Готово" used both for closing and for finishing (the sheet
now says «Закрыть»), "12 / 40 s" (now 0:12 / 0:40), a wall of setup text (now
three collapsible steps). The template row appears only with a message, because
standalone text has no template. Pause now keeps the words: Continue records
more after them; Create reply generates from them. Cancel discards the turn.

## Keyboard

- Live caption showed "Ответь / …третиться" (UIKit head truncation cuts the
  middle of a two-line label); it now fits the newest words after "…".
- The watch control steps aside during a voice session, so the caption has
  room; with two trailing controls they no longer touch.
- The microphone menu closed itself on listening → speech detected and rebuilt
  its buttons twice a second (a tap could be lost); both fixed.
- Insert (now `text.insert`, not the up arrow that reads as Send) inserts with
  one tap only for the field that sent Done or the copied message, into an empty
  field with no open draft. Otherwise the button says «Проверить» and opens the
  review composer, whose Insert asks to replace or append.
- Inserted and closed results are remembered by request ID across keyboard
  instances (15 minutes, no text stored), so a result cannot come back for a
  second insertion here or in another chat.
- "Reply to copied message" confirms what will be answered («Ответ на «…»») and
  refuses a response AI Reply itself inserted (compared by digest only).
- Done from the keyboard binds the result to that field, unless the copied
  message was attached from another field.

## 40-second instructions

A 40-second turn produces more than 400 characters (≈133 words at 200 wpm,
up to 7.5 characters per Kazakh or Russian word). The app refused it
(«Запрос слишком длинный») and replies cut it at 280 characters on the client
and 400 on the server. Now a dictated instruction is sent with
`instruction_source: "voice"` (only to a server that publishes
`features.voice_instruction`) and may use `max_voice_instruction_length`
= 1 000 characters (never below the typed limit). Typed instructions keep the
admin limit. With an older server the app cuts the dictation after the last
whole word instead of failing. The backend must be deployed for the 1 000-
character room; the app works with the old backend as described.

## Verified / not verified

Verified: 981 iOS unit tests (1 skipped, as before), the full Go suite, the
keyboard modal guard, simulator screenshots of the sheet (light/dark,
ru/kk/en), the keyboard strip states, the Control Center gallery and Lock
Screen Live Activity. Not verified: a physical iPhone (none connected), real
microphone capture, the expanded Dynamic Island (the test simulator has a
notch), and the production backend before deployment.
