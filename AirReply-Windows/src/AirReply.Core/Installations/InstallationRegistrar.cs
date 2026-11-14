using System.Security.Cryptography;
using System.Text;
using System.Text.Json;
using AirReply.Core.Accounts;
using AirReply.Core.Infrastructure;
using AirReply.Core.Networking;

namespace AirReply.Core.Installations;

public enum InstallationOutcome { Registered, Unchanged, Failed, Unsupported, NotSignedIn }

public sealed class InstallationRegistrar(ApiClient api, AccountStore account, IClock? clock = null) : IDisposable
{
    private readonly SemaphoreSlim sync = new(1, 1);
    private readonly IClock clock = clock ?? SystemClock.Instance;
    private string? confirmedAccount;
    private string? confirmedFingerprint;
    private DateTimeOffset? confirmedAt;
    private DateTimeOffset? nextAttempt;
    private string? blockedFingerprint;
    private bool unsupported;
    public string InstallationId => api.InstallationId;
    public bool IsConfirmedForCurrentSession => api.Session is { } session && confirmedAccount == session.UserId && session.InstallationId == InstallationId;

    public async Task<InstallationOutcome> SyncAsync(bool force = false, CancellationToken ct = default)
    {
        await sync.WaitAsync(ct).ConfigureAwait(false);
        try
        {
            var session = api.Session;
            if (session is null) { return InstallationOutcome.NotSignedIn; }
            if (unsupported || (account.IsConfigLoaded && !account.Config.Feature("installations"))) { return InstallationOutcome.Unsupported; }
            var device = api.CurrentDevice();
            var body = new
            {
                installation_id = InstallationId, platform = AppInfo.Platform, app_version = api.Info.Version,
                app_build = api.Info.Build, os_name = "Windows", os_version = api.Info.OsVersion,
                device_model = device.Model, manufacturer = api.Device.Manufacturer, locale = api.Language,
                timezone = device.Timezone, notifications_enabled = false,
            };
            var fingerprint = Convert.ToHexString(SHA256.HashData(Encoding.UTF8.GetBytes(JsonSerializer.Serialize(body, ApiJson.Options) + "|account:" + session.UserId)));
            if (!force && (fingerprint == blockedFingerprint || nextAttempt > clock.Now)) { return InstallationOutcome.Failed; }
            if (!force && fingerprint == confirmedFingerprint && clock.Now - confirmedAt < TimeSpan.FromHours(24)) { return InstallationOutcome.Unchanged; }
            try
            {
                var result = await api.SendAsync<InstallationResponse>(new(HttpMethod.Post, "/api/v1/installations")
                { Body = body, Timeout = TimeSpan.FromSeconds(10) }, ct).ConfigureAwait(false);
                if (!result.Attached || api.Session?.UserId != session.UserId || api.Session?.InstallationId != session.InstallationId)
                {
                    return InstallationOutcome.Failed;
                }
                confirmedAccount = session.UserId;
                confirmedFingerprint = fingerprint;
                confirmedAt = clock.Now;
                blockedFingerprint = null;
                nextAttempt = null;
                return InstallationOutcome.Registered;
            }
            catch (ApiException ex) when (ex.Failure != ApiFailure.Cancelled)
            {
                if (ex.Status == 404) { unsupported = true; return InstallationOutcome.Unsupported; }
                if (ex.Status is 400 or 409) { blockedFingerprint = fingerprint; }
                nextAttempt = clock.Now.Add(ex.RetryAfter > TimeSpan.FromSeconds(60) ? ex.RetryAfter.Value : TimeSpan.FromSeconds(60));
                return InstallationOutcome.Failed;
            }
        }
        finally { sync.Release(); }
    }
    public async Task WaitForSessionAsync(TimeSpan? max = null, CancellationToken ct = default)
    {
        if (IsConfirmedForCurrentSession) { return; }
        using var timeout = CancellationTokenSource.CreateLinkedTokenSource(ct);
        timeout.CancelAfter(max ?? TimeSpan.FromSeconds(10));
        try { await SyncAsync(ct: timeout.Token).ConfigureAwait(false); }
        catch (OperationCanceledException) when (!ct.IsCancellationRequested) { }
        catch (ApiException ex) when (ex.Failure == ApiFailure.Cancelled && !ct.IsCancellationRequested) { }
    }
    public void Dispose() => sync.Dispose();
    private sealed class InstallationResponse { public bool Attached { get; init; } }
}
