using AirReply.App.Views;
using AirReply.Context;
using AirReply.Core.Infrastructure;
using AirReply.Core.Accounts;
using AirReply.Core.Installations;
using AirReply.Core.Networking;
using AirReply.Core.Localization;
using AirReply.Core.Settings;
using AirReply.Presentation.Models;
using AirReply.Presentation.Platform;
using Avalonia;
using Avalonia.Controls;
using Avalonia.Headless;
using Avalonia.Headless.XUnit;
using Avalonia.Styling;
using Avalonia.Threading;
using Avalonia.VisualTree;
using Xunit;

[assembly: AvaloniaTestApplication(typeof(AirReply.App.Tests.TestAppBuilder))]

namespace AirReply.App.Tests;

public static class TestAppBuilder
{
    public static AppBuilder BuildAvaloniaApp() => AppBuilder.Configure<App>().UseSkia()
        .UseHeadless(new AvaloniaHeadlessPlatformOptions { UseHeadlessDrawing = false }).WithInterFont();
}

public sealed class UiTests
{
    [AvaloniaTheory]
    [InlineData(UiLanguage.Ru, false)]
    [InlineData(UiLanguage.Ru, true)]
    [InlineData(UiLanguage.Kk, false)]
    [InlineData(UiLanguage.Kk, true)]
    [InlineData(UiLanguage.En, false)]
    [InlineData(UiLanguage.En, true)]
    public async Task RenderEveryWorkflowWithRealTranslations(UiLanguage language, bool dark)
    {
        ContextStrings.Include();
        Localizer.Include(typeof(QuickBarViewModel).Assembly, "AirReply.Presentation.Localization.strings.json");
        foreach (var state in new[] { "email", "code", "consent", "compose", "reply", "context", "generating", "result", "issue", "recording", "transcribing" })
        {
            var backend = new TestBackend { Snapshot = state is "email" or "code" ? new() : SignedIn with { HasConsent = state != "consent" } };
            var services = new TestServices();
            using var model = new QuickBarViewModel(backend, services, services, new AppSettings(), new Localizer(language), new UiDispatcher());
            model.Draft = language == UiLanguage.Kk ? "Клиентке құжаттарды алғанымызды сыпайы түрде хабарла" : "Напиши клиенту, что документы получили";
            model.Email = "person@example.kz";
            Task? pending = null;
            if (state == "code") await model.SubmitAsync();
            if (state == "reply") { model.IsReply = true; model.Source = "Здравствуйте! Когда будут готовы документы?"; }
            if (state == "context") { model.ContextEnabled = true; model.CustomInstructions = "Вежливо, кратко, с благодарностью"; model.Extended = true; }
            if (state == "issue") { backend.Failure = new QuickBarException("issue.pro_device_limit"); await model.SubmitAsync(); }
            if (state == "result") await model.SubmitAsync();
            if (state == "generating") { backend.Pending = new(TaskCreationOptions.RunContinuationsAsynchronously); pending = model.SubmitAsync(); }
            if (state is "recording" or "transcribing") await model.ToggleVoiceAsync();
            if (state == "transcribing") { backend.Pending = new(TaskCreationOptions.RunContinuationsAsynchronously); pending = model.ToggleVoiceAsync(); }
            var view = new QuickBarView { DataContext = model };
            if (state == "context") view.GetVisualDescendants().OfType<Expander>().FirstOrDefault()?.SetValue(Expander.IsExpandedProperty, true);
            var window = new Window { Width = 680, SizeToContent = SizeToContent.Height, RequestedThemeVariant = dark ? ThemeVariant.Dark : ThemeVariant.Light, Content = view };
            window.Show(); Dispatcher.UIThread.RunJobs();
            if (state == "context") { view.GetVisualDescendants().OfType<Expander>().Single().IsExpanded = true; Dispatcher.UIThread.RunJobs(); }
            Assert.True(view.Bounds.Width > 600);
            Assert.All(view.GetVisualDescendants().OfType<TextBlock>().Where(t => t.IsEffectivelyVisible && !string.IsNullOrEmpty(t.Text)), t =>
            {
                Assert.False(t.Text!.StartsWith("issue.", StringComparison.Ordinal));
                Assert.False(t.Text.StartsWith("signin.", StringComparison.Ordinal));
                Assert.False(t.Text.Contains('⌘', StringComparison.Ordinal));
                Assert.True(t.Bounds.Width <= 648, $"Overflow: {t.Text}");
            });
            var frame = window.CaptureRenderedFrame(); Assert.NotNull(frame);
            var output = Path.Combine(AppContext.BaseDirectory, "renders"); Directory.CreateDirectory(output);
            frame.Save(Path.Combine(output, $"{state}-{Localizer.Code(language)}-{(dark ? "dark" : "light")}.png"), Avalonia.Media.Imaging.PngBitmapEncoderOptions.Default);
            if (state == "result") Assert.Equal(backend.Reply, view.FindControl<SelectableTextBlock>("ResultText")!.Text);
            window.Close();
            if (pending is not null) { backend.Pending!.TrySetResult(backend.Reply); await pending; }
        }
    }

