# AI Reply for Windows — OS integration services

Part of `AirReply-Windows/Docs/spec/`. Covers: global hotkey (§2), clipboard monitor (§3), foreground tracking, focus
restore and automatic paste (§4), keyboard input language (§5), start with Windows (§6), single instance (§7), secret
storage with DPAPI incl. the installation id (§8), strings these services need (§9), P/Invoke (§10), open questions
(§11).
Target: .NET 10, Avalonia 12, Win32 through `LibraryImport`, x64 + ARM64, Windows 10 2004 (10.0.19041) and later.
Source snapshot: `main` @ `0200bc8` (2026-11-03).
Out of scope: bar layout, states and the copy-button UI (QB), HTTP, auth, billing and the network side of PRO-1 (API),
AI context (MAC-1…MAC-4 and MAC-6 do not touch these services), voice capture.

## 0. Conventions

### 0.1 Source aliases

| Alias | Path (macOS Swift package = `AirReply-macOS/Packages/AirReplyKit`) |
|---|---|
| HK / KC / KL / CLIP / PST | `Sources/AirReplySystem/{HotKeyCenter,KeyCombo,KeyboardLayout,ClipboardMonitor,PasteService}.swift` |
| HKT / KCT / CLIPT / PSTT | `Tests/AirReplySystemTests/{HotKeyCenter,KeyCombo,ClipboardMonitor,PasteService}Tests.swift` |
| AC / QP / QBM / SW / AS / SUP / SIC | `Sources/AirReplyUI/{AppCoordinator,QuickPanel,QuickBarModel,SettingsWindow,AppSettings,Support,StatusItemController}.swift` |
| GEN / SEC / SES / CFG / TM / CTX | `Sources/AirReplyCore/{Generator,SecureStore,SessionStore,AppConfiguration,TextMetrics,ContextProfiles}.swift` |
| AGT / QBT | `Tests/AirReplyCoreTests/AccountAndGenerationTests.swift` / `Tests/AirReplyUITests/QuickBarModelTests.swift` |
| APP / XCS / DES | `AirReply-macOS/AirReply/AirReplyApp.swift` / `AirReply-macOS/AirReply/Localizable.xcstrings` / `AirReply-macOS/Docs/DESIGN.md` |
| WARCH | `AirReply-Windows/Docs/ARCHITECTURE.md` |
| QB / API | `AirReply-Windows/Docs/spec/quickbar-ui.md` / `AirReply-Windows/Docs/spec/api-contract.md` |
| AUD | `docs/audits/2026-11-03-backend-macos-audit.md` |
| B: | `ai-reply-back-end/` |
| MSDN | Microsoft Learn pages of the named API (RegisterHotKey, Clipboard Formats, SetForegroundWindow, SendInput) |

### 0.2 Tags

- **MUST** = parity with the macOS code (code wins over DES where they differ, e.g. copy feedback 700 ms in
  QBM:361 vs "0.8 s" in DES:76-77).
- **(Win)** = Windows adaptation without a macOS equivalent.
- **(fix vs macOS, ID)** = audit defect corrected on Windows.
- **(decision)** = choice made in this spec; owner may veto (§11.1).
- **(align)** = differs from QB, API or WARCH; reconcile (§11.2).

### 0.3 Map

| Concern | macOS | Windows |
|---|---|---|
| Global hotkey | Carbon `RegisterEventHotKey`, `kEventHotKeyExclusive`, signature `'AIRp'` (HK) | `RegisterHotKey(… \| MOD_NOREPEAT)` on a message-only window, `WM_HOTKEY`, generation id |
| Default / fallbacks | ⌥Space → ⌃⌥Space → ⌃⌥⌘Space (AC:217-236) | Alt+Space → Ctrl+Alt+Space → Ctrl+Alt+Shift+Space (§2.4) |
| "Used by the system" | `CopySymbolicHotKeys` (KC:40-49) | optional `ImmGetHotKey` probe (§2.6) |
| Clipboard change dating | 1 s timer on `changeCount` (CLIP:75-90) | `AddClipboardFormatListener` → `WM_CLIPBOARDUPDATE` + `GetClipboardSequenceNumber` |
| Concealed items | nspasteboard.org types + 1Password (CLIP:45-52) | `ExcludeClipboardContentFromMonitorProcessing`, `Clipboard Viewer Ignore`, `CanIncludeInClipboardHistory`=0, `CanUploadToCloudClipboard`=0 |
| Previous app | `NSWorkspace.frontmostApplication` (PST:14-31) | `GetForegroundWindow` + `EVENT_SYSTEM_FOREGROUND` tracking (§4.2) |
| Focus back after the bar | automatic: non-activating `NSPanel` (QP:5-6) | explicit `SetForegroundWindow(previous)` **before** hiding the bar (§4.4) |
| Paste permission | Accessibility (`CGPreflightPostEventAccess`) + Secure Event Input (PST:116-121) | none; UIPI: never inject into a higher-integrity window (§4.6) |
| ⌘V / Ctrl+V | `CGEvent` ⌘V at the HID tap (PST:141-152) | one `SendInput` call: Ctrl↓ V↓ V↑ Ctrl↑ (§4.8) |
| Input language | `TISCopyCurrentKeyboardInputSource` (SUP:84-92) | `GetKeyboardLayout` of the bar's thread (§5) |
| Start at login | `SMAppService.mainApp` (PST:155-190) | `HKCU\…\Run` + `…\Explorer\StartupApproved\Run` (§6) |
| Reopen → show bar | `applicationShouldHandleReopen` (APP:24-28) | named mutex + registered window message (§7) |
| Secrets | Keychain, `AfterFirstUnlockThisDeviceOnly` (SEC:16-56) | DPAPI CurrentUser files under `%LOCALAPPDATA%` (§8) |

### 0.4 Deviations from WARCH

| WARCH | This spec | Reason |
|---|---|---|
| WinUI 3 + H.NotifyIcon + CsWin32 (WARCH:9-13) | Avalonia 12 + hand-written `LibraryImport` (CsWin32 output is acceptable if it matches §10, incl. the full `INPUT` union) | current plan; resolved in DECISIONS.md D1 |
| Tokens in `PasswordVault` (WARCH:53) | DPAPI files (§8, API §3.3) | AUD:132 ("Windows — файл под DPAPI"); Credential Locker can roam with a Microsoft account, Keychain items here are ThisDeviceOnly (SEC:16-17, 53) |
| Fallback Alt+Space → Ctrl+Alt+Space only (WARCH:41-42) | adds Ctrl+Alt+Shift+Space | three-step chain in AC:217-236 (§11.1 Q2) |
| `IsAllowed`: Alt or Ctrl "(or Win)" (WARCH:109) | Win never allowed | MSDN RegisterHotKey reserves Windows-key shortcuts for the OS; same as QB §1.2.5 |
| Clipboard listener on the bar's HWND (WARCH:47) | dedicated message-only window (§1.2) | the bar HWND is Avalonia's and may be recreated; clipboard writes need a stable owner |
| `CopyAsync` returns `Task` (WARCH:131) | `Task<bool>` | `OpenClipboard` can stay busy (§3.7) |
| `nint` target in `IFocusTracker`/`IPasteService` (WARCH:151-164) | `TargetWindow` (hwnd, pid, tid) | HWND reuse detection, per-thread keyboard layout |
| `IStartupService { IsEnabled; SetEnabled }` (WARCH:166-170) | `State` incl. `DisabledInWindowsSettings`, `OpenStartupSettings()` | parity with `LoginItem.requiresApproval` (PST:158-163) |
| `KeyGesture` in `AirReply.App.Services` (WARCH:104-110) | pure rules in `AirReply.Core.Hotkeys` | unit-testable on any OS (`tests/AirReply.Core.Tests` is `net10.0`, WARCH:31) |

## 1. Shared Win32 infrastructure

### 1.1 Threading and lifetime

1. Every service in this file is UI-thread affine (macOS: every type is `@MainActor`, HK:6, CLIP:29,43, PST:7,41,
   KL:6). Calls from other threads are bugs (`Dispatcher.UIThread.VerifyAccess()` in Debug). Events are raised on the
   UI thread.
2. Startup order of the OS part (mirrors AC:53-82, QB §1.1.4): single-instance check (§7, before Avalonia starts) →
   message window (§1.2) → foreground tracker `Start()` (§4.2) → hotkey `RegisterSaved()` (§2.4) → clipboard monitor
   `Start()` (§3.3) → `StartupService.RepairCommandLine()` (§6).
3. Exit (AC:84-92, APP:30-32): hotkey `Unregister()` → clipboard `Stop()` → foreground tracker `Dispose()` → message
   window `Dispose()`.

### 1.2 Message-only window `MessageWindow` (Win)

1. Created once on the UI thread after Avalonia's dispatcher runs, before any `RegisterHotKey` or
   `AddClipboardFormatListener` (MSDN RegisterHotKey: registration fails for a window created by another thread).
2. `RegisterClassExW` with `cbSize = sizeof(WNDCLASSEXW)` (80 on x64/ARM64), `lpfnWndProc = &WndProc` (a static
   `[UnmanagedCallersOnly]` method — no delegate to keep alive), `hInstance = GetModuleHandleW(null)`,
   `lpszClassName = "AIReply.MessageWindow.{env}"` (`{env}` = `production` | `sandbox` | `local-<port>`, API §1 rule 4).
   Then `CreateWindowExW(0, class, "AI Reply", 0, 0, 0, 0, 0, HWND_MESSAGE (-3), 0, hInstance, 0)`.
3. Dispatch: `WM_HOTKEY` 0x0312 → §2.3; `WM_CLIPBOARDUPDATE` 0x031D → §3.3; registered `"AIReply.ShowBar"` → §7;
   everything else → `DefWindowProcW`.
4. `WndProc` never lets an exception cross the native boundary: try/catch, log exception type + `HResult`, return
   `DefWindowProcW`.
5. Message-only windows receive no broadcasts (`WM_SETTINGCHANGE`, `WM_ENDSESSION`, `TaskbarCreated`); those stay with
   Avalonia / the tray icon (QB §1.3, §1.4). `WM_HOTKEY` and `WM_CLIPBOARDUPDATE` are posted to the window, so they
   arrive (verify in the integration tests, §2.10, §3.11).
6. Avalonia's Win32 backend pumps `GetMessage`/`DispatchMessage` on the UI thread, which dispatches this window's
   messages (verify on Avalonia 12).
7. `Dispose()`: `DestroyWindow`, `UnregisterClassW`.

### 1.3 Manifest

`requestedExecutionLevel level="asInvoker" uiAccess="false"`. The app never asks for elevation; §4.6 assumes medium
integrity. If a user starts it elevated anyway, §7 still works (`ChangeWindowMessageFilterEx`).

### 1.4 Privacy (DES:130-134)

1. No low-level keyboard hook (`WH_KEYBOARD_LL`) and no raw input: the hotkey uses `RegisterHotKey` only, so the app
   sees no other keystrokes (HK:4-5).
2. Never call `GetWindowText` on foreign windows (titles carry chat names). Class names only (§4.2).
3. Logs and diagnostics never contain clipboard text, replies, window titles or secret values; they carry Win32 error
   codes, `HResult`s and our own state names. Diagnostics show the shortcut display string only (AC:255-265,
   QB §10.5).

### 1.5 Identifiers

| Purpose | Value |
|---|---|
| App id (≙ bundle id `kz.ai-reply.desktop`, CFG:15) | `AIReply` prefix + `{env}` |
| Single-instance mutex | `Local\AIReply-{env}` (API §3.3) |
| Message window class | `AIReply.MessageWindow.{env}` |
| Show-bar message | `RegisterWindowMessageW("AIReply.ShowBar")` |
| Run value name | `AI Reply` (production); `AI Reply ({env})` otherwise **(Win)** |
| Secret files | `%LOCALAPPDATA%\AI Reply\{env}\secure\{key}.bin`, DPAPI entropy `AIReply/{env}/{key}/v1` (API §3.3) |
| Injected-input marker `dwExtraInfo` | `0x41495270` (`'AIRp'`, same value as HK:18-19) |
| Hotkey ids | 1…0xBFFF (applications must use 0x0000-0xBFFF, MSDN RegisterHotKey) |

## 2. Global hotkey

### 2.1 macOS behaviour (normative)

1. One global hotkey through Carbon `RegisterEventHotKey`; needs no Accessibility or Input Monitoring permission and
   sees no other keystrokes (HK:4-5).
2. Default ⌥Space = key code 49 + `optionKey` 0x800 (KC:17-18, AS:46). Stored in UserDefaults `settings.shortcut` as
   JSON `{"keyCode":49,"modifiers":2048}` (AS:29,58,69-79; KCT:104-109); unreadable → default (AS:46, 77-79).
