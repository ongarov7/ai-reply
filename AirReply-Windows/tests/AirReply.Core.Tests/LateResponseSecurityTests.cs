using System.Net;
using System.Text.Json;
using AirReply.Core.Accounts;
using AirReply.Core.Authentication;
using AirReply.Core.Generation;
using AirReply.Core.Infrastructure;
using AirReply.Core.Installations;
using AirReply.Core.Networking;
using Xunit;

namespace AirReply.Core.Tests;

public sealed class LateResponseSecurityTests
{
    [Theory]
    [InlineData("cancel")]
    [InlineData("signout")]
    [InlineData("api-signout")]
    [InlineData("another-email")]
    [InlineData("new-challenge")]
    [InlineData("same-user-relogin")]
    public async Task Late_otp_response_cannot_restore_abandoned_flow(string action)
    {
        using var fixture = new Fixture();
        if (action == "same-user-relogin") fixture.Api.Install(Payload("user-1", "initial", 20));
        await fixture.Auth.RequestCodeAsync("first@example.test", TestContext.Current.CancellationToken);
        var started = Signal(); var release = new TaskCompletionSource<HttpResponseMessage>(TaskCreationOptions.RunContinuationsAsynchronously);
        fixture.Handler.Reply = (request, _) =>
        {
            if (request.Url.EndsWith("/verify", StringComparison.Ordinal)) { started.SetResult(); return release.Task; }
            return Task.FromResult(DefaultResponse(request));
        };
        using var cancellation = CancellationTokenSource.CreateLinkedTokenSource(TestContext.Current.CancellationToken);
        var verifying = fixture.Auth.VerifyAsync("1234", cancellation.Token);
        await started.Task.WaitAsync(TimeSpan.FromSeconds(5), TestContext.Current.CancellationToken);
        switch (action)
        {
            case "cancel": cancellation.Cancel(); break;
            case "signout": await fixture.Auth.SignOutAsync(TestContext.Current.CancellationToken); break;
            case "api-signout": fixture.Api.ClearSession(); break;
            case "another-email": fixture.Auth.UseAnotherEmail(); break;
            case "new-challenge": await fixture.Auth.RequestCodeAsync("second@example.test", TestContext.Current.CancellationToken); break;
            case "same-user-relogin":
                fixture.Api.ClearSession(); fixture.Api.Install(Payload("user-1", "new-login", 99)); break;
        }
        // Deliberately ignore the transport cancellation: this is a response that arrives after the user's action.
        release.SetResult(RecordingHandler.Json(SessionJson("user-1", "abandoned", 1)));
        var error = await Assert.ThrowsAsync<ApiException>(() => verifying);
        Assert.Equal(ApiFailure.Cancelled, error.Failure);
        if (action == "same-user-relogin")
        {
            Assert.Equal("new-login", fixture.Api.Session?.AccessToken);
            Assert.Contains("new-login", fixture.Secrets.Read("production/session"), StringComparison.Ordinal);
            Assert.Equal(99, fixture.Account.Balance?.RemainingTotal);
        }
        else { Assert.Null(fixture.Api.Session); Assert.Null(fixture.Secrets.Read("production/session")); }
        Assert.Equal(action == "new-challenge" ? "second@example.test" : null, fixture.Auth.Challenge?.Email);
        Assert.DoesNotContain(fixture.Handler.Requests, request => request.Url.EndsWith("/installations", StringComparison.Ordinal));
    }

    [Fact]
    public async Task Older_otp_request_cannot_replace_newer_email_challenge()
    {
        using var fixture = new Fixture();
        var started = Signal(); var release = new TaskCompletionSource<HttpResponseMessage>(TaskCreationOptions.RunContinuationsAsynchronously);
        fixture.Handler.Reply = (request, _) =>
        {
            if (request.Body?.Contains("first@example.test", StringComparison.Ordinal) == true) { started.SetResult(); return release.Task; }
            return Task.FromResult(DefaultResponse(request));
        };
        var old = fixture.Auth.RequestCodeAsync("first@example.test", TestContext.Current.CancellationToken);
        await started.Task;
        await fixture.Auth.RequestCodeAsync("second@example.test", TestContext.Current.CancellationToken);
        release.SetResult(RecordingHandler.Json("{\"masked_email\":\"first***\",\"code_length\":4}"));
        var error = await Assert.ThrowsAsync<ApiException>(() => old);
        Assert.Equal(ApiFailure.Cancelled, error.Failure);
        Assert.Equal("second@example.test", fixture.Auth.Challenge?.Email);
        Assert.Equal("second@example.test", fixture.Auth.LastEmail);
    }

