using System.Collections.Frozen;
using System.Globalization;
using System.Reflection;
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
        lock (SourcesGate)
        {
            strings = Shared.Value;
        }
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

    private static readonly Lock SourcesGate = new();
    private static readonly List<(Assembly Assembly, string Resource)> Sources =
        [(typeof(Localizer).Assembly, "AirReply.Core.Localization.strings.json")];
    private static Lazy<FrozenDictionary<string, FrozenDictionary<UiLanguage, string>>> Shared = new(Load);

    /// <summary>
    /// Adds another module's strings (an embedded JSON resource with the same shape as Core's strings.json).
    /// Call at startup before the first lookup; a key defined twice keeps the later module's text.
    /// </summary>
    public static void Include(Assembly assembly, string resourceName)
    {
        ArgumentNullException.ThrowIfNull(assembly);
        lock (SourcesGate)
        {
            if (Sources.Exists(s => s.Assembly == assembly && s.Resource == resourceName))
            {
                return;
            }
            Sources.Add((assembly, resourceName));
            Shared = new(Load);
        }
    }

    private static FrozenDictionary<string, FrozenDictionary<UiLanguage, string>> Load()
    {
        (Assembly Assembly, string Resource)[] sources;
        lock (SourcesGate)
        {
            sources = [.. Sources];
        }
        var result = new Dictionary<string, FrozenDictionary<UiLanguage, string>>(StringComparer.Ordinal);
        foreach (var (assembly, resource) in sources)
        {
            using var stream = assembly.GetManifestResourceStream(resource)
                ?? throw new InvalidOperationException($"{resource} is not embedded in {assembly.GetName().Name}");
            Merge(stream, result);
        }
        return result.ToFrozenDictionary(StringComparer.Ordinal);
    }

    private static void Merge(Stream stream, Dictionary<string, FrozenDictionary<UiLanguage, string>> result)
    {
        using var document = JsonDocument.Parse(stream);
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
    }
}
