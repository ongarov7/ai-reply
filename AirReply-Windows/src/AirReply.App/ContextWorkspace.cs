using AirReply.Context;
using AirReply.Core.Generation;
using AirReply.Core.Infrastructure;
using AirReply.Presentation.Models;

namespace AirReply.App;

/// <summary>Publishes one immutable account/selection snapshot; storage mutations never outlive their account.</summary>
internal sealed class ContextWorkspace(string directory, Func<string, IDataProtector> protector) : IDisposable
{
    private readonly SemaphoreSlim gate = new(1, 1);
    private readonly Lock stateLock = new();
    private ContextProfileStore? store;
    private Snapshot snapshot = Snapshot.Empty;
    private string? requestedUser;
    private CancellationTokenSource accountCancellation = new();
    private CancellationTokenSource? importCancellation;
    private readonly List<CancellationTokenSource> retiredCancellations = [];
    private long accountVersion;
    private long importVersion;
    private int operations;
    private bool disposed;

    public Guid? SelectedProfileId => Volatile.Read(ref snapshot).SelectedProfileId;
    public IReadOnlyList<ContextProfile> Profiles => Volatile.Read(ref snapshot).Profiles;
    public ContextStorageState State => Volatile.Read(ref snapshot).State;
    public bool IsBusy => Volatile.Read(ref snapshot).IsBusy;
    public event EventHandler? Changed;

    public async Task SwitchAccountAsync(string? userId)
    {
        Ticket ticket;
        CancellationTokenSource previousCancellation;
        lock (stateLock)
        {
            ObjectDisposedException.ThrowIf(disposed, this);
            if (requestedUser == userId) return;
            requestedUser = userId;
            accountVersion++;
            importVersion++;
            importCancellation?.Cancel();
            previousCancellation = accountCancellation;
            retiredCancellations.Add(previousCancellation);
            accountCancellation = new();
            operations++;
            ticket = new Ticket(accountVersion, userId, accountCancellation.Token);
            // Clear the former account before waiting for any disk operation or decryption.
            Volatile.Write(ref snapshot, Snapshot.Empty with { IsBusy = userId is not null });
        }
        previousCancellation.Cancel();
        NotifyChanged();
        await gate.WaitAsync();
        ContextProfileStore? next = null;
        try
        {
            if (!IsCurrent(ticket)) return;
            store?.Dispose();
            store = null;
            if (userId is not null)
            {
                next = new ContextProfileStore(directory, userId, protector(userId));
                await next.OpenAsync(ticket.Cancellation);
            }
            lock (stateLock)
            {
                if (!IsCurrentLocked(ticket)) return;
                store = next;
                next = null;
                Volatile.Write(ref snapshot, new Snapshot(store?.Profiles.ToArray() ?? [], store?.State ?? ContextStorageState.Ready, null, [], false));
            }
        }
        catch (OperationCanceledException) when (!IsCurrent(ticket)) { }
        finally
        {
            next?.Dispose();
            ClearBusy(ticket);
            gate.Release();
            EndOperation();
            NotifyChanged();
        }
    }

    public async Task SelectAsync(Guid? id)
    {
        var ticket = BeginOperation();
        if (id is null)
            lock (stateLock)
                if (IsCurrentLocked(ticket)) { importVersion++; importCancellation?.Cancel(); }
        await gate.WaitAsync();
        try
        {
            RequireStore(ticket);
            SetBusy(ticket);
            await PublishAsync(ticket, id);
        }
        catch (OperationCanceledException) when (!IsCurrent(ticket)) { }
        finally { Finish(ticket); }
    }

