# AI Reply for Windows — architecture

Product and interface rules are shared with the Mac app (`../AirReply-macOS/Docs/DESIGN.md`). The exact behaviour the
Windows app reproduces is specified in `spec/` (written from the macOS sources, with the audit fixes of
`docs/audits/2026-11-03-backend-macos-audit.md` applied). This file fixes the stack, layout, dependency rules and the
seams between the parts.

## Stack (decided 2026-11-03)

- .NET 10 (SDK pinned in `global.json`), C# latest, nullable, `TreatWarningsAsErrors`, analyzers `latest-recommended`.
- UI: **Avalonia 12.1.5** with `FluentTheme` and our design system (`src/AirReply.App/Styles`). Chosen over WinUI 3 because
  the whole app (UI included) builds, runs and renders headless on any OS: every screen is rendered to PNG in tests
  (light/dark × ru/kk/en) and the Windows binaries are cross-published from macOS or CI. On Windows 11 the bar uses the
  Mica/Acrylic backdrop (`TransparencyLevelHint`), rounded corners and the system accent.
- CommunityToolkit.Mvvm 8.4.2 (view models), DocumentFormat.OpenXml 3.5.1 (xlsx/docx), PdfPig 0.1.16 (PDF text),
  NAudio.Wasapi 3.3.0 (microphone), System.Security.Cryptography.ProtectedData 10.0.14 (DPAPI). Nothing else.
  `NuGet.Config` maps every package to nuget.org only.
- Tests: xUnit v3 3.2.2 on Microsoft.Testing.Platform (`dotnet test --solution AirReply.Windows.slnx`),
  Avalonia.Headless.XUnit for UI. Win32 tests run on Windows only (CI `windows-latest`).
- Distribution: self-contained single-file `AIReply.exe` for `win-x64` and `win-arm64` (CI artifact). Not signed yet.

## Layout and dependency rules

```
AirReply-Windows/
  AirReply.Windows.slnx, global.json, NuGet.Config, Directory.Build.props, Directory.Packages.props
  src/AirReply.Core/              API contract, session, account, generation, voice upload, installation identity,
                                  crash reports, localization (strings.json), settings model. No UI, no Win32.
  src/AirReply.Context/           AI Context: per-account encrypted profiles, local document import, retrieval.
  src/AirReply.Presentation/      View models + platform-service interfaces. No Avalonia, no Win32.
  src/AirReply.Platform.Windows/  Win32/NAudio/DPAPI implementations of the Presentation interfaces.
  src/AirReply.App/               Avalonia views, windows, tray, design system, composition root (Program/App).
  tests/<Project>.Tests/          one test project per source project.
  Docs/                           ARCHITECTURE.md, DESIGN-WINDOWS.md, spec/*.md
```

`Core ← Context ← Presentation ← Platform.Windows ← App` (arrows point at dependencies). Core and Context know nothing
about UI; Presentation knows nothing about Avalonia or Win32; only App references Avalonia; only Platform.Windows calls
Win32. Every Win32 entry point is `[SupportedOSPlatform("windows")]` and constructed only when
`OperatingSystem.IsWindows()`; on other systems App composes development stand-ins so the UI can run for screenshots.

## Threading

- View models are created and mutated on the UI thread only (`IUiDispatcher`). Long work (HTTP, parsing, encryption)
  runs off the UI thread; results are marshalled back. No `.Result`/`.Wait()`.
- Win32 message hooks (hotkey, clipboard) live on one hidden message-only window owned by the UI thread.
- One `CancellationTokenSource` per user action; Esc/close cancels it. Late results after cancellation are dropped.

## Security and privacy

- Tokens, device id, installation id: DPAPI (CurrentUser) protected file under `%LOCALAPPDATA%\AI Reply\`.
- AI Context profiles: one encrypted file per account id (DPAPI), atomic replace, never shared between accounts.
- Logs and crash reports carry codes and request ids only — never message text, replies, audio, documents or tokens.
- Clipboard items marked concealed (password managers) are never read. Audio files are deleted after upload/cancel.

## Backend contract notes

- Platform value `windows` everywhere (sign-in device, installation registration, consent, AI requests).
- Sign-in/refresh device payload carries a stable `installation_id`; the app registers the installation
  (`POST /api/v1/installations`) right after sign-in, before the first AI request (Pro device limit readiness).
- AI Context is enabled only when `/api/v1/config` sends `features.desktop_context_generation = true`; `context`,
  `output_char_limit` and `template_id` are sent only when the user actually chose context or a length.
