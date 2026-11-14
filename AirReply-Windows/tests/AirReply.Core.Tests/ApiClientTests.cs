using System.Net;
using System.Text;
using System.Text.Json;
using AirReply.Core.Infrastructure;
using AirReply.Core.Networking;
using Xunit;

namespace AirReply.Core.Tests;

public sealed class ApiClientTests
{
    [Fact]
    public async Task Sandbox_path_headers_and_null_omission_are_preserved()
    {
        var handler = new RecordingHandler((_, _) => Task.FromResult(RecordingHandler.Json("{}")));
        using var http = new HttpClient(handler);
        using var api = new ApiClient(http, Info("https://ai-reply.kz/sandbox"), new InMemorySecretStore(), () => "kk");
        await api.SendAsync(new(HttpMethod.Post, "/api/v1/config") { Authenticated = false, Body = new { value = "мәтін", omitted = (string?)null } }, TestContext.Current.CancellationToken);
        var request = Assert.Single(handler.Requests);
        Assert.Equal("https://ai-reply.kz/sandbox/api/v1/config", request.Url);
        Assert.Equal("kk", request.Headers["Accept-Language"]);
        Assert.Contains("AIReply-Windows/", request.Headers["User-Agent"], StringComparison.Ordinal);
        Assert.True(Guid.TryParse(request.Headers["X-Request-ID"], out _));
        Assert.False(request.Headers.ContainsKey("Authorization"));
        Assert.False(request.Headers.ContainsKey("X-Installation-ID"));
        Assert.Equal("{\"value\":\"мәтін\"}", request.Body);
    }

    [Fact]
    public async Task Retry_after_header_wins_and_html_is_invalid_response()
    {
        var handler = new RecordingHandler((_, _) =>
        {
            var response = RecordingHandler.Json("{\"error\":{\"code\":\"RATE_LIMITED\",\"details\":{\"retry_after_seconds\":\"8\"}}}", HttpStatusCode.TooManyRequests);
            response.Headers.Add("Retry-After", "42");
            return Task.FromResult(response);
        });
        using var http = new HttpClient(handler);
        using var api = new ApiClient(http, Info(), new InMemorySecretStore());
        var ex = await Assert.ThrowsAsync<ApiException>(() => api.SendAsync(new(HttpMethod.Get, "/api/v1/config") { Authenticated = false }, TestContext.Current.CancellationToken));
        Assert.Equal(ApiFailure.Server, ex.Failure);
        Assert.Equal(TimeSpan.FromSeconds(42), ex.RetryAfter);
        handler.Reply = (_, _) => Task.FromResult(RecordingHandler.Json("<html>gateway</html>", HttpStatusCode.BadGateway));
        var invalid = await Assert.ThrowsAsync<ApiException>(() => api.SendAsync(new(HttpMethod.Get, "/api/v1/config") { Authenticated = false }, TestContext.Current.CancellationToken));
        Assert.Equal(ApiFailure.InvalidResponse, invalid.Failure);
        Assert.Equal(502, invalid.Status);
    }

    [Fact]
    public async Task Authenticated_request_without_session_does_not_hit_network()
    {
        var handler = new RecordingHandler();
        using var http = new HttpClient(handler);
        using var api = new ApiClient(http, Info(), new InMemorySecretStore());
        var error = await Assert.ThrowsAsync<ApiException>(() => api.SendAsync(new(HttpMethod.Get, "/api/v1/me"), TestContext.Current.CancellationToken));
        Assert.Equal(ApiFailure.SignedOut, error.Failure);
        Assert.Empty(handler.Requests);
    }

    [Fact]
    public async Task Redirects_are_invalid_even_with_a_valid_error_envelope()
    {
        var handler = new RecordingHandler((_, _) => Task.FromResult(RecordingHandler.Error("UNAUTHORIZED", HttpStatusCode.Found)));
        using var http = new HttpClient(handler);
        using var api = new ApiClient(http, Info(), new InMemorySecretStore());
        var error = await Assert.ThrowsAsync<ApiException>(() => api.SendAsync(new(HttpMethod.Get, "/api/v1/config")
        { Authenticated = false }, TestContext.Current.CancellationToken));
        Assert.Equal(ApiFailure.InvalidResponse, error.Failure);
        Assert.Equal(302, error.Status);
    }

