using System.Globalization;
using System.Runtime.Versioning;
using AirReply.App.Views;
using AirReply.Context;
using AirReply.Core.Accounts;
using AirReply.Core.Infrastructure;
using AirReply.Core.Installations;
using AirReply.Core.Localization;
using AirReply.Core.Networking;
using AirReply.Core.Settings;
using AirReply.Platform.Windows;
using AirReply.Presentation.Models;
using AirReply.Presentation.Layout;
using AirReply.Presentation.Platform;
using Avalonia;
using Avalonia.Controls;
using Avalonia.Controls.ApplicationLifetimes;
using Avalonia.Input;
using Avalonia.Platform;
using Avalonia.Styling;
using Avalonia.Threading;

namespace AirReply.App;

internal sealed class DesktopHost : IDisposable
{
    private readonly IClassicDesktopStyleApplicationLifetime desktop;
    private readonly JsonSettingsStore settingsStore = new(JsonSettingsStore.DefaultPath());
    private readonly AppSettings settings;
    private readonly QuickBarView view = new();
    private readonly Window bar;
    private readonly List<IDisposable> owned = [];
    private readonly UiDispatcher dispatcher = new UiDispatcher();
    private IClipboardService clipboard = null!;
    private IVoiceRecorder voice = null!;
    private IFocusTracker focus = null!;
    private IPasteService paste = null!;
    private IKeyboardLanguage keyboard = null!;
    private IStartupService startup = null!;
    private IShell shell = null!;
    private IHotKeyService? hotkey;
    private IQuickBarBackend backend = null!;
    private QuickBarViewModel model = null!;
    private Localizer localizer;
    private AccountStore? account;
    private ApiClient? api;
    private CrashReporter? crash;
    private ContextWorkspace? context;
    private SettingsWindow? settingsWindow;
    private TrayIcon? tray;
    private bool showing;
    private bool disposing;
    private bool hiding;
    private bool dragging;
    private BarAnchor? anchor;

    public DesktopHost(IClassicDesktopStyleApplicationLifetime desktop)
    {
        this.desktop = desktop;
        settings = settingsStore.Load();
        ContextStrings.Include();
        Localizer.Include(typeof(QuickBarViewModel).Assembly, "AirReply.Presentation.Localization.strings.json");
        localizer = MakeLocalizer();
        bar = new Window
        {
            Title = "AI Reply", Width = 680, SizeToContent = SizeToContent.Height, CanResize = false,
            WindowDecorations = WindowDecorations.None, ShowInTaskbar = false, Topmost = true,
            Background = Avalonia.Media.Brushes.Transparent,
            TransparencyLevelHint = [WindowTransparencyLevel.AcrylicBlur, WindowTransparencyLevel.Transparent],
            Content = new ScrollViewer { Content = view, HorizontalScrollBarVisibility = Avalonia.Controls.Primitives.ScrollBarVisibility.Disabled },
            WindowStartupLocation = WindowStartupLocation.Manual,
        };
        bar.Closing += (_, e) => { if (!disposing) { e.Cancel = true; Hide(true); } };
        bar.Deactivated += (_, _) => { if (!model.HoldsPanelOpen) Hide(false); };
        bar.SizeChanged += (_, _) => { if (bar.IsVisible && !showing && !dragging) Place(); };
        if (OperatingSystem.IsWindows()) Win32Properties.AddWndProcHookCallback(bar, WindowMessage);
        view.FindControl<Grid>("DragHandle")!.PointerPressed += (_, e) =>
        {
            if (e.Source is TextBlock or Grid && e.GetCurrentPoint(bar).Properties.IsLeftButtonPressed)
            { bar.BeginMoveDrag(e); if (!OperatingSystem.IsWindows()) RememberPosition(); }
        };

    }

