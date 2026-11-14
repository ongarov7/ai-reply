using System.Net;
using System.Text.Json;
using AirReply.Core.Accounts;
using AirReply.Core.Authentication;
using AirReply.Core.Generation;
using AirReply.Core.Infrastructure;
using AirReply.Core.Installations;
using AirReply.Core.Networking;
using AirReply.Core.Voice;
using Xunit;

namespace AirReply.Core.Tests;

public sealed class AccountAndGenerationTests
{
    [Fact]
    public async Task Signin_uses_canonical_device_and_registers_before_generation()
    {
        using var fixture = new Fixture();
        fixture.Handler.Reply = (request, _) => Task.FromResult(request.Url.Split('/').Last() switch
        {
            "request" => RecordingHandler.Json("{\"masked_email\":\"u***@example.com\",\"resend_after\":32,\"code_length\":4}"),
            "verify" => RecordingHandler.Json(ApiClientTests.SessionJson()),
            "installations" => RecordingHandler.Json("{\"attached\":true}"),
            "compose" => RecordingHandler.Json("{\"text\":\"Готовый ответ\"}"),
            _ => RecordingHandler.Json("{}"),
        });
        await fixture.Auth.RequestCodeAsync(" user@example.com ", TestContext.Current.CancellationToken);
        Assert.Equal("user@example.com", fixture.Auth.Challenge?.Email);
        await fixture.Auth.VerifyAsync("03 84", TestContext.Current.CancellationToken);
        await fixture.Generator.ComposeAsync("привет", false, null, Guid.NewGuid().ToString(), GenerationOptions.None, TestContext.Current.CancellationToken);
        var requests = fixture.Handler.Requests;
        var installationIndex = requests.FindIndex(r => r.Url.EndsWith("/installations", StringComparison.Ordinal));
        var generationIndex = requests.FindIndex(r => r.Url.EndsWith("/compose", StringComparison.Ordinal));
        Assert.InRange(installationIndex, 2, generationIndex - 1);
        using var verify = JsonDocument.Parse(requests[1].Body!);
        var device = verify.RootElement.GetProperty("device");
        Assert.Equal(new[] { "app_version", "device_id", "installation_id", "locale", "model", "os_version", "platform", "timezone" }, Keys(device));
        Assert.Equal("windows", device.GetProperty("platform").GetString());
        Assert.Equal(fixture.Api.InstallationId, device.GetProperty("installation_id").GetString());
        Assert.Equal("server-device", fixture.Api.CurrentDevice().DeviceId);
        using var installation = JsonDocument.Parse(requests[installationIndex].Body!);
        Assert.Equal(new[] { "app_build", "app_version", "device_model", "installation_id", "locale", "manufacturer", "notifications_enabled", "os_name", "os_version", "platform", "timezone" }, Keys(installation.RootElement));
        Assert.Equal("Bearer access", requests[installationIndex].Headers["Authorization"]);
    }

    [Fact]
    public async Task Invalid_email_code_and_resend_cooldown_do_not_send_requests()
    {
        using var fixture = new Fixture();
        var invalidEmail = await Assert.ThrowsAsync<AuthException>(() => fixture.Auth.RequestCodeAsync("invalid", TestContext.Current.CancellationToken));
        Assert.Equal(AuthErrorKind.InvalidEmail, invalidEmail.Kind);
        Assert.Empty(fixture.Handler.Requests);
        await fixture.Auth.RequestCodeAsync("user@example.com", TestContext.Current.CancellationToken);
        var invalidCode = await Assert.ThrowsAsync<AuthException>(() => fixture.Auth.VerifyAsync("１２３４", TestContext.Current.CancellationToken));
        Assert.Equal(AuthErrorKind.InvalidCode, invalidCode.Kind);
        var cooldown = await Assert.ThrowsAsync<AuthException>(() => fixture.Auth.ResendCodeAsync(TestContext.Current.CancellationToken));
        Assert.Equal(AuthErrorKind.Cooldown, cooldown.Kind);
        Assert.Single(fixture.Handler.Requests);
    }