3. `register(combo)` always unregisters first, so on failure nothing stays registered (HK:32-35; HKT:50-59).
4. Order inside `register`: allowed check → `.notAllowed` before any system call (HK:36); install the event handler
   once (HK:37-38, 67-75) → failure `.system(status)`; increment the generation id with wrap-around (`&+=`, HK:39);
   register with `kEventHotKeyExclusive` so a combination owned by another app is reported instead of shared
   (HK:41-46); `eventHotKeyExistsErr` → `.alreadyTaken` (HK:47); other error → `.system(status)` (HK:48); success →
   `registered = combo` (HK:49-51).
5. Registering the same combination again replaces it (success, not "taken"); `unregister()` is idempotent (HK:54-58;
   HKT:61-77).
6. A press is handled only if the event's signature is `'AIRp'` (0x41495270), its id equals the current generation and a
   registration exists; otherwise it is ignored (HK:18-21, 60-65, 86; HKT:79-97 — presses queued for a replaced
   hotkey, another app's signature, and presses after `unregister()` are dropped). `onPress` runs on the main actor
   once per press (HK:14-15).
7. Releasing the center removes the handler and the hotkey; a later press is ignored (HK:27-30; HKT:99-114).
8. Allowed (KC:31-38, 98-104): at least one of ⌘⌥⌃; no bits besides ⌘⌥⌃⇧; the key is not a modifier key (⌘, ⇧, ⌥, ⌃
   left/right, Caps Lock, fn); not reserved (KCT:69-102).
9. Reserved (KC:106-133): ⌘Space, ⌃Space, ⌥⌘Space, ⌃⌘Space; ⌘Tab, ⇧⌘Tab, ⌘\`, ⇧⌘\`; ⌘Q, ⌘W, ⌘H, ⌘M, ⌥⌘Esc, ⌃⌘Q,
   ⇧⌘Q, ⌥⌘D; ⇧⌘3, ⇧⌘4, ⇧⌘5, ⌃⇧⌘3, ⌃⇧⌘4; ⌃←, ⌃→, ⌃↑, ⌃↓; ⌘C, ⌘V, ⌘X, ⌘A, ⌘Z, ⇧⌘Z ("including the ⌘C and ⌘V of
   AI Reply's own copy-and-paste flow").
10. Used by the system: enabled entries of `CopySymbolicHotKeys`, reduced to ⌘⌥⌃⇧ (KC:40-49, 86-96; KCT:132-142).
    Example: ⌃⌥Space "Select next source in Input menu".
11. Startup (AC:67-68, 221-236): `onPress = togglePanel`; candidates = saved == ⌥Space ? [⌥Space, ⌃⌥Space,
    ⌃⌥⌘Space] : [saved]; candidates used by the system are skipped; first success: if ≠ saved → **persist it as the
    shortcut** and note `settings.shortcut.fallback` (saved, used), else note nil; none → note
    `settings.shortcut.taken` (saved), nothing registered.
12. Change from Settings (AC:200-215): used by the system → note `settings.shortcut.system` (new), previous
    registration untouched; else `register(new)`: success → persist, note nil; failure → note `settings.shortcut.taken`
    (new) and re-register the previous combination, result ignored.
13. The note is session state, shown under the recorder as an orange caption (AC:29-30; SW:79-81). The menu item shows
    `menu.open` + 4 spaces + display string (SIC:48-49); diagnostics show `shortcut: <display>` (AC:261).
14. Display (KC:22-29, 66-84, 139-183; KL:3-34; KCT:23-67): modifiers ⌃⌥⇧⌘, then: named key ("Space", "Return",
    "Esc", arrows, F1…F20 …) → else the character the ASCII-capable layout types, upper-cased unless that changes its
    length ("ß" stays) → else US key cap → else `"Key <code>"`. Off the main thread US key caps only.
15. Recorder (SW:214-276): click toggles recording; title `settings.shortcut.recording` while recording, else display
    string or "—"; Esc (key code 53, any modifiers) cancels; a key-down that is not a valid combination → `NSSound.beep()`
    and keep recording; valid → stop, `changeShortcut`; losing first responder stops recording. Conversion
    (KC:51-64; KCT:111-130): key-down only; modifier key codes rejected; ⌘⌥⌃⇧ from the flags; Caps Lock, fn and keypad
    flags ignored.
16. Press → `togglePanel()`: visible ? hide : show (AC:104-106). `stop()` unregisters (AC:84-86).

### 2.2 Windows key gesture model (`AirReply.Core.Hotkeys`)

1. `KeyGesture(uint VirtualKey, HotKeyModifiers Modifiers)`; modifier bits equal Win32 `MOD_*`: Alt 0x1, Control 0x2,
   Shift 0x4, Win 0x8. `MOD_NOREPEAT` (0x4000) is added at registration and never stored.
2. Default `Alt+Space` = (0x20, Alt). Fallbacks: `Ctrl+Alt+Space` (0x20, Control|Alt); `Ctrl+Alt+Shift+Space`
   (0x20, Control|Alt|Shift) as the analog of ⌃⌥⌘Space **(decision; align: QB §1.2.3 lists two candidates)**.
3. Storage: `settings.json` property `Shortcut` (QB §1.6) = `{"VirtualKey":32,"Modifiers":1}` (numbers; enum as
   number). Missing or unparseable → Alt+Space, not rewritten until a change or a persisted fallback (MUST, AS:46).
4. `IsAllowed` = all of (MUST, mapped from KC:31-38):
   - a. `Modifiers` contains Control or Alt (Shift-only and none rejected; ≙ required ⌘⌥⌃).
   - b. `Modifiers` ⊆ {Control, Alt, Shift}: **Win never** (MSDN RegisterHotKey reserves Win shortcuts for the OS; same
     as QB §1.2.5); any other bit rejected (≙ unknown modifier bits, KCT:94-97).
   - c. `VirtualKey` is in the main-key allow-list **(Win; macOS only rejects modifier key codes — the Win32 VK space
     also holds mouse buttons, IME, lock and injection codes)**: A–Z 0x41–0x5A, 0–9 0x30–0x39, F1–F11 0x70–0x7A,
     F13–F24 0x7C–0x87, Space 0x20, Enter 0x0D, Tab 0x09, Esc 0x1B, Backspace 0x08, Insert 0x2D, Delete 0x2E,
     Home 0x24, End 0x23, PageUp 0x21, PageDown 0x22, ← ↑ → ↓ 0x25–0x28, Num 0–9 0x60–0x69, Num * + - . /
     0x6A 0x6B 0x6D 0x6E 0x6F, OEM 0xBA–0xC0 and 0xDB–0xDF, 0xE2 (ISO key), Pause 0x13. Not in the list: F12 0x7B
     (debugger-reserved, MSDN), PrintScreen 0x2C (OS hotkeys; QB §1.2.5 reserves it with any modifiers), modifier
     and lock keys 0x10–0x12, 0xA0–0xA5, 0x5B, 0x5C, 0x14, 0x90, 0x91, mouse 0x01–0x06, IME 0x15–0x1F, 0xE5, 0xE7.
   - d. Not in the reserved set below.
5. Reserved set (MUST in spirit of KC:106-133; list = QB §1.2.5 ∪ additions):

| Gesture(s) | Why (macOS analog) |
|---|---|
| Ctrl+C, Ctrl+V, Ctrl+X, Ctrl+A, Ctrl+Z, Ctrl+Y, Ctrl+Shift+Z, Ctrl+Insert | editing, incl. AI Reply's own copy/paste flow (⌘C ⌘V ⌘X ⌘A ⌘Z ⇧⌘Z) |
| Alt+Tab, Alt+Shift+Tab, Ctrl+Alt+Tab, Alt+Esc, Alt+Shift+Esc, Ctrl+Esc | app/window switching, Start (⌘Tab, ⇧⌘Tab) |
| Ctrl+Tab, Ctrl+Shift+Tab **(addition, align)** | tab/document switching in every browser and editor (⌘\`, ⇧⌘\`) |
| Ctrl+Space **(addition, align)** | IME on/off on CJK layouts, completion in editors (⌃Space input sources) |
| Alt+F4, Ctrl+F4, Ctrl+W | close window/document/tab (⌘Q, ⌘W) |
| Ctrl+Shift+Esc | Task Manager (⌥⌘Esc) |
| Ctrl+Alt+Delete | secure attention (⌃⌘Q lock) |
| Shift+Insert, Shift+Delete | already rejected by rule a; listed for parity with QB |

   Alt+Space is deliberately **not** reserved: registering it replaces the window-menu shortcut of every app while AI
   Reply runs, the same trade-off as other AI quick bars (WARCH:41).
6. Display string (QB §1.2.6, MUST order ⌃⌥⇧ → `Ctrl+Alt+Shift+`; a stored Win bit, only possible in an invalid
   value, is shown first as `Win+`). Key names: `Space`, `Enter`, `Tab`, `Esc`, `Backspace`, `Delete`, `Insert`,
   `Home`, `End`, `PgUp`, `PgDn`, `←` `→` `↑` `↓`, `F1`–`F24`, `Num 0`–`Num 9`, `Num *`, `Num +`, `Num -`, `Num .`,
   `Num /`, `Pause`. Letters and digits: the VK's own character (one capital Latin letter also while a ru/kk layout is
   active, ≙ KCT:54-62). OEM keys: US key caps 0xBA `;` 0xBB `=` 0xBC `,` 0xBD `-` 0xBE `.` 0xBF `/` 0xC0 `` ` ``
   0xDB `[` 0xDC `\` 0xDD `]` 0xDE `'` 0xE2 `\` (QB §1.2.6 chose US caps; macOS would show the ASCII-capable layout's
   character, e.g. `ß` on German — accepted difference). Anything else: `Key {vk}` in decimal (≙ KC:83).
   Examples: (0x20, Alt) `Alt+Space`; (0x20, Control|Alt) `Ctrl+Alt+Space`; (0x74, none) `F5`;
   (0x25, Control|Alt) `Ctrl+Alt+←`; (0x0D, Control|Alt|Shift) `Ctrl+Alt+Shift+Enter`; (0x4B, Control|Shift)
   `Ctrl+Shift+K`; (0xBF, Alt) `Alt+/`; (0x37, Control) `Ctrl+7`; (0xE9, Control) `Ctrl+Key 233`.

### 2.3 Registration service `HotKeyService : IHotKeyService`

1. `Register(g)` (MUST order of HK:32-52):
   1. `Unregister()`.
   2. `!g.IsAllowed` → `NotAllowed` (no Win32 call).
   3. `id = generation % 0xBFFF + 1` (generation increments on every attempt; ≙ HK:39).
   4. `RegisterHotKey(messageHwnd, id, (uint)g.Modifiers | MOD_NOREPEAT, g.VirtualKey)`.
   5. FALSE → `Marshal.GetLastPInvokeError()`: 1409 `ERROR_HOTKEY_ALREADY_REGISTERED` → `AlreadyTaken`; anything else
      → `Failed` (log the code; 1459 = no interactive window station). Nothing stays registered.
   6. TRUE → `Current = g`, `CurrentId = id` → `Ok`.
2. Exclusivity is inherent: RegisterHotKey fails if any thread or process registered the same keystrokes
   (≙ `kEventHotKeyExclusive`). A second `RegisterHotKey` with the same hWnd and id keeps **both** (MSDN), hence step 1.
3. `Unregister()`: `Current != null` → `UnregisterHotKey(messageHwnd, CurrentId)`; `Current = null`. Idempotent.
4. `WM_HOTKEY` (wParam = id, lParam = MAKELPARAM(MOD_* mask, VK)): ignore if `Current == null` or `(int)wParam !=
   CurrentId` (covers presses queued for a replaced registration and the system ids IDHOT_SNAPWINDOW −1 /
   IDHOT_SNAPDESKTOP −2; ≙ HK:60-65); else raise `Pressed`. Return 0.
5. `MOD_NOREPEAT`: holding the combination produces one `Pressed` (Carbon hotkeys do not auto-repeat; without the flag
   the bar would toggle with the key repeat rate).
6. `Dispose()`: `Unregister()`; later `WM_HOTKEY` messages are ignored (≙ HKT:99-114).
7. `Pressed` → `TogglePanel()`: bar visible ? `Hide(restoreFocus: true)` : `ShowPanel()` (AC:67, 104-106; QB §1.2.2).

### 2.4 Startup registration (`ShortcutCoordinator.RegisterSaved`, MUST ≙ AC:221-236)

1. `candidates = saved == AltSpace ? [AltSpace, ControlAltSpace, ControlAltShiftSpace] : [saved]` (third candidate:
   §2.2.2).
2. Skip candidates with `ISystemShortcuts.IsUsedBySystem` = true (§2.6; always false if the probe is not adopted).
3. First `Ok`: candidate ≠ saved → `settings.Shortcut = candidate`, save `settings.json`, `Note =`
   `settings.shortcut.fallback` (saved, candidate); else `Note = null`.
4. None → `Note = settings.shortcut.taken` (saved); nothing registered.
5. Persisting the fallback (step 3) means Alt+Space is never retried on later launches — macOS behaves the same
   (AC:227-229); see §11.1 Q3.

