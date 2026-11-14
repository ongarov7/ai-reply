using System.Runtime.InteropServices;
using System.Runtime.Versioning;
using AirReply.Presentation.Platform;

namespace AirReply.Platform.Windows;

[SupportedOSPlatform("windows")]
public sealed unsafe class WindowsFocusTracker : IFocusTracker, IDisposable
{
    private static readonly Dictionary<nint, WindowsFocusTracker> Hooks = [];
    private readonly nint hook;
    private nint lastReal;
    private nint previous;
    private uint previousProcess, previousThread;
    /// <summary>Register Settings HWND here so opening the bar from Settings does not paste into an older app.</summary>
    public nint SettingsWindow { get; set; }
    public nint PreviousWindow => IsPreviousAlive ? previous : 0;
    private bool IsPreviousAlive => previous != 0 && NativeMethods.IsWindow(previous) &&
        NativeMethods.GetWindowThreadProcessId(previous, out var process) == previousThread && process == previousProcess;
    public WindowsFocusTracker()
    {
        Observe(NativeMethods.GetForegroundWindow());
        hook = NativeMethods.SetWinEventHook(3, 3, 0, &OnForeground, 0, 0, 0);
        if (hook != 0) Hooks.Add(hook, this);
    }
    [UnmanagedCallersOnly]
    private static void OnForeground(nint hook, uint eventId, nint window, int objectId, int childId, uint thread, uint time)
    {
        try { if (objectId == 0 && Hooks.TryGetValue(hook, out var tracker)) tracker.Observe(window); }
        catch (Exception exception) { System.Diagnostics.Trace.TraceError("Focus callback: {0} ({1:X8})", exception.GetType().Name, exception.HResult); }
    }
    private void Observe(nint window)
    {
        if (window != 0 && !IsTransparent(window)) lastReal = NativeMethods.GetAncestor(window, 2);
    }
    private bool IsTransparent(nint window)
    {
        var root = NativeMethods.GetAncestor(window, 2);
        NativeMethods.GetWindowThreadProcessId(root, out var process);
        if (process == Environment.ProcessId)
            return SettingsWindow == 0 || NativeMethods.GetAncestor(root, 3) != NativeMethods.GetAncestor(SettingsWindow, 3);
        Span<char> buffer = stackalloc char[256];
        fixed (char* pointer = buffer)
        {
            var length = NativeMethods.GetClassName(root, pointer, buffer.Length);
            return new string(buffer[..Math.Clamp(length, 0, buffer.Length)]) is "Shell_TrayWnd" or "Shell_SecondaryTrayWnd" or
                "NotifyIconOverflowWindow" or "TopLevelWindowForOverflowXamlIsland" or "Windows.UI.Core.CoreWindow" or
                "Shell_InputSwitchTopLevelWindow" or "XamlExplorerHostIslandWindow" or "MultitaskingViewFrame" or "ForegroundStaging";
        }
    }
    public void RememberForegroundWindow()
    {
        var foreground = NativeMethods.GetForegroundWindow();
        if (foreground == 0 || IsTransparent(foreground)) foreground = lastReal;
        previous = 0;
        if (foreground == 0 || !NativeMethods.IsWindow(foreground)) return;
        var root = NativeMethods.GetAncestor(foreground, 2);
        var thread = NativeMethods.GetWindowThreadProcessId(root, out var process);
        if (process == Environment.ProcessId) return;
        previous = root;
        previousProcess = process;
        previousThread = thread;
    }
    /// <summary>Call before hiding the bar, while it still owns foreground activation rights.</summary>
    public bool RestorePreviousWindow() => IsPreviousAlive && !NativeMethods.IsIconic(previous) && Activate(previous);
    internal static bool IsForeground(nint target) => target != 0 &&
        NativeMethods.GetAncestor(NativeMethods.GetForegroundWindow(), 3) == NativeMethods.GetAncestor(target, 3);
    internal static bool Activate(nint target)
    {
        if (!NativeMethods.IsWindow(target) || NativeMethods.IsIconic(target)) return false;
        if (IsForeground(target)) return true;
        NativeMethods.SetForegroundWindow(target);
        if (IsForeground(target)) return true;
        NativeMethods.Input nudge = default;
        NativeMethods.SendInput(1, &nudge, sizeof(NativeMethods.Input));
        NativeMethods.SetForegroundWindow(target);
        if (IsForeground(target)) return true;
        var foreground = NativeMethods.GetForegroundWindow();
        if (foreground == 0 || NativeMethods.IsHungAppWindow(foreground)) return false;
        var currentThread = NativeMethods.GetCurrentThreadId();
        var foregroundThread = NativeMethods.GetWindowThreadProcessId(foreground, out _);
        var attached = foregroundThread != currentThread && NativeMethods.AttachThreadInput(currentThread, foregroundThread, true);
        try { NativeMethods.SetForegroundWindow(target); NativeMethods.BringWindowToTop(target); }
        finally { if (attached) NativeMethods.AttachThreadInput(currentThread, foregroundThread, false); }
        return IsForeground(target);
    }
    public void Dispose()
    {
        if (hook == 0) return;
        Hooks.Remove(hook);
        NativeMethods.UnhookWinEvent(hook);
    }
}
