using AirReply.Core.Generation;
using AirReply.Core.Infrastructure;
using AirReply.Core.Localization;
using AirReply.Core.Settings;
using AirReply.Presentation.Models;
using AirReply.Presentation.Platform;
using Xunit;

namespace AirReply.Presentation.Tests;

public sealed class QuickBarViewModelTests
{
    [Fact]
    public async Task Submit_snapshots_request_and_retry_keeps_original_context_and_key()
    {
        using var fixture = new Fixture();
        var references = new List<string> { "original document" };
        fixture.Vm.ContextEnabled = fixture.Vm.Extended = true;
        fixture.Vm.IsReply = true;
        fixture.Vm.Source = "  исходное сообщение  ";
        fixture.Vm.Draft = "  вежливо  ";
        fixture.Vm.Recipient = Recipient.Business;
        fixture.Vm.BuildContext = () => new("original profile", references);
        fixture.Backend.Generate = (_, _) => throw new QuickBarException("issue.offline", retryable: true);
        await fixture.Vm.SubmitAsync();
        Assert.Equal(BarPhase.Editing, fixture.Vm.Phase);
        Assert.True(fixture.Vm.CanRetry);
        var first = Assert.Single(fixture.Backend.Requests);
        Assert.Equal("  исходное сообщение  ", first.Source);
        Assert.Equal("вежливо", first.Instruction);
        Assert.Equal(Recipient.Business, first.Recipient);
        Assert.Equal(600, first.Options.OutputCharacterLimit);
        references[0] = "changed document";
        fixture.Vm.Source = "новое сообщение";
        fixture.Vm.Draft = "другая инструкция";
        fixture.Vm.BuildContext = () => new("changed profile", []);
        fixture.Backend.Generate = (_, _) => Task.FromResult("Готовый ответ");
        await fixture.Vm.RetryAsync();
        var retried = fixture.Backend.Requests[1];
        Assert.Equal(first, retried);
        Assert.Equal("original document", Assert.Single(retried.Options.Context!.References));
        Assert.Equal("original profile", retried.Options.Context.Profile);
        Assert.Equal(BarPhase.Result, fixture.Vm.Phase);
        Assert.False(fixture.Vm.CanRetry);
    }

    [Fact]
    public async Task Spent_key_retry_gets_fresh_key_and_regenerate_keeps_snapshot()
    {
        using var fixture = new Fixture();
        fixture.Vm.Draft = "исходный запрос";
        fixture.Backend.Generate = (_, _) => throw new QuickBarException("issue.server", retryable: true, keySpent: true);
        await fixture.Vm.SubmitAsync();
        var first = Assert.Single(fixture.Backend.Requests);
        fixture.Backend.Generate = (_, _) => Task.FromResult("готово");
        await fixture.Vm.RetryAsync();
        var retried = fixture.Backend.Requests[1];
        Assert.NotEqual(first.IdempotencyKey, retried.IdempotencyKey);
        Assert.Equal(first.Instruction, retried.Instruction);
        fixture.Vm.Draft = "отредактированный запрос";
        await fixture.Vm.RegenerateAsync();
        var variant = fixture.Backend.Requests[2];
        Assert.True(variant.Regenerate);
        Assert.NotEqual(retried.IdempotencyKey, variant.IdempotencyKey);
        Assert.Equal(first.Instruction, variant.Instruction);
    }

    [Theory]
    [InlineData(false, "", "issue.instruction_missing")]
    [InlineData(false, "long", "issue.instruction_too_long")]
    [InlineData(true, "source", "issue.source_too_long")]
    public async Task Local_validation_blocks_invalid_generation(bool reply, string input, string issueKey)
    {
        using var fixture = new Fixture();
        fixture.Backend.Snapshot = fixture.Backend.Snapshot with { SourceLimit = 3, InstructionLimit = 3 };
        fixture.Vm.IsReply = reply;
        if (reply) fixture.Vm.Source = input;
        else fixture.Vm.Draft = input;
        await fixture.Vm.SubmitAsync();
        Assert.Empty(fixture.Backend.Requests);
        var expected = issueKey == "issue.instruction_missing" ? fixture.Vm.L[issueKey] : fixture.Vm.L.Format(issueKey, 3);
        Assert.Equal(expected, fixture.Vm.Issue);
    }