### 2.5 Changing the shortcut (`ShortcutCoordinator.Change`, MUST ≙ AC:200-215)

1. `IsUsedBySystem(g)` → `Note = settings.shortcut.system` (g); keep the previous registration; return.
2. `Register(g)`: `Ok` → `settings.Shortcut = g`, save, `Note = null`; otherwise → `Note = settings.shortcut.taken`
   (g) and `Register(previous)`, result ignored (an `AlreadyTaken` here leaves nothing registered and no extra note,
   as on macOS).
3. `Note` lives for the session and is shown under the recorder (SW:79-81); tray "Open" item and diagnostics show
   `settings.Shortcut.DisplayString` (SIC:48-49, AC:261; QB §1.3 item 1 uses a TAB accelerator column).

### 2.6 "Used by the system" probe `ISystemShortcuts` (optional parity, **align**: QB §1.2.4 / §11.4 drop it)

1. Windows has no `CopySymbolicHotKeys`. The collision that matters for ru/kk users (≙ ⌃⌥Space switching input sources,
   AC:221-222) is the input-method hotkeys. Enumerate them with `ImmGetHotKey(id, out mods, out vk, out hkl)` for
   ids 0x10–0x12 (IME_CHOTKEY_*), 0x30 (IME_JHOTKEY_CLOSE_OPEN), 0x50–0x52 (IME_KHOTKEY_*), 0x70–0x72
   (IME_THOTKEY_*), 0x100–0x11F (IME_HOTKEY_DSWITCH_FIRST…LAST, "switch to layout N" hotkeys).
2. Match when TRUE and `vk != 0` and `(vk & 0xFF) == g.VirtualKey` and either `mods & 0x0400`
   (MOD_IGNORE_ALL_MODIFIER) or `(mods & 0x7) == (uint)g.Modifiers` (MOD_ALT/CONTROL/SHIFT share the RegisterHotKey
   bits; MOD_LEFT 0x8000, MOD_RIGHT 0x4000, MOD_ON_KEYUP 0x0800 ignored).
3. Effect differs from macOS: a RegisterHotKey registration would win and silently break the input-method hotkey
   (instead of never firing); either way the combination must not be used.
4. Modifier-only toggles (Left Alt+Shift, Ctrl+Shift, Grave in `HKCU\Keyboard Layout\Toggle`) and Win+Space can never
   be gestures (§2.2.4 a–c), so they need no check.
5. `imm32.dll` missing or any exception → false.
6. If the probe is not adopted, `IsUsedBySystem` returns false and `settings.shortcut.system` is not shipped
   (QB §11.4).

### 2.7 Recorder (Settings → General, MUST ≙ SW:214-276; UI in QB §10.2)

1. Text: recording ? `settings.shortcut.recording` : `settings.Shortcut.DisplayString` (or `—` when unset).
2. Click toggles recording. Start: the button takes focus; `ShortcutCoordinator.SuspendForRecording()` →
   `Unregister()` **(Win, QB quirk fix Q9: otherwise pressing the current shortcut toggles the bar and steals focus;
   RegisterHotKey swallows the keystroke)**.
3. Input: `Win32Properties.AddWndProcHookCallback` on the Settings window (Avalonia 11.x API; verify on 12) while
   recording. Handle `WM_KEYDOWN` 0x0100 and `WM_SYSKEYDOWN` 0x0104; mark `WM_KEYUP`, `WM_SYSKEYUP` 0x0105, `WM_CHAR`
   0x0102 and `WM_SYSCHAR` 0x0106 handled too (no system menu on Alt+Space, no access keys on Alt, no F10 menu).
   Fallback without the hook: map Avalonia `Key` to VK with an explicit table (Avalonia `Key` values are not VKs).
4. Auto-repeat (`lParam` bit 30 set) → ignore.
5. `VK_ESCAPE` with any modifiers → stop recording, no change (≙ key code 53, SW:256-259).
6. Modifier and lock keys (0x10–0x12, 0xA0–0xA5, 0x5B, 0x5C, 0x14, 0x90, 0x91) → ignore silently and keep recording
   **(Win: they arrive as key-downs; on macOS they are `flagsChanged` and never beep)**.
7. Gesture = (wParam, modifiers from `GetKeyState(VK_CONTROL/VK_MENU/VK_SHIFT) < 0`, Win from `VK_LWIN`/`VK_RWIN`).
   Lock toggle states are ignored (≙ Caps Lock/fn/keypad flags, KC:56-63). AltGr arrives as LCtrl+RAlt and is
   recorded as Ctrl+Alt. `VK_PROCESSKEY` 0xE5 (IME composing) → not allowed.
8. Not allowed → `MessageBeep(MB_OK)` (≙ `NSSound.beep()`), keep recording (SW:260-263).
9. Allowed → stop recording → `ResumeAfterRecording()` (re-register the saved gesture) → `Change(g)` (SW:264-265).
10. Focus leaves the button or the window closes → stop recording → `ResumeAfterRecording()` (≙ SW:268-271).
    A failed resume → `Note = settings.shortcut.taken` (saved).

### 2.8 Interfaces

```csharp
namespace AirReply.Core.Hotkeys;   // net10.0, no Windows APIs

[Flags] public enum HotKeyModifiers : uint { None = 0, Alt = 0x1, Control = 0x2, Shift = 0x4, Win = 0x8 }

public readonly record struct KeyGesture(uint VirtualKey, HotKeyModifiers Modifiers)
{
    public static KeyGesture AltSpace { get; } = new(0x20, HotKeyModifiers.Alt);
    public static KeyGesture ControlAltSpace { get; } = new(0x20, HotKeyModifiers.Control | HotKeyModifiers.Alt);
    public static KeyGesture ControlAltShiftSpace { get; } =
        new(0x20, HotKeyModifiers.Control | HotKeyModifiers.Alt | HotKeyModifiers.Shift);
    public bool IsAllowed => KeyGestureRules.IsAllowed(this);
    public string DisplayString => KeyGestureRules.Display(this);
    /// Recorder conversion; null for modifier and lock keys (ignored silently).
    public static KeyGesture? FromKeyDown(uint virtualKey, bool control, bool alt, bool shift, bool win)
        => KeyGestureRules.FromKeyDown(virtualKey, control, alt, shift, win);
}

public static class KeyGestureRules
{
    public static bool IsAllowed(KeyGesture gesture);
    public static bool IsMainKey(uint virtualKey);
    public static IReadOnlySet<KeyGesture> Reserved { get; }
    public static string Display(KeyGesture gesture);
    public static KeyGesture? FromKeyDown(uint virtualKey, bool control, bool alt, bool shift, bool win);
}

namespace AirReply.App.Services;

public enum HotKeyResult { Ok, NotAllowed, AlreadyTaken, Failed }

public interface IHotKeyService : IDisposable
{
    event EventHandler? Pressed;              // UI thread, once per press of the current registration
    KeyGesture? Current { get; }
    HotKeyResult Register(KeyGesture gesture); // unregisters first; on failure nothing stays registered
    void Unregister();                         // idempotent
    int LastError { get; }                     // Win32 code of the last failed Register (diagnostics only)
}

public interface ISystemShortcuts { bool IsUsedBySystem(KeyGesture gesture); }

public sealed class ShortcutCoordinator
{
    public ShortcutCoordinator(IHotKeyService hotKeys, ISystemShortcuts system, ISettingsStore settings, IStrings strings);
    public string? Note { get; }               // localized when set; session only
    public event EventHandler? NoteChanged;
    public void RegisterSaved();               // §2.4
    public void Change(KeyGesture gesture);    // §2.5
    public void SuspendForRecording();         // §2.7.2
    public void ResumeAfterRecording();        // §2.7.9-10
}
```

### 2.9 Pitfalls

1. `RegisterHotKey`, `UnregisterHotKey` and the window must share the UI thread.
2. Low-level hooks of other software (PowerToys, AutoHotkey, VM/remote-desktop clients in full screen, vendor keyboard
   tools) can swallow the combination while `RegisterHotKey` reports success. Undetectable; Settings always shows the
   active shortcut so the user can change it.
3. Ctrl+Alt gestures also fire for AltGr on layouts that use AltGr (Polish, German…): AltGr+Space triggers
   Ctrl+Alt+Space.
4. `WM_HOTKEY` is not delivered on the secure desktop / lock screen.
5. The hotkey fires with an elevated app in front; the bar can still be activated (the hotkey grants foreground
   rights), but paste-back is blocked (§4.6).
6. MSDN: an app's registration can override some default OS hotkeys (PrintScreen → Snipping Tool) while its window
   is in front — PrintScreen is excluded (§2.2.4c).

### 2.10 Tests

Core (any OS), ports of KCT:
1. `displayStrings` (KCT:23-34) — the §2.2.6 examples; named keys never consult a layout (KCT:46-51).
2. `allowed` (KCT:69-78): Alt+Space, Ctrl+Alt+Space, Ctrl+Alt+Shift+Space, Ctrl+Shift+K, Ctrl+F5, Ctrl+Shift+Space,
   Ctrl+Alt+←, Ctrl+Shift+6, Ctrl+Alt+R, Alt+Enter.
3. `plainAndShiftOnlyAreNotEnough` (KCT:80-84): Esc, Enter, Tab, Space, K, F5 with none and with Shift.
4. `modifierKeysAreNotShortcutKeys` (KCT:86-92): 0x10–0x12, 0xA0–0xA5, 0x5B, 0x5C, 0x14, 0x90, 0x91 with Ctrl+Alt.
5. `winAndUnknownBitsAreRejected` (≙ KCT:94-97): Win+K, Ctrl+Win+K, modifiers 0x10, 0x4000.
6. `reservedAreRejected` (KCT:99-102): every row of §2.2.5; F12 and PrintScreen with Ctrl, Alt, Ctrl+Alt.
7. `jsonShape` (KCT:104-109): `{"VirtualKey":32,"Modifiers":1}` ⇄ Alt+Space.
8. `fromKeyDown` (KCT:111-130): (0x4B, ctrl, shift) → Ctrl+Shift+K; modifier VKs → null; CapsLock state irrelevant.
9. Coordinator with a fake `IHotKeyService`: default taken → Ctrl+Alt+Space saved + fallback note; all three taken →
   taken note, nothing registered; non-default saved taken → taken note; change success/failure/system-used per §2.5.

Windows integration (`[Trait("Category","Desktop")]`, interactive session; skip on 1459), ports of HKT with
Ctrl+Alt+Shift+F19 (0x82) and Ctrl+Alt+Shift+F18 (0x81) (≙ HKT:5-7):
10. not-allowed gestures (Ctrl+C, F5, Shift+K) → `NotAllowed`, `Current == null` (HKT:42-48).
11. failed registration drops the previous one; a second service on another thread can then register it (HKT:50-59).
12. register → re-register same → other → unregister ×2 → register (HKT:61-77).
13. a second registrar on another thread gets `AlreadyTaken` (1409).
14. posted `WM_HOTKEY` with the old id and with a foreign id is ignored; with `CurrentId` raises `Pressed` once; after
    `Unregister` ignored (HKT:79-97).
15. `Dispose` releases the combination (HKT:99-114).

## 3. Clipboard monitor

### 3.1 macOS behaviour (normative)

1. The monitor watches only `NSPasteboard.changeCount` (a cheap integer, contents unread) with a 1.0 s repeating timer,
   tolerance 0.5 s, common run-loop modes; text is read only when the panel opens (CLIP:3-4, 41-42, 75-90;
   CLIPT:222-241).
2. `start()` is idempotent; `baseline` = change count at start; the baseline and everything before it are never offered
   (CLIP:57-58, 76-78; CLIPT:151-158).
3. `stop()` invalidates the timer and clears baseline, last-seen, last-read, own and handled sets; nothing is offered
   afterwards; a restart takes a new baseline (CLIP:92-100; CLIPT:222-241). Releasing the monitor stops the timer
   (CLIP:71-73; CLIPT:243-249).
4. Dating: a tick that sees a count ≠ last seen records `(count, now)` (CLIP:146-153). A change no tick has seen yet is
   dated "now" at take time (CLIP:112-118; CLIPT:87-96). `observedAt` = first time this monitor saw the change
   (CLIP:8-9).
5. Every newly observed count drops own/handled marks of other counts and the cached read of another count
   (CLIP:152-158).
6. `takeFreshCopiedText(maxAge)` (CLIP:102-132), in order:
   1. stopped → `none`;
   2. count == baseline, or own, or handled → `none`;
   3. date the change (rule 4);
   4. `now − observedAt > maxAge` → `none` — exactly `maxAge` is still fresh (CLIPT:143-149); a stale change reads
      neither types nor contents (CLIPT:133-141);
   5. access denied (macOS 15.4+ `accessBehavior == .alwaysDeny`) → `denied`, nothing read (CLIP:120-121, 178-184;
      CLIPT:173-183);
   6. any concealed type → mark handled, `concealed`; contents never read (CLIP:122-126; CLIPT:160-171);
   7. needs `.string` and no `.fileURL` ("Files copied in Finder carry their names as text too") → else `none`
      (CLIP:127-128; CLIPT:191-199);
   8. read the string at most once per change (cached, including a nil result), and drop it when the count changed
      during the read (CLIP:160-167; CLIPT:72-85, 201-208, 210-220);
   9. nil or whitespace-only (`whitespacesAndNewlines`) → `none` (CLIP:129-130; CLIPT:185-189);
   10. return the **untrimmed** text (CLIP:131; CLIPT:72-81).