    public void Start(string[] args)
    {
        if (OperatingSystem.IsWindows() && !args.Contains("--preview", StringComparer.Ordinal)) CreateWindowsServices();
        else
        {
            var preview = new PreviewServices();
            clipboard = preview; voice = preview; focus = preview; paste = preview;
            keyboard = preview; startup = preview; shell = preview; backend = preview;
        }
        model = new QuickBarViewModel(backend, clipboard, voice, settings, localizer, dispatcher);
        bar.Activated += (_, _) => { if (!showing && !disposing) _ = model.RefreshAccountAsync(); };
        context = new ContextWorkspace(Path.Combine(Path.GetDirectoryName(JsonSettingsStore.DefaultPath())!, "context"),
            user => OperatingSystem.IsWindows() ? new WindowsDataProtector(ContextProfileStore.Entropy(user)) : new EphemeralDataProtector());
        model.ReadInputLanguage = () => keyboard.CurrentInputLanguage(0);
        model.BuildContext = () => context.Build(model.Source, model.Draft, model.CustomInstructions, model.IsReply);
        model.AccountChanged += async (_, _) =>
        {
            await context.SwitchAccountAsync(model.Account.UserId);
            UpdateTray();
            if (settingsWindow is not null) settingsWindow.RefreshAccount();
        };
        context.Changed += (_, _) => UpdateContextPicker();
        model.CopyCompleted += async (_, _) =>
        {
            var target = focus.PreviousWindow;
            var shouldHide = settings.CloseAfterCopy || settings.PasteAfterCopy;
            var shouldPaste = settings.PasteAfterCopy;
            await Task.Delay(700);
            if (disposing || !bar.IsActive) return;
            if (shouldHide) Hide(true);
            if (shouldPaste) await paste.PasteAsync(target);
        };
        model.HideRequested += (_, _) => Hide(true);
        model.SourceFocusRequested += (_, _) => view.FindControl<TextBox>("SourceField")?.Focus();
        model.PropertyChanged += (_, e) =>
        {
            if (e.PropertyName == nameof(QuickBarViewModel.CodeSent)) Dispatcher.UIThread.Post(view.FocusEditor);
            if (e.PropertyName == nameof(QuickBarViewModel.Recipient)) SaveSettings();
        };
        voice.Level += (_, level) => dispatcher.Post(() => view.FindControl<ProgressBar>("VoiceLevel")!.Value = level);
        view.DataContext = model;
        view.ActionRequested += async (_, action) => await HandleActionAsync(action);
        ApplyAppearance();
        UpdateContextPicker();
        UpdateTray();
        _ = InitializeAsync(!args.Contains("--background", StringComparer.Ordinal));
    }

    [SupportedOSPlatform("windows")]
    private void CreateWindowsServices()
    {
        var messages = new WindowsMessageWindow(); owned.Add(messages);
        var keys = new WindowsHotKeyService(messages); owned.Add(keys); hotkey = keys;
        keys.RegisterSaved(settings.Shortcut);
        keys.Pressed += (_, _) => { if (bar.IsVisible) Hide(true); else _ = ShowAsync(); };
        messages.ShowRequested += (_, _) => _ = ShowAsync();
        messages.SuspendOrLock += (_, _) => model?.OnSessionLocked();
        messages.Resumed += (_, _) => { if (model is not null) _ = model.OpenAsync(CopiedTextResult.None); };
        var clip = new WindowsClipboardService(messages); clipboard = clip; owned.Add(clip);
        var tracker = new WindowsFocusTracker(); focus = tracker; owned.Add(tracker);
        paste = new WindowsPasteService(focus); keyboard = new WindowsKeyboardLanguage();
        var startupService = new WindowsStartupService();
        try { startupService.RepairCommandLine(); }
        catch (Exception ex) when (ex is IOException or UnauthorizedAccessException or System.Security.SecurityException)
        { System.Diagnostics.Trace.TraceError("Startup repair: {0}", ex.GetType().Name); }
        startup = startupService;
        shell = new WindowsShell(); voice = new WindowsVoiceRecorder();
        var http = ApiClient.CreateHttpClient(); owned.Add(http);
        api = new ApiClient(http, AppInfo.Current(), new WindowsSecretStore(), () => localizer.LanguageCode); owned.Add(api);
        account = new AccountStore(api); owned.Add(account);
        var registrar = new InstallationRegistrar(api, account); owned.Add(registrar);
        var adapter = new BackendAdapter(api, account, registrar, () => localizer);
        backend = adapter; owned.Add(adapter);
        crash = new CrashReporter(api, Path.Combine(Path.GetDirectoryName(JsonSettingsStore.DefaultPath())!, "pending-crashes.json"));
        AppDomain.CurrentDomain.UnhandledException += OnFatal;
    }

    private async Task InitializeAsync(bool show)
    {
        if (show) await ShowAsync();
        await model.InitializeAsync();
        if (show) view.FocusEditor();
        if (crash is not null) await crash.SendPendingAsync();
    }

    private void OnFatal(object sender, UnhandledExceptionEventArgs e)
    { if (e.ExceptionObject is Exception ex) crash?.RecordFatal(ex); }

    private async Task ShowAsync()
    {
        if (bar.IsVisible) { bar.Activate(); return; }
        focus.RememberForegroundWindow();
        if (OperatingSystem.IsWindows())
        {
            var point = WindowsDesktop.CursorPosition();
            bar.Position = new PixelPoint(point.X, point.Y);
        }
        model.InputLanguage = keyboard.CurrentInputLanguage(focus.PreviousWindow);
        var fresh = settings.OfferCopiedText ? clipboard.TakeFreshCopiedText(TimeSpan.FromMinutes(10)) : CopiedTextResult.None;
        anchor = null;
        showing = true;
        bar.Show(); bar.Activate(); Place(); showing = false;
        view.FocusEditor();
        await model.OpenAsync(fresh);
    }