    [Fact]
    public async Task Limits_count_unicode_runes_and_empty_reply_focuses_source()
    {
        using var fixture = new Fixture();
        fixture.Backend.Snapshot = fixture.Backend.Snapshot with { InstructionLimit = 2 };
        fixture.Vm.Draft = " 😀😀 ";
        Assert.Equal(2, QuickBarViewModel.ServerLength(fixture.Vm.Draft));
        await fixture.Vm.SubmitAsync();
        Assert.Single(fixture.Backend.Requests);
        fixture.Vm.ResetConversation();
        fixture.Vm.IsReply = true;
        var focused = 0;
        fixture.Vm.SourceFocusRequested += (_, _) => focused++;
        await fixture.Vm.SubmitAsync();
        Assert.Equal(1, focused);
        Assert.Single(fixture.Backend.Requests);
    }

    [Fact]
    public async Task Context_options_require_capability_and_inactive_instructions_do_not_leak()
    {
        using var fixture = new Fixture();
        fixture.Backend.Snapshot = fixture.Backend.Snapshot with { ContextAvailable = false };
        fixture.Vm.Draft = "запрос";
        fixture.Vm.Extended = true;
        await fixture.Vm.SubmitAsync();
        Assert.Equal(fixture.Vm.L["bar.context.server_update"], fixture.Vm.Issue);
        Assert.Empty(fixture.Backend.Requests);
        fixture.Vm.Extended = false;
        fixture.Vm.CustomInstructions = new string('a', 501);
        fixture.Vm.BuildContext = () => throw new InvalidOperationException("must not be called while inactive");
        await fixture.Vm.SubmitAsync();
        var request = Assert.Single(fixture.Backend.Requests);
        Assert.Null(request.Options.Context);
        Assert.Null(request.Options.OutputCharacterLimit);
    }

    [Fact]
    public async Task Active_conversation_instruction_limit_and_builder_errors_block_request()
    {
        using var fixture = new Fixture();
        fixture.Vm.Draft = "запрос";
        fixture.Vm.ContextEnabled = true;
        fixture.Vm.CustomInstructions = new string('a', 501);
        await fixture.Vm.SubmitAsync();
        Assert.Equal(fixture.Vm.L.Format("issue.instruction_too_long", 500), fixture.Vm.Issue);
        fixture.Vm.CustomInstructions = "коротко";
        fixture.Vm.BuildContext = () => throw new IOException("broken profile");
        await fixture.Vm.SubmitAsync();
        Assert.Equal(fixture.Vm.L["bar.context.load_failed"], fixture.Vm.Issue);
        Assert.Empty(fixture.Backend.Requests);
    }

    [Fact]
    public async Task Typed_context_builder_failure_is_reported_before_network_call()
    {
        using var fixture = new Fixture();
        fixture.Vm.Draft = "запрос";
        fixture.Vm.ContextEnabled = true;
        fixture.Vm.BuildContext = () => throw new QuickBarException("issue.context_too_large");
        await fixture.Vm.SubmitAsync();
        Assert.Empty(fixture.Backend.Requests);
        Assert.Equal(fixture.Vm.L["issue.context_too_large"], fixture.Vm.Issue);
        Assert.False(fixture.Vm.CanRetry);
    }

    [Fact]
    public async Task Server_disabling_context_clears_hidden_options_and_allows_plain_submit()
    {
        using var fixture = new Fixture();
        fixture.Vm.Draft = "запрос";
        fixture.Vm.ContextEnabled = fixture.Vm.Extended = true;
        fixture.Backend.Generate = (_, _) =>
        {
            fixture.Backend.Snapshot = fixture.Backend.Snapshot with { ContextAvailable = false };
            throw new QuickBarException("issue.context_unavailable");
        };
        await fixture.Vm.SubmitAsync();
        Assert.False(fixture.Vm.ContextEnabled);
        Assert.False(fixture.Vm.Extended);
        fixture.Backend.Generate = (_, _) => Task.FromResult("готово");
        await fixture.Vm.SubmitAsync();
        Assert.Equal(2, fixture.Backend.Requests.Count);
        Assert.Null(fixture.Backend.Requests[1].Options.Context);
        Assert.Null(fixture.Backend.Requests[1].Options.OutputCharacterLimit);
    }

