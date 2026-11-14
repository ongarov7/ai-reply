# AI Reply for Windows — handoff (2026-11-14)

Base: origin/main `819936a`. Work branch: `codex/windows-client`.
macOS 1.0.2, iOS, Android, backend, website and production are unchanged.
`Docs/spec/DECISIONS.md` remains authoritative.

## Implemented

- Core: typed API/errors, snake_case/header contracts, OTP/resend/sign-out, DPAPI sessions with canonical
  device/installation IDs and environment isolation, single-flight refresh and 60 s lost-answer replay,
  registration before consent/AI, consent/balance/config, reply/compose, optional context/600 fields,
  multipart WAV transcription, safe payment handoff, anonymous managed crash reporting.
- Context: encrypted per-account atomic profile/document store; safe XLSX/CSV/PDF/DOCX/TXT import,
  encoding/ZIP/XML/content limits, cached formula values/internal-column filtering, SKU/RU/KK lexical
  retrieval, wide-row projection/stock joins and exact escaped UTF-8 budget. ContextWorkspace serializes
  operations and immediately clears private state on account switch. Metadata saves preserve imports.
- Windows: message-only window/hotkey fallbacks, concealed/own clipboard filtering, foreground tracker,
  restore-before-hide, UIPI-aware SendInput paste, keyboard language, DPAPI, startup, single instance,
  cursor placement, power/session notifications, WASAPI 16 kHz mono WAV with 40 s cap.
- App: functional quick bar, sign-in/consent/compose/reply/generate/result/error/voice states, tray,
  settings (General/Voice/Account/Profiles/About), profile editing/import/deletion confirmations,
  system/light/dark, ru/kk/en, optional paste, placement/DPI math and native drag completion hook.
  Interactive launch opens; `--background` stays hidden; `--preview` never uses production.
- CI: xUnit's actual `--report-xunit-trx` replaces unsupported `--report-trx`; per-module results,
  failure annotations, checked publish script, self-contained x64/ARM64 ZIPs + SHA-256 and test screenshots.

## Verification on macOS

Release build: 0 warnings / 0 errors. Full solution: **231 tests, 225 passed, 6 explicitly skipped**
(native Win32 tests run only on Windows). Core 42, Context 90, Presentation 43, Platform 40, App 16.
66 screenshots (11 states × ru/kk/en × light/dark) are generated headlessly; representative images reviewed.
No test sends real OTP/AI/payment requests. Corpus deviations are documented in
`tests/AirReply.Context.Tests/README.md`; passing 62 does not mean semantic translation/OCR is implemented.

Run from this directory:

```
dotnet build AirReply.Windows.slnx -c Release
dotnet test --solution AirReply.Windows.slnx -c Release --no-build --report-xunit-trx --results-directory out/test-results --results-directory-layout per-module
```

PowerShell packaging: `./scripts/Publish-Windows.ps1`, then `Get-InnoSetup.ps1` and
`Build-Installer.ps1 -IsccPath <compiler>`. The new `AIReply-Setup.exe` contains both architectures and
supports per-user installation, shortcuts, upgrade guard, removal and preservation of account data.
CI runs real silent install/preview launch/reinstall/uninstall checks using `Test-Installer.ps1`.
See README.md and README-Windows.txt.

## Release gate / remaining limits

Do not infer working messenger/mic/DPI behavior merely from cross-publishing or headless tests.
Execute `Docs/QA-WINDOWS.md` on Windows 10/11 and actual ARM64 hardware. Check CI artifacts for the
final branch commit. Code signing/public Windows download page and production deployment remain the owner's call.

Known limits: unbenchmarked 200k-segment retrieval, no LRU cache/translation/OCR; PDF cancellation is
cooperative between pages, not hard process isolation. Kazakh additions need native review. Native Windows
Event Log crash import is not implemented. Managed crash reports contain exception types/HResults only.
WAV removal after force-kill cannot be guaranteed; inspect cleanup during Windows QA. Installation registration
cache is in-memory (safe re-registration on restart). High contrast/acrylic/native accent fidelity needs OS review.