    [Fact]
    public async Task Concurrent_duplicate_verify_does_not_discard_first_valid_signin()
    {
        using var fixture = new Fixture();
        await fixture.Auth.RequestCodeAsync("first@example.test", TestContext.Current.CancellationToken);
        var started = Signal(); var release = new TaskCompletionSource<HttpResponseMessage>(TaskCreationOptions.RunContinuationsAsynchronously);
        fixture.Handler.Reply = (request, _) =>
        {
            if (request.Url.EndsWith("/verify", StringComparison.Ordinal)) { started.SetResult(); return release.Task; }
            return Task.FromResult(DefaultResponse(request));
        };
        var first = fixture.Auth.VerifyAsync("1234", TestContext.Current.CancellationToken);
        await started.Task;
        var duplicate = await Assert.ThrowsAsync<AuthException>(() => fixture.Auth.VerifyAsync("1234", TestContext.Current.CancellationToken));
        Assert.Equal(AuthErrorKind.RateLimited, duplicate.Kind);
        release.SetResult(RecordingHandler.Json(SessionJson("user-1", "valid-login", 20)));
        Assert.True(await first);
        Assert.Equal("valid-login", fixture.Api.Session?.AccessToken);
        Assert.Single(fixture.Handler.Requests, request => request.Url.EndsWith("/verify", StringComparison.Ordinal));
    }

    [Theory]
    [InlineData(false)]
    [InlineData(true)]
    public async Task Late_generation_cannot_update_another_session_even_for_same_user(bool sameUser)
    {
        using var fixture = new Fixture(); fixture.Api.Install(Payload("user-1", "original", 20));
        var started = Signal(); var release = new TaskCompletionSource<HttpResponseMessage>(TaskCreationOptions.RunContinuationsAsynchronously);
        fixture.Handler.Reply = (request, _) =>
        {
            if (request.Url.EndsWith("/compose", StringComparison.Ordinal)) { started.SetResult(); return release.Task; }
            return Task.FromResult(DefaultResponse(request));
        };
        var generating = fixture.Generator.ComposeAsync("Напиши ответ", false, null, Guid.NewGuid().ToString(), GenerationOptions.None, TestContext.Current.CancellationToken);
        await started.Task;
        fixture.Api.ClearSession(); fixture.Api.Install(Payload(sameUser ? "user-1" : "user-2", "new-session", 99));
        release.SetResult(RecordingHandler.Json("{\"text\":\"Old private answer\",\"usage\":{\"remaining_total\":1}}"));
        var error = await Assert.ThrowsAsync<GenerationException>(() => generating);
        Assert.Equal(GenerationErrorKind.SignedOut, error.Kind);
        Assert.Equal("new-session", fixture.Api.Session?.AccessToken);
        Assert.Equal(99, fixture.Account.Balance?.RemainingTotal);
    }

    [Fact]
    public async Task Session_change_during_registration_never_submits_generation_as_new_account()
    {
        using var fixture = new Fixture(); fixture.Api.Install(Payload("user-1", "original", 20));
        var started = Signal(); var release = new TaskCompletionSource<HttpResponseMessage>(TaskCreationOptions.RunContinuationsAsynchronously);
        fixture.Handler.Reply = (request, _) =>
        {
            if (request.Url.EndsWith("/installations", StringComparison.Ordinal)) { started.SetResult(); return release.Task; }
            return Task.FromResult(DefaultResponse(request));
        };
        var generating = fixture.Generator.ComposeAsync("Private account one draft", false, null, Guid.NewGuid().ToString(), GenerationOptions.None, TestContext.Current.CancellationToken);
        await started.Task;
        fixture.Api.Install(Payload("user-2", "new-session", 99));
        release.SetResult(RecordingHandler.Json("{\"attached\":true}"));
        var error = await Assert.ThrowsAsync<GenerationException>(() => generating);
        Assert.Equal(GenerationErrorKind.SignedOut, error.Kind);
        Assert.DoesNotContain(fixture.Handler.Requests, request => request.Url.EndsWith("/compose", StringComparison.Ordinal));
        Assert.Equal(99, fixture.Account.Balance?.RemainingTotal);
    }

