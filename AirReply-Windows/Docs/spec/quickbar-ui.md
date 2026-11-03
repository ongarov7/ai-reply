# AI Reply for Windows — quick bar UI and app shell

Normative spec for the Windows clone (C#/.NET 10, Avalonia UI, Win32 via P/Invoke). Derived read-only from macOS
`main` @ `0200bc8` (2026-11-03). Covers: process/tray/hotkey shell, the floating quick bar (geometry, every state,
transitions, keyboard, focus, limits, copy/paste flow), the AI Context section as it appears in the bar, the Settings
window, and the string keys of every visible text. Networking, voice capture, clipboard internals, retrieval and
document parsing are specified elsewhere; they appear here only where the UI depends on them.

## 0. Conventions

### 0.1 Source aliases

macOS package paths are relative to `AirReply-macOS/Packages/AirReplyKit/`.

| Alias | File |
|---|---|
| QBM | `Sources/AirReplyUI/QuickBarModel.swift` |
| QBV | `Sources/AirReplyUI/QuickBarView.swift` |
| QP | `Sources/AirReplyUI/QuickPanel.swift` |
| ACV | `Sources/AirReplyUI/AdvancedContextView.swift` |
| AC | `Sources/AirReplyUI/AppCoordinator.swift` |
| SIC | `Sources/AirReplyUI/StatusItemController.swift` |
| SW | `Sources/AirReplyUI/SettingsWindow.swift` |
| AS | `Sources/AirReplyUI/AppSettings.swift` |
| SUP | `Sources/AirReplyUI/Support.swift` |
| MOD / GEN / ACC / AUTH / CTX / TM / CFG / API / DCI | `Sources/AirReplyCore/{Models,Generator,AccountStore,AuthSession,ContextProfiles,TextMetrics,AppConfiguration,APIClient,DocumentContextImporter}.swift` |
| CLIP / PST / KC / HK | `Sources/AirReplySystem/{ClipboardMonitor,PasteService,KeyCombo,HotKeyCenter}.swift` |
| VC | `Sources/AirReplyVoice/VoiceController.swift` |
| QBT / SNT / LOC | `Tests/AirReplyUITests/{QuickBarModelTests,SnapshotTests,LocalizationTests}.swift` |
| APP | `AirReply-macOS/AirReply/AirReplyApp.swift` |
| XCS | `AirReply-macOS/AirReply/Localizable.xcstrings` (string catalog, source language `ru`) |
| DES / CTXD | `AirReply-macOS/Docs/DESIGN.md` / `AirReply-macOS/Docs/CONTEXT.md` |
| AUD / AUDT | `docs/audits/2026-11-03-backend-macos-audit.md` / `docs/audits/evidence/2026-11-03-macos-audit-confirm-tests.swift.txt` |
| WARCH | `AirReply-Windows/Docs/ARCHITECTURE.md` |
| BE-srv / BE-ai / BE-svc / BE-mac / BE-cfg / BE-inst / BE-web | `ai-reply-back-end/internal/`: `transport/api/server.go`, `transport/api/ai_handlers.go`, `ai/service.go`, `ai/mac_context.go`, `transport/api/me_handlers.go`, `transport/api/installation_handlers.go`, `transport/web/server.go` |

### 0.2 Rules of this document

1. Units: macOS pt = Windows DIP (1/96 in). Convert to physical px with the DPI of the monitor the window is on
   (per-monitor DPI aware v2).
2. Modifier mapping: in-app ⌘ → **Ctrl**, ⌥ → **Alt**, Return → **Enter**; global hotkey ⌥ → Alt, ⌃ → Ctrl.
3. Precedence: macOS **code** wins over DESIGN.md where they disagree (§13.1).
4. Tags: **(fix vs macOS)** = audit defect MAC-1…MAC-6 / PRO-1 corrected on Windows; **(quirk fix)** = further macOS
   defect found while writing this spec, corrected on Windows (§13.2, owner may veto); **(Win)** = platform adaptation.
5. Strings: every visible text uses the XCS key named here; values ru/kk/en are copied verbatim from XCS except the
   Windows overrides and new keys in §11. `%lld` = integer, `%@` = string; `%1$… %2$…` keep argument order.
6. Framework names (Avalonia `SystemDecorations`, `BeginMoveDrag`, `TrayIcon`, `SelectableTextBlock`, …) are hints;
   the behaviour is normative, not the API.

## 1. App shell

### 1.1 Process model

1. Tray-only utility: no main window; the bar has no taskbar button (mac: `LSUIElement`, activation policy
   `.accessory`, APP:5-17).
2. Single instance per user session (named mutex). A second launch passes "show" to the running instance (calls
   `AllowSetForegroundWindow(ASFW_ANY)` first, then signals e.g. a message-only window) and exits; the running
   instance calls `ShowPanel()` (mac `applicationShouldHandleReopen` → `showPanel()`, APP:24-28).
3. Launch modes (Win): `--background` (autostart, §10.2) starts silently; an interactive launch (no argument) starts
   and then calls `ShowPanel()` once. macOS shows nothing on first launch (APP:20-22); Windows shows the bar because
   new tray icons land in the hidden overflow.
4. Startup order (AC:53-82): crash reporting → apply theme (§10.2) → wire bar → wire account → tray icon → register
   hotkey (§1.2) → start clipboard listener (baseline = current clipboard sequence number, CLIP:76-90) → power/session
   observers (§1.4) → async `RefreshConfig()` then, if signed in, `RefreshMe()` → if signed in,
   `EnsureInstallationRegistered()` **(fix vs macOS, PRO-1, §1.7)**.
5. Exit (tray item 7, Windows session end): unregister hotkey, stop clipboard listener, `voice.Cancel()` (audio
   dropped), remove observers (AC:84-92), dispose tray icon.

### 1.2 Global hotkey

1. Register with `RegisterHotKey(MOD_NOREPEAT | mods, vk)` on the shell's message window. `ERROR_HOTKEY_ALREADY_REGISTERED`
   (1409) = taken (mac: exclusive Carbon hotkey, HK:34-52).
2. Press → `TogglePanel()`: visible ? `Hide(restoreFocus: true)` : `ShowPanel()` (AC:67, 104-106).
3. Startup (AC:223-236): saved gesture == default Alt+Space → try `[Alt+Space, Ctrl+Alt+Space]` in order (WARCH:41-42;
   mac tries ⌥Space, ⌃⌥Space, ⌃⌥⌘Space and skips combos the OS uses, AC:217-226). First success ≠ saved → save it and
   set note `settings.shortcut.fallback %@ %@` (saved, used). None succeeds → note `settings.shortcut.taken %@`
   (saved). A non-default saved gesture is tried alone; failure → `settings.shortcut.taken %@`.
4. Change in Settings (AC:200-215): `Register(new)` ok → save, clear note; fails → note `settings.shortcut.taken %@`
   (new) and re-register the previous gesture. mac's "used by the system" check (`settings.shortcut.system %@`,
   KC:40-49) has no Windows source → not implemented (§11.4).
5. Allowed gesture (mac KC:31-38, 98-133, mapped): at least one of Ctrl/Alt; Shift optional; **Win never** (reserved
   by Windows); key is not a modifier; not in the reserved set: Alt+Tab, Alt+Shift+Tab, Alt+Esc, Alt+Shift+Esc,
   Ctrl+Esc, Ctrl+Alt+Tab, Alt+F4, Ctrl+F4, Ctrl+W, Ctrl+Shift+Esc, Ctrl+Alt+Delete, F12 with any modifiers
   (debugger-reserved), PrintScreen with any modifiers, Ctrl+C, Ctrl+V, Ctrl+X, Ctrl+A, Ctrl+Z, Ctrl+Y, Ctrl+Shift+Z,
   Ctrl+Insert, Shift+Insert, Shift+Delete.
6. Display string (KC:22-29, 66-84): modifiers in mac order ⌃⌥⇧ → `Ctrl+Alt+Shift+` then the key name: `Space`,
   `Enter`, `Tab`, `Esc`, `Backspace`, `Delete`, `Insert`, `Home`, `End`, `PgUp`, `PgDn`, `←` `→` `↑` `↓`, `F1`–`F24`;
   letters/digits/OEM keys from the **US** layout (never the Cyrillic character; mac uses the ASCII-capable layout).

### 1.3 Tray icon and menu

1. Icon: brand glyph (§2.6.4) as a monochrome template: white on a dark taskbar, black on a light one
   (`HKCU\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize\SystemUsesLightTheme`; refresh on
   `WM_SETTINGCHANGE` "ImmersiveColorSet"). Sizes 16/20/24/32 px. Tooltip `AI Reply` (verbatim, SIC:34-37).
2. Left click (Win; the mac item only opens its menu): bar visible → `Hide(restoreFocus: false)`; bar hidden by
   deactivation ≤ 300 ms ago (the same click) → nothing; otherwise `ShowPanel()`.
3. Right click / context-menu key: the menu below, built from current state each time it opens (SIC:43-74). Opening
   it calls `RefreshAccount(force: false)` when signed in (AC:271-277); the open menu shows cached values.

| # | Visible when | Text (key) | Enabled | Action | Source |
|---|---|---|---|---|---|
| 1 | always | `menu.open` + TAB + hotkey display string (right-aligned accelerator column; display only) | yes | `ShowPanel()` | SIC:47-50, AC:60 |
| 2 | `VoiceAvailable && signedIn` | `menu.dictate` | yes | `StartDictation()`: `ShowPanel()`; if voice Idle → `ToggleVoice()` | SIC:51-53, AC:108-111 |
| — | separator | | | | SIC:54 |
| 3a | signed in | account email, else `menu.account` | no | — | SIC:56-57, AC:166-169 |
| 3b | signed in && balance known | `planName + " · "` (omitted when planName empty) + `menu.balance.unlimited` if unlimited else `menu.balance.remaining %lld` (remaining_total) | no | — | SIC:58-60, 78-82 |
| 3c | signed out | `menu.sign_in` | yes | `ShowPanel()` | SIC:61-62, AC:62 |
| 4 | always | `menu.plans` | yes | `OpenPlans()` (§1.5) | SIC:64, AC:63 |
| — | separator | | | | SIC:65 |
| 5 | always | `menu.settings` (mac ⌘, — no accelerator on Windows) | yes | `OpenSettings()` | SIC:66-68, AC:64 |
| 6 | always | `menu.updates` | yes | open `<base>/windows` (open question 5) | SIC:69, AC:65 |
| — | separator | | | | SIC:70 |
| 7 | always | `menu.quit` (mac ⌘Q — none on Windows) | yes | exit (§1.1.5) | SIC:71-73, AC:66 |

```
+-------------------------------------+
| Open AI Reply           Alt+Space   |   item 1
| Dictate                             |   item 2
|-------------------------------------|
| user@example.kz          (disabled) |   item 3a
| Base · 27 left           (disabled) |   item 3b   (signed out: "Sign In…", item 3c)
| Plans & Payment…                    |   item 4
|-------------------------------------|
| Settings…                           |   item 5
| Check for Updates…                  |   item 6
|-------------------------------------|
| Exit AI Reply                       |   item 7
+-------------------------------------+
```

### 1.4 System events

| mac event | Windows source | Action | Source |
|---|---|---|---|
| `didWakeNotification` | `WM_POWERBROADCAST` `PBT_APMRESUMEAUTOMATIC` | `RefreshAccount(force: false)` (`GET /api/v1/me`, skipped if < 10 s old) | AC:70-73, 171-174 |
| `didBecomeActive` (app activated, i.e. Settings shown) | Settings window `Activated` | `RefreshAccount(force: true)` | AC:74-77 |
| will sleep / screen locked | `PBT_APMSUSPEND` / `WTS_SESSION_LOCK` | voice RequestingPermission or Recording → `Cancel()`; a running transcription continues | VC:336-353 |
| appearance change | `WM_SETTINGCHANGE` "ImmersiveColorSet" | re-apply theme if System; switch tray icon variant | AC:238-244 |
| — | `WM_DISPLAYCHANGE`, `WM_DPICHANGED` | visible bar: re-place on its monitor keeping the anchor; monitor gone → hide | (Win) |

### 1.5 External URLs

`<base>` = backend base URL; site pages are `<base>/<path>` (CFG:55-62).

| Action | URL | Source |
|---|---|---|
| Plans (tray, Settings) | signed in: `POST /api/v1/web/payment-handoff` `{expected_user_id, locale}` → `url` if `https` and same host as `<base>`; else `<base>/payment` | AC:180-186, ACC:97-110 |
| "Plans" on the limit issue card | mac: plain `<base>/payment`, then hide (QBV:517, QBM:627-630, AC:116); Windows: same flow as tray Plans, then hide **(quirk fix Q5)** | |
| Check for updates | mac `<base>/mac`; Windows `<base>/windows` | AC:65, BE-web:116 |
| Delete account | `config.legal.account_deletion_url` ?? `<base>/account/delete` | AC:188-190 |
| Privacy / Terms / Support | `config.legal.privacy_url` / `terms_url` / `support_url` (only http/https accepted) | MOD:245-289 |
| Microphone privacy | `ms-settings:privacy-microphone` (mac `x-apple.systempreferences:…Privacy_Microphone`) | AC:250-252, WARCH:56-58 |
| Startup apps | `ms-settings:startupapps` (mac Login Items) | PST:187-189 |

Rule (Win): call `AllowSetForegroundWindow(ASFW_ANY)` before `ShellExecute`, and hide the bar after launching any URL
from it (mac hides implicitly when the browser takes key focus, QP:152-155).

### 1.6 Local settings

`%LOCALAPPDATA%\AI Reply\settings.json` (WARCH:53-55, 175-187). Saved on every change (mac `didSet`, AS:29-42).
Invalid/missing value → default.