    [AvaloniaFact]
    public async Task ViewBindsUserTextAndEnterGeneratesThenCopyUsesResult()
    {
        var backend = new TestBackend(); var services = new TestServices();
        using var model = new QuickBarViewModel(backend, services, services, new AppSettings(), new Localizer(UiLanguage.En), new UiDispatcher());
        var view = new QuickBarView { DataContext = model }; var window = new Window { Width = 680, Content = view };
        window.Show(); Dispatcher.UIThread.RunJobs();
        var draft = view.FindControl<TextBox>("DraftField")!; draft.Text = "Tell them we received the documents";
        Dispatcher.UIThread.RunJobs(); Assert.Equal(draft.Text, model.Draft);
        draft.Focus(); window.KeyPress(Avalonia.Input.Key.Enter, Avalonia.Input.RawInputModifiers.None, Avalonia.Input.PhysicalKey.Enter, null); Dispatcher.UIThread.RunJobs();
        Assert.Equal(BarPhase.Result, model.Phase); Assert.Single(backend.Requests);
        await model.CopyAsync(); Assert.Equal(backend.Reply, services.Clipboard);
        window.Close();
    }

    [AvaloniaFact]
    public async Task SettingsOpensFiveTabsAndAccountSwitchClearsEditor()
    {
        ContextStrings.Include();
        Localizer.Include(typeof(QuickBarViewModel).Assembly, "AirReply.Presentation.Localization.strings.json");
        var backend = new TestBackend(); var services = new TestServices(); var path = Path.Combine(Path.GetTempPath(), Guid.NewGuid().ToString("N"));
        using var model = new QuickBarViewModel(backend, services, services, new AppSettings(), new Localizer(UiLanguage.Kk), new UiDispatcher());
        using var workspace = new ContextWorkspace(path, _ => new EphemeralDataProtector());
        await workspace.SwitchAccountAsync("a"); await workspace.SaveAsync(new ContextProfile { Name = "Secret A" });
        var window = new SettingsWindow(model.Settings, model, workspace, services, null, () => { }, () => { }, _ => { });
        window.Show(); Dispatcher.UIThread.RunJobs();
        Assert.Equal(5, window.GetVisualDescendants().OfType<TabControl>().Single().ItemCount);
        await workspace.SwitchAccountAsync("b"); Assert.Empty(workspace.Profiles); Assert.Null(workspace.SelectedProfileId);
        window.Close(); if (Directory.Exists(path)) Directory.Delete(path, true);
    }

    private static readonly AccountSnapshot SignedIn = new(true, true, true, "user-a", "person@example.kz", "Осталось ответов: 27", true, true, true);