    [Fact]
    public async Task New_validation_and_external_issue_do_not_offer_stale_generation_retry()
    {
        using var fixture = new Fixture();
        fixture.Vm.Draft = "запрос";
        fixture.Backend.Generate = (_, _) => throw new QuickBarException("issue.offline", retryable: true);
        await fixture.Vm.SubmitAsync();
        Assert.True(fixture.Vm.CanRetry);
        fixture.Vm.Draft = "";
        await fixture.Vm.SubmitAsync();
        Assert.False(fixture.Vm.CanRetry);
        await fixture.Vm.RetryAsync();
        Assert.Single(fixture.Backend.Requests);
        fixture.Vm.ReportIssue("clipboard.failed");
        Assert.Equal(fixture.Vm.L["clipboard.failed"], fixture.Vm.Issue);
        Assert.False(fixture.Vm.CanRetry);
    }

    [Fact]
    public async Task Keyboard_layout_is_read_for_each_new_submit_but_retry_preserves_original()
    {
        using var fixture = new Fixture();
        var language = "kk";
        fixture.Vm.ReadInputLanguage = () => language;
        fixture.Vm.Draft = "запрос";
        await fixture.Vm.SubmitAsync();
        Assert.Equal("kk", fixture.Backend.Requests[0].InputLanguage);
        fixture.Vm.ResetConversation();
        fixture.Vm.Draft = "другой запрос";
        language = "en";
        fixture.Backend.Snapshot = fixture.Backend.Snapshot with { SenderProfile = false };
        await fixture.Vm.SubmitAsync();
        Assert.Null(fixture.Backend.Requests[1].InputLanguage);
        Assert.Equal("en", fixture.Vm.InputLanguage);
    }

    [Fact]
    public async Task Consent_and_unready_account_block_generation_and_voice()
    {
        using var fixture = new Fixture();
        fixture.Vm.Draft = "запрос";
        fixture.Backend.Snapshot = fixture.Backend.Snapshot with { HasConsent = false };
        Assert.True(fixture.Vm.NeedsConsent);
        Assert.False(fixture.Vm.CanEdit);
        await fixture.Vm.SubmitAsync();
        await fixture.Vm.ToggleVoiceAsync();
        Assert.Empty(fixture.Backend.Requests);
        Assert.Equal(0, fixture.Voice.Starts);
        fixture.Backend.Snapshot = fixture.Backend.Snapshot with { Ready = false };
        Assert.False(fixture.Vm.ShowEditor);
        Assert.False(fixture.Vm.SubmitCommand.CanExecute(null));
    }

    [Fact]
    public async Task Signin_uses_email_then_code_without_generating()
    {
        using var fixture = new Fixture(signedIn: false);
        fixture.Vm.Email = " user@example.com ";
        await fixture.Vm.SubmitAsync();
        Assert.True(fixture.Vm.CodeSent);
        Assert.Equal("user@example.com", Assert.Single(fixture.Backend.Emails));
        fixture.Vm.Code = " 0384 ";
        await fixture.Vm.SubmitAsync();
        Assert.False(fixture.Vm.CodeSent);
        Assert.False(fixture.Vm.SignedOut);
        Assert.Equal("", fixture.Vm.Code);
        Assert.Equal("0384", Assert.Single(fixture.Backend.Codes));
        Assert.Empty(fixture.Backend.Requests);
    }

    [Fact]
    public async Task New_clipboard_source_resets_conversation_and_handled_mark_requires_success_or_dismissal()
    {
        using var fixture = new Fixture();
        fixture.Vm.CustomInstructions = "инструкция прошлого разговора";
        fixture.Vm.Extended = true;
        var copied = new CopiedText("входящее сообщение", 42, fixture.Clock.Now);
        await fixture.Vm.OpenAsync(new(CopiedTextStatus.Text, copied));
        Assert.True(fixture.Vm.IsReply);
        Assert.Equal(copied.Text, fixture.Vm.Source);
        Assert.Empty(fixture.Vm.CustomInstructions);
        Assert.False(fixture.Vm.Extended);
        fixture.Backend.Generate = (_, _) => throw new QuickBarException("issue.offline", retryable: true);
        await fixture.Vm.SubmitAsync();
        Assert.Empty(fixture.Clipboard.Handled);
        fixture.Backend.Generate = (_, _) => Task.FromResult("ответ");
        await fixture.Vm.RetryAsync();
        Assert.Equal(copied, Assert.Single(fixture.Clipboard.Handled));
        fixture.Vm.ResetConversation();
        var next = copied with { SequenceNumber = 43 };
        await fixture.Vm.OpenAsync(new(CopiedTextStatus.Text, next));
        fixture.Vm.DismissSource();
        Assert.Equal(next, fixture.Clipboard.Handled[1]);
        Assert.False(fixture.Vm.IsReply);
    }

