using System.Text;
using AirReply.Core.Generation;
using AirReply.Core.Infrastructure;
using AirReply.Core.Localization;
using AirReply.Core.Settings;
using AirReply.Presentation.Platform;
using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;

namespace AirReply.Presentation.Models;

public enum BarPhase { Editing, Generating, Result, Recording, Transcribing }

public sealed class QuickBarViewModel : ObservableObject, IDisposable
{
    private readonly IQuickBarBackend backend;
    private readonly IClipboardService clipboard;
    private readonly IVoiceRecorder voice;
    private readonly IClock clock;
    private readonly IUiDispatcher dispatcher;
    private readonly CancellationTokenSource lifetime = new();
    private CancellationTokenSource? operation;
    private GenerationRequest? lastRequest;
    private DateTimeOffset? lastClosed;
    private DateTimeOffset? lastCopy;
    private CopiedText? copied;
    private string? accountId;
    private long accountSessionGeneration;
    private CancellationTokenSource? sessionTransitionOperation;
    private string draft = "", source = "", email = "", code = "", result = "", issue = "", customInstructions = "";
    private bool reply, codeSent, busy, retryable, extended, contextEnabled, copying, requestingVoice;
    private BarPhase phase;
    private Recipient recipient;
    private bool disposed;

    public QuickBarViewModel(IQuickBarBackend backend, IClipboardService clipboard, IVoiceRecorder voice,
        AppSettings settings, Localizer localizer, IUiDispatcher dispatcher, IClock? clock = null)
    {
        this.backend = backend;
        this.clipboard = clipboard;
        this.voice = voice;
        this.dispatcher = dispatcher;
        this.clock = clock ?? new SystemClock();
        accountId = backend.Snapshot.SignedIn ? backend.Snapshot.UserId : null;
        accountSessionGeneration = backend.Snapshot.SessionGeneration;
        Settings = settings;
        L = localizer;
        recipient = RecipientWire.FromWire(settings.LastRecipient) ?? Recipient.Client;
        SubmitCommand = new AsyncRelayCommand(SubmitAsync, () => !IsBusy && Phase == BarPhase.Editing && (SignedOut || CanEdit));
        RetryCommand = new AsyncRelayCommand(RetryAsync, () => !IsBusy && Account.SignedIn && Account.Ready && Account.HasConsent && retryable && lastRequest is not null);
        RegenerateCommand = new AsyncRelayCommand(RegenerateAsync, () => !IsBusy && Account.SignedIn && Account.HasConsent && lastRequest is not null);
        CopyCommand = new AsyncRelayCommand(CopyAsync, () => !IsBusy && !copying && Phase == BarPhase.Result && Result.Length > 0);
        ConsentCommand = new AsyncRelayCommand(() => RunAsync(ct => backend.AcceptConsentAsync(ct)));
        ResendCommand = new AsyncRelayCommand(() => RunAsync(ct => backend.ResendAsync(ct)));
        VoiceCommand = new AsyncRelayCommand(ToggleVoiceAsync, () => CanRecord || Phase == BarPhase.Recording);
        voice.Stopped += VoiceStopped;
        backend.Changed += BackendChanged;
    }

