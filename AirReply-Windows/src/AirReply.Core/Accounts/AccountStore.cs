using System.Text.Json;
using AirReply.Core.Infrastructure;
using AirReply.Core.Networking;

namespace AirReply.Core.Accounts;

public sealed record BalanceSummary(string PlanCode, string PlanName, bool IsUnlimited, int RemainingTotal,
    int RemainingToday, int DailyLimit, int? WelcomeRemaining, int? WelcomeTotal, int Credits, DateTimeOffset? ResetsAt)
{
    public bool IsExhausted => !IsUnlimited && RemainingTotal <= 0;
}

public enum PaymentHandoffFailure { Unavailable, AccountChanged, InvalidResponse }

/// <summary>Never contains a handoff token, URL, account address or provider response.</summary>
public sealed class PaymentHandoffException(PaymentHandoffFailure failure, Exception? inner = null)
    : Exception("A secure payment handoff could not be opened.", inner)
{
    public PaymentHandoffFailure Failure { get; } = failure;
}

public sealed class AccountStore : IDisposable
{
    private static readonly TimeSpan PaymentWindow = TimeSpan.FromSeconds(120);
    private static readonly TimeSpan PaymentInterval = TimeSpan.FromSeconds(3);
    private readonly ApiClient api;
    private readonly IClock clock;
    private readonly Func<TimeSpan, CancellationToken, Task> paymentDelay;
    private readonly Lock stateGate = new();
    private DateTimeOffset? configLoaded;
    private DateTimeOffset? meLoaded;
    private ServerConfig config = new();
    private MeResponse? me;
    private bool consentSaving;
    private bool disposed;
    private long stateRevision, meRequestRevision, configRequestRevision, paymentRevision, observedSessionGeneration;
    private PaymentReturn? paymentReturn;
    private CancellationTokenSource? paymentCancellation;
    private Task? paymentRefreshTask;

    public AccountStore(ApiClient api, IClock? clock = null, Func<TimeSpan, CancellationToken, Task>? paymentDelay = null)
    {
        ArgumentNullException.ThrowIfNull(api);
        this.api = api;
        this.clock = clock ?? SystemClock.Instance;
        this.paymentDelay = paymentDelay ?? Task.Delay;
        observedSessionGeneration = api.SessionGeneration;
        api.SessionChanged += OnSessionChanged;
    }
    public ServerConfig Config { get { lock (stateGate) { return config; } } }
    public bool IsConfigLoaded { get { lock (stateGate) { return configLoaded is not null; } } }
    public MeResponse? Me { get { lock (stateGate) { return me; } } }
    public bool? HasConsent => Me is { } value ? value.LegalConsent is not null : null;
    public bool IsConsentSaving { get { lock (stateGate) { return consentSaving; } } }
    public Task? PaymentRefreshTask { get { lock (stateGate) { return paymentRefreshTask; } } }
    public bool IsWebsitePaymentPending { get { lock (stateGate) { return paymentReturn is not null; } } }
    public long SessionGeneration => api.SessionGeneration;
    public long StateRevision { get { lock (stateGate) { return stateRevision; } } }
    public BalanceSummary? Balance
    {
        get
        {
            var value = Me;
            return value?.Usage is not UsageDto usage ? null : new(
                value.Subscription?.Plan.Code ?? "free", value.Subscription?.Plan.DisplayName(api.Language) ?? "", usage.Unlimited,
                usage.EffectiveRemaining, usage.RemainingToday, usage.DailyLimit, usage.Welcome?.Remaining,
                usage.Welcome?.Total, usage.EffectiveCredits, usage.ResetsAt);
        }
    }
    public event EventHandler? Changed;
    public event EventHandler<string?>? AccountChanged;

    private void OnSessionChanged(object? sender, SessionEvent e)
    {
        // Do not hold stateGate while taking ApiClient's session lock: its events may run during refresh refusal.
        var session = api.Session;
        var generation = api.SessionGeneration;
        if (e.Generation is { } eventGeneration && eventGeneration != generation) return;
        if (e.Payload is { } payload && (session is null || payload.AccessToken != session.AccessToken || payload.User.Id != session.UserId)) return;
        if (e.Payload is null && session is not null) return;
        string? previous;
        string? current;
        lock (stateGate)
        {
            if (disposed || generation < observedSessionGeneration) return;
            previous = me?.User.Id;
            if (generation != observedSessionGeneration) CancelPaymentLocked();
            observedSessionGeneration = generation;
            if (e.Payload is { } fresh) ApplyMeLocked(fresh.ToMe());
            else ClearLocked();
            current = me?.User.Id;
        }
        Notify(previous, current);
    }