    [Theory]
    [InlineData(CopiedTextStatus.Concealed, true)]
    [InlineData(CopiedTextStatus.Text, false)]
    public async Task Clipboard_concealment_and_offer_setting_prevent_incoming_text(CopiedTextStatus status, bool offer)
    {
        using var fixture = new Fixture();
        fixture.Settings.OfferCopiedText = offer;
        await fixture.Vm.OpenAsync(new(status, new("private", 9, fixture.Clock.Now)));
        Assert.Empty(fixture.Vm.Source);
        Assert.False(fixture.Vm.IsReply);
    }

    [Fact]
    public async Task Reopen_after_ten_minutes_resets_private_state_and_options()
    {
        using var fixture = new Fixture();
        fixture.Vm.Draft = "прошлый разговор";
        fixture.Vm.CustomInstructions = "не переносить";
        fixture.Vm.Extended = true;
        fixture.Vm.Close();
        fixture.Clock.Now += TimeSpan.FromMinutes(11);
        await fixture.Vm.OpenAsync(CopiedTextResult.None);
        Assert.Empty(fixture.Vm.Draft);
        Assert.Empty(fixture.Vm.CustomInstructions);
        Assert.False(fixture.Vm.Extended);
        Assert.False(fixture.Vm.CanRetry);
    }

    [Fact]
    public async Task Account_switch_during_refresh_clears_drafts_result_and_retry_snapshot()
    {
        using var fixture = new Fixture();
        fixture.Vm.Draft = "секрет первого аккаунта";
        await fixture.Vm.SubmitAsync();
        fixture.Backend.Refresh = _ =>
        { fixture.Backend.Snapshot = fixture.Backend.Snapshot with { UserId = "account-two" }; return Task.CompletedTask; };
        await fixture.Vm.OpenAsync(CopiedTextResult.None);
        Assert.Empty(fixture.Vm.Draft);
        Assert.Empty(fixture.Vm.Result);
        await fixture.Vm.RegenerateAsync();
        Assert.Single(fixture.Backend.Requests);
    }

    [Fact]
    public async Task Cancel_discards_late_generation_even_when_transport_ignores_token()
    {
        using var fixture = new Fixture();
        var pending = new TaskCompletionSource<string>(TaskCreationOptions.RunContinuationsAsynchronously);
        fixture.Backend.Generate = (_, _) => pending.Task;
        fixture.Vm.Draft = "запрос";
        var generation = fixture.Vm.SubmitAsync();
        Assert.Equal(BarPhase.Generating, fixture.Vm.Phase);
        fixture.Vm.Cancel();
        pending.SetResult("поздний результат");
        await generation;
        Assert.Equal(BarPhase.Editing, fixture.Vm.Phase);
        Assert.Empty(fixture.Vm.Result);
        Assert.Empty(fixture.Vm.Issue);
        Assert.False(fixture.Vm.IsBusy);
    }

    [Fact]
    public async Task Signout_replaces_pending_operation_and_late_result_does_not_restore_private_state()
    {
        using var fixture = new Fixture();
        var pending = new TaskCompletionSource<string>(TaskCreationOptions.RunContinuationsAsynchronously);
        fixture.Backend.Generate = (_, _) => pending.Task;
        fixture.Vm.Draft = "секрет";
        var generation = fixture.Vm.SubmitAsync();
        await fixture.Vm.SignOutAsync();
        Assert.Equal(1, fixture.Backend.Signouts);
        Assert.True(fixture.Vm.SignedOut);
        Assert.Empty(fixture.Vm.Draft);
        pending.SetResult("старый результат");
        await generation;
        Assert.Empty(fixture.Vm.Result);
        Assert.Equal(BarPhase.Editing, fixture.Vm.Phase);
    }