    public AppSettings Settings { get; }
    public Localizer L { get; private set; }
    public string this[string key] => L[key];
    public AccountSnapshot Account => backend.Snapshot;
    public bool SignedOut => !Account.SignedIn;
    public bool NeedsConsent => Account.SignedIn && Account.Ready && !Account.HasConsent;
    public bool CanEdit => Account.SignedIn && Account.Ready && Account.HasConsent && !IsBusy && Phase == BarPhase.Editing;
    public bool ShowEditor => Account.SignedIn && Account.Ready && !NeedsConsent;
    public bool ShowResult => Phase == BarPhase.Result;
    public bool ShowProgress => Phase is BarPhase.Generating or BarPhase.Transcribing;
    public bool IsRecording => Phase == BarPhase.Recording;
    public bool CanRecord => CanEdit && Account.VoiceAvailable && Phase != BarPhase.Result;
    public bool HoldsPanelOpen => requestingVoice || Phase is BarPhase.Recording or BarPhase.Transcribing;
    public bool HasIssue => Issue.Length > 0;
    public bool CanRetry => retryable && lastRequest is not null;
    public bool CodeSent { get => codeSent; private set { SetProperty(ref codeSent, value); Notify(); } }
    public string Draft { get => draft; set { SetProperty(ref draft, value); OnPropertyChanged(nameof(Counter)); } }
    public string Source { get => source; set => SetProperty(ref source, value); }
    public string Email { get => email; set => SetProperty(ref email, value); }
    public string Code { get => code; set => SetProperty(ref code, value); }
    public string Result { get => result; private set => SetProperty(ref result, value); }
    public string Issue { get => issue; private set { SetProperty(ref issue, value); OnPropertyChanged(nameof(HasIssue)); } }
    public bool IsReply { get => reply; set { SetProperty(ref reply, value); OnPropertyChanged(nameof(Placeholder)); } }
    public Recipient Recipient { get => recipient; set { SetProperty(ref recipient, value); Settings.LastRecipient = value.ToWire(); } }
    public bool IsBusy { get => busy; private set { SetProperty(ref busy, value); Notify(); } }
    public BarPhase Phase { get => phase; private set { SetProperty(ref phase, value); Notify(); } }
    public bool Extended { get => extended; set => SetProperty(ref extended, value); }
    public bool ContextEnabled { get => contextEnabled; set => SetProperty(ref contextEnabled, value); }
    public string CustomInstructions { get => customInstructions; set => SetProperty(ref customInstructions, value); }
    public string Counter => L.Format("bar.counter", ServerLength(Draft), Account.InstructionLimit);
    public string Placeholder => L[IsReply ? "bar.placeholder.reply" : "bar.placeholder.compose"];
    public string ConsentBody => L.Format("consent.body", Account.Provider);
    public string ProgressText => L[Phase == BarPhase.Transcribing ? "voice.transcribing" : "bar.generating"];
    public string CopyLabel => L[lastCopy is { } t && clock.Now - t < TimeSpan.FromMilliseconds(700) ? "result.copied" : "result.copy"];
    public string? InputLanguage { get; set; }
    /// <summary>Reads the remembered foreground target's keyboard layout at the instant of submission.</summary>
    public Func<string?>? ReadInputLanguage { get; set; }
    public Func<GenerationContextPayload?>? BuildContext { get; set; }
    public event EventHandler? CopyCompleted;
    public event EventHandler? HideRequested;
    public event EventHandler? SourceFocusRequested;
    public event EventHandler? AccountChanged;
    public IAsyncRelayCommand SubmitCommand { get; }
    public IAsyncRelayCommand RetryCommand { get; }
    public IAsyncRelayCommand RegenerateCommand { get; }
    public IAsyncRelayCommand CopyCommand { get; }
    public IAsyncRelayCommand ConsentCommand { get; }
    public IAsyncRelayCommand ResendCommand { get; }
    public IAsyncRelayCommand VoiceCommand { get; }

    public Task InitializeAsync() => RunAsync(ct => backend.InitializeAsync(ct));
    public Task RefreshAccountAsync() => RunAsync(ct => backend.RefreshAsync(ct), clearIssue: false);

    private void BackendChanged(object? sender, EventArgs e) => dispatcher.Post(() =>
    {
        if (disposed) return;
        SynchronizeAccount(cancelOperation: !ReferenceEquals(operation, sessionTransitionOperation));
        if (!Account.ContextAvailable) { ContextEnabled = false; Extended = false; }
        Notify();
        AccountChanged?.Invoke(this, EventArgs.Empty);
    });

    public async Task OpenAsync(CopiedTextResult fresh)
    {
        SynchronizeAccount();
        if (lastClosed is { } closed && clock.Now - closed > TimeSpan.FromMinutes(10) && !IsBusy && !IsRecording)
            ResetConversation();
        if (Settings.OfferCopiedText && fresh.Status == CopiedTextStatus.Text && fresh.Copied is { } text && Phase == BarPhase.Editing)
        {
            if (copied?.SequenceNumber != text.SequenceNumber) ResetConversation();
            copied = text;
            Source = text.Text;
            IsReply = true;
        }
        await RunAsync(ct => backend.RefreshAsync(ct), clearIssue: false);
    }

    public void Close()
    {
        lastClosed = clock.Now;
        if (requestingVoice) { operation?.Cancel(); voice.Cancel(); }
        if (IsRecording) { voice.Cancel(); Phase = BarPhase.Editing; }
    }

