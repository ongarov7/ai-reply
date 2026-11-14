using System.Globalization;
using System.Text;
using System.Text.RegularExpressions;
using AirReply.Core.Generation;

namespace AirReply.Context;

public sealed record ContextLimits(int ProfileCharacters = 1500, int ReferenceCharacters = 4000, int References = 8,
    int ContextBytes = 4096, int InputTokenBudget = 4096);
public enum ContextBudgetError { NoRoom, InstructionsDoNotFit, InstructionsTooLong }
public sealed class ContextBudgetException(ContextBudgetError error) : Exception(error.ToString())
{
    public ContextBudgetError Error { get; } = error;
}
public sealed record ContextEvidence(string FileName, string Locator, DateTimeOffset ImportedAt, bool Partial);
public sealed record ContextPlan(GenerationContextPayload? Payload, IReadOnlyList<ContextEvidence> Evidence,
    int OmittedMatches, IReadOnlyList<string> OmittedProfileFields, IReadOnlyList<string> NotFound)
{
    public int PromptBytes { get; init; }
}

/// <summary>Local lexical retrieval and the server's exact escaped UTF-8 prompt budget.</summary>
public static partial class ContextPlanner
{
    private const int Margin = 256;
    public static ContextPlan Build(ContextProfile? profile, IEnumerable<ContextDocument> documents, string query,
        string conversationInstructions = "", string? sourceText = null, string? instruction = null, bool isReply = false,
        ContextLimits? limits = null)
    {
        ArgumentNullException.ThrowIfNull(documents);
        limits ??= new ContextLimits();
        var profileCap = Math.Clamp(limits.ProfileCharacters, 0, 1500);
        var refCap = Math.Clamp(limits.ReferenceCharacters, 0, 4000);
        var slots = Math.Clamp(limits.References, 0, 8);
        var rawCap = Math.Clamp(limits.ContextBytes, 0, 4096);
        var promptCap = Math.Clamp(limits.InputTokenBudget, 256, 8192) - Margin;
        conversationInstructions = Clean(conversationInstructions);
        if (ContextText.Runes(conversationInstructions) > 500) throw new ContextBudgetException(ContextBudgetError.InstructionsTooLong);
        sourceText = ContextText.Valid(sourceText ?? "").Trim();
        instruction = ContextText.Valid(instruction ?? (isReply ? "" : query)).Trim();
        var baseBytes = isReply ? 176 + Bytes(sourceText) + (instruction.Length > 0 ? 213 + Bytes(instruction) : 0) : 115 + Bytes(instruction);
        var room0 = promptCap - baseBytes - 39;
        if (room0 < 0) throw new ContextBudgetException(ContextBudgetError.NoRoom);
        var first = conversationInstructions.Length == 0 ? "" : "Instructions for this message: " + conversationInstructions;
        if (ContextText.Runes(first) > profileCap || EscapedBytes(first) > room0 || Bytes(first) > rawCap)
            throw new ContextBudgetException(ContextBudgetError.InstructionsDoNotFit);
        var ranked = Rank(documents, query, out var notFound);
        var firstBytes = first.Length > 0 ? EscapedBytes(first) + 1 : 0;
        var reserve = Math.Min((int)Math.Floor(Math.Max(0, room0 - firstBytes) * .6), ranked.Take(3).Sum(c => 43 + EscapedBytes(Reference(c, Project(c, 1200), false))));
        var omittedFields = new List<string>();
        var profileLines = new List<string>();
        if (first.Length > 0) profileLines.Add(first);
        var current = first;
        foreach (var field in Fields(profile))
        {
            var value = Clean(field.Value);
            if (value.Length == 0) continue;
            var line = field.Label + ": " + value;
            var next = current + (current.Length > 0 ? "\n" : "") + line;
            bool Fits(string text) => ContextText.Runes(text) <= profileCap && Bytes(text) <= rawCap && EscapedBytes(text) <= room0 - reserve;
            if (Fits(next)) { profileLines.Add(line); current = next; continue; }
            if (field.Long && ContextText.Runes(value) >= 80)
            {
                var shortened = "";
                var words = value.Split(' ', StringSplitOptions.RemoveEmptyEntries);
                var candidate = "";
                foreach (var word in words)
                {
                    var test = candidate + (candidate.Length > 0 ? " " : "") + word;
                    var joined = current + (current.Length > 0 ? "\n" : "") + field.Label + ": " + test + "…";
                    if (!Fits(joined)) break;
                    candidate = test;
                    if (ContextText.Runes(candidate) >= 80) shortened = candidate;
                }
                if (shortened.Length > 0)
                {
                    line = field.Label + ": " + shortened + "…";
                    current += (current.Length > 0 ? "\n" : "") + line;
                    profileLines.Add(line);
                }
            }
            omittedFields.Add(field.Label);
        }
        var profileText = string.Join('\n', profileLines);
        var room = promptCap - baseBytes - (profileText.Length > 0 ? 39 + EscapedBytes(profileText) : 0);
        var rawRoom = rawCap - Bytes(profileText);
        var runeRoom = refCap;
        var references = new List<string>();
        var evidence = new List<ContextEvidence>();
        foreach (var candidate in ranked)
        {
            if (slots <= 0) break;
            var body = candidate.Segment.Text;
            var partial = false;
            var reference = Reference(candidate, body, false);
            bool FitsReference(string text) => 43 + EscapedBytes(text) <= room && Bytes(text) <= rawRoom && ContextText.Runes(text) <= runeRoom;
            if ((candidate.Segment.Cells.Count > 0 && ContextText.Runes(body) > 1200) || !FitsReference(reference))
            {
                if (candidate.Segment.Cells.Count == 0) continue;
                body = Project(candidate, 1200, text => FitsReference(Reference(candidate, text, true)));
                if (body.Length == 0) continue;
                partial = body != candidate.Segment.Text;
                reference = Reference(candidate, body, partial);
                if (!FitsReference(reference)) continue;
            }
            references.Add(reference);
            evidence.Add(new ContextEvidence(candidate.Attachment.Name, candidate.Segment.Locator, candidate.Attachment.ImportedAt, partial));
            room -= 43 + EscapedBytes(reference);
            rawRoom -= Bytes(reference);
            runeRoom -= ContextText.Runes(reference);
            slots--;
        }
        var payload = profileText.Length == 0 && references.Count == 0 ? null : new GenerationContextPayload(profileText.Length > 0 ? profileText : null, references);
        return new ContextPlan(payload, evidence, ranked.Count - references.Count, omittedFields, notFound)
        { PromptBytes = baseBytes + (profileText.Length > 0 ? 39 + EscapedBytes(profileText) : 0) + references.Sum(r => 43 + EscapedBytes(r)) };
    }

