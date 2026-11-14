using AirReply.Context;
using AirReply.Core.Infrastructure;
using AirReply.Core.Settings;
using AirReply.Platform.Windows;
using AirReply.Presentation.Models;
using AirReply.Presentation.Platform;
using Avalonia;
using Avalonia.Controls;
using Avalonia.Input;
using Avalonia.Layout;
using Avalonia.Platform.Storage;
using Avalonia.Threading;
using Gesture = AirReply.Core.Settings.KeyGesture;
using Modifiers = AirReply.Core.Settings.KeyModifiers;

namespace AirReply.App.Views;

internal sealed class SettingsWindow : Window
{
    private readonly AppSettings settings;
    private readonly QuickBarViewModel model;
    private readonly ContextWorkspace context;
    private readonly IStartupService startup;
    private readonly IHotKeyService? hotkey;
    private readonly Action save;
    private readonly Action appearance;
    private readonly Action<string> open;
    private readonly TextBlock accountText = new() { TextWrapping = Avalonia.Media.TextWrapping.Wrap };
    private readonly TextBlock notice = new() { TextWrapping = Avalonia.Media.TextWrapping.Wrap };
    private readonly Dictionary<string, TextBox> fields = new(StringComparer.Ordinal);
    private readonly ComboBox profilePicker = new() { HorizontalAlignment = HorizontalAlignment.Stretch };
    private readonly StackPanel attachments = new() { Spacing = 6 };
    private readonly Button shortcut = new();
    private readonly TextBlock generalNotice = new() { TextWrapping = Avalonia.Media.TextWrapping.Wrap };
    private readonly ComboBox role = new() { MinWidth = 240 };
    private ContextProfile editing = new();
    private bool dirty, loading, recording, closingApproved, dialog;
    private string? accountId;
    private string T(string key) => model.L[key];

    public SettingsWindow(AppSettings settings, QuickBarViewModel model, ContextWorkspace context,
        IStartupService startup, IHotKeyService? hotkey, Action save, Action appearance, Action<string> open)
    {
        this.settings = settings; this.model = model; this.context = context; this.startup = startup;
        this.hotkey = hotkey; this.save = save; this.appearance = appearance; this.open = open;
        Title = "AI Reply · " + T("menu.settings").TrimEnd('…'); Width = 800; Height = 680;
        MinWidth = 620; MinHeight = 480;
        WindowStartupLocation = WindowStartupLocation.CenterScreen;
        var tabs = new TabControl { Margin = new Thickness(20) };
        tabs.ItemsSource = new[]
        {
            Tab("settings.general", General()), Tab("settings.voice", Voice()), Tab("settings.account", Account()),
            Tab("profiles.title", ProfilesEditor()), Tab("settings.about", About()),
        };
        Content = tabs;
        profilePicker.SelectionChanged += async (_, _) =>
        {
            if (loading || profilePicker.SelectedItem is not ProfileChoice choice) return;
            if (dirty && !await ConfirmAsync(T("profiles.discard"))) { ReloadPicker(editing.Id); return; }
            LoadProfile(context.Profiles.FirstOrDefault(p => p.Id == choice.Id) ?? new ContextProfile());
        };
        AddHandler(KeyDownEvent, RecordShortcut, Avalonia.Interactivity.RoutingStrategies.Tunnel);
        shortcut.LostFocus += (_, _) => { if (recording) RestoreHotkey(); };
        Deactivated += (_, _) => { if (recording) RestoreHotkey(); };
        Closing += async (_, e) =>
        {
            if (recording) RestoreHotkey();
            if (!dirty || closingApproved) return;
            e.Cancel = true;
            if (await ConfirmAsync(T("profiles.discard"))) { closingApproved = true; Close(); }
        };
        context.Changed += ContextChanged;
        Closed += (_, _) => context.Changed -= ContextChanged;
        RefreshAccount();
        LoadProfile(context.Profiles.Count > 0 ? context.Profiles[0] : new ContextProfile());
    }