| mac UserDefaults key | Windows property | Default | Source |
|---|---|---|---|
| `settings.shortcut` | `Shortcut` | Alt+Space | AS:46, KC:18 |
| `settings.placement` | `Placement` (Top, Bottom, Remembered) | Top | AS:8-15, 47 |
| `settings.rememberedPosition` (x = card centre, y = anchor edge, both 0…1 of the visible frame, y from the **bottom**) | `RememberedX` (card centre), `RememberedY` (anchor edge, from the **top** = 1 − mac y) | null → (0.5, 0.22) | AS:31-32, QP:104 |
| `settings.appearance` | `Theme` (System, Light, Dark) | System | AS:17-21, 49 |
| `settings.offerCopiedText` | `OfferCopiedText` | true | AS:34-35, 50 |
| `settings.closeAfterCopy` | `CloseAfterCopy` | true | AS:36-37, 51 |
| `settings.pasteAfterCopy` | `PasteAfterCopy` | false | AS:38-39, 52 |
| `settings.dictationLanguage` | `DictationLanguage` (Auto, Ru, Kk, En) | Auto | AS:40, 53 |
| `settings.lastRecipient` | `LastRecipient` ∈ `friend`, `client`, `work`, `business` | `client` | AS:41-42, 54 |

### 1.7 Installation identity — UI-facing part **(fix vs macOS, PRO-1)**

1. One random `installation_id` (UUID) per Windows user install, created once, persisted (DPAPI-protected file,
   AUD:126), never derived from hardware. Sent in the `device` object of `POST /api/v1/auth/email/otp/verify` and
   `POST /api/v1/auth/refresh` (mac sends neither, API:77-80, 265-283; AUD:78-82).
2. `EnsureInstallationRegistered()`: single-flight authenticated `POST /api/v1/installations` (BE-srv:207-208; body
   fields BE-inst:75-89; `platform: "windows"`). Runs right after verify succeeds (before any AI request), at startup
   with a stored session and after an account switch. Body, fingerprint skip, back-off and gating:
   `api-contract.md` §5.8 and §12 (sibling spec, owner of the wire rules).
3. Every AI request (`/ai/reply`, `/ai/compose`, `/ai/transcribe`) awaits a pending registration for at most 10 s
   (`api-contract.md` §8.1 step 3, §12 rule 3), then proceeds. A failed registration never blocks the AI request
   (non-Pro accounts must keep working).
4. UI: no extra state. Sign-in success switches the bar at once; any wait is covered by the Generating spinner or the
   Transcribing row.
5. `PRO_DEVICE_LIMIT_REACHED`, `PRO_DEVICE_IDENTITY_REQUIRED`, `PRO_DEVICE_REVOKED` (HTTP 403): on generation → issue
   cards #12-14 (§5.11; parity GEN:47-49); on transcription → the same three cards (mac shows `voice.issue.server`,
   AUD:117-118). Never sign out, never clear tokens, never refresh for these codes (refresh only follows a 401
   `UNAUTHORIZED`/`TOKEN_EXPIRED`, API:123-126). Draft, result and copied text stay. `PRO_DEVICE_IDENTITY_REQUIRED`
   first self-heals once in the network layer (`api-contract.md` §12 rule 9) before the card appears.
6. Headers (incl. `X-Installation-ID`, read by the server on sign-out, BE-inst:18-19): `api-contract.md` §2.2.

## 2. Quick bar window

### 2.1 Window traits

| # | Rule | mac source | Windows |
|---|---|---|---|
| 1 | Floats above normal windows | QP:14-15 (`.floating`) | `Topmost` (HWND_TOPMOST) |
| 2 | No taskbar button, not in app switcher | QP:16 (`.ignoresCycle`), APP:16 | `ShowInTaskbar=false` (WS_EX_TOOLWINDOW) |
| 3 | On the current Space and over full-screen apps | QP:16 (`.canJoinAllSpaces`, `.fullScreenAuxiliary`) | shown on the current virtual desktop, never switches desktops; above borderless full-screen windows |
| 4 | Borderless, fully transparent window; cards draw themselves | QP:12, 19-22, 25-26 | `SystemDecorations=None`, transparent background, no DWM frame or shadow |
| 5 | Takes keyboard focus while the previous app stays the paste target | QP:5-6, 29-30 | must activate: remember `GetForegroundWindow()` (ignoring own windows, PST:26-35) before showing, then `SetForegroundWindow(bar)`; focus is given back on hide (§2.4) |
| 6 | Hides when it loses key status unless held open | QP:17, 152-155 | `Deactivated` → if `!HoldsPanelOpen` → `Hide(restoreFocus: false)`. Own popups (dropdowns, tooltips) are not a deactivation |
| 7 | Draggable by its background | QP:18 | pointer press on card background (not on a control or selectable text) → `BeginMoveDrag`; drag end → §2.3 step 7 |
| 8 | Window object kept | QP:24 | `Hide()`, reuse |
| 9 | Soft shadow inside a transparent margin | QP:21-22, SUP:12-13, 80 | same. Clicks on the margin and on the 8 DIP gap must reach the window below (window region / per-pixel hit test); acceptable fallback: a press there hides the bar like an outside click |
| 10 | Esc → `model.Escape()` | QP:32-34 | window-level `KeyDown(Escape)` after open popups handled it |
| 11 | Editing shortcuts work on ru/kk layouts | QP:36-53 | TextBox native Ctrl+X/C/V/A/Z, Ctrl+Y, Ctrl+Shift+Z; all shortcuts match the **virtual key**, never the typed character |
| 12 | Show/hide animation | QP:23 (`.utilityWindow`) | none required |

### 2.2 Geometry tokens

| Token | Value (DIP) | Source |
|---|---|---|
| `BarWidth` | 680 (card width) | SUP:9, QBV:34 |
| Narrow work area | mac: none (fixed 680); brief says min 560 (DES:21). Windows: `BarWidth = clamp(workArea.Width − 32, 560, 680)` | (Win) |
| `CornerRadius` | 16, every card | SUP:10, 75-78 |
| `CardGap` | 8 between bar card and secondary card | SUP:11, QBV:25 |
| `ShadowInset` | 18 on every side → window width 680 + 36 = 716 | SUP:13, QP:11, 93, QBV:35 |
| Card border | 1 px inside, primary @ 8 % | SUP:76-79 |
| Card shadow | black @ 18 %, blur radius 14, offset (0, 6) | SUP:80 |
| Top anchor | card top at 22 % of the work-area height | QP:100-102, DES:23 |
| Bottom anchor | card bottom 24 above the work-area bottom | QP:97-99, DES:24 |
| Remembered default | x 0.5, y 0.22 from top (mac 0.78 from bottom) | QP:104 |

Derived block heights (measure at runtime, never hardcode):

| Block | Height | Source |
|---|---|---|
| Toolbar | 10 top + 26 = 36 | ACV:57-59 |
| Input row, 1 line | 14 + 32 (send) + 14 = 60; text grows to 5 lines of 17 pt, then scrolls | QBV:126-149, 205 |
| Copied row | 12 + capsule (≈ 27) + 8 + chips (≈ 25) ≈ 72 | QBV:220-255 |
| Manual source | 8 top + rounded field of 2–3 lines | QBV:59-63 |
| Recording row | 12 + 32 + 12 = 56 | QBV:312-326 |
| Transcribing row | 14 + 30 + 14 = 58 | QBV:364-373 |
| Sign-in row | 14 + 22 + 8 + footer line (12 pt) + 14 ≈ 73 | QBV:577-607 |
| Result card | 16 + text (scrolls after 260) + 12 + footer (large buttons) + 16 | QBV:383-414 |
| AI Context section | divider + scroll (max 245) + optional issue line + 12 | ACV:68-117 |

### 2.3 Placement algorithm

Coordinates: y grows downward; `WA` = work area (`rcWork`) of the chosen monitor; the window is `BarWidth + 36` wide
and `content + 36` high; "card" = window minus the 18 DIP margin.

1. Monitor = the one under the cursor at show time, fallback primary (QP:91, 145-148).
2. Anchor (QP:96-109):
   - **Top**: `anchoredBottom = false`; `cardTop = WA.Top + 0.22·WA.Height`.
   - **Bottom**: `anchoredBottom = true`; `cardBottom = WA.Bottom − 24`.
   - **Remembered**: `(rx, ry) = saved ?? (0.5, 0.22)`; `anchoredBottom = ry > 0.5` (mac: y-from-bottom < 0.5, QP:107);
     `anchorY = WA.Top + ry·WA.Height` is `cardBottom` if `anchoredBottom` else `cardTop`.
   - x: card centre at `WA.Left + rx·WA.Width` (`rx = 0.5` for Top/Bottom).
3. `GrowsUpward = anchoredBottom` is set before layout (QP:110, AC:120); it orders the cards (§4).
4. Clamp x: `windowLeft ∈ [WA.Left − 18, WA.Right − windowWidth + 18]` (card stays inside WA) (QP:111).
5. Clamp y (QP:135-143): `windowTop < WA.Top − 18` → `windowTop = WA.Top − 18`; then `windowBottom > WA.Bottom + 18`
   → `windowBottom = WA.Bottom + 18` (applied second, wins). **(quirk fix Q7)**: when content is taller than WA, cap the
   secondary card's scroll area so the bar card stays fully inside WA (mac can push the bar off-screen).
6. Height change (QP:124-133): measured height differs by > 0.5 → keep the anchored edge (top-anchored: top fixed,
   grows down; bottom-anchored: bottom fixed, grows up), then clamp (step 5) on the window's current monitor.
   Window resize is not animated (QP:141).
7. User drag end (QP:158-166) → `Placement = Remembered`, `rx = (cardCentreX − WA.Left)/WA.Width`. **(quirk fix Q1)**:
   recompute the anchor from the dropped card: `anchoredBottom = cardCentreY > WA.Top + WA.Height/2`;
   `ry = ((anchoredBottom ? cardBottom : cardTop) − WA.Top)/WA.Height`; update `GrowsUpward` at once. (mac keeps the
   anchor from the last show, so a drag across the midline makes the next open jump by the card height and grow the
   wrong way until reopened.) Only user drags save; programmatic placement never does (QP:140-142, 159).
8. Example (100 %, `WA` = 0,0,1920,1040, content 96): Top → window (602, 211, 716 × 132) (`cardTop` 228.8);
   Bottom → window bottom at 1034 (`cardBottom` 1016).

```
 Top / upper half: grows down                Bottom / lower half: grows up
 WA top                                      +----------------------------+
   ... 22 % of WA height ...                 | secondary card             |
 +----------------------------+ <- anchor    +----------------------------+
 | bar card                   |                    8 DIP gap
 +----------------------------+              +----------------------------+
       8 DIP gap                             | bar card                   |
 +----------------------------+    anchor -> +----------------------------+
 | secondary card             |                ... 24 DIP ...
 +----------------------------+              WA bottom
```

### 2.4 Show / hide lifecycle

`ShowPanel()` (AC:96-102):
1. Visible → return (Win: also activate it).
2. Remember the foreground window; null when it is one of ours, e.g. Settings (AC:98, PST:26-35).
3. `copied = OfferCopiedText ? clipboard.TakeFreshCopiedText(maxAge: 600 s) : None` (AC:99, CLIP:104-132).
4. `model.PanelDidOpen(copied)` (§3.3).
5. Place (§2.3), show, activate, focus (§7.3) (QP:90-115).

`Hide(restoreFocus)` (QP:117-121): visible? → hide → `model.PanelDidClose()`: `lastActivity = now`; voice
RequestingPermission/Recording → `voice.Cancel()` (a transcription continues) (QBM:186-189) → (Win) `restoreFocus` →
`SetForegroundWindow(previous)` if still a valid window.

| Hide trigger | restoreFocus | Source |
|---|---|---|
| Esc with no voice activity and no generation | yes | QBM:368-382 |
| Global hotkey while visible | yes | AC:104-106 |
| Deactivation (click elsewhere) unless `HoldsPanelOpen` | no | QP:152-155 |
| After copy when `CloseAfterCopy \|\| PasteAfterCopy` | yes (paste needs it) | AC:134-142 |
| Limit card "Plans" | no | QBM:627-630 |
| Settings opened (tray item 5, account button) | no | AC:194-198 |
| (Win) any URL opened from the bar | no | §1.5 |

`HoldsPanelOpen` = file dialog open OR voice ∈ {RequestingPermission, Recording, Transcribing} (QBM:192-198).
A running generation continues while hidden; its result or issue is shown on the next open.

Keep-alive (QBM:157-176, 200-209; QBT:299-311): in `PanelDidOpen`, if `lastActivity != null && now − lastActivity >
600 s && Phase != Generating` → `ResetConversation()`: `Draft = ""`, `ManualReplySource = ""`,
`ReferenceAttachments = []`, `Result = ""`, `Issue = null`, `Phase = Editing`, `Mode = Compose`, `LastRequest = null`.
Not reset (parity, AUD MAC-9 open): `CustomInstructions`, `OutputLength`, `SelectedProfileId`, `AdvancedExpanded`,
`EmailInput`, `CodeInput`. `lastActivity` is set on hide, on generation success and on copy (QBM:187, 330, 359).

### 2.5 Request rules the UI drives **(fix vs macOS, MAC-1/MAC-2)**

| Field | Windows rule | mac behaviour | Source |
|---|---|---|---|
| `/ai/reply` `template_id` | always the selected recipient (`friend`, `client`, `work`, `business`) | same | GEN:131; BE-svc:184 (reply never takes the mac path through `template_id`) |
| `/ai/compose` `template_id` | **never sent** (template menu removed, mobile compose has no recipient) | template's recipient whenever the server advertises `mac_context_generation`, `custom` → `work` | QBM:20-27, 409; GEN:167; AUD:175-183 |
| `context` | only when `ContextActive` (§3.2) and retrieval yields profile text or references | built whenever the capability is on | QBM:403-411; CTX:221-271 |
| `output_char_limit` | `600` only when Extended; Standard → omitted | `400`/`600` on every request when the capability is on | CTX:197-200; AUD:175-178; BE-mac:85-87 |
| `instruction` | reply: trimmed draft, omitted if empty; compose: trimmed draft | same | QBM:252, 268-270; GEN:128-129 |
| `regenerate` (compose) | true only for "Another version" | same | QBM:273-282 |
| `input_language` | keyboard layout language at submit (`GetKeyboardLayout(0)` primary LANGID → `kk`/`ru`/`en`), else omitted | TIS input source | QBM:302; SUP:85-92; GEN:189-193 |
| `language` | UI language `kk`/`ru`/`en`, else `ru` | same | API:258-262 |
| `platform` | `windows` | `macos` | CFG:9-15 |
| `Idempotency-Key` | new lowercase UUID per user action; Retry reuses it | same | QBM:285-288, 350 |

