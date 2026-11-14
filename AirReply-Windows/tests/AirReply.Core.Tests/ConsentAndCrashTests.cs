using System.Net;
using System.Text.Json;
using AirReply.Core.Accounts;
using AirReply.Core.Infrastructure;
using AirReply.Core.Networking;
using Xunit;

namespace AirReply.Core.Tests;

public sealed class ConsentAndCrashTests
{
    [Fact]
    public async Task Consent_refreshes_changed_legal_versions_once_and_uses_windows_platform()
    {
        var configCalls = 0;
        var consentCalls = 0;
        var handler = new RecordingHandler((request, _) => Task.FromResult(request.Url.Split('/').Last() switch
        {
            "config" => RecordingHandler.Json(JsonSerializer.Serialize(new
            { legal = new { terms_version = ++configCalls == 1 ? "old" : "new", privacy_version = "privacy" } })),
            "consents" => ++consentCalls == 1 ? RecordingHandler.Error("INVALID_REQUEST", HttpStatusCode.BadRequest) : RecordingHandler.Json("{}"),
            "me" => RecordingHandler.Json("{\"user\":{\"id\":\"user-1\"},\"legal_consent\":{\"terms_version\":\"new\",\"privacy_version\":\"privacy\"}}"),
            _ => throw new InvalidOperationException("Unexpected endpoint"),
        }));
        using var http = new HttpClient(handler);
        using var api = new ApiClient(http, ApiClientTests.Info(), new InMemorySecretStore(), () => "kk");
        using var account = new AccountStore(api);
        api.Install(ApiClientTests.Payload());
        Assert.False(account.HasConsent);
        await account.AcceptConsentAsync(TestContext.Current.CancellationToken);
        Assert.True(account.HasConsent);
        Assert.False(account.IsConsentSaving);
        Assert.Equal(2, configCalls);
        Assert.Equal(2, consentCalls);
        var consents = handler.Requests.Where(r => r.Url.EndsWith("/consents", StringComparison.Ordinal)).ToArray();
        using var body = JsonDocument.Parse(consents[1].Body!);
        Assert.Equal(new[] { "app_version", "locale", "platform", "privacy_version", "terms_version" }, body.RootElement.EnumerateObject().Select(p => p.Name).Order(StringComparer.Ordinal));
        Assert.Equal("new", body.RootElement.GetProperty("terms_version").GetString());
        Assert.Equal("windows", body.RootElement.GetProperty("platform").GetString());
        Assert.Equal("kk", body.RootElement.GetProperty("locale").GetString());
    }

    [Theory]
    [InlineData(HttpStatusCode.Accepted, false)]
    [InlineData(HttpStatusCode.BadRequest, false)]
    [InlineData(HttpStatusCode.ServiceUnavailable, true)]
    public async Task Crash_reports_are_anonymous_six_field_summaries_and_keep_transient_failures(HttpStatusCode status, bool kept)
    {
        var directory = Path.Combine(Path.GetTempPath(), "aireply-crash-tests-" + Guid.NewGuid().ToString("N"));
        Directory.CreateDirectory(directory);
        try
        {
            var path = Path.Combine(directory, "pending.json");
            var handler = new RecordingHandler((_, _) => Task.FromResult(status == HttpStatusCode.Accepted
                ? RecordingHandler.Json("{\"ok\":true}", status) : RecordingHandler.Error("INTERNAL_ERROR", status)));
            using var http = new HttpClient(handler);
            using var api = new ApiClient(http, ApiClientTests.Info(), new InMemorySecretStore());
            api.Install(ApiClientTests.Payload());
            var reporter = new CrashReporter(api, path);
            reporter.RecordFatal(new InvalidOperationException("PRIVATE USER MESSAGE / ACCESS TOKEN"));
            reporter.RecordFatal(new InvalidOperationException("SECOND PRIVATE TEXT"));
            await reporter.SendPendingAsync(TestContext.Current.CancellationToken);
            var request = Assert.Single(handler.Requests);
            Assert.False(request.Headers.ContainsKey("Authorization"));
            Assert.False(request.Headers.ContainsKey("X-Installation-ID"));
            Assert.DoesNotContain("PRIVATE", request.Body, StringComparison.Ordinal);
            using var body = JsonDocument.Parse(request.Body!);
            Assert.Equal(new[] { "app_version", "code", "count", "kind", "os_version", "platform" }, body.RootElement.EnumerateObject().Select(p => p.Name).Order(StringComparer.Ordinal));
            Assert.Equal("windows", body.RootElement.GetProperty("platform").GetString());
            Assert.Equal(2, body.RootElement.GetProperty("count").GetInt32());
            using var persisted = JsonDocument.Parse(File.ReadAllText(path));
            Assert.Equal(kept ? 1 : 0, persisted.RootElement.GetArrayLength());
        }
        finally { Directory.Delete(directory, recursive: true); }
    }
}
