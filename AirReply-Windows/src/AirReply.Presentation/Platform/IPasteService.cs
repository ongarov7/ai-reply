namespace AirReply.Presentation.Platform;

/// <summary>Remembers the window that was active before the bar appeared and gives focus back to it.</summary>
public interface IFocusTracker
{
    /// <summary>Called right before the bar shows; ignores the app's own windows.</summary>
    void RememberForegroundWindow();

    nint PreviousWindow { get; }

    /// <summary>Best effort; returns false when Windows refused to switch focus.</summary>
    bool RestorePreviousWindow();
}

public enum PasteAvailability { Available, TargetElevated, NoTarget }

/// <summary>Optional auto-paste: presses Ctrl+V in the previous window (SendInput). Elevated targets are skipped (UIPI).</summary>
public interface IPasteService
{
    PasteAvailability GetAvailability(nint targetWindow);

    Task<bool> PasteAsync(nint targetWindow, CancellationToken cancellationToken = default);
}
