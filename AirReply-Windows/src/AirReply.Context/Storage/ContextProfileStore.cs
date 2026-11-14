using System.Security.Cryptography;
using System.Text;
using System.Text.Json;
using AirReply.Core.Infrastructure;

namespace AirReply.Context;

public enum ContextStorageState { Ready, Unreadable, NewerVersion }

/// <summary>An account-scoped encrypted store. Pass a DPAPI protector bound to Entropy(userId) on Windows.</summary>
public sealed class ContextProfileStore : IDisposable
{
    private static readonly int[] RetryDelays = [50, 150, 400];
    private static readonly byte[] Header = "AIRCTX\x01\0"u8.ToArray();
    private static readonly JsonSerializerOptions Json = new() { PropertyNamingPolicy = JsonNamingPolicy.SnakeCaseLower, MaxDepth = 32 };
    private readonly string directory;
    private readonly string rootDirectory;
    private readonly string accountKey;
    private readonly IDataProtector protector;
    private readonly SemaphoreSlim gate = new(1, 1);
    private IReadOnlyList<ContextProfile> profiles = [];
    private bool opened;

    public ContextProfileStore(string rootDirectory, string userId, IDataProtector protector)
    {
        ArgumentException.ThrowIfNullOrWhiteSpace(userId);
        ArgumentNullException.ThrowIfNull(protector);
        accountKey = AccountKey(userId);
        this.rootDirectory = Path.GetFullPath(rootDirectory);
        directory = Path.Combine(this.rootDirectory, accountKey);
        this.protector = protector;
    }

    public static string AccountKey(string userId) => Convert.ToHexStringLower(SHA256.HashData(Encoding.UTF8.GetBytes("aireply.context.account:" + userId)))[..32];
    public static string Entropy(string userId) => "aireply.context.v1:" + AccountKey(userId);
    public IReadOnlyList<ContextProfile> Profiles => profiles;
    public ContextStorageState State { get; private set; } = ContextStorageState.Ready;

    public async Task OpenAsync(CancellationToken cancellationToken = default)
    {
        await gate.WaitAsync(cancellationToken).ConfigureAwait(false);
        try
        {
            profiles = [];
            State = ContextStorageState.Ready;
            if (Directory.Exists(rootDirectory)) RefuseReparse(rootDirectory);
            if (!Directory.Exists(directory)) { opened = true; return; }
            RefuseReparse(directory);
            var path = Path.Combine(directory, "profiles-v1.aicontext");
            if (File.Exists(path))
            {
                var index = await ReadAsync<ProfileIndex>(path, 8 * 1024 * 1024, cancellationToken).ConfigureAwait(false);
                if (index.Schema > 1) { State = ContextStorageState.NewerVersion; opened = true; return; }
                if (index.Schema != 1 || index.AccountKey != accountKey || index.Profiles is null || index.Profiles.Any(p => p is null) || index.Profiles.Count > 24 || index.Profiles.Select(p => p.Id).Distinct().Count() != index.Profiles.Count)
                    throw new InvalidDataException("Invalid profile index.");
                foreach (var p in index.Profiles) p.Validate();
                profiles = index.Profiles;
            }
            foreach (var file in Directory.EnumerateFiles(directory))
            {
                cancellationToken.ThrowIfCancellationRequested();
                if (file.EndsWith(".tmp", StringComparison.Ordinal) ||
                    (Path.GetFileName(file).StartsWith("doc-", StringComparison.Ordinal) && !profiles.SelectMany(p => p.Attachments).Any(a => FilePath(a.Id) == file)))
                    TryDelete(file);
            }
        }
        catch (Exception ex) when (ex is IOException or InvalidDataException or UnauthorizedAccessException or CryptographicException or JsonException or ArgumentException)
        {
            State = ContextStorageState.Unreadable;
            profiles = [];
        }
        finally { opened = true; gate.Release(); }
    }

    public async Task<ContextProfile> SaveAsync(ContextProfile profile, CancellationToken cancellationToken = default)
    {
        ArgumentNullException.ThrowIfNull(profile);
        profile.Validate();
        await gate.WaitAsync(cancellationToken).ConfigureAwait(false);
        try
        {
            EnsureWritable();
            var current = profiles.FirstOrDefault(p => p.Id == profile.Id);
            if (current is null && profiles.Count >= 24) throw new ContextImportException(ContextImportError.TooManyProfiles);
            // An editor's snapshot must never overwrite newly imported documents.
            var saved = profile with { Name = profile.Name.Trim(), Attachments = current?.Attachments ?? [], CreatedAt = current?.CreatedAt ?? profile.CreatedAt, UpdatedAt = DateTimeOffset.UtcNow };
            var next = current is null ? profiles.Append(saved).ToArray() : profiles.Select(p => p.Id == saved.Id ? saved : p).ToArray();
            await SaveIndexAsync(next, cancellationToken).ConfigureAwait(false);
            profiles = next;
            return saved;
        }
        finally { gate.Release(); }
    }