    [Fact]
    public async Task Ordinary_reply_and_compose_have_exact_keys_and_preserve_idempotency()
    {
        using var fixture = new Fixture();
        fixture.Api.Install(ApiClientTests.Payload());
        const string key = "b51d1627-0965-47d0-96d1-e60c76857fea";
        await fixture.Generator.ReplyAsync("Встретимся завтра?", Recipient.Work, "", "de", key, GenerationOptions.None, TestContext.Current.CancellationToken);
        await fixture.Generator.ComposeAsync("  Напиши другу  ", true, "en-US", key, GenerationOptions.None, TestContext.Current.CancellationToken);
        var reply = fixture.Handler.Requests.Single(r => r.Url.EndsWith("/reply", StringComparison.Ordinal));
        using var replyJson = JsonDocument.Parse(reply.Body!);
        Assert.Equal(new[] { "app_version", "language", "platform", "source_text", "template_id" }, Keys(replyJson.RootElement));
        Assert.Equal("work", replyJson.RootElement.GetProperty("template_id").GetString());
        Assert.Equal(key, reply.Headers["Idempotency-Key"]);
        var compose = fixture.Handler.Requests.Single(r => r.Url.EndsWith("/compose", StringComparison.Ordinal));
        using var composeJson = JsonDocument.Parse(compose.Body!);
        Assert.Equal(new[] { "app_version", "instruction", "language", "platform", "regenerate" }, Keys(composeJson.RootElement));
        Assert.Equal("Напиши другу", composeJson.RootElement.GetProperty("instruction").GetString());
        Assert.Equal(key, compose.Headers["Idempotency-Key"]);
    }

    [Fact]
    public async Task Context_and_extended_length_are_explicit_and_server_gated()
    {
        using var fixture = new Fixture();
        fixture.Api.Install(ApiClientTests.Payload());
        var options = new GenerationOptions(new("Компания", ["Документ"]), 600);
        var unavailable = await Assert.ThrowsAsync<GenerationException>(() => fixture.Generator.ComposeAsync("Привет", false, null, Guid.NewGuid().ToString(), options, TestContext.Current.CancellationToken));
        Assert.Equal(GenerationErrorKind.ContextUnavailable, unavailable.Kind);
        Assert.Empty(fixture.Handler.Requests);
        fixture.Handler.Reply = (request, _) => Task.FromResult(request.Url.EndsWith("/config", StringComparison.Ordinal)
            ? RecordingHandler.Json("{\"features\":{\"desktop_context_generation\":true,\"sender_profile\":true}}")
            : RecordingHandler.Json("{\"text\":\"готово\"}"));
        await fixture.Account.RefreshConfigAsync(ct: TestContext.Current.CancellationToken);
        await fixture.Generator.ComposeAsync("Привет", false, "kk-KZ", Guid.NewGuid().ToString(), options, TestContext.Current.CancellationToken);
        var request = fixture.Handler.Requests.Single(r => r.Url.EndsWith("/compose", StringComparison.Ordinal));
        using var json = JsonDocument.Parse(request.Body!);
        Assert.Equal(600, json.RootElement.GetProperty("output_char_limit").GetInt32());
        Assert.Equal("Компания", json.RootElement.GetProperty("context").GetProperty("profile").GetString());
        Assert.Equal("kk", json.RootElement.GetProperty("input_language").GetString());
        Assert.False(json.RootElement.TryGetProperty("template_id", out _));
    }

    [Fact]
    public async Task Limits_count_unicode_runes_and_context_bytes_locally()
    {
        using var fixture = new Fixture();
        fixture.Api.Install(ApiClientTests.Payload());
        await fixture.Generator.ReplyAsync(string.Concat(Enumerable.Repeat("😀", 400)), Recipient.Client, null, null, Guid.NewGuid().ToString(), GenerationOptions.None, TestContext.Current.CancellationToken);
        var before = fixture.Handler.Requests.Count;
        var tooLong = await Assert.ThrowsAsync<GenerationException>(() => fixture.Generator.ComposeAsync(new string('a', 401), false, null,
            Guid.NewGuid().ToString(), GenerationOptions.None, TestContext.Current.CancellationToken));
        Assert.Equal(GenerationErrorKind.InstructionTooLong, tooLong.Kind);
        Assert.Equal(before, fixture.Handler.Requests.Count);
    }