    public void ResetConversation(bool cancelOperation = true)
    {
        if (cancelOperation) operation?.Cancel();
        voice.Cancel();
        requestingVoice = false;
        Draft = Source = Result = CustomInstructions = "";
        Extended = ContextEnabled = IsReply = false;
        copied = null;
        lastRequest = null;
        lastCopy = null;
        retryable = false;
        Issue = "";
        Phase = BarPhase.Editing;
    }

    public void DismissSource()
    {
        if (copied is not null) clipboard.MarkHandled(copied);
        copied = null;
        Source = "";
        IsReply = false;
    }

    public void ReportIssue(string key)
    {
        Issue = L[key];
        retryable = false;
        Notify();
    }

    public void OnSessionLocked()
    {
        if (requestingVoice || IsRecording) Cancel();
    }

    public void AnotherEmail() { operation?.Cancel(); backend.UseAnotherEmail(); CodeSent = false; Code = ""; Issue = ""; }

    public async Task SignOutAsync()
    {
        Cancel();
        await RunAsync(ct => backend.SignOutAsync(ct), replaceRunning: true, allowsSessionChange: true);
        ResetConversation();
        CodeSent = false;
        Code = "";
    }

    public void Cancel()
    {
        operation?.Cancel();
        voice.Cancel();
        requestingVoice = false;
        OnPropertyChanged(nameof(HoldsPanelOpen));
        if (Phase is BarPhase.Generating or BarPhase.Recording or BarPhase.Transcribing) Phase = BarPhase.Editing;
        else HideRequested?.Invoke(this, EventArgs.Empty);
    }

    public async Task SubmitAsync()
    {
        if (IsBusy) return;
        SynchronizeAccount();
        if (SignedOut)
        {
            await RunAsync(async ct =>
            {
                if (CodeSent) { await backend.VerifyAsync(Code.Trim(), ct); ct.ThrowIfCancellationRequested(); Code = ""; CodeSent = false; ResetConversation(); }
                else { await backend.RequestCodeAsync(Email.Trim(), ct); ct.ThrowIfCancellationRequested(); CodeSent = true; }
            }, allowsSessionChange: true);
            return;
        }
        if (!CanEdit) return;
        Issue = "";
        retryable = false;
        Notify();
        if (ReadInputLanguage is not null) InputLanguage = ReadInputLanguage();
        if (IsReply && ServerLength(Source) == 0) { SourceFocusRequested?.Invoke(this, EventArgs.Empty); return; }
        if (ServerLength(Draft) > Account.InstructionLimit) { Issue = L.Format("issue.instruction_too_long", Account.InstructionLimit); return; }
        if (IsReply && ServerLength(Source) > Account.SourceLimit) { Issue = L.Format("issue.source_too_long", Account.SourceLimit); return; }
        if (!IsReply && ServerLength(Draft) == 0) { Issue = L["issue.instruction_missing"]; return; }
        if (ContextEnabled && ServerLength(CustomInstructions) > 500) { Issue = L.Format("issue.instruction_too_long", 500); return; }
        GenerationContextPayload? context = null;
        try
        {
            if (ContextEnabled && Account.ContextAvailable && BuildContext?.Invoke() is { } built)
                context = new(built.Profile, built.References.ToArray());
        }
        catch (QuickBarException ex) { ReportIssue(ex.Key); return; }
        catch (Exception ex) when (ex is IOException or InvalidOperationException or ArgumentException or System.Security.Cryptography.CryptographicException)
        { Issue = L["bar.context.load_failed"]; return; }
        if ((ContextEnabled || Extended) && !Account.ContextAvailable) { Issue = L["bar.context.server_update"]; return; }
        lastRequest = new GenerationRequest(Source, Draft.Trim(), Recipient, IsReply, false,
            Account.SenderProfile ? InputLanguage : null, Guid.NewGuid().ToString(),
            new GenerationOptions(context, Extended ? 600 : null));
        await GenerateAsync(lastRequest);
    }

    public Task RetryAsync() => !IsBusy && retryable && Account.SignedIn && Account.Ready && Account.HasConsent && lastRequest is { } request
        ? GenerateAsync(request) : Task.CompletedTask;

    public Task RegenerateAsync()
    {
        if (IsBusy || !Account.SignedIn || !Account.Ready || !Account.HasConsent || lastRequest is not { } request) return Task.CompletedTask;
        lastRequest = request with { IdempotencyKey = Guid.NewGuid().ToString(), Regenerate = true };
        return GenerateAsync(lastRequest);
    }

