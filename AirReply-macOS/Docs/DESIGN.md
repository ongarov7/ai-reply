# AI Reply for Mac — product and interface spec

Owner's brief (31.10.2026): minimal, modern, very convenient. A Spotlight/Claude-style quick bar. A copied
message shows up at once, as on iOS and Android. One click copies the generated reply; the user pastes with ⌘V
and sends it. A microphone that feels like ChatGPT dictation. The app lives in the menu bar.

## Principles

- One surface: a floating quick bar. No main window, no Dock icon (`LSUIElement`).
- Light-first, natural dark mode, system accent (asset AccentColor = system blue when the user's accent is Multicolor).
- Native macOS look: SF Pro, SF Symbols, materials, subtle borders, the window's own shadow. No gradients except the
  brand icon, no heavy cards, no animated backgrounds. Respect Reduce Motion and Increase Contrast.
- Nothing is sent to the server without an explicit action (Enter, a recipient click, the mic ✓).
- Keyboard first: everything works without the mouse.

## Quick bar (NSPanel)

- Non-activating `NSPanel`: borderless, `isFloatingPanel`, level `.floating`, `collectionBehavior`
  `[.canJoinAllSpaces, .fullScreenAuxiliary, .ignoresCycle]`, `hidesOnDeactivate = false`, can become key (typing works)
  while the previous app stays active, so ⌘V after closing goes straight back to it.
- Width 680 pt (min 560), height follows content. Corner radius 16. Background: `NSVisualEffectView` material
  `.popover`, blending `.behindWindow`, state `.active`; 1 px border `Color.primary.opacity(0.08)`; window shadow on.
- Position: screen under the pointer. `top` = horizontally centred, top edge at 22 % of the visible frame height;
  `bottom` = 24 pt above the visible frame's bottom (above the Dock); `remembered` = last dragged position. The panel
  grows away from its anchor: downward for `top`/upper half, upward for `bottom`/lower half. Draggable by its
  background.
- Closes on Esc, on the hotkey again, and when it resigns key because the user clicked another app. The last result
  and draft are kept for 10 minutes and shown again on reopen.

### Bar row (56 pt)

`[brand icon 24 pt] [text field, grows to 5 lines] [mic] [send]`, horizontal padding 14, spacing 10. There is no
recipient menu here: `/api/v1/ai/compose` takes only the instruction, and people say who it is for ("напиши другу…").

- Brand icon: `NSApp.applicationIconImage`, 22 pt, corner radius 5.
- Text field: 17 pt regular, placeholder secondary. Compose placeholder: "Что написать? Например: вежливо ответь клиенту,
  что документы получили". Reply-mode placeholder: "Как ответить? Можно оставить пустым".
- Mic: 28 pt circular plain button, SF Symbol `mic`, hover fill `primary.opacity(0.08)`. Hidden when the server turns
  voice off (`features.voice_transcription == false`).
- Send: 32×32, radius 8, accent fill, white `arrow.up` 14 pt semibold. Disabled (accent 35 %) when there is nothing to
  send. While generating it shows a small spinner.

### Copied message (reply mode)

When the panel opens and another app copied text within the last 10 minutes (see `ClipboardMonitor`), a context row
appears above the text field inside the bar:

- Capsule `primary.opacity(0.06)`: `arrowshape.turn.up.left` 11 pt + «first line of the copied text…» (one line,
  truncated) + `xmark.circle.fill` to dismiss (back to compose mode).
- Below it, one-click recipient chips: [Другу] [Клиенту] [Коллеге] [Партнёру] (12.5 pt, capsule). A click generates the
  reply at once. ⌘1…⌘4 do the same. Enter uses the last recipient.
- Over the server source limit: the capsule shows "Скопировано N символов из L — сократите сообщение" in the warning
  colour and the chips are disabled. Never truncate silently.
- Concealed/transient pasteboard items (password managers) and our own copied replies never appear here.
- If macOS denies pasteboard access: no capsule; the field accepts ⌘V as usual.

### Microphone (ChatGPT-like)

1. Click mic (or ⌘D): permission prompt the first time; denied → inline message with "Открыть настройки"
   (Privacy & Security → Microphone).
2. Recording: the text field is replaced by a live waveform (≈40 rounded bars, 2 pt wide, 3 pt gap, accent colour,
   heights from `VoiceController.levels`), a small pulsing red dot and the timer "0:07 / 0:40". Right side: ✕ (cancel,
   Esc) and ✓ (stop and recognise, Enter). Recording stops by itself at the server limit.
3. Transcribing: "Распознаю…" with a subtle shimmer. Then the text is inserted into the field (appended if the user
   already typed), cursor at the end, field focused. The user presses Enter to generate (no automatic generation).
4. Errors stay inline and short; the microphone is closed in every exit path.

### Result card

Appears under (or above, when anchored at the bottom) the bar with an 8 pt gap, same material and radius, padding 16.

- The reply text: 15 pt, line spacing 3, editable in place (plain style), scrolls after 260 pt.
- Footer, left: balance caption from `/api/v1/me` — "Осталось сегодня: 27", "Безлимит · Pro", "Бесплатно: 7 из 10",
  "Пакет: 30". Right: secondary "Ещё вариант" (`arrow.clockwise`, ⌘R) and the primary accent button
  "Копировать" (`doc.on.doc`, ⌘↩).
- Copy: puts the (possibly edited) text on the pasteboard as plain text, button turns into "Скопировано ✓" for
  0.8 s, then the panel closes if `closeAfterCopy` (default on) so ⌘V goes straight into the messenger. Optional
  `pasteAfterCopy` (off by default) also posts ⌘V into the previous app via `PasteService`.
- Over-limit, limit reached, consent missing, signed out, offline and server errors replace the card content with one
  line + one action: "Повторить", "Тарифы" (opens `<base>/payment` in the browser), "Войти", "Принимаю",
  "Открыть настройки".

### Sign-in and consent (inside the bar)

- Signed out: the field becomes "Email для входа" → Enter sends the code → "Код из письма" (digits) with "Отправить
  снова" (after the server cooldown) and "Другой email". Success returns to the normal bar.
- Missing AI-processing consent: a card with a two-sentence explanation, links to the privacy policy and terms, and
  "Принимаю". Nothing is generated before acceptance.

## Menu bar item

Template SF Symbol `ellipsis.bubble`. Menu:

- Открыть AI Reply — shows the shortcut (e.g. ⌥Space)
- Диктовать
- —
- account email (disabled) or "Войти…"
- plan and balance line (disabled), e.g. "Base · осталось сегодня 27"
- Тарифы и оплата… (browser, `<base>/payment`)
- —
- Настройки… ⌘,
- Проверить обновления…
- —
- Выйти из AI Reply ⌘Q

## Settings (SwiftUI `Settings` scene, grouped `Form`, tabs)

- Общие: shortcut recorder, open at login (`LoginItem`), panel position, show copied message, close after copy,
  paste automatically (explains the permission), appearance.
- Голос: dictation language (Авто / Русский / Қазақша / English), microphone access status with a button to
  System Settings.
- Аккаунт: email, plan, balance, "Тарифы и оплата", "Выйти", "Удалить аккаунт…" (website).
- О программе: version and build, privacy policy, terms, support, "Скопировать сведения для поддержки" (versions and
  last error codes only — never text).

## Keyboard

Enter — generate / stop recording; ⌥Enter — new line (the macOS field convention); ⌘↩ — copy; ⌘R — another
variant; ⌘D — dictate; ⌘1…⌘4 — recipient; Esc — cancel recording, then a running request, then close.

Default global shortcut ⌥Space. If another app owns it, the first free of ⌃⌥Space and ⌃⌥⌘Space is used; ⌃⌥Space is
skipped where macOS uses it to switch input sources (most ru/kk setups). Settings explain which one is active.

## Strings

All user-facing strings are semantic keys resolved from the app's `AirReply/Localizable.xcstrings` (source `ru`,
plus `kk` and `en`) through `Bundle.main`: `Text("bar.placeholder.compose")`, `String(localized: "…")`. Reuse the iOS
vocabulary for recipients, balance and errors.

## Privacy

- No text, audio or clipboard content in logs, analytics or crash reports. Logs carry request ids and error codes.
- Tokens only in the login keychain (`KeychainStore`).
- Audio files are temporary and deleted right after upload or cancel.