    [Theory]
    [InlineData("PRO_DEVICE_LIMIT_REACHED", GenerationErrorKind.ProDeviceLimit)]
    [InlineData("PRO_DEVICE_REVOKED", GenerationErrorKind.ProDeviceRevoked)]
    [InlineData("PRO_DEVICE_IDENTITY_REQUIRED", GenerationErrorKind.ProDeviceIdentity)]
    [InlineData("AI_OUTPUT_LIMIT_EXCEEDED", GenerationErrorKind.OutputTooLong)]
    public async Task Generation_errors_have_specific_kinds_without_refresh_or_signout(string code, GenerationErrorKind expected)
    {
        using var fixture = new Fixture();
        fixture.Api.Install(ApiClientTests.Payload());
        fixture.Handler.Reply = (request, _) => Task.FromResult(request.Url.EndsWith("/installations", StringComparison.Ordinal)
            ? RecordingHandler.Json("{\"attached\":true}") : RecordingHandler.Error(code, HttpStatusCode.Forbidden));
        var error = await Assert.ThrowsAsync<GenerationException>(() => fixture.Generator.ComposeAsync("Привет", false, null,
            Guid.NewGuid().ToString(), GenerationOptions.None, TestContext.Current.CancellationToken));
        Assert.Equal(expected, error.Kind);
        Assert.NotNull(fixture.Api.Session);
        Assert.DoesNotContain(fixture.Handler.Requests, r => r.Url.EndsWith("/auth/refresh", StringComparison.Ordinal));
    }

    [Fact]
    public async Task In_progress_conflict_retries_twice_with_same_key_and_used_marks_key_spent()
    {
        using var fixture = new Fixture();
        fixture.Api.Install(ApiClientTests.Payload());
        fixture.Handler.Reply = (request, _) => Task.FromResult(request.Url.EndsWith("/installations", StringComparison.Ordinal)
            ? RecordingHandler.Json("{\"attached\":true}")
            : RecordingHandler.Json("{\"error\":{\"code\":\"CONFLICT\",\"details\":{\"field\":\"idempotency_key\",\"reason\":\"in_progress\"}}}", HttpStatusCode.Conflict));
        var error = await Assert.ThrowsAsync<GenerationException>(() => fixture.Generator.ComposeAsync("Привет", false, null,
            "same-key", GenerationOptions.None, TestContext.Current.CancellationToken));
        Assert.Equal(GenerationErrorKind.Server, error.Kind);
        Assert.Equal(2, fixture.Delays.Count);
        var requests = fixture.Handler.Requests.Where(r => r.Url.EndsWith("/compose", StringComparison.Ordinal)).ToArray();
        Assert.Equal(3, requests.Length);
        Assert.All(requests, r => Assert.Equal("same-key", r.Headers["Idempotency-Key"]));
        fixture.Handler.Reply = (_, _) => Task.FromResult(RecordingHandler.Json("{\"error\":{\"code\":\"CONFLICT\",\"details\":{\"field\":\"idempotency_key\",\"reason\":\"used\"}}}", HttpStatusCode.Conflict));
        var spent = await Assert.ThrowsAsync<GenerationException>(() => fixture.Generator.ComposeAsync("Привет", false, null,
            "same-key", GenerationOptions.None, TestContext.Current.CancellationToken));
        Assert.True(spent.KeySpent);
    }

    [Theory]
    [InlineData("context", "budget", GenerationErrorKind.ContextTooLarge)]
    [InlineData("context.profile", "max_characters", GenerationErrorKind.ContextTooLarge)]
    [InlineData("context", "feature unavailable", GenerationErrorKind.ContextUnavailable)]
    public void Context_failures_are_not_generic_server_errors(string field, string reason, GenerationErrorKind expected)
    {
        using var json = JsonDocument.Parse(JsonSerializer.Serialize(new { field, reason }));
        var details = json.RootElement.EnumerateObject().ToDictionary(p => p.Name, p => p.Value.Clone());
        var error = Generator.Map(new(ApiFailure.Server, 400, "INVALID_REQUEST") { Details = details });
        Assert.Equal(expected, error.Kind);
    }

    [Fact]
    public void Balance_handles_unlimited_before_zero_and_falls_back_to_credit_buckets()
    {
        using var fixture = new Fixture();
        fixture.Account.Apply(new MeResponse { User = new() { Id = "u" }, Usage = new() { Unlimited = true, RemainingTotal = 0 } });
        Assert.True(fixture.Account.Balance?.IsUnlimited);
        Assert.False(fixture.Account.Balance?.IsExhausted);
        fixture.Account.Apply(new UsageDto { RemainingToday = 2, PurchasedCredits = 3, AdminCredits = 4, Welcome = new() { Remaining = 5 }, Pass = new() { Remaining = 6 } });
        Assert.Equal(20, fixture.Account.Balance?.RemainingTotal);
    }

    [Fact]
    public async Task Foreign_payment_handoff_host_is_rejected()
    {
        using var fixture = new Fixture();
        fixture.Api.Install(ApiClientTests.Payload());
        fixture.Handler.Reply = (_, _) => Task.FromResult(RecordingHandler.Json("{\"url\":\"https://evil.example/payment#handoff=secret\"}"));
        var uri = await fixture.Account.PlansUriAsync(TestContext.Current.CancellationToken);
        Assert.Equal("https://ai-reply.kz/payment", uri.AbsoluteUri);
        using var body = JsonDocument.Parse(Assert.Single(fixture.Handler.Requests).Body!);
        Assert.Equal("user-1", body.RootElement.GetProperty("expected_user_id").GetString());
    }

