using System.Text;
using System.Text.Json;
using Xunit;

namespace AirReply.Context.Tests;

/// <summary>The 62 original macOS document audit queries, with explicit Windows limit/lexical expectations.</summary>
public sealed class AcceptanceCorpusTests
{
    private static readonly JsonSerializerOptions Json = new() { PropertyNameCaseInsensitive = true };
    public static IEnumerable<object[]> Cases()
    {
        using var json = JsonDocument.Parse(File.ReadAllText(Path.Combine(AppContext.BaseDirectory, "Fixtures", "cases.json")));
        return json.RootElement.GetProperty("cases").EnumerateArray().Select(c => new object[] { c.GetProperty("id").GetString()! }).ToArray();
    }

    [Theory]
    [MemberData(nameof(Cases))]
    public async Task AuditQueryPreservesExpectedFactsOrReportsItsDeclaredLimit(string id)
    {
        using var json = JsonDocument.Parse(await File.ReadAllTextAsync(Path.Combine(AppContext.BaseDirectory, "Fixtures", "cases.json"), TestContext.Current.CancellationToken));
        var c = json.RootElement.GetProperty("cases").EnumerateArray().Single(e => e.GetProperty("id").GetString() == id);
        string Text(string key) => c.TryGetProperty(key, out var value) ? value.GetString() ?? "" : "";
        var files = new List<string>();
        foreach (var key in new[] { "files", "profileFiles" })
            if (c.TryGetProperty(key, out var list)) files.AddRange(list.EnumerateArray().Select(f => f.GetString()!));
        var docs = new List<ContextDocument>();
        var importer = new DocumentImporter();
        foreach (var file in files)
        {
            if (id is "9g" or "9h")
            {
                var ex = await Assert.ThrowsAsync<ContextImportException>(() => importer.ImportAsync(Path.Combine(AppContext.BaseDirectory, "Fixtures", file), TestContext.Current.CancellationToken));
                Assert.Equal(id == "9g" ? ContextImportError.PdfNoText : ContextImportError.PdfProtected, ex.Error);
                return;
            }
            docs.Add(await importer.ImportAsync(Path.Combine(AppContext.BaseDirectory, "Fixtures", file), TestContext.Current.CancellationToken));
        }
        ContextProfile? profile = null;
        if (c.TryGetProperty("profile", out var profileName))
            profile = JsonSerializer.Deserialize<ContextProfile>(json.RootElement.GetProperty("profiles").GetProperty(profileName.GetString()!).GetRawText(), Json);
        var reply = Text("mode") == "reply";
        var draft = Text("draft");
        var copied = Text("copied");
        var query = reply ? copied + (draft.Length > 0 ? "\n" + draft : "") : draft;
        var instructions = Text("customInstructions");
        // The Windows editor deliberately caps per-message instructions at 500 runes; the old 1000-char query is rejected whole.
        if (id == "7c")
        {
            Assert.Throws<ContextBudgetException>(() => ContextPlanner.Build(profile, docs, query, new string('я', 1000), copied, draft, reply));
            return;
        }
        var plan = ContextPlanner.Build(profile, docs, query, instructions, copied, draft, reply);
        Assert.True(plan.PromptBytes <= 4096 - 256, id + " exceeded the server's escaped prompt budget");
        if (plan.Payload is { } payload)
        {
            Assert.True(payload.References.Count <= 8);
            Assert.True((payload.Profile ?? "").EnumerateRunes().Count() <= 1500);
            Assert.True(payload.References.Sum(r => r.EnumerateRunes().Count()) <= 4000);
            Assert.True(Encoding.UTF8.GetByteCount(payload.Profile ?? "") + payload.References.Sum(Encoding.UTF8.GetByteCount) <= 4096);
        }
        // Lexical retrieval does not translate arbitrary furniture names or infer synonyms.
        if (id is "3e" or "3f" or "3g") { Assert.Null(plan.Payload); return; }
        Assert.NotNull(plan.Payload);
        var all = (plan.Payload.Profile ?? "") + "\n" + string.Join('\n', plan.Payload.References);
        if (id == "1d") { Assert.Contains("Арабика", all, StringComparison.Ordinal); return; }
        // The supported tie-break is last import, not a guess about which prices are commercially current.
        if (id == "6b") { Assert.Contains("599990", plan.Payload.References[0], StringComparison.Ordinal); return; }
        // Long profiles retain the high priority fields and expose omitted facts instead of pretending everything fit.
        if (id is "7b" or "7d")
        {
            Assert.Contains("Instructions:", plan.Payload.Profile, StringComparison.Ordinal);
            Assert.Contains("Диван угловой", all, StringComparison.Ordinal);
            Assert.True(plan.OmittedProfileFields.Count > 0 || all.Contains("Возврат в течение 14 дней", StringComparison.Ordinal));
            return;
        }
        if (c.TryGetProperty("expectFirst", out var first)) Assert.Contains(first.GetString()!, plan.Payload.References[0], StringComparison.Ordinal);
        if (c.TryGetProperty("expectContains", out var contains))
            foreach (var expected in contains.EnumerateArray()) Assert.Contains(expected.GetString()!, all, StringComparison.Ordinal);
    }
}