    private async Task GenerateAsync(GenerationRequest request)
    {
        if (IsBusy) return;
        var user = Account.UserId;
        var sessionGeneration = Account.SessionGeneration;
        CancellationToken generationToken = default;
        Phase = BarPhase.Generating;
        await RunAsync(async ct =>
        {
            generationToken = ct;
            var text = await backend.GenerateAsync(request, ct);
            ct.ThrowIfCancellationRequested();
            if (!Account.SignedIn || Account.UserId != user || Account.SessionGeneration != sessionGeneration) return;
            Result = text;
            lastCopy = null;
            lastClosed = clock.Now;
            Phase = BarPhase.Result;
            if (copied is not null) clipboard.MarkHandled(copied);
        }, retryAllowed: true);
        if (!generationToken.IsCancellationRequested && Phase == BarPhase.Generating) Phase = BarPhase.Editing;
    }

    public async Task CopyAsync()
    {
        if (IsBusy || copying || Phase != BarPhase.Result || Result.Length == 0 ||
            lastCopy is { } t && clock.Now - t < TimeSpan.FromMilliseconds(700)) return;
        copying = true;
        CopyCommand.NotifyCanExecuteChanged();
        var text = Result;
        var user = Account.UserId;
        var sessionGeneration = Account.SessionGeneration;
        try
        {
            if (!await clipboard.CopyAsync(text)) { Issue = L["clipboard.failed"]; return; }
            if (!Account.SignedIn || Account.UserId != user || Account.SessionGeneration != sessionGeneration || text != Result) return;
            lastCopy = lastClosed = clock.Now;
            CustomInstructions = "";
            Extended = false;
            OnPropertyChanged(nameof(CopyLabel));
            CopyCompleted?.Invoke(this, EventArgs.Empty);
            _ = RestoreCopyLabelAsync();
        }
        finally { copying = false; CopyCommand.NotifyCanExecuteChanged(); }
    }

    private async Task RestoreCopyLabelAsync()
    {
        try { await Task.Delay(700, lifetime.Token); dispatcher.Post(() => OnPropertyChanged(nameof(CopyLabel))); }
        catch (OperationCanceledException) when (lifetime.IsCancellationRequested) { }
    }

    public async Task ToggleVoiceAsync()
    {
        if (IsRecording) { await FinishVoiceAsync(); return; }
        if (!CanRecord) return;
        if (ReadInputLanguage is not null) InputLanguage = ReadInputLanguage();
        requestingVoice = true;
        OnPropertyChanged(nameof(HoldsPanelOpen));
        try
        {
            await RunAsync(async ct =>
            {
                try { await voice.StartAsync(TimeSpan.FromSeconds(40), ct); ct.ThrowIfCancellationRequested(); Phase = BarPhase.Recording; }
                catch (OperationCanceledException) { voice.Cancel(); throw; }
            });
        }
        finally { requestingVoice = false; OnPropertyChanged(nameof(HoldsPanelOpen)); }
    }

    private void VoiceStopped(object? sender, VoiceCaptureException? error) => dispatcher.Post(() =>
    {
        if (!IsRecording) return;
        if (error is null) _ = FinishVoiceAsync();
        else { voice.Cancel(); Phase = BarPhase.Editing; Issue = L[VoiceKey(error)]; }
    });

    private async Task FinishVoiceAsync()
    {
        if (IsBusy || Phase != BarPhase.Recording) return;
        var user = Account.UserId;
        var sessionGeneration = Account.SessionGeneration;
        CancellationToken transcriptionToken = default;
        Phase = BarPhase.Transcribing;
        await RunAsync(async ct =>
        {
            transcriptionToken = ct;
            var audio = await voice.StopAsync();
            if (audio is null) { Issue = L["voice.issue.nothing_heard"]; return; }
            try
            {
                ct.ThrowIfCancellationRequested();
                if (audio.Duration < TimeSpan.FromMilliseconds(500)) { Issue = L["voice.issue.too_short"]; return; }
                var preference = Settings.DictationLanguage == DictationLanguage.Auto ? null : Settings.DictationLanguage.ToString().ToLowerInvariant();
                var languages = new[] { preference, InputLanguage, "ru", "kk", "en" }
                    .Select(value => value?.Split('-', '_')[0].ToLowerInvariant())
                    .Where(value => value is "ru" or "kk" or "en").OfType<string>().Distinct(StringComparer.Ordinal).Take(3).ToArray();
                var text = await backend.TranscribeAsync(await File.ReadAllBytesAsync(audio.FilePath, ct), audio.Duration, languages, ct);
                ct.ThrowIfCancellationRequested();
                if (!Account.SignedIn || Account.UserId != user || Account.SessionGeneration != sessionGeneration) return;
                Draft = string.Join(' ', new[] { Draft.Trim(), text.Trim() }.Where(s => s.Length > 0));
            }
            finally { File.Delete(audio.FilePath); }
        });
        if (!transcriptionToken.IsCancellationRequested && Phase == BarPhase.Transcribing) Phase = BarPhase.Editing;
    }