    public static int EscapedBytes(string value)
    {
        var length = Bytes(value);
        foreach (var c in value) length += c switch { '&' or '\'' or '"' => 4, '<' or '>' => 3, _ => 0 };
        return length;
    }
    private static int Bytes(string value) => Encoding.UTF8.GetByteCount(ContextText.Valid(value));

    private static List<Candidate> Rank(IEnumerable<ContextDocument> documents, string query, out IReadOnlyList<string> notFound)
    {
        var normalized = ContextText.Normalize(query);
        normalized = Greetings().Replace(normalized, "", 1);
        normalized = ContextText.Compact(normalized);
        var words = Words(normalized).Where(t => !StopWords.Contains(t)).ToArray();
        var terms = words.Where(t => ContextText.Runes(t) > 1 || t.All(char.IsDigit)).Distinct(StringComparer.Ordinal).ToArray();
        var skus = Skus(normalized);
        var product = ProductName().Match(normalized);
        var productTerm = product.Success ? Words(product.Groups[1].Value).FirstOrDefault(t => !StopWords.Contains(t)) : null;
        var entries = documents.SelectMany(d => d.Segments.Select(s => new Indexed(d.Attachment, s, ContextText.Normalize(s.Text), Words(ContextText.Normalize(s.Text)).ToHashSet(StringComparer.Ordinal), Skus(ContextText.Normalize(s.Text))))).ToArray();
        var missing = new List<string>();
        foreach (var sku in skus) if (!entries.Any(e => e.Skus.Contains(sku))) missing.Add(sku);
        if (productTerm is not null && !entries.Any(e => e.Words.Any(w => Matches(productTerm, w)))) missing.Add(productTerm);
        notFound = missing;
        var frequency = terms.ToDictionary(t => t, t => entries.Count(e => e.Words.Any(w => Matches(t, w))), StringComparer.Ordinal);
        var pairs = words.Zip(words.Skip(1), (a, b) => a + " " + b).Distinct(StringComparer.Ordinal).ToArray();
        var pairFrequency = pairs.ToDictionary(p => p, p => entries.Count(e => Phrase(e.Text, p)), StringComparer.Ordinal);
        var commonNameTerms = terms.Where(t => entries.Any(e => e.Segment.Name is { } name && MatchesAny(t, name))).ToHashSet(StringComparer.Ordinal);
        var candidates = new List<Candidate>();
        for (var index = 0; index < entries.Length; index++)
        {
            var entry = entries[index];
            if (skus.Count > 0 && !entry.Skus.Overlaps(skus)) continue;
            if (productTerm is not null && !entry.Words.Any(w => Matches(productTerm, w))) continue;
            var score = 0;
            foreach (var term in terms)
            {
                // Product names repeated across several price lists remain relevant. Suppress repeated
                // headers/status boilerplate, but do not erase an all-products query.
                if (entries.Length >= 6 && frequency[term] > entries.Length / 2 &&
                    !commonNameTerms.Contains(term)) continue;
                if (entry.Words.Contains(term)) score += Math.Min(12, ContextText.Runes(term));
                else
                {
                    var stem = entry.Words.Select(w => SharedStem(term, w)).Max();
                    score += Math.Min(8, stem);
                }
            }
            score += 40 * skus.Count(entry.Skus.Contains);
            if (ContextText.Runes(normalized) >= 3 && Phrase(entry.Text, normalized)) score += 30;
            if (pairs.Any(p => Phrase(entry.Text, p) && (entries.Length < 6 || pairFrequency[p] <= entries.Length / 2))) score += 30;
            if (score == 0) continue;
            if (entry.Segment.Name is { } name)
            {
                var unmatched = Words(ContextText.Normalize(name)).Where(w => ContextText.Runes(w) >= 2 && !StopWords.Contains(w))
                    .Count(w => !terms.Any(t => Matches(t, w)) && !skus.Contains(SkuNormalize(w)));
                score += unmatched == 0 ? 10 : -2 * Math.Min(5, unmatched);
            }
            candidates.Add(new Candidate(entry.Attachment, entry.Segment, Math.Max(1, score), index, terms));
        }
        var sorted = candidates.OrderByDescending(c => c.Score).ThenByDescending(c => c.Attachment.ImportedAt).ThenBy(c => c.Order).ToArray();
        var threshold = sorted.Length == 0 ? 1 : Math.Max(1, sorted[0].Score / 2);
        var result = new List<Candidate>();
        var bodies = new HashSet<string>(StringComparer.Ordinal);
        foreach (var candidate in sorted.Where(c => c.Score >= threshold))
        {
            if (bodies.Add(candidate.Segment.Text)) result.Add(candidate);
            if (candidate.Segment.Key is not { Length: > 0 } key) continue;
            var normalizedKey = ContextText.Normalize(key);
            foreach (var entry in entries.Where(e => e.Attachment.Id == candidate.Attachment.Id && e.Segment.Table != candidate.Segment.Table && e.Segment.Key is not null && ContextText.Normalize(e.Segment.Key) == normalizedKey))
                if (bodies.Add(entry.Segment.Text)) result.Add(new Candidate(entry.Attachment, entry.Segment, candidate.Score, candidate.Order, terms));
        }
        return result;
    }

