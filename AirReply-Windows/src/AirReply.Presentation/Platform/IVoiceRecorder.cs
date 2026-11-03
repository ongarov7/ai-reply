namespace AirReply.Presentation.Platform;

public enum VoiceCaptureError { PermissionDenied, NoInputDevice, RecordingFailed }

public sealed class VoiceCaptureException(VoiceCaptureError error, string? detail = null, Exception? inner = null)
    : Exception(detail ?? error.ToString(), inner)
{
    public VoiceCaptureError Error { get; } = error;
}

/// <summary>A finished recording on disk. The owner deletes the file after upload or cancel.</summary>
public sealed record RecordedAudio(string FilePath, TimeSpan Duration, string ContentType, string FileName);

/// <summary>
/// Microphone capture (WASAPI). The microphone is open only between <see cref="StartAsync"/> and
/// <see cref="StopAsync"/>/<see cref="Cancel"/>. Levels feed the waveform.
/// </summary>
public interface IVoiceRecorder : IAsyncDisposable
{
    /// <summary>Normalized input levels 0…1, raised about 20 times a second on a background thread.</summary>
    event EventHandler<float>? Level;

    /// <summary>Raised when capture stops on its own (device removed, maximum duration reached).</summary>
    event EventHandler<VoiceCaptureException?>? Stopped;

    bool IsRecording { get; }

    /// <summary>Throws <see cref="VoiceCaptureException"/> when the microphone cannot be opened.</summary>
    Task StartAsync(TimeSpan maxDuration, CancellationToken cancellationToken = default);

    /// <summary>Stops and returns the recording, or null when nothing was captured.</summary>
    Task<RecordedAudio?> StopAsync();

    /// <summary>Stops and deletes everything captured.</summary>
    void Cancel();
}