    [Fact]
    public async Task Eight_concurrent_expired_requests_share_one_persisted_refresh()
    {
        var clock = new FakeClock();
        var secrets = new InMemorySecretStore();
        var started = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var release = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var refreshCount = 0;
        var handler = new RecordingHandler(async (request, _) =>
        {
            if (request.Url.EndsWith("/auth/refresh", StringComparison.Ordinal))
            {
                Interlocked.Increment(ref refreshCount);
                started.TrySetResult();
                await release.Task;
                return RecordingHandler.Json(SessionJson("new-access", "new-refresh"));
            }
            Assert.Equal("Bearer new-access", request.Headers["Authorization"]);
            Assert.Contains("new-refresh", secrets.Read("production/session"), StringComparison.Ordinal);
            return RecordingHandler.Json("{}");
        });
        using var http = new HttpClient(handler);
        using var api = new ApiClient(http, Info(), secrets, clock: clock);
        api.Install(Payload());
        clock.Now = clock.Now.AddMinutes(20);
        var requests = Enumerable.Range(0, 8).Select(_ => api.SendAsync(new(HttpMethod.Get, "/api/v1/me"), TestContext.Current.CancellationToken)).ToArray();
        await started.Task.WaitAsync(TimeSpan.FromSeconds(5), TestContext.Current.CancellationToken);
        release.SetResult();
        await Task.WhenAll(requests);
        Assert.Equal(1, refreshCount);
        Assert.Equal("new-refresh", api.Session?.RefreshToken);
        var refresh = handler.Requests.Single(r => r.Url.EndsWith("/auth/refresh", StringComparison.Ordinal));
        Assert.False(refresh.Headers.ContainsKey("Authorization"));
        Assert.Equal(api.InstallationId, refresh.Headers["X-Installation-ID"]);
        using var json = JsonDocument.Parse(refresh.Body!);
        Assert.Equal("refresh", json.RootElement.GetProperty("refresh_token").GetString());
        Assert.Equal(api.InstallationId, json.RootElement.GetProperty("device").GetProperty("installation_id").GetString());
    }

    [Fact]
    public async Task Lost_refresh_answer_is_replayed_once_with_same_token()
    {
        var clock = new FakeClock();
        var refreshCount = 0;
        var waits = new List<TimeSpan>();
        var handler = new RecordingHandler((request, _) =>
        {
            if (!request.Url.EndsWith("/auth/refresh", StringComparison.Ordinal)) { return Task.FromResult(RecordingHandler.Json("{}")); }
            if (++refreshCount == 1) { throw new HttpRequestException(HttpRequestError.ResponseEnded, "lost"); }
            return Task.FromResult(RecordingHandler.Json(SessionJson("new", "rotated")));
        });
        using var http = new HttpClient(handler);
        using var api = new ApiClient(http, Info(), new InMemorySecretStore(), clock: clock,
            delay: (span, _) => { waits.Add(span); return Task.CompletedTask; });
        api.Install(Payload());
        clock.Now = clock.Now.AddMinutes(20);
        await api.SendAsync(new(HttpMethod.Get, "/api/v1/me"), TestContext.Current.CancellationToken);
        Assert.Equal(2, refreshCount);
        Assert.Equal(TimeSpan.FromSeconds(2), Assert.Single(waits));
        var attempts = handler.Requests.Where(r => r.Url.EndsWith("/auth/refresh", StringComparison.Ordinal)).ToArray();
        Assert.Equal(attempts[0].Body, attempts[1].Body);
        Assert.NotEqual(attempts[0].Headers["X-Request-ID"], attempts[1].Headers["X-Request-ID"]);
    }

    [Theory]
    [InlineData(HttpRequestError.NameResolutionError, 1)]
    [InlineData(HttpRequestError.ResponseEnded, 2)]
    public async Task Offline_refresh_keeps_session_and_limits_ambiguous_replay(HttpRequestError failure, int attempts)
    {
        var clock = new FakeClock();
        var handler = new RecordingHandler((_, _) => throw new HttpRequestException(failure, "offline"));
        using var http = new HttpClient(handler);
        using var api = new ApiClient(http, Info(), new InMemorySecretStore(), clock: clock, delay: (_, _) => Task.CompletedTask);
        api.Install(Payload());
        clock.Now = clock.Now.AddMinutes(20);
        var error = await Assert.ThrowsAsync<ApiException>(() => api.SendAsync(new(HttpMethod.Get, "/api/v1/me"), TestContext.Current.CancellationToken));
        Assert.Equal(ApiFailure.Offline, error.Failure);
        Assert.NotNull(api.Session);
        Assert.Equal(attempts, handler.Requests.Count);
    }

    [Theory]
    [InlineData(HttpStatusCode.Unauthorized, "UNAUTHORIZED")]
    [InlineData(HttpStatusCode.Forbidden, "ACCOUNT_DISABLED")]
    public async Task Refused_refresh_clears_session(HttpStatusCode status, string code)
    {
        var clock = new FakeClock();
        var store = new InMemorySecretStore();
        var handler = new RecordingHandler((_, _) => Task.FromResult(RecordingHandler.Error(code, status)));
        using var http = new HttpClient(handler);
        using var api = new ApiClient(http, Info(), store, clock: clock);
        api.Install(Payload());
        clock.Now = clock.Now.AddMinutes(20);
        var error = await Assert.ThrowsAsync<ApiException>(() => api.SendAsync(new(HttpMethod.Get, "/api/v1/me"), TestContext.Current.CancellationToken));
        Assert.Equal(ApiFailure.SignedOut, error.Failure);
        Assert.Null(api.Session);
        Assert.Null(store.Read("production/session"));
    }

