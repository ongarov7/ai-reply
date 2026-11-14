using System.Runtime.InteropServices;
using System.Runtime.Versioning;
using AirReply.Core.Settings;
using AirReply.Presentation.Platform;

namespace AirReply.Platform.Windows;

[SupportedOSPlatform("windows")]
public sealed class WindowsHotKeyService : IHotKeyService
{
    private readonly WindowsMessageWindow window;
    private int generation;
    private int currentId;
    private bool disposed;
    public event EventHandler? Pressed;
    public KeyGesture? Current { get; private set; }
    public int LastError { get; private set; }
    internal int CurrentId => currentId;
    public WindowsHotKeyService(WindowsMessageWindow window)
    {
        this.window = window;
        window.Message += OnMessage;
    }
    public HotKeyResult Register(KeyGesture gesture)
    {
        ObjectDisposedException.ThrowIf(disposed, this);
        window.VerifyThread();
        if (!HotKeyRules.IsAllowed(gesture)) return HotKeyResult.NotAllowed;
        if (Current == gesture) return HotKeyResult.Ok;
        var id = ++generation % 0xBFFF + 1;
        if (!NativeMethods.RegisterHotKey(window.Handle, id, (uint)gesture.Modifiers | 0x4000, gesture.VirtualKey))
        {
            LastError = Marshal.GetLastPInvokeError();
            return LastError == 1409 ? HotKeyResult.AlreadyTaken : HotKeyResult.Failed;
        }
        if (Current != null) NativeMethods.UnregisterHotKey(window.Handle, currentId);
        Current = gesture;
        currentId = id;
        LastError = 0;
        return HotKeyResult.Ok;
    }
    /// <summary>Retries the chosen shortcut on every launch. A fallback is never written over the preference.</summary>
    public HotKeyResult RegisterSaved(KeyGesture saved)
    {
        var result = HotKeyResult.Failed;
        foreach (var candidate in HotKeyRules.StartupCandidates(saved))
        {
            result = Register(candidate);
            if (result == HotKeyResult.Ok) break;
        }
        return result;
    }
    private void OnMessage(uint message, nint wParam, nint lParam)
    {
        if (message == NativeMethods.HotKey && !disposed && Current != null && (int)wParam == currentId)
            Pressed?.Invoke(this, EventArgs.Empty);
    }
    public void Unregister()
    {
        window.VerifyThread();
        if (Current == null) return;
        NativeMethods.UnregisterHotKey(window.Handle, currentId);
        Current = null;
    }
    public void Dispose()
    {
        if (disposed) return;
        Unregister();
        window.Message -= OnMessage;
        disposed = true;
    }
}