    [Fact]
    public async Task Copy_suppresses_concurrent_actions_and_repeat_paste_events_and_resets_ephemeral_options()
    {
        using var fixture = new Fixture();
        fixture.Vm.Draft = "запрос";
        await fixture.Vm.SubmitAsync();
        fixture.Vm.CustomInstructions = "инструкции";
        fixture.Vm.Extended = true;
        var pending = new TaskCompletionSource<bool>(TaskCreationOptions.RunContinuationsAsynchronously);
        fixture.Clipboard.Copy = _ => pending.Task;
        var events = 0;
        fixture.Vm.CopyCompleted += (_, _) => events++;
        var copy = fixture.Vm.CopyAsync();
        await fixture.Vm.CopyAsync();
        pending.SetResult(true);
        await copy;
        await fixture.Vm.CopyAsync();
        Assert.Equal(1, fixture.Clipboard.Copies);
        Assert.Equal(1, events);
        Assert.Empty(fixture.Vm.CustomInstructions);
        Assert.False(fixture.Vm.Extended);
        Assert.Equal(fixture.Vm.L["result.copied"], fixture.Vm.CopyLabel);
        fixture.Clock.Now += TimeSpan.FromSeconds(1);
        await fixture.Vm.CopyAsync();
        Assert.Equal(2, events);
    }

    [Fact]
    public async Task Failed_clipboard_copy_keeps_result_and_never_requests_paste()
    {
        using var fixture = new Fixture();
        fixture.Vm.Draft = "запрос";
        await fixture.Vm.SubmitAsync();
        fixture.Clipboard.Copy = _ => Task.FromResult(false);
        var events = 0;
        fixture.Vm.CopyCompleted += (_, _) => events++;
        await fixture.Vm.CopyAsync();
        Assert.Equal(0, events);
        Assert.Equal("готовый ответ", fixture.Vm.Result);
        Assert.Equal(fixture.Vm.L["clipboard.failed"], fixture.Vm.Issue);
    }

    [Fact]
    public async Task Voice_uses_forty_second_cap_language_priority_and_deletes_temp_file()
    {
        using var fixture = new Fixture();
        fixture.Vm.Draft = "начало";
        fixture.Settings.DictationLanguage = DictationLanguage.En;
        fixture.Vm.ReadInputLanguage = () => "kk-KZ";
        var file = fixture.Voice.MakeRecording(TimeSpan.FromSeconds(2));
        fixture.Backend.Transcription = (_, _, _, _) => Task.FromResult(" продолжение ");
        await fixture.Vm.ToggleVoiceAsync();
        Assert.Equal(TimeSpan.FromSeconds(40), fixture.Voice.Maximum);
        Assert.True(fixture.Vm.HoldsPanelOpen);
        Assert.False(fixture.Vm.CanEdit);
        await fixture.Vm.ToggleVoiceAsync();
        Assert.Equal(new[] { "en", "kk", "ru" }, fixture.Backend.Languages);
        Assert.Equal("начало продолжение", fixture.Vm.Draft);
        Assert.False(File.Exists(file));
        Assert.False(fixture.Vm.HoldsPanelOpen);
        Assert.Equal(BarPhase.Editing, fixture.Vm.Phase);
    }

    [Theory]
    [InlineData(true)]
    [InlineData(false)]
    public async Task Voice_short_and_failed_uploads_delete_audio_and_keep_draft(bool tooShort)
    {
        using var fixture = new Fixture();
        fixture.Vm.Draft = "сохранить";
        var file = fixture.Voice.MakeRecording(TimeSpan.FromMilliseconds(tooShort ? 499 : 2000));
        fixture.Backend.Transcription = (_, _, _, _) => throw new QuickBarException("voice.issue.server");
        await fixture.Vm.ToggleVoiceAsync();
        await fixture.Vm.ToggleVoiceAsync();
        Assert.False(File.Exists(file));
        Assert.Equal("сохранить", fixture.Vm.Draft);
        Assert.Equal(fixture.Vm.L[tooShort ? "voice.issue.too_short" : "voice.issue.server"], fixture.Vm.Issue);
        Assert.Equal(tooShort ? 0 : 1, fixture.Backend.Uploads);
    }