    [AvaloniaFact]
    public async Task Confirmed_website_balance_reaches_visible_bar_without_manual_refresh()
    {
        Localizer.Include(typeof(QuickBarViewModel).Assembly, "AirReply.Presentation.Localization.strings.json");
        var handler = new PaymentHandler();
        using var http = new HttpClient(handler);
        using var api = new ApiClient(http, AppInfo.Current(), new MemorySecrets());
        var release = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        using var account = new AccountStore(api, paymentDelay: (_, ct) => release.Task.WaitAsync(ct));
        using var registrar = new InstallationRegistrar(api, account);
        api.Install(new SessionPayload
        {
            AccessToken = "test-access", RefreshToken = "test-refresh", DeviceId = "test-device",
            User = new UserDto { Id = "payment-user", Email = "test@example.com" },
            Usage = new UsageDto { Credits = 1, RemainingTotal = 1 }, LegalConsent = new LegalConsentDto(),
        });
        using var backend = new BackendAdapter(api, account, registrar, () => new Localizer(UiLanguage.En));
        var services = new TestServices();
        using var model = new QuickBarViewModel(backend, services, services, new AppSettings(), new(UiLanguage.En), new UiDispatcher());
        model.Draft = "Keep this draft while a payment is confirmed";
        var view = new QuickBarView { DataContext = model };
        var window = new Window { Width = 680, Content = view };
        window.Show(); Dispatcher.UIThread.RunJobs();
        var payment = await backend.PlansUriAsync(TestContext.Current.CancellationToken);
        Assert.Equal("ai-reply.kz", payment.Host);
        await model.RefreshAccountAsync();
        var catchup = account.PaymentRefreshTask;
        Assert.NotNull(catchup);
        handler.Credits = 501;
        release.TrySetResult();
        await catchup.WaitAsync(TimeSpan.FromSeconds(10), TestContext.Current.CancellationToken);
        Dispatcher.UIThread.RunJobs();
        var balance = model.L.Format("balance.remaining", 501);
        Assert.Equal(balance, model.Account.Balance);
        Assert.Contains(view.GetVisualDescendants().OfType<TextBlock>(), text => text.Text == balance);
        Assert.Equal("Keep this draft while a payment is confirmed", model.Draft);
        Assert.False(account.IsWebsitePaymentPending);
        window.Close();
    }

    [AvaloniaFact]
    public async Task Invalid_signed_in_payment_handoff_becomes_a_localized_issue()
    {
        var handler = new PaymentHandler { InvalidHandoff = true };
        using var http = new HttpClient(handler);
        using var api = new ApiClient(http, AppInfo.Current(), new MemorySecrets());
        using var account = new AccountStore(api);
        using var registrar = new InstallationRegistrar(api, account);
        api.Install(new SessionPayload { AccessToken = "test-access", RefreshToken = "test-refresh", DeviceId = "test-device", User = new UserDto { Id = "payment-user" } });
        using var backend = new BackendAdapter(api, account, registrar, () => new Localizer(UiLanguage.En));
        var error = await Assert.ThrowsAsync<QuickBarException>(() => backend.PlansUriAsync(TestContext.Current.CancellationToken));
        Assert.Equal("issue.payment_unavailable", error.Key);
        Assert.NotNull(api.Session);
        Assert.False(account.IsWebsitePaymentPending);
    }

    private sealed class PaymentHandler : HttpMessageHandler
    {
        public int Credits { get; set; } = 1;
        public bool InvalidHandoff { get; init; }
        protected override Task<HttpResponseMessage> SendAsync(HttpRequestMessage request, CancellationToken cancellationToken)
        {
            var body = request.RequestUri!.AbsolutePath switch
            {
                "/api/v1/config" => "{}",
                "/api/v1/web/payment-handoff" => System.Text.Json.JsonSerializer.Serialize(new { url = InvalidHandoff ? "https://other.example/payment#handoff=" + new string('A', 43) : "https://ai-reply.kz/payment#handoff=" + new string('A', 43) }),
                "/api/v1/me" => System.Text.Json.JsonSerializer.Serialize(new { user = new { id = "payment-user", email = "test@example.com" }, usage = new { credits = Credits, remaining_total = Credits }, legal_consent = new { terms_version = "1", privacy_version = "1" } }),
                _ => throw new InvalidOperationException("Unexpected test request."),
            };
            return Task.FromResult(new HttpResponseMessage(System.Net.HttpStatusCode.OK) { Content = new StringContent(body, System.Text.Encoding.UTF8, "application/json") });
        }
    }