    public async Task DeleteAsync(Guid id, CancellationToken cancellationToken = default)
    {
        await gate.WaitAsync(cancellationToken).ConfigureAwait(false);
        try
        {
            EnsureWritable();
            var old = profiles.FirstOrDefault(p => p.Id == id);
            var next = profiles.Where(p => p.Id != id).ToArray();
            await SaveIndexAsync(next, cancellationToken).ConfigureAwait(false);
            profiles = next;
            if (old is not null) foreach (var a in old.Attachments) TryDelete(FilePath(a.Id));
        }
        finally { gate.Release(); }
    }

    public Task AddDocumentAsync(Guid profileId, ContextDocument document, CancellationToken cancellationToken = default) =>
        PutDocumentAsync(profileId, document, null, cancellationToken);

    public Task ReplaceDocumentAsync(Guid profileId, Guid replacedDocumentId, ContextDocument document, CancellationToken cancellationToken = default) =>
        PutDocumentAsync(profileId, document, replacedDocumentId, cancellationToken);

    private async Task PutDocumentAsync(Guid profileId, ContextDocument document, Guid? replacedDocumentId, CancellationToken cancellationToken)
    {
        ArgumentNullException.ThrowIfNull(document);
        await gate.WaitAsync(cancellationToken).ConfigureAwait(false);
        try
        {
            EnsureWritable();
            var profile = profiles.FirstOrDefault(p => p.Id == profileId) ?? throw new ArgumentException("Profile no longer exists.", nameof(profileId));
            if (replacedDocumentId.HasValue && !profile.Attachments.Any(a => a.Id == replacedDocumentId.Value)) throw new ArgumentException("Document no longer exists.", nameof(replacedDocumentId));
            if (!replacedDocumentId.HasValue && profile.Attachments.Count >= 8) throw new ContextImportException(ContextImportError.ProfileAttachmentsFull);
            if (profiles.SelectMany(p => p.Attachments).Any(a => a.Id == document.Attachment.Id)) throw new ArgumentException("Document already exists.", nameof(document));
            var bytes = JsonSerializer.SerializeToUtf8Bytes(document, Json);
            try
            {
                if (bytes.Length > 16 * 1024 * 1024) throw new ContextImportException(ContextImportError.StorageFull);
                var attachment = document.Attachment with { StoredBytes = bytes.Length };
                var next = profiles.Select(p => p.Id == profileId ? p with
                {
                    UpdatedAt = DateTimeOffset.UtcNow,
                    Attachments = replacedDocumentId.HasValue ? p.Attachments.Select(a => a.Id == replacedDocumentId.Value ? attachment : a).ToArray() : p.Attachments.Append(attachment).ToArray(),
                } : p).ToArray();
                if (next.SelectMany(p => p.Attachments).Sum(a => a.StoredBytes) > 32 * 1024 * 1024 - 128) throw new ContextImportException(ContextImportError.StorageFull);
                var file = FilePath(attachment.Id);
                await WriteAsync(file, bytes, cancellationToken).ConfigureAwait(false);
                try { await SaveIndexAsync(next, cancellationToken).ConfigureAwait(false); }
                catch { TryDelete(file); throw; }
                profiles = next;
                if (replacedDocumentId.HasValue) TryDelete(FilePath(replacedDocumentId.Value));
            }
            finally { CryptographicOperations.ZeroMemory(bytes); }
        }
        finally { gate.Release(); }
    }

    public async Task RemoveDocumentAsync(Guid profileId, Guid documentId, CancellationToken cancellationToken = default)
    {
        await gate.WaitAsync(cancellationToken).ConfigureAwait(false);
        try
        {
            EnsureWritable();
            var next = profiles.Select(p => p.Id == profileId ? p with { Attachments = p.Attachments.Where(a => a.Id != documentId).ToArray() } : p).ToArray();
            await SaveIndexAsync(next, cancellationToken).ConfigureAwait(false);
            profiles = next;
            if (!profiles.SelectMany(p => p.Attachments).Any(a => a.Id == documentId)) TryDelete(FilePath(documentId));
        }
        finally { gate.Release(); }
    }