If `ContextActive && !DesktopContextAvailable` submit is disabled and the section shows `bar.context.server_update`
(QBM:237, ACV:70-74). Backend dependency: context fields are accepted only for session platform `macos`
(BE-mac:37-39, AUD CTX-B7), so `DesktopContextAvailable` stays false on Windows until the server accepts `windows`
sessions (§3.2; `api-contract.md` §14.3, §15.1; open question 2).

### 2.6 Visual tokens

2.6.1 **Card surface.** mac: `NSVisualEffectView` popover material, behind-window blur (SUP:18-32, 74; DES:21-22).
Windows cannot blur per card inside one transparent window: card fill light `#F9F9F9`, dark `#2C2C2C` (Fluent acrylic
fallback colours), 1 px border, shadow per §2.2. Transparency effects off or High Contrast → system colours, no
transparency, 1 px `WindowText` border.

2.6.2 **Colours.**

| Token | Light | Dark | mac origin |
|---|---|---|---|
| Accent | Windows accent (Fluent `AccentFillColorDefault`); fallback `#007AFF` | fallback `#0A84FF` | asset AccentColor, DES:10 |
| TextPrimary / TextSecondary | `#E4000000` / `#9E000000` | `#FFFFFF` / `#C5FFFFFF` | `.primary` / `.secondary` |
| Overlay | black @ α | white @ α | `Color.primary.opacity(α)`, α ∈ {0.035, 0.05, 0.06, 0.08, 0.10} |
| Warning text | `#B35900` (4.6:1 on card) | `#FFB340` | `.orange` (`#FF9500` / `#FF9F0A`), contrast fixed |
| Danger text | `#D70015` | `#FF6961` | `.red`, contrast fixed (QBV:162, ACV:85) |
| Recording dot | `#FF3B30` | `#FF453A` | `.red` (QBV:302) |
| Chip selected | fg Accent, bg Accent @ 12 % | same | QBV:281-282 |
| Chip normal | fg TextPrimary, bg overlay 5 % (hover 10 %) | same | QBV:282 |
| Icon button hover | circle overlay 8 % | same | QBV:179 |
| Send disabled | Accent @ 35 % | same | QBV:198 |

2.6.3 **Type.** Segoe UI Variable (Win 11) / Segoe UI (Win 10), same DIP sizes as mac pt: 17 input and sign-in fields;
15 result and Transcribing text, icon glyphs; 14 semibold consent title; 13.5 issue text; 13 consent body;
12.5 capsule, chips, links, output label; 12 footers, warnings, balance caption, attachment rows; 11.5 privacy
caption; 11 medium counter. Tabular digits (OpenType `tnum`) for counters, timer and OTP code (QBV:161, 308-309, 584).

2.6.4 **Brand glyph** (SUP:36-66; 18 × 18 canvas, y downward = 18 − mac y):
- back bubble: rounded rect (6.5, 3.0, 10.5 × 9.5), radius 3.6, stroke 1.5, erased where the front bubble lies;
- front bubble: rounded rect (1, 4.0, 12 × 10.5), radius 4, plus tail polygon (3, 13.5) → (2, 17) → (6.5, 14.4), filled;
- three holes in the front bubble (even-odd): circles r 1.1 at (4.2, 9.3), (7.0, 9.3), (9.8, 9.3).

In the bar: 22 × 22, filled with Accent, hidden from UI Automation (QBV:110-119). Tray: monochrome. Accessible name
`AI Reply` (SUP:63).

2.6.5 **Icons** (SF Symbol → Fluent UI System Icons name; embed as vector or use the Segoe Fluent/MDL2 equivalent):
mic → Microphone; arrow.up → ArrowUp; arrowshape.turn.up.left → ArrowReply; xmark.circle.fill → DismissCircle
(filled); person / briefcase / person.2 / building.2 → Person / Briefcase / People / Building (chips, SUP:159-166);
xmark → Dismiss; checkmark → Checkmark; arrow.clockwise → ArrowClockwise; doc.on.doc → Copy; hourglass → Hourglass;
wifi.slash → WifiOff; mic.slash → MicOff; exclamationmark.circle → ErrorCircle; chevron.down/up →
ChevronDown/ChevronUp; person.crop.circle → PersonCircle; pencil → Edit; plus → Add; paperclip → Attach;
doc.text → DocumentText; gearshape / mic / person.crop.circle / info.circle → Settings / Mic / PersonCircle / Info
(Settings tabs).

2.6.6 **Motion.** 220 ms "snappy" content transitions on changes of `Phase`, `Issue`, `Mode`, `AdvancedExpanded`
(QBV:43-46): result and consent cards fade + slide in from the bar side (top edge when growing down, bottom edge when
growing up) (QBV:92, 98); issue card fades (QBV:95); AI Context section fades (QBV:81). Waveform bars animate 80 ms
linear (QBV:354). All animation off when Windows "Animation effects" is off (`SPI_GETCLIENTAREAANIMATION` false; mac
Reduce Motion, QBV:14, DES:12).

## 3. View-model contract (`QuickBarViewModel`)

### 3.1 State (QBM:79-103)

| Name | Type | Initial | Notes |
|---|---|---|---|
| `Draft` | string | "" | instruction field |
| `EmailInput`, `CodeInput` | string | "" | sign-in |
| `ManualReplySource` | string | "" | manual reply field |
| `AdvancedExpanded` | bool | false | AI Context section open |
| `SelectedProfileId` | Guid? | null | |
| `CustomInstructions` | string | "" | per-conversation instructions |
| `OutputLength` | Standard = 400, Extended = 600 | Standard | CTX:197-200 |
| `EditingProfile` | profile metadata? | null | **(fix vs macOS, MAC-3)**: metadata only, no attachments |
| `ContextIssue` | string? | null | one line under the AI Context scroll area |
| `ImportingDocument` | bool | false | |
| `ContextStorageBusy` | bool | true until profiles loaded | QBM:91, 145-150 |
| `ReferenceAttachments` | list | [] | ephemeral, not in a profile |
| `Mode` | Compose \| Reply(CopiedText) \| ManualReply | Compose | QBM:43-48 |
| `Phase` | Editing \| Generating \| Result | Editing | QBM:50-54 |
| `Result` | string | "" | |
| `Issue` | QuickBarIssue? | null | Generation(err) \| Voice(err, serverCode) \| NothingHeard \| ConsentFailed (QBM:31-36) |
| `JustCopied` | bool | false | |
| `Recipient` | friend \| client \| work \| business | parse(`LastRecipient`) ?? client | QBM:139 |
| `FocusRequested` | event | | mac `focusRequest += 1` (QBM:100-101) |
| `GrowsUpward` | bool | false | QBM:102-103 |

Removed on Windows: `template` / `DesktopTemplate` and `selectTemplate` (QBM:8-28, 84, 396-401) **(fix vs macOS, MAC-2)**.

### 3.2 Derived values (QBM:211-242)

| Name | Definition | Source |
|---|---|---|
| `NeedsConsent` | signed in && `hasConsent == false` (null until `/me` read → false) | QBM:213, ACC:43-46 |
| `CopiedText` | `Mode is Reply(t) ? t : null` | QBM:215-218 |
| `SourceLimit` | config `max_source_characters` (default 400) | QBM:220, MOD:305 |
| `InstructionLimit` | config `max_instruction_length` (default 400) | QBM:221, MOD:306 |
| `IsReplyMode` | `Mode != Compose` | QBM:222 |
| `SourceText` | `CopiedText?.Text ?? (IsReplyMode ? ManualReplySource : "")` | QBM:223 |
| `SourceLength`, `DraftLength` | `ServerLength` = trim whitespace/newlines, count Unicode code points | QBM:224-225, TM:7-9 |
| `SourceTooLong` | `SourceLength > SourceLimit` | QBM:226 |
| `VoiceAvailable` | `features.voice_transcription == true` | QBM:227, MOD:321 |
| `DesktopContextAvailable` | mac: `features.mac_context_generation == true`. Windows: the server accepts context from `windows` sessions — proposed flag `features.windows_context_generation` (`api-contract.md` §15.1); false while absent, and false for the rest of the run after issue #4b (open question 2) | QBM:228, MOD:322 |
| `Attachments` | selected profile's attachments + `ReferenceAttachments` | QBM:230 |
| `ContextActive` (mac `hasAdvancedContext`) | profile selected \|\| `CustomInstructions` non-blank \|\| `Attachments` non-empty \|\| `OutputLength == Extended` | QBM:231-233 |
| `CanSubmit` | signed in; `!NeedsConsent`; `Phase != Generating`; `!ImportingDocument`; `!ContextActive \|\| DesktopContextAvailable`; `!ContextActive \|\| !ContextStorageBusy`; `ServerLength(CustomInstructions) ≤` effective limit (1000, §5.12.3); reply: `SourceLength > 0 && !SourceTooLong && DraftLength ≤ InstructionLimit`; compose: `0 < DraftLength ≤ InstructionLimit` | QBM:235-242 |

### 3.3 Actions

| Action | Behaviour | Source |
|---|---|---|
| `PanelDidOpen(copied)` | keep-alive check (§2.4); if `OfferCopiedText` and `copied` is text and `Mode != Reply(same)` → `Mode = Reply(text)`, and if `Phase == Result` → `Phase = Editing`, `Result = ""`; `Issue = null`; raise focus; async: `RefreshConfig()` (cache 300 s), if signed in `RefreshMe()` (always fetches), then `ClearLimitIfLifted()` | QBM:160-176, ACC:81-92 |
| `ClearLimitIfLifted()` | `Issue` is LimitReached and balance not exhausted (`!unlimited && remaining_total ≤ 0` = exhausted) → `Issue = null` | QBM:178-184, ACC:18 |
| `PanelDidClose()` | §2.4 | QBM:186-189 |
| `Submit()` (Enter) | `!CanSubmit` → nothing; reply → `ReplyTo(Recipient)`; compose → run Compose(trimmed draft, regenerate false, new key) | QBM:247-255 |
| `ReplyTo(r)` (chip, Ctrl+1…4) | `Recipient = r`; save `LastRecipient`; then only if `IsReplyMode && CanSubmit` → run Reply(source, r, trimmed draft or null, new key) | QBM:258-271 |
| `Regenerate()` (Ctrl+R) | needs `LastRequest` and not Generating; same request and context snapshot, new key; compose sets `regenerate = true` | QBM:273-282 |
| `Retry()` | same request, same key, same snapshot (never charged twice) | QBM:284-288, QBT:214-230, 359-393 |
| `CancelGeneration()` | cancel task; `Phase = Result.IsEmpty ? Editing : Result` | QBM:290-294 |
| Run (internal) | cancel previous; `LastRequest = request`; `Issue = null`; `Phase = Generating`; resolve context off the UI thread from the snapshot; success → `Result = text`, `Phase = Result`, `lastActivity = now`, apply returned usage, mark copied text handled; failure → `Phase = Result.IsEmpty ? Editing : Result`, `Issue = Generation(err)`; SignedOut → end session; ConsentRequired → `RefreshMe()`; LimitReached → `RefreshMe()` then `ClearLimitIfLifted()`. Timeout 45 s | QBM:296-348, GEN:134, 172 |
| `CopyResult()` | §9 | QBM:354-366 |
| `Escape()` | §7.2 | QBM:368-382 |
| `DismissCopied()` | mark copied text handled; `Mode = Compose`; `Issue = null`; raise focus | QBM:384-389 |
| `SelectReplyMode()` | `Mode == Compose` → `ManualReply`; raise focus | QBM:391-394 |
| `ToggleVoice()` | Recording → `Stop()`; Idle/Finished/Failed → requires signed in && `!NeedsConsent`; `Issue = null`; reset if not Idle; `Start(DictationLanguage)`; RequestingPermission/Transcribing → nothing | QBM:536-548 |
| `CancelVoice()` | `voice.Cancel()`; raise focus | QBM:550-553 |
| Voice result | Finished(text): trim; reset; empty → `Issue = NothingHeard`; else `Draft = Draft.Trim().IsEmpty ? text : Draft.Trim() + " " + text`, raise focus. Failed(err): reset; `CONSENT_REQUIRED` → `RefreshMe()`; PRO codes → §1.7.5; `Issue = Voice(err)` | QBM:566-587 |
| `SubmitEmail()`, `SubmitCode()`, `ResendCode()`, `UseAnotherEmail()` | §5.1, §5.2 | QBM:591-612 |
| `AcceptConsent()` | §5.3 | QBM:614-624 |
| `OpenPricing()` | open URL (§1.5), then hide | QBM:627-630 |
| `ClearIssue()` | `Issue = null` | QBM:530-532 |
| `OpenAccountSettings()` | `OpenSettings()` (hides bar) + select Account tab | AC:127-130 |

## 4. Composition

Bar card, top to bottom (QBV:49-86):
1. Signed out → sign-in row only (§5.1/§5.2).
2. Signed in → toolbar (§5.4), then:
   a. `CopiedText != null && voice Idle` → copied row (§5.5);
   b. else `IsReplyMode && voice Idle` → manual source field + (too-long warning | chips) (§5.6);
   c. voice RequestingPermission or Recording → recording row; Transcribing → transcribing row; else input row;
   d. `AdvancedExpanded` → AI Context section (§5.12).

