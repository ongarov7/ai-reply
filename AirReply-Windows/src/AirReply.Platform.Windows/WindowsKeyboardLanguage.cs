using System.Runtime.Versioning;
using AirReply.Presentation.Platform;

namespace AirReply.Platform.Windows;

public static class KeyboardLanguageMap
{
    public static string? FromLayout(nint layout)
    {
        var value = (ulong)(nuint)layout;
        var low = (uint)value & 0xFFFF;
        var high = ((uint)value >> 16) & 0xFFFF;
        var language = high != 0 && (high & 0xF000) is not 0xE000 and not 0xF000 ? high : low;
        return (language & 0x3FF) switch { 0x09 => "en", 0x19 => "ru", 0x3F => "kk", _ => null };
    }
}

[SupportedOSPlatform("windows")]
public sealed class WindowsKeyboardLanguage : IKeyboardLanguage
{
    public string? CurrentInputLanguage(nint window)
    {
        var thread = window == 0 ? 0 : NativeMethods.GetWindowThreadProcessId(window, out _);
        return KeyboardLanguageMap.FromLayout(NativeMethods.GetKeyboardLayout(thread));
    }
}