    public GenerationContextPayload? Build(string source, string instruction, string conversation, bool reply)
    {
        Snapshot current;
        long version;
        lock (stateLock)
        {
            current = snapshot;
            version = accountVersion;
            if (current.IsBusy || disposed) throw new QuickBarException("issue.context_unavailable");
        }
        // The profile and documents come from the same published version, even if selection changes concurrently.
        var profile = current.Profiles.FirstOrDefault(p => p.Id == current.SelectedProfileId);
        var query = reply ? source.Trim() + (string.IsNullOrWhiteSpace(instruction) ? "" : "\n" + instruction.Trim()) : instruction.Trim();
        try
        {
            var plan = ContextPlanner.Build(profile, current.Documents, query, conversation, source, instruction, reply);
            lock (stateLock)
            {
                if (disposed || version != accountVersion) throw new QuickBarException("issue.context_unavailable");
                return plan.Payload;
            }
        }
        catch (ContextBudgetException ex) { throw new QuickBarException("issue.context_too_large", inner: ex); }
    }

    public async Task<ContextProfile> SaveAsync(ContextProfile profile)
    {
        var ticket = BeginOperation();
        await gate.WaitAsync();
        try
        {
            var current = RequireStore(ticket);
            SetBusy(ticket);
            var saved = await current.SaveAsync(profile, ticket.Cancellation);
            await PublishAsync(ticket, SelectedProfileId);
            EnsureCurrent(ticket);
            return saved;
        }
        catch (OperationCanceledException ex) when (!IsCurrent(ticket)) { throw AccountChanged(ex); }
        finally { Finish(ticket); }
    }

    public async Task ImportAsync(Guid profileId, string path)
    {
        var ticket = BeginOperation();
        long version;
        CancellationTokenSource cancellation;
        lock (stateLock)
        {
            version = importVersion;
            cancellation = CancellationTokenSource.CreateLinkedTokenSource(ticket.Cancellation);
            if (IsCurrentLocked(ticket))
            {
                version = ++importVersion;
                importCancellation?.Cancel();
                importCancellation = cancellation;
            }
        }
        var acquired = false;
        try
        {
            var document = await new DocumentImporter().ImportAsync(path, cancellation.Token);
            await gate.WaitAsync();
            acquired = true;
            if (!IsCurrentImport(ticket, version)) return;
            var current = RequireStore(ticket);
            // The captured profile may have been deleted while parsing the file.
            if (!current.Profiles.Any(p => p.Id == profileId)) return;
            SetBusy(ticket);
            await current.AddDocumentAsync(profileId, document, cancellation.Token);
            if (IsCurrentImport(ticket, version)) await PublishAsync(ticket, SelectedProfileId);
        }
        catch (OperationCanceledException) when (!IsCurrentImport(ticket, version)) { }
        finally
        {
            lock (stateLock) if (ReferenceEquals(importCancellation, cancellation)) importCancellation = null;
            cancellation.Dispose();
            if (acquired) { ClearBusy(ticket); gate.Release(); }
            EndOperation();
            NotifyChanged();
        }
    }

    public Task DeleteAsync(Guid id) => MutateAsync((current, ct) => current.DeleteAsync(id, ct), id);

    public Task RemoveDocumentAsync(Guid profileId, Guid documentId) =>
        MutateAsync((current, ct) => current.RemoveDocumentAsync(profileId, documentId, ct));

    public Task DeleteAllAsync()
    {
        lock (stateLock) { importVersion++; importCancellation?.Cancel(); }
        return MutateAsync((current, ct) => current.DeleteAllAsync(ct), clearSelection: true);
    }

    private async Task MutateAsync(Func<ContextProfileStore, CancellationToken, Task> mutate, Guid? deletedProfile = null, bool clearSelection = false)
    {
        var ticket = BeginOperation();
        await gate.WaitAsync();
        try
        {
            var current = RequireStore(ticket);
            SetBusy(ticket);
            await mutate(current, ticket.Cancellation);
            var selected = clearSelection || SelectedProfileId == deletedProfile ? null : SelectedProfileId;
            await PublishAsync(ticket, selected);
            EnsureCurrent(ticket);
        }
        catch (OperationCanceledException ex) when (!IsCurrent(ticket)) { throw AccountChanged(ex); }
        finally { Finish(ticket); }
    }

