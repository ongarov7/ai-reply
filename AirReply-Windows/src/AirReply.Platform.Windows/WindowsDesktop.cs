using System.ComponentModel;
using System.Runtime.InteropServices;
using System.Runtime.Versioning;

namespace AirReply.Platform.Windows;

[SupportedOSPlatform("windows")]
public static class WindowsDesktop
{
    /// <summary>Physical screen coordinates; Avalonia Screens chooses the monitor and applies its scale.</summary>
    public static (int X, int Y) CursorPosition()
    {
        if (!NativeMethods.GetCursorPos(out var point)) throw new Win32Exception(Marshal.GetLastPInvokeError());
        return (point.X, point.Y);
    }
}