    [Fact]
    public async Task Logout_omits_bearer_keeps_identity_and_clears_session()
    {
        using var fixture = new Fixture();
        fixture.Api.Install(ApiClientTests.Payload());
        var id = fixture.Api.InstallationId;
        await fixture.Auth.SignOutAsync(TestContext.Current.CancellationToken);
        var request = Assert.Single(fixture.Handler.Requests);
        Assert.EndsWith("/auth/logout", request.Url, StringComparison.Ordinal);
        Assert.False(request.Headers.ContainsKey("Authorization"));
        Assert.Equal(id, request.Headers["X-Installation-ID"]);
        Assert.Null(fixture.Api.Session);
        Assert.Equal("server-device", fixture.Api.CurrentDevice().DeviceId);
    }

    [Fact]
    public async Task Transcription_sends_wav_multipart_and_rejects_short_audio_locally()
    {
        using var fixture = new Fixture();
        fixture.Api.Install(ApiClientTests.Payload());
        var transcriber = new Transcriber(fixture.Api, fixture.Registrar);
        var tooShort = await Assert.ThrowsAsync<VoiceException>(() => transcriber.TranscribeAsync([1, 2], TimeSpan.FromMilliseconds(299), ["ru"], ct: TestContext.Current.CancellationToken));
        Assert.Equal(VoiceErrorKind.TooShort, tooShort.Kind);
        Assert.Empty(fixture.Handler.Requests);
        fixture.Handler.Reply = (request, _) => Task.FromResult(request.Url.EndsWith("/installations", StringComparison.Ordinal)
            ? RecordingHandler.Json("{\"attached\":true}") : RecordingHandler.Json("{\"text\":\"Распознано\"}"));
        var result = await transcriber.TranscribeAsync("RIFF-WAVE"u8.ToArray(), TimeSpan.FromSeconds(2), ["kk-KZ", "ru", "kk", "en"], "recording-id", TestContext.Current.CancellationToken);
        Assert.Equal("Распознано", result);
        var request = fixture.Handler.Requests.Single(r => r.Url.EndsWith("/transcribe", StringComparison.Ordinal));
        Assert.StartsWith("multipart/form-data; boundary=", request.ContentType, StringComparison.Ordinal);
        Assert.Contains("voice.wav", request.Body, StringComparison.Ordinal);
        Assert.Contains("audio/wav", request.Body, StringComparison.Ordinal);
        Assert.Contains("kk,ru,en", request.Body, StringComparison.Ordinal);
        Assert.Contains("recording-id", request.Body, StringComparison.Ordinal);
        Assert.False(request.Headers.ContainsKey("Idempotency-Key"));
        Assert.Equal(fixture.Api.InstallationId, request.Headers["X-Installation-ID"]);
    }

    private static string[] Keys(JsonElement value) => value.EnumerateObject().Select(p => p.Name).Order(StringComparer.Ordinal).ToArray();
    private sealed class Fixture : IDisposable
    {
        private readonly HttpClient http;
        public RecordingHandler Handler { get; } = new((request, _) => Task.FromResult(request.Url.Split('/').Last() switch
        {
            "installations" => RecordingHandler.Json("{\"attached\":true}"),
            "reply" => RecordingHandler.Json("{\"reply\":\"Готовый ответ\"}"),
            "compose" => RecordingHandler.Json("{\"text\":\"Готовый ответ\"}"),
            _ => RecordingHandler.Json("{}"),
        }));
        public ApiClient Api { get; }
        public AccountStore Account { get; }
        public InstallationRegistrar Registrar { get; }
        public AuthSession Auth { get; }
        public Generator Generator { get; }
        public List<TimeSpan> Delays { get; } = [];
        public Fixture()
        {
            http = new(Handler);
            Api = new(http, ApiClientTests.Info(), new InMemorySecretStore());
            Account = new(Api);
            Registrar = new(Api, Account);
            Auth = new(Api, Account, Registrar);
            Generator = new(Api, Account, Registrar, (span, _) => { Delays.Add(span); return Task.CompletedTask; });
        }
        public void Dispose() { Registrar.Dispose(); Account.Dispose(); Api.Dispose(); http.Dispose(); }
    }
}