    [Fact]
    public async Task Rejected_fresh_token_clears_session_without_second_refresh()
    {
        var handler = new RecordingHandler((request, _) => Task.FromResult(request.Url.EndsWith("/auth/refresh", StringComparison.Ordinal)
            ? RecordingHandler.Json(SessionJson("new", "rotated")) : RecordingHandler.Error("UNAUTHORIZED", HttpStatusCode.Unauthorized)));
        using var http = new HttpClient(handler);
        using var api = new ApiClient(http, Info(), new InMemorySecretStore());
        api.Install(Payload());
        var error = await Assert.ThrowsAsync<ApiException>(() => api.SendAsync(new(HttpMethod.Get, "/api/v1/me"), TestContext.Current.CancellationToken));
        Assert.Equal(ApiFailure.SignedOut, error.Failure);
        Assert.Null(api.Session);
        Assert.Single(handler.Requests, r => r.Url.EndsWith("/auth/refresh", StringComparison.Ordinal));
    }

    [Fact]
    public async Task Signout_during_refresh_cannot_persist_rotated_tokens()
    {
        var started = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var release = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var clock = new FakeClock();
        var store = new InMemorySecretStore();
        var handler = new RecordingHandler(async (_, _) =>
        { started.SetResult(); await release.Task; return RecordingHandler.Json(SessionJson("new", "rotated")); });
        using var http = new HttpClient(handler);
        using var api = new ApiClient(http, Info(), store, clock: clock);
        api.Install(Payload());
        clock.Now = clock.Now.AddMinutes(20);
        var request = api.SendAsync(new(HttpMethod.Get, "/api/v1/me"), TestContext.Current.CancellationToken);
        await started.Task;
        api.ClearSession();
        release.SetResult();
        await Assert.ThrowsAsync<ApiException>(() => request);
        Assert.Null(api.Session);
        Assert.Null(store.Read("production/session"));
    }

    [Fact]
    public void Environments_are_isolated_and_invalid_installation_resets_device()
    {
        var store = new InMemorySecretStore();
        using var http = new HttpClient(new RecordingHandler());
        using var production = new ApiClient(http, Info(), store);
        production.Install(Payload());
        using var sandbox = new ApiClient(http, Info("https://ai-reply.kz/sandbox"), store);
        Assert.Null(sandbox.Session);
        Assert.NotEqual(production.InstallationId, sandbox.InstallationId);
        store.Write("sandbox/installation-id", "! invalid");
        store.Write("sandbox/device-id", "stale-device");
        using var repaired = new ApiClient(http, Info("https://ai-reply.kz/sandbox"), store);
        Assert.True(Guid.TryParse(repaired.InstallationId, out _));
        Assert.Null(store.Read("sandbox/device-id"));
    }

    internal static AppInfo Info(string url = "https://ai-reply.kz") => new(new Uri(url), "1.0.0", "1", "10.0.26100", "x64");
    internal static string SessionJson(string access = "access", string refresh = "refresh") =>
        JsonSerializer.Serialize(new { access_token = access, refresh_token = refresh, expires_in = 900, device_id = "server-device", user = new { id = "user-1", email = "user@example.com" } });
    internal static SessionPayload Payload() => JsonSerializer.Deserialize<SessionPayload>(SessionJson(), ApiJson.Options)!;
}

internal sealed class FakeClock : IClock
{
    public DateTimeOffset Now { get; set; } = new(2026, 11, 14, 10, 0, 0, TimeSpan.Zero);
}
internal sealed record CapturedRequest(string Url, string? Body, Dictionary<string, string> Headers, string? ContentType);
internal sealed class RecordingHandler(Func<CapturedRequest, CancellationToken, Task<HttpResponseMessage>>? reply = null) : HttpMessageHandler
{
    private readonly Lock gate = new();
    public List<CapturedRequest> Requests { get; } = [];
    public Func<CapturedRequest, CancellationToken, Task<HttpResponseMessage>> Reply { get; set; } = reply ?? ((_, _) => Task.FromResult(Json("{}")));
    protected override async Task<HttpResponseMessage> SendAsync(HttpRequestMessage request, CancellationToken cancellationToken)
    {
        var captured = new CapturedRequest(request.RequestUri!.AbsoluteUri,
            request.Content is null ? null : await request.Content.ReadAsStringAsync(cancellationToken),
            request.Headers.ToDictionary(header => header.Key, header => string.Join(",", header.Value), StringComparer.OrdinalIgnoreCase),
            request.Content?.Headers.ContentType?.ToString());
        lock (gate) { Requests.Add(captured); }
        return await Reply(captured, cancellationToken);
    }
    public static HttpResponseMessage Json(string body, HttpStatusCode status = HttpStatusCode.OK) => new(status)
    { Content = new StringContent(body, Encoding.UTF8, "application/json") };
    public static HttpResponseMessage Error(string code, HttpStatusCode status) => Json(JsonSerializer.Serialize(new { error = new { code } }), status);
}