7. A result is offered again at every opening until handled or expired (CLIPT:82-84).
8. `markHandled(copied)` adds its count to handled and drops its cached read (CLIP:134-138). Copying the same words
   again is a new change and is offered (CLIPT:109-121); handling an older copy keeps a newer one (CLIPT:123-131).
9. `copy(text)`: `clearContents` + `setString(.string)`; the resulting count is own; cached read dropped
   (CLIP:140-144, 190-195). Own copies are never offered and never read (CLIPT:98-107).
10. Concealed markers: `org.nspasteboard.ConcealedType`, `org.nspasteboard.TransientType`,
    `org.nspasteboard.AutoGeneratedType`, `com.agilebits.onepassword` (CLIP:45-52).
11. Callers: `start()` at launch, `stop()` at exit (AC:69, 86); `showPanel` passes `offerCopiedText ?
    take(maxAge: QuickBarModel.keepAlive = 600 s) : .none` (AC:99; QBM:157-158); the model applies only `.text`
    (QBM:164-168), so `.concealed`/`.denied` show nothing (DES:54-55); `markHandled` after a successful generation
    (QBM:327-333) and on dismiss (QBM:384-389); the Copy button trims the result and calls `copy` (QBM:354-366);
    "Copy diagnostics" also goes through `copy` (AC:255-265). `offerCopiedText` defaults to true (AS:35, 50).

### 3.2 Windows state

Same fields as macOS: `Baseline: uint?`, `LastSeen: (uint Seq, DateTimeOffset At)?`, `Own: HashSet<uint>`,
`Handled: HashSet<uint>`, `LastRead: (uint Seq, string? Text)?`. `Seq` = `GetClipboardSequenceNumber()`: a 32-bit
counter per window station that changes when the clipboard contents change or the clipboard is emptied; 0 when the
process has no `WINSTA_ACCESSCLIPBOARD` access. Time = `TimeProvider.GetUtcNow()` (wall clock, like `Date`).

### 3.3 Dating changes (≙ rules 1-5)

1. `Start()`: started → return. `Baseline = GetClipboardSequenceNumber()`; `AddClipboardFormatListener(messageHwnd)`.
   If it fails → log the code and start a 1.0 s `DispatcherTimer` calling `OnClipboardUpdate()` (exact macOS cadence).
2. `WM_CLIPBOARDUPDATE` (or the fallback tick) → `OnClipboardUpdate()`: `seq = GetClipboardSequenceNumber()`;
   `seq != LastSeen?.Seq` → `Observe(seq, now)`. Never opens the clipboard (≙ `poll`, CLIP:146-150).
3. `Observe(seq, at)`: `LastSeen = (seq, at)`; `Own` and `Handled` keep only `seq`; `LastRead` dropped unless
   `LastRead.Seq == seq` (≙ CLIP:152-158).
4. `Stop()`: `RemoveClipboardFormatListener` / stop the timer; clear every field. `Dispose()` = `Stop()`.

### 3.4 `TakeFreshCopiedText(maxAge)` (Windows algorithm)

1. `Baseline == null` → None.
2. `seq = GetClipboardSequenceNumber()`; `seq == Baseline` or `Own ∋ seq` or `Handled ∋ seq` → None.
3. Clipboard owner is ours: `GetClipboardOwner()` ≠ 0 and its process id == ours → None **(decision §11.1 Q4:
   covers manual Ctrl+C inside the bar or Settings — CLIP:103 promises "never returns text this app wrote", but
   macOS only tracks its Copy button)**.
4. `observedAt = LastSeen?.Seq == seq ? LastSeen.At : (Observe(seq, now), now)`.
5. `now − observedAt > maxAge` → None. Nothing opened, nothing read.
6. (`Denied` is never produced on Windows: desktop apps have no per-app clipboard permission. The enum value stays for
   the shared model.)
7. `LastRead?.Seq == seq` → `text = LastRead.Text`, go to step 10 (contents cannot change without a new sequence
   number; ≙ one read per change, CLIPT:72-85).
8. `IsHungAppWindow(GetClipboardOwner())` → None, not cached **(Win: `GetClipboardData` on delayed-rendered data
   synchronously asks the owner and would freeze the UI)**.
9. Open: `OpenClipboard(messageHwnd)`, up to 5 attempts 10 ms apart (`Thread.Sleep`, ≤ ~40 ms on the UI thread). All
   fail (`ERROR_ACCESS_DENIED` 5: another process holds it) → None, nothing cached. Inside, with `CloseClipboard()` in
   `finally` and no `await`:
   1. `GetClipboardSequenceNumber() != seq` → None, nothing cached (replaced since step 2; ≙ CLIP:163-164).
   2. Concealed (§3.5) → `Handled.Add(seq)`, return Concealed (text never read).
   3. `IsClipboardFormatAvailable(CF_HDROP = 15)` → None (Explorer file copies); `!IsClipboardFormatAvailable
      (CF_UNICODETEXT = 13)` → None. A CF_UNICODETEXT synthesized from CF_TEXT/CF_OEMTEXT counts as text.
   4. `text = ReadUnicodeText()` (§3.6), may be null.
   5. `seqNow = GetClipboardSequenceNumber()` (still open): `seqNow != seq` → **alias**: only delayed rendering for our
      read can change it while we hold the clipboard; set `LastSeen = (seqNow, observedAt)` and `seq = seqNow`
      (§11.1 Q5).
   6. `LastRead = (seq, text)` — cached also when null (a failed read is not retried for the same change,
      ≙ CLIPT:201-208).
10. `text == null` or `string.IsNullOrWhiteSpace(text)` → None. (.NET white space = Zs, Zl, Zp, U+0009–U+000D, U+0085,
    U+00A0 — the same set as Swift `whitespacesAndNewlines`; " \n\t　 " is blank, CLIPT:185-189.)
11. Normalize `"\r\n"` → `"\n"` **(Win: same server length as macOS, TM:3-9; the server counts runes)**; no other
    change, no trimming.
12. Return `Text(new CopiedText(text, seq, observedAt))`.

### 3.5 Concealed markers (Windows)

| Registered format (`RegisterClipboardFormatW`) | Concealed when | Source |
|---|---|---|
| `ExcludeClipboardContentFromMonitorProcessing` | present (any data) | MSDN Clipboard Formats: keeps every format out of history and sync |
| `Clipboard Viewer Ignore` | present | convention of clipboard managers and password managers |
| `CanIncludeInClipboardHistory` | present with DWORD 0, or present but unreadable / < 4 bytes | MSDN: 0 = keep out of history, 1 = explicitly allow |
| `CanUploadToCloudClipboard` | present with DWORD 0, or unreadable / < 4 bytes | MSDN: 0 = do not sync, 1 = explicitly allow |

1. Format ids are registered once at `Start()` (the same name returns the same id in every process).
2. Presence: `IsClipboardFormatAvailable`. Value: `GetClipboardData` + `GlobalLock`, first 4 bytes little-endian, inside
   the open section. This reads only the marker, never the text.
3. Value 1 in the last two rows is not concealed.
4. Result ≙ rule 6.6: handled + Concealed; the next opening → None; the model shows nothing.

### 3.6 Reading CF_UNICODETEXT

1. `h = GetClipboardData(CF_UNICODETEXT)`; 0 → null.
2. `p = GlobalLock(h)`; null → null.
3. `chars = GlobalSize(h) / 2`; take the span up to the first `'\0'` or `chars` (never trust termination).
4. `new string(span)`; `GlobalUnlock(h)` in `finally`. Never `GlobalFree` a handle from `GetClipboardData`; never use
   it after `CloseClipboard`.
5. No length cap (macOS reads the whole string); the bar shows the first line and the count (DES:48-53).

### 3.7 `CopyAsync(text)` → `Task<bool>` (≙ rule 9)

1. Line endings: `"\r\n"` → `"\n"`, then `"\n"` → `"\r\n"` (Windows text convention).
2. Up to 10 attempts 25 ms apart (`Task.Delay`, UI not blocked): `OpenClipboard(messageHwnd)`.
3. `EmptyClipboard()` (makes the message window the owner); `hMem = GlobalAlloc(GMEM_MOVEABLE 0x0002, (len + 1) * 2)`;
   copy chars + NUL; `GlobalUnlock`; `SetClipboardData(CF_UNICODETEXT, hMem)`: failure → `GlobalFree(hMem)`, success →
   the system owns `hMem`.
4. Only CF_UNICODETEXT (Windows synthesizes CF_TEXT, CF_OEMTEXT, CF_LOCALE): no private or exclusion formats, so the
   reply appears in Win+V history like any copy (macOS writes a plain string only, CLIP:190-195). Immediate
   rendering, so the data survives app exit.
5. `CloseClipboard()`; `Own.Add(GetClipboardSequenceNumber())`; `LastRead = null`; return true.
6. All attempts busy → false; the caller shows `issue.clipboard_busy` (§9) and does not run the after-copy hide/paste
   **(Win: a macOS write cannot fail)**.
7. Never use Avalonia's `IClipboard` for this write (OLE, delayed rendering, owner = OLE window).

### 3.8 Callers on Windows (MUST ≙ rule 11; UI flow in QB §2.4, §9)

1. `Start()` at launch, `Stop()` at exit (§1.1).
2. `ShowPanel`: `OfferCopiedText ? TakeFreshCopiedText(TimeSpan.FromSeconds(600)) : None`; with the setting off the
   monitor keeps dating changes but never opens the clipboard.
3. `MarkHandled` after a successful generation and on dismiss of the copied capsule.
4. Copy button, "Copy diagnostics": `CopyAsync`.

### 3.9 Interfaces

```csharp
namespace AirReply.Core.Clipboard;   // net10.0, logic + fakeable seam (≙ PasteboardAccess, CLIP:28-39)

public sealed record CopiedText(string Text, uint SequenceNumber, DateTimeOffset ObservedAt);
public enum CopiedTextStatus { None, Text, Concealed, Denied }   // Denied: parity only, not produced on Windows
public readonly record struct CopiedTextResult(CopiedTextStatus Status, CopiedText? Copied)
{
    public static CopiedTextResult None => new(CopiedTextStatus.None, null);
    public static CopiedTextResult Concealed => new(CopiedTextStatus.Concealed, null);
    public static CopiedTextResult Of(CopiedText copied) => new(CopiedTextStatus.Text, copied);
}

public enum ClipboardFormat { UnicodeText, FileDrop, ExcludeFromMonitor, ViewerIgnore, CanIncludeInHistory, CanUploadToCloud }

public interface IClipboardAccess
{
    uint SequenceNumber { get; }                       // GetClipboardSequenceNumber; never opens
    bool IsOwnedByThisProcess { get; }                 // GetClipboardOwner → GetWindowThreadProcessId
    bool IsOwnerHung { get; }                          // IsHungAppWindow(GetClipboardOwner())
    IOpenClipboard? TryOpen(int attempts, TimeSpan delay);                 // null when busy; Dispose closes
    Task<uint?> WriteTextAsync(string text, int attempts, TimeSpan delay); // CRLF text; seq after close, null when busy
}

public interface IOpenClipboard : IDisposable
{
    uint SequenceNumber { get; }
    bool HasFormat(ClipboardFormat format);
    uint? ReadDword(ClipboardFormat format);           // null: absent, unreadable or < 4 bytes
    string? ReadUnicodeText();                         // §3.6
}

public sealed class ClipboardMonitor : IDisposable
{
    public static readonly TimeSpan FallbackPollInterval = TimeSpan.FromSeconds(1);
    public ClipboardMonitor(IClipboardAccess clipboard, TimeProvider time);
    public void Start();
    public void Stop();
    public void OnClipboardUpdate();
    public CopiedTextResult TakeFreshCopiedText(TimeSpan maxAge);
    public void MarkHandled(CopiedText copied);
    public Task<bool> CopyAsync(string text);
}
// App: Win32ClipboardAccess : IClipboardAccess; ClipboardService wires the monitor to MessageWindow
// (AddClipboardFormatListener, WM_CLIPBOARDUPDATE → OnClipboardUpdate, fallback timer).
```

### 3.10 Pitfalls

1. Always `CloseClipboard` in `finally`; never `await` or call into the model while it is open — it blocks copy/paste
   in every app.
2. Bounded retries only; never spin on `OpenClipboard`.
3. Delayed rendering (Excel, Office, RDP `rdpclip`, browsers) makes `GetClipboardData` synchronous with the owner; the
   hung-owner guard (§3.4.8) and the alias rule (§3.4.9.5) exist for it.