Secondary card, first match wins (QBV:88-100): (1) signed in && `NeedsConsent` → consent; (2) `Issue != null` →
issue; (3) `Phase == Result` → result; (4) none.

Stack: `GrowsUpward ? [secondary, bar] : [bar, secondary]`, gap 8, column width 680, margin 18 (QBV:24-36).

Legend for wireframes: `*` brand glyph, `(@)` account, `(mic)` mic, `[^]` send, `[ ]` disabled send, `[~]` spinner,
`(x)` dismiss/cancel, `(v)` stop, `<Chip>` chip, `<*Chip>` selected chip, `o` context dot.

## 5. States

### 5.1 Signed out — email entry (auth state SignedOut)

```
+------------------------------------------------------------------------+
|  *   Email to sign in                                           [~]    |
|      Use the same email as in the phone app                            |
+------------------------------------------------------------------------+
```

| Element | Spec | Key | Source |
|---|---|---|---|
| Row | VStack spacing 8; HStack spacing 10; padding 14 all round; no toolbar | | QBV:576-607 |
| Glyph | 22, Accent | | QBV:579 |
| Email field | borderless, 17, email input scope, Enter → `SubmitEmail()` | `signin.email.placeholder` | QBV:593-598 |
| Spinner | small, right of the field while a request runs | | QBV:600-602 |
| Footer | 12, left inset 34; error in Warning colour, else hint in TextSecondary | `signin.hint` | QBV:614-622, 635-636 |

