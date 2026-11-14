using System.Globalization;
using System.Net.Http.Headers;
using System.Text;
using AirReply.Core.Generation;
using AirReply.Core.Installations;
using AirReply.Core.Networking;

namespace AirReply.Core.Voice;

public enum VoiceErrorKind { TooShort, TooLarge, Offline, SignedOut, Unavailable, LimitReached, RateLimited,
    ConsentRequired, AccountDisabled, ProDeviceLimit, ProDeviceIdentity, ProDeviceRevoked, Server }
public sealed class VoiceException(VoiceErrorKind kind, Exception? innerException = null) : Exception(kind.ToString(), innerException)
{
    public VoiceErrorKind Kind { get; } = kind;
    public string? ServerCode { get; init; }
}

public sealed class Transcriber(ApiClient api, InstallationRegistrar registrar)
{
    public async Task<string> TranscribeAsync(byte[] wav, TimeSpan duration, IEnumerable<string> languages,
        string? requestId = null, CancellationToken ct = default)
    {
        ArgumentNullException.ThrowIfNull(wav);
        ArgumentNullException.ThrowIfNull(languages);
        var milliseconds = (long)Math.Round(duration.TotalMilliseconds, MidpointRounding.AwayFromZero);
        if (milliseconds < 300) { throw new VoiceException(VoiceErrorKind.TooShort); }
        if (wav.Length > 4 * 1024 * 1024) { throw new VoiceException(VoiceErrorKind.TooLarge); }
        var id = requestId ?? Guid.NewGuid().ToString();
        var preferred = languages.Select(ApiClient.NormalizeLanguage).OfType<string>().Distinct(StringComparer.Ordinal).Take(3).ToArray();
        await registrar.WaitForSessionAsync(ct: ct).ConfigureAwait(false);
        var healed = false;
        while (true)
        {
            try
            {
                var result = await api.SendAsync<Response>(new(HttpMethod.Post, "/api/v1/ai/transcribe")
                {
                    Timeout = TimeSpan.FromSeconds(75),
                    ContentFactory = () => Multipart(wav, id, Math.Min(milliseconds, 40500), preferred),
                }, ct).ConfigureAwait(false);
                return result.Text ?? throw new VoiceException(VoiceErrorKind.Server);
            }
            catch (ApiException ex) when (ex.Code == "PRO_DEVICE_IDENTITY_REQUIRED" && !healed &&
                api.Session?.InstallationId == api.InstallationId && !registrar.IsConfirmedForCurrentSession)
            {
                healed = true;
                if (await registrar.SyncAsync(force: true, ct).ConfigureAwait(false) is not (InstallationOutcome.Registered or InstallationOutcome.Unchanged))
                { throw Map(ex); }
            }
            catch (ApiException ex) when (ex.Failure != ApiFailure.Cancelled) { throw Map(ex); }
        }
    }
    private static MultipartFormDataContent Multipart(byte[] wav, string requestId, long milliseconds, string[] languages)
    {
        var content = new MultipartFormDataContent("AIReply-" + Guid.NewGuid().ToString("N"));
        content.Add(new StringContent(requestId, Encoding.UTF8), "request_id");
        content.Add(new StringContent(milliseconds.ToString(CultureInfo.InvariantCulture), Encoding.UTF8), "duration_ms");
        if (languages.Length > 0) { content.Add(new StringContent(string.Join(',', languages), Encoding.UTF8), "languages"); }
        content.Add(new StringContent("instruction", Encoding.UTF8), "purpose");
        var audio = new ByteArrayContent(wav);
        audio.Headers.ContentType = new MediaTypeHeaderValue("audio/wav");
        content.Add(audio, "audio", "voice.wav");
        return content;
    }
    private static VoiceException Map(ApiException ex)
    {
        var kind = ex.Code switch
        {
            "NOT_FOUND" => VoiceErrorKind.Unavailable,
            "VOICE_LIMIT_REACHED" => VoiceErrorKind.LimitReached,
            "RATE_LIMITED" => VoiceErrorKind.RateLimited,
            "INVALID_REQUEST" when ex.DetailString("field") == "audio" && ex.DetailString("reason") == "too_short" => VoiceErrorKind.TooShort,
            _ => Generator.Map(ex).Kind switch
            {
                GenerationErrorKind.SignedOut => VoiceErrorKind.SignedOut,
                GenerationErrorKind.Offline => VoiceErrorKind.Offline,
                GenerationErrorKind.ConsentRequired => VoiceErrorKind.ConsentRequired,
                GenerationErrorKind.AccountDisabled => VoiceErrorKind.AccountDisabled,
                GenerationErrorKind.ProDeviceLimit => VoiceErrorKind.ProDeviceLimit,
                GenerationErrorKind.ProDeviceIdentity => VoiceErrorKind.ProDeviceIdentity,
                GenerationErrorKind.ProDeviceRevoked => VoiceErrorKind.ProDeviceRevoked,
                _ => VoiceErrorKind.Server,
            },
        };
        return new(kind, ex) { ServerCode = ex.Code };
    }
    private sealed class Response { public string? Text { get; init; } }
}
