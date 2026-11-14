using System.Runtime.Versioning;
using AirReply.Core.Infrastructure;
using AirReply.Presentation.Platform;

namespace AirReply.Platform.Windows;

[SupportedOSPlatform("windows")]
public sealed class WindowsClipboardService : IClipboardService
{
    private readonly WindowsMessageWindow window;
    private readonly IClock clock;
    private readonly uint baseline;
    private readonly uint exclude, ignore, history, cloud;
    private uint lastSeen, own, handled;
    private DateTimeOffset observedAt;
    private uint? cachedSequence;
    private string? cached;
    private bool disposed;
    private readonly bool listening;
    private readonly Timer? fallback;
    public WindowsClipboardService(WindowsMessageWindow window, IClock? clock = null)
    {
        this.window = window;
        this.clock = clock ?? SystemClock.Instance;
        baseline = lastSeen = NativeMethods.GetClipboardSequenceNumber();
        observedAt = this.clock.Now;
        exclude = NativeMethods.RegisterClipboardFormat("ExcludeClipboardContentFromMonitorProcessing");
        ignore = NativeMethods.RegisterClipboardFormat("Clipboard Viewer Ignore");
        history = NativeMethods.RegisterClipboardFormat("CanIncludeInClipboardHistory");
        cloud = NativeMethods.RegisterClipboardFormat("CanUploadToCloudClipboard");
        listening = NativeMethods.AddClipboardFormatListener(window.Handle);
        window.Message += OnMessage;
        if (!listening)
        {
            var handle = window.Handle;
            fallback = new Timer(_ => NativeMethods.PostMessage(handle, NativeMethods.ClipboardUpdate, 0, 0), null,
                TimeSpan.FromSeconds(1), TimeSpan.FromSeconds(1));
        }
    }
    private void OnMessage(uint message, nint wParam, nint lParam)
    {
        if (message == NativeMethods.ClipboardUpdate) Observe(NativeMethods.GetClipboardSequenceNumber());
    }
    private void Observe(uint sequence)
    {
        if (lastSeen == sequence) return;
        lastSeen = sequence;
        observedAt = clock.Now;
        cachedSequence = null;
        cached = null;
    }
    public CopiedTextResult TakeFreshCopiedText(TimeSpan maxAge)
    {
        if (disposed) return CopiedTextResult.None;
        window.VerifyThread();
        var sequence = NativeMethods.GetClipboardSequenceNumber();
        if (sequence == 0 || sequence == baseline || sequence == own || sequence == handled) return CopiedTextResult.None;
        var owner = NativeMethods.GetClipboardOwner();
        if (owner != 0)
        {
            NativeMethods.GetWindowThreadProcessId(owner, out var process);
            if (process == Environment.ProcessId || NativeMethods.IsHungAppWindow(owner)) return CopiedTextResult.None;
        }
        Observe(sequence);
        if (clock.Now - observedAt > maxAge) return CopiedTextResult.None;
        if (cachedSequence != sequence)
        {
            if (!TryOpen()) return CopiedTextResult.None;
            try
            {
                if (NativeMethods.GetClipboardSequenceNumber() != sequence) return CopiedTextResult.None;
                if (Available(exclude) || Available(ignore) || Disallows(history) || Disallows(cloud))
                {
                    handled = sequence;
                    return new CopiedTextResult(CopiedTextStatus.Concealed, null);
                }
                if (Available(NativeMethods.FileDrop) || !Available(NativeMethods.UnicodeText)) return CopiedTextResult.None;
                cached = ReadText();
                // Delayed rendering can advance the sequence while we hold the clipboard open.
                sequence = NativeMethods.GetClipboardSequenceNumber();
                lastSeen = sequence;
                cachedSequence = sequence;
            }
            finally { NativeMethods.CloseClipboard(); }
        }
        return string.IsNullOrWhiteSpace(cached) ? CopiedTextResult.None :
            new CopiedTextResult(CopiedTextStatus.Text, new CopiedText(cached.Replace("\r\n", "\n", StringComparison.Ordinal), sequence, observedAt));
    }
    public void MarkHandled(CopiedText copied)
    {
        ArgumentNullException.ThrowIfNull(copied);
        handled = copied.SequenceNumber;
        if (cachedSequence == handled) { cachedSequence = null; cached = null; }
    }
    public async Task<bool> CopyAsync(string text)
    {
        ArgumentNullException.ThrowIfNull(text);
        ObjectDisposedException.ThrowIf(disposed, this);
        window.VerifyThread();
        text = text.Replace("\r\n", "\n", StringComparison.Ordinal).Replace("\n", "\r\n", StringComparison.Ordinal);
        for (var attempt = 0; attempt < 10; attempt++)
        {
            ObjectDisposedException.ThrowIf(disposed, this);
            window.VerifyThread();
            if (NativeMethods.OpenClipboard(window.Handle))
            {
                try { return WriteText(text); }
                finally { NativeMethods.CloseClipboard(); }
            }
            if (attempt < 9) await Task.Delay(25);
        }
        return false;
    }
    private unsafe bool WriteText(string text)
    {
        nint memory = 0;
        try
        {
            memory = NativeMethods.GlobalAlloc(2, checked((nuint)(text.Length + 1) * 2));
            if (memory == 0) return false;
            var pointer = (char*)NativeMethods.GlobalLock(memory);
            if (pointer == null) return false;
            try { text.AsSpan().CopyTo(new Span<char>(pointer, text.Length)); pointer[text.Length] = '\0'; }
            finally { NativeMethods.GlobalUnlock(memory); }
            if (!NativeMethods.EmptyClipboard() || NativeMethods.SetClipboardData(NativeMethods.UnicodeText, memory) == 0)
                return false;
            memory = 0;
            own = NativeMethods.GetClipboardSequenceNumber();
            Observe(own);
            return true;
        }
        finally { if (memory != 0) NativeMethods.GlobalFree(memory); }
    }
    private bool TryOpen()
    {
        for (var attempt = 0; attempt < 5; attempt++)
        {
            if (NativeMethods.OpenClipboard(window.Handle)) return true;
            if (attempt < 4) Thread.Sleep(10);
        }
        return false;
    }
    private static bool Available(uint format) => format != 0 && NativeMethods.IsClipboardFormatAvailable(format);
    private static unsafe bool Disallows(uint format)
    {
        if (!Available(format)) return false;
        var memory = NativeMethods.GetClipboardData(format);
        if (memory == 0 || NativeMethods.GlobalSize(memory) < sizeof(uint)) return true;
        var pointer = NativeMethods.GlobalLock(memory);
        if (pointer == null) return true;
        try { return *(uint*)pointer == 0; }
        finally { NativeMethods.GlobalUnlock(memory); }
    }
    private static unsafe string? ReadText()
    {
        var memory = NativeMethods.GetClipboardData(NativeMethods.UnicodeText);
        if (memory == 0) return null;
        var pointer = NativeMethods.GlobalLock(memory);
        if (pointer == null) return null;
        try
        {
            // Bound malformed or malicious clipboard data; server input limits are much smaller than this.
            var length = (int)Math.Min((ulong)NativeMethods.GlobalSize(memory) / 2, 1_000_000UL);
            var span = new ReadOnlySpan<char>(pointer, length);
            var end = span.IndexOf('\0');
            return new string(end >= 0 ? span[..end] : span);
        }
        finally { NativeMethods.GlobalUnlock(memory); }
    }
    public void Dispose()
    {
        if (disposed) return;
        window.VerifyThread();
        fallback?.Dispose();
        if (listening) NativeMethods.RemoveClipboardFormatListener(window.Handle);
        window.Message -= OnMessage;
        cached = null;
        disposed = true;
    }
}
