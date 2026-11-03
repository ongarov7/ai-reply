using System.Text.Json;
using System.Text.Json.Serialization;

namespace AirReply.Core.Settings;

/// <summary>Where the quick bar appears (same choices as the Mac).</summary>
public enum PanelPlacement { Top, Bottom, Remembered }

public enum AppearancePreference { System, Light, Dark }

public enum DictationLanguage { Auto, Ru, Kk, En }

/// <summary>
/// A global shortcut as Win32 sees it: virtual-key code plus modifiers. Stored, compared and shown by value.
/// </summary>
public readonly record struct KeyGesture(uint VirtualKey, KeyModifiers Modifiers)
{
    public const uint VkSpace = 0x20;

    public static KeyGesture AltSpace => new(VkSpace, KeyModifiers.Alt);
    public static KeyGesture ControlAltSpace => new(VkSpace, KeyModifiers.Control | KeyModifiers.Alt);
    public static KeyGesture ControlShiftSpace => new(VkSpace, KeyModifiers.Control | KeyModifiers.Shift);
}

[Flags]
public enum KeyModifiers : uint { None = 0, Alt = 1, Control = 2, Shift = 4, Win = 8 }

/// <summary>Local preferences of this Windows user. Account data lives on the server.</summary>
public sealed class AppSettings
{
    public KeyGesture Shortcut { get; set; } = KeyGesture.AltSpace;
    public PanelPlacement Placement { get; set; } = PanelPlacement.Top;
    /// <summary>Panel origin relative to the work area of its monitor (0…1), for <see cref="PanelPlacement.Remembered"/>.</summary>
    public double? RememberedX { get; set; }
    public double? RememberedY { get; set; }
    public AppearancePreference Appearance { get; set; } = AppearancePreference.System;
    /// <summary>Show text copied in another app as "reply to" when the bar opens.</summary>
    public bool OfferCopiedText { get; set; } = true;
    /// <summary>Hide the bar right after "Copy", so Ctrl+V goes straight into the messenger.</summary>
    public bool CloseAfterCopy { get; set; } = true;
    /// <summary>Also press Ctrl+V in the previous window after copying. Off by default.</summary>
    public bool PasteAfterCopy { get; set; }
    public DictationLanguage DictationLanguage { get; set; } = DictationLanguage.Auto;
    /// <summary>Recipient last used (server vocabulary: friend, client, work, business).</summary>
    public string LastRecipient { get; set; } = "client";
    /// <summary>Start with Windows. Off until the user turns it on.</summary>
    public bool LaunchAtStartup { get; set; }
    /// <summary>Interface language override; null follows the Windows display language.</summary>
    public string? InterfaceLanguage { get; set; }

    public AppSettings Clone() => (AppSettings)MemberwiseClone();
}

public interface ISettingsStore
{
    AppSettings Load();
    void Save(AppSettings settings);
}

/// <summary>JSON in %LOCALAPPDATA%\AI Reply\settings.json, replaced atomically. A damaged file falls back to defaults.</summary>
public sealed class JsonSettingsStore(string path) : ISettingsStore
{
    private static readonly JsonSerializerOptions Options = new()
    {
        WriteIndented = true,
        Converters = { new JsonStringEnumConverter(JsonNamingPolicy.CamelCase) },
        PropertyNamingPolicy = JsonNamingPolicy.CamelCase,
    };

    public static string DefaultPath() => Path.Combine(
        Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "AI Reply", "settings.json");

    public AppSettings Load()
    {
        try
        {
            if (!File.Exists(path))
            {
                return new AppSettings();
            }
            return JsonSerializer.Deserialize<AppSettings>(File.ReadAllText(path), Options) ?? new AppSettings();
        }
        catch (Exception exception) when (exception is IOException or JsonException or UnauthorizedAccessException)
        {
            return new AppSettings();
        }
    }

    public void Save(AppSettings settings)
    {
        ArgumentNullException.ThrowIfNull(settings);
        var directory = Path.GetDirectoryName(path)!;
        Directory.CreateDirectory(directory);
        var temporary = Path.Combine(directory, $".settings-{Guid.NewGuid():N}.tmp");
        File.WriteAllText(temporary, JsonSerializer.Serialize(settings, Options));
        File.Move(temporary, path, overwrite: true);
    }
}