    private static string Project(Candidate candidate, int cap, Func<string, bool>? fits = null)
    {
        if (candidate.Segment.Cells.Count == 0) return candidate.Segment.Text;
        var cells = candidate.Segment.Cells;
        var keys = cells.Where(c => c.Value == candidate.Segment.Key || c.Value == candidate.Segment.Name).ToArray();
        if (keys.Length == 0) keys = [cells[0]];
        var selected = new List<ContextCell>();
        var ordered = keys.Concat(cells.Where(c => candidate.Terms.Any(t => MatchesAny(t, c.Header + " " + c.Value))))
            .Concat(cells.Where(c => DocumentImporter.PriceWords.Any(p => ContextText.Normalize(c.Header).Contains(p, StringComparison.Ordinal))))
            .Concat(cells).DistinctBy(c => c.Column);
        foreach (var cell in ordered)
        {
            var body = DocumentImporter.Render(selected.Append(cell));
            if (ContextText.Runes(body) <= cap && (fits is null || fits(body))) selected.Add(cell);
        }
        if (!keys.All(k => selected.Contains(k)) || selected.Count <= keys.Length && cells.Count > keys.Length) return "";
        return DocumentImporter.Render(selected);
    }
    private static bool MatchesAny(string term, string text) => Words(ContextText.Normalize(text)).Any(w => Matches(term, w));
    private static string Reference(Candidate candidate, string body, bool partial)
    {
        var name = ContextText.Compact(string.Concat(candidate.Attachment.Name.Select(c => char.IsControl(c) ? ' ' : c))).Replace('[', '(').Replace(']', ')');
        if (ContextText.Runes(name) > 80) name = ContextText.Take(name, 60) + "…" + string.Concat(name.EnumerateRunes().TakeLast(19).Select(r => r.ToString()));
        var locator = candidate.Segment.Locator.Replace('[', '(').Replace(']', ')');
        return "[" + name + " · " + locator + (partial ? " (partial)" : "") + " · imported " + candidate.Attachment.ImportedAt.ToString("yyyy-MM-dd", CultureInfo.InvariantCulture) + "]\n" + body;
    }
    private static string Clean(string value)
    {
        value = ContextText.Valid(value).Replace("\r\n", "\n", StringComparison.Ordinal).Replace('\r', '\n').Replace('\t', ' ');
        var cleaned = string.Concat(value.EnumerateRunes().Where(r => Rune.GetUnicodeCategory(r) != UnicodeCategory.Format || r.Value == 0x200d).Select(r => r.ToString()));
        return Regex.Replace(string.Join('\n', cleaned.Split('\n').Select(l => l.TrimEnd())), "\\n{3,}", "\n\n", RegexOptions.CultureInvariant, TimeSpan.FromSeconds(1)).Trim();
    }
    private static IEnumerable<(string Label, string Value, bool Long)> Fields(ContextProfile? profile)
    {
        if (profile is null) yield break;
        yield return ("Instructions", profile.Instructions, true);
        yield return ("Style", profile.CommunicationStyle, false);
        yield return ("Name", profile.PersonName, false);
        yield return ("Occupation", profile.Occupation, false);
        yield return ("Company", profile.Company, false);
        yield return ("Industry", profile.Industry, false);
        yield return ("Policies", profile.Policies, true);
        yield return ("Products", profile.Products, true);
        yield return ("Services", profile.Services, true);
        yield return ("Description", profile.Description, true);
    }
    private static string[] Words(string text) => WordPattern().Matches(text).Select(m => m.Value).ToArray();
    private static bool Phrase(string text, string phrase)
    {
        var start = 0;
        while ((start = text.IndexOf(phrase, start, StringComparison.Ordinal)) >= 0)
        {
            if ((start == 0 || !char.IsLetterOrDigit(text[start - 1])) && (start + phrase.Length == text.Length || !char.IsLetterOrDigit(text[start + phrase.Length]))) return true;
            start++;
        }
        return false;
    }
    private static bool Matches(string a, string b) => a == b || SharedStem(a, b) > 0;
    private static int SharedStem(string a, string b)
    {
        if (Translations.TryGetValue(a, out var translated) && translated == b) return Math.Min(8, ContextText.Runes(b));
        if (a.Length < 4 || b.Length < 4 || !a.All(c => c >= '\u0400' && c <= '\u052f') || !b.All(c => c >= '\u0400' && c <= '\u052f')) return 0;
        var left = Stems(a);
        var right = Stems(b);
        return left.Intersect(right, StringComparer.Ordinal).Select(ContextText.Runes).DefaultIfEmpty(0).Max();
    }
    private static HashSet<string> Stems(string word)
    {
        var set = new HashSet<string>(StringComparer.Ordinal) { word };
        foreach (var suffix in Suffixes) if (word.EndsWith(suffix, StringComparison.Ordinal) && word.Length - suffix.Length >= 3) set.Add(word[..^suffix.Length]);
        foreach (var stem in set.ToArray())
            foreach (var suffix in KazakhSuffixes) if (stem.EndsWith(suffix, StringComparison.Ordinal) && stem.Length - suffix.Length >= 3) set.Add(stem[..^suffix.Length]);
        return set;
    }
    private static HashSet<string> Skus(string text)
    {
        var set = new HashSet<string>(StringComparer.Ordinal);
        foreach (Match match in SkuPattern().Matches(text))
        {
            var token = SkuNormalize(match.Value);
            if (token.Length is >= 3 and <= 32 && token.Any(char.IsLetter) && token.Any(char.IsDigit)) set.Add(token);
        }
        foreach (var word in Words(text))
            if (word.Length is >= 3 and <= 32 && word.Any(char.IsLetter) && word.Any(char.IsDigit)) set.Add(SkuNormalize(word));
        return set;
    }
    private static string SkuNormalize(string value)
    {
        const string cyrillic = "авекмнорстухі";
        const string latin = "abekmhopctyxi";
        return string.Concat(value.Where(char.IsLetterOrDigit).Select(c => cyrillic.IndexOf(c, StringComparison.Ordinal) is var index && index >= 0 ? latin[index] : c));
    }
    [GeneratedRegex(@"[\p{L}\p{Nd}]+", RegexOptions.CultureInvariant)] private static partial Regex WordPattern();
    [GeneratedRegex(@"[\p{L}\p{Nd}]+(?:[-_./][\p{L}\p{Nd}]+)+|\b[\p{L}]{1,3} [\p{Nd}]{1,8}\b", RegexOptions.CultureInvariant)] private static partial Regex SkuPattern();
    [GeneratedRegex(@"^(здравствуйте|здравствуи|добрыи день|добрыи вечер|доброе утро|привет|сәлеметсіз бе|сәлеметсіз|сәлем|қаиырлы күн|қаиырлы кеш|hello|hi|hey|good morning|good afternoon|good evening)[\s,.!:;—-]*", RegexOptions.CultureInvariant)] private static partial Regex Greetings();
    [GeneratedRegex(@"(?:product|товар|продукт|өнім)\s+([^.!?\n]+)", RegexOptions.CultureInvariant)] private static partial Regex ProductName();
    private static readonly string[] KazakhSuffixes = "лары лері дары дері тары тері лар лер дар дер тар тер ның нің дың дің тың тің дан ден тан тен нан нен мен бен пен ға ге қа ке на не ны ні ды ді ты ті да де та те сы сі ым ім ың ің ын ін ы і а е".Split(' ');
    private static readonly string[] Suffixes = "ами ями ого его ому ему ыми ими иях иям ием иеи ах ях ам ям ов ев еи ои ии ыи ая яя ое ее ые ие ую юю ых их ым им ом ем ою ею ия ья ье а я о е ы и у ю ь".Split(' ').Concat(KazakhSuffixes).ToArray();
    private static readonly HashSet<string> StopWords = "the and for what with hello please this that price cost about is of how much an как что это для здравствуйте пожалуйста цена стоимость есть можно нужно сәлем үшін қандай бағасы қанша бар в с у к о а и я на по за из от до не ли же бы во со ко об вы мы он она они вас нас мне все это эти этот ма ме ба бе па пе да де та те мен бен пен және ғой ше in on to at do does you your we my me it are have has any there".Split(' ').Select(ContextText.Normalize).ToHashSet(StringComparer.Ordinal);
    private static readonly Dictionary<string, string> Translations = new(StringComparer.Ordinal)
    {
        ["аифон"] = "iphone", ["аипад"] = "ipad", ["макбук"] = "macbook", ["аирподс"] = "airpods", ["эирподс"] = "airpods", ["самсунг"] = "samsung", ["галакси"] = "galaxy", ["ксиаоми"] = "xiaomi", ["сяоми"] = "xiaomi", ["редми"] = "redmi", ["хуавеи"] = "huawei", ["про"] = "pro", ["макс"] = "max", ["плюс"] = "plus", ["мини"] = "mini", ["ультра"] = "ultra", ["лаит"] = "lite",
    };
    private sealed record Indexed(ContextAttachment Attachment, ContextSegment Segment, string Text, HashSet<string> Words, HashSet<string> Skus);
    private sealed record Candidate(ContextAttachment Attachment, ContextSegment Segment, int Score, int Order, string[] Terms);
}
