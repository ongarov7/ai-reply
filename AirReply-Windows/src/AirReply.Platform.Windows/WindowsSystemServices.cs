using System.Diagnostics;
using System.Runtime.Versioning;
using Microsoft.Win32;
using AirReply.Presentation.Platform;

namespace AirReply.Platform.Windows;

[SupportedOSPlatform("windows")]
public sealed class WindowsShell : IShell
{
    public void OpenUri(Uri uri)
    {
        ArgumentNullException.ThrowIfNull(uri);
        if (!uri.IsAbsoluteUri || uri.Scheme is not ("https" or "http" or "ms-settings" or "mailto"))
            throw new ArgumentException("Unsupported URI scheme.", nameof(uri));
        using var process = Process.Start(new ProcessStartInfo(uri.AbsoluteUri) { UseShellExecute = true });
    }
}

public enum WindowsStartupState { Disabled, Enabled, DisabledInWindowsSettings }

[SupportedOSPlatform("windows")]
public sealed class WindowsStartupService : IStartupService
{
    private const string RunPath = @"Software\Microsoft\Windows\CurrentVersion\Run";
    private const string ApprovedPath = @"Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run";
    private readonly string valueName;
    private readonly string executable;
    public WindowsStartupService(string environment = "production", string? executablePath = null)
    {
        WindowsMessageWindow.ValidateEnvironment(environment);
        valueName = environment == "production" ? "AI Reply" : $"AI Reply ({environment})";
        executable = executablePath ?? Environment.ProcessPath ?? throw new InvalidOperationException("Executable path is unavailable.");
    }
    public bool IsEnabled => State != WindowsStartupState.Disabled;
    public WindowsStartupState State
    {
        get
        {
            using var run = Registry.CurrentUser.OpenSubKey(RunPath);
            if (run?.GetValue(valueName) is not string) return WindowsStartupState.Disabled;
            using var approved = Registry.CurrentUser.OpenSubKey(ApprovedPath);
            var bytes = approved?.GetValue(valueName) as byte[];
            return bytes is { Length: > 0 } && (bytes[0] & 1) != 0 ? WindowsStartupState.DisabledInWindowsSettings : WindowsStartupState.Enabled;
        }
    }
    internal static string CommandLine(string path) => $"\"{path}\" --background";
    public void SetEnabled(bool enabled)
    {
        if (enabled == IsEnabled) return;
        using var key = Registry.CurrentUser.CreateSubKey(RunPath);
        if (enabled) key.SetValue(valueName, CommandLine(executable), RegistryValueKind.String);
        else key.DeleteValue(valueName, false);
    }
    public void RepairCommandLine()
    {
        using var key = Registry.CurrentUser.OpenSubKey(RunPath, true);
        if (key?.GetValue(valueName) is string existing && existing != CommandLine(executable))
            key.SetValue(valueName, CommandLine(executable), RegistryValueKind.String);
    }
    public static void OpenStartupSettings() => new WindowsShell().OpenUri(new Uri("ms-settings:startupapps"));
}

[SupportedOSPlatform("windows")]
public sealed class WindowsSingleInstance : IDisposable
{
    private readonly Mutex mutex;
    private WindowsSingleInstance(Mutex mutex) => this.mutex = mutex;
    public static bool TryAcquire(string environment, out WindowsSingleInstance? instance)
    {
        WindowsMessageWindow.ValidateEnvironment(environment);
        var mutex = new Mutex(false, $@"Local\AIReply-{environment}", out var created);
        if (!created) { mutex.Dispose(); instance = null; return false; }
        instance = new WindowsSingleInstance(mutex);
        return true;
    }
    public static void NotifyExisting(string environment)
    {
        WindowsMessageWindow.ValidateEnvironment(environment);
        var message = NativeMethods.RegisterWindowMessage("AIReply.ShowBar");
        for (var attempt = 0; attempt < 20; attempt++)
        {
            var window = NativeMethods.FindWindowEx(-3, 0, WindowsMessageWindow.ClassName(environment), null);
            if (window != 0)
            {
                NativeMethods.GetWindowThreadProcessId(window, out var process);
                NativeMethods.AllowSetForegroundWindow(process);
                NativeMethods.PostMessage(window, message, 0, 0);
                return;
            }
            Thread.Sleep(100);
        }
    }
    public void Dispose() => mutex.Dispose();
}