    private Ticket BeginOperation()
    {
        lock (stateLock)
        {
            ObjectDisposedException.ThrowIf(disposed, this);
            if (requestedUser is null) throw new InvalidOperationException("Sign in first.");
            operations++;
            return new Ticket(accountVersion, requestedUser, accountCancellation.Token);
        }
    }

    private ContextProfileStore RequireStore(Ticket ticket)
    {
        EnsureCurrent(ticket);
        return store ?? throw new InvalidOperationException("Context storage is unavailable.");
    }
    private void EnsureCurrent(Ticket ticket)
    {
        if (!IsCurrent(ticket)) throw AccountChanged();
    }
    private static InvalidOperationException AccountChanged(Exception? inner = null) => new("Account changed during the operation.", inner);
    private bool IsCurrent(Ticket ticket) { lock (stateLock) return IsCurrentLocked(ticket); }
    private bool IsCurrentLocked(Ticket ticket) => !disposed && ticket.Version == accountVersion && ticket.User == requestedUser;
    private bool IsCurrentImport(Ticket ticket, long version) { lock (stateLock) return IsCurrentLocked(ticket) && version == importVersion; }

    private void SetBusy(Ticket ticket)
    {
        lock (stateLock)
            if (IsCurrentLocked(ticket)) Volatile.Write(ref snapshot, snapshot with { IsBusy = true });
    }
    private void ClearBusy(Ticket ticket)
    {
        lock (stateLock)
            if (IsCurrentLocked(ticket)) Volatile.Write(ref snapshot, snapshot with { IsBusy = false });
    }

    private async Task PublishAsync(Ticket ticket, Guid? selectedId)
    {
        var current = RequireStore(ticket);
        var profiles = current.Profiles.ToArray();
        var selected = profiles.FirstOrDefault(p => p.Id == selectedId);
        var loaded = new List<ContextDocument>();
        if (selected is not null)
        {
            foreach (var attachment in selected.Attachments)
            {
                var document = await current.ReadDocumentAsync(attachment.Id, ticket.Cancellation);
                // An unavailable document remains removable in Settings and is never used for generation.
                if (document is not null) loaded.Add(document);
            }
        }
        lock (stateLock)
        {
            if (!IsCurrentLocked(ticket)) return;
            Volatile.Write(ref snapshot, new Snapshot(profiles, current.State, selected?.Id, loaded.ToArray(), false));
        }
    }

    private void Finish(Ticket ticket)
    {
        ClearBusy(ticket);
        gate.Release();
        EndOperation();
        NotifyChanged();
    }
    private void EndOperation()
    {
        lock (stateLock)
        {
            operations--;
            if (operations == 0)
            {
                foreach (var cancellation in retiredCancellations) cancellation.Dispose();
                retiredCancellations.Clear();
                if (disposed) DisposeResources();
            }
        }
    }
    public void Dispose()
    {
        lock (stateLock)
        {
            if (disposed) return;
            disposed = true;
            accountVersion++;
            importVersion++;
            importCancellation?.Cancel();
            requestedUser = null;
            Volatile.Write(ref snapshot, Snapshot.Empty);
            accountCancellation.Cancel();
            // Accepted operations retain the store and semaphore until their final release.
            if (operations == 0) DisposeResources();
        }
    }
    private void NotifyChanged()
    {
        if (!Volatile.Read(ref disposed)) Changed?.Invoke(this, EventArgs.Empty);
    }
    private void DisposeResources()
    {
        store?.Dispose();
        store = null;
        gate.Dispose();
        accountCancellation.Dispose();
        foreach (var cancellation in retiredCancellations) cancellation.Dispose();
        retiredCancellations.Clear();
    }

    private sealed record Snapshot(IReadOnlyList<ContextProfile> Profiles, ContextStorageState State,
        Guid? SelectedProfileId, IReadOnlyList<ContextDocument> Documents, bool IsBusy)
    {
        public static Snapshot Empty { get; } = new([], ContextStorageState.Ready, null, [], false);
    }
    private readonly record struct Ticket(long Version, string? User, CancellationToken Cancellation);
}