    [Fact]
    public async Task Voice_cancel_during_upload_discards_late_transcription_and_deletes_audio()
    {
        using var fixture = new Fixture();
        var file = fixture.Voice.MakeRecording(TimeSpan.FromSeconds(2));
        var started = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var pending = new TaskCompletionSource<string>(TaskCreationOptions.RunContinuationsAsynchronously);
        fixture.Backend.Transcription = (_, _, _, _) => { started.SetResult(); return pending.Task; };
        fixture.Vm.Draft = "сохранить";
        await fixture.Vm.ToggleVoiceAsync();
        var stop = fixture.Vm.ToggleVoiceAsync();
        await started.Task.WaitAsync(TestContext.Current.CancellationToken);
        Assert.Equal(BarPhase.Transcribing, fixture.Vm.Phase);
        fixture.Vm.Cancel();
        pending.SetResult("не добавлять");
        await stop;
        Assert.Equal("сохранить", fixture.Vm.Draft);
        Assert.Empty(fixture.Vm.Issue);
        Assert.False(File.Exists(file));
    }

    [Theory]
    [InlineData(true)]
    [InlineData(false)]
    public async Task Voice_permission_request_holds_panel_and_close_or_lock_cancels_late_start(bool locked)
    {
        using var fixture = new Fixture();
        var pending = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        fixture.Voice.Start = _ => pending.Task;
        var start = fixture.Vm.ToggleVoiceAsync();
        Assert.True(fixture.Vm.HoldsPanelOpen);
        if (locked) fixture.Vm.OnSessionLocked();
        else fixture.Vm.Close();
        pending.SetResult();
        await start;
        Assert.False(fixture.Vm.IsRecording);
        Assert.False(fixture.Vm.HoldsPanelOpen);
        Assert.False(fixture.Voice.IsRecording);
        Assert.Empty(fixture.Vm.Issue);
    }

    [Fact]
    public async Task Session_lock_preserves_generation_but_cancels_recording()
    {
        using var fixture = new Fixture();
        await fixture.Vm.ToggleVoiceAsync();
        fixture.Vm.OnSessionLocked();
        Assert.False(fixture.Vm.IsRecording);
        var pending = new TaskCompletionSource<string>(TaskCreationOptions.RunContinuationsAsynchronously);
        fixture.Backend.Generate = (_, _) => pending.Task;
        fixture.Vm.Draft = "запрос";
        var generation = fixture.Vm.SubmitAsync();
        fixture.Vm.OnSessionLocked();
        Assert.Equal(BarPhase.Generating, fixture.Vm.Phase);
        pending.SetResult("готово");
        await generation;
        Assert.Equal("готово", fixture.Vm.Result);
    }

    [Fact]
    public async Task Voice_device_failure_keeps_draft_and_shows_specific_issue()
    {
        using var fixture = new Fixture();
        fixture.Vm.Draft = "сохранить";
        await fixture.Vm.ToggleVoiceAsync();
        fixture.Voice.Fail(new(VoiceCaptureError.NoInputDevice));
        Assert.Equal(BarPhase.Editing, fixture.Vm.Phase);
        Assert.Equal("сохранить", fixture.Vm.Draft);
        Assert.Equal(fixture.Vm.L["voice.issue.no_device"], fixture.Vm.Issue);
    }

    [Fact]
    public void Background_balance_push_notifies_on_dispatcher_and_preserves_the_draft()
    {
        var backend = new FakeBackend(); var voice = new FakeVoice(); var clipboard = new FakeClipboard();
        var dispatcher = new QueuedDispatcher();
        using var model = new QuickBarViewModel(backend, clipboard, voice, new AppSettings(), new(UiLanguage.En), dispatcher);
        model.Draft = "keep this conversation";
        var changes = 0;
        model.AccountChanged += (_, _) => changes++;
        backend.Snapshot = backend.Snapshot with { Balance = "500 replies remaining" };
        backend.RaiseChanged();
        Assert.Equal(0, changes);
        dispatcher.Drain();
        Assert.Equal(1, changes);
        Assert.Equal("500 replies remaining", model.Account.Balance);
        Assert.Equal("keep this conversation", model.Draft);
    }