    private void Hide(bool restore)
    {
        if (!bar.IsVisible || hiding) return;
        hiding = true;
        try
        {
            if (restore && bar.IsActive) focus.RestorePreviousWindow();
            bar.Hide();
            model.Close();
        }
        finally { hiding = false; }
    }

    private void Place()
    {
        var screen = bar.Screens.ScreenFromPoint(bar.Position) ?? bar.Screens.Primary;
        if (screen is null) return;
        var a = screen.WorkingArea;
        var area = new WorkAreaPixels(a.X, a.Y, a.Width, a.Height);
        anchor ??= WindowPlacement.ForShow(area, screen.Scaling, settings.Placement, settings.RememberedX, settings.RememberedY);
        var layout = WindowPlacement.Calculate(area, screen.Scaling, 680, Math.Max(160, bar.Bounds.Height), anchor.Value, shadowInsetDip: 16);
        anchor = layout.Anchor;
        bar.Width = layout.Width / screen.Scaling;
        bar.MaxHeight = layout.MaxHeightDip;
        bar.Position = new PixelPoint((int)Math.Round(layout.X), (int)Math.Round(layout.Y));
    }

    private nint WindowMessage(nint window, uint message, nint wParam, nint lParam, ref bool handled)
    {
        if (message == 0x0231) dragging = true; // WM_ENTERSIZEMOVE
        if (message == 0x0232) // WM_EXITSIZEMOVE: native drag release can occur outside the card.
            Dispatcher.UIThread.Post(() => { dragging = false; RememberPosition(); Place(); });
        if (message is 0x007E or 0x02E0 && !dragging)
            Dispatcher.UIThread.Post(() => { if (bar.IsVisible) { anchor = null; Place(); } });
        return 0;
    }

    private void RememberPosition()
    {
        var screen = bar.Screens.ScreenFromWindow(bar); if (screen is null) return;
        var a = screen.WorkingArea;
        var remembered = WindowPlacement.CaptureDrag(new WorkAreaPixels(a.X, a.Y, a.Width, a.Height), screen.Scaling,
            bar.Position.X, bar.Position.Y, bar.Bounds.Width, bar.Bounds.Height, shadowInsetDip: 16);
        settings.Placement = PanelPlacement.Remembered;
        settings.RememberedX = remembered.XFraction;
        settings.RememberedY = remembered.YFraction;
        anchor = remembered.Anchor;
        SaveSettings();
    }

    private async Task HandleActionAsync(string action)
    {
        if (action == "hide") { Hide(true); return; }
        if (action == "settings") { ShowSettings(); return; }
        if (action == "signin-again") { await model.SignOutAsync(); if (!bar.IsVisible) await ShowAsync(); return; }
        var generation = api?.SessionGeneration;
        try
        {
            var uri = action switch
            {
                "plans" => await backend.PlansUriAsync(),
            "privacy" => AccountStore.SafeWebUri(account?.Config.Legal?.PrivacyUrl) ?? Website("privacy"),
            "terms" => AccountStore.SafeWebUri(account?.Config.Legal?.TermsUrl) ?? Website("terms"),
            "support" => AccountStore.SafeWebUri(account?.Config.Legal?.SupportUrl) ?? Website("support"),
            "delete" => account?.AccountDeletionUri ?? Website("account/delete"),
            _ => Website(WebsitePaths.Windows),
            };
            if (action == "plans" && generation != api?.SessionGeneration)
                throw new QuickBarException("issue.payment_unavailable");
            if (disposing) return;
            shell.OpenUri(uri); Hide(false);
        }
        catch (QuickBarException ex)
        {
            if (!bar.IsVisible) await ShowAsync();
            model.ReportIssue(ex.Key);
        }
        catch (OperationCanceledException) { }
        catch (Exception ex) when (ex is System.ComponentModel.Win32Exception or InvalidOperationException)
        { model.ReportIssue("issue.server"); }
    }

    private Uri Website(string path) => api?.WebsiteUri(path) ?? new Uri(AppInfo.ProductionBackend, path);

