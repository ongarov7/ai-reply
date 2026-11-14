using System.Text;
using System.Text.Json;
using AirReply.Core.Infrastructure;
using Xunit;

namespace AirReply.Context.Tests;

public sealed class ContextTests
{
    private static string Fixture(string path) => Path.Combine(AppContext.BaseDirectory, "Fixtures", path);

    [Fact]
    public async Task StoreIsEncryptedAccountScopedAndMetadataEditsPreserveDocuments()
    {
        var root = Path.Combine(Path.GetTempPath(), Guid.NewGuid().ToString("N"));
        using var protector = new TestProtector();
        try
        {
            using var store = new ContextProfileStore(root, "account-one", protector);
            await store.OpenAsync(cancellationToken: TestContext.Current.CancellationToken);
            var created = await store.SaveAsync(new ContextProfile { Name = "SECRET PROFILE NAME" }, cancellationToken: TestContext.Current.CancellationToken);
            var doc = await new DocumentImporter().ImportBytesAsync("catalog.csv", ContextDocumentFormat.Csv, Encoding.UTF8.GetBytes("Product,Price\nChair,25000"), cancellationToken: TestContext.Current.CancellationToken);
            await store.AddDocumentAsync(created.Id, doc, cancellationToken: TestContext.Current.CancellationToken);
            await store.SaveAsync(created with { Name = "Edited name" }, cancellationToken: TestContext.Current.CancellationToken);
            Assert.Single(store.Profiles[0].Attachments);
            var replacement = await new DocumentImporter().ImportBytesAsync("catalog.csv", ContextDocumentFormat.Csv, Encoding.UTF8.GetBytes("Product,Price\nChair,24000"), TestContext.Current.CancellationToken);
            await store.ReplaceDocumentAsync(created.Id, doc.Attachment.Id, replacement, TestContext.Current.CancellationToken);
            Assert.Null(await store.ReadDocumentAsync(doc.Attachment.Id, TestContext.Current.CancellationToken));
            doc = replacement;
            Assert.Single(store.Profiles[0].Attachments);
            Assert.NotNull(await store.ReadDocumentAsync(doc.Attachment.Id, cancellationToken: TestContext.Current.CancellationToken));
            var content = await File.ReadAllBytesAsync(Directory.GetFiles(root, "profiles-v1.aicontext", SearchOption.AllDirectories)[0], cancellationToken: TestContext.Current.CancellationToken);
            Assert.False(Encoding.UTF8.GetString(content).Contains("Edited name", StringComparison.Ordinal));
            using var reopened = new ContextProfileStore(root, "account-one", protector);
            await reopened.OpenAsync(cancellationToken: TestContext.Current.CancellationToken);
            Assert.Equal("Edited name", Assert.Single(reopened.Profiles).Name);
            using var other = new ContextProfileStore(root, "account-two", protector);
            await other.OpenAsync(cancellationToken: TestContext.Current.CancellationToken);
            Assert.Empty(other.Profiles);
            await reopened.RemoveDocumentAsync(created.Id, doc.Attachment.Id, cancellationToken: TestContext.Current.CancellationToken);
            Assert.Empty(reopened.Profiles[0].Attachments);
            Assert.Null(await reopened.ReadDocumentAsync(doc.Attachment.Id, cancellationToken: TestContext.Current.CancellationToken));
            await reopened.DeleteAsync(created.Id, cancellationToken: TestContext.Current.CancellationToken);
            Assert.Empty(reopened.Profiles);
            await reopened.DeleteAllAsync(cancellationToken: TestContext.Current.CancellationToken);
            Assert.False(Directory.Exists(Path.Combine(root, ContextProfileStore.AccountKey("account-one"))));
        }
        finally { if (Directory.Exists(root)) Directory.Delete(root, true); }
    }

