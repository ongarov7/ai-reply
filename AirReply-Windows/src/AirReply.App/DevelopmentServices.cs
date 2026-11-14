using AirReply.Core.Infrastructure;
using AirReply.Presentation.Models;
using AirReply.Presentation.Platform;
using Avalonia.Threading;

namespace AirReply.App;

internal sealed class UiDispatcher : IUiDispatcher
{
    public bool CheckAccess() => Dispatcher.UIThread.CheckAccess();
    public void Post(Action action) => Dispatcher.UIThread.Post(action);
    public async Task InvokeAsync(Func<Task> action) => await Dispatcher.UIThread.InvokeAsync(action);
}

/// <summary>Non-Windows preview never stores credentials or calls production.</summary>
internal sealed class PreviewServices : IClipboardService, IVoiceRecorder, IFocusTracker, IPasteService,
    IStartupService, IShell, IKeyboardLanguage, ISecretStore, IQuickBarBackend
{
    public event EventHandler? Changed { add { } remove { } }
    private readonly Dictionary<string, string> secrets = new(StringComparer.Ordinal);
    public event EventHandler<float>? Level { add { } remove { } }
    public event EventHandler<VoiceCaptureException?>? Stopped { add { } remove { } }
    public bool IsRecording => false;
    public bool IsEnabled { get; private set; }
    public nint PreviousWindow => 0;
    public AccountSnapshot Snapshot => new();
    public CopiedTextResult TakeFreshCopiedText(TimeSpan maxAge) => CopiedTextResult.None;
    public void MarkHandled(CopiedText copied) { }
    public Task<bool> CopyAsync(string text) => Task.FromResult(true);
    public void Dispose() { }
    public ValueTask DisposeAsync() => ValueTask.CompletedTask;
    public void RememberForegroundWindow() { }
    public bool RestorePreviousWindow() => false;
    public PasteAvailability GetAvailability(nint targetWindow) => PasteAvailability.NoTarget;
    public Task<bool> PasteAsync(nint targetWindow, CancellationToken cancellationToken = default) => Task.FromResult(false);
    public Task StartAsync(TimeSpan maxDuration, CancellationToken cancellationToken = default) => Task.FromException(new VoiceCaptureException(VoiceCaptureError.NoInputDevice));
    public Task<RecordedAudio?> StopAsync() => Task.FromResult<RecordedAudio?>(null);
    public void Cancel() { }
    public void SetEnabled(bool enabled) => IsEnabled = enabled;
    public void OpenUri(Uri uri) => System.Diagnostics.Process.Start(new System.Diagnostics.ProcessStartInfo(uri.AbsoluteUri) { UseShellExecute = true });
    public string? CurrentInputLanguage(nint window) => null;
    public string? Read(string key) => secrets.GetValueOrDefault(key);
    public void Write(string key, string value) => secrets[key] = value;
    public void Delete(string key) => secrets.Remove(key);
    public Task InitializeAsync(CancellationToken cancellationToken = default) => Task.CompletedTask;
    public Task RefreshAsync(CancellationToken cancellationToken = default) => Task.CompletedTask;
    private static Task Unavailable() => Task.FromException(new QuickBarException("app.platform_preview"));
    public Task RequestCodeAsync(string email, CancellationToken cancellationToken = default) => Unavailable();
    public Task VerifyAsync(string code, CancellationToken cancellationToken = default) => Unavailable();
    public void UseAnotherEmail() { }
    public Task ResendAsync(CancellationToken cancellationToken = default) => Unavailable();
    public Task SignOutAsync(CancellationToken cancellationToken = default) => Task.CompletedTask;
    public Task AcceptConsentAsync(CancellationToken cancellationToken = default) => Unavailable();
    public Task<string> GenerateAsync(GenerationRequest request, CancellationToken cancellationToken = default) => Task.FromException<string>(new QuickBarException("app.platform_preview"));
    public Task<string> TranscribeAsync(byte[] wav, TimeSpan duration, IReadOnlyList<string> languages, CancellationToken cancellationToken = default) => Task.FromException<string>(new QuickBarException("app.platform_preview"));
    public Task<Uri> PlansUriAsync(CancellationToken cancellationToken = default) => Task.FromResult(new Uri("https://ai-reply.kz/payment"));
}