    private TabItem Tab(string key, Control content) => new() { Header = T(key), Content = new ScrollViewer { Content = content, HorizontalScrollBarVisibility = Avalonia.Controls.Primitives.ScrollBarVisibility.Disabled } };
    private static StackPanel Stack() => new() { Spacing = 14, Margin = new Thickness(4, 20, 12, 16) };
    private TextBlock Label(string key) => new() { Text = T(key), TextWrapping = Avalonia.Media.TextWrapping.Wrap };
    private Button Button(string key, Func<Task> action)
    {
        var b = new Button { Content = T(key) };
        b.Click += async (_, _) =>
        {
            b.IsEnabled = false;
            try { await action(); }
            catch (Exception ex) when (ex is ContextImportException or IOException or UnauthorizedAccessException or InvalidOperationException or ArgumentException)
            { notice.Text = ex is ContextImportException import ? T(import.MessageKey) : T("bar.context.save_failed"); }
            finally { b.IsEnabled = true; }
        };
        return b;
    }
    private CheckBox Check(string key, bool value, Action<bool> change)
    {
        var box = new CheckBox { Content = T(key), IsChecked = value };
        box.IsCheckedChanged += (_, _) =>
        {
            try { change(box.IsChecked == true); save(); }
            catch (Exception ex) when (ex is IOException or UnauthorizedAccessException or System.Security.SecurityException)
            { generalNotice.Text = T("issue.server"); }
        };
        return box;
    }
    private ComboBox Select(string[] keys, int selected, Action<int> change)
    {
        var combo = new ComboBox { ItemsSource = keys.Select(T).ToArray(), SelectedIndex = selected, MinWidth = 240 };
        combo.SelectionChanged += (_, _) => { if (combo.SelectedIndex >= 0) { change(combo.SelectedIndex); save(); } };
        return combo;
    }

    private StackPanel General()
    {
        var panel = Stack();
        panel.Children.Add(Label("settings.shortcut"));
        shortcut.Content = HotKeyRules.Display(settings.Shortcut);
        shortcut.Click += (_, _) =>
        {
            if (hotkey is null) { shortcut.Content = T("app.platform_preview"); return; }
            hotkey.Unregister(); recording = true; shortcut.Content = T("settings.shortcut.recording"); shortcut.Focus();
        };
        panel.Children.Add(shortcut);
        if (hotkey?.Current is { } current && current != settings.Shortcut)
            panel.Children.Add(new TextBlock { Text = model.L.Format("settings.shortcut.fallback", HotKeyRules.Display(settings.Shortcut), HotKeyRules.Display(current)), TextWrapping = Avalonia.Media.TextWrapping.Wrap });
        panel.Children.Add(Label("settings.placement"));
        panel.Children.Add(Select(["settings.placement.top", "settings.placement.bottom", "settings.placement.remembered"], (int)settings.Placement, i => settings.Placement = (PanelPlacement)i));
        panel.Children.Add(Label("settings.appearance"));
        panel.Children.Add(Select(["settings.appearance.system", "settings.appearance.light", "settings.appearance.dark"], (int)settings.Appearance, i => { settings.Appearance = (AppearancePreference)i; appearance(); }));
        panel.Children.Add(Check("settings.offer_copied", settings.OfferCopiedText, v => settings.OfferCopiedText = v));
        panel.Children.Add(Check("settings.close_after_copy", settings.CloseAfterCopy, v => settings.CloseAfterCopy = v));
        panel.Children.Add(Check("settings.paste_after_copy", settings.PasteAfterCopy, v => settings.PasteAfterCopy = v));
        panel.Children.Add(Label("settings.paste_after_copy.footer"));
        panel.Children.Add(Check("settings.login_item", startup.IsEnabled, v => { startup.SetEnabled(v); settings.LaunchAtStartup = v; }));
        if (OperatingSystem.IsWindows() && startup is WindowsStartupService { State: WindowsStartupState.DisabledInWindowsSettings })
            panel.Children.Add(Label("settings.login_item.approval"));
        var language = new ComboBox { ItemsSource = new[] { "Русский", "Қазақша", "English" }, SelectedIndex = (int)model.L.Language, MinWidth = 240 };
        language.SelectionChanged += (_, _) => { settings.InterfaceLanguage = language.SelectedIndex switch { 1 => "kk", 2 => "en", _ => "ru" }; save(); appearance(); };
        panel.Children.Add(language);
        panel.Children.Add(generalNotice);
        return panel;
    }

