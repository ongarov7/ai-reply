using System.ComponentModel;
using System.Runtime.InteropServices;
using System.Runtime.Versioning;

namespace AirReply.Platform.Windows;

/// <summary>Stable native owner for hotkey, clipboard and second-launch activation. Construct/dispose on the UI thread.</summary>
[SupportedOSPlatform("windows")]
public sealed unsafe class WindowsMessageWindow : IDisposable
{
    private static readonly Dictionary<nint, WindowsMessageWindow> Windows = [];
    private readonly string className;
    private readonly nint instance;
    private readonly uint thread;
    private readonly uint showMessage;
    private readonly nint suspendRegistration;
    private readonly bool sessionRegistered;
    public nint Handle { get; private set; }
    public event EventHandler? ShowRequested;
    public event EventHandler? SuspendOrLock;
    public event EventHandler? Resumed;
    internal event Action<uint, nint, nint>? Message;

    public WindowsMessageWindow(string environment = "production")
    {
        ValidateEnvironment(environment);
        thread = NativeMethods.GetCurrentThreadId();
        className = ClassName(environment);
        instance = NativeMethods.GetModuleHandle(null);
        fixed (char* name = className)
        {
            var definition = new NativeMethods.WindowClass
            {
                Size = (uint)sizeof(NativeMethods.WindowClass), Procedure = &WindowProcedure,
                Instance = instance, ClassName = name,
            };
            if (NativeMethods.RegisterClass(&definition) == 0)
                throw new Win32Exception(Marshal.GetLastPInvokeError());
        }
        Handle = NativeMethods.CreateWindow(0, className, "AI Reply", 0, 0, 0, 0, 0, -3, 0, instance, 0);
        if (Handle == 0)
        {
            NativeMethods.UnregisterClass(className, instance);
            throw new Win32Exception(Marshal.GetLastPInvokeError());
        }
        Windows.Add(Handle, this);
        showMessage = NativeMethods.RegisterWindowMessage("AIReply.ShowBar");
        NativeMethods.ChangeWindowMessageFilterEx(Handle, showMessage, 1, 0);
        suspendRegistration = NativeMethods.RegisterSuspendResumeNotification(Handle, 0);
        sessionRegistered = NativeMethods.WTSRegisterSessionNotification(Handle, 0);
    }

    internal static string ClassName(string environment) => $"AIReply.MessageWindow.{environment}";
    internal static void ValidateEnvironment(string environment)
    {
        ArgumentException.ThrowIfNullOrWhiteSpace(environment);
        if (environment.Length > 64 || environment.Any(c => !char.IsAsciiLetterOrDigit(c) && c is not '-' and not '_'))
            throw new ArgumentException("Invalid environment identifier.", nameof(environment));
    }
    internal void VerifyThread()
    {
        if (thread != NativeMethods.GetCurrentThreadId())
            throw new InvalidOperationException("Native window services must run on their owning UI thread.");
        ObjectDisposedException.ThrowIf(Handle == 0, this);
    }
    [UnmanagedCallersOnly]
    private static nint WindowProcedure(nint window, uint message, nint wParam, nint lParam)
    {
        try
        {
            if (Windows.TryGetValue(window, out var owner))
            {
                if (message == owner.showMessage) { owner.ShowRequested?.Invoke(owner, EventArgs.Empty); return 0; }
                if ((message == 0x0218 && wParam == 4) || (message == 0x02B1 && wParam == 7))
                    owner.SuspendOrLock?.Invoke(owner, EventArgs.Empty);
                if ((message == 0x0218 && wParam is 7 or 18) || (message == 0x02B1 && wParam == 8))
                    owner.Resumed?.Invoke(owner, EventArgs.Empty);
                owner.Message?.Invoke(message, wParam, lParam);
                if (message is NativeMethods.HotKey or NativeMethods.ClipboardUpdate) return 0;
            }
        }
        catch (Exception exception)
        {
            // Never expose content or permit managed exceptions to unwind through the native message pump.
            System.Diagnostics.Trace.TraceError("Native callback: {0} ({1:X8})", exception.GetType().Name, exception.HResult);
        }
        return NativeMethods.DefWindowProc(window, message, wParam, lParam);
    }
    public void Dispose()
    {
        if (Handle == 0) return;
        VerifyThread();
        if (suspendRegistration != 0) NativeMethods.UnregisterSuspendResumeNotification(suspendRegistration);
        if (sessionRegistered) NativeMethods.WTSUnRegisterSessionNotification(Handle);
        Windows.Remove(Handle);
        NativeMethods.DestroyWindow(Handle);
        Handle = 0;
        NativeMethods.UnregisterClass(className, instance);
    }
}
