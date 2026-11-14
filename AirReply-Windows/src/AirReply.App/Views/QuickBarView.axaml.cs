using AirReply.Core.Generation;
using AirReply.Presentation.Models;
using Avalonia.Controls;
using Avalonia.Input;
using Avalonia.Interactivity;
using Avalonia.Markup.Xaml;

namespace AirReply.App.Views;

public partial class QuickBarView : UserControl
{
    public QuickBarView()
    {
        AvaloniaXamlLoader.Load(this);
        AddHandler(KeyDownEvent, OnKeyDown, RoutingStrategies.Tunnel);
        DataContextChanged += (_, _) =>
        {
            if (DataContext is QuickBarViewModel model) model.PropertyChanged += (_, e) =>
            {
                if (e.PropertyName == nameof(QuickBarViewModel.Issue))
                    this.FindControl<Button>("SignInAgainButton")!.IsVisible = model.Issue == model.L["issue.pro_device_identity"];
            };
        };
    }

    private QuickBarViewModel Model => (QuickBarViewModel)DataContext!;
    public event EventHandler<string>? ActionRequested;
    public void FocusEditor()
    {
        var box = this.FindControl<TextBox>(Model.SignedOut ? Model.CodeSent ? "CodeField" : "EmailField" : "DraftField");
        box?.Focus();
        if (box is not null) box.CaretIndex = box.Text?.Length ?? 0;
    }

    private async void OnKeyDown(object? sender, KeyEventArgs e)
    {
        if (e.Key == Key.Escape) { Model.Cancel(); e.Handled = true; }
        else if (e.Key == Key.Enter && (e.KeyModifiers & (KeyModifiers.Alt | KeyModifiers.Shift)) == 0)
        {
            e.Handled = true;
            if (e.KeyModifiers.HasFlag(KeyModifiers.Control) && Model.ShowResult) await Model.CopyAsync();
            else if (Model.IsRecording) await Model.ToggleVoiceAsync();
            else await Model.SubmitAsync();
        }
        else if (e.Key == Key.D && e.KeyModifiers == KeyModifiers.Control)
        { e.Handled = true; await Model.ToggleVoiceAsync(); }
        else if (e.KeyModifiers == KeyModifiers.Control && e.Key is >= Key.D1 and <= Key.D4)
        {
            e.Handled = true;
            Model.IsReply = true;
            Model.Recipient = (Recipient)((int)e.Key - (int)Key.D1);
            await Model.SubmitAsync();
        }
        else if (e.Key == Key.OemComma && e.KeyModifiers == KeyModifiers.Control)
        { e.Handled = true; ActionRequested?.Invoke(this, "settings"); }
    }

    private void SignInAgain(object? sender, RoutedEventArgs e) => ActionRequested?.Invoke(this, "signin-again");
    private void OpenSettings(object? sender, RoutedEventArgs e) => ActionRequested?.Invoke(this, "settings");
    private void HideBar(object? sender, RoutedEventArgs e) => ActionRequested?.Invoke(this, "hide");
    private void OpenPlans(object? sender, RoutedEventArgs e) => ActionRequested?.Invoke(this, "plans");
    private void OpenPrivacy(object? sender, RoutedEventArgs e) => ActionRequested?.Invoke(this, "privacy");
    private void OpenTerms(object? sender, RoutedEventArgs e) => ActionRequested?.Invoke(this, "terms");
    private void AnotherEmail(object? sender, RoutedEventArgs e) { Model.AnotherEmail(); FocusEditor(); }
    private void DismissSource(object? sender, RoutedEventArgs e) { Model.DismissSource(); FocusEditor(); }
    private void NewConversation(object? sender, RoutedEventArgs e) { Model.ResetConversation(); FocusEditor(); }
    private void CancelOperation(object? sender, RoutedEventArgs e) => Model.Cancel();
    private async void ReplyTo(object? sender, RoutedEventArgs e)
    {
        if (sender is Button { Tag: string value } && Enum.TryParse<Recipient>(value, out var chosen))
        { Model.Recipient = chosen; await Model.SubmitAsync(); }
    }
}