    public void Apply(MeResponse value)
    {
        ArgumentNullException.ThrowIfNull(value);
        string? previous;
        lock (stateGate)
        {
            if (disposed) return;
            previous = me?.User.Id;
            ApplyMeLocked(value);
        }
        Notify(previous, value.User.Id);
    }
    private void ApplyMeLocked(MeResponse value)
    {
        if (me?.User.Id != value.User.Id) CancelPaymentLocked();
        stateRevision++;
        me = value;
        meLoaded = clock.Now;
        StopCompletedPaymentLocked();
    }
    public void Apply(UsageDto usage)
    {
        ArgumentNullException.ThrowIfNull(usage);
        lock (stateGate)
        {
            if (disposed || me is null) return;
            stateRevision++;
            me = new MeResponse { User = me.User, Subscription = me.Subscription, Usage = usage, LegalConsent = me.LegalConsent };
            StopCompletedPaymentLocked();
        }
        Changed?.Invoke(this, EventArgs.Empty);
    }
    /// <summary>Apply usage only to the exact state for which an AI request was submitted.</summary>
    public bool TryApplyUsage(UsageDto usage, long expectedSessionGeneration, string expectedUserId, long expectedStateRevision)
    {
        ArgumentNullException.ThrowIfNull(usage);
        var generation = api.SessionGeneration;
        var currentUser = api.Session?.UserId;
        lock (stateGate)
        {
            if (disposed || generation != expectedSessionGeneration || currentUser != expectedUserId ||
                stateRevision != expectedStateRevision || me?.User.Id != expectedUserId) return false;
            stateRevision++;
            me = new MeResponse { User = me.User, Subscription = me.Subscription, Usage = usage, LegalConsent = me.LegalConsent };
            StopCompletedPaymentLocked();
        }
        Changed?.Invoke(this, EventArgs.Empty);
        return true;
    }

    public void Clear()
    {
        string? previous;
        lock (stateGate)
        {
            if (disposed) return;
            previous = me?.User.Id;
            ClearLocked();
        }
        Notify(previous, null);
    }
    private void ClearLocked()
    {
        CancelPaymentLocked();
        stateRevision++;
        me = null;
        meLoaded = null;
    }
    private void Notify(string? previous, string? current)
    {
        if (previous != current) AccountChanged?.Invoke(this, current);
        Changed?.Invoke(this, EventArgs.Empty);
    }

    public async Task RefreshConfigAsync(TimeSpan? maxAge = null, CancellationToken ct = default)
    {
        long request;
        lock (stateGate)
        {
            var maximumAge = maxAge ?? TimeSpan.FromMinutes(5);
            if (disposed || configLoaded is not null && maximumAge > TimeSpan.Zero && clock.Now - configLoaded < maximumAge) return;
            request = ++configRequestRevision;
        }
        try
        {
            var fresh = await api.SendAsync<ServerConfig>(new(HttpMethod.Get, "/api/v1/config") { Authenticated = false }, ct).ConfigureAwait(false);
            ct.ThrowIfCancellationRequested();
            lock (stateGate)
            {
                if (disposed || request != configRequestRevision) return;
                config = fresh;
                configLoaded = clock.Now;
            }
            Changed?.Invoke(this, EventArgs.Empty);
        }
        catch (ApiException ex) when (ex.Failure != ApiFailure.Cancelled) { /* Retain last usable configuration. */ }
    }
    public async Task RefreshMeAsync(TimeSpan? maxAge = null, CancellationToken ct = default)
    {
        var expected = api.Session;
        var sessionGeneration = api.SessionGeneration;
        if (expected is null) return;
        long revision;
        long request;
        lock (stateGate)
        {
            if (disposed || meLoaded is not null && maxAge > TimeSpan.Zero && clock.Now - meLoaded < maxAge) return;
            revision = stateRevision;
            request = ++meRequestRevision;
        }
        var fresh = await api.SendAsync<MeResponse>(new(HttpMethod.Get, "/api/v1/me"), ct).ConfigureAwait(false);
        ct.ThrowIfCancellationRequested();
        var current = api.Session;
        var currentGeneration = api.SessionGeneration;
        string? previous;
        lock (stateGate)
        {
            if (disposed || revision != stateRevision || request != meRequestRevision ||
                currentGeneration != sessionGeneration || current?.UserId != expected.UserId) return;
            if (fresh.User.Id != expected.UserId) throw new ApiException(ApiFailure.InvalidResponse);
            previous = me?.User.Id;
            ApplyMeLocked(fresh);
        }
        Notify(previous, fresh.User.Id);
    }

