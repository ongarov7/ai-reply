using System.Globalization;
using System.Net.Http.Headers;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;
using AirReply.Core.Infrastructure;

namespace AirReply.Core.Networking;

/// <summary>Cookie-free transport, persisted rotating sessions and one shared refresh flight.</summary>
public sealed class ApiClient : IDisposable
{
    private readonly HttpClient http;
    private readonly ISecretStore secrets;
    private readonly IClock clock;
    private readonly Func<string> language;
    private readonly DeviceMetadata device;
    private readonly Func<TimeSpan, CancellationToken, Task> delay;
    private readonly Lock gate = new();
    private SessionTokens? session;
    private Task<SessionTokens>? refreshTask;
    private CancellationTokenSource sessionCancellation = new();
    private long generation;

    public ApiClient(HttpClient http, AppInfo info, ISecretStore secrets, Func<string>? language = null,
        IClock? clock = null, DeviceMetadata? device = null,
        Func<TimeSpan, CancellationToken, Task>? delay = null)
    {
        ArgumentNullException.ThrowIfNull(http);
        ArgumentNullException.ThrowIfNull(info);
        ArgumentNullException.ThrowIfNull(secrets);
        this.http = http;
        Info = info;
        this.secrets = secrets;
        this.language = language ?? (() => "ru");
        this.clock = clock ?? SystemClock.Instance;
        this.device = device ?? new();
        this.delay = delay ?? Task.Delay;
        EnvironmentKey = info.BackendBaseUrl.AbsolutePath.TrimEnd('/') switch
        {
            "/sandbox" => "sandbox",
            _ when info.BackendBaseUrl.IsLoopback => $"local-{info.BackendBaseUrl.Port}",
            _ => "production",
        };
        InstallationId = RestoreInstallationId();
        try
        {
            var value = secrets.Read(Key("session"));
            if (value is not null)
            {
                session = JsonSerializer.Deserialize<SessionTokens>(value, ApiJson.Options);
                if (session is not null && (string.IsNullOrEmpty(session.AccessToken) || string.IsNullOrEmpty(session.RefreshToken) || string.IsNullOrEmpty(session.UserId)))
                {
                    session = null;
                }
            }
        }
        catch (Exception ex) when (ex is JsonException or IOException or CryptographicException or UnauthorizedAccessException)
        {
            session = null;
        }
    }

    public AppInfo Info { get; }
    public string EnvironmentKey { get; }
    public string InstallationId { get; }
    public string Language => NormalizeLanguage(language()) ?? "ru";
    public DeviceMetadata Device => device;
    public SessionTokens? Session { get { lock (gate) { return session; } } }
    public event EventHandler<SessionEvent>? SessionChanged;
    public static HttpClient CreateHttpClient() => new(new SocketsHttpHandler
    {
        UseCookies = false,
        AllowAutoRedirect = false,
        AutomaticDecompression = System.Net.DecompressionMethods.All,
    }) { Timeout = Timeout.InfiniteTimeSpan };

    public Uri ApiUri(string path) => new(Info.BackendBaseUrl.AbsoluteUri.TrimEnd('/') + "/" + path.TrimStart('/'));
    public Uri WebsiteUri(string path) => ApiUri(path);
    public static string? NormalizeLanguage(string? value)
    {
        var prefix = value?.Split('-', '_')[0].ToLowerInvariant();
        return prefix is "ru" or "kk" or "en" ? prefix : null;
    }

    private string Key(string key) => $"{EnvironmentKey}/{key}";
    private string RestoreInstallationId()
    {
        var saved = secrets.Read(Key("installation-id"));
        if (saved is not null && saved.Length is >= 8 and <= 64 && saved.All(c => char.IsAsciiLetterOrDigit(c) || c == '-'))
        {
            return saved;
        }
        var value = Guid.NewGuid().ToString();
        secrets.Delete(Key("device-id"));
        secrets.Write(Key("installation-id"), value);
        return value;
    }

