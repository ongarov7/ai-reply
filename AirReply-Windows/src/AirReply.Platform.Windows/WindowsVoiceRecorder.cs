using System.Runtime.Versioning;
using Microsoft.Win32;
using NAudio.CoreAudioApi;
using NAudio.Wave;
using AirReply.Presentation.Platform;

namespace AirReply.Platform.Windows;

/// <summary>Shared-mode Console microphone capture. Device open/stop/dispose never blocks the UI thread.</summary>
[SupportedOSPlatform("windows")]
public sealed class WindowsVoiceRecorder : IVoiceRecorder
{
    private readonly SemaphoreSlim lifecycle = new(1, 1);
    private readonly Lock gate = new();
    private Capture? current;
    private bool disposed;
    public event EventHandler<float>? Level;
    public event EventHandler<VoiceCaptureException?>? Stopped;
    public bool IsRecording { get { lock (gate) { return current is { Active: true }; } } }
    public async Task StartAsync(TimeSpan maxDuration, CancellationToken cancellationToken = default)
    {
        ObjectDisposedException.ThrowIf(disposed, this);
        if (maxDuration <= TimeSpan.Zero || maxDuration > TimeSpan.FromSeconds(40)) throw new ArgumentOutOfRangeException(nameof(maxDuration));
        await lifecycle.WaitAsync(cancellationToken);
        try
        {
            lock (gate) { if (current != null) throw new InvalidOperationException("A recording is already active."); }
            var capture = new Capture(maxDuration, level => Level?.Invoke(this, level), error => Stopped?.Invoke(this, error));
            lock (gate) { current = capture; }
            try
            {
                await Task.Run(capture.Open, cancellationToken);
                cancellationToken.ThrowIfCancellationRequested();
            }
            catch (Exception exception)
            {
                lock (gate) { if (current == capture) current = null; }
                await Task.Run(capture.Delete, CancellationToken.None);
                if (exception is OperationCanceledException) throw;
                throw MapError(exception);
            }
        }
        finally { lifecycle.Release(); }
    }
    public async Task<RecordedAudio?> StopAsync()
    {
        await lifecycle.WaitAsync();
        try
        {
            Capture? capture;
            lock (gate) { capture = current; current = null; }
            return capture == null ? null : await Task.Run(capture.Finish);
        }
        finally { lifecycle.Release(); }
    }
    public void Cancel()
    {
        Capture? capture;
        lock (gate) { capture = current; current = null; }
        if (capture != null) _ = Task.Run(capture.Delete);
    }
    public async ValueTask DisposeAsync()
    {
        if (disposed) return;
        disposed = true;
        await lifecycle.WaitAsync();
        try
        {
            Capture? capture;
            lock (gate) { capture = current; current = null; }
            if (capture != null) await Task.Run(capture.Delete, CancellationToken.None);
        }
        finally { lifecycle.Release(); lifecycle.Dispose(); }
    }
    internal static VoiceCaptureException MapError(Exception exception)
    {
        for (Exception? current = exception; current != null; current = current.InnerException)
        {
            if (current is VoiceCaptureException known) return known;
            if (current.HResult == unchecked((int)0x80070005)) return new(VoiceCaptureError.PermissionDenied, inner: exception);
            if (current.HResult == unchecked((int)0x80070490)) return new(VoiceCaptureError.NoInputDevice, inner: exception);
        }
        return new(VoiceCaptureError.RecordingFailed, inner: exception);
    }
    private static bool MicrophoneDenied()
    {
        const string path = @"SOFTWARE\Microsoft\Windows\CurrentVersion\CapabilityAccessManager\ConsentStore\microphone";
        foreach (var hive in new[] { Registry.CurrentUser, Registry.LocalMachine })
        {
            using var key = hive.OpenSubKey(path);
            if (string.Equals(key?.GetValue("Value") as string, "Deny", StringComparison.OrdinalIgnoreCase)) return true;
        }
        using var desktop = Registry.CurrentUser.OpenSubKey(path + @"\NonPackaged");
        if (string.Equals(desktop?.GetValue("Value") as string, "Deny", StringComparison.OrdinalIgnoreCase)) return true;
        var exe = Environment.ProcessPath?.Replace('\\', '#');
        using var app = exe == null ? null : Registry.CurrentUser.OpenSubKey(path + @"\NonPackaged\" + exe);
        return string.Equals(app?.GetValue("Value") as string, "Deny", StringComparison.OrdinalIgnoreCase);
    }
    private sealed class Capture(TimeSpan limit, Action<float> level, Action<VoiceCaptureException?> stopped) : IDisposable
    {
        private readonly Lock gate = new();
        private readonly Lock stoppingGate = new();
        private WasapiRecorder? recorder;
        private MMDeviceEnumerator? enumerator;
        private MMDeviceNotificationClient? notifications;
        private MMDevice? device;
        private PcmWaveWriter? writer;
        private Pcm16MonoConverter? converter;
        private Timer? timer;
        private string? path;
        private bool ended;
        private bool cancelled;
        private bool notified;
        public bool Active { get { lock (gate) { return !ended && recorder != null; } } }
        public void Open()
        {
            lock (gate)
            {
                if (cancelled) return;
                if (MicrophoneDenied()) throw new VoiceCaptureException(VoiceCaptureError.PermissionDenied);
                enumerator = new MMDeviceEnumerator();
                if (!enumerator.HasDefaultAudioEndpoint(DataFlow.Capture, Role.Console))
                    throw new VoiceCaptureException(VoiceCaptureError.NoInputDevice);
                notifications = enumerator.CreateNotificationClient(useSynchronizationContext: false);
                notifications.DefaultDeviceChanged += (_, args) =>
                {
                    if (args.Flow == DataFlow.Capture && args.Role == Role.Console) _ = Task.Run(OnDefaultDeviceChanged);
                };
                device = enumerator.GetDefaultAudioEndpoint(DataFlow.Capture, Role.Console);
                var directory = Path.Combine(Path.GetTempPath(), "AirReplyVoice");
                Directory.CreateDirectory(directory);
                path = Path.Combine(directory, Guid.NewGuid().ToString("N") + ".wav");
                writer = new PcmWaveWriter(new FileStream(path, FileMode.CreateNew, FileAccess.ReadWrite, FileShare.Read), limit);
                try { AttachWithFormatFallback(); }
                catch (Exception exception) when (exception.HResult == unchecked((int)0x88890004))
                {
                    // A Bluetooth endpoint can switch profiles during activation. Retry once on the new Console device.
                    recorder?.Dispose(); recorder = null;
                    device.Dispose();
                    if (!enumerator.HasDefaultAudioEndpoint(DataFlow.Capture, Role.Console))
                        throw new VoiceCaptureException(VoiceCaptureError.NoInputDevice);
                    device = enumerator.GetDefaultAudioEndpoint(DataFlow.Capture, Role.Console);
                    AttachWithFormatFallback();
                }
                timer = new Timer(_ => End(null), null, limit, Timeout.InfiniteTimeSpan);
            }
        }
        private void AttachWithFormatFallback()
        {
            try { Attach(new WaveFormat(16000, 16, 1)); }
            catch (Exception exception) when (exception.HResult == unchecked((int)0x88890008))
            {
                recorder?.Dispose(); recorder = null;
                Attach(null);
            }
        }
        private void OnDefaultDeviceChanged()
        {
            try
            {
                VoiceCaptureException? error;
                lock (gate)
                {
                    if (ended || cancelled || enumerator == null || device == null) return;
                    var available = enumerator.HasDefaultAudioEndpoint(DataFlow.Capture, Role.Console);
                    using var selected = available ? enumerator.GetDefaultAudioEndpoint(DataFlow.Capture, Role.Console) : null;
                    if (selected?.ID == device.ID) return;
                    error = (writer?.Duration ?? TimeSpan.Zero) >= TimeSpan.FromMilliseconds(500) ? null :
                        new VoiceCaptureException(available ? VoiceCaptureError.RecordingFailed : VoiceCaptureError.NoInputDevice);
                }
                End(error);
            }
            catch (Exception exception) { End(MapError(exception)); }
        }
        private void Attach(WaveFormat? format)
        {
            var builder = new WasapiRecorderBuilder().WithDevice(device!).WithSharedMode().WithPollingSync().WithBufferLength(100);
            if (format != null) builder.WithFormat(format);
            recorder = builder.Build();
            var source = recorder.WaveFormat;
            var floating = source.Encoding == WaveFormatEncoding.IeeeFloat || source is WaveFormatExtensible extensible &&
                extensible.SubFormat == new Guid("00000003-0000-0010-8000-00aa00389b71");
            converter = new Pcm16MonoConverter(source.SampleRate, source.Channels, source.BitsPerSample, floating);
            recorder.DataAvailable += OnData;
            recorder.RecordingStopped += (_, args) => _ = Task.Run(() => End(args.Exception == null ? null : MapError(args.Exception)));
            recorder.StartRecording();
        }
        private void OnData(ReadOnlySpan<byte> buffer, AudioClientBufferFlags flags, long devicePosition, long qpcPosition)
        {
            float value;
            bool atLimit;
            lock (gate)
            {
                if (ended || cancelled || writer == null || converter == null) return;
                try
                {
                    value = writer.Append(converter.Convert(buffer, (flags & AudioClientBufferFlags.Silent) != 0));
                    atLimit = writer.AtLimit;
                }
                catch (Exception exception) { _ = Task.Run(() => End(MapError(exception))); return; }
            }
            try { level(value); } catch (Exception exception) { _ = Task.Run(() => End(MapError(exception))); }
            if (atLimit) _ = Task.Run(() => End(null));
        }
        private void End(VoiceCaptureException? error)
        {
            lock (gate)
            {
                if (ended || cancelled || notified) return;
                notified = true;
            }
            StopDevice();
            stopped(error);
        }
        private void StopDevice()
        {
            lock (stoppingGate)
            {
                WasapiRecorder? retired;
                lock (gate)
                {
                    ended = true;
                    timer?.Dispose(); timer = null;
                    writer?.Dispose();
                    retired = recorder; recorder = null;
                }
                // Never hold the data callback lock while NAudio joins its capture thread.
                try { retired?.Dispose(); }
                finally
                {
                    lock (gate) { notifications?.Dispose(); notifications = null; device?.Dispose(); device = null; enumerator?.Dispose(); enumerator = null; }
                }
            }
        }
        public RecordedAudio? Finish()
        {
            StopDevice();
            string? finishedPath;
            TimeSpan duration;
            bool discarded;
            lock (gate)
            {
                duration = writer?.Duration ?? TimeSpan.Zero;
                finishedPath = path;
                path = null;
                discarded = cancelled || duration <= TimeSpan.Zero;
            }
            if (finishedPath == null) return null;
            if (discarded) { File.Delete(finishedPath); return null; }
            return new RecordedAudio(finishedPath, duration, "audio/wav", "voice.wav");
        }
        public void Dispose() => Delete();
        public void Delete()
        {
            lock (gate) { cancelled = true; }
            StopDevice();
            lock (gate)
            {
                if (path != null) { try { File.Delete(path); } catch (IOException) { } path = null; }
            }
        }
    }
}
