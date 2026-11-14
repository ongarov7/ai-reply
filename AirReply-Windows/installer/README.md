# Direct-download Windows installer

`AIReply.iss` produces one `AIReply-Setup.exe` containing both self-contained .NET payloads.
The 32-bit Setup bootstrap selects ARM64 with `IsArm64`; all other permitted systems receive x64.
Only Windows 10/11 x64 or ARM64 are permitted. No administrator rights or separate .NET install is required.

Build from prepared `out/win-x64/AIReply.exe` and `out/win-arm64/AIReply.exe`:

```powershell
./scripts/Build-Installer.ps1 -IsccPath 'C:\tools\Inno Setup 6\ISCC.exe'
```

The script accepts `-OutputRoot` (relative to `AirReply-Windows` or absolute) and an optional numeric
`-AppVersion`; by default the version comes from `Directory.Build.props`. It checks the native PE machine
type of both inputs, compiler exit status and output existence, then writes `AIReply-Setup.exe.sha256`.
`Get-InnoSetup.ps1` acquires the pinned Inno Setup 6.7.4 compiler in CI; the compiler is not bundled in the app.

The stable AppId is `{7AEBA055-83B4-4B99-A30B-92E5C9C0C5D0}`. Both native architectures share one
per-user installation and uninstall registration, so reinstalling/upgrading replaces the existing app.
The default path is `%LOCALAPPDATA%\Programs\AI Reply`; account data remains separately under
`%LOCALAPPDATA%\AI Reply` and is never removed by the uninstaller.

Start-menu shortcut and Apps/Installed apps entry are created automatically; a desktop shortcut is optional.
Autostart remains an in-app preference. Uninstall removes Run/StartupApproved entries only if the Run command
points to this installed executable. It preserves preferences owned by a different portable copy.

The installer and uninstaller refuse to change a running app while `Local\AIReply-production` exists.
Users exit AI Reply from its tray menu before updating/removing it; Setup does not force-kill the process.
Silent installations use `/VERYSILENT /SUPPRESSMSGBOXES /NORESTART /SP-`; silent installs never launch the app.

No release certificate is configured. The generated installer is unsigned and remains subject to Windows
download/reputation warnings. Native payload selection on actual Windows ARM64 and interactive installation
still require manual Windows QA in addition to the automated silent install/upgrade/uninstall smoke checks.

Compiler licensing: Inno Setup's [official license](https://jrsoftware.org/files/is/license.txt) permits use
for any purpose, including commercial applications. The upstream purchase page requests financial support;
this project does not remove Inno Setup's embedded copyright or website information.