    private async Task RunAsync(Func<CancellationToken, Task> action, bool clearIssue = true, bool replaceRunning = false,
        bool retryAllowed = false, bool allowsSessionChange = false)
    {
        if (IsBusy && !replaceRunning) return;
        operation?.Cancel();
        var current = CancellationTokenSource.CreateLinkedTokenSource(lifetime.Token);
        operation = current;
        sessionTransitionOperation = allowsSessionChange ? current : null;
        IsBusy = true;
        if (clearIssue) { Issue = ""; retryable = false; }
        try { await action(current.Token); }
        catch (Exception) when (current.IsCancellationRequested) { }
        catch (QuickBarException ex)
        {
            Issue = L[ex.Key]; retryable = retryAllowed && ex.Retryable;
            if (ex.KeySpent && lastRequest is { } request) lastRequest = request with { IdempotencyKey = Guid.NewGuid().ToString() };
        }
        catch (VoiceCaptureException ex) { Issue = L[VoiceKey(ex)]; }
        catch (Exception ex) when (ex is HttpRequestException or IOException or TimeoutException or OperationCanceledException)
        { Issue = L[ex is HttpRequestException ? "issue.offline" : "issue.server"]; retryable = retryAllowed; }
        finally
        {
            if (ReferenceEquals(operation, current))
            {
                operation = null;
                sessionTransitionOperation = null;
                SynchronizeAccount();
                if (!Account.ContextAvailable) { ContextEnabled = false; Extended = false; }
                if (!Account.SignedIn && Phase != BarPhase.Editing) { Result = ""; Phase = BarPhase.Editing; }
                IsBusy = false;
                AccountChanged?.Invoke(this, EventArgs.Empty);
                Notify();
            }
            current.Dispose();
        }
    }

    private void SynchronizeAccount(bool cancelOperation = true)
    {
        var user = Account.SignedIn ? Account.UserId : null;
        if (accountId == user && accountSessionGeneration == Account.SessionGeneration) return;
        accountId = user;
        accountSessionGeneration = Account.SessionGeneration;
        ResetConversation(cancelOperation);
        Code = "";
        CodeSent = false;
    }

    public void ChangeLanguage(Localizer localizer) { L = localizer; Notify(); OnPropertyChanged("Item[]"); }

    private void Notify()
    {
        foreach (var property in new[] { nameof(Account), nameof(SignedOut), nameof(NeedsConsent), nameof(CanEdit),
            nameof(ShowEditor), nameof(ShowResult), nameof(ShowProgress), nameof(IsRecording), nameof(CanRecord),
            nameof(HasIssue), nameof(CanRetry), nameof(Counter), nameof(Placeholder), nameof(ConsentBody), nameof(ProgressText), nameof(CopyLabel), nameof(HoldsPanelOpen) })
            OnPropertyChanged(property);
        SubmitCommand?.NotifyCanExecuteChanged(); RetryCommand?.NotifyCanExecuteChanged();
        RegenerateCommand?.NotifyCanExecuteChanged(); CopyCommand?.NotifyCanExecuteChanged(); VoiceCommand?.NotifyCanExecuteChanged();
    }

    private static string VoiceKey(VoiceCaptureException ex) => ex.Error switch
    {
        VoiceCaptureError.PermissionDenied => "voice.issue.permission",
        VoiceCaptureError.NoInputDevice => "voice.issue.no_device",
        _ => "voice.issue.recording_failed",
    };

    public static int ServerLength(string text) => text.Trim().EnumerateRunes().Count();

    public void Dispose()
    {
        if (disposed) return;
        disposed = true;
        backend.Changed -= BackendChanged;
        lifetime.Cancel();
        operation?.Cancel(); operation?.Dispose();
        lifetime.Dispose();
        voice.Cancel();
        voice.Stopped -= VoiceStopped;
        GC.SuppressFinalize(this);
    }
}
