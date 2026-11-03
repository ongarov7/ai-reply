namespace AirReply.Presentation.Platform;

/// <summary>Start with Windows (HKCU Run key).</summary>
public interface IStartupService
{
    bool IsEnabled { get; }
    void SetEnabled(bool enabled);
}

/// <summary>Opens web pages and system settings pages (ms-settings:privacy-microphone) in the default handler.</summary>
public interface IShell
{
    void OpenUri(Uri uri);
}

/// <summary>The keyboard language of the window the user was typing in (kk, ru, en or null).</summary>
public interface IKeyboardLanguage
{
    string? CurrentInputLanguage(nint window);
}

/// <summary>Runs work on the UI thread. View models never touch UI state from other threads.</summary>
public interface IUiDispatcher
{
    bool CheckAccess();
    void Post(Action action);
    Task InvokeAsync(Func<Task> action);
}
