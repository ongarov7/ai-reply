using System.Collections.Concurrent;
using System.Text;
using AirReply.Context;
using AirReply.Core.Infrastructure;
using AirReply.Presentation.Models;
using Xunit;

namespace AirReply.App.Tests;

public sealed class ContextWorkspaceTests
{
    [Fact]
    public async Task AccountSwitchClearsProfilesBeforeWaitingAndCancelsTheFormerMutation()
    {
        using var fixture = new WorkspaceFixture();
        using var workspace = fixture.Create();
        await workspace.SwitchAccountAsync("alice");
        var profile = await workspace.SaveAsync(new ContextProfile { Name = "Alice private profile", Instructions = "ALICE-SECRET" });
        await workspace.SelectAsync(profile.Id);
        using var block = fixture.For("alice").BlockNextProtect();
        var save = Task.Run(() => workspace.SaveAsync(profile with { Name = "Pending private edit" }), TestContext.Current.CancellationToken);
        await block.Entered.WaitAsync(TimeSpan.FromSeconds(10), TestContext.Current.CancellationToken);
        var switching = workspace.SwitchAccountAsync("bob");
        Assert.Empty(workspace.Profiles);
        Assert.Null(workspace.SelectedProfileId);
        Assert.True(workspace.IsBusy);
        Assert.Equal("issue.context_unavailable", Assert.Throws<QuickBarException>(() => workspace.Build("", "query", "", false)).Key);
        block.Release();
        await Assert.ThrowsAsync<InvalidOperationException>(() => save);
        await switching;
        Assert.Empty(workspace.Profiles);
        await workspace.SwitchAccountAsync("alice");
        Assert.Equal("Alice private profile", Assert.Single(workspace.Profiles).Name);
        Assert.Null(workspace.SelectedProfileId);
    }

    [Fact]
    public async Task LatestAccountRequestWinsEvenWhenAnEarlierAccountIsDecrypting()
    {
        using var fixture = new WorkspaceFixture();
        using (var seed = fixture.Create())
        {
            await seed.SwitchAccountAsync("bob");
            await seed.SaveAsync(new ContextProfile { Name = "Bob private profile" });
        }
        using var workspace = fixture.Create();
        await workspace.SwitchAccountAsync("alice");
        await workspace.SaveAsync(new ContextProfile { Name = "Alice private profile" });
        using var block = fixture.For("bob").BlockNextUnprotect();
        var bob = Task.Run(() => workspace.SwitchAccountAsync("bob"), TestContext.Current.CancellationToken);
        await block.Entered.WaitAsync(TimeSpan.FromSeconds(10), TestContext.Current.CancellationToken);
        Assert.Empty(workspace.Profiles);
        var carol = workspace.SwitchAccountAsync("carol");
        Assert.Empty(workspace.Profiles);
        block.Release();
        await Task.WhenAll(bob, carol);
        Assert.Empty(workspace.Profiles);
        var saved = await workspace.SaveAsync(new ContextProfile { Name = "Carol profile" });
        Assert.Equal(saved.Id, Assert.Single(workspace.Profiles).Id);
        await workspace.SwitchAccountAsync("bob");
        Assert.Equal("Bob private profile", Assert.Single(workspace.Profiles).Name);
    }

    [Fact]
    public async Task ImportIsDiscardedWhenAccountChangesBeforeItsWriteCompletes()
    {
        using var fixture = new WorkspaceFixture();
        using var workspace = fixture.Create();
        await workspace.SwitchAccountAsync("alice");
        var profile = await workspace.SaveAsync(new ContextProfile { Name = "Alice" });
        var path = await fixture.TextAsync("old.csv", "Product,Price\nSecret chair,25000");
        using var block = fixture.For("alice").BlockNextProtect();
        var importing = Task.Run(() => workspace.ImportAsync(profile.Id, path), TestContext.Current.CancellationToken);
        await block.Entered.WaitAsync(TimeSpan.FromSeconds(10), TestContext.Current.CancellationToken);
        var switching = workspace.SwitchAccountAsync("bob");
        Assert.Empty(workspace.Profiles);
        block.Release();
        await Task.WhenAll(importing, switching);
        Assert.Empty(workspace.Profiles);
        await workspace.SwitchAccountAsync("alice");
        Assert.Empty(Assert.Single(workspace.Profiles).Attachments);
    }