    public DeviceDescriptor CurrentDevice()
    {
        lock (gate)
        {
            var id = session?.DeviceId ?? secrets.Read(Key("device-id"));
            if (string.IsNullOrWhiteSpace(id))
            {
                id = Guid.NewGuid().ToString();
                secrets.Write(Key("device-id"), id);
            }
            var timezone = device.Timezone;
            if (timezone is null)
            {
                timezone = TimeZoneInfo.TryConvertWindowsIdToIanaId(TimeZoneInfo.Local.Id, out var iana) ? iana : TimeZoneInfo.Local.Id;
            }
            return new(InstallationId, id, AppInfo.Platform, Info.Version, Info.OsVersion, device.Model, Language, timezone);
        }
    }

    public void Install(SessionPayload payload, string? fallbackEmail = null)
    {
        ArgumentNullException.ThrowIfNull(payload);
        ValidateSessionPayload(payload);
        lock (gate)
        {
            var tokens = Tokens(payload, fallbackEmail, InstallationId);
            Persist(tokens);
            sessionCancellation.Cancel();
            sessionCancellation.Dispose();
            sessionCancellation = new();
            generation++;
            refreshTask = null;
            session = tokens;
        }
        SessionChanged?.Invoke(this, new(payload));
    }

    public void ClearSession(SignOutReason reason = SignOutReason.User)
    {
        lock (gate)
        {
            generation++;
            session = null;
            refreshTask = null;
            sessionCancellation.Cancel();
            sessionCancellation.Dispose();
            sessionCancellation = new();
            secrets.Delete(Key("session"));
        }
        SessionChanged?.Invoke(this, new(SignOutReason: reason));
    }

    public async Task SignOutAsync(CancellationToken ct = default)
    {
        var snapshot = Session;
        // Clear first: a slow logout must never let a refresh restore the local account.
        ClearSession();
        if (snapshot is null) { return; }
        try
        {
            await SendAttemptAsync<JsonElement>(new(HttpMethod.Post, "/api/v1/auth/logout")
            {
                Authenticated = false,
                SendInstallationId = true,
                Timeout = TimeSpan.FromSeconds(10),
                Body = new { refresh_token = snapshot.RefreshToken },
            }, null, ct).ConfigureAwait(false);
        }
        catch (ApiException) { /* Local sign-out succeeds even if the server is unreachable. */ }
    }

    public async Task<T> SendAsync<T>(ApiRequest request, CancellationToken ct = default)
    {
        ArgumentNullException.ThrowIfNull(request);
        if (!request.Authenticated)
        {
            return await SendAttemptAsync<T>(request, null, ct).ConfigureAwait(false);
        }
        SessionTokens tokens;
        CancellationToken sessionToken;
        lock (gate)
        {
            tokens = session ?? throw new ApiException(ApiFailure.SignedOut);
            sessionToken = sessionCancellation.Token;
        }
        using var linked = CancellationTokenSource.CreateLinkedTokenSource(ct, sessionToken);
        if (tokens.NeedsRefresh(clock.Now))
        {
            tokens = await RefreshedAsync(tokens.AccessToken, linked.Token).ConfigureAwait(false);
        }
        try
        {
            return await SendAttemptAsync<T>(request, tokens.AccessToken, linked.Token).ConfigureAwait(false);
        }
        catch (ApiException ex) when (ex.IsExpiredSession)
        {
            tokens = await RefreshedAsync(tokens.AccessToken, linked.Token).ConfigureAwait(false);
            try
            {
                return await SendAttemptAsync<T>(request, tokens.AccessToken, linked.Token).ConfigureAwait(false);
            }
            catch (ApiException retry) when (retry.IsExpiredSession)
            {
                ClearSession(SignOutReason.TokenRejected);
                throw new ApiException(ApiFailure.SignedOut);
            }
        }
    }

    public Task SendAsync(ApiRequest request, CancellationToken ct = default) => SendAsync<JsonElement>(request, ct);