    private sealed class MemorySecrets : ISecretStore
    {
        private readonly Dictionary<string, string> values = new(StringComparer.Ordinal);
        public string? Read(string key) => values.GetValueOrDefault(key);
        public void Write(string key, string value) => values[key] = value;
        public void Delete(string key) => values.Remove(key);
    }

    private sealed class TestBackend : IQuickBarBackend
    {
        public event EventHandler? Changed;
        public void RaiseChanged() => Changed?.Invoke(this, EventArgs.Empty);
        public AccountSnapshot Snapshot { get; set; } = SignedIn;
        public string Reply => "Спасибо! Мы получили документы и скоро сообщим о следующих шагах.";
        public QuickBarException? Failure { get; set; }
        public TaskCompletionSource<string>? Pending { get; set; }
        public List<GenerationRequest> Requests { get; } = [];
        public Task InitializeAsync(CancellationToken cancellationToken = default) => Task.CompletedTask;
        public Task RefreshAsync(CancellationToken cancellationToken = default) => Task.CompletedTask;
        public Task RequestCodeAsync(string email, CancellationToken cancellationToken = default) => Task.CompletedTask;
        public Task VerifyAsync(string code, CancellationToken cancellationToken = default) { Snapshot = SignedIn; return Task.CompletedTask; }
        public void UseAnotherEmail() { }
        public Task ResendAsync(CancellationToken cancellationToken = default) => Task.CompletedTask;
        public Task SignOutAsync(CancellationToken cancellationToken = default) { Snapshot = new(); return Task.CompletedTask; }
        public Task AcceptConsentAsync(CancellationToken cancellationToken = default) { Snapshot = Snapshot with { HasConsent = true }; return Task.CompletedTask; }
        public Task<string> GenerateAsync(GenerationRequest request, CancellationToken cancellationToken = default)
        { Requests.Add(request); return Failure is not null ? Task.FromException<string>(Failure) : Pending?.Task ?? Task.FromResult(Reply); }
        public Task<string> TranscribeAsync(byte[] wav, TimeSpan duration, IReadOnlyList<string> languages, CancellationToken cancellationToken = default) => Pending?.Task ?? Task.FromResult("Получили документы");
        public Task<Uri> PlansUriAsync(CancellationToken cancellationToken = default) => Task.FromResult(new Uri("https://ai-reply.kz/payment"));
    }

    private sealed class TestServices : IClipboardService, IVoiceRecorder, IStartupService
    {
        public string Clipboard { get; private set; } = "";
        public event EventHandler<float>? Level { add { } remove { } }
        public event EventHandler<VoiceCaptureException?>? Stopped { add { } remove { } }
        public bool IsRecording { get; private set; }
        public bool IsEnabled { get; private set; }
        public Task<bool> CopyAsync(string text) { Clipboard = text; return Task.FromResult(true); }
        public CopiedTextResult TakeFreshCopiedText(TimeSpan maxAge) => CopiedTextResult.None;
        public void MarkHandled(CopiedText copied) { }
        public void Dispose() { }
        public ValueTask DisposeAsync() => ValueTask.CompletedTask;
        public Task StartAsync(TimeSpan maxDuration, CancellationToken cancellationToken = default) { IsRecording = true; return Task.CompletedTask; }
        public Task<RecordedAudio?> StopAsync()
        {
            IsRecording = false; var path = Path.GetTempFileName(); File.WriteAllBytes(path, new byte[64]);
            return Task.FromResult<RecordedAudio?>(new(path, TimeSpan.FromSeconds(2), "audio/wav", "voice.wav"));
        }
        public void Cancel() => IsRecording = false;
        public void SetEnabled(bool enabled) => IsEnabled = enabled;
    }
}