    [Fact]
    public async Task NewImportCancelsOlderImportAndGenerationKeepsItsLoadedSnapshot()
    {
        using var fixture = new WorkspaceFixture();
        using var workspace = fixture.Create();
        await workspace.SwitchAccountAsync("alice");
        var profile = await workspace.SaveAsync(new ContextProfile { Name = "Furniture" });
        await workspace.SelectAsync(profile.Id);
        var oldFile = await fixture.TextAsync("old.csv", "Product,Price\nChair,25000");
        var newFile = await fixture.TextAsync("new.csv", "Product,Price\nChair,24000");
        using var block = fixture.For("alice").BlockNextProtect();
        var oldImport = Task.Run(() => workspace.ImportAsync(profile.Id, oldFile), TestContext.Current.CancellationToken);
        await block.Entered.WaitAsync(TimeSpan.FromSeconds(10), TestContext.Current.CancellationToken);
        var newImport = workspace.ImportAsync(profile.Id, newFile);
        block.Release();
        await Task.WhenAll(oldImport, newImport);
        Assert.Equal("new.csv", Assert.Single(Assert.Single(workspace.Profiles).Attachments).Name);
        var plan = workspace.Build("", "Chair", "", false);
        Assert.Contains("24000", Assert.Single(plan!.References), StringComparison.Ordinal);
        // A generation snapshot does not read a file again after selection or depend on the caller's source file.
        File.Delete(newFile);
        var documentPath = Directory.GetFiles(fixture.Directory, "doc-*.aicontext", SearchOption.AllDirectories).Single();
        await File.WriteAllBytesAsync(documentPath, [0, 1, 2], TestContext.Current.CancellationToken);
        var cached = workspace.Build("", "Chair", "", false);
        Assert.Equal(plan.Profile, cached!.Profile);
        Assert.Equal(plan.References, cached.References);
        await workspace.SelectAsync(null);
        Assert.Null(workspace.Build("", "Chair", "", false));
        Assert.Contains("24000", Assert.Single(plan.References), StringComparison.Ordinal);
        await workspace.SelectAsync(profile.Id);
        Assert.Null(workspace.Build("", "Chair", "", false));
        Assert.Single(Assert.Single(workspace.Profiles).Attachments); // Unavailable metadata remains removable.
    }

    [Theory]
    [InlineData(false)]
    [InlineData(true)]
    public async Task ClearingContextOrDeletingAllDiscardsAnImportStillBeingSaved(bool deleteAll)
    {
        using var fixture = new WorkspaceFixture();
        using var workspace = fixture.Create();
        await workspace.SwitchAccountAsync("alice");
        var profile = await workspace.SaveAsync(new ContextProfile { Name = "Furniture" });
        await workspace.SelectAsync(profile.Id);
        var path = await fixture.TextAsync("discard.csv", "Product,Price\nChair,25000");
        using var block = fixture.For("alice").BlockNextProtect();
        var importing = Task.Run(() => workspace.ImportAsync(profile.Id, path), TestContext.Current.CancellationToken);
        await block.Entered.WaitAsync(TimeSpan.FromSeconds(10), TestContext.Current.CancellationToken);
        var clearing = deleteAll ? workspace.DeleteAllAsync() : workspace.SelectAsync(null);
        block.Release();
        await Task.WhenAll(importing, clearing);
        Assert.Null(workspace.SelectedProfileId);
        if (deleteAll) Assert.Empty(workspace.Profiles);
        else Assert.Empty(Assert.Single(workspace.Profiles).Attachments);
        Assert.Empty(Directory.GetFiles(fixture.Directory, "doc-*.aicontext", SearchOption.AllDirectories));
    }

    [Fact]
    public async Task DeleteAndDisposeCannotDisposeAStoreWhileAnOperationStillUsesIt()
    {
        using var fixture = new WorkspaceFixture();
        var workspace = fixture.Create();
        await workspace.SwitchAccountAsync("alice");
        var profile = await workspace.SaveAsync(new ContextProfile { Name = "Private" });
        using var block = fixture.For("alice").BlockNextProtect();
        var deleting = Task.Run(() => workspace.DeleteAsync(profile.Id), TestContext.Current.CancellationToken);
        await block.Entered.WaitAsync(TimeSpan.FromSeconds(10), TestContext.Current.CancellationToken);
        workspace.Dispose();
        Assert.Empty(workspace.Profiles);
        block.Release();
        await Assert.ThrowsAsync<InvalidOperationException>(() => deleting);
        await Assert.ThrowsAsync<ObjectDisposedException>(() => workspace.SwitchAccountAsync("bob"));
        workspace.Dispose();
        using var reopened = fixture.Create();
        await reopened.SwitchAccountAsync("alice");
        Assert.Equal("Private", Assert.Single(reopened.Profiles).Name);
    }