    /// <summary>Missing/corrupt documents are unavailable, leaving the profile index intact.</summary>
    public async Task<ContextDocument?> ReadDocumentAsync(Guid id, CancellationToken cancellationToken = default)
    {
        await gate.WaitAsync(cancellationToken).ConfigureAwait(false);
        try
        {
            if (!profiles.SelectMany(p => p.Attachments).Any(a => a.Id == id)) return null;
            var doc = await ReadAsync<ContextDocument>(FilePath(id), 16 * 1024 * 1024, cancellationToken).ConfigureAwait(false);
            return doc.Attachment is not null && doc.Attachment.Id == id && doc.Segments is not null && doc.Segments.Count <= 50000 &&
                doc.Segments.All(s => s is not null && s.Text is not null && s.Locator is not null && s.Cells is not null) ? doc : null;
        }
        catch (Exception ex) when (ex is IOException or InvalidDataException or UnauthorizedAccessException or CryptographicException or JsonException) { return null; }
        finally { gate.Release(); }
    }

    public async Task DeleteAllAsync(CancellationToken cancellationToken = default)
    {
        await gate.WaitAsync(cancellationToken).ConfigureAwait(false);
        try
        {
            if (Directory.Exists(rootDirectory)) RefuseReparse(rootDirectory);
            if (Directory.Exists(directory)) { RefuseReparse(directory); Directory.Delete(directory, true); }
            profiles = [];
            opened = true;
            State = ContextStorageState.Ready;
        }
        finally { gate.Release(); }
    }

    private void EnsureWritable()
    {
        if (!opened || State != ContextStorageState.Ready) throw new ContextImportException(ContextImportError.StorageFailed);
    }

    private async Task SaveIndexAsync(IReadOnlyList<ContextProfile> next, CancellationToken ct)
    {
        var bytes = JsonSerializer.SerializeToUtf8Bytes(new ProfileIndex(1, accountKey, next), Json);
        try
        {
            if (bytes.Length > 8 * 1024 * 1024 || bytes.Length + next.SelectMany(p => p.Attachments).Sum(a => a.StoredBytes) > 32 * 1024 * 1024)
                throw new ContextImportException(ContextImportError.StorageFull);
            await WriteAsync(Path.Combine(directory, "profiles-v1.aicontext"), bytes, ct).ConfigureAwait(false);
        }
        finally { CryptographicOperations.ZeroMemory(bytes); }
    }

    private async Task WriteAsync(string path, byte[] plaintext, CancellationToken ct)
    {
        string? temp = null;
        try
        {
            if (Directory.Exists(rootDirectory)) RefuseReparse(rootDirectory);
            Directory.CreateDirectory(directory);
            RefuseReparse(directory);
            if (File.Exists(path)) RefuseReparse(path);
            temp = path + "." + Guid.NewGuid().ToString("N") + ".tmp";
            var encrypted = protector.Protect(plaintext);
            await using (var stream = new FileStream(temp, FileMode.CreateNew, FileAccess.Write, FileShare.None, 4096, FileOptions.WriteThrough | FileOptions.Asynchronous))
            {
                await stream.WriteAsync(Header, ct).ConfigureAwait(false);
                await stream.WriteAsync(encrypted, ct).ConfigureAwait(false);
                stream.Flush(true);
            }
            for (var attempt = 0; ; attempt++)
            {
                ct.ThrowIfCancellationRequested();
                try { File.Move(temp, path, true); break; }
                catch (IOException) when (attempt < 3) { await Task.Delay(RetryDelays[attempt], ct).ConfigureAwait(false); }
            }
        }
        catch (Exception ex) when (ex is IOException or UnauthorizedAccessException or CryptographicException)
        { throw new ContextImportException(ContextImportError.StorageFailed, inner: ex); }
        finally { if (temp is not null) TryDelete(temp); }
    }

    private async Task<T> ReadAsync<T>(string path, int cap, CancellationToken ct)
    {
        RefuseReparse(path);
        if (new FileInfo(path).Length > cap + 65536) throw new InvalidDataException("Encrypted file too large.");
        var bytes = await File.ReadAllBytesAsync(path, ct).ConfigureAwait(false);
        if (!bytes.AsSpan().StartsWith(Header)) throw new InvalidDataException("Unknown context file format.");
        var plaintext = protector.Unprotect(bytes.AsSpan(Header.Length));
        try
        {
            if (plaintext.Length > cap) throw new InvalidDataException("Context file too large.");
            return JsonSerializer.Deserialize<T>(plaintext, Json) ?? throw new JsonException("Empty context file.");
        }
        finally { CryptographicOperations.ZeroMemory(plaintext); }
    }

    private string FilePath(Guid id) => Path.Combine(directory, "doc-" + id.ToString("N") + "-v1.aicontext");
    private static void RefuseReparse(string path)
    {
        if ((File.GetAttributes(path) & FileAttributes.ReparsePoint) != 0) throw new IOException("Linked storage is not supported.");
    }
    private static void TryDelete(string path) { try { File.Delete(path); } catch (IOException) { } catch (UnauthorizedAccessException) { } }
    public void Dispose() => gate.Dispose();
    private sealed record ProfileIndex(int Schema, string AccountKey, IReadOnlyList<ContextProfile> Profiles);
}
