using AirReply.Core.Infrastructure;
using AirReply.Core.Networking;

namespace AirReply.Core.Accounts;

public sealed record BalanceSummary(string PlanCode, string PlanName, bool IsUnlimited, int RemainingTotal,
    int RemainingToday, int DailyLimit, int? WelcomeRemaining, int? WelcomeTotal, int Credits, DateTimeOffset? ResetsAt)
{
    public bool IsExhausted => !IsUnlimited && RemainingTotal <= 0;
}

public sealed class AccountStore : IDisposable
{
    private readonly ApiClient api;
    private readonly IClock clock;
    private DateTimeOffset? configLoaded;
    private DateTimeOffset? meLoaded;
    public AccountStore(ApiClient api, IClock? clock = null)
    {
        ArgumentNullException.ThrowIfNull(api);
        this.api = api;
        this.clock = clock ?? SystemClock.Instance;
        api.SessionChanged += OnSessionChanged;
    }
    public ServerConfig Config { get; private set; } = new();
    public bool IsConfigLoaded => configLoaded is not null;
    public MeResponse? Me { get; private set; }
    public bool? HasConsent => Me is null ? null : Me.LegalConsent is not null;
    public bool IsConsentSaving { get; private set; }
    public BalanceSummary? Balance => Me?.Usage is not UsageDto usage ? null : new(
        Me.Subscription?.Plan.Code ?? "free", Me.Subscription?.Plan.DisplayName(api.Language) ?? "", usage.Unlimited,
        usage.EffectiveRemaining, usage.RemainingToday, usage.DailyLimit, usage.Welcome?.Remaining,
        usage.Welcome?.Total, usage.EffectiveCredits, usage.ResetsAt);
    public event EventHandler? Changed;
    public event EventHandler<string?>? AccountChanged;

    private void OnSessionChanged(object? sender, SessionEvent e)
    {
        if (e.Payload is not null) { Apply(e.Payload.ToMe()); }
        else { Clear(); }
    }
    public void Apply(MeResponse me)
    {
        ArgumentNullException.ThrowIfNull(me);
        var oldId = Me?.User.Id;
        Me = me;
        meLoaded = clock.Now;
        if (oldId != me.User.Id) { AccountChanged?.Invoke(this, me.User.Id); }
        Changed?.Invoke(this, EventArgs.Empty);
    }
    public void Apply(UsageDto usage)
    {
        if (Me is not null) { Me.Usage = usage; Changed?.Invoke(this, EventArgs.Empty); }
    }
    public void Clear()
    {
        var hadAccount = Me is not null;
        Me = null;
        meLoaded = null;
        if (hadAccount) { AccountChanged?.Invoke(this, null); }
        Changed?.Invoke(this, EventArgs.Empty);
    }

    public async Task RefreshConfigAsync(TimeSpan? maxAge = null, CancellationToken ct = default)
    {
        if (configLoaded is not null && clock.Now - configLoaded < (maxAge ?? TimeSpan.FromMinutes(5))) { return; }
        try
        {
            Config = await api.SendAsync<ServerConfig>(new(HttpMethod.Get, "/api/v1/config") { Authenticated = false }, ct).ConfigureAwait(false);
            configLoaded = clock.Now;
            Changed?.Invoke(this, EventArgs.Empty);
        }
        catch (ApiException ex) when (ex.Failure != ApiFailure.Cancelled) { /* Retain last usable configuration. */ }
    }
    public async Task RefreshMeAsync(TimeSpan? maxAge = null, CancellationToken ct = default)
    {
        if (meLoaded is not null && clock.Now - meLoaded < (maxAge ?? TimeSpan.Zero)) { return; }
        var account = api.Session?.UserId;
        if (account is null) { return; }
        var me = await api.SendAsync<MeResponse>(new(HttpMethod.Get, "/api/v1/me"), ct).ConfigureAwait(false);
        if (api.Session?.UserId == account && me.User.Id == account) { Apply(me); }
    }
    public async Task AcceptConsentAsync(CancellationToken ct = default)
    {
        IsConsentSaving = true;
        Changed?.Invoke(this, EventArgs.Empty);
        try
        {
            for (var attempt = 0; ; attempt++)
            {
                await RefreshConfigAsync(attempt == 0 ? TimeSpan.FromSeconds(60) : TimeSpan.Zero, ct).ConfigureAwait(false);
                var legal = Config.Legal;
                if (string.IsNullOrEmpty(legal?.TermsVersion) || string.IsNullOrEmpty(legal.PrivacyVersion))
                {
                    throw new ApiException(ApiFailure.InvalidResponse);
                }
                try
                {
                    await api.SendAsync(new(HttpMethod.Post, "/api/v1/me/consents")
                    {
                        Body = new { legal.TermsVersion, legal.PrivacyVersion, locale = api.Language, platform = AppInfo.Platform, app_version = api.Info.Version },
                    }, ct).ConfigureAwait(false);
                    await RefreshMeAsync(ct: ct).ConfigureAwait(false);
                    return;
                }
                catch (ApiException ex) when (attempt == 0 && ex.Status == 400) { /* Legal versions changed: fetch and retry once. */ }
            }
        }
        finally { IsConsentSaving = false; Changed?.Invoke(this, EventArgs.Empty); }
    }
    public async Task<Uri> PlansUriAsync(CancellationToken ct = default)
    {
        var fallback = api.WebsiteUri(WebsitePaths.Payment);
        var expected = api.Session?.UserId;
        if (expected is null) { return fallback; }
        try
        {
            var response = await api.SendAsync<PaymentHandoff>(new(HttpMethod.Post, "/api/v1/web/payment-handoff")
            { Body = new { expected_user_id = expected, locale = api.Language } }, ct).ConfigureAwait(false);
            if (expected == api.Session?.UserId && Uri.TryCreate(response.Url, UriKind.Absolute, out var url) &&
                url.Scheme == Uri.UriSchemeHttps && url.Authority == fallback.Authority && url.AbsolutePath == fallback.AbsolutePath &&
                url.Fragment.StartsWith("#handoff=", StringComparison.Ordinal)) { return url; }
        }
        catch (ApiException ex) when (ex.Failure != ApiFailure.Cancelled) { /* A plain payment page remains usable. */ }
        return fallback;
    }
    public Uri AccountDeletionUri => SafeWebUri(Config.Legal?.AccountDeletionUrl) ?? api.WebsiteUri(WebsitePaths.AccountDeletion);
    public static Uri? SafeWebUri(string? value) => Uri.TryCreate(value, UriKind.Absolute, out var url) && url.Scheme is "https" or "http" ? url : null;
    public void Dispose() => api.SessionChanged -= OnSessionChanged;
    private sealed class PaymentHandoff { public string? Url { get; init; } }
}
