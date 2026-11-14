using System.Net;
using System.Text;
using System.Text.Json;
using AirReply.Core.Accounts;
using AirReply.Core.Infrastructure;
using AirReply.Core.Networking;
using Xunit;

namespace AirReply.Core.Tests;

public sealed class SessionActionRaceTests
{
    [Theory]
    [InlineData(false)]
    [InlineData(true)]
    public async Task Late_retry_unauthorized_cannot_clear_new_login(bool sameUser)
    {
        using var fixture = new Fixture();
        fixture.Api.Install(Payload("first", "old"));
        var attempts = 0;
        fixture.Handler.Reply = (request, _) =>
        {
            if (request.Url.EndsWith("/refresh", StringComparison.Ordinal))
                return Task.FromResult(RecordingHandler.Json(SessionJson("first", "rotated")));
            if (++attempts == 1) return Task.FromResult(RecordingHandler.Error("UNAUTHORIZED", HttpStatusCode.Unauthorized));
            // Switch after HttpClient returns headers, while SendAttemptAsync reads the old error body.
            return Task.FromResult(new HttpResponseMessage(HttpStatusCode.Unauthorized)
            {
                Content = new CallbackContent("{\"error\":{\"code\":\"UNAUTHORIZED\"}}",
                    () => fixture.Api.Install(Payload(sameUser ? "first" : "second", "new-login"))),
            });
        };
        var error = await Assert.ThrowsAsync<ApiException>(() => fixture.Api.SendAsync(new(HttpMethod.Get, "/api/v1/me"), TestContext.Current.CancellationToken));
        Assert.Equal(ApiFailure.SignedOut, error.Failure);
        Assert.Equal("new-login", fixture.Api.Session?.AccessToken);
        Assert.Equal(sameUser ? "first" : "second", fixture.Api.Session?.UserId);
        Assert.Contains("new-login", fixture.Secrets.Read("production/session"), StringComparison.Ordinal);
    }

    [Fact]
    public async Task Bound_action_refuses_replacement_session_before_sending()
    {
        using var fixture = new Fixture();
        fixture.Api.Install(Payload("first", "old"));
        var expectedGeneration = fixture.Api.SessionGeneration;
        fixture.Api.Install(Payload("first", "new-login"));
        var error = await Assert.ThrowsAsync<ApiException>(() => fixture.Api.SendAsync(
            new(HttpMethod.Post, "/api/v1/me/consents"), expectedGeneration, TestContext.Current.CancellationToken));
        Assert.Equal(ApiFailure.SignedOut, error.Failure);
        Assert.Empty(fixture.Handler.Requests);
        Assert.Equal("new-login", fixture.Api.Session?.AccessToken);
    }

    [Theory]
    [InlineData(false, false)]
    [InlineData(false, true)]
    [InlineData(true, false)]
    [InlineData(true, true)]
    public async Task Consent_cannot_cross_session_while_loading_or_retrying_legal_versions(bool retry, bool sameUser)
    {
        using var fixture = new Fixture();
        fixture.Api.Install(Payload("first", "old"));
        var started = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var release = new TaskCompletionSource<HttpResponseMessage>(TaskCreationOptions.RunContinuationsAsynchronously);
        var configs = 0;
        fixture.Handler.Reply = (request, _) =>
        {
            if (request.Url.EndsWith("/config", StringComparison.Ordinal))
            {
                if (++configs == (retry ? 2 : 1)) { started.SetResult(); return release.Task; }
                return Task.FromResult(RecordingHandler.Json(LegalConfig));
            }
            return Task.FromResult(RecordingHandler.Error("INVALID_REQUEST", HttpStatusCode.BadRequest));
        };
        var accepting = fixture.Account.AcceptConsentAsync(TestContext.Current.CancellationToken);
        await started.Task.WaitAsync(TimeSpan.FromSeconds(5), TestContext.Current.CancellationToken);
        fixture.Api.ClearSession();
        fixture.Api.Install(Payload(sameUser ? "first" : "second", "new-login"));
        release.SetResult(RecordingHandler.Json(LegalConfig));
        var error = await Assert.ThrowsAsync<ApiException>(() => accepting);
        Assert.Equal(ApiFailure.SignedOut, error.Failure);
        var consentRequests = fixture.Handler.Requests.Where(request => request.Url.EndsWith("/consents", StringComparison.Ordinal)).ToArray();
        Assert.Equal(retry ? 1 : 0, consentRequests.Length);
        Assert.All(consentRequests, request => Assert.Equal("Bearer old", request.Headers["Authorization"]));
        Assert.Equal("new-login", fixture.Api.Session?.AccessToken);
        Assert.False(fixture.Account.IsConsentSaving);
    }

    [Fact]
    public async Task Late_consent_success_does_not_refresh_or_accept_new_account()
    {
        using var fixture = new Fixture();
        fixture.Api.Install(Payload("first", "old"));
        fixture.Handler.Reply = (request, _) => Task.FromResult(request.Url.EndsWith("/config", StringComparison.Ordinal)
            ? RecordingHandler.Json(LegalConfig)
            : new HttpResponseMessage(HttpStatusCode.OK)
            {
                Content = new CallbackContent("{}", () => fixture.Api.Install(Payload("second", "new-login"))),
            });
        var error = await Assert.ThrowsAsync<ApiException>(() => fixture.Account.AcceptConsentAsync(TestContext.Current.CancellationToken));
        Assert.Equal(ApiFailure.SignedOut, error.Failure);
        Assert.DoesNotContain(fixture.Handler.Requests, request => request.Url.EndsWith("/me", StringComparison.Ordinal));
        Assert.Equal("second", fixture.Account.Me?.User.Id);
        Assert.False(fixture.Account.HasConsent);
        Assert.Equal("new-login", fixture.Api.Session?.AccessToken);
    }

    private const string LegalConfig = "{\"legal\":{\"terms_version\":\"2026-11\",\"privacy_version\":\"2026-11\"}}";
    private static SessionPayload Payload(string user, string access) => JsonSerializer.Deserialize<SessionPayload>(SessionJson(user, access), ApiJson.Options)!;
    private static string SessionJson(string user, string access) => JsonSerializer.Serialize(new
    {
        access_token = access, refresh_token = "refresh-" + access, expires_in = 900, device_id = "device-" + user,
        user = new { id = user, email = user + "@example.test" }, usage = new { remaining_total = 20 },
    });

    private sealed class CallbackContent(string body, Action callback) : HttpContent
    {
        protected override Task SerializeToStreamAsync(Stream stream, TransportContext? context)
        {
            callback();
            return stream.WriteAsync(Encoding.UTF8.GetBytes(body)).AsTask();
        }
        protected override bool TryComputeLength(out long length) { length = Encoding.UTF8.GetByteCount(body); return true; }
    }

    private sealed class Fixture : IDisposable
    {
        private readonly HttpClient http;
        public RecordingHandler Handler { get; } = new();
        public InMemorySecretStore Secrets { get; } = new();
        public ApiClient Api { get; }
        public AccountStore Account { get; }
        public Fixture()
        {
            http = new(Handler); Api = new(http, ApiClientTests.Info(), Secrets); Account = new(Api);
        }
        public void Dispose() { Account.Dispose(); Api.Dispose(); http.Dispose(); }
    }
}