    private StackPanel Voice()
    {
        var panel = Stack();
        panel.Children.Add(Label("settings.dictation_language"));
        var languages = new ComboBox { ItemsSource = new[] { T("language.auto"), "Русский", "Қазақша", "English" }, SelectedIndex = (int)settings.DictationLanguage, MinWidth = 240 };
        languages.SelectionChanged += (_, _) => { settings.DictationLanguage = (DictationLanguage)languages.SelectedIndex; save(); };
        panel.Children.Add(languages);
        panel.Children.Add(Label("settings.voice.footer"));
        panel.Children.Add(Button("settings.open_system_settings", () => { if (OperatingSystem.IsWindows()) new WindowsShell().OpenUri(new Uri("ms-settings:privacy-microphone")); return Task.CompletedTask; }));
        return panel;
    }

    private StackPanel Account()
    {
        var panel = Stack(); panel.Children.Add(accountText);
        panel.Children.Add(Button("settings.account.refresh", () => model.RefreshAccountAsync()));
        panel.Children.Add(Button("settings.account.plans", () => { open("plans"); return Task.CompletedTask; }));
        panel.Children.Add(Button("settings.account.sign_out", () => model.SignOutAsync()));
        panel.Children.Add(Button("settings.account.delete", () => { open("delete"); return Task.CompletedTask; }));
        return panel;
    }

    public void RefreshAccount()
    {
        var account = model.Account;
        accountText.Text = account.SignedIn ? $"{T("settings.account.email")}: {account.Email}\n{account.Balance}" : T("settings.account.signed_out");
        if (accountId != account.UserId)
        {
            accountId = account.UserId;
            dirty = false;
            LoadProfile(new ContextProfile());
        }
    }

    private StackPanel ProfilesEditor()
    {
        var panel = Stack();
        panel.Children.Add(profilePicker);
        panel.Children.Add(Label("context.profile.limits"));
        role.ItemsSource = new[] { T("bar.context.personal"), T("bar.context.business") };
        role.SelectedIndex = 0;
        role.SelectionChanged += (_, _) => { if (!loading) dirty = true; };
        panel.Children.Add(role);
        panel.Children.Add(Button("bar.context.new", async () => { if (!dirty || await ConfirmAsync(T("profiles.discard"))) LoadProfile(new ContextProfile()); }));
        foreach (var (name, key, multiline) in new[]
        {
            ("Name", "bar.context.profile_name", false), ("PersonName", "bar.context.person", false),
            ("Occupation", "bar.context.occupation", false), ("Company", "bar.context.company", false),
            ("Industry", "bar.context.industry", false), ("CommunicationStyle", "bar.context.style", false),
            ("Instructions", "bar.context.profile_instructions", true), ("Policies", "bar.context.policies", true),
            ("Products", "bar.context.products", true), ("Services", "bar.context.services", true),
            ("Description", "bar.context.description", true),
        })
        {
            panel.Children.Add(Label(key));
            var box = new TextBox { AcceptsReturn = multiline, TextWrapping = Avalonia.Media.TextWrapping.Wrap, MinHeight = multiline ? 64 : 32, MaxHeight = 160 };
            Avalonia.Automation.AutomationProperties.SetName(box, T(key));
            box.TextChanged += (_, _) => { if (!loading) dirty = true; };
            fields[name] = box; panel.Children.Add(box);
        }
        panel.Children.Add(Button("bar.context.save", SaveProfileAsync));
        panel.Children.Add(Label("bar.context.formats"));
        panel.Children.Add(attachments);
        panel.Children.Add(Button("bar.context.attach", async () =>
        {
            var files = await StorageProvider.OpenFilePickerAsync(new FilePickerOpenOptions
            {
                Title = T("bar.context.attach"), AllowMultiple = false,
                FileTypeFilter = [new FilePickerFileType("Documents") { Patterns = ["*.xlsx", "*.csv", "*.pdf", "*.docx", "*.txt"] }],
            });
            if (files.Count == 0) return;
            if (!context.Profiles.Any(p => p.Id == editing.Id)) await SaveProfileAsync();
            var path = files[0].TryGetLocalPath();
            if (path is null) throw new IOException("Local file required.");
            notice.Text = T("bar.context.importing");
            await context.ImportAsync(editing.Id, path);
            notice.Text = T("profiles.saved"); RefreshDocuments();
        }));
        panel.Children.Add(Button("bar.context.delete", async () =>
        {
            if (await ConfirmAsync(T("profiles.confirm_delete"))) { await context.DeleteAsync(editing.Id); LoadProfile(new ContextProfile()); }
        }));
        panel.Children.Add(Button("profiles.delete_all", async () =>
        {
            if (await ConfirmAsync(T("profiles.confirm_all"))) { await context.DeleteAllAsync(); LoadProfile(new ContextProfile()); }
        }));
        panel.Children.Add(notice);
        panel.Children.Add(Label("bar.context.privacy"));
        return panel;
    }