    /// <summary>Refresh immediately when the user returns. Only a validated website-payment handoff enables bounded retries.</summary>
    public async Task RefreshForForegroundAsync(TimeSpan? maxAge = null, CancellationToken ct = default)
    {
        bool pending;
        lock (stateGate)
        {
            if (disposed) return;
            pending = paymentReturn is not null;
            if (paymentReturn is { Deadline: null } payment)
                paymentReturn = payment with { Deadline = clock.Now.Add(PaymentWindow) };
        }
        try { await RefreshMeAsync(pending ? TimeSpan.Zero : maxAge ?? TimeSpan.Zero, ct).ConfigureAwait(false); }
        catch (ApiException ex) when (ex.Failure != ApiFailure.Cancelled) { /* A foreground refresh is best-effort; bounded payment catch-up may recover. */ }
        ct.ThrowIfCancellationRequested();
        ResumePaymentRefresh();
    }

    private void ResumePaymentRefresh()
    {
        TaskCompletionSource? start = null;
        lock (stateGate)
        {
            if (disposed || paymentReturn is not { Deadline: { } deadline } pending) return;
            if (PaymentHasArrivedLocked(pending) || clock.Now >= deadline) { CancelPaymentLocked(); return; }
            if (paymentRefreshTask is not null) return;
            var cancellation = new CancellationTokenSource();
            paymentCancellation = cancellation;
            start = new(TaskCreationOptions.RunContinuationsAsynchronously);
            paymentRefreshTask = RunPaymentRefreshAsync(pending, paymentRevision, cancellation, start.Task);
        }
        start.SetResult();
    }

    private async Task RunPaymentRefreshAsync(PaymentReturn pending, long run, CancellationTokenSource cancellation, Task start)
    {
        try
        {
            await start.ConfigureAwait(false);
            for (var attempt = 0; attempt < 40; attempt++)
            {
                TimeSpan wait;
                lock (stateGate)
                {
                    if (!IsPaymentCurrentLocked(pending, run) || pending.Deadline is not { } deadline || clock.Now >= deadline) return;
                    wait = deadline - clock.Now < PaymentInterval ? deadline - clock.Now : PaymentInterval;
                }
                await paymentDelay(wait, cancellation.Token).ConfigureAwait(false);
                cancellation.Token.ThrowIfCancellationRequested();
                lock (stateGate)
                {
                    if (!IsPaymentCurrentLocked(pending, run) || pending.Deadline is not { } deadline || clock.Now > deadline) return;
                }
                try { await RefreshMeAsync(ct: cancellation.Token).ConfigureAwait(false); }
                catch (ApiException ex) when (ex.Failure != ApiFailure.Cancelled) { /* Keep trying only within this account's deadline. */ }
                lock (stateGate)
                {
                    if (!IsPaymentCurrentLocked(pending, run) || PaymentHasArrivedLocked(pending)) return;
                }
            }
        }
        catch (OperationCanceledException) when (cancellation.IsCancellationRequested) { }
        catch (ApiException ex) when (ex.Failure == ApiFailure.Cancelled) { }
        finally
        {
            lock (stateGate)
            {
                if (paymentRevision == run)
                {
                    paymentRefreshTask = null;
                    paymentCancellation = null;
                    paymentReturn = null;
                    paymentRevision++;
                }
            }
            cancellation.Dispose();
        }
    }
    private bool IsPaymentCurrentLocked(PaymentReturn pending, long run) => !disposed && paymentRevision == run &&
        paymentReturn is not null && me?.User.Id == pending.UserId;
    private bool PaymentHasArrivedLocked(PaymentReturn pending) => (me?.Usage?.EffectiveCredits ?? 0) > pending.Credits ||
        (!pending.Unlimited && me?.Usage?.Unlimited == true) || SubscriptionSignature(me?.Subscription) != pending.Subscription;
    private void StopCompletedPaymentLocked()
    {
        if (paymentReturn is { } pending && PaymentHasArrivedLocked(pending)) CancelPaymentLocked();
    }
    public void CancelWebsitePaymentRefresh()
    {
        lock (stateGate) { CancelPaymentLocked(); }
    }
    private void CancelPaymentLocked()
    {
        paymentRevision++;
        paymentCancellation?.Cancel();
        // An in-flight loop owns its CTS until it exits; never dispose a token still used by HTTP or delay.
        paymentCancellation = null;
        paymentRefreshTask = null;
        paymentReturn = null;
    }

