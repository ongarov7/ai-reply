using System.Globalization;
using AirReply.Core.Settings;

namespace AirReply.Platform.Windows;

/// <summary>Pure Windows shortcut rules; does not consult keyboard layouts or capture other keystrokes.</summary>
public static class HotKeyRules
{
    public static KeyGesture ControlAltShiftSpace => new(0x20, KeyModifiers.Control | KeyModifiers.Alt | KeyModifiers.Shift);
    private static readonly HashSet<KeyGesture> Reserved =
    [
        new(0x43, KeyModifiers.Control), new(0x56, KeyModifiers.Control), new(0x58, KeyModifiers.Control),
        new(0x41, KeyModifiers.Control), new(0x5A, KeyModifiers.Control), new(0x59, KeyModifiers.Control),
        new(0x5A, KeyModifiers.Control | KeyModifiers.Shift), new(0x2D, KeyModifiers.Control),
        new(0x09, KeyModifiers.Alt), new(0x09, KeyModifiers.Alt | KeyModifiers.Shift),
        new(0x09, KeyModifiers.Control | KeyModifiers.Alt), new(0x1B, KeyModifiers.Alt),
        new(0x1B, KeyModifiers.Alt | KeyModifiers.Shift), new(0x1B, KeyModifiers.Control),
        new(0x09, KeyModifiers.Control), new(0x09, KeyModifiers.Control | KeyModifiers.Shift),
        new(0x20, KeyModifiers.Control), new(0x73, KeyModifiers.Alt), new(0x73, KeyModifiers.Control),
        new(0x57, KeyModifiers.Control), new(0x1B, KeyModifiers.Control | KeyModifiers.Shift),
        new(0x2E, KeyModifiers.Control | KeyModifiers.Alt),
    ];
    public static bool IsAllowed(KeyGesture gesture)
    {
        var modifiers = gesture.Modifiers;
        return (modifiers & (KeyModifiers.Control | KeyModifiers.Alt)) != 0 &&
            (modifiers & ~(KeyModifiers.Control | KeyModifiers.Alt | KeyModifiers.Shift)) == 0 &&
            IsMainKey(gesture.VirtualKey) && !Reserved.Contains(gesture);
    }
    public static bool IsMainKey(uint key) => key is >= 0x41 and <= 0x5A or >= 0x30 and <= 0x39 or
        >= 0x70 and <= 0x7A or >= 0x7C and <= 0x87 or >= 0x60 and <= 0x6B or >= 0x6D and <= 0x6F or
        >= 0xBA and <= 0xC0 or >= 0xDB and <= 0xDF or 0xE2 or 0x20 or 0x0D or 0x09 or 0x1B or 0x08 or
        0x2D or 0x2E or >= 0x21 and <= 0x28 or 0x13;
    public static IReadOnlyList<KeyGesture> StartupCandidates(KeyGesture saved) => saved == KeyGesture.AltSpace
        ? [saved, KeyGesture.ControlAltSpace, ControlAltShiftSpace] : [saved];
    public static string Display(KeyGesture gesture)
    {
        var prefix = ((gesture.Modifiers & KeyModifiers.Win) != 0 ? "Win+" : "") +
            ((gesture.Modifiers & KeyModifiers.Control) != 0 ? "Ctrl+" : "") +
            ((gesture.Modifiers & KeyModifiers.Alt) != 0 ? "Alt+" : "") +
            ((gesture.Modifiers & KeyModifiers.Shift) != 0 ? "Shift+" : "");
        var key = gesture.VirtualKey;
        var name = key switch
        {
            0x20 => "Space", 0x0D => "Enter", 0x09 => "Tab", 0x1B => "Esc", 0x08 => "Backspace",
            0x2D => "Insert", 0x2E => "Delete", 0x24 => "Home", 0x23 => "End", 0x21 => "PgUp", 0x22 => "PgDn",
            0x25 => "←", 0x26 => "↑", 0x27 => "→", 0x28 => "↓", 0x13 => "Pause",
            >= 0x70 and <= 0x87 => "F" + (key - 0x6F).ToString(CultureInfo.InvariantCulture),
            >= 0x60 and <= 0x69 => "Num " + (key - 0x60).ToString(CultureInfo.InvariantCulture),
            0x6A => "Num *", 0x6B => "Num +", 0x6D => "Num -", 0x6E => "Num .", 0x6F => "Num /",
            >= 0x41 and <= 0x5A or >= 0x30 and <= 0x39 => ((char)key).ToString(),
            0xBA => ";", 0xBB => "=", 0xBC => ",", 0xBD => "-", 0xBE => ".", 0xBF => "/", 0xC0 => "`",
            0xDB => "[", 0xDC or 0xE2 => "\\", 0xDD => "]", 0xDE => "'", _ => "Key " + key.ToString(CultureInfo.InvariantCulture),
        };
        return prefix + name;
    }
}
