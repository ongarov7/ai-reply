using System.Buffers.Binary;
using System.IO.Compression;
using System.Text;
using AirReply.Core.Infrastructure;
using Xunit;

namespace AirReply.Context.Tests;

public sealed class ImportSecurityTests
{
    [Theory]
    [InlineData("../word/document.xml", "<document/>", ContextImportError.UnsafeContent)]
    [InlineData("word/vbaProject.bin", "macro", ContextImportError.UnsafeContent)]
    [InlineData("word/document.xml", "<!DOCTYPE doc [<!ENTITY leak SYSTEM 'file:///private'>]><doc>&leak;</doc>", ContextImportError.UnsafeContent)]
    [InlineData("word/document.xml", "<?xml version='1.0' encoding='UTF-7'?><doc>+ADw-!DOCTYPE doc+AD4-</doc>", ContextImportError.UnsafeContent)]
    public async Task UnsafeArchiveEntriesAreRejectedBeforeOpenXml(string name, string content, ContextImportError expected)
    {
        var bytes = Zip(name, Encoding.UTF8.GetBytes(content));
        var error = await Assert.ThrowsAsync<ContextImportException>(() => new DocumentImporter().ImportBytesAsync("bad.docx", ContextDocumentFormat.Docx, bytes, TestContext.Current.CancellationToken));
        Assert.Equal(expected, error.Error);
    }

    [Fact]
    public async Task WrongCrcTruncationAndEncryptionFlagsHaveTypedFailures()
    {
        var original = await File.ReadAllBytesAsync(Path.Combine(AppContext.BaseDirectory, "Fixtures", "case09", "price_table.docx"), TestContext.Current.CancellationToken);
        var central = FindSignature(original, 0x02014b50);
        Assert.True(central >= 0);
        foreach (var modification in new[] { 0, 1, 2 })
        {
            var bytes = original.ToArray();
            var expected = ContextImportError.Malformed;
            if (modification == 0) bytes[central + 16] ^= 0x5a;
            if (modification == 1) bytes = bytes[..^10];
            if (modification == 2) { bytes[central + 8] |= 1; expected = ContextImportError.UnsafeContent; }
            var error = await Assert.ThrowsAsync<ContextImportException>(() => new DocumentImporter().ImportBytesAsync("bad.docx", ContextDocumentFormat.Docx, bytes, TestContext.Current.CancellationToken));
            Assert.Equal(expected, error.Error);
        }
    }

    [Fact]
    public async Task ExpandedArchivesAndTooManyRowsAreBounded()
    {
        var bomb = Zip("word/document.xml", new byte[16 * 1024 * 1024 + 1]);
        var error = await Assert.ThrowsAsync<ContextImportException>(() => new DocumentImporter().ImportBytesAsync("large.docx", ContextDocumentFormat.Docx, bomb, TestContext.Current.CancellationToken));
        Assert.Equal(ContextImportError.ArchiveTooLarge, error.Error);
        var csv = Encoding.UTF8.GetBytes("Product,Price\n" + string.Concat(Enumerable.Repeat("Chair,5\n", 50001)));
        var rowsError = await Assert.ThrowsAsync<ContextImportException>(() => new DocumentImporter().ImportBytesAsync("many.csv", ContextDocumentFormat.Csv, csv, TestContext.Current.CancellationToken));
        Assert.Equal(ContextImportError.TooManyRows, rowsError.Error);
    }