4. `WM_CLIPBOARDUPDATE` also arrives for our own writes and for Win+V history pastes; only the sequence number matters.
5. Clipboard tools that rewrite every copy (plain-text scrubbers, some managers) make our own reply look like a foreign
   copy (new owner, new number) — same as macOS with such tools.
6. In a non-interactive window station `GetClipboardSequenceNumber()` returns 0: baseline 0, nothing ever offered.

### 3.11 Tests

Core with a fake `IClipboardAccess` and `FakeTimeProvider` (ports of CLIPT, same texts):
1. fresh copy offered, dated when first observed, offered again, opened/read once (CLIPT:72-85).
2. change not yet observed is dated now; later update keeps the date (CLIPT:87-96).
3. own reply never offered or opened; a later foreign copy is (CLIPT:98-107).
4. handled not offered again; same words copied again offered (CLIPT:109-121).
5. handling an older copy keeps the newer (CLIPT:123-131).
6. stale copy: no open, no format query (CLIPT:133-141); exactly 600 s is fresh (CLIPT:143-149).
7. copies before `Start` never offered (CLIPT:151-158).
8. each concealed marker → Concealed, then None, text never read (CLIPT:160-171); history/cloud DWORD 1 → offered;
   unreadable marker → Concealed.
9. blank " \r\n\t　 " → None (CLIPT:185-189).
10. no text format → None; CF_HDROP + text → None; no text read (CLIPT:191-199).
11. null `ReadUnicodeText` → None, read once for the same change (CLIPT:201-208).
12. sequence changes between step 2 and the open → None; next take evaluates the new change (≙ CLIPT:210-220).
13. sequence changes while open (delayed rendering) → result carries the new number; `MarkHandled` of it stops it.
14. busy clipboard (5 failed opens) → None, nothing cached; next take succeeds.
15. owner is our process → None.
16. `Start` idempotent; `Stop` ends offering; restart → new baseline (CLIPT:222-241); `Dispose` stops listening
    (CLIPT:243-249).
17. CRLF in → LF out; `CopyAsync` writes CRLF, records own number, returns false when busy.
Windows integration (Desktop trait): listener on `HWND_MESSAGE` receives `WM_CLIPBOARDUPDATE`; concealed detection
with KeePassXC (sets the exclusion formats) and with a test writer; Excel copy (delayed rendering) to settle Q5.

## 4. Foreground tracking, bar show/hide, automatic paste

### 4.1 macOS behaviour (normative)

1. Right before the panel shows, remember the frontmost app; nil when it is this app (same pid or same bundle id) or
   when there is none — "With our own window in front (settings) there is nothing to go back to, and a paste must not
   land in an app the user left earlier" (PST:26-35; PSTT:122-160).
2. `showPanel()`: no-op while visible; order remember → take copied text → `model.panelDidOpen` → `panel.show()`
   (AC:96-102).
3. The panel is non-activating: it takes key focus while the previous app stays active, so after it hides ⌘V lands
   there (QP:5-6, 10-30; DES:18-20). It hides on Esc (`model.escape`), on the hotkey, and on resign-key unless
   `holdsOpen` (permission prompt, recording, transcribing, document picker) (QP:32-34, 65-66, 117-121, 152-155;
   QBM:191-198).
4. After Copy (700 ms "copied" state, QBM:354-366): `closeAfterCopy || pasteAfterCopy` → hide; `pasteAfterCopy` and
   `availability() == .available` → `paste(into: previousApp)` (AC:133-142). Defaults: close on, paste off
   (AS:37-39, 51-52).
5. Availability: Secure Event Input → `secureInputActive` (wins); no event-posting permission → `needsPermission`;
   else `available` (PST:43-48, 62-65; PSTT:66-74). Turning the toggle on shows the permission prompt once
   (SW:111-116; AC:246-248); privacy settings URL (PST:72-76).
6. `paste(into:)` (PST:78-96): no app, terminated, or not available → false without activating (PSTT:76-89);
   activate (`yieldActivation` + `activate`, PST:127-131); not frontmost → up to 12 checks × 50 ms, still not
   frontmost → false, nothing posted ("Posting anyway could paste the reply into whatever app is in front instead");
   became frontmost → one more 50 ms for its key window (PSTT:98-111); cancelled → false; post ⌘V.
7. ⌘V = the key that types "v" with ⌘ in the ASCII-capable layout (Dvorak), down + up with only the ⌘ flag, posted at
   the HID tap (PST:141-152; KL:36-44). The panel must be closed first or it would get the keystroke (PST:79).

### 4.2 Remembering the foreground window (Windows)

1. Tracking: `FocusTracker.Start()` → `SetWinEventHook(EVENT_SYSTEM_FOREGROUND 0x0003, 0x0003, 0, &OnWinEvent, 0, 0,
   WINEVENT_OUTOFCONTEXT 0)` on the UI thread (needs its message loop). Callback (`idObject == 0` only): unless
   `IsTransparent(hwnd)` → `LastRealForeground = hwnd`. Do not pass `WINEVENT_SKIPOWNPROCESS`: the Settings window
   must be recorded.