    public async Task AcceptConsentAsync(CancellationToken ct = default)
    {
        lock (stateGate) { consentSaving = true; }
        Changed?.Invoke(this, EventArgs.Empty);
        try
        {
            for (var attempt = 0; ; attempt++)
            {
                await RefreshConfigAsync(attempt == 0 ? TimeSpan.FromSeconds(60) : TimeSpan.Zero, ct).ConfigureAwait(false);
                var legal = Config.Legal;
                if (string.IsNullOrEmpty(legal?.TermsVersion) || string.IsNullOrEmpty(legal.PrivacyVersion))
                    throw new ApiException(ApiFailure.InvalidResponse);
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
        finally { lock (stateGate) { consentSaving = false; } Changed?.Invoke(this, EventArgs.Empty); }
    }

    public async Task<Uri> PlansUriAsync(CancellationToken ct = default)
    {
        var fallback = api.WebsiteUri(WebsitePaths.Payment);
        var expected = api.Session;
        var sessionGeneration = api.SessionGeneration;
        if (expected is null) return fallback;
        if (Me is null)
        {
            try { await RefreshMeAsync(ct: ct).ConfigureAwait(false); }
            catch (ApiException ex) when (ex.Failure != ApiFailure.Cancelled)
            { throw new PaymentHandoffException(PaymentHandoffFailure.Unavailable, ex); }
        }
        if (api.SessionGeneration != sessionGeneration || api.Session?.UserId != expected.UserId || Me?.User.Id != expected.UserId)
            throw new PaymentHandoffException(PaymentHandoffFailure.AccountChanged);
        long request;
        lock (stateGate)
        {
            if (disposed) throw new PaymentHandoffException(PaymentHandoffFailure.Unavailable);
            CancelPaymentLocked();
            request = paymentRevision;
        }
        PaymentHandoff response;
        try
        {
            response = await api.SendAsync<PaymentHandoff>(new(HttpMethod.Post, "/api/v1/web/payment-handoff")
            { Body = new { expected_user_id = expected.UserId, locale = api.Language } }, ct).ConfigureAwait(false);
        }
        catch (ApiException ex) when (ex.Failure != ApiFailure.Cancelled)
        { throw new PaymentHandoffException(api.SessionGeneration == sessionGeneration ? PaymentHandoffFailure.Unavailable : PaymentHandoffFailure.AccountChanged, ex); }
        ct.ThrowIfCancellationRequested();
        var current = api.Session;
        var currentGeneration = api.SessionGeneration;
        if (currentGeneration != sessionGeneration || current?.UserId != expected.UserId)
            throw new PaymentHandoffException(PaymentHandoffFailure.AccountChanged);
        if (!IsSafeHandoff(response.Url, fallback, out var url))
            throw new PaymentHandoffException(PaymentHandoffFailure.InvalidResponse);
        lock (stateGate)
        {
            if (disposed || request != paymentRevision)
                throw new PaymentHandoffException(PaymentHandoffFailure.AccountChanged);
            if (me is not null && me.User.Id != expected.UserId)
                throw new PaymentHandoffException(PaymentHandoffFailure.AccountChanged);
            paymentReturn = new(expected.UserId, me?.Usage?.EffectiveCredits ?? 0, me?.Usage?.Unlimited ?? false,
                SubscriptionSignature(me?.Subscription), null);
        }
        return url!;
    }

    private static bool IsSafeHandoff(string? value, Uri expected, out Uri? url)
    {
        url = null;
        if (!Uri.TryCreate(value, UriKind.Absolute, out var parsed) || parsed.Scheme != Uri.UriSchemeHttps ||
            !string.Equals(parsed.Authority, expected.Authority, StringComparison.OrdinalIgnoreCase) ||
            parsed.AbsolutePath != expected.AbsolutePath || parsed.UserInfo.Length != 0 || parsed.Query.Length != 0 ||
            !parsed.Fragment.StartsWith("#handoff=", StringComparison.Ordinal)) return false;
        var code = parsed.Fragment[9..];
        if (code.Length != 43 || !code.All(c => char.IsAsciiLetterOrDigit(c) || c is '-' or '_')) return false;
        try
        {
            var bytes = Convert.FromBase64String(code.Replace('-', '+').Replace('_', '/') + "=");
            if (bytes.Length != 32 || Convert.ToBase64String(bytes).TrimEnd('=').Replace('+', '-').Replace('/', '_') != code) return false;
        }
        catch (FormatException) { return false; }
        url = parsed;
        return true;
    }
    private static string? SubscriptionSignature(SubscriptionDto? value) => value is null ? null :
        JsonSerializer.Serialize(new { value.Status, value.Plan.Code, Name = value.Plan.Name.OrderBy(p => p.Key, StringComparer.Ordinal) }, ApiJson.Options);

    public Uri AccountDeletionUri => SafeWebUri(Config.Legal?.AccountDeletionUrl) ?? api.WebsiteUri(WebsitePaths.AccountDeletion);
    public static Uri? SafeWebUri(string? value) => Uri.TryCreate(value, UriKind.Absolute, out var url) && url.Scheme is "https" or "http" ? url : null;
    public void Dispose()
    {
        api.SessionChanged -= OnSessionChanged;
        lock (stateGate)
        {
            if (disposed) return;
            disposed = true;
            CancelPaymentLocked();
            stateRevision++;
        }
    }
    private sealed record PaymentReturn(string UserId, int Credits, bool Unlimited, string? Subscription, DateTimeOffset? Deadline);
    private sealed class PaymentHandoff { public string? Url { get; init; } }
}
