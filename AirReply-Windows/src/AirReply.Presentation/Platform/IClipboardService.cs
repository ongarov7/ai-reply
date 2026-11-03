namespace AirReply.Presentation.Platform;

/// <summary>Text another app copied, with the clipboard sequence number that identifies that copy.</summary>
public sealed record CopiedText(string Text, uint SequenceNumber, DateTimeOffset ObservedAt);

public enum CopiedTextStatus { None, Text, Concealed, Denied }

public readonly record struct CopiedTextResult(CopiedTextStatus Status, CopiedText? Copied)
{
    public static CopiedTextResult None => new(CopiedTextStatus.None, null);
}

/// <summary>
/// Watches the clipboard (AddClipboardFormatListener) without reading it; content is read only when the bar opens.
/// Concealed items (password managers) and the app's own copies are never offered back.
/// </summary>
public interface IClipboardService : IDisposable
{
    /// <summary>Text copied in another app within <paramref name="maxAge"/> that was not handled yet.</summary>
    CopiedTextResult TakeFreshCopiedText(TimeSpan maxAge);

    /// <summary>The user replied to it or dismissed it: never offer this copy again.</summary>
    void MarkHandled(CopiedText copied);

    /// <summary>Puts plain text on the clipboard; remembered as our own so it is never offered back.</summary>
    Task<bool> CopyAsync(string text);
}