    private async Task SaveProfileAsync()
    {
        string V(string key) => fields[key].Text ?? "";
        editing = await context.SaveAsync(editing with
        {
            Role = role.SelectedIndex == 1 ? "business" : "personal",
            Name = V("Name"), PersonName = V("PersonName"), Occupation = V("Occupation"), Company = V("Company"),
            Industry = V("Industry"), CommunicationStyle = V("CommunicationStyle"), Instructions = V("Instructions"),
            Policies = V("Policies"), Products = V("Products"), Services = V("Services"), Description = V("Description"),
        });
        dirty = false; notice.Text = T("profiles.saved"); ReloadPicker(editing.Id); RefreshDocuments();
    }

    private void LoadProfile(ContextProfile profile)
    {
        loading = true; editing = profile;
        role.SelectedIndex = profile.Role == "business" ? 1 : 0;
        foreach (var (key, box) in fields) box.Text = typeof(ContextProfile).GetProperty(key)?.GetValue(profile)?.ToString() ?? "";
        ReloadPicker(profile.Id); RefreshDocuments(); dirty = false; loading = false;
    }

    private void ContextChanged(object? sender, EventArgs e) => Dispatcher.UIThread.Post(() =>
    {
        ReloadPicker(editing.Id); RefreshDocuments();
        if (context.State != ContextStorageState.Ready) notice.Text = T("bar.context.load_failed");
    });

    private void ReloadPicker(Guid? selected)
    {
        var previous = loading; loading = true;
        var choices = context.Profiles.Select(p => new ProfileChoice(p.Id, p.Name)).ToArray();
        profilePicker.ItemsSource = choices; profilePicker.SelectedItem = choices.FirstOrDefault(p => p.Id == selected);
        loading = previous;
    }

    private void RefreshDocuments()
    {
        attachments.Children.Clear();
        foreach (var document in context.Profiles.FirstOrDefault(p => p.Id == editing.Id)?.Attachments ?? [])
        {
            var row = new StackPanel { Orientation = Orientation.Horizontal, Spacing = 8 };
            row.Children.Add(new TextBlock { Text = document.Name, MaxWidth = 400, TextWrapping = Avalonia.Media.TextWrapping.Wrap, VerticalAlignment = VerticalAlignment.Center });
            var remove = Button("bar.context.remove_attachment", async () =>
            { if (await ConfirmAsync(T("profiles.confirm_document"))) await context.RemoveDocumentAsync(editing.Id, document.Id); });
            remove.Content = "✕"; Avalonia.Automation.AutomationProperties.SetName(remove, T("bar.context.remove_attachment") + ": " + document.Name);
            row.Children.Add(remove); attachments.Children.Add(row);
        }
    }

