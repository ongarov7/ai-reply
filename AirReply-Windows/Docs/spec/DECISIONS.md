# Decisions that resolve the specs' open questions (2026-11-03)

The five specs in this folder were written in parallel. Where they disagree or leave a question open, this file is
authoritative. The owner was away; every decision below is conservative and easy to revisit.

## Stack and contracts

| # | Question | Decision |
| --- | --- | --- |
| D1 | WinUI 3 + CsWin32 + PasswordVault (old WARCH) vs Avalonia | **Avalonia 12.1.5, .NET 10, `LibraryImport`, DPAPI files.** `../ARCHITECTURE.md` is current. |
| D2 | Context capability flag name | **`features.desktop_context_generation`** (implemented on this branch: backend accepts `windows` in `mac_context.go` and `ai_handlers.go`). Not `windows_context_generation`. AI Context UI is hidden while the flag is absent/false. |
| D3 | Extended length without context; server counts the saved mobile profile | Send `output_char_limit` only. A `400 INVALID_REQUEST field=context` maps to a dedicated "request too long — shorten the message or instructions" issue, never to the generic server error with Retry. |
| D4 | No `/windows` page on the website | "Check for updates" and the download link open `{base}/` until the owner publishes a Windows page (one constant `WebsitePaths.Windows`). |
| D5 | Installation registration timing | Right after sign-in, before consent and before any AI request (PRO-1). Registration is not an AI request. |
| D6 | Crash reports | Anonymous, like macOS. |
| D7 | Kazakh wording of new keys | Implementers write kk; it is flagged for native review in the final report. |
| D8 | `input_language` | Derived from the foreground window's keyboard layout (system-services §5.2 rule) and sent while `features.sender_profile` is true. |

## Hotkey, clipboard, focus

| # | Question | Decision |
| --- | --- | --- |
| H1 | Fallbacks | Alt+Space → Ctrl+Alt+Space → Ctrl+Alt+Shift+Space. |
| H2 | Persisting a fallback | Never overwrite the user's chosen gesture with a fallback; retry the chosen one at every launch and show "X занято, поэтому используется Y" in Settings. |
| H3 | `ImmGetHotKey` "used by system" probe | Dropped. |
| H4 | Reserved combos | system-services §2.2 is authoritative (includes Ctrl+Tab, Ctrl+Shift+Tab, Ctrl+Space). |
| H5 | Clipboard owned by our own window | Treated as our own copy — never offered back. |
| H6 | Focus order on hide | Restore focus to the previous window **before** hiding the bar (system-services §4.4.2). |

## Quick bar behaviour

| # | Question | Decision |
| --- | --- | --- |
| Q | Quirk fixes Q1–Q9 (quickbar-ui §13.2) | All approved. |
| B1 | Compose recipient | Compose sends no `template_id` (mobile parity; MAC-2). Reply uses the four recipient chips: friend / client / work (Коллега) / business (Партнёр). |
| B2 | MAC-7, MAC-9, MAC-11 | Fixed on Windows: confirmation before deleting a profile or document and before discarding unsaved edits; per-conversation instructions and Extended length reset after Copy and on a new conversation; every icon button has an accessible name (documents include the file name). |
| B3 | Interactive launch | A user-started launch shows the bar (sign-in on first run); autostart (`--background`) stays in the tray. |
| B4 | Sign-out and saved profiles | Profiles stay on disk, namespaced per account (another account never sees them). Settings → Профили has "Удалить профили этого аккаунта с компьютера". |
| B5 | Newline keys | Alt+Enter and Shift+Enter insert a newline; Enter submits. |
| B6 | Reply with an empty manual source | Focus the source field. |
| B7 | Reply-mode instruction counter | Shown, same rule as compose. |

## AI Context, documents, voice

| # | Question | Decision |
| --- | --- | --- |
| C1 | Conversation instructions | Max 500 characters, budget reserved first (context spec numbers are authoritative). |
| C2 | Product deviations from macOS | Approved: hide internal columns (purchase price, margin, supplier, manager comments), use cached formula values marked " (формула)", accept owner-password-only PDFs. |
| C3 | PDF isolation | In-process on a background thread with the spec's limits and a timeout; no helper process in v1. |
| V1 | NAudio API | Prefer the non-obsolete NAudio 3.3 capture API when it supports the needs; otherwise `WasapiCapture` with a targeted `#pragma warning disable` and a comment. Default device role: Console. |
| V2 | Offline during upload | No automatic retry (macOS parity); the issue card offers retry by recording again. Client upload timeout 75 s. |
