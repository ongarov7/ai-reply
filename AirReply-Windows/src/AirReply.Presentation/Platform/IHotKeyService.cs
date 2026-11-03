using AirReply.Core.Settings;

namespace AirReply.Presentation.Platform;

public enum HotKeyResult { Ok, NotAllowed, AlreadyTaken, Failed }

/// <summary>One global shortcut that opens the quick bar (Win32 RegisterHotKey on a message-only window).</summary>
public interface IHotKeyService : IDisposable
{
    /// <summary>Raised on the UI thread when the registered shortcut is pressed.</summary>
    event EventHandler? Pressed;

    KeyGesture? Current { get; }

    /// <summary>Replaces the previous registration. On failure the previous one stays registered.</summary>
    HotKeyResult Register(KeyGesture gesture);

    void Unregister();
}