    private async Task<SessionTokens> RefreshedAsync(string stale, CancellationToken ct)
    {
        Task<SessionTokens> flight;
        lock (gate)
        {
            var current = session ?? throw new ApiException(ApiFailure.SignedOut);
            if (current.AccessToken != stale && !current.NeedsRefresh(clock.Now)) { return current; }
            if (refreshTask is null || refreshTask.IsCompleted)
            {
                refreshTask = PerformRefreshAsync(current, generation, sessionCancellation.Token);
            }
            flight = refreshTask;
        }
        try { return await flight.WaitAsync(ct).ConfigureAwait(false); }
        catch (OperationCanceledException ex) { throw new ApiException(ApiFailure.Cancelled, innerException: ex); }
    }

    private async Task<SessionTokens> PerformRefreshAsync(SessionTokens snapshot, long sessionGeneration, CancellationToken ct)
    {
        var started = clock.Now;
        for (var attempt = 0; ; attempt++)
        {
            SessionPayload payload;
            try
            {
                payload = await SendAttemptAsync<SessionPayload>(new(HttpMethod.Post, "/api/v1/auth/refresh")
                {
                    Authenticated = false,
                    SendInstallationId = true,
                    Body = new { refresh_token = snapshot.RefreshToken, device = CurrentDevice() },
                }, null, ct).ConfigureAwait(false);
                ValidateSessionPayload(payload);
            }
            catch (ApiException ex) when (ex.Failure == ApiFailure.Server && ex.Status is 401 or 403)
            {
                lock (gate)
                {
                    if (generation == sessionGeneration) { ClearSession(SignOutReason.RefreshRefused); }
                }
                throw new ApiException(ApiFailure.SignedOut);
            }
            catch (ApiException ex) when (attempt == 0 && IsAmbiguous(ex) && clock.Now - started < TimeSpan.FromSeconds(48))
            {
                // The backend permits one lost-answer replay of this same refresh token within 60 seconds.
                await delay(TimeSpan.FromSeconds(2), ct).ConfigureAwait(false);
                continue;
            }
            SessionTokens result;
            lock (gate)
            {
                if (generation != sessionGeneration || ct.IsCancellationRequested)
                {
                    throw new ApiException(ApiFailure.SignedOut);
                }
                result = Tokens(payload, snapshot.Email, snapshot.InstallationId);
                try { Persist(result); }
                catch (Exception ex) when (ex is IOException or UnauthorizedAccessException or CryptographicException)
                {
                    // Keep the usable rotated tokens in memory; retry persistence on the next rotation.
                }
                session = result;
            }
            SessionChanged?.Invoke(this, new(payload));
            return result;
        }
    }

    private static bool IsAmbiguous(ApiException ex) => ex.Failure == ApiFailure.InvalidResponse ||
        (ex.Failure == ApiFailure.Offline && ex.MaybeDelivered) || (ex.Failure == ApiFailure.Server && ex.Status >= 500);

    private SessionTokens Tokens(SessionPayload payload, string? email, string installation) => new(payload.AccessToken,
        payload.RefreshToken, clock.Now.AddSeconds(Math.Max(60, payload.ExpiresIn)), payload.RefreshExpiresAt,
        payload.DeviceId, payload.User.Id, payload.User.Email ?? email, installation);

    private void Persist(SessionTokens tokens)
    {
        secrets.Write(Key("session"), JsonSerializer.Serialize(tokens, ApiJson.Options));
        secrets.Write(Key("device-id"), tokens.DeviceId);
    }
    private static void ValidateSessionPayload(SessionPayload payload)
    {
        if (string.IsNullOrWhiteSpace(payload.AccessToken) || string.IsNullOrWhiteSpace(payload.RefreshToken) ||
            string.IsNullOrWhiteSpace(payload.User?.Id) || string.IsNullOrWhiteSpace(payload.DeviceId))
        {
            throw new ApiException(ApiFailure.InvalidResponse, 200) { MaybeDelivered = true };
        }
    }

