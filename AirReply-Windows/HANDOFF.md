# AI Reply for Windows — handoff (state on 2026-11-03)

Goal (owner): a Windows clone of the existing macOS app (`../AirReply-macOS`) that really works, with a gorgeous but
strictly native Windows 11 look (system accent, no gradients, no red except errors, calm motion), same account and
balance as the other apps. Payment stays on the website (BCC); iOS code must not be touched.

## 1. What is done (all on `main`)

| Area | State |
| --- | --- |
| Stack decision | Avalonia 12.1.5 + .NET 10 (SDK 10.0.403 pinned in `global.json`). Chosen over WinUI 3 because everything builds, renders headless to PNG and cross-publishes `win-x64`/`win-arm64` exe from macOS or Linux CI. See `Docs/ARCHITECTURE.md`. |
| Specs | `Docs/spec/` — 6,600 lines written from the macOS sources with file:line citations: `api-contract.md` (13 endpoints, headers, refresh single-flight, storage, C# sketch), `quickbar-ui.md` (every bar state with wireframes, shortcuts, string keys, settings, tray), `system-services.md` (hotkey, clipboard, focus/paste, keyboard language, autostart, single instance, DPAPI, P/Invoke block), `voice.md` (state machine, WASAPI → 16 kHz WAV, multipart upload, errors), `context.md` (profiles, import, retrieval, budgets). **`Docs/spec/DECISIONS.md` resolves every open question and wins over the specs.** |
| Design | `Docs/design/v1`, `v2`, `v3`: three directions, 9 screens × light/dark each (`renders/*.png`) with their Avalonia sources (`Mock/Theme.axaml`, `Mock/Screens/*.axaml`). The judging step was not finished. Recommendation: take **v2** (input row first, keycap hints, footer with profile chip) as the base and borrow v1's 8 px Windows 11 proportions and v3's readability/contrast; render again and compare before porting. Re-render a direction: `cd Docs/design/v2 && MOCK_OUT=$PWD/renders dotnet test --project Tests/Tests.csproj`. |
| Solution | `AirReply.Windows.slnx`: `src/AirReply.Core`, `AirReply.Context`, `AirReply.Presentation`, `AirReply.Platform.Windows`, `AirReply.App` (+ one test project each). Central versions in `Directory.Packages.props`, `NuGet.Config` maps everything to nuget.org only, `TreatWarningsAsErrors` with `latest-recommended` analyzers. Builds with 0 warnings; 12 tests pass. |
| Core base | `Localization/Localizer` + `strings.json` (all 170 macOS strings in ru/kk/en, placeholders `{0}`; `Localizer.Include(assembly, resource)` merges other modules' strings), `Infrastructure/AppInfo` (platform `windows`, User-Agent), `ISecretStore`, `IClock`, `IDataProtector` + `EphemeralDataProtector`, `Settings/AppSettings` + atomic `JsonSettingsStore`, `Generation/GenerationOptions` (`Recipient`, `GenerationContextPayload`). |
| Presentation | `Platform/*.cs` — interfaces for hotkey, clipboard, focus/paste, voice recorder, startup, shell, keyboard language, UI dispatcher. |
| App | `Program.cs`, `App.axaml`, `app.manifest` (asInvoker, PerMonitorV2, Windows 10/11), `Assets/AIReply.ico` (16–256 px, from the macOS icon). |
| CI | `../.github/workflows/desktop-windows.yml` on `windows-latest`: restore, build, test (failed tests become annotations), publish self-contained single-file `win-x64`/`win-arm64` zips as artifacts. |
| Backend | `desktop_context_generation` in `/api/v1/config`; desktop context accepted from `windows` sessions (`internal/ai/mac_context.go`, `ai_handlers.go`, tests). **Needs a backend deploy by the owner.** |

## 2. What is left (in this order)

1. **Core** (`api-contract.md`, `voice.md` §upload): `ApiClient` (HttpClient + injectable handler, snake_case JSON, Accept-Language, User-Agent, X-Request-ID, Idempotency-Key, X-Installation-ID, error envelope → typed errors), session store (DPAPI keys per environment, canonical `device_id`, stable `installation_id`), single-flight refresh incl. the 60 s lost-answer replay, email OTP sign-in/resend/sign-out, account (`/config`, `/me`, consent, balance text), generator (reply/compose; send `context`/`output_char_limit` only when chosen — audit MAC-1; compose sends no `template_id` — MAC-2; map `PRO_DEVICE_*`, `field=context`, `AI_OUTPUT_LIMIT_EXCEEDED` to their own messages), transcriber (multipart), installation registration right after sign-in (Pro readiness), payment handoff, crash reports.
2. **Context** (`context.md`): per-account encrypted profile store (`IDataProtector`), importers (xlsx/docx via OpenXml, pdf via **`PdfPig`** — never `UglyToad.PdfPig`, csv/txt with BOM/UTF-16/Windows-1251), typed import errors, retrieval with the fixes (formula cached values marked, internal columns hidden, SKU normalisation, RU/KK stemming, wide-row projection, dates in labels, conversation instructions first, exact server byte budget). Port the 62 acceptance queries and fixtures from `../docs/audits/evidence/2026-11-03-document-acceptance/` into `tests/AirReply.Context.Tests`.
3. **Platform.Windows** (`system-services.md`, `voice.md`): `LibraryImport` P/Invoke; message-only window on the UI thread; RegisterHotKey (Alt+Space → Ctrl+Alt+Space → Ctrl+Alt+Shift+Space), AddClipboardFormatListener + concealed formats, focus restore **before** hiding, SendInput Ctrl+V (40-byte `INPUT` on x64, UIPI → copy only), keyboard language, HKCU Run + StartupApproved, named-mutex single instance, DPAPI secret store/protector, NAudio.Wasapi 3.3.0 capture → 16 kHz mono PCM WAV. Every type `[SupportedOSPlatform("windows")]`; tests skip unless `OperatingSystem.IsWindows()`.
4. **Design system port**: `src/AirReply.App/Styles/Theme.axaml` (semantic brushes per theme, radii, spacing, type ramp, control classes) from the chosen direction; bar window: borderless, topmost, rounded, `TransparencyLevelHint` Mica/AcrylicBlur on Windows 11 with a solid fallback, shadow, 680 px wide, on the monitor under the cursor.
5. **View models + views** (`quickbar-ui.md`): quick bar (sign-in, consent, compose, reply with copied text + 4 recipient chips, recording, transcribing, generating, result with balance, issue cards), AI Context row + Settings → Профили, Settings window (Общие, Голос, Аккаунт, Профили, О программе), tray icon + menu, composition root with development stand-ins on non-Windows.
6. **Screenshot tests**: every state × light/dark × ru/kk/en rendered headless; review them; no clipping in Kazakh.
7. **CI green on Windows**, then manual QA on real Windows 10 and 11 (hotkey conflicts, paste into WhatsApp/Telegram desktop, mic permission off/on, DPI 100–200 %, two monitors). Code signing and a download page (`/windows` on the site) are the owner's call.

## 3. Toolchain notes

- `export PATH="$HOME/.dotnet:$PATH"`; build `dotnet build AirReply.Windows.slnx`; test `dotnet test --solution AirReply.Windows.slnx` (Microsoft.Testing.Platform via `global.json`; plain VSTest is refused by .NET 10).
- `xunit.v3` must stay **3.2.2** while `Avalonia.Headless.XUnit` is 12.1.5 (4.x breaks its discoverer with MissingMethodException).
- `NAudio.Wasapi` 22.0.0 on nuget.org is an unlisted accident — use 3.3.0. `WasapiCapture` is `[Obsolete]` in 3.3.0 (see DECISIONS V1).
- Cross-publish: `dotnet publish src/AirReply.App/AirReply.App.csproj -c Release -r win-x64 --self-contained -p:PublishSingleFile=true -p:IncludeNativeLibrariesForSelfExtract=true`.
- Pushing `.github/workflows/*` needs a token with the `workflow` scope.

## 4. Related backend/macOS work (not Windows, from `../docs/audits/2026-11-03-backend-macos-audit.md`)

- Production now: Pro shadow mode writes 1–2 `pro_device_events` rows per Pro request with no retention — throttle it or set exactly `PRO_DEVICE_LIMIT_SHADOW_MODE=false` (`off`/`no` are ignored). Keep strict Pro enforcement off: no client sends `installation_id` at sign-in yet.
- macOS 1.0.1 must not be published before fixing MAC-1…MAC-4 and RET-1/RET-2 (report §7 has the order and fixes).