    [Fact]
    public async Task CorruptStoreStaysUntouchedAndBudgetErrorsUseTheQuickBarContract()
    {
        using var fixture = new WorkspaceFixture();
        using var workspace = fixture.Create();
        await workspace.SwitchAccountAsync("alice");
        await workspace.SaveAsync(new ContextProfile { Name = "Private" });
        await workspace.SwitchAccountAsync(null);
        var index = Directory.GetFiles(fixture.Directory, "profiles-v1.aicontext", SearchOption.AllDirectories).Single();
        byte[] corrupt = [0, 1, 2, 3, 4];
        await File.WriteAllBytesAsync(index, corrupt, TestContext.Current.CancellationToken);
        await workspace.SwitchAccountAsync("alice");
        Assert.Equal(ContextStorageState.Unreadable, workspace.State);
        Assert.Empty(workspace.Profiles);
        await Assert.ThrowsAsync<ContextImportException>(() => workspace.SaveAsync(new ContextProfile { Name = "Never overwrite" }));
        Assert.Equal(corrupt, await File.ReadAllBytesAsync(index, TestContext.Current.CancellationToken));
        var issue = Assert.Throws<QuickBarException>(() => workspace.Build("", "query", new string('я', 501), false));
        Assert.Equal("issue.context_too_large", issue.Key);
        Assert.IsType<ContextBudgetException>(issue.InnerException);
        await workspace.DeleteAllAsync();
        Assert.Equal(ContextStorageState.Ready, workspace.State);
        Assert.Empty(workspace.Profiles);
    }

    private sealed class WorkspaceFixture : IDisposable
    {
        private readonly ConcurrentDictionary<string, BlockingProtector> protectors = new(StringComparer.Ordinal);
        public string Directory { get; } = Path.Combine(Path.GetTempPath(), "AIReply-workspace-" + Guid.NewGuid().ToString("N"));
        public BlockingProtector For(string user) => protectors.GetOrAdd(user, _ => new BlockingProtector());
        public ContextWorkspace Create() => new(Directory, For);
        public async Task<string> TextAsync(string name, string text)
        {
            System.IO.Directory.CreateDirectory(Directory);
            var path = Path.Combine(Directory, name);
            await File.WriteAllTextAsync(path, text, Encoding.UTF8, TestContext.Current.CancellationToken);
            return path;
        }
        public void Dispose()
        {
            foreach (var p in protectors.Values) p.Dispose();
            if (System.IO.Directory.Exists(Directory)) System.IO.Directory.Delete(Directory, true);
        }
    }
    private sealed class BlockingProtector : IDataProtector, IDisposable
    {
        private readonly EphemeralDataProtector inner = new();
        private Block? nextProtect;
        private Block? nextUnprotect;
        public Block BlockNextProtect() { var block = new Block(); Interlocked.Exchange(ref nextProtect, block); return block; }
        public Block BlockNextUnprotect() { var block = new Block(); Interlocked.Exchange(ref nextUnprotect, block); return block; }
        public byte[] Protect(ReadOnlySpan<byte> plaintext) { Interlocked.Exchange(ref nextProtect, null)?.Wait(); return inner.Protect(plaintext); }
        public byte[] Unprotect(ReadOnlySpan<byte> protectedData) { Interlocked.Exchange(ref nextUnprotect, null)?.Wait(); return inner.Unprotect(protectedData); }
        public void Dispose() { nextProtect?.Dispose(); nextUnprotect?.Dispose(); }
    }
    private sealed class Block : IDisposable
    {
        private readonly TaskCompletionSource entered = new(TaskCreationOptions.RunContinuationsAsynchronously);
        private readonly ManualResetEventSlim release = new(false);
        public Task Entered => entered.Task;
        public void Wait() { entered.TrySetResult(); if (!release.Wait(TimeSpan.FromSeconds(15))) throw new TimeoutException("Test did not release its crypto operation."); }
        public void Release() => release.Set();
        public void Dispose() { release.Set(); release.Dispose(); }
    }
}
