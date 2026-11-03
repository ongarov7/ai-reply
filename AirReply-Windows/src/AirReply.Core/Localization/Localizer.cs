using System.Collections.Frozen;
using System.Globalization;
using System.Text.Json;

namespace AirReply.Core.Localization;

/// <summary>The three interface languages. Russian is the source language and the fallback.</summary>
public enum UiLanguage { Ru, Kk, En }

/// <summary>
/// User-facing strings ported from the Mac string catalog (same keys, .NET composite placeholders).
/// Lookup falls back to Russian, then to the key itself, so a missing translation never crashes the UI.
/// </summary>
public sealed class Localizer
{
    private readonly FrozenDictionary<string, FrozenDictionary<UiLanguage, string>> strings;

    public Localizer(UiLanguage language)
    {
        Language = language;
        strings = Shared.Value;
    }

    public UiLanguage Language { get; }

    /// <summary>BCP-47 code sent to the server as Accept-Language and as the app language.</summary>
    public string LanguageCode => Code(Language);

    public static string Code(UiLanguage language) => language switch
    {
        UiLanguage.Kk => "kk",
        UiLanguage.En => "en",
        _ => "ru",
    };

    /// <summary>The Windows display language decides, as on the Mac: kk, en, otherwise Russian.</summary>
    public static UiLanguage FromCulture(CultureInfo culture)
    {
        ArgumentNullException.ThrowIfNull(culture);
        return culture.TwoLetterISOLanguageName switch
        {
            "kk" => UiLanguage.Kk,
            "en" => UiLanguage.En,
            _ => UiLanguage.Ru,
        };
    }

    public bool Contains(string key) => strings.ContainsKey(key);

    public string this[string key] => Get(key);

    public string Get(string key)
    {
        if (!strings.TryGetValue(key, out var row))
        {
            return key;
        }
        return row.TryGetValue(Language, out var text) ? text : row.GetValueOrDefault(UiLanguage.Ru, key);
    }

    public string Format(string key, params object?[] args) =>
        string.Format(CultureInfo.GetCultureInfo(LanguageCode), Get(key), args);

    /// <summary>All keys, for tests that check every language is complete.</summary>
    public IEnumerable<string> Keys => strings.Keys;

    private static readonly Lazy<FrozenDictionary<string, FrozenDictionary<UiLanguage, string>>> Shared = new(Load);

    private static FrozenDictionary<string, FrozenDictionary<UiLanguage, string>> Load()
    {
        using var stream = typeof(Localizer).Assembly.GetManifestResourceStream("AirReply.Core.Localization.strings.json")
            ?? throw new InvalidOperationException("strings.json is not embedded");
        using var document = JsonDocument.Parse(stream);
        var result = new Dictionary<string, FrozenDictionary<UiLanguage, string>>(StringComparer.Ordinal);
        foreach (var entry in document.RootElement.GetProperty("strings").EnumerateObject())
        {
            var row = new Dictionary<UiLanguage, string>();
            foreach (var (name, language) in new[] { ("ru", UiLanguage.Ru), ("kk", UiLanguage.Kk), ("en", UiLanguage.En) })
            {
                if (entry.Value.TryGetProperty(name, out var value) && value.ValueKind == JsonValueKind.String)
                {
                    row[language] = value.GetString()!;
                }
            }
            result[entry.Name] = row.ToFrozenDictionary();
        }
        return result.ToFrozenDictionary(StringComparer.Ordinal);
    }
}