    private StackPanel About()
    {
        var panel = Stack(); var info = AppInfo.Current();
        panel.Children.Add(new TextBlock { Text = "AI Reply", FontSize = 28, FontWeight = Avalonia.Media.FontWeight.SemiBold });
        panel.Children.Add(new TextBlock { Text = model.L.Format("settings.about.version", info.Version, info.Build) });
        panel.Children.Add(Button("settings.about.support", () => { open("support"); return Task.CompletedTask; }));
        panel.Children.Add(Button("menu.updates", () => { open("updates"); return Task.CompletedTask; }));
        return panel;
    }

    private async Task<bool> ConfirmAsync(string message)
    {
        if (dialog) return false;
        dialog = true;
        try
        {
            var window = new Window { Title = "AI Reply", Width = 420, SizeToContent = SizeToContent.Height, CanResize = false, WindowStartupLocation = WindowStartupLocation.CenterOwner };
            var panel = new StackPanel { Spacing = 16, Margin = new Thickness(24) };
            panel.Children.Add(new TextBlock { Text = message, TextWrapping = Avalonia.Media.TextWrapping.Wrap });
            var buttons = new StackPanel { Orientation = Orientation.Horizontal, Spacing = 10, HorizontalAlignment = HorizontalAlignment.Right };
            var cancel = new Button { Content = T("bar.context.cancel") }; cancel.Click += (_, _) => window.Close(false);
            var confirm = new Button { Content = T("issue.action.ok") }; confirm.Click += (_, _) => window.Close(true);
            buttons.Children.Add(cancel); buttons.Children.Add(confirm); panel.Children.Add(buttons); window.Content = panel;
            return await window.ShowDialog<bool>(this);
        }
        finally { dialog = false; }
    }

    private void RestoreHotkey()
    {
        if (OperatingSystem.IsWindows() && hotkey is WindowsHotKeyService native) native.RegisterSaved(settings.Shortcut);
        else hotkey?.Register(settings.Shortcut);
        recording = false; shortcut.Content = HotKeyRules.Display(settings.Shortcut);
    }
    private void RecordShortcut(object? sender, KeyEventArgs e)
    {
        if (!recording) return;
        e.Handled = true;
        if (e.Key == Key.Escape) { RestoreHotkey(); return; }
        uint key = e.Key switch
        {
            >= Key.A and <= Key.Z => 0x41u + (uint)(e.Key - Key.A),
            >= Key.D0 and <= Key.D9 => 0x30u + (uint)(e.Key - Key.D0),
            >= Key.F1 and <= Key.F24 => 0x70u + (uint)(e.Key - Key.F1),
            Key.Space => 0x20, _ => 0,
        };
        if (key == 0) return;
        var modifiers = Modifiers.None;
        if (e.KeyModifiers.HasFlag(Avalonia.Input.KeyModifiers.Control)) modifiers |= Modifiers.Control;
        if (e.KeyModifiers.HasFlag(Avalonia.Input.KeyModifiers.Alt)) modifiers |= Modifiers.Alt;
        if (e.KeyModifiers.HasFlag(Avalonia.Input.KeyModifiers.Shift)) modifiers |= Modifiers.Shift;
        if (e.KeyModifiers.HasFlag(Avalonia.Input.KeyModifiers.Meta)) modifiers |= Modifiers.Win;
        var gesture = new Gesture(key, modifiers);
        if (hotkey?.Register(gesture) == HotKeyResult.Ok) { settings.Shortcut = gesture; save(); recording = false; shortcut.Content = HotKeyRules.Display(gesture); }
        else { RestoreHotkey(); shortcut.Content = T("shortcut.invalid"); }
    }

    private sealed record ProfileChoice(Guid Id, string Name) { public override string ToString() => Name; }
}
