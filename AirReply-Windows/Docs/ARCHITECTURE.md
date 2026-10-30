# AI Reply for Windows — architecture

Product and interface rules are shared with the Mac app: read `../AirReply-macOS/Docs/DESIGN.md` first. This file
fixes the Windows stack, layout and the interfaces the parts are written against.

## Stack

- .NET 10 (LTS), C# latest, nullable enabled, implicit usings.
- WinUI 3 / Windows App SDK 2.6.4, unpackaged (`WindowsPackageType=None`), self-contained Windows App SDK
  (`WindowsAppSDKSelfContained=true`), `SelfContained=true`, runtime identifiers `win-x64` and `win-arm64`.
  Target `net10.0-windows10.0.22621.0`, minimum `10.0.19041.0`.
- CommunityToolkit.Mvvm 8.4.2 for view models, H.NotifyIcon.WinUI 2.4.1 for the tray icon,
  Microsoft.Windows.CsWin32 0.3.367 for Win32 interop (NativeMethods.txt). No other third-party code.
- Tests: xUnit v3 4.0.2 + Microsoft.NET.Test.Sdk 18.11.0 + xunit.runner.visualstudio 4.0.1, `net10.0`.
- CI: `.github/workflows/desktop-windows.yml` on `windows-latest` builds, tests and publishes zips.

## Layout

```
AirReply-Windows/
  AirReply.Windows.sln
  Directory.Build.props            shared settings
  src/AirReply.Core/               net10.0, no Windows APIs: API client, models, auth, limits, abstractions
  src/AirReply.App/                WinUI 3 app (tray, quick bar, settings, native services)
    App.xaml(.cs)                  startup, single instance, composition root
    QuickBar/                      floating bar window, views, QuickBarViewModel
    Settings/                      settings window
    Services/                      Windows services behind the interfaces below
    Strings/{ru,kk,en}/Resources.resw
    Assets/                        icons
  tests/AirReply.Core.Tests/       xUnit, runs anywhere
```

Namespaces follow folders: `AirReply.Core`, `AirReply.Core.Abstractions`, `AirReply.App.Services`,
`AirReply.App.QuickBar`, `AirReply.App.Settings`.

## Behaviour that differs from the Mac

- Tray icon instead of the menu bar; left click opens the bar, right click opens the menu (same items as the Mac
  menu: Open, Dictate, account, plan/balance, Plans and payment, Settings, Check for updates, Exit).
- Default shortcut Alt+Space (as in other AI quick bars); if `RegisterHotKey` fails, fall back to Ctrl+Alt+Space and
  say so in Settings.
- Quick bar: borderless always-on-top window, rounded corners (DWM), Desktop Acrylic backdrop (Mica Alt fallback,
  solid colour when transparency effects are off), 680 epx wide, on the monitor under the cursor
  (`DisplayArea.GetFromPoint`), DPI-correct. Hides when deactivated or on Esc. The previously focused window
  (`GetForegroundWindow` before showing) gets focus back when the bar hides, so Ctrl+V lands in the messenger.
- Clipboard: `AddClipboardFormatListener` on the bar's HWND; `WM_CLIPBOARDUPDATE` timestamps each change
  (`GetClipboardSequenceNumber`). Content is read only when the bar opens. Items carrying any of the formats
  `ExcludeClipboardContentFromMonitorProcessing`, `Clipboard Viewer Ignore`, `CanIncludeInClipboardHistory` = 0 or
  `CanUploadToCloudClipboard` = 0 are treated as concealed and never read.
- Optional auto-paste (off by default): `SendInput` Ctrl+V into the previous window. It cannot reach elevated windows
  (UIPI) — detect and fall back to copy only.
- Tokens: `Windows.Security.Credentials.PasswordVault` (resource `AI Reply`). Settings: JSON in
  `%LOCALAPPDATA%\AI Reply\settings.json`. Start with Windows: `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`,
  off until the user turns it on.
- Microphone: `AudioGraph` (device input → frame output node for levels + file output node, M4A/AAC 16 kHz mono).
  The microphone is open only while recording. Windows privacy settings can block it: show "Open microphone
  settings" (`ms-settings:privacy-microphone`).
- Strings: `.resw` per language (`ru` default, `kk`, `en`), loaded with
  `Microsoft.Windows.ApplicationModel.Resources.ResourceLoader`. Same keys as the Mac string catalog where possible.

## Interfaces

Core abstractions (`src/AirReply.Core/Abstractions`), shared by the app and the Core implementation:

```csharp
namespace AirReply.Core.Abstractions;

public enum DictationLanguage { Auto, Ru, Kk, En }

public enum VoiceErrorKind { PermissionDenied, NoInputDevice, RecordingFailed, TooShort, Unavailable, LimitReached, RateLimited, Offline, Server }

public sealed class VoiceException(VoiceErrorKind kind, string? serverCode = null) : Exception(kind.ToString())
{
    public VoiceErrorKind Kind { get; } = kind;
    public string? ServerCode { get; } = serverCode;
}

/// A finished recording on disk; the recorder deletes it after use.
public sealed record RecordedAudio(string FilePath, TimeSpan Duration, string ContentType, string FileName);

/// Uploads a recording to the AI Reply server and returns the text. Throws VoiceException.
public interface ITranscriber
{
    Task<string> TranscribeAsync(RecordedAudio audio, DictationLanguage language, CancellationToken cancellationToken);
}

/// Tokens and other small secrets.
public interface ISecretStore
{
    string? Read(string key);
    void Write(string key, string value);
    void Delete(string key);
}
```

App services (`src/AirReply.App/Services`):

```csharp
namespace AirReply.App.Services;

[Flags] public enum HotKeyModifiers : uint { None = 0, Alt = 1, Control = 2, Shift = 4, Win = 8 }

public readonly record struct KeyGesture(uint VirtualKey, HotKeyModifiers Modifiers)
{
    public static KeyGesture AltSpace { get; }          // Alt+Space
    public static KeyGesture ControlAltSpace { get; }   // Ctrl+Alt+Space
    public string DisplayString { get; }                // "Alt+Space"
    public bool IsAllowed { get; }                      // needs Alt or Ctrl (or Win); rejects reserved combos
}

public enum HotKeyResult { Ok, NotAllowed, AlreadyTaken, Failed }

public interface IHotKeyService : IDisposable
{
    event EventHandler? Pressed;
    KeyGesture? Current { get; }
    HotKeyResult Register(KeyGesture gesture);   // replaces the previous one
    void Unregister();
}

public sealed record CopiedText(string Text, uint SequenceNumber, DateTimeOffset ObservedAt);
public enum CopiedTextStatus { None, Text, Concealed, Denied }
public readonly record struct CopiedTextResult(CopiedTextStatus Status, CopiedText? Copied);

public interface IClipboardService : IDisposable
{
    void Attach(nint hwnd);                              // starts listening on this window
    CopiedTextResult TakeFreshCopiedText(TimeSpan maxAge);
    void MarkHandled(CopiedText copied);
    Task CopyAsync(string text);                         // remembered as our own, never offered back
}

public enum VoicePhase { Idle, RequestingPermission, Recording, Transcribing, Finished, Failed }

public interface IVoiceRecorder : IAsyncDisposable
{
    event EventHandler? Changed;                         // raised on the UI thread
    VoicePhase Phase { get; }
    string? Text { get; }                                // when Finished
    VoiceErrorKind? Error { get; }                       // when Failed
    IReadOnlyList<float> Levels { get; }                 // 40 values 0..1, oldest first
    TimeSpan Elapsed { get; }
    TimeSpan MaxDuration { get; set; }
    Task StartAsync(DictationLanguage language);
    Task StopAsync();                                    // stop and transcribe
    void Cancel();                                       // drop audio
    void Reset();
}

public interface IFocusTracker
{
    void RememberForegroundWindow();                     // before the bar shows; ignores our own windows
    nint PreviousWindow { get; }
    void RestorePreviousWindow();
}

public enum PasteAvailability { Available, TargetElevated, NoTarget }

public interface IPasteService
{
    PasteAvailability GetAvailability(nint targetWindow);
    Task<bool> PasteAsync(nint targetWindow);            // Ctrl+V via SendInput
}

public interface IStartupService
{
    bool IsEnabled { get; }
    void SetEnabled(bool enabled);
}

public enum PanelPlacement { Top, Bottom, Remembered }
public enum AppTheme { System, Light, Dark }

public sealed class AppSettings
{
    public KeyGesture Shortcut { get; set; } = KeyGesture.AltSpace;
    public PanelPlacement Placement { get; set; } = PanelPlacement.Top;
    public double? RememberedX { get; set; }             // 0..1 of the work area
    public double? RememberedY { get; set; }
    public AppTheme Theme { get; set; } = AppTheme.System;
    public bool OfferCopiedText { get; set; } = true;
    public bool CloseAfterCopy { get; set; } = true;
    public bool PasteAfterCopy { get; set; }
    public DictationLanguage DictationLanguage { get; set; } = DictationLanguage.Auto;
    public string LastRecipient { get; set; } = "client";
}

public interface ISettingsStore
{
    AppSettings Load();
    void Save(AppSettings settings);
}
```

The API client, account and generation types in `AirReply.Core` mirror the Mac `AirReplyCore` module and the
shared contract in `../docs/desktop-api-contract.md`.