    private void ShowSettings()
    {
        Hide(false);
        if (settingsWindow is null)
        {
            settingsWindow = new SettingsWindow(settings, model, context!, startup, hotkey,
                SaveSettings, ApplyAppearance, action => _ = HandleActionAsync(action));
            settingsWindow.Activated += (_, _) => { if (!disposing) _ = model.RefreshAccountAsync(); };
            settingsWindow.Closed += (_, _) =>
            {
                settingsWindow = null; anchor = null;
                if (OperatingSystem.IsWindows() && focus is WindowsFocusTracker tracker) tracker.SettingsWindow = 0;
            };
        }
        settingsWindow.Show(); settingsWindow.Activate();
        if (OperatingSystem.IsWindows() && focus is WindowsFocusTracker tracker) tracker.SettingsWindow = settingsWindow.TryGetPlatformHandle()?.Handle ?? 0;
        _ = model.RefreshAccountAsync();
    }

    private void UpdateContextPicker()
    {
        if (context is null) return;
        var choices = context.Profiles.Select(p => new ProfileChoice(p.Id, p.Name)).Prepend(new ProfileChoice(null, localizer["bar.context.none"])).ToArray();
        var combo = new ComboBox { ItemsSource = choices, SelectedItem = choices.FirstOrDefault(c => c.Id == context.SelectedProfileId), HorizontalAlignment = Avalonia.Layout.HorizontalAlignment.Stretch };
        combo.SelectionChanged += async (_, _) =>
        {
            if (combo.SelectedItem is not ProfileChoice choice || context.SelectedProfileId == choice.Id) return;
            try { await context.SelectAsync(choice.Id); }
            catch (Exception ex) when (ex is IOException or InvalidOperationException or System.Security.Cryptography.CryptographicException)
            { model.ReportIssue("bar.context.load_failed"); }
        };
        view.FindControl<ContentControl>("ContextHost")!.Content = combo;
    }

    private void UpdateTray()
    {
        if (tray is null)
        {
            tray = new TrayIcon { ToolTipText = "AI Reply", Icon = new WindowIcon(AssetLoader.Open(new Uri("avares://AIReply/Assets/AIReply.ico"))) };
            tray.Clicked += (_, _) => _ = ShowAsync();
            TrayIcon.SetIcons(Application.Current!, [tray]);
        }
        var menu = new NativeMenu();
        void Add(string label, Action click, bool enabled = true)
        {
            var item = new NativeMenuItem(label) { IsEnabled = enabled }; item.Click += (_, _) => click(); menu.Add(item);
        }
        Add(localizer["menu.open"], () => _ = ShowAsync());
        Add(localizer["menu.dictate"], async () => { await ShowAsync(); await model.ToggleVoiceAsync(); }, model.CanRecord);
        menu.Add(new NativeMenuItemSeparator());
        if (model.Account.SignedIn)
        {
            Add(model.Account.Email ?? "AI Reply", () => { }, false);
            Add(model.Account.Balance, () => { }, false);
        }
        else Add(localizer["menu.sign_in"], () => _ = ShowAsync());
        Add(localizer["menu.plans"], () => _ = HandleActionAsync("plans"));
        menu.Add(new NativeMenuItemSeparator());
        Add(localizer["menu.settings"], ShowSettings);
        Add(localizer["menu.updates"], () => _ = HandleActionAsync("updates"));
        Add(localizer["menu.quit"], () => { disposing = true; desktop.Shutdown(); });
        tray.Menu = menu;
    }

    private Localizer MakeLocalizer() => new(settings.InterfaceLanguage switch { "kk" => UiLanguage.Kk, "en" => UiLanguage.En, "ru" => UiLanguage.Ru, _ => Localizer.FromCulture(CultureInfo.CurrentUICulture) });
    private void SaveSettings()
    {
        try { settingsStore.Save(settings); }
        catch (Exception ex) when (ex is IOException or UnauthorizedAccessException) { crash?.RecordFatal(ex); }
    }
    private void ApplyAppearance()
    {
        Application.Current!.RequestedThemeVariant = settings.Appearance switch { AppearancePreference.Light => ThemeVariant.Light, AppearancePreference.Dark => ThemeVariant.Dark, _ => ThemeVariant.Default };
        localizer = MakeLocalizer();
        model.ChangeLanguage(localizer);
        if (tray is not null) UpdateTray();
    }

    public void Dispose()
    {
        disposing = true;
        AppDomain.CurrentDomain.UnhandledException -= OnFatal;
        if (OperatingSystem.IsWindows()) Win32Properties.RemoveWndProcHookCallback(bar, WindowMessage);
        voice.Cancel(); model.Dispose(); tray?.Dispose(); context?.Dispose();
        _ = voice.DisposeAsync().AsTask();
        for (var i = owned.Count - 1; i >= 0; i--) owned[i].Dispose();
    }

    private sealed record ProfileChoice(Guid? Id, string Name) { public override string ToString() => Name; }
}
