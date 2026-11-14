# AI Reply for Windows

Avalonia 12.1.5 / .NET 10 portable desktop client. The production endpoint is `https://ai-reply.kz`;
the application never deploys or changes the backend. macOS, mobile and website sources are unchanged.

Implemented: email OTP, DPAPI sessions and single-flight refresh, installation registration, consent,
shared account/balance, compose/reply, retry/regenerate, copy and optional paste, tray/single instance,
hotkey fallbacks, startup, ru/kk/en and system/light/dark appearance, WASAPI dictation, local encrypted
profiles, XLSX/CSV/PDF/DOCX/TXT import and bounded lexical retrieval, website payment handoff, anonymous
managed crash reports. AI Context and voice follow server capability flags.

Run commands **from `AirReply-Windows`** so `global.json` selects Microsoft.Testing.Platform:

```sh
dotnet restore AirReply.Windows.slnx
dotnet build AirReply.Windows.slnx -c Release --no-restore
dotnet test --solution AirReply.Windows.slnx -c Release --no-build \
  --report-xunit-trx --results-directory out/test-results --results-directory-layout per-module
```

`--report-xunit-trx` is the reporter supplied by xUnit 3.2.2. `--report-trx` requires an extension
that this solution does not install. App tests render 11 states × 3 languages × 2 themes to
`tests/AirReply.App.Tests/bin/Release/net10.0/renders`. Test HTTP handlers never send OTP/AI/payment
requests to production. Native tests report a real skip outside Windows.

Package on Windows with PowerShell:

```powershell
./scripts/Publish-Windows.ps1
```

The script builds self-contained single-file executables, checks exit codes and `AIReply.exe`, then
creates `out/AIReply-win-x64.zip`, `out/AIReply-win-arm64.zip` and SHA-256 sidecars. GitHub Actions uses
the same script. Cross-publishing is supported from macOS/Linux with the `dotnet publish` commands
inside that script; creating ZIPs does not require a Windows runtime.

Run `AIReply.exe` interactively to open the bar; `--background` starts in the tray.
`--preview` uses development stand-ins without production networking or persistent credentials.
Non-Windows execution always uses these stand-ins. Tests inject functional fakes separately.

Manual Windows testing is still required: see [QA-WINDOWS.md](Docs/QA-WINDOWS.md).
This is an unsigned preview; it does not change the macOS 1.0.2 release or production.
Document corpus expectations and lexical limitations are explicit in
[the context test README](tests/AirReply.Context.Tests/README.md).