    private async Task<T> SendAttemptAsync<T>(ApiRequest request, string? accessToken, CancellationToken ct)
    {
        using var timeout = CancellationTokenSource.CreateLinkedTokenSource(ct);
        timeout.CancelAfter(request.Timeout);
        using var message = new HttpRequestMessage(request.Method, ApiUri(request.Path));
        message.Headers.Accept.Add(new MediaTypeWithQualityHeaderValue("application/json"));
        message.Headers.AcceptLanguage.ParseAdd(Language);
        message.Headers.TryAddWithoutValidation("User-Agent", Info.UserAgent);
        message.Headers.TryAddWithoutValidation("X-Request-ID", Guid.NewGuid().ToString());
        if (accessToken is not null) { message.Headers.Authorization = new("Bearer", accessToken); }
        if (request.SendInstallationId ?? request.Authenticated) { message.Headers.Add("X-Installation-ID", InstallationId); }
        if (request.IdempotencyKey is not null) { message.Headers.Add("Idempotency-Key", request.IdempotencyKey); }
        if (request.ContentFactory is not null) { message.Content = request.ContentFactory(); }
        else if (request.Body is not null)
        {
            message.Content = new StringContent(JsonSerializer.Serialize(request.Body, ApiJson.Options), Encoding.UTF8, "application/json");
        }
        try
        {
            using var response = await http.SendAsync(message, HttpCompletionOption.ResponseHeadersRead, timeout.Token).ConfigureAwait(false);
            var text = await response.Content.ReadAsStringAsync(timeout.Token).ConfigureAwait(false);
            var status = (int)response.StatusCode;
            var requestId = response.Headers.TryGetValues("X-Request-ID", out var ids) ? ids.FirstOrDefault() : null;
            if (status is >= 300 and < 400)
            {
                throw new ApiException(ApiFailure.InvalidResponse, status) { MaybeDelivered = true, RequestId = requestId };
            }
            if (!response.IsSuccessStatusCode)
            {
                try
                {
                    using var document = JsonDocument.Parse(text);
                    var error = document.RootElement.GetProperty("error");
                    var code = error.GetProperty("code").GetString();
                    if (string.IsNullOrWhiteSpace(code)) { throw new JsonException(); }
                    var details = error.TryGetProperty("details", out var data) && data.ValueKind == JsonValueKind.Object
                        ? data.EnumerateObject().ToDictionary(p => p.Name, p => p.Value.Clone(), StringComparer.Ordinal)
                        : new Dictionary<string, JsonElement>(StringComparer.Ordinal);
                    var failure = new ApiException(ApiFailure.Server, status, code) { Details = details, RequestId = requestId, MaybeDelivered = true };
                    TimeSpan? retryAfter = null;
                    if (response.Headers.TryGetValues("Retry-After", out var values) && double.TryParse(values.FirstOrDefault(), CultureInfo.InvariantCulture, out var seconds))
                    {
                        retryAfter = TimeSpan.FromSeconds(Math.Clamp(seconds, 0, 86400));
                    }
                    else if (failure.DetailInt("retry_after_seconds") is int retrySeconds) { retryAfter = TimeSpan.FromSeconds(Math.Clamp(retrySeconds, 0, 86400)); }
                    throw new ApiException(ApiFailure.Server, status, code) { Details = details, RequestId = requestId, MaybeDelivered = true, RetryAfter = retryAfter };
                }
                catch (Exception ex) when (ex is JsonException or KeyNotFoundException or InvalidOperationException)
                {
                    throw new ApiException(ApiFailure.InvalidResponse, status, innerException: ex) { MaybeDelivered = true, RequestId = requestId };
                }
            }
            try { return JsonSerializer.Deserialize<T>(text, ApiJson.Options) ?? throw new JsonException(); }
            catch (JsonException ex) { throw new ApiException(ApiFailure.InvalidResponse, status, innerException: ex) { MaybeDelivered = true, RequestId = requestId }; }
        }
        catch (OperationCanceledException ex)
        {
            throw new ApiException(ct.IsCancellationRequested ? ApiFailure.Cancelled : ApiFailure.Offline, innerException: ex)
            { MaybeDelivered = !ct.IsCancellationRequested };
        }
        catch (HttpRequestException ex)
        {
            throw new ApiException(ApiFailure.Offline, innerException: ex)
            { MaybeDelivered = ex.HttpRequestError is not (HttpRequestError.NameResolutionError or HttpRequestError.ConnectionError or HttpRequestError.SecureConnectionError) };
        }
    }

    public void Dispose()
    {
        sessionCancellation.Cancel();
        sessionCancellation.Dispose();
    }
}