    [Fact]
    public async Task Same_user_new_session_push_clears_previous_private_result()
    {
        using var fixture = new Fixture();
        fixture.Vm.Draft = "private";
        await fixture.Vm.SubmitAsync();
        Assert.NotEmpty(fixture.Vm.Result);
        fixture.Backend.Snapshot = fixture.Backend.Snapshot with { SessionGeneration = 2 };
        fixture.Backend.RaiseChanged();
        Assert.Empty(fixture.Vm.Result);
        Assert.Empty(fixture.Vm.Draft);
        Assert.False(fixture.Vm.RegenerateCommand.CanExecute(null));
    }

    [Fact]
    public async Task Authentication_push_does_not_cancel_its_own_logout_request()
    {
        using var fixture = new Fixture();
        fixture.Backend.RaiseAuthChanges = true;
        await fixture.Vm.SignOutAsync();
        Assert.False(fixture.Backend.AuthCancellationObserved);
        Assert.True(fixture.Vm.SignedOut);
    }

    [Fact]
    public async Task Authentication_push_does_not_cancel_its_own_code_verification()
    {
        using var fixture = new Fixture(signedIn: false);
        fixture.Backend.RaiseAuthChanges = true;
        fixture.Vm.Email = "person@example.com";
        await fixture.Vm.SubmitAsync();
        fixture.Vm.Code = "1234";
        await fixture.Vm.SubmitAsync();
        Assert.False(fixture.Backend.AuthCancellationObserved);
        Assert.True(fixture.Vm.Account.SignedIn);
    }

    [Fact]
    public void Queued_backend_push_is_ignored_after_disposal()
    {
        var backend = new FakeBackend(); var dispatcher = new QueuedDispatcher();
        var model = new QuickBarViewModel(backend, new FakeClipboard(), new FakeVoice(), new AppSettings(), new(UiLanguage.En), dispatcher);
        var changes = 0; model.AccountChanged += (_, _) => changes++;
        backend.RaiseChanged();
        model.Dispose();
        dispatcher.Drain();
        backend.RaiseChanged(); dispatcher.Drain();
        Assert.Equal(0, changes);
    }

    private sealed class QueuedDispatcher : IUiDispatcher
    {
        private readonly Queue<Action> pending = [];
        public bool CheckAccess() => true;
        public void Post(Action action) => pending.Enqueue(action);
        public Task InvokeAsync(Func<Task> action) => action();
        public void Drain() { while (pending.TryDequeue(out var action)) action(); }
    }

    private sealed class Fixture : IDisposable
    {
        public FakeBackend Backend { get; } = new();
        public FakeClipboard Clipboard { get; } = new();
        public FakeVoice Voice { get; } = new();
        public FakeClock Clock { get; } = new();
        public AppSettings Settings { get; } = new();
        public QuickBarViewModel Vm { get; }
        public Fixture(bool signedIn = true)
        {
            if (!signedIn) Backend.Snapshot = new();
            Localizer.Include(typeof(QuickBarViewModel).Assembly, "AirReply.Presentation.Localization.strings.json");
            Vm = new(Backend, Clipboard, Voice, Settings, new(UiLanguage.Ru), new ImmediateDispatcher(), Clock);
        }
        public void Dispose() { Vm.Dispose(); Voice.Cancel(); Clipboard.Dispose(); }
    }