    [Fact]
    public async Task Late_usage_does_not_overwrite_newer_balance_and_forces_authoritative_refresh()
    {
        using var fixture = new Fixture(); fixture.Api.Install(Payload("user-1", "original", 20));
        var started = Signal(); var release = new TaskCompletionSource<HttpResponseMessage>(TaskCreationOptions.RunContinuationsAsynchronously);
        fixture.Handler.Reply = (request, _) =>
        {
            if (request.Url.EndsWith("/compose", StringComparison.Ordinal)) { started.SetResult(); return release.Task; }
            if (request.Url.EndsWith("/me", StringComparison.Ordinal)) return Task.FromResult(RecordingHandler.Json("{\"user\":{\"id\":\"user-1\"},\"usage\":{\"remaining_total\":54}}"));
            return Task.FromResult(DefaultResponse(request));
        };
        var generating = fixture.Generator.ComposeAsync("Напиши ответ", false, null, Guid.NewGuid().ToString(), GenerationOptions.None, TestContext.Current.CancellationToken);
        await started.Task;
        fixture.Account.Apply(new UsageDto { RemainingTotal = 55 });
        release.SetResult(RecordingHandler.Json("{\"text\":\"Answer\",\"usage\":{\"remaining_total\":1}}"));
        Assert.Equal("Answer", (await generating).Text);
        Assert.Equal(54, fixture.Account.Balance?.RemainingTotal);
        Assert.Single(fixture.Handler.Requests, request => request.Url.EndsWith("/me", StringComparison.Ordinal));
    }

    [Fact]
    public async Task Explicitly_cancelled_generation_never_applies_usage()
    {
        using var fixture = new Fixture(); fixture.Api.Install(Payload("user-1", "original", 20));
        var started = Signal(); var release = new TaskCompletionSource<HttpResponseMessage>(TaskCreationOptions.RunContinuationsAsynchronously);
        fixture.Handler.Reply = (request, _) =>
        {
            if (request.Url.EndsWith("/compose", StringComparison.Ordinal)) { started.SetResult(); return release.Task; }
            return Task.FromResult(DefaultResponse(request));
        };
        using var cancel = CancellationTokenSource.CreateLinkedTokenSource(TestContext.Current.CancellationToken);
        var generating = fixture.Generator.ComposeAsync("Напиши ответ", false, null, Guid.NewGuid().ToString(), GenerationOptions.None, cancel.Token);
        await started.Task; cancel.Cancel();
        release.SetResult(RecordingHandler.Json("{\"text\":\"Cancelled\",\"usage\":{\"remaining_total\":1}}"));
        var error = await Assert.ThrowsAsync<ApiException>(() => generating);
        Assert.Equal(ApiFailure.Cancelled, error.Failure);
        Assert.Equal(20, fixture.Account.Balance?.RemainingTotal);
    }

    private static TaskCompletionSource Signal() => new(TaskCreationOptions.RunContinuationsAsynchronously);
    private static SessionPayload Payload(string user, string access, int remaining) => JsonSerializer.Deserialize<SessionPayload>(SessionJson(user, access, remaining), ApiJson.Options)!;
    private static string SessionJson(string user, string access, int remaining) => JsonSerializer.Serialize(new
    {
        access_token = access, refresh_token = "refresh-" + access, expires_in = 900, device_id = "device-" + user,
        user = new { id = user, email = user + "@example.test" }, usage = new { remaining_total = remaining },
    });
    private static HttpResponseMessage DefaultResponse(CapturedRequest request) => request.Url.Split('/').Last() switch
    {
        "request" => RecordingHandler.Json("{\"masked_email\":\"u***\",\"code_length\":4,\"resend_after\":0}"),
        "installations" => RecordingHandler.Json("{\"attached\":true}"),
        "compose" => RecordingHandler.Json("{\"text\":\"Answer\"}"),
        _ => RecordingHandler.Json("{}"),
    };
    private sealed class Fixture : IDisposable
    {
        private readonly HttpClient http;
        public RecordingHandler Handler { get; } = new((request, _) => Task.FromResult(DefaultResponse(request)));
        public InMemorySecretStore Secrets { get; } = new();
        public ApiClient Api { get; }
        public AccountStore Account { get; }
        public InstallationRegistrar Registrar { get; }
        public AuthSession Auth { get; }
        public Generator Generator { get; }
        public Fixture()
        {
            http = new(Handler); Api = new(http, ApiClientTests.Info(), Secrets); Account = new(Api);
            Registrar = new(Api, Account); Auth = new(Api, Account, Registrar); Generator = new(Api, Account, Registrar);
        }
        public void Dispose() { Registrar.Dispose(); Account.Dispose(); Api.Dispose(); http.Dispose(); }
    }
}