    [Fact]
    public async Task NewerOrForeignAccountIndexIsNeverSilentlyReset()
    {
        var root = Path.Combine(Path.GetTempPath(), Guid.NewGuid().ToString("N"));
        var protector = new EphemeralDataProtector();
        try
        {
            var account = ContextProfileStore.AccountKey("account");
            var directory = Path.Combine(root, account);
            Directory.CreateDirectory(directory);
            var future = "{\"schema\":2,\"account_key\":\"" + account + "\",\"profiles\":[]}";
            var bytes = "AIRCTX\x01\0"u8.ToArray().Concat(protector.Protect(Encoding.UTF8.GetBytes(future))).ToArray();
            var index = Path.Combine(directory, "profiles-v1.aicontext");
            await File.WriteAllBytesAsync(index, bytes, TestContext.Current.CancellationToken);
            using var store = new ContextProfileStore(root, "account", protector);
            await store.OpenAsync(TestContext.Current.CancellationToken);
            Assert.Equal(ContextStorageState.NewerVersion, store.State);
            await Assert.ThrowsAsync<ContextImportException>(() => store.SaveAsync(new ContextProfile { Name = "Blocked" }, TestContext.Current.CancellationToken));
            Assert.Equal(bytes, await File.ReadAllBytesAsync(index, TestContext.Current.CancellationToken));
            var otherDirectory = Path.Combine(root, ContextProfileStore.AccountKey("other-account"));
            Directory.CreateDirectory(otherDirectory);
            var foreign = "{\"schema\":1,\"account_key\":\"" + account + "\",\"profiles\":[]}";
            await File.WriteAllBytesAsync(Path.Combine(otherDirectory, "profiles-v1.aicontext"), "AIRCTX\x01\0"u8.ToArray().Concat(protector.Protect(Encoding.UTF8.GetBytes(foreign))).ToArray(), TestContext.Current.CancellationToken);
            using var other = new ContextProfileStore(root, "other-account", protector);
            await other.OpenAsync(TestContext.Current.CancellationToken);
            Assert.Equal(ContextStorageState.Unreadable, other.State);
        }
        finally { Directory.Delete(root, true); }
    }

    [Fact]
    public async Task ProfileAndAttachmentCapsPreserveExistingData()
    {
        var root = Path.Combine(Path.GetTempPath(), Guid.NewGuid().ToString("N"));
        try
        {
            using var store = new ContextProfileStore(root, "account", new EphemeralDataProtector());
            await store.OpenAsync(TestContext.Current.CancellationToken);
            for (var i = 0; i < 24; i++) await store.SaveAsync(new ContextProfile { Name = "Profile " + i }, TestContext.Current.CancellationToken);
            var full = await Assert.ThrowsAsync<ContextImportException>(() => store.SaveAsync(new ContextProfile { Name = "25" }, TestContext.Current.CancellationToken));
            Assert.Equal(ContextImportError.TooManyProfiles, full.Error);
            var profileId = store.Profiles[0].Id;
            var importer = new DocumentImporter();
            for (var i = 0; i < 8; i++) await store.AddDocumentAsync(profileId, await importer.ImportBytesAsync("facts.txt", ContextDocumentFormat.Txt, "Delivery costs 5000 KZT."u8.ToArray(), TestContext.Current.CancellationToken), TestContext.Current.CancellationToken);
            var extra = await importer.ImportBytesAsync("extra.txt", ContextDocumentFormat.Txt, "Extra facts for the store."u8.ToArray(), TestContext.Current.CancellationToken);
            var attachmentFull = await Assert.ThrowsAsync<ContextImportException>(() => store.AddDocumentAsync(profileId, extra, TestContext.Current.CancellationToken));
            Assert.Equal(ContextImportError.ProfileAttachmentsFull, attachmentFull.Error);
            Assert.Equal(8, store.Profiles[0].Attachments.Count);
        }
        finally { Directory.Delete(root, true); }
    }

    private static byte[] Zip(string name, byte[] content)
    {
        using var stream = new MemoryStream();
        using (var zip = new ZipArchive(stream, ZipArchiveMode.Create, true))
        {
            using var entry = zip.CreateEntry(name, CompressionLevel.Optimal).Open();
            entry.Write(content);
        }
        return stream.ToArray();
    }
    private static int FindSignature(byte[] bytes, uint signature)
    {
        for (var i = 0; i <= bytes.Length - 4; i++) if (BinaryPrimitives.ReadUInt32LittleEndian(bytes.AsSpan(i, 4)) == signature) return i;
        return -1;
    }
}