`SubmitEmail()` (AUTH:82-112): trim; no `@` or > 254 chars → `signin.error.email`, no request; else
`POST /api/v1/auth/email/otp/request {email, locale}` → code entry. `EmailInput` survives "Another email".
Secondary card: only an issue (e.g. `issue.signed_out` after a forced sign-out, §5.11 #11).

### 5.2 Signed out — code entry (auth state AwaitingCode)

```
+------------------------------------------------------------------------+
|  *   Code sent to a***@example.kz                               [~]    |
|      Enter the code from the email    Send again in 27 s  Another email|
+------------------------------------------------------------------------+
```

| Element | Spec | Key | Source |
|---|---|---|---|
| Code field | borderless, 17, tabular digits; on change keep digits only, cut to `codeLength` (default 4); reaching `codeLength` → `SubmitCode()`; Enter → `SubmitCode()` | `signin.code.placeholder %@` (masked email) | QBV:581-591, MOD:227-233 |
| Footer left | error (Warning) else hint | `signin.code.hint` | QBV:618-619 |
| Resend link | `wait = ceil(resendAvailableAt − now)`, ticks every 1 s; `wait > 0` → disabled `signin.resend.wait %lld`; else `signin.resend`, disabled while a request runs | `signin.resend.wait %lld`, `signin.resend` | QBV:624-631, AUTH:107-108 |
| Another email | link → `UseAnotherEmail()`: `CodeInput = ""`, clear error, state SignedOut, focus | `signin.another_email` | QBV:632, QBM:608-612, AUTH:146-149 |

`SubmitCode()` (AUTH:116-143, QBM:595-602): ASCII digits only; count ≠ `codeLength` → `signin.error.code`, no request;
else `POST /api/v1/auth/email/otp/verify {email, code, device(+installation_id)}` → signed in: `CodeInput = ""`,
focus, apply `/me` payload, `RefreshConfig()` (AC:147-150), `EnsureInstallationRegistered()` (§1.7). A wrong code keeps
the digits (parity). `ResendCode()` re-requests for the same email (AUTH:92-95).

| AuthError | Server code | Footer key | Source |
|---|---|---|---|
| invalidEmail | `INVALID_EMAIL` | `signin.error.email` | AUTH:21 |
| invalidCode | `INVALID_OTP` | `signin.error.code` | AUTH:22 |
| codeExpired | `OTP_EXPIRED` | `signin.error.expired` | AUTH:23 |
| codeAlreadyUsed | `OTP_ALREADY_USED` | `signin.error.used` | AUTH:24 |
| tooManyAttempts | `OTP_ATTEMPTS_EXCEEDED` | `signin.error.attempts` | AUTH:25 |
| cooldown(s) | `OTP_RESEND_COOLDOWN` (s = ceil Retry-After, default 30) | `signin.error.cooldown %lld` | AUTH:26-31 |
| rateLimited | `RATE_LIMITED` | `issue.rate_limited` | AUTH:32 |
| emailDelivery | `EMAIL_DELIVERY_FAILED` | `signin.error.delivery` | AUTH:33 |
| accountDisabled | `ACCOUNT_DISABLED` | `issue.account_disabled` | AUTH:34 |
| offline | transport failure | `issue.offline` | AUTH:35 |
| server | anything else | `issue.server` | AUTH:35 |

Footer message mapping: QBV:639-653.

### 5.3 Consent card

Shown when signed in and `/me` has no `legal_consent` (QBM:213, ACC:43-46). The bar above is the normal signed-in
bar; Send is disabled and the mic does nothing (QBM:236, 541).

```
+------------------------------------------------------------------------+
| [Message|Reply]                                    AI Context v    (@) |
|  *   What should I write? E.g. politely tell the client...  (mic) [ ]  |
+------------------------------------------------------------------------+
+------------------------------------------------------------------------+
|  Allow text processing                                                 |
|  To write a reply, AI Reply sends your text to its server and to       |
|  OpenAI. We don't store this text.                                     |
|  Privacy Policy   Terms                                   [ Accept ]   |
+------------------------------------------------------------------------+
```

| Element | Spec | Key | Source |
|---|---|---|---|
| Card | padding 16, spacing 10 | | QBV:540, 566 |
| Title | 14 semibold | `consent.title` | QBV:541 |
| Body | 13 TextSecondary, wraps; arg = `legal.ai_provider` ?? `OpenAI` | `consent.body %@` | QBV:542-545 |
| Links | 12.5, spacing 14, each only when its URL exists; open in browser + hide (§1.5) | `consent.privacy`, `consent.terms` | QBV:546-552 |
| Accept | accent, large; spinner and disabled while saving | `consent.accept` | QBV:554-563 |

`AcceptConsent()` (ACC:113-132, QBM:614-624): `RefreshConfig(maxAge 60 s)`; both `terms_version` and `privacy_version`
required; `POST /api/v1/me/consents {terms_version, privacy_version, locale, platform: "windows", app_version}`; then
`GET /me` → card disappears, `Issue = null`, focus input. Failure → `Issue = ConsentFailed` (message `issue.server`);
mac hides it behind the consent card (QBV:90-95). **(quirk fix Q2)**: Windows shows `issue.server` as a Warning line
inside the consent card above the buttons until the next Accept.

### 5.4 Compose (Mode Compose, Phase Editing, voice Idle)

```
+------------------------------------------------------------------------+
| [*Message|Reply]                                   AI Context v    (@) |
|  *   What should I write? E.g. politely tell the client...  (mic) [ ]  |
+------------------------------------------------------------------------+
  with text over 3/4 of the limit:
|  *   <long instruction text ...>                  312/400   (mic) [^]  |
```

Toolbar (ACV:6-62), height 26, padding 14 sides / 10 top, spacing 10, disabled while Generating:

| Element | Spec | Key | Source |
|---|---|---|---|
| Mode switch | segmented, width 210; Message → `DismissCopied()`, Reply → `SelectReplyMode()`; UIA name `bar.mode` | `bar.mode.message`, `bar.mode.reply`, `bar.mode` | ACV:11-18 |
| Template menu | **removed (fix vs macOS, MAC-2)** | (`bar.template*`, §11.4) | ACV:19-33 |
| AI Context toggle | right-aligned: dot 5 × 5 Accent when `ContextActive`, label, chevron down/up; 12 medium; Accent when expanded else TextSecondary; toggles `AdvancedExpanded`; UIA expand/collapse state. (Win) shown only when `DesktopContextAvailable \|\| ContextActive` (mac: always); hidden → section hidden too | `bar.context.title`, `bar.context.expanded`, `bar.context.collapsed` | ACV:34-47 |
| Account button | PersonCircle 15, TextSecondary; tooltip + UIA name; → `OpenAccountSettings()` | `settings.account` | ACV:48-55 |

Input row (QBV:121-153), HStack spacing 10, padding 14:

| Element | Spec | Key | Source |
|---|---|---|---|
| Glyph | 22 Accent | | QBV:127 |
| Field | borderless, 17, 1–5 lines then scrolls; placeholder by mode; UIA name; disabled while Generating; Enter → `Submit()`; Alt+Enter → newline | `bar.placeholder.compose` (Compose), `bar.placeholder.reply` (reply modes), `bar.field.accessibility` | QBV:128-137 |
| Counter | Compose only, when `DraftLength > InstructionLimit*3/4` (integer division: 300 for 400); text `N/L` (verbatim) 11 medium tabular; Danger if `N > L`, else TextSecondary; UIA name | `bar.counter %lld %lld` | QBV:138-140, 155-165 |
| Mic | only if `VoiceAvailable`; 30 × 30 circle, Microphone 15 medium TextSecondary, hover overlay 8 %; tooltip + UIA; Ctrl+D; disabled while Generating; → `ToggleVoice()` | `bar.mic` | QBV:141-145, 167-187 |
| Send | 32 × 32, radius 8, Accent (35 % when disabled and idle), white ArrowUp 14 semibold; spinner on full Accent while Generating; disabled when `!CanSubmit`; → `Submit()` | tooltip `bar.send.help`, UIA `bar.send` | QBV:146, 189-212 |

### 5.5 Reply to a copied message (Mode Reply)

Entry: `PanelDidOpen` with text another app copied ≤ 600 s ago, after app start, not concealed, not our own copy, not
already handled, not a file copy (`CF_HDROP` present) (QBM:164-168; CLIP:104-132; WARCH:47-50).

```
+------------------------------------------------------------------------+
| [Message|*Reply]                                   AI Context v    (@) |
|  ( <- «Добрый день! Документы дошли? Когда сможете ответить…»   (x) )  |
|  <Friend> <*Client> <Colleague> <Partner>                              |
|  *   How should I reply? You can leave this empty          (mic) [^]   |
+------------------------------------------------------------------------+
  source over the limit:
|  ( <- «Очень длинное сообщение…»                                (x) )  |
|  412 characters copied, the limit is 400 — shorten the message         |
|  *   How should I reply? You can leave this empty          (mic) [ ]   |
```

| Element | Spec | Key | Source |
|---|---|---|---|
| Row | VStack spacing 8; padding 14 sides / 12 top | | QBV:220, 254-255 |
| Capsule | ArrowReply 11 semibold; preview = `«` + first non-empty line, trimmed + `»`, 1 line, end ellipsis; 12.5 TextSecondary; padding 10 / 6; capsule overlay 6 %; UIA: one element named by the key | `copied.accessibility %@` (preview) | QBV:221-238, 258-262 |
| Dismiss | DismissCircle 13 → `DismissCopied()`; tooltip + UIA | `copied.dismiss` | QBV:225-230 |
| Chips | HStack spacing 6; order Friend, Client, Colleague, Partner (= `Recipient` order `friend, client, work, business`, GEN:5-10); icon 11 + label, 12.5 medium, padding 11 / 5, capsule; selected = `Recipient`; enabled = `CanSubmit`; click or Ctrl+1…Ctrl+4 → `ReplyTo(r)` (generates at once); tooltip on every chip | `recipient.friend`, `recipient.client`, `recipient.colleague`, `recipient.partner`, `recipient.help` | QBV:245-251, 265-289; SUP:150-166 |
| Too long | replaces the chips when `SourceTooLong`: 12, Warning; Send disabled; never truncated | `copied.too_long %lld %lld` (length, limit) | QBV:240-243, DES:52-53 |

Enter → `Submit()` = `ReplyTo(Recipient)` (QBM:249-250). The draft is an optional instruction.

### 5.6 Manual reply (Mode ManualReply)

Entry: Reply segment with no copied text (QBM:391-394).

```
+------------------------------------------------------------------------+
| [Message|*Reply]                                   AI Context v    (@) |
|  +------------------------------------------------------------------+  |
|  | Message to reply to                                              |  |
|  |                                                                  |  |
|  +------------------------------------------------------------------+  |
|  <Friend> <*Client> <Colleague> <Partner>                              |
|  *   How should I reply? You can leave this empty          (mic) [ ]   |
+------------------------------------------------------------------------+
```

| Element | Spec | Key | Source |
|---|---|---|---|
| Source field | rounded border, 2–3 lines then scrolls; padding 14 sides / 8 top; Enter does nothing (no submit in this field); Alt+Enter newline | `bar.reply.source` | QBV:58-63 |
| Too long | under the field, 12 Warning, padding 14 sides; replaces the chips | `copied.too_long %lld %lld` | QBV:64-69 |
| Chips | **(fix vs macOS, MAC-2)**: same row, metrics, enablement (`CanSubmit`, so disabled until a source is typed; the selected chip still shows `Recipient`), shortcuts and action as §5.5 (mac picks the recipient here through the removed template menu) | as §5.5 | QBM:258-271 |

Focus after entering: the instruction field (parity QBM:393; open question 12).

### 5.7 Recording (voice RequestingPermission or Recording)

Entry: mic, Ctrl+D or tray "Dictate" → `ToggleVoice()`. The row replaces the input row; copied row and manual source
are hidden (QBV:56-58, 71-78).

```
+------------------------------------------------------------------------+
| [Message|Reply]                                    AI Context v    (@) |
|  o  |||| ||||||| || |||||||| |||| ||| |||||| |   0:07 / 0:40  (x) (v)  |
+------------------------------------------------------------------------+
```

| Element | Spec | Key | Source |
|---|---|---|---|
| Row | HStack spacing 12, padding 16 / 12; UIA group name | `voice.recording` | QBV:300, 325-328 |
| Dot | 8 × 8 recording red; opacity 1 when Recording, 0.4 while RequestingPermission; static (no pulse) | | QBV:301-304 |
| Waveform | fills the free width, height 28; 40 levels = last 2 s at 50 ms, oldest first; bar width `max(2, (W − 3·39)/40)`, gap 3, bar height `max(3, level·28)`, Accent capsules; hidden from UIA | | QBV:305-307, 337-357; VC:81-88 |
| Timer | `m:ss / m:ss` elapsed / max (max 40 s → `0:40`), seconds floored; 12 medium tabular TextSecondary | | QBV:308-310, 331-334 |
| Cancel | 30 × 30 Dismiss → `CancelVoice()` (audio dropped, focus input); tooltip + UIA | `voice.cancel` | QBV:311 |
| Stop | 32 × 32 Accent circle, white Checkmark 13 bold; Enter; enabled only while Recording; → `ToggleVoice()` (stop + transcribe) | `voice.stop` | QBV:312-323 |

Rules: auto-stop at 40 s → Transcribing (VC:242-248); captured < 0.5 s → `Voice(TooShort)`, nothing uploaded
(VC:283-288); input device lost → transcribe what was captured, or `NoInputDevice` if too short (VC:258-270); sleep,
lock or bar hidden → `Cancel()` (VC:349-353, QBM:188). Microphone blocked by Windows privacy → `Voice(PermissionDenied)`.

### 5.8 Transcribing (voice Transcribing)

```
+------------------------------------------------------------------------+
| [Message|Reply]                                    AI Context v    (@) |
|  [~]  Recognizing…                                               (x)   |
+------------------------------------------------------------------------+
```

| Element | Spec | Key | Source |
|---|---|---|---|
| Row | HStack spacing 10, padding 16 / 14 | | QBV:364, 372-373 |
| Spinner + text | small spinner; text 15 TextSecondary; no shimmer | `voice.transcribing` | QBV:365-368 |
| Cancel | Dismiss → `CancelVoice()` | `voice.cancel` | QBV:370 |

Upload `POST /api/v1/ai/transcribe` (multipart `request_id`, `duration_ms` ≤ 40500, `languages` ≤ 3 hints,
`purpose=instruction`, `audio`), timeout 75 s (SUP:95-145). Result handling §3.3 "Voice result": text appended to the
draft, focus with caret at the end and no selection **(quirk fix Q4)**; never auto-generates (DES:63-65). The bar
stays open (`HoldsPanelOpen`); Esc cancels; hiding via hotkey does not cancel the transcription (QBM:188).

### 5.9 Generating (Phase Generating)

```
+------------------------------------------------------------------------+
| [Message|Reply] (disabled)                AI Context v (disabled)  (@) |
|  *   ответь клиенту                       (disabled)        (mic) [~]  |
+------------------------------------------------------------------------+
  (no secondary card unless the consent card applies)
```

Entry: `Submit()`, chip, Retry, Regenerate. Field, mic, toolbar and AI Context section disabled (QBV:136, 144;
ACV:60, 118); Send shows the spinner on full Accent; the previous result card is hidden while generating (QBV:96).
Esc → `CancelGeneration()`. Hiding does not cancel. Exit → Result (§5.10) or Issue (§5.11); (Win) refocus the
instruction field afterwards (§7.3 F5).

### 5.10 Result card (Phase Result, no issue, no consent)

```
+------------------------------------------------------------------------+
| [Message|Reply]                                    AI Context v    (@) |
|  *   ответь клиенту                                        (mic) [^]   |
+------------------------------------------------------------------------+
+------------------------------------------------------------------------+
|  Добрый день! Документы получили, спасибо. Завтра до обеда пришлём     |
|  ответ по договору.                                                    |
|  Replies left: 27              [ (r) Another version ]  [ (c) Copy  ]  |
+------------------------------------------------------------------------+
  for 700 ms after copying:                               [ (v) Copied ]
```

| Element | Spec | Key | Source |
|---|---|---|---|
| Card | VStack spacing 12, padding 16 | | QBV:383, 414 |
| Text | 15, line spacing 3, selectable, read-only (DES:72 says editable; code wins), left-aligned; scroll viewer max height 260, shrinks to content | | QBV:384-392 |
| Balance caption | 12 TextSecondary: unlimited → `balance.unlimited %@` (plan name); `remaining_total ≤ 0` → `balance.empty`; else `balance.remaining %lld`; nothing when balance unknown | `balance.unlimited %@`, `balance.empty`, `balance.remaining %lld` | QBV:395, 418-436 |
| Another version | standard button, large, ArrowClockwise; Ctrl+R; disabled while Generating; → `Regenerate()` | `result.regenerate` | QBV:397-402 |
| Copy | accent button, large, min width 110; Copy icon + `result.copy`, for 700 ms Checkmark + `result.copied`; Ctrl+Enter; tooltip → `CopyResult()` | `result.copy`, `result.copied`, `result.copy.help` | QBV:403-411 |

Footer HStack spacing 8: caption, spacer, buttons (QBV:394-412). The draft is not cleared after generation; Enter in
the field sends a new request with a new key (QBM:247-255).

### 5.11 Issue cards (`Issue != null`, no consent card)

```
+------------------------------------------------------------------------+
| (icon)  No replies left. Choose a plan or a pack on the website [Plans]|
+------------------------------------------------------------------------+
```

Card: HStack spacing 12, padding 14; icon 15 medium in the tint; message 13.5, wraps; spacer ≥ 8; one action button
(QBV:440-457). Tint = Accent for #1, Warning otherwise (QBV:468-471). Icon: #1 Hourglass, #2 and #21 WifiOff, #15 MicOff,
others ErrorCircle (QBV:459-466). Default action = `issue.action.ok` → `ClearIssue()` (QBV:527-529).

| # | Issue | Server code / cause | Message key | Action key → effect | Source |
|---|---|---|---|---|---|
| 1 | Generation LimitReached(resetsAt) | `DAILY_LIMIT_REACHED`, `MONTHLY_LIMIT_REACHED`, `PAYMENT_REQUIRED`, `SUBSCRIPTION_EXPIRED` (`details.resets_at`) | `resetsAt != null && balance.daily_limit > 0` → `issue.limit.daily %@` (local short time); else `issue.limit.empty` | `issue.action.plans` (accent) → `OpenPricing()` (Q5) | GEN:51-52; QBV:477-481, 516-518 |
| 2 | Generation Offline | transport failure, cancelled | `issue.offline` | `issue.action.retry` → `Retry()` | GEN:41; QBV:492, 519-520 |
| 3 | Generation AiUnavailable | `AI_PROVIDER_UNAVAILABLE`, `AI_TIMEOUT`, `AI_EMPTY_RESPONSE` | `issue.ai_unavailable` | `issue.action.retry` | GEN:54-55; QBV:491 |
| 4 | Generation Server | any other code, `INVALID_REQUEST` without a known field, unreadable response | `issue.server` | `issue.action.retry` | GEN:42, 62-64; QBV:494 |
| 5 | Generation RateLimited | `RATE_LIMITED` | `issue.rate_limited` | `issue.action.retry` | GEN:53; QBV:490 |
| 6 | Generation SourceTooLong(limit) | `INVALID_REQUEST` `field=source_text`, `max_characters` | `issue.source_too_long %lld` | OK | GEN:56-59; QBV:482 |
| 7 | Generation InstructionTooLong(limit) | `INVALID_REQUEST` `field=instruction`, `max_characters` | `issue.instruction_too_long %lld` | OK | GEN:60; QBV:483 |
| 8 | Generation InstructionMissing | `INVALID_REQUEST` `field=instruction`, no limit | `issue.instruction_missing` | OK | GEN:61; QBV:484 |
| 9 | Generation ConsentRequired | `CONSENT_REQUIRED` (then `/me` → consent card covers it) | `issue.consent` | OK | GEN:45; QBV:485; QBM:340 |
| 10 | Generation AccountDisabled | `ACCOUNT_DISABLED` | `issue.account_disabled` | OK | GEN:46; QBV:486 |
| 11 | Generation SignedOut | no session / refresh failed / 401 `UNAUTHORIZED`, `TOKEN_EXPIRED` → session ended, bar shows sign-in | `issue.signed_out` | OK | GEN:40, 50; QBV:493; QBM:339 |
| 4a | Generation ContextTooLarge **(fix vs macOS, MAC-10)** | `INVALID_REQUEST` `field` ∈ {`context`, `context.profile`, `context.references`} | `issue.context_too_large` (new, `api-contract.md` §6.3) | OK (no Retry: the same body fails again) | AUD:210-211 |
| 4b | Generation ContextUnavailable **(Win)** | `INVALID_REQUEST` `field=context`, reason `feature unavailable` / `requires an authenticated macOS session` | `issue.context_unavailable` (new) | OK; `DesktopContextAvailable = false` for the rest of the run (toggle stays visible with `bar.context.server_update` until the user clears the context) | BE-mac:34-39 |
| 4c | Generation OutputTooLong **(fix vs macOS, MAC-10)** | 502 `AI_OUTPUT_LIMIT_EXCEEDED` (refunded) | `issue.output_too_long` (new) | `issue.action.retry` | AUD:176, 210-211 |
| 12 | Generation ProDeviceLimit | `PRO_DEVICE_LIMIT_REACHED` | `issue.pro_device_limit` | `issue.action.support` → open `support_url` if present, else OK | GEN:47; QBV:487, 523-526 |
| 13 | Generation ProDeviceIdentity | `PRO_DEVICE_IDENTITY_REQUIRED` | `issue.pro_device_identity` | as #12; when the session can never be bound (`api-contract.md` §12 rule 9) → `issue.action.sign_in_again` (new) → user-initiated sign-out, bar shows sign-in | GEN:48; QBV:488 |
| 14 | Generation ProDeviceRevoked | `PRO_DEVICE_REVOKED` | `issue.pro_device_revoked` | as #12 | GEN:49; QBV:489 |
| 15 | Voice PermissionDenied / Restricted | Windows microphone privacy off | `voice.issue.permission` | `issue.action.settings` → `ms-settings:privacy-microphone` | QBV:498, 521-522 |
| 16 | Voice NoInputDevice | no capture device | `voice.issue.no_device` | OK | QBV:499 |
| 17 | Voice TooShort | < 0.5 s locally, or 400 `INVALID_REQUEST` `reason=too_short` | `voice.issue.too_short` | OK | QBV:500; SUP:131 |
| 18 | Voice Unavailable | 404 `NOT_FOUND` | `voice.issue.unavailable` | OK | QBV:501; SUP:128 |
| 19 | Voice LimitReached | `VOICE_LIMIT_REACHED` | `voice.issue.limit` | OK | QBV:502; SUP:129 |
| 20 | Voice RateLimited | `RATE_LIMITED` | `issue.rate_limited` | OK | QBV:503 |
| 21 | Voice Offline | transport failure | `issue.offline` | OK | QBV:504 |
| 22 | Voice RecordingFailed | capture failure | `voice.issue.recording_failed` | OK | QBV:505 |
| 23 | Voice Server(code) | other codes | `voice.issue.server` | OK | QBV:506 |
| 23a | Voice ProDeviceLimit / Identity / Revoked **(fix vs macOS, PRO-1)** | 403 `PRO_DEVICE_*` on `/ai/transcribe` | `issue.pro_device_limit` / `issue.pro_device_identity` / `issue.pro_device_revoked` | as #12 / #13 | AUD:117-118; `api-contract.md` §6.4 |
| 23b | Voice ConsentRequired **(Win)** | 403 `CONSENT_REQUIRED` (also `RefreshMe()` → consent card) | `issue.consent` | OK | QBM:580-583; `api-contract.md` §6.4 |
| 23c | Voice AccountDisabled **(Win)** | 403 `ACCOUNT_DISABLED` | `issue.account_disabled` | OK | `api-contract.md` §6.4 |
| 23d | Voice SignedOut **(Win)** | session ended during upload (mac: `voice.issue.server`, SUP:124) | `issue.signed_out` | OK; bar shows sign-in | `api-contract.md` §6.4 |
| 24 | NothingHeard | empty transcript | `voice.issue.nothing_heard` | OK | QBV:508; QBM:571-574 |
| 25 | ConsentFailed | accepting consent failed | `issue.server` | none while the consent card is up: shown as a line inside it (Q2); otherwise a normal card with OK | QBV:509 |

Actions are Retry, Plans, Open Settings, Support, OK and (Windows, #13 only) Sign in again; there is no "Accept"
action on this card (DES:79-81 differs, §13.1). Server-code → kind mapping (incl. 409 `CONFLICT` handling) is owned by
`api-contract.md` §6.3/§6.4; this table owns how each kind renders. Icons for #4a-4c, #23a-23d: ErrorCircle, Warning
tint.

### 5.12 AI Context section (`AdvancedExpanded`)

```
+------------------------------------------------------------------------+
| [Message|*Reply]                                o AI Context ^     (@) |
|  | Здравствуйте! Сколько стоит Product A и есть ли доставка?        |  |
|  <Friend> <*Client> <Colleague> <Partner>                              |
|  *   How should I reply? You can leave this empty          (mic) [^]   |
|  --------------------------------------------------------------------  |
|  Profile [ My Business v ]  (edit) (+)  Clear context        (scroll)  |
|  | Ответь вежливо и уточни удобное время доставки.                  |  |
|  (clip) Attach document   Excel, CSV, PDF, Word, TXT · up to 8 MB      |
|  [doc] Прайс-лист.xlsx                                          (x)    |
|  Reply length               [ Standard · 400 | Extended · 600 ]        |
|  Documents are processed on this computer. Generation sends only ...   |
+------------------------------------------------------------------------+
  profile editor open (new profile; "Delete profile" only for a saved one):
|  Profile [ No profile v ]  (+)  Clear context                          |
|  +------------------------------------------------------------------+  |
|  | [ Profile name, e.g. My Business                               ] |  |
|  | [ Your name                    ] [ Company                     ] |  |
|  | [ Occupation or job role                                       ] |  |
|  | Profile type [ Personal v ]      [ Industry                    ] |  |
|  | [ Company, work and other relevant details                     ] |  |
|  | [ Communication style, e.g. polite and concise                 ] |  |
|  | [ Products and sales information                               ] |  |
|  | [ Services and customer support                                ] |  |
|  | [ Business policies, delivery and returns                      ] |  |
|  | [ Instructions for this profile                                ] |  |
|  | [Delete profile]                          [ Cancel ] [ Save ]    |  |
|  +------------------------------------------------------------------+  |
```

Section (ACV:64-119): VStack spacing 10, padding 14 sides / 12 bottom, below the input/recording row, disabled while
Generating. Order:
1. Divider.
2. `!DesktopContextAvailable` → `bar.context.server_update` (12, Warning) (ACV:70-74).
3. Scroll viewer, max height 245, content spacing 12, right padding 4 (ACV:75-108):
   1. **Profile row** (ACV:121-143), small controls, spacing 8, disabled while `ContextStorageBusy`:
      combo labelled `bar.context.profile` with `bar.context.none` + profile names (verbatim); changing it sets
      `EditingProfile = null` (unsaved edits dropped, parity; MAC-7 open). Button Edit (profile selected) / Add (none),
      tooltip `bar.context.edit` → begin edit (selected profile's metadata, or a new profile). When a profile is
      selected, extra Add button, tooltip `bar.context.new` → deselect, begin a new profile. Link `bar.context.clear` →
      `ClearContext()`: drop pending import, deselect, `CustomInstructions = ""`, `ReferenceAttachments = []`, close
      editor, `OutputLength = Standard`, `ContextIssue = null`; saved profiles untouched (QBM:465-473; QBT:395-414).
      Spinner while `ContextStorageBusy`.
   2. **Profile editor** (when `EditingProfile != null`; ACV:145-185): small rounded fields, spacing 8, padding 10,
      background radius 9 overlay 3.5 %, disabled while busy. Fields in order (keys = placeholders):
      `bar.context.profile_name` (required, ≤ 80); row [`bar.context.person` | `bar.context.company`];
      `bar.context.occupation`; row [combo `bar.context.purpose` = `bar.context.personal` / `bar.context.business` |
      `bar.context.industry`]; `bar.context.description` (2–3 lines); `bar.context.style`; `bar.context.products`
      (1–2); `bar.context.services` (1–2); `bar.context.policies` (1–2); `bar.context.profile_instructions` (2–3).
      Buttons: `bar.context.delete` (destructive, only for an existing selected profile; deletes at once, parity) …
      `bar.context.cancel` · `bar.context.save` (accent, disabled while the name is blank).
      Save (QBM:418-432): validation CTX:165-172 (name 1–80 chars, each field ≤ 4000 code points, ≤ 8 attachments,
      ≤ 24 profiles); success → select it (if the account did not change meanwhile), close the editor, clear
      `ContextIssue`; failure → `ContextIssue = bar.context.save_failed`.
      **(fix vs macOS, MAC-3)**: the editor holds metadata only. Save re-reads the stored profile by id and replaces
      only name, role and the ten text fields; attachments are never written from the editor, so documents
      added/removed while it was open survive (AUD:185-187; AUDT:49-67). New profile → saved with no attachments.
   3. **Conversation instructions** (ACV:79-86): rounded field 2–3 lines, Alt+Enter newline, placeholder
      `bar.context.instructions`. Counter `N/1000` (verbatim, caption tabular) when `N > 900`, Danger when
      `N > 1000`; `N > 1000` disables submit. Windows counts `N` with `ServerLength` (mac shows `Character` count but
      gates on `ServerLength`, QBM:239) **(quirk fix Q6)**.
      **(fix vs macOS, MAC-4)**: instructions are sent with priority — budget reserved for them first and placed
      before every profile field; profile fields are truncated first (mac appends them to `Preferences`, last, and they
      are cut first: QBM:405-408, CTX:223-228; AUD:189-191; AUDT:69-85). They are never cut silently: if they cannot fit
      the request budget, the counter turns Danger and submit is disabled (budget numbers: open question 4).
   4. **Attachments** (ACV:187-214), spacing 7, rows 12: button Attach `bar.context.attach` (disabled while importing,
      busy, or 4 active attachments) → file dialog filtered to `*.xlsx;*.csv;*.pdf;*.docx;*.txt`, single file, bar held
      open (QBM:475-491). Caption: spinner + `bar.context.importing` while importing, else `bar.context.formats`.
      Rows: DocumentText (TextSecondary), file name (1 line, middle ellipsis), remove DismissCircle, tooltip
      `bar.context.remove_attachment` → profile-owned: removed from the stored profile; else from
      `ReferenceAttachments` (QBM:448-463). Import target = profile selected when the import started, else ephemeral;
      a result arriving after `ClearContext()` is dropped (QBM:493-512). Errors **(fix vs macOS, MAC-6)**: typed →
      specific keys §11.3; unknown → `bar.context.import_failed` (mac uses this one text for all, QBM:509-511).
   5. **Output length** (ACV:88-100), row height 26: label `bar.context.output` (12.5) + segmented, width 300,
      `bar.context.standard` / `bar.context.extended`; disabled when `!DesktopContextAvailable`.
   6. **Privacy note** `bar.context.privacy` (11.5 TextSecondary, wraps) (ACV:101-104).
4. `ContextIssue` line under the scroll viewer (12, Warning) (ACV:109-114).

Profile store: loaded at start, busy until done; load failure → `bar.context.load_failed` (QBM:145-150).
**(fix vs macOS, MAC-5)**: one encrypted store per account, keyed by the signed-in session's user id (available
before `/me`; e.g. `profiles-<userId>.aicontext`, AUD:193-195; AUDT:87-97). The combo lists only that account's
profiles; account change → `ClearContext()` (QBM:514-528, QBT:433-447) + reload (busy); signed out → empty list;
stores stay on disk. Profile and import errors never generate and never spend a reply (QBT:416-431; CTXD:11).

## 6. Transitions

| # | Trigger | Guard | Effect | Source |
|---|---|---|---|---|
| T1 | Hotkey / tray left click / tray Open / second launch | bar hidden | `ShowPanel()` §2.4 | AC:96-106, APP:25-28 |
| T2 | Hotkey | bar visible | hide, restore focus | AC:104-106 |
| T3 | Deactivation | `!HoldsPanelOpen` | hide | QP:152-155 |
| T4 | Esc | §7.2 | cancel voice / cancel generation / hide | QBM:368-382 |
| T5 | Enter in email field | | request code → §5.2 or footer error | QBM:591-593 |
| T6 | Code complete or Enter | AwaitingCode | verify → signed-in bar (+ consent card if needed) | QBM:595-602 |
| T7 | Another email | | §5.1, focus | QBM:608-612 |
| T8 | Accept | consent card | save consent → card gone, focus | QBM:614-624 |
| T9 | Enter in instruction field | `CanSubmit` | Generating | QBM:247-255 |
| T10 | Chip click / Ctrl+1…4 | reply mode, chips visible | select; Generating if `CanSubmit` | QBM:258-271 |
| T11 | Message segment / capsule (x) | | Compose, copied text never offered again | QBM:384-389 |
| T12 | Reply segment | Compose | ManualReply | QBM:391-394 |
| T13 | Mic / Ctrl+D / tray Dictate | signed in, consent, voice Idle | RequestingPermission → Recording or Issue | QBM:536-548, AC:108-111 |
| T14 | Enter / Stop / 40 s | Recording | Transcribing | QBV:320, VC:242-248 |
| T15 | Transcript | | draft appended, focus; empty → NothingHeard | QBM:566-577 |
| T16 | Generation success / failure | | Result / Issue | QBM:327-348 |
| T17 | Ctrl+Enter / Copy | Result card visible | §9 | QBM:354-366 |
| T18 | Ctrl+R | Result card visible, not Generating | Generating (new key) | QBM:273-282 |
| T19 | Issue action | | Retry / Plans / Settings / Support / OK | QBV:513-530 |
| T20 | AI Context toggle | not Generating | expand / collapse | ACV:35 |
| T21 | Account button | not Generating | Settings → Account tab, bar hidden | AC:127-130 |
| T22 | Server ends session | | sign-in row; `issue.signed_out` if it surfaced on a request | AC:159-164 |
| T23 | Account id changes | | `ClearContext()`, reload profiles (MAC-5) | QBM:514-528 |
| T24 | Open after > 600 s | not Generating | `ResetConversation()` | QBM:161-163 |
| T25 | Fresh balance not exhausted | Issue is LimitReached | issue cleared | QBM:178-184, QBT:241-254 |
| T26 | New copied text on open | `OfferCopiedText` | Reply(text); result cleared if shown | QBM:164-168 |

## 7. Keyboard and focus

### 7.1 Shortcut map

| mac | Windows | Active when | Action | Source |
|---|---|---|---|---|
| Return | Enter | instruction field focused | `Submit()` | QBV:135 |
| ⌥Return | Alt+Enter | any multi-line field (instruction, manual source, conversation instructions, profile multi-line fields) | newline | DES:118 |
| Return | Enter | email / code field | `SubmitEmail()` / `SubmitCode()` | QBV:586, 598 |
| Return | Enter | voice Recording | stop and transcribe | QBV:320 |
| ⌘Return | Ctrl+Enter | result card visible | `CopyResult()` | QBV:410 |
| ⌘R | Ctrl+R | result card visible, not Generating | `Regenerate()` | QBV:401-402 |
| ⌘D | Ctrl+D | mic visible (voice available, input row shown), not Generating | `ToggleVoice()` (start only; while recording use Enter/Esc) | QBV:141-145 |
| ⌘1…⌘4 | Ctrl+1…Ctrl+4 (top row and numpad) | chips visible and enabled | `ReplyTo(friend/client/work/business)` | QBV:249 |
| Esc | Esc | bar | §7.2 | QBM:368-382, QP:32-34 |
| ⌘X/C/V/A/Z, ⇧⌘Z | Ctrl+X/C/V/A/Z, Ctrl+Y, Ctrl+Shift+Z | text fields | editing | QP:36-53 |
| ⌥Space (global) | Alt+Space (global) | anywhere | `TogglePanel()` | AC:67, AS:46 |

All matching by virtual key (works on ru/kk layouts). Enter during an IME composition commits the composition and does
not submit.

### 7.2 Esc order (QBM:368-382; QBT:279-290)

1. An open dropdown/popup/file dialog handles Esc itself.
2. Voice RequestingPermission, Recording or Transcribing → `voice.Cancel()` (stop; nothing else).
3. Phase Generating → `CancelGeneration()`.
4. Otherwise → `Hide(restoreFocus: true)` — also with an issue card, an open editor or the AI Context section.

### 7.3 Focus rules

- F1 Show: signed in → instruction field; signed out → email or code field (QBV:150, 608). Caret at the end, no
  selection (DES:65; Q4).
- F2 `FocusRequested` (QBM:169, 388, 393, 552, 577, 599, 611, 619 — open, dismiss copied, Reply segment, cancel voice,
  transcript inserted, code accepted, another email, consent accepted) → same target as F1 (QBV:151, 609).
- F3 The sign-in field also refocuses on every auth-state change (QBV:610).
- F4 Recording/Transcribing: no text field; Enter/Esc handled at window level.
- F5 (Win) When Generating ends (success, failure, cancel) → refocus the instruction field (a disabled TextBox drops
  focus on Windows).
- F6 Shortcuts in §7.1 work from any focused element inside the bar.
- F7 Tab order follows the visual order: bar card top→bottom, left→right, then the secondary card.

## 8. Limits and counters

| Item | Limit | Metric | UI when exceeded | Source |
|---|---|---|---|---|
| Instruction (draft) | `max_instruction_length`, default 400 | `ServerLength` | Compose: counter from > 3/4·L, Danger > L, Send disabled. Reply: Send/chips disabled, no counter (parity; open question 13) | QBM:221, 240-241; QBV:138-140 |
| Source (copied/manual) | `max_source_characters`, default 400; > 0 | `ServerLength` | `copied.too_long %lld %lld`, chips hidden, Send disabled; never truncated | QBM:220, 226, 240; DES:52-53 |
| Conversation instructions | 1000 (MAC-4 effective limit) | `ServerLength` | counter from > 900, Danger > 1000, submit disabled | QBM:239; ACV:82-86 |
| Profile | name 1–80; fields ≤ 4000 code points; ≤ 8 attachments; ≤ 24 profiles; store ≤ 32 MiB | | Save disabled (blank name) / `bar.context.save_failed` / storage keys §11.3 | CTX:108-114, 149, 165-172; ACV:176 |
| Active attachments | 4 | count | Attach disabled | QBM:476, 494; ACV:192 |
| Document | file 8 MiB; expanded 16 MiB; 2,048 entries; 2,000,000 chars; 50,000 rows; 128 sheets; 256 columns | | typed import error §11.3 | DCI:7-21; CTXD:19-29 |
| Context payload | profile ≤ 1500 chars; ≤ 8 references, ≤ 4000 chars total; ≤ 4096 bytes | | client validation → issue #4a, no request (mac: #4 with Retry) | GEN:180-187; BE-mac:13-18; `api-contract.md` §14.4 |
| Voice | 40 s max (auto-stop), 0.5 s min | | auto-stop / #17 | VC:91, 116 |
| OTP code | `code_length` ASCII digits (default 4) | | input filtered | QBV:587-591 |
| Email | contains `@`, ≤ 254 chars | | `signin.error.email` | AUTH:85-88 |

## 9. Copy, close-after-copy, paste-after-copy

1. `CopyResult()` (QBM:354-366): text = `Result` trimmed; empty → nothing. **(quirk fix Q3)**: ignored while
   `JustCopied` (mac schedules a second hide+paste, pasting twice).
2. Write `CF_UNICODETEXT`; remember the new clipboard sequence number as our own so it is never offered back
   (CLIP:140-144, WARCH:126-131).
3. `JustCopied = true` (button `result.copied` + Checkmark); `lastActivity = now`.
4. After 700 ms: `JustCopied = false`; `AfterCopy()` (QBM:360-365).
5. `AfterCopy()` (AC:134-142): `!(CloseAfterCopy || PasteAfterCopy)` → stay open. Else `Hide(restoreFocus: true)`.
   If `PasteAfterCopy` and paste is available for the remembered window: wait until it is foreground (12 × 50 ms, then
   give up) + 50 ms (PST:50-51, 80-96) → `SendInput` Ctrl down, V down, V up, Ctrl up (virtual key `V`, layout
   independent). Unavailable (no remembered window, window gone, target elevated — UIPI, WARCH:51-52, 158-164) → copy
   only, silently.

## 10. Settings window

### 10.1 Window

Normal top-level window: title = selected tab label (SW:13); content width 520, height to content, not resizable,
close button only (SW:15, 27); taskbar button and Alt+Tab entry; centred on the monitor under the cursor every time it
is presented (mac `center()` each time, SW:36-42); one instance reused (AC:194-198); opening hides the bar first;
brought to front and activated. Tabs (SW:22-25): General (Settings icon) `settings.general`, Voice (Mic)
`settings.voice`, Account (PersonCircle) `settings.account`, About (Info) `settings.about`. Account button in the bar
selects tab index 2 (SW:44-46). Grouped form layout (sections) (SW:120, 146, 176, 209).

### 10.2 General tab (SW:68-123)

```
+-[ General ]-[ Voice ]-[ Account ]-[ About ]---------------------+
| Shortcut                                        [ Alt+Space ]   |
|   Alt+Space is taken, so Ctrl+Alt+Space is used   (note)        |
| Start with Windows                                       [on]   |
|-----------------------------------------------------------------|
| Bar position                                    [ Top      v ]  |
| Appearance                                      [ System   v ]  |
|-----------------------------------------------------------------|
| Show the copied message                                  [on]   |
| Hide the bar after copying                               [on]   |
| Paste the reply automatically                           [off]   |
|   AI Reply will press Ctrl+V in the window you were using. ...  |
+-----------------------------------------------------------------+
```

| Section | Control | Key(s) | Binding / behaviour | Source |
|---|---|---|---|---|
| 1 | Shortcut recorder | label `settings.shortcut`; while recording `settings.shortcut.recording` | button shows current gesture or `—`; click → recording; Esc cancels; focus loss cancels; modifier-only keys wait; disallowed gesture (§1.2.5) → `MessageBeep`, keep recording; allowed → `ChangeShortcut` (§1.2.4). (Win) unregister the global hotkey while recording and re-register on cancel (`RegisterHotKey` would swallow the keystroke) **(quirk fix Q9)** | SW:76-78, 214-277 |
| 1 | Shortcut note | `settings.shortcut.fallback %@ %@` / `settings.shortcut.taken %@` | caption, Warning, only when a note exists | SW:79-81 |
| 1 | Start at sign-in | `settings.login_item` | on = state ≠ Disabled; toggling writes/removes `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` value `AI Reply` = `"<exe>" --background` (WARCH:54-55); then re-read. Turning on when the value already exists is a no-op (mac `requiresApproval`, PST:173-185) | SW:82-87 |
| 1 | Disabled-by-user note | `settings.login_item.approval` + link `settings.open_system_settings` | shown when the Run value exists but `HKCU\…\Explorer\StartupApproved\Run` marks it disabled (first byte odd); link → `ms-settings:startupapps` | SW:88-93, PST:157-171 |
| 2 | Placement | `settings.placement`: `settings.placement.top` / `settings.placement.bottom` / `settings.placement.remembered` | `Placement`; used on next show | SW:96-100 |
| 2 | Appearance | `settings.appearance`: `settings.appearance.system` / `settings.appearance.light` / `settings.appearance.dark` | `Theme`; applied at once to all windows (Avalonia `RequestedThemeVariant` Default/Light/Dark) | SW:101-106, AC:238-244 |
| 3 | Show copied message | `settings.offer_copied` | `OfferCopiedText` | SW:109 |
| 3 | Hide after copying | `settings.close_after_copy` | `CloseAfterCopy` | SW:110 |
| 3 | Paste automatically | `settings.paste_after_copy` | `PasteAfterCopy`; mac asks for Accessibility on enable (AC:246-248) — Windows needs no permission | SW:111-116 |
| 3 | Footer | `settings.paste_after_copy.footer` | caption, TextSecondary (Windows wording §11.2) | SW:117 |

Run-key path is rewritten with the current exe path at each start while enabled (Win).

### 10.3 Voice tab (SW:125-148)

| Control | Key(s) | Behaviour |
|---|---|---|
| Dictation language | `settings.dictation_language`: `language.auto`, `Русский`, `Қазақша`, `English` (names verbatim, not localized) | `DictationLanguage` Auto/Ru/Kk/En |
| Footer | `settings.voice.footer` | caption (Windows wording §11.2) |
| Microphone row | label `settings.microphone`, button `settings.open_system_settings` | opens `ms-settings:privacy-microphone` (no status shown, parity; DES:110-111 differs) |

### 10.4 Account tab (SW:150-179)

Signed in — section 1: `settings.account.email` = email ?? `—`; when balance known: `settings.account.plan` = plan name,
`settings.account.balance` = balance caption (§5.10). Section 2 buttons: `settings.account.refresh` →
`RefreshAccount(force: true)`; `settings.account.plans` → `OpenPlans()`; `settings.account.sign_out` (destructive, no
confirmation) → `POST /api/v1/auth/logout {refresh_token}` (timeout 10 s, errors ignored) then local sign-out
(AUTH:155-164); `settings.account.delete` → deletion URL. Signed out: `settings.account.signed_out` (TextSecondary) +
button `menu.sign_in` → `ShowPanel()`. On tab appear: `RefreshAccount()` (10 s throttle).

### 10.5 About tab (SW:181-212)

App icon 56 × 56; `AI Reply` (verbatim, title, semibold); `settings.about.version %@ %@` (version, build),
TextSecondary. When `config.legal` exists: links `consent.privacy`, `consent.terms`, `settings.about.support` (each
only with its URL). Button `settings.about.copy_diagnostics` → copies the text below; label becomes
`settings.about.copied` until the window is reopened **(quirk fix Q8)** (mac keeps it until restart). On appear:
`RefreshConfig()` (300 s cache). Diagnostics (AC:255-265), never user text:

```
AI Reply <version> (<build>)
Windows <os version>, <device model>
Server: <host><path>
Signed in: yes|no, consent: yes|no|-
Voice: on|off, shortcut: <display string>
Last problem: <issue case name>|none
```

## 11. Localization

### 11.1 Keys used, by surface (values verbatim from XCS unless §11.2)

- Sign-in: `signin.email.placeholder`, `signin.hint`, `signin.code.placeholder %@`, `signin.code.hint`, `signin.resend`,
  `signin.resend.wait %lld`, `signin.another_email`, `signin.error.email`, `signin.error.code`, `signin.error.expired`,
  `signin.error.used`, `signin.error.attempts`, `signin.error.cooldown %lld`, `signin.error.delivery`.
- Toolbar: `bar.mode`, `bar.mode.message`, `bar.mode.reply`, `bar.context.title`, `bar.context.expanded`,
  `bar.context.collapsed`, `settings.account`.
- Input: `bar.placeholder.compose`, `bar.placeholder.reply`, `bar.field.accessibility`, `bar.counter %lld %lld`,
  `bar.mic`, `bar.send`, `bar.send.help`, `bar.reply.source`.
- Copied/recipients: `copied.accessibility %@`, `copied.dismiss`, `copied.too_long %lld %lld`, `recipient.friend`,
  `recipient.client`, `recipient.colleague`, `recipient.partner`, `recipient.help`.
- Voice: `voice.recording`, `voice.cancel`, `voice.stop`, `voice.transcribing`.
- Result: `result.regenerate`, `result.copy`, `result.copied`, `result.copy.help`, `balance.unlimited %@`,
  `balance.empty`, `balance.remaining %lld`.
- Issues: `issue.limit.daily %@`, `issue.limit.empty`, `issue.source_too_long %lld`, `issue.instruction_too_long %lld`,
  `issue.instruction_missing`, `issue.consent`, `issue.account_disabled`, `issue.pro_device_limit`,
  `issue.pro_device_identity`, `issue.pro_device_revoked`, `issue.rate_limited`, `issue.ai_unavailable`,
  `issue.offline`, `issue.signed_out`, `issue.server`, `voice.issue.permission`, `voice.issue.no_device`,
  `voice.issue.too_short`, `voice.issue.unavailable`, `voice.issue.limit`, `voice.issue.recording_failed`,
  `voice.issue.server`, `voice.issue.nothing_heard`, `issue.action.plans`, `issue.action.retry`,
  `issue.action.settings`, `issue.action.support`, `issue.action.ok`.
- Consent: `consent.title`, `consent.body %@`, `consent.privacy`, `consent.terms`, `consent.accept`.
- AI Context: `bar.context.server_update`, `bar.context.profile`, `bar.context.none`, `bar.context.edit`,
  `bar.context.new`, `bar.context.clear`, `bar.context.profile_name`, `bar.context.person`, `bar.context.company`,
  `bar.context.occupation`, `bar.context.purpose`, `bar.context.personal`, `bar.context.business`,
  `bar.context.industry`, `bar.context.description`, `bar.context.style`, `bar.context.products`,
  `bar.context.services`, `bar.context.policies`, `bar.context.profile_instructions`, `bar.context.delete`,
  `bar.context.cancel`, `bar.context.save`, `bar.context.instructions`, `bar.context.attach`, `bar.context.importing`,
  `bar.context.formats`, `bar.context.remove_attachment`, `bar.context.output`, `bar.context.standard`,
  `bar.context.extended`, `bar.context.privacy`, `bar.context.load_failed`, `bar.context.save_failed`,
  `bar.context.import_failed`.
- Tray: `menu.open`, `menu.dictate`, `menu.account`, `menu.balance.unlimited`, `menu.balance.remaining %lld`,
  `menu.sign_in`, `menu.plans`, `menu.settings`, `menu.updates`, `menu.quit`.
- Settings: `settings.general`, `settings.voice`, `settings.about`, `settings.shortcut`, `settings.shortcut.recording`,
  `settings.shortcut.fallback %@ %@`, `settings.shortcut.taken %@`, `settings.login_item`,
  `settings.login_item.approval`, `settings.open_system_settings`, `settings.placement`, `settings.placement.top`,
  `settings.placement.bottom`, `settings.placement.remembered`, `settings.appearance`, `settings.appearance.system`,
  `settings.appearance.light`, `settings.appearance.dark`, `settings.offer_copied`, `settings.close_after_copy`,
  `settings.paste_after_copy`, `settings.paste_after_copy.footer`, `settings.dictation_language`, `language.auto`,
  `settings.voice.footer`, `settings.microphone`, `settings.account.email`, `settings.account.plan`,
  `settings.account.balance`, `settings.account.refresh`, `settings.account.plans`, `settings.account.sign_out`,
  `settings.account.delete`, `settings.account.signed_out`, `settings.about.version %@ %@`, `settings.about.support`,
  `settings.about.copy_diagnostics`, `settings.about.copied`.
- Verbatim, not localized: `AI Reply`, `Русский`, `Қазақша`, `English`, `—`, counters `N/L`, `N/1000`, timer.

### 11.2 Windows value overrides (same keys; mac values mention ⌘, Return, Mac, Dock, macOS, System Settings)

| Key | en | ru | kk |
|---|---|---|---|
| `bar.mic` | Dictate (Ctrl+D) | Диктовка (Ctrl+D) | Дауыспен енгізу (Ctrl+D) |
| `bar.send.help` | Write (Enter) | Написать (Enter) | Жазу (Enter) |
| `recipient.help` | Write the reply now (Ctrl+1–Ctrl+4) | Написать ответ сразу (Ctrl+1–Ctrl+4) | Жауапты бірден жазу (Ctrl+1–Ctrl+4) |
| `result.copy.help` | Copy the reply (Ctrl+Enter), then paste it with Ctrl+V | Скопировать ответ (Ctrl+Enter), затем вставьте его Ctrl+V | Жауапты көшіру (Ctrl+Enter), содан кейін Ctrl+V арқылы қойыңыз |
| `voice.stop` | Done (Enter) | Готово (Enter) | Дайын (Enter) |
| `bar.context.importing` | Processing on this computer… | Обрабатываю на компьютере… | Компьютерде өңделуде… |
| `bar.context.privacy` | Documents are processed on this computer. Generation sends only matching excerpts and the selected profile. Review prices and other important details before sending. | Документы обрабатываются на компьютере. При генерации отправляются только подходящие фрагменты и выбранный профиль. Проверяйте цены и другие важные сведения перед отправкой. | Құжаттар осы компьютерде өңделеді. Генерация кезінде тек сәйкес үзінділер мен таңдалған профиль жіберіледі. Жібермес бұрын бағалар мен басқа маңызды деректерді тексеріңіз. |
| `bar.context.load_failed` | Saved profiles could not be opened. Restart AI Reply and try again. | Не удалось открыть сохранённые профили. Перезапустите AI Reply и попробуйте снова. | Сақталған профильдерді ашу мүмкін болмады. AI Reply-ды қайта іске қосып, қайталап көріңіз. |
| `settings.placement.bottom` | Bottom, above the taskbar | Снизу, над панелью задач | Төменде, тапсырмалар тақтасының үстінде |
| `settings.paste_after_copy.footer` | AI Reply will press Ctrl+V in the window you were using. Apps running as administrator can't receive it — the reply stays copied. | AI Reply сам нажмёт Ctrl+V в окне, где вы были. В программы, запущенные от имени администратора, вставить нельзя — ответ останется скопированным. | AI Reply сіз болған терезеде Ctrl+V басады. Әкімші ретінде іске қосылған бағдарламаларға қою мүмкін емес — жауап көшірілген күйде қалады. |
| `settings.voice.footer` | Audio is sent to the AI Reply server and recognized by OpenAI. The recording on this computer is deleted right after. | Запись отправляется на сервер AI Reply и распознаётся OpenAI. На этом компьютере она удаляется сразу после распознавания. | Жазба AI Reply серверіне жіберіліп, OpenAI арқылы танылады. Осы компьютердегі жазба танылған соң бірден өшіріледі. |
| `voice.issue.permission` | No microphone access. Allow it in Windows Settings | Нет доступа к микрофону. Разрешите его в параметрах Windows | Микрофонға рұқсат жоқ. Windows параметрлерінде рұқсат беріңіз |
| `settings.login_item` | Start with Windows | Запускать вместе с Windows | Windows-пен бірге іске қосу |
| `settings.login_item.approval` | Turned off in Windows Settings › Apps › Startup | Отключено в параметрах Windows › Приложения › Автозагрузка | Windows параметрлерінде (Қолданбалар › Іске қосу) өшірілген |
| `settings.open_system_settings` | Open Windows Settings | Открыть параметры Windows | Windows параметрлерін ашу |
| `menu.quit` | Exit AI Reply | Выйти из AI Reply | AI Reply-дан шығу |

kk values above are proposals — native review required (open question 14).

### 11.3 New keys **(fix vs macOS, MAC-6)**

Typed import/storage errors replace the single `bar.context.import_failed` (kept as fallback). Mapping from mac
`DocumentImportError` (DCI:23-24) and `ContextStorageError` (CTX:52-54):

| Key | Cause | en | ru | kk (proposal) |
|---|---|---|---|---|
| `bar.context.import.unsupported` | extension not xlsx/csv/pdf/docx/txt (`unsupportedFormat`) | This file type isn't supported. Use Excel (.xlsx), CSV, PDF, Word (.docx) or TXT. | Этот тип файла не поддерживается. Используйте Excel (.xlsx), CSV, PDF, Word (.docx) или TXT. | Бұл файл түріне қолдау жоқ. Excel (.xlsx), CSV, PDF, Word (.docx) не TXT пайдаланыңыз. |
| `bar.context.import.file_too_large` | file > 8 MiB (`tooLarge`, size check) | The file is larger than 8 MB. | Файл больше 8 МБ. | Файл 8 МБ-тан үлкен. |
| `bar.context.import.too_much_text` | > 2,000,000 chars / 50,000 rows / 128 sheets / 256 columns / 16 MiB expanded (`tooLarge`, content) | The document has too much text. Split it into smaller files. | В документе слишком много текста. Разделите его на файлы поменьше. | Құжатта мәтін тым көп. Оны кішірек файлдарға бөліңіз. |
| `bar.context.import.protected` | encrypted PDF or password-protected Office file (mac: `unreadable`) | The document is password-protected. Remove the password and try again. | Документ защищён паролем. Снимите пароль и попробуйте снова. | Құжат құпиясөзбен қорғалған. Құпиясөзді алып тастап, қайталап көріңіз. |
| `bar.context.import.no_text` | nothing readable extracted (`noReadableText`) | No text found. Scanned PDFs need a text layer. | Текст не найден. Для сканов PDF нужен текстовый слой. | Мәтін табылмады. Сканерленген PDF үшін мәтін қабаты қажет. |
| `bar.context.import.malformed` | broken file (`malformed`) | The file is damaged or isn't a valid document. | Файл повреждён или не является документом. | Файл зақымдалған немесе жарамды құжат емес. |
| `bar.context.import.unsafe` | archive bomb, external parts (`unsafeArchive`) | The file's internal structure isn't safe, so it wasn't opened. | Внутренняя структура файла небезопасна, поэтому он не открыт. | Файлдың ішкі құрылымы қауіпсіз емес, сондықтан ол ашылмады. |
| `bar.context.import.unreadable` | I/O, sharing violation, bad encoding, not a regular file (`unreadable`) | The file couldn't be opened. Make sure it isn't open in another program. | Не удалось открыть файл. Проверьте, что он не открыт в другой программе. | Файлды ашу мүмкін болмады. Ол басқа бағдарламада ашық емес екенін тексеріңіз. |
| `bar.context.import.storage_full` | > 8 attachments in the profile or store > 32 MiB (`invalidProfile` / `tooLarge` on save) | Not enough room in saved profiles (up to 8 documents per profile, 32 MB in total). Remove a document and try again. | В сохранённых профилях не хватает места (до 8 документов в профиле, всего 32 МБ). Удалите документ и попробуйте снова. | Сақталған профильдерде орын жетпейді (бір профильге 8 құжатқа дейін, барлығы 32 МБ). Бір құжатты өшіріп, қайталап көріңіз. |
| `bar.context.import.storage_unavailable` | protected store/key unavailable (`invalidStorage`) | Saved profiles are unavailable right now. Restart AI Reply and try again. | Сохранённые профили сейчас недоступны. Перезапустите AI Reply и попробуйте снова. | Сақталған профильдер қазір қолжетімсіз. AI Reply-ды қайта іске қосып, қайталап көріңіз. |

Also rendered by the bar, defined (with ru/en values) in the sibling `api-contract.md` §6.3: `issue.context_too_large`,
`issue.context_unavailable`, `issue.output_too_long`, `issue.action.sign_in_again` (§5.11 #4a-4c, #13).

### 11.4 Keys not used on Windows

`bar.template`, `bar.template.friend`, `bar.template.business`, `bar.template.customer`, `bar.template.custom`
(template menu removed, MAC-2); `settings.shortcut.system %@` (no Windows source for "used by the system"). Remove them
from the Windows resources so a catalog test (port of LOC:51-74: every used key in ru/kk/en, same placeholder count, no
unused entries) passes.

## 12. Acceptance tests

Port QBT 1:1 onto the Windows view model with fakes (generator, clipboard, voice, backend stub):
`composeShowsTheResult` (167-181), `nothingToSendWhenEmptyOrTooLong` (183-190), `copiedMessageRepliesWithOneClick`
(192-203), `tooLongCopiedMessageIsNotSent` (205-212), `retryReusesTheKeyAndRegenerateDoesNot` (214-230),
`limitReachedAsksForAFreshBalance` (232-239), `aWebsitePurchaseClearsTheLimitCardOnTheNextOpen` (241-254),
`endedSessionSignsOut` (256-263), `copyPutsTheReplyOnThePasteboardThenCloses` (265-277),
`escapeCancelsGenerationThenCloses` (279-290), `consentGatesGeneration` (292-297),
`draftExpiresAfterTenMinutesClosed` (299-311), `dismissingTheCopiedMessageReturnsToCompose` (313-318),
`voiceUnavailableWhenTheServerSaysSo` (320-323), `advancedContextIsOptionalAndRequiresServerCapability` (342-357),
`profilesAndMatchingReferencesAreSnapshottedForRetries` (359-393), `importingAndRemovingAReferenceNeverGenerate`
(416-431), `accountSwitchClearsActivePrivateContext` (433-447), transcriber hints and error mapping (457-476).
Adapted: `manualReplyAndTemplateSelectionDoNotGenerateUntilSubmit` (325-340) → manual reply; `ReplyTo(business)` with
an empty source only selects (no call; the chip itself is disabled in the UI); source typed + Enter → one reply with
`business`; `customTemplatePersistsSelectedProfileAndCanClearAttachments` (395-414) → save a profile without any
template, `ClearContext()` keeps saved profiles.

Must pass on Windows (fail on mac, AUDT): `plainComposeSendsNoMacOnlyFields` (no `output_char_limit`, `context`,
`template_id`), `editorSaveKeepsDocumentImportedWhileEditing`, `conversationInstructionsSurviveAFilledProfile`,
`savedProfilesAreIsolatedPerAccount`; replaces `customTemplateIsNotColleague`: compose never sends `template_id`,
reply sends the chip's value.

Windows-specific:
1. Each typed import error shows its §11.3 key; no generation call.
2. `PRO_DEVICE_*` on `/ai/compose` and `/ai/transcribe` → §5.11 #12-14 / #23a; still signed in, tokens intact.
3. Request order after sign-in: verify → `POST /api/v1/installations` → first `/ai/*` call; registration failure does
   not block the AI call.
4. Placement (§2.3 example) at 100 % and 150 %; drag across the midline keeps the card in place on reopen (Q1).
5. Two Ctrl+Enter within 700 ms with paste on → one paste (Q3).
6. Snapshot render of every §5 state in ru/kk/en, light/dark, with real strings (mac snapshots show keys, AUD MAC-13).

## 13. macOS discrepancies and quirks

### 13.1 DESIGN.md vs code (code wins)

| Topic | DESIGN.md | Code |
|---|---|---|
| Bar row height | 56 (DES:30) | 60 = 14 + 32 + 14 (QBV:148-149, 205) |
| Brand icon | app icon 22, radius 5 (DES:35) | vector glyph in Accent, 22 (QBV:110-119, SUP:36-66) |
| Mic button | 28 (DES:38) | 30 (QBV:178) |
| Waveform bars | 2 wide (DES:61) | width from free space, min 2 (QBV:342-350) |
| Red dot | pulsing (DES:62) | static, opacity 1 / 0.4 (QBV:301-304) |
| Transcribing | shimmer (DES:64) | none (`shimmer` unused, QBV:361) |
| Result text | editable in place (DES:72) | read-only, selectable (QBV:385-388) |
| Copied feedback | 0.8 s (DES:76-77) | 0.7 s (QBM:361) |
| Balance caption | 4 example forms (DES:73-74) | 3 forms (QBV:418-436) |
| Issue actions | incl. "Войти", "Принимаю" (DES:79-81) | Plans / Retry / Open Settings / Support / OK (QBV:513-530) |
| Recipient choice in bar | none; compose takes the instruction only (DES:32-33) | template menu in both modes (ACV:19-33) → removed on Windows (MAC-2) |
| Menu bar icon | `ellipsis.bubble` (DES:92) | brand glyph (SIC:35, 76) |
| Voice settings | mic access status (DES:110-111) | button only (SW:140-144) |
| Account settings | no refresh (DES:112) | + `settings.account.refresh` (SW:164) |
| Settings container | SwiftUI `Settings` scene (DES:106) | `NSTabViewController` toolbar tabs (SW:9-31) |
| Min width | 560 (DES:21) | fixed 680 (SUP:9) |

### 13.2 Quirk fixes on Windows (owner may veto)

| Id | mac behaviour | Windows |
|---|---|---|
| Q1 | Remembered anchor not recomputed after a drag (QP:107, 162) → jump by card height, wrong growth | recompute on drop (§2.3.7) |
| Q2 | Consent failure hidden behind the consent card (QBV:90-95; QBM:620-622) | inline `issue.server` in the card |
| Q3 | Second copy within 700 ms → second hide + paste (QBM:354-366; AC:134-142) | ignore copy while `JustCopied` |
| Q4 | Focusing a field selects its text (AppKit field editor); brief wants caret at end (DES:65) | caret at end, no selection |
| Q5 | Limit card opens plain `/payment` (QBV:517; AC:116), tray uses the account handoff (AC:180-186) | handoff for both |
| Q6 | Instructions counter counts `Character`s, gate counts code points (ACV:82-86; QBM:239) | `ServerLength` for both |
| Q7 | Oversized content can push the bar above the work area (QP:138-139) | keep bar inside, shrink secondary scroll |
| Q8 | "Copied" in About never resets (SW:183, 203-206) | reset on reopen |
| Q9 | Recording the currently registered hotkey triggers it instead | unregister while recording |

### 13.3 Audit items outside the fix list — Windows keeps mac behaviour

MAC-7 no confirmation for delete / profile switch drops edits (ACV:129, 170); MAC-8 no "what was sent" evidence in the
result; MAC-9 instructions and length survive copy and the 10-minute reset (QBM:200-209); MAC-11 icon buttons without
names, fixed widths 210/300, "AI Context" untranslated in ru/kk (XCS); MAC-12 density; MAC-14/15 storage robustness.
MAC-10 (context/output errors → generic Retry, GEN:56-64) is fixed on Windows by the sibling `api-contract.md` §6.3
(issues #4a-4c). Backend CTX-B1…B7 apply to Windows too (CTX-B7: platform gate).

## 14. Open questions

1. Framework: the task says Avalonia 12; WARCH:8-14 says WinUI 3 + H.NotifyIcon + CsWin32. Which is authoritative?
   (This spec is framework-neutral.)
2. Backend accepts `context`/`output_char_limit`/compose `template_id` only for session platform `macos` (BE-mac:37-39,
   AUD CTX-B7). This spec hides the AI Context toggle until the server accepts `windows`; confirm the capability flag
   (proposed `features.windows_context_generation`, `api-contract.md` §15.1).
3. Compose recipient: this spec sends no compose `template_id` (MAC-2 removes its only source; mobile compose has
   none). Add a recipient row inside AI Context for compose, sent only when context is active?
4. MAC-4 numbers: effective limit for prioritized conversation instructions vs the 1000-byte profile cap and the
   4096-byte prompt cap (CTX:226-228, CTX-B1). To be fixed in the context/request spec; UI shows the result.
5. "Check for updates": the website has `/mac` only (BE-web:116); add `/windows` or another URL?
6. Keep mac parity for MAC-7, MAC-9, MAC-11 on Windows, or fix them now?
7. Approve or veto quirk fixes Q1–Q9 (§13.2).
8. Third hotkey fallback (mac ⌃⌥⌘Space) — WARCH lists only Ctrl+Alt+Space.
9. Interactive launch shows the bar (§1.1.3, Windows-only) — confirm.
10. Sign-out: keep the account's profiles on disk silently (this spec) or ask "keep / delete" (AUD:195)?
11. Shift+Enter as an extra newline key (Windows chat convention) besides Alt+Enter?
12. Reply segment with an empty manual source focuses the instruction field (parity) — focus the source field instead?
13. Reply mode shows no instruction counter although the 400 limit applies (QBV:138) — show it?
14. kk wording of §11.2 overrides and §11.3 new keys needs a native speaker.