    [Fact]
    public async Task ConcurrentSavesPersistAndCorruptStoreIsNeverOverwritten()
    {
        var root = Path.Combine(Path.GetTempPath(), Guid.NewGuid().ToString("N"));
        using var protector = new TestProtector();
        try
        {
            using var store = new ContextProfileStore(root, "account", protector);
            await store.OpenAsync(cancellationToken: TestContext.Current.CancellationToken);
            await Task.WhenAll(store.SaveAsync(new ContextProfile { Name = "One" }, cancellationToken: TestContext.Current.CancellationToken), store.SaveAsync(new ContextProfile { Name = "Two" }, cancellationToken: TestContext.Current.CancellationToken));
            Assert.Equal(2, store.Profiles.Count);
            var file = Directory.GetFiles(root, "profiles-v1.aicontext", SearchOption.AllDirectories)[0];
            var bytes = await File.ReadAllBytesAsync(file, cancellationToken: TestContext.Current.CancellationToken);
            bytes[^1] ^= 0xff;
            await File.WriteAllBytesAsync(file, bytes, cancellationToken: TestContext.Current.CancellationToken);
            using var broken = new ContextProfileStore(root, "account", protector);
            await broken.OpenAsync(cancellationToken: TestContext.Current.CancellationToken);
            Assert.Equal(ContextStorageState.Unreadable, broken.State);
            await Assert.ThrowsAsync<ContextImportException>(() => broken.SaveAsync(new ContextProfile { Name = "Never written" }, cancellationToken: TestContext.Current.CancellationToken));
            Assert.Equal(bytes, await File.ReadAllBytesAsync(file, cancellationToken: TestContext.Current.CancellationToken));
        }
        finally { if (Directory.Exists(root)) Directory.Delete(root, true); }
    }

    [Theory]
    [InlineData("case08/policy_cp1251.txt")]
    [InlineData("case08/policy_utf16le_bom.txt")]
    [InlineData("case08/policy_utf16be_bom.txt")]
    [InlineData("case08/policy_utf16le_nobom.txt")]
    public async Task TextEncodingsPreserveRussianFacts(string path)
    {
        var doc = await new DocumentImporter().ImportAsync(Fixture(path), cancellationToken: TestContext.Current.CancellationToken);
        Assert.Contains(doc.Segments, s => s.Text.Contains("Доставка по Казахстану", StringComparison.Ordinal));
    }

    [Fact]
    public async Task CachedFormulasAreMarkedAndInternalColumnsNeverReachPayload()
    {
        var doc = await new DocumentImporter().ImportAsync(Fixture("case04/formulas_numbers.xlsx"), cancellationToken: TestContext.Current.CancellationToken);
        var all = string.Join('\n', doc.Segments.Select(s => s.Text));
        Assert.Contains("(формула)", all, StringComparison.Ordinal);
        Assert.DoesNotContain("Закупка", all, StringComparison.Ordinal);
        Assert.Contains("2026-11-01", all, StringComparison.Ordinal);
        Assert.Contains("15%", all, StringComparison.Ordinal);
        Assert.NotEmpty(doc.Attachment.Summary.InternalColumnsSkipped);
    }

    [Theory]
    [InlineData("A-1043")]
    [InlineData("А-1043")]
    [InlineData("A1043")]
    [InlineData("A 1043")]
    public async Task SkuFormsSelectOnlyTheCorrectProduct(string query)
    {
        var doc = await new DocumentImporter().ImportAsync(Fixture("case02/sku.xlsx"), cancellationToken: TestContext.Current.CancellationToken);
        var plan = ContextPlanner.Build(null, [doc], "есть артикул " + query);
        var reference = Assert.Single(Assert.IsType<AirReply.Core.Generation.GenerationContextPayload>(plan.Payload).References);
        Assert.Contains("Артикул: A-1043;", reference, StringComparison.Ordinal);
        Assert.DoesNotContain("B-1043", reference, StringComparison.Ordinal);
    }

    [Fact]
    public async Task WideRowsAreProjectedAndStockIsJoinedAcrossSheets()
    {
        var importer = new DocumentImporter();
        var wide = await importer.ImportAsync(Fixture("case05/wide_rows.xlsx"), cancellationToken: TestContext.Current.CancellationToken);
        var plan = ContextPlanner.Build(null, [wide], "Сколько стоит диван угловой Комфорт?");
        Assert.Contains("Наименование: Диван угловой Комфорт", plan.Payload!.References[0], StringComparison.Ordinal);
        Assert.Contains("289990", plan.Payload.References[0], StringComparison.Ordinal);
        Assert.Contains("(partial)", plan.Payload.References[0], StringComparison.Ordinal);
        Assert.DoesNotContain("Поставщик", plan.Payload.References[0], StringComparison.Ordinal);
        var sheets = await importer.ImportAsync(Fixture("case05/price_and_stock_sheets.xlsx"), cancellationToken: TestContext.Current.CancellationToken);
        var stock = ContextPlanner.Build(null, [sheets], "Диван угловой Комфорт есть в наличии в Астане?");
        Assert.Contains(stock.Payload!.References, r => r.Contains("Астана: 2", StringComparison.Ordinal));
    }