    private sealed class FakeClock : IClock
    {
        public DateTimeOffset Now { get; set; } = new(2026, 11, 14, 0, 0, 0, TimeSpan.Zero);
    }
    private sealed class ImmediateDispatcher : IUiDispatcher
    {
        public bool CheckAccess() => true;
        public void Post(Action action) => action();
        public Task InvokeAsync(Func<Task> action) => action();
    }
    private sealed class FakeClipboard : IClipboardService
    {
        public List<CopiedText> Handled { get; } = [];
        public int Copies { get; private set; }
        public Func<string, Task<bool>> Copy { get; set; } = _ => Task.FromResult(true);
        public CopiedTextResult TakeFreshCopiedText(TimeSpan maxAge) => CopiedTextResult.None;
        public void MarkHandled(CopiedText copied) => Handled.Add(copied);
        public Task<bool> CopyAsync(string text) { Copies++; return Copy(text); }
        public void Dispose() { }
    }
    private sealed class FakeVoice : IVoiceRecorder
    {
        private RecordedAudio? recording;
        public event EventHandler<float>? Level { add { } remove { } }
        public event EventHandler<VoiceCaptureException?>? Stopped;
        public bool IsRecording { get; private set; }
        public int Starts { get; private set; }
        public TimeSpan Maximum { get; private set; }
        public Func<CancellationToken, Task> Start { get; set; } = _ => Task.CompletedTask;
        public string MakeRecording(TimeSpan duration)
        {
            var path = Path.GetTempFileName();
            File.WriteAllBytes(path, "RIFF-WAVE"u8.ToArray());
            recording = new(path, duration, "audio/wav", "voice.wav");
            return path;
        }
        public async Task StartAsync(TimeSpan maxDuration, CancellationToken cancellationToken = default)
        { Starts++; Maximum = maxDuration; await Start(cancellationToken); IsRecording = true; }
        public Task<RecordedAudio?> StopAsync()
        { IsRecording = false; var result = recording; recording = null; return Task.FromResult(result); }
        public void Fail(VoiceCaptureException error) => Stopped?.Invoke(this, error);
        public void Cancel()
        {
            IsRecording = false;
            if (recording is not null) { File.Delete(recording.FilePath); recording = null; }
        }
        public ValueTask DisposeAsync() { Cancel(); return ValueTask.CompletedTask; }
    }
    private sealed class FakeBackend : IQuickBarBackend
    {
        public event EventHandler? Changed;
        public void RaiseChanged() => Changed?.Invoke(this, EventArgs.Empty);
        public AccountSnapshot Snapshot { get; set; } = new(true, true, true, "account-one", "user@example.com",
            VoiceAvailable: true, ContextAvailable: true, SenderProfile: true);
        public Func<GenerationRequest, CancellationToken, Task<string>> Generate { get; set; } = (_, _) => Task.FromResult("готовый ответ");
        public Func<CancellationToken, Task> Refresh { get; set; } = _ => Task.CompletedTask;
        public Func<byte[], TimeSpan, IReadOnlyList<string>, CancellationToken, Task<string>> Transcription { get; set; } = (_, _, _, _) => Task.FromResult("текст");
        public List<GenerationRequest> Requests { get; } = [];
        public List<string> Emails { get; } = [];
        public List<string> Codes { get; } = [];
        public IReadOnlyList<string> Languages { get; private set; } = [];
        public int Signouts { get; private set; }
        public int Uploads { get; private set; }
        public bool RaiseAuthChanges { get; set; }
        public bool AuthCancellationObserved { get; private set; }
        public Task InitializeAsync(CancellationToken cancellationToken = default) => Task.CompletedTask;
        public Task RefreshAsync(CancellationToken cancellationToken = default) => Refresh(cancellationToken);
        public Task RequestCodeAsync(string email, CancellationToken cancellationToken = default) { Emails.Add(email); return Task.CompletedTask; }
        public Task VerifyAsync(string code, CancellationToken cancellationToken = default)
        {
            Codes.Add(code); Snapshot = new(true, true, false, "account-one", "user@example.com", SessionGeneration: 1);
            if (RaiseAuthChanges) RaiseChanged();
            AuthCancellationObserved = cancellationToken.IsCancellationRequested;
            return Task.CompletedTask;
        }
        public void UseAnotherEmail() { }
        public Task ResendAsync(CancellationToken cancellationToken = default) => Task.CompletedTask;
        public Task SignOutAsync(CancellationToken cancellationToken = default)
        {
            Signouts++; Snapshot = new(SessionGeneration: 2);
            if (RaiseAuthChanges) RaiseChanged();
            AuthCancellationObserved = cancellationToken.IsCancellationRequested;
            return Task.CompletedTask;
        }
        public Task AcceptConsentAsync(CancellationToken cancellationToken = default) { Snapshot = Snapshot with { HasConsent = true }; return Task.CompletedTask; }
        public Task<string> GenerateAsync(GenerationRequest request, CancellationToken cancellationToken = default)
        { Requests.Add(request); return Generate(request, cancellationToken); }
        public Task<string> TranscribeAsync(byte[] wav, TimeSpan duration, IReadOnlyList<string> languages, CancellationToken cancellationToken = default)
        { Uploads++; Languages = languages; return Transcription(wav, duration, languages, cancellationToken); }
        public Task<Uri> PlansUriAsync(CancellationToken cancellationToken = default) => Task.FromResult(new Uri("https://ai-reply.kz/payment"));
    }
}