2. `IsTransparent(hwnd)` — windows that take the foreground only because the user reached for AI Reply (on macOS the
   status item and its menu never activate anything):
   - root class (`GetClassNameW(GetAncestor(hwnd, GA_ROOT))`) ∈ `Shell_TrayWnd`, `Shell_SecondaryTrayWnd`,
     `NotifyIconOverflowWindow`, `TopLevelWindowForOverflowXamlIsland`, `Windows.UI.Core.CoreWindow` (Start, search,
     flyouts), `Shell_InputSwitchTopLevelWindow`, `XamlExplorerHostIslandWindow`, `MultitaskingViewFrame`,
     `ForegroundStaging` (heuristic list; verify on Windows 10 22H2 and 11 24H2);
   - or the root belongs to our process and is not the Settings window or a window it owns (the bar, the tray icon's
     hidden window, the tray menu, the bar's file dialog).
3. `RememberForegroundWindow()`: `fg = GetForegroundWindow()`; `fg == 0 || IsTransparent(fg)` →
   `fg = LastRealForeground`; `fg == 0 || !IsWindow(fg)` → `Previous = null`; `root = GetAncestor(fg, GA_ROOT)`;
   `tid = GetWindowThreadProcessId(root, out pid)`; `pid == Environment.ProcessId` → `Previous = null` (our Settings in
   front, ≙ rule 4.1.1); else `Previous = new TargetWindow(root, pid, tid)`.
4. The bundle-id clause (PST:33-35, PSTT:149-154) needs no code: a second AI Reply of the same env cannot run (§7).
5. `IsAlive(t)`: `IsWindow(t.Handle)` and `GetWindowThreadProcessId(t.Handle, out pid) == t.ThreadId` and
   `pid == t.ProcessId` (HWND values are reused after destruction).

### 4.3 Showing the bar (MUST order ≙ AC:96-102; UI in QB §2.4)

1. Visible → return (QB: also re-activate it).
2. `RememberForegroundWindow()`.
3. `copied = OfferCopiedText ? clipboard.TakeFreshCopiedText(600 s) : None`.
4. `model.PanelDidOpen(copied)`.
5. Place, show, `Activate(bar)` (§4.4.5) **(Win: Windows has no non-activating window that takes keyboard focus; a
   `WS_EX_NOACTIVATE` window cannot be typed into, so the bar is activated and focus is handed back on hide)**. The bar
   is `WS_EX_TOOLWINDOW` (no taskbar button, not in Alt+Tab ≙ `.ignoresCycle`) and topmost (QB §2.1).
6. Foreground rights: `WM_HOTKEY` and a tray-icon click let our process call `SetForegroundWindow`; a second launch
   grants them with `AllowSetForegroundWindow` (§7).

### 4.4 Hiding the bar and restoring focus

1. `Hide(restoreFocus)` triggers and flags as in QB §2.4 (Esc, hotkey, after copy → true; deactivation, URL, Settings,
   tray left click → false).
2. Order **(align: QB §2.4 hides first, then restores)**: if `restoreFocus` and the bar is the foreground window and
   `Previous` is alive and not minimized → `Activate(Previous)` **before** hiding; then hide the bar; then
   `model.PanelDidClose()`. Reason: while the bar is foreground our process may set the foreground window; after
   `SW_HIDE` Windows activates the next window in z-order (an extra activation flash, possibly another app) and our
   right then depends on "received the last input event", which a mouse move can take away.
3. Activating `Previous` deactivates the bar; that `Deactivated` is ignored while a hide is in progress (flag), so
   `PanelDidClose` runs once (≙ QP:117-121 guard).
4. Bar not foreground (held open, user clicked elsewhere) → hide without touching focus.
5. `Activate(t)` ladder:
   1. `IsForeground(t)` (`GetAncestor(GetForegroundWindow(), GA_ROOTOWNER) == GetAncestor(t.Handle, GA_ROOTOWNER)`) →
      true.
   2. `SetForegroundWindow(t.Handle)`; foreground now → true.
   3. Send one zero-filled `INPUT_MOUSE` with `SendInput` (makes our process the last input provider), then
      `SetForegroundWindow` again.
   4. Still not: if the current foreground window is not hung (`IsHungAppWindow` false) →
      `AttachThreadInput(GetCurrentThreadId(), fgThread, TRUE)`; `SetForegroundWindow`; `BringWindowToTop`;
      `AttachThreadInput(…, FALSE)` in `finally`.
   5. Return `IsForeground(t)`.
   Rejected: the "ALT trick" (injected VK_MENU down/up): injected input goes to whichever window is foreground when it
   is processed, so the Alt tap can reach the target and open its menu bar, eating the following Ctrl+V. Never change
   `SPI_SETFOREGROUNDLOCKTIMEOUT` (a system setting).
6. Focus restore is attempted for elevated targets too; if Windows refuses, the user clicks the window. Paste is never
   attempted for them (§4.6).

### 4.5 After copy (MUST ≙ AC:133-142; UI in QB §9)

1. `CopyAsync` false → `issue.clipboard_busy`, stop.
2. 700 ms "copied" state (QBM:361), then `AfterCopy()`.
3. `!(CloseAfterCopy || PasteAfterCopy)` → stay open.
4. `Hide(restoreFocus: true)` (§4.4).
5. `PasteAfterCopy && paste.GetAvailability(Previous) == Available` → `_ = paste.PasteAsync(Previous)`; otherwise copy
   only, silently.

### 4.6 Paste availability and UIPI

1. `NoTarget`: `Previous == null`, `!IsAlive`, or `IsIconic`. `TargetElevated`: target integrity level > ours, or
   unknown. Else `Available`.
2. Integrity: `OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION 0x1000, FALSE, pid)` → `OpenProcessToken(TOKEN_QUERY
   0x0008)` → `GetTokenInformation(TokenIntegrityLevel = 25)` → `TOKEN_MANDATORY_LABEL.Label.Sid` → last
   sub-authority (RID 0x1000 low, 0x2000 medium, 0x2100 medium-plus, 0x3000 high, 0x4000 system). Own RID through the
   `GetCurrentProcess()` pseudo-handle. Any failure → treat as elevated (copy only). Close every handle.
3. Why before sending: UIPI silently drops injected input into a higher-integrity window, and per MSDN SendInput
   neither its return value nor `GetLastError` reveals that block.
4. No permission model **(Win)**: no `needsPermission` state, no prompt when the toggle is turned on (QB §10.2 row
   "Paste automatically"); the footer explains the administrator limit (QB §11.2). No Secure Event Input analog:
   password fields do not block injected input, and on the secure desktop the bar cannot be in use.

### 4.7 `PasteAsync(t, ct)` (MUST ≙ PST:78-96)

1. `t == null`, `!IsAlive(t)`, `IsIconic`, or `GetAvailability(t) != Available` → false; no activation, no input
   (PSTT:76-89).
2. `!IsForeground(t)` → `Activate(t)`; `checks = 0`; loop: `IsForeground(t)` → break; `checks == 12` → return false;
   `await Pause()`; `checks++` (12 × 50 ms = 600 ms).
3. Settle: `await Pause()` once — always **(Win, decision; same as QB §9.5 "+ 50 ms")**: the bar held activation, so
   the target's own focus restore is only milliseconds old (macOS skips this when the app was already frontmost).
4. Modifiers **(Win)**: while any of `VK_SHIFT`, `VK_CONTROL`, `VK_MENU`, `VK_LWIN`, `VK_RWIN` is down
   (`GetAsyncKeyState(vk) & 0x8000`): after 12 pauses → false; else `await Pause()`. (Injected Ctrl+V combines with
   physically held keys; a CGEvent carries explicit flags.)
5. `ct.IsCancellationRequested` → false (≙ `Task.isCancelled`).
6. `!IsForeground(t)` → false (the user switched meanwhile).
7. `return PostControlV(t)`.
`Pause()` = `Task.Delay(50 ms, ct)` (timer resolution makes it 46–62 ms).

### 4.8 `PostControlV(t)` — SendInput

1. `hkl = GetKeyboardLayout(t.ThreadId)`; `scanCtrl = MapVirtualKeyExW(VK_CONTROL 0x11, MAPVK_VK_TO_VSC 0, hkl)`
   (0x1D); `scanV = MapVirtualKeyExW(0x56, 0, hkl)`.
2. Four `INPUT_KEYBOARD` (1) inputs, `time = 0`, `dwExtraInfo = 0x41495270`: Ctrl down, V down, V up
   (`KEYEVENTF_KEYUP` 0x2), Ctrl up.
3. One `SendInput(4, inputs, sizeof(INPUT))` call (inserted serially, not interleaved with user input). Return value
   must be 4; else false and log `GetLastPInvokeError()` (87 = wrong `cbSize`).
4. VK 0x56 is right under ru/kk/Dvorak: apps resolve Ctrl+V by virtual key, and the target layout supplies a matching
   scan code for apps that read scan codes (≙ the Dvorak lookup in KL:36-44).
5. Not used: `keybd_event` (legacy), `PostMessage(WM_PASTE)` (standard edit controls only), posted `WM_KEYDOWN` (no
   key state; ignored by many apps).
6. `sizeof(INPUT) == 40` on x64 and ARM64: `type` (4) + padding (4) + union (32, `MOUSEINPUT` is the largest member,
   8-byte aligned because of `dwExtraInfo`). A union declared with `KEYBDINPUT` only gives 32 → `SendInput` fails with
   87. Unit-test the size.

### 4.9 Interfaces

```csharp
namespace AirReply.App.Services;

public readonly record struct TargetWindow(nint Handle, uint ProcessId, uint ThreadId);

public interface IFocusTracker : IDisposable
{
    void Start();                                  // SetWinEventHook(EVENT_SYSTEM_FOREGROUND)
    void RememberForegroundWindow();               // right before the bar shows (§4.2.3)
    TargetWindow? PreviousWindow { get; }
    bool IsAlive(TargetWindow target);
    bool IsForeground(TargetWindow target);
    bool Activate(TargetWindow target);            // §4.4.5; returns IsForeground afterwards
}

public enum PasteAvailability { Available, TargetElevated, NoTarget }

public interface IPasteService
{
    PasteAvailability GetAvailability(TargetWindow? target);
    Task<bool> PasteAsync(TargetWindow? target, CancellationToken cancellationToken = default);
}

/// The system calls behind PasteService, faked in tests so no input is ever injected (≙ PasteSystem, PST:99-112).
internal interface IPasteSystem
{
    bool IsAlive(TargetWindow target);
    bool IsMinimized(TargetWindow target);
    bool IsHigherIntegrity(TargetWindow target);   // true also when unknown
    bool Activate(TargetWindow target);
    bool IsForeground(TargetWindow target);
    bool AnyModifierDown();
    Task PauseAsync(CancellationToken cancellationToken);   // 50 ms
    bool PostControlV(TargetWindow target);
}

public static class PasteTiming
{
    public const int ActivationChecks = 12;        // ≙ PST:50-51
    public const int ModifierChecks = 12;
    public static readonly TimeSpan Pause = TimeSpan.FromMilliseconds(50);
}
```

### 4.10 Pitfalls

1. Hiding first and restoring second can activate an unrelated window in between (§4.4.2).
2. Tray click, tray menu and Start make the shell or our hidden window foreground; without §4.2.2 the paste target would
   be the taskbar.
3. `SetForegroundWindow` also fails while a menu is open in the foreground app or after `LockSetForegroundWindow`;
   the ladder then ends false and paste gives up after 600 ms (MUST behaviour).
4. `AttachThreadInput` to a hung thread can block; never attach without the `IsHungAppWindow` check; always detach.
5. UWP apps live in `ApplicationFrameHost` (`ApplicationFrameWindow` is the root); comparing root owners, not process
   ids, keeps them apart.

### 4.11 Tests

Core/App with a fake `IPasteSystem` (ports of PSTT):
1. no target, dead target, minimized, elevated → false, no activation, no input (PSTT:76-89).
2. target in front → pauses == 1 (settle), posts == 1 (≙ PSTT:91-96, macOS 0 pauses).
3. foreground after 3 pauses → pauses == 4, posts == 1 (PSTT:98-104).
4. never foreground → pauses == 12, posts == 0 (PSTT:106-111).
5. modifiers held 2 pauses → posted; held forever → false after 12 more.
6. cancelled → false, no post.
Focus tracker with fakes: remembers a foreign window; `null` with Settings in front; `null` with no foreground;
taskbar/tray/our tray menu in front → last real window; last real window = our Settings → `null`; HWND reuse →
`IsAlive` false (≙ PSTT:122-160).
Interop: `sizeof(INPUT) == 40` when 64-bit; the 4-input sequence (order, flags, marker). Manual: paste into Notepad,
Chrome, Telegram Desktop, Windows Terminal, an elevated Notepad (must not paste), with ru and kk layouts.

## 5. Keyboard input language (`input_language`, voice hints)

### 5.1 macOS behaviour (normative)

1. `KeyboardLanguage.current()` = first language of the current keyboard input source
   (`TISCopyCurrentKeyboardInputSource`, `kTISPropertyInputSourceLanguages`), first two characters lower-cased; nil if
   unavailable (SUP:84-92).
2. Read on the main actor at the start of every generation request — including Retry (same idempotency key) and
   "Another variant" (QBM:273-288, 296-302) — and before every voice upload (AC:42-44; SUP:97-98, 107).
3. The generator keeps it only if the first two letters are `kk`, `ru` or `en`; otherwise `input_language` is omitted
   (GEN:115, 130, 155, 168, 189-193; AGT:61-87: `ru-KZ` → `ru`, `de` → absent).
4. Backend: `InputLanguage` trims, lower-cases and accepts only kk/ru/en, ignoring anything else
   (B:internal/ai/language.go:188-199); used only as a fallback after a confident message/instruction language and
   before the app language (B:internal/ai/language.go:201-230); fields in
   B:internal/transport/api/ai_handlers.go:23-26, 186, 344.
5. Voice hints: [dictation setting unless auto] + [keyboard language if kk/ru/en] + `ru`, `kk`, `en`; de-duplicated;
   first three (SUP:137-145).

### 5.2 Windows

1. On the UI thread at the moments of §5.1.2: `hkl = GetKeyboardLayout(0)` (the bar's thread). The bar is the
   foreground window at each call site (Enter, recipient chip, voice ✓), so this is the layout the user types with,
   also with "Let me use a different input method for each app window" on. Same call as API §13.1 and QB §2.5.
2. Language id **(decision, align)**: `lo = LOWORD(hkl)` (input language), `hi = HIWORD(hkl)` (keyboard device
   handle). `layoutLang = hi` when `hi != 0` and `(hi & 0xF000)` is neither 0xE000 (IME) nor 0xF000 (layout variant);
   otherwise `lo`. This returns the layout's own language, like macOS (a Kazakh keyboard added under the Russian
   language, HKL 0x043F0419, types Kazakh → `kk`). API/QB use `lo`; results differ only for such cross-language
   layouts.
3. Mapping: `PRIMARYLANGID = layoutLang & 0x3FF`: 0x09 → `en`, 0x19 → `ru`, 0x3F → `kk`, anything else → null.
   Equivalent to §5.1.3 because the server accepts only these three. Avoid `new CultureInfo(lcid)` (API §13.1): it
   throws `CultureNotFoundException` under `InvariantGlobalization`.
4. The same value feeds `input_language` and the voice hints.

| HKL | layoutLang | Result |
|---|---|---|
| 0x04090409 (US) | 0x0409 | `en` |
| 0x04190419 (Russian) | 0x0419 | `ru` |
| 0x043F043F (Kazakh) | 0x043F | `kk` |
| 0xF0020409 (US-Dvorak) | 0x0409 (variant → lo) | `en` |
| 0x043F0419 (Kazakh keyboard, Russian language) | 0x043F | `kk` |
| 0x04090419 (US keyboard, Russian language) | 0x0409 | `en` |
| 0xE0010411 (Japanese IME) | 0x0411 | null |
| 0x04070407 (German) | 0x0407 | null |

5. Pitfalls: `GetKeyboardLayout(0)` on a worker thread returns that thread's layout; do not cache through
   `WM_INPUTLANGCHANGE` — read at request time like macOS. In-bar shortcuts (Ctrl+C/V/X/A/Z, Ctrl+Shift+Z, Ctrl+Enter,
   Ctrl+R, Ctrl+D, Ctrl+1…4) match Avalonia `Key` (derived from the VK), never `KeySymbol`/text, so they work under
   ru/kk layouts (≙ key-code routing QP:36-53; QB §2.1 row 11).

### 5.3 Interface and tests

```csharp
namespace AirReply.Core.Abstractions;
public interface IKeyboardLanguage { string? Current(); }        // "kk" | "ru" | "en" | null; UI thread
namespace AirReply.Core.Hotkeys;
public static class KeyboardLanguageMap { public static string? FromHkl(nint hkl); }   // pure, table-tested
```

Tests: every row of the table; `input_language` present/absent per AGT:61-87 through the API client fake.

## 6. Start with Windows

### 6.1 macOS behaviour (normative)

1. `SMAppService.mainApp`; off until the user turns it on (PST:155-156).
2. State: `enabled`; `requiresApproval` ("Registered, but the user must allow it in System Settings → Login Items");
   `disabled` for anything else (PST:158-171).
3. `setEnabled` is idempotent against the current status: (on, enabled|requiresApproval) and (off, notRegistered|
   notFound) do nothing; else register/unregister (PST:173-185). `openLoginItemsSettings` (PST:187-189).
4. Toggle: on = state ≠ disabled; set → `try? setEnabled`, then re-read; `requiresApproval` → caption
   `settings.login_item.approval` + link `settings.open_system_settings`; state re-read when the pane appears
   (SW:70, 82-93, 121).

### 6.2 Windows (UI in QB §10.2)

1. `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, value `AI Reply` (§1.5 for other envs), `REG_SZ`
   `"<Environment.ProcessPath>" --background` (always quoted; `--background` per QB §1.1.3).
2. User override: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`, same value name,
   `REG_BINARY` 12 bytes written by Task Manager → Startup and Settings → Apps → Startup: byte 0 = 0x02 or 0x06
   enabled, 0x03 disabled (then 8 bytes FILETIME of the change). Undocumented format: odd byte 0 = disabled;
   missing/other = enabled.
3. `State`: Run value missing → `Disabled`; present and byte 0 odd → `DisabledInWindowsSettings` (≙
   `requiresApproval`); else `Enabled`.
4. `SetEnabled(true)`: `Enabled` or `DisabledInWindowsSettings` → no-op (≙ PST:178); else write the Run value.
   `SetEnabled(false)`: `Disabled` → no-op; else delete the Run value. Never write or delete `StartupApproved` (the
   user's decision in Windows).
5. Toggle ≙ §6.1.4: exceptions (`UnauthorizedAccessException`, `SecurityException`, `IOException`) are caught and
   logged by type/`HResult`, then `State` is re-read. `DisabledInWindowsSettings` → caption + button →
   `ms-settings:startupapps` (`Process.Start` with `UseShellExecute = true`). Re-read `State` whenever Settings shows.
6. `RepairCommandLine()` at every launch: Run value present and ≠ expected → rewrite (the exe moved after an update;
   `SMAppService` tracks the bundle by itself).
7. `--background`: start silently (tray only). Interactive launch behaviour is QB §1.1.3.
8. Pitfalls: Group Policy `DisableCurrentUserRun` silently ignores HKCU Run (shown as Enabled); the uninstaller
   deletes the Run value; an MSIX package would need a `StartupTask` instead.

```csharp
public enum StartupState { Disabled, Enabled, DisabledInWindowsSettings }
public interface IStartupService
{
    StartupState State { get; }
    void SetEnabled(bool enabled);    // idempotent per State; throws on registry errors (callers catch, re-read State)
    void RepairCommandLine();
    void OpenStartupSettings();       // ms-settings:startupapps
}
```

Tests (fake registry seam): every `State` row incl. bytes 0x02, 0x06, 0x03, 0x07, missing; idempotence of both
directions; repair rewrites only a differing value; quoting of a path with spaces.

## 7. Single instance and activation

1. Before Avalonia starts: `s_mutex = new Mutex(false, @"Local\AIReply-{env}", out bool createdNew)` (API §3.3); keep
   it in a static field for the process lifetime (a collected `Mutex` closes the handle and frees the name). No
   `WaitOne`, so no `AbandonedMutexException`; a crashed first instance frees the name with its handle.
2. `createdNew` → first instance. `MessageWindow` registers `AIReply.ShowBar` and allows it through UIPI:
   `ChangeWindowMessageFilterEx(hwnd, msg, MSGFLT_ALLOW 1, 0)`.
3. Not created → second instance, no UI: `--background` in args → exit 0. Else up to 20 × 100 ms:
   `hwnd = FindWindowExW(HWND_MESSAGE, 0, "AIReply.MessageWindow.{env}", null)`; found →
   `GetWindowThreadProcessId(hwnd, out pid)`; `AllowSetForegroundWindow(pid)` (QB §1.1.2 uses `ASFW_ANY`; either
   works — the second instance was just started by the user, so it may grant foreground rights); `PostMessageW(hwnd,
   showBarMsg, 0, 0)`; exit 0. Not found → exit 0.
4. First instance on `AIReply.ShowBar` → `ShowPanel()` (≙ `applicationShouldHandleReopen`, APP:24-28; visible bar →
   no-op except activation).
5. `Local\` names are per session: each RDP / fast-user-switching session has its own instance, clipboard and hotkey
   (all three are per session anyway).
6. Tests (manual): double launch shows the bar of the first; `--background` second launch shows nothing; elevated
   first + normal second still shows the bar; production and sandbox builds run side by side (the second gets
   `AlreadyTaken` for the hotkey and shows the taken note).

## 8. Secrets (DPAPI) and the installation id

### 8.1 macOS behaviour (normative)

1. `KeychainStore`: generic passwords, service + account (= key), not synchronizable,
   `kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly`; read: not found → nil, other errors throw; write: update or
   add; delete tolerates not found (SEC:16-61).
2. Keys: `session` (JSON `SessionTokens`), `device-id` (random lowercase UUID created once, "Never derived from
   hardware", replaced by the server's canonical id) (SES:33-68). Callers treat read errors as absence (`try?`,
   SES:42-45, 56-62). Service = `kz.ai-reply.desktop` (AC:36; CFG:15). Context storage: service
   `kz.ai-reply.macos.context`, key `context.profiles.encryption-key.v1` (CTX:67, 148).

### 8.2 Windows `DpapiSecretStore : ISecretStore` (layout = API §3.3)

1. Interface (WARCH:88-94): `string? Read(string key)`, `void Write(string key, string value)`,
   `void Delete(string key)`. Thread-safe (one lock): the API client reads tokens off the UI thread.
2. Key must match `^[a-z0-9][a-z0-9._-]{0,63}$`, else `ArgumentException` (no path traversal).
3. File `%LOCALAPPDATA%\AI Reply\{env}\secure\{key}.bin` = the raw DPAPI blob. `%LOCALAPPDATA%` does not roam
   (≙ ThisDeviceOnly).
4. Protect: `CryptProtectData` (CurrentUser scope = no `CRYPTPROTECT_LOCAL_MACHINE`), flags `CRYPTPROTECT_UI_FORBIDDEN`
   (0x1), optional entropy UTF-8 `AIReply/{env}/{key}/v1`, description null; free the output with `LocalFree`.
   Equivalent: `ProtectedData.Protect(bytes, entropy, DataProtectionScope.CurrentUser)` from the
   `System.Security.Cryptography.ProtectedData` package (not in the base framework of an Avalonia app); the blobs are
   interchangeable. The entropy binds a blob to its key: a file copied over another key does not decrypt.
5. Write: create the directory; write `{key}.bin.tmp` (`FileShare.None`, `Flush(true)`); `MoveFileExW(tmp, final,
   MOVEFILE_REPLACE_EXISTING 0x1 | MOVEFILE_WRITE_THROUGH 0x8)`. Zero plaintext byte arrays
   (`CryptographicOperations.ZeroMemory`).
6. Read: missing → null. `CryptUnprotectData` failure (`CryptographicException` with `ProtectedData`) — profile master
   key reset by an administrator, file from another user or PC — → log `secret_unreadable` + key name, return null,
   keep the file (the next `Write` replaces it) (≙ `try?` callers).
7. Delete: missing is not an error (≙ `errSecItemNotFound`).
8. Never store secrets in `settings.json`; never log values.

| Key | Content | Owner |
|---|---|---|
| `session` | JSON `SessionTokens` | API §3.2-3.3 |
| `device-id` | UTF-8 string | API §3.1 |
| `installation-id` | UTF-8 string | §8.3, API §3.1 |
| per-account context key(s) | context store keys, namespaced by `UserId` **(fix vs macOS, MAC-5)** | context spec (API §3.3 last rule) |

### 8.3 Installation id — storage part **(fix vs macOS, PRO-1)**

1. Key `installation-id`; value `Guid.NewGuid().ToString("D")` (lowercase, 36 chars), matching
   `^[A-Za-z0-9-]{8,64}$` (B:internal/installations/installations.go:90, 96).
2. Created at first launch (before any sign-in); never derived from hardware (≙ SES:55); kept across sign-out, account
   switch and app updates (an installation is the Pro slot, AUD:132-133; API §3.1); the uninstaller keeps it (≙
   Keychain items surviving app deletion). `device-id` and `installation-id` are both DPAPI-protected, so a profile
   folder copied to another PC decrypts neither and both are regenerated together — the restored-backup trap of
   AUD:162 (F1: old `device_id` restored, new installation) cannot occur.
3. Missing, unreadable or invalid → generate, persist, log `installation_id_regenerated`, and delete `device-id`
   (API §3.1 rule 3). A failed write keeps the in-memory value for this process and retries at the next access.
4. Network use (sign-in and refresh `device.installation_id`, `POST /api/v1/installations` right after sign-in and
   before any AI request, `X-Installation-ID`, `PRO_DEVICE_*` handling without sign-out incl. the voice path) is
   specified in API §3.1, §5.8, §12 and QB §1.7.

```csharp
public interface IInstallationId { string Value { get; } }   // stable per Windows user, machine and env
```

Tests: round trip; missing → null; corrupted blob → null; file copied between keys → null (entropy); no `.tmp` left
after `Write`; invalid key throws; `installation-id` stable across two store instances; regenerated when unreadable
and `device-id` removed.

## 9. Strings

Windows wording for `settings.login_item`, `settings.login_item.approval`, `settings.open_system_settings` and
`settings.paste_after_copy.footer` is defined in QB §11.2 (do not duplicate). Shortcut notes reuse
`settings.shortcut.fallback %@ %@`, `settings.shortcut.taken %@` and `settings.shortcut.recording` with the XCS values
unchanged (they do not mention macOS); placeholders follow QB §0.2 rule 5. New or conditional keys:

| Key | en | ru | kk (proposal, native review) | When |
|---|---|---|---|---|
| `issue.clipboard_busy` **(Win)** | Another app is using the clipboard. Try again | Буфер обмена занят другой программой. Попробуйте ещё раз | Алмасу буферін басқа бағдарлама пайдаланып тұр. Қайталап көріңіз | `CopyAsync` false (§3.7.6) |
| `settings.shortcut.system %@` | Windows already uses %@ (for example, to switch the keyboard layout). Choose another shortcut | %@ уже использует Windows (например, для смены раскладки). Выберите другое сочетание | %@ тіркесімін Windows пайдаланады (мысалы, тілді ауыстыру үшін). Басқа тіркесім таңдаңыз | only if §2.6 is adopted; else removed (QB §11.4) |

## 10. P/Invoke declarations

Project: `net10.0-windows`, `<AllowUnsafeBlocks>true</AllowUnsafeBlocks>`. All functions are UTF-16 (`…W`) entry points.
BOOL returns use `[return: MarshalAs(UnmanagedType.Bool)]`; read errors with `Marshal.GetLastPInvokeError()`.

```csharp
using System.Runtime.InteropServices;

namespace AirReply.App.Interop;

internal static unsafe partial class NativeMethods
{
    // ---- constants
    internal const uint WM_KEYDOWN = 0x0100, WM_KEYUP = 0x0101, WM_CHAR = 0x0102,
        WM_SYSKEYDOWN = 0x0104, WM_SYSKEYUP = 0x0105, WM_SYSCHAR = 0x0106,
        WM_HOTKEY = 0x0312, WM_CLIPBOARDUPDATE = 0x031D;
    internal const uint MOD_ALT = 0x1, MOD_CONTROL = 0x2, MOD_SHIFT = 0x4, MOD_WIN = 0x8, MOD_NOREPEAT = 0x4000;
    internal const int ERROR_ACCESS_DENIED = 5, ERROR_INVALID_PARAMETER = 87, ERROR_INSUFFICIENT_BUFFER = 122,
        ERROR_HOTKEY_ALREADY_REGISTERED = 1409, ERROR_REQUIRES_INTERACTIVE_WINDOWSTATION = 1459;
    internal static readonly nint HWND_MESSAGE = -3;
    internal const uint CF_UNICODETEXT = 13, CF_HDROP = 15, GMEM_MOVEABLE = 0x0002;
    internal const uint GA_ROOT = 2, GA_ROOTOWNER = 3;
    internal const uint EVENT_SYSTEM_FOREGROUND = 0x0003, WINEVENT_OUTOFCONTEXT = 0x0000;
    internal const uint INPUT_MOUSE = 0, INPUT_KEYBOARD = 1, KEYEVENTF_KEYUP = 0x0002;
    internal const uint MAPVK_VK_TO_VSC = 0;
    internal const ushort VK_SHIFT = 0x10, VK_CONTROL = 0x11, VK_MENU = 0x12, VK_ESCAPE = 0x1B,
        VK_V = 0x56, VK_LWIN = 0x5B, VK_RWIN = 0x5C, VK_PROCESSKEY = 0xE5;
    internal const uint PROCESS_QUERY_LIMITED_INFORMATION = 0x1000, TOKEN_QUERY = 0x0008;
    internal const int TokenIntegrityLevel = 25;
    internal const uint MSGFLT_ALLOW = 1, MB_OK = 0x0, CRYPTPROTECT_UI_FORBIDDEN = 0x1;
    internal const uint MOVEFILE_REPLACE_EXISTING = 0x1, MOVEFILE_WRITE_THROUGH = 0x8;
    internal const nuint InjectedInputMarker = 0x41495270;   // 'AIRp'

    // ---- structs (all blittable)
    [StructLayout(LayoutKind.Sequential)]
    internal struct WNDCLASSEXW
    {
        public uint cbSize, style;
        public delegate* unmanaged<nint, uint, nint, nint, nint> lpfnWndProc;
        public int cbClsExtra, cbWndExtra;
        public nint hInstance, hIcon, hCursor, hbrBackground;
        public char* lpszMenuName, lpszClassName;
        public nint hIconSm;
    }

    [StructLayout(LayoutKind.Sequential)]
    internal struct INPUT { public uint type; public INPUTUNION u; }          // 40 bytes on x64/ARM64

    [StructLayout(LayoutKind.Explicit)]
    internal struct INPUTUNION
    {
        [FieldOffset(0)] public MOUSEINPUT mi;                                 // largest member: keep it
        [FieldOffset(0)] public KEYBDINPUT ki;
        [FieldOffset(0)] public HARDWAREINPUT hi;
    }

    [StructLayout(LayoutKind.Sequential)]
    internal struct MOUSEINPUT { public int dx, dy; public uint mouseData, dwFlags, time; public nuint dwExtraInfo; }

    [StructLayout(LayoutKind.Sequential)]
    internal struct KEYBDINPUT { public ushort wVk, wScan; public uint dwFlags, time; public nuint dwExtraInfo; }

    [StructLayout(LayoutKind.Sequential)]
    internal struct HARDWAREINPUT { public uint uMsg; public ushort wParamL, wParamH; }

    [StructLayout(LayoutKind.Sequential)]
    internal struct SID_AND_ATTRIBUTES { public nint Sid; public uint Attributes; }

    [StructLayout(LayoutKind.Sequential)]
    internal struct TOKEN_MANDATORY_LABEL { public SID_AND_ATTRIBUTES Label; }

    [StructLayout(LayoutKind.Sequential)]
    internal struct DATA_BLOB { public uint cbData; public byte* pbData; }

    // ---- message window (user32 / kernel32)
    [LibraryImport("user32.dll", EntryPoint = "RegisterClassExW", SetLastError = true)]
    internal static partial ushort RegisterClassEx(WNDCLASSEXW* lpwcx);

    [LibraryImport("user32.dll", EntryPoint = "UnregisterClassW", SetLastError = true, StringMarshalling = StringMarshalling.Utf16)]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static partial bool UnregisterClass(string lpClassName, nint hInstance);

    [LibraryImport("user32.dll", EntryPoint = "CreateWindowExW", SetLastError = true, StringMarshalling = StringMarshalling.Utf16)]
    internal static partial nint CreateWindowEx(uint dwExStyle, string lpClassName, string? lpWindowName, uint dwStyle,
        int x, int y, int nWidth, int nHeight, nint hWndParent, nint hMenu, nint hInstance, nint lpParam);

    [LibraryImport("user32.dll", SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static partial bool DestroyWindow(nint hWnd);

    [LibraryImport("user32.dll", EntryPoint = "DefWindowProcW")]
    internal static partial nint DefWindowProc(nint hWnd, uint msg, nint wParam, nint lParam);

    [LibraryImport("user32.dll", EntryPoint = "RegisterWindowMessageW", SetLastError = true, StringMarshalling = StringMarshalling.Utf16)]
    internal static partial uint RegisterWindowMessage(string lpString);

    [LibraryImport("user32.dll", EntryPoint = "FindWindowExW", SetLastError = true, StringMarshalling = StringMarshalling.Utf16)]
    internal static partial nint FindWindowEx(nint hWndParent, nint hWndChildAfter, string? lpszClass, string? lpszWindow);

    [LibraryImport("user32.dll", EntryPoint = "PostMessageW", SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static partial bool PostMessage(nint hWnd, uint msg, nint wParam, nint lParam);

    [LibraryImport("user32.dll", SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static partial bool ChangeWindowMessageFilterEx(nint hwnd, uint message, uint action, nint pChangeFilterStruct);

    [LibraryImport("user32.dll", SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static partial bool AllowSetForegroundWindow(uint dwProcessId);

    [LibraryImport("kernel32.dll", EntryPoint = "GetModuleHandleW", SetLastError = true, StringMarshalling = StringMarshalling.Utf16)]
    internal static partial nint GetModuleHandle(string? lpModuleName);

    // ---- hotkey, keys
    [LibraryImport("user32.dll", SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static partial bool RegisterHotKey(nint hWnd, int id, uint fsModifiers, uint vk);

    [LibraryImport("user32.dll", SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static partial bool UnregisterHotKey(nint hWnd, int id);

    [LibraryImport("imm32.dll")]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static partial bool ImmGetHotKey(uint dwHotKeyID, out uint lpuModifiers, out uint lpuVKey, out nint phKL);

    [LibraryImport("user32.dll")] internal static partial short GetKeyState(int nVirtKey);
    [LibraryImport("user32.dll")] internal static partial short GetAsyncKeyState(int vKey);

    [LibraryImport("user32.dll")]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static partial bool MessageBeep(uint uType);

    [LibraryImport("user32.dll")] internal static partial nint GetKeyboardLayout(uint idThread);

    [LibraryImport("user32.dll", EntryPoint = "MapVirtualKeyExW")]
    internal static partial uint MapVirtualKeyEx(uint uCode, uint uMapType, nint dwhkl);

    [LibraryImport("user32.dll", SetLastError = true)]
    internal static partial uint SendInput(uint cInputs, INPUT* pInputs, int cbSize);

    // ---- clipboard (user32 + kernel32 global memory)
    [LibraryImport("user32.dll", SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static partial bool AddClipboardFormatListener(nint hwnd);

    [LibraryImport("user32.dll", SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static partial bool RemoveClipboardFormatListener(nint hwnd);

    [LibraryImport("user32.dll")] internal static partial uint GetClipboardSequenceNumber();

    [LibraryImport("user32.dll", SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static partial bool OpenClipboard(nint hWndNewOwner);

    [LibraryImport("user32.dll", SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static partial bool CloseClipboard();

    [LibraryImport("user32.dll", SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static partial bool EmptyClipboard();

    [LibraryImport("user32.dll", SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static partial bool IsClipboardFormatAvailable(uint format);

    [LibraryImport("user32.dll", SetLastError = true)] internal static partial nint GetClipboardData(uint uFormat);
    [LibraryImport("user32.dll", SetLastError = true)] internal static partial nint SetClipboardData(uint uFormat, nint hMem);
    [LibraryImport("user32.dll", SetLastError = true)] internal static partial nint GetClipboardOwner();

    [LibraryImport("user32.dll", EntryPoint = "RegisterClipboardFormatW", SetLastError = true, StringMarshalling = StringMarshalling.Utf16)]
    internal static partial uint RegisterClipboardFormat(string lpszFormat);

    [LibraryImport("kernel32.dll", SetLastError = true)] internal static partial nint GlobalAlloc(uint uFlags, nuint dwBytes);
    [LibraryImport("kernel32.dll", SetLastError = true)] internal static partial void* GlobalLock(nint hMem);
    [LibraryImport("kernel32.dll", SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static partial bool GlobalUnlock(nint hMem);   // FALSE + error 0 = no longer locked: not a failure
    [LibraryImport("kernel32.dll", SetLastError = true)] internal static partial nuint GlobalSize(nint hMem);
    [LibraryImport("kernel32.dll", SetLastError = true)] internal static partial nint GlobalFree(nint hMem);

    // ---- foreground, windows, threads
    [LibraryImport("user32.dll")] internal static partial nint GetForegroundWindow();

    [LibraryImport("user32.dll")]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static partial bool SetForegroundWindow(nint hWnd);

    [LibraryImport("user32.dll", SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static partial bool BringWindowToTop(nint hWnd);

    [LibraryImport("user32.dll", SetLastError = true)]
    internal static partial uint GetWindowThreadProcessId(nint hWnd, out uint lpdwProcessId);

    [LibraryImport("user32.dll")] internal static partial nint GetAncestor(nint hwnd, uint gaFlags);

    [LibraryImport("user32.dll")]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static partial bool IsWindow(nint hWnd);

    [LibraryImport("user32.dll")]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static partial bool IsIconic(nint hWnd);

    [LibraryImport("user32.dll")]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static partial bool IsHungAppWindow(nint hwnd);

    [LibraryImport("user32.dll", EntryPoint = "GetClassNameW", SetLastError = true)]
    internal static partial int GetClassName(nint hWnd, char* lpClassName, int nMaxCount);   // buffer 256 chars

    [LibraryImport("user32.dll", SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static partial bool AttachThreadInput(uint idAttach, uint idAttachTo, [MarshalAs(UnmanagedType.Bool)] bool fAttach);

    [LibraryImport("kernel32.dll")] internal static partial uint GetCurrentThreadId();

    [LibraryImport("user32.dll", SetLastError = true)]
    internal static partial nint SetWinEventHook(uint eventMin, uint eventMax, nint hmodWinEventProc,
        delegate* unmanaged<nint, uint, nint, int, int, uint, uint, void> pfnWinEventProc,
        uint idProcess, uint idThread, uint dwFlags);

    [LibraryImport("user32.dll")]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static partial bool UnhookWinEvent(nint hWinEventHook);

    // ---- process integrity (kernel32 / advapi32)
    [LibraryImport("kernel32.dll", SetLastError = true)]
    internal static partial nint OpenProcess(uint dwDesiredAccess, [MarshalAs(UnmanagedType.Bool)] bool bInheritHandle, uint dwProcessId);

    [LibraryImport("kernel32.dll")] internal static partial nint GetCurrentProcess();   // pseudo-handle, do not close

    [LibraryImport("kernel32.dll", SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static partial bool CloseHandle(nint hObject);

    [LibraryImport("advapi32.dll", SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static partial bool OpenProcessToken(nint ProcessHandle, uint DesiredAccess, out nint TokenHandle);

    [LibraryImport("advapi32.dll", SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static partial bool GetTokenInformation(nint TokenHandle, int TokenInformationClass,
        void* TokenInformation, uint TokenInformationLength, out uint ReturnLength);   // first call: size (error 122)

    [LibraryImport("advapi32.dll")] internal static partial byte* GetSidSubAuthorityCount(nint pSid);
    [LibraryImport("advapi32.dll")] internal static partial uint* GetSidSubAuthority(nint pSid, uint nSubAuthority);

    // ---- DPAPI and files (crypt32 / kernel32)
    [LibraryImport("crypt32.dll", SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static partial bool CryptProtectData(DATA_BLOB* pDataIn, char* szDataDescr, DATA_BLOB* pOptionalEntropy,
        nint pvReserved, nint pPromptStruct, uint dwFlags, DATA_BLOB* pDataOut);

    [LibraryImport("crypt32.dll", SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static partial bool CryptUnprotectData(DATA_BLOB* pDataIn, char** ppszDataDescr, DATA_BLOB* pOptionalEntropy,
        nint pvReserved, nint pPromptStruct, uint dwFlags, DATA_BLOB* pDataOut);

    [LibraryImport("kernel32.dll")] internal static partial nint LocalFree(nint hMem);       // frees pDataOut->pbData

    [LibraryImport("kernel32.dll", EntryPoint = "MoveFileExW", SetLastError = true, StringMarshalling = StringMarshalling.Utf16)]
    [return: MarshalAs(UnmanagedType.Bool)]
    internal static partial bool MoveFileEx(string lpExistingFileName, string lpNewFileName, uint dwFlags);
}
```

Usage sketches (normative where they encode a rule above):

```csharp
// §1.2: the window procedure — static, no delegate to keep alive, never throws into native code
[UnmanagedCallersOnly]
private static nint WndProc(nint hwnd, uint msg, nint wParam, nint lParam)
{
    try { if (s_window?.Handle(msg, wParam, lParam) == true) return 0; }
    catch (Exception ex) { Log.Error("message_window", ex.GetType().Name, ex.HResult); }
    return NativeMethods.DefWindowProc(hwnd, msg, wParam, lParam);
}

// §4.8: Ctrl+V into the target, one atomic SendInput call
static unsafe bool PostControlV(TargetWindow target)
{
    nint hkl = GetKeyboardLayout(target.ThreadId);
    ushort ctrl = (ushort)MapVirtualKeyEx(VK_CONTROL, MAPVK_VK_TO_VSC, hkl);
    ushort v = (ushort)MapVirtualKeyEx(VK_V, MAPVK_VK_TO_VSC, hkl);
    INPUT* inputs = stackalloc INPUT[4];
    inputs[0] = Key(VK_CONTROL, ctrl, 0);
    inputs[1] = Key(VK_V, v, 0);
    inputs[2] = Key(VK_V, v, KEYEVENTF_KEYUP);
    inputs[3] = Key(VK_CONTROL, ctrl, KEYEVENTF_KEYUP);
    return SendInput(4, inputs, sizeof(INPUT)) == 4;

    static INPUT Key(ushort vk, ushort scan, uint flags) => new()
    {
        type = INPUT_KEYBOARD,
        u = new() { ki = new() { wVk = vk, wScan = scan, dwFlags = flags, dwExtraInfo = InjectedInputMarker } },
    };
}

// §4.4.5 step 3: a no-op input makes this process the last input provider
static unsafe void Nudge() { INPUT input = default; input.type = INPUT_MOUSE; SendInput(1, &input, sizeof(INPUT)); }

// §3.6: never trust NUL termination
static unsafe string? ReadUnicodeText()
{
    nint h = GetClipboardData(CF_UNICODETEXT);
    if (h == 0) return null;
    void* p = GlobalLock(h);
    if (p == null) return null;
    try
    {
        var span = new ReadOnlySpan<char>(p, (int)Math.Min((ulong)GlobalSize(h) / 2, int.MaxValue));
        int end = span.IndexOf('\0');
        return new string(end >= 0 ? span[..end] : span);
    }
    finally { GlobalUnlock(h); }
}
```

## 11. Open questions

### 11.1 For the owner

1. Q1 Stack: WARCH says WinUI 3 + CsWin32 + PasswordVault; the current plan says Avalonia 12 + `LibraryImport` + DPAPI.
   Confirm and update WARCH (also QB §14 Q1).
2. Q2 Third fallback Ctrl+Alt+Shift+Space (parity with ⌃⌥⌘Space) — QB §1.2.3 and WARCH list two (also QB §14 Q8).
3. Q3 macOS persists a fallback, so Alt+Space is never retried after one conflict (AC:227-229). Keep parity or retry
   the default at every launch?
4. Q4 Treat a clipboard whose owner window is ours as our own copy (manual Ctrl+C inside the bar is never offered
   back; macOS offers it)?
5. Q5 Does `GetClipboardSequenceNumber` change when our read triggers delayed rendering (Excel, RDP)? The alias rule
   (§3.4.9.5) is correct either way; settle with a Windows 10 22H2 / 11 24H2 test.
6. Q6 Adopt the `ImmGetHotKey` "used by the system" probe (§2.6) or drop the feature as QB does?
7. Q7 kk wording of §9 (and QB §11.2) needs a native speaker.

### 11.2 To reconcile with the parallel specs

1. QB §2.4: restore focus **before** hiding the bar (§4.4.2), not after.
2. QB §1.2.5: add Ctrl+Tab, Ctrl+Shift+Tab, Ctrl+Space to the reserved set and use the main-key allow-list
   (§2.2.4c, §2.2.5).
3. API §13.1 / QB §2.5: `input_language` from the layout's own language (HIWORD rule, §5.2.2) and the
   `PRIMARYLANGID` table instead of `CultureInfo` (§5.2.3).
4. API §3.3 already fixes the secret layout and mutex name; this spec follows it (§1.5, §7, §8) — keep one source.
5. `input_language` is sent unconditionally like macOS, although B:internal/transport/api/ai_handlers.go:23-25 says new
   clients send it only with `features.sender_profile`; confirm for desktop.