    [Fact]
    public void InstructionsAreWholeFirstAndBudgetCountsGoHtmlEscaping()
    {
        Assert.Equal(65, ContextPlanner.EscapedBytes("Кофе \"Арабика\" & M&M's <new>"));
        var instructions = new string('я', 500);
        var profile = new ContextProfile { Name = "Shop", Instructions = new string('ю', 4000), Policies = "Возврат в течение 14 дней" };
        var plan = ContextPlanner.Build(profile, [], "test", instructions, new string('д', 400), new string('и', 400), true);
        Assert.StartsWith("Instructions for this message: " + instructions, plan.Payload!.Profile, StringComparison.Ordinal);
        Assert.True(plan.PromptBytes <= 3840);
        Assert.True(plan.Payload.Profile!.EnumerateRunes().Count() <= 1500);
        Assert.Throws<ContextBudgetException>(() => ContextPlanner.Build(null, [], "test", new string('я', 501)));
        Assert.Throws<ContextBudgetException>(() => ContextPlanner.Build(profile, [], new string('x', 4000)));
    }

    [Fact]
    public async Task CsvMultilineAndHeaderCommasPreservePriceAndPhysicalRowNumber()
    {
        var importer = new DocumentImporter();
        var csv = await importer.ImportBytesAsync("prices.csv", ContextDocumentFormat.Csv,
            Encoding.UTF8.GetBytes("sep=;\nTitle\nProduct;Price, ₸;Notes\nChair;\"11 990,50\";\"Line one\nLine two\"\n"), cancellationToken: TestContext.Current.CancellationToken);
        var row = Assert.Single(csv.Segments, s => s.Text.Contains("Chair", StringComparison.Ordinal));
        Assert.Contains("Price, ₸: 11 990,50", row.Text, StringComparison.Ordinal);
        Assert.Equal("row 4", row.Locator);
        Assert.Contains("Line one Line two", row.Text, StringComparison.Ordinal);
        var error = await Assert.ThrowsAsync<ContextImportException>(() => importer.ImportBytesAsync("bad.csv", ContextDocumentFormat.Csv, Encoding.UTF8.GetBytes("Product,Price\nChair,\"unclosed"), cancellationToken: TestContext.Current.CancellationToken));
        Assert.Equal(ContextImportError.Malformed, error.Error);
        Assert.Contains("line 2", error.Detail, StringComparison.Ordinal);
    }

    [Theory]
    [InlineData("case09/scan_image_only.pdf", ContextImportError.PdfNoText)]
    [InlineData("case09/encrypted_user_password.pdf", ContextImportError.PdfProtected)]
    public async Task PdfFailuresAreTyped(string path, ContextImportError error)
    {
        var ex = await Assert.ThrowsAsync<ContextImportException>(() => new DocumentImporter().ImportAsync(Fixture(path), cancellationToken: TestContext.Current.CancellationToken));
        Assert.Equal(error, ex.Error);
    }

    [Fact]
    public async Task OwnerPasswordPdfIsReadable()
    {
        var doc = await new DocumentImporter().ImportAsync(Fixture("case09/owner_password_only.pdf"), cancellationToken: TestContext.Current.CancellationToken);
        Assert.Contains(doc.Segments, s => s.Text.Contains("289 990", StringComparison.Ordinal));
    }

    private sealed class TestProtector : IDataProtector, IDisposable
    {
        private readonly EphemeralDataProtector inner = new();
        public byte[] Protect(ReadOnlySpan<byte> plaintext) => inner.Protect(plaintext);
        public byte[] Unprotect(ReadOnlySpan<byte> protectedData) => inner.Unprotect(protectedData);
        public void Dispose() { }
    }
}
