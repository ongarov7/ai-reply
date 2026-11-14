using System.Collections.Concurrent;
using System.Net;
using System.Text.Json;
using AirReply.Core.Accounts;
using AirReply.Core.Infrastructure;
using AirReply.Core.Networking;
using Xunit;

namespace AirReply.Core.Tests;

public sealed class PaymentRefreshTests
{
    private const string Code = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA";
    private const string SafeUrl = "https://ai-reply.kz/payment#handoff=" + Code;
    private static CancellationToken Ct => TestContext.Current.CancellationToken;

    [Fact]
    public async Task Delayed_bank_credit_appears_after_return_without_another_login()
    {
        using var fixture = new Fixture();
        fixture.MeResponses.Enqueue(MeJson(0));
        fixture.MeResponses.Enqueue(MeJson(0));
        fixture.MeResponses.Enqueue(MeJson(500));
        Assert.Equal(SafeUrl, (await fixture.Account.PlansUriAsync(Ct)).AbsoluteUri);
        Assert.True(fixture.Account.IsWebsitePaymentPending);
        Assert.Null(fixture.Account.PaymentRefreshTask);
        Assert.Equal(0, fixture.MeCalls);
        var changed = 0;
        fixture.Account.Changed += (_, _) => Interlocked.Increment(ref changed);
        await fixture.Account.RefreshForForegroundAsync(TimeSpan.FromMinutes(5), Ct);
        await AwaitPaymentAsync(fixture.Account);
        Assert.Equal(500, fixture.Account.Balance?.Credits);
        Assert.Equal(3, fixture.MeCalls);
        Assert.Equal(new[] { TimeSpan.FromSeconds(3), TimeSpan.FromSeconds(3) }, fixture.Delays);
        Assert.True(changed >= 3);
        Assert.False(fixture.Account.IsWebsitePaymentPending);
        Assert.DoesNotContain(fixture.Handler.Requests, r => r.Url.EndsWith("/auth/refresh", StringComparison.Ordinal));
    }

    [Fact]
    public async Task Ordinary_foreground_and_manual_force_refresh_never_start_polling()
    {
        using var fixture = new Fixture();
        await fixture.Account.RefreshForForegroundAsync(TimeSpan.FromSeconds(10), Ct);
        Assert.Equal(0, fixture.MeCalls); // The initial session payload is fresh.
        await fixture.Account.RefreshMeAsync(TimeSpan.Zero, Ct);
        Assert.Equal(1, fixture.MeCalls);
        await fixture.Account.RefreshForForegroundAsync(ct: Ct);
        Assert.Equal(2, fixture.MeCalls);
        Assert.Empty(fixture.Delays);
        Assert.Null(fixture.Account.PaymentRefreshTask);
    }

    [Fact]
    public async Task An_unchanged_balance_stops_after_two_minutes_and_does_not_rearm()
    {
        using var fixture = new Fixture();
        await fixture.Account.PlansUriAsync(Ct);
        await fixture.Account.RefreshForForegroundAsync(ct: Ct);
        await AwaitPaymentAsync(fixture.Account);
        Assert.Equal(41, fixture.MeCalls);
        Assert.Equal(40, fixture.Delays.Count);
        Assert.Equal(TimeSpan.FromSeconds(120), fixture.Clock.Now - fixture.StartedAt);
        Assert.False(fixture.Account.IsWebsitePaymentPending);
        await fixture.Account.RefreshForForegroundAsync(ct: Ct);
        Assert.Equal(42, fixture.MeCalls);
        Assert.Equal(40, fixture.Delays.Count);
    }

    [Fact]
    public async Task Attempt_cap_also_bounds_polling_when_the_wall_clock_does_not_advance()
    {
        using var fixture = new Fixture(advanceClock: false);
        await fixture.Account.PlansUriAsync(Ct);
        await fixture.Account.RefreshForForegroundAsync(ct: Ct);
        await AwaitPaymentAsync(fixture.Account);
        Assert.Equal(41, fixture.MeCalls);
        Assert.Equal(40, fixture.Delays.Count);
        Assert.Null(fixture.Account.PaymentRefreshTask);
    }

    [Theory]
    [InlineData("unlimited")]
    [InlineData("plan")]
    [InlineData("credits")]
    public async Task Confirmed_server_entitlement_change_stops_without_more_polling(string kind)
    {
        using var fixture = new Fixture();
        fixture.MeResponses.Enqueue(kind switch
        {
            "unlimited" => MeJson(0, unlimited: true),
            "plan" => MeJson(0, plan: "base"),
            _ => MeJson(30),
        });
        await fixture.Account.PlansUriAsync(Ct);
        await fixture.Account.RefreshForForegroundAsync(ct: Ct);
        Assert.Equal(1, fixture.MeCalls);
        Assert.False(fixture.Account.IsWebsitePaymentPending);
        Assert.Null(fixture.Account.PaymentRefreshTask);
        Assert.Empty(fixture.Delays);
    }

    [Theory]
    [InlineData("logout")]
    [InlineData("switch")]
    [InlineData("cancel")]
    [InlineData("dispose")]
    [InlineData("new-handoff")]
    public async Task Session_and_user_actions_cancel_waiting_payment_reads(string action)
    {
        var entered = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var wait = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        using var fixture = new Fixture(async (_, ct) => { entered.TrySetResult(); await wait.Task.WaitAsync(ct); });
        await fixture.Account.PlansUriAsync(Ct);
        await fixture.Account.RefreshForForegroundAsync(ct: Ct);
        var run = Assert.IsAssignableFrom<Task>(fixture.Account.PaymentRefreshTask);
        await entered.Task.WaitAsync(TimeSpan.FromSeconds(5), Ct);
        switch (action)
        {
            case "logout": fixture.Api.ClearSession(); break;
            case "switch": fixture.Api.Install(Payload("user-2", 7)); break;
            case "cancel": fixture.Account.CancelWebsitePaymentRefresh(); break;
            case "dispose": fixture.Account.Dispose(); break;
            default: await fixture.Account.PlansUriAsync(Ct); break;
        }
        await run.WaitAsync(TimeSpan.FromSeconds(5), Ct);
        Assert.Equal(1, fixture.MeCalls);
        Assert.Null(fixture.Account.PaymentRefreshTask);
        Assert.Equal(action == "new-handoff", fixture.Account.IsWebsitePaymentPending);
        if (action == "logout") Assert.Null(fixture.Account.Me);
        if (action == "switch") Assert.Equal("user-2", fixture.Account.Me?.User.Id);
    }

    [Fact]
    public async Task Failed_foreground_read_still_catches_up_within_the_same_bounded_window()
    {
        using var fixture = new Fixture();
        var calls = 0;
        fixture.Handler.Reply = (request, _) => Task.FromResult(request.Url.EndsWith("/payment-handoff", StringComparison.Ordinal)
            ? RecordingHandler.Json(JsonSerializer.Serialize(new { url = SafeUrl }))
            : Interlocked.Increment(ref calls) == 1 ? RecordingHandler.Error("SERVER_ERROR", HttpStatusCode.ServiceUnavailable)
            : RecordingHandler.Json(MeJson(500)));
        await fixture.Account.PlansUriAsync(Ct);
        await fixture.Account.RefreshForForegroundAsync(ct: Ct);
        await AwaitPaymentAsync(fixture.Account);
        Assert.Equal(2, calls);
        Assert.Equal(500, fixture.Account.Balance?.Credits);
        Assert.Single(fixture.Delays);
    }

    public static TheoryData<string> UnsafeHandoffUrls => new()
    {
        "http://ai-reply.kz/payment#handoff=" + Code,
        "https://evil.example/payment#handoff=" + Code,
        "https://ai-reply.kz.evil.example/payment#handoff=" + Code,
        "https://ai-reply.kz:444/payment#handoff=" + Code,
        "https://user:password@ai-reply.kz/payment#handoff=" + Code,
        "https://ai-reply.kz/elsewhere#handoff=" + Code,
        "https://ai-reply.kz/payment?account=other#handoff=" + Code,
        "https://ai-reply.kz/payment#handoff=",
        "https://ai-reply.kz/payment#handoff=" + Code + "&redirect=elsewhere",
        "https://ai-reply.kz/payment#handoff=" + Code[..42] + "B", // Non-canonical trailing base64 bits.
        "https://ai-reply.kz/payment#handoff=short",
        "javascript:alert(1)",
    };

    [Theory]
    [MemberData(nameof(UnsafeHandoffUrls))]
    public async Task Signed_in_payment_handoff_fails_closed_for_unsafe_urls(string url)
    {
        using var fixture = new Fixture();
        fixture.Handler.Reply = (_, _) => Task.FromResult(RecordingHandler.Json(JsonSerializer.Serialize(new { url })));
        var failure = await Assert.ThrowsAsync<PaymentHandoffException>(() => fixture.Account.PlansUriAsync(Ct));
        Assert.Equal(PaymentHandoffFailure.InvalidResponse, failure.Failure);
        Assert.False(fixture.Account.IsWebsitePaymentPending);
        Assert.DoesNotContain(url, failure.Message, StringComparison.Ordinal);
    }

    [Fact]
    public async Task Signed_in_handoff_unavailability_never_returns_the_plain_other_browser_account_page()
    {
        using var fixture = new Fixture();
        fixture.Handler.Reply = (_, _) => Task.FromResult(RecordingHandler.Error("SERVER_ERROR", HttpStatusCode.ServiceUnavailable));
        var failure = await Assert.ThrowsAsync<PaymentHandoffException>(() => fixture.Account.PlansUriAsync(Ct));
        Assert.Equal(PaymentHandoffFailure.Unavailable, failure.Failure);
        Assert.False(fixture.Account.IsWebsitePaymentPending);
        fixture.Api.ClearSession();
        var before = fixture.Handler.Requests.Count;
        Assert.Equal("https://ai-reply.kz/payment", (await fixture.Account.PlansUriAsync(Ct)).AbsoluteUri);
        Assert.Equal(before, fixture.Handler.Requests.Count);
    }

    [Fact]
    public async Task Sandbox_handoff_must_stay_under_its_exact_website_path()
    {
        using var fixture = new Fixture(backend: "https://ai-reply.kz/sandbox");
        Assert.Equal("https://ai-reply.kz/sandbox/payment#handoff=" + Code, (await fixture.Account.PlansUriAsync(Ct)).AbsoluteUri);
        fixture.Handler.Reply = (_, _) => Task.FromResult(RecordingHandler.Json(JsonSerializer.Serialize(new { url = SafeUrl })));
        await Assert.ThrowsAsync<PaymentHandoffException>(() => fixture.Account.PlansUriAsync(Ct));
    }

    [Theory]
    [InlineData("logout")]
    [InlineData("switch-away-and-back")]
    public async Task Old_account_me_response_cannot_restore_a_replaced_session(string action)
    {
        using var fixture = new Fixture();
        var entered = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var release = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        fixture.Handler.Reply = async (_, _) => { entered.TrySetResult(); await release.Task; return RecordingHandler.Json(MeJson(500)); };
        var request = fixture.Account.RefreshMeAsync(ct: Ct);
        await entered.Task.WaitAsync(TimeSpan.FromSeconds(5), Ct);
        fixture.Api.ClearSession();
        if (action != "logout") { fixture.Api.Install(Payload("user-2", 7)); fixture.Api.Install(Payload("user-1", 9)); }
        release.SetResult();
        await AllowSessionCancellationAsync(request);
        Assert.Equal(action == "logout" ? null : "user-1", fixture.Account.Me?.User.Id);
        Assert.Equal(action == "logout" ? null : 9, fixture.Account.Balance?.Credits);
    }

    [Fact]
    public async Task Older_concurrent_me_cannot_overwrite_a_newer_response_or_ai_usage()
    {
        using var fixture = new Fixture();
        var entered = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var release = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var calls = 0;
        fixture.Handler.Reply = async (_, _) =>
        {
            if (Interlocked.Increment(ref calls) == 1) { entered.TrySetResult(); await release.Task; return RecordingHandler.Json(MeJson(0)); }
            return RecordingHandler.Json(MeJson(500));
        };
        var old = fixture.Account.RefreshMeAsync(ct: Ct);
        await entered.Task.WaitAsync(TimeSpan.FromSeconds(5), Ct);
        await fixture.Account.RefreshMeAsync(ct: Ct);
        fixture.Account.Apply(new UsageDto { Credits = 499, RemainingTotal = 499 });
        release.SetResult();
        await old;
        Assert.Equal(499, fixture.Account.Balance?.Credits);
    }

    [Fact]
    public void Generation_usage_only_applies_to_its_original_session_and_state()
    {
        using var fixture = new Fixture();
        var generation = fixture.Api.SessionGeneration;
        var revision = fixture.Account.StateRevision;
        Assert.True(fixture.Account.TryApplyUsage(new UsageDto { Credits = 499 }, generation, "user-1", revision));
        Assert.False(fixture.Account.TryApplyUsage(new UsageDto { Credits = 0 }, generation, "user-1", revision));
        var freshRevision = fixture.Account.StateRevision;
        fixture.Api.ClearSession(); fixture.Api.Install(Payload("user-1", 9));
        Assert.False(fixture.Account.TryApplyUsage(new UsageDto { Credits = 0 }, generation, "user-1", freshRevision));
        Assert.Equal(9, fixture.Account.Balance?.Credits);
    }

    [Theory]
    [InlineData("logout")]
    [InlineData("switch-away-and-back")]
    [InlineData("cancel")]
    public async Task Delayed_handoff_cannot_arm_after_the_original_intent_is_invalidated(string action)
    {
        using var fixture = new Fixture();
        var entered = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var release = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        fixture.Handler.Reply = async (_, _) => { entered.TrySetResult(); await release.Task; return RecordingHandler.Json(JsonSerializer.Serialize(new { url = SafeUrl })); };
        var handoff = fixture.Account.PlansUriAsync(Ct);
        await entered.Task.WaitAsync(TimeSpan.FromSeconds(5), Ct);
        if (action == "cancel") fixture.Account.CancelWebsitePaymentRefresh();
        else
        {
            fixture.Api.ClearSession();
            if (action == "switch-away-and-back") { fixture.Api.Install(Payload("user-2", 7)); fixture.Api.Install(Payload("user-1", 9)); }
        }
        release.SetResult();
        if (action == "cancel") await Assert.ThrowsAsync<PaymentHandoffException>(() => handoff);
        else await AllowSessionCancellationAsync(handoff);
        Assert.False(fixture.Account.IsWebsitePaymentPending);
        Assert.Null(fixture.Account.PaymentRefreshTask);
    }

    [Fact]
    public async Task Latest_handoff_wins_when_an_older_handoff_finishes_after_it()
    {
        using var fixture = new Fixture();
        var entered = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var release = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var calls = 0;
        fixture.Handler.Reply = async (_, _) =>
        {
            if (Interlocked.Increment(ref calls) == 1) { entered.TrySetResult(); await release.Task; }
            return RecordingHandler.Json(JsonSerializer.Serialize(new { url = SafeUrl }));
        };
        var old = fixture.Account.PlansUriAsync(Ct);
        await entered.Task.WaitAsync(TimeSpan.FromSeconds(5), Ct);
        await fixture.Account.PlansUriAsync(Ct);
        release.SetResult();
        var failure = await Assert.ThrowsAsync<PaymentHandoffException>(() => old);
        Assert.Equal(PaymentHandoffFailure.AccountChanged, failure.Failure);
        Assert.True(fixture.Account.IsWebsitePaymentPending);
        Assert.Null(fixture.Account.PaymentRefreshTask);
    }

    [Fact]
    public async Task A_handoff_without_a_loaded_account_first_loads_its_balance_baseline()
    {
        using var fixture = new Fixture();
        fixture.Account.Clear();
        await fixture.Account.PlansUriAsync(Ct);
        Assert.Equal(1, fixture.MeCalls);
        fixture.MeResponses.Enqueue(MeJson(0));
        fixture.MeResponses.Enqueue(MeJson(500));
        await fixture.Account.RefreshForForegroundAsync(ct: Ct);
        await AwaitPaymentAsync(fixture.Account);
        Assert.Equal(500, fixture.Account.Balance?.Credits);
        Assert.Equal(3, fixture.MeCalls);
        Assert.Single(fixture.Delays);
    }

    [Fact]
    public async Task A_foreign_account_me_response_is_rejected_without_overwriting_balance()
    {
        using var fixture = new Fixture();
        fixture.Handler.Reply = (_, _) => Task.FromResult(RecordingHandler.Json(MeJson(9999, user: "foreign-user")));
        var failure = await Assert.ThrowsAsync<ApiException>(() => fixture.Account.RefreshMeAsync(ct: Ct));
        Assert.Equal(ApiFailure.InvalidResponse, failure.Failure);
        Assert.Equal("user-1", fixture.Account.Me?.User.Id);
        Assert.Equal(0, fixture.Account.Balance?.Credits);
    }

    [Theory]
    [InlineData(false)]
    [InlineData(true)]
    public async Task Late_session_events_cannot_restore_an_older_signin_or_clear_a_new_signin(bool clearing)
    {
        var clock = new FakeClock();
        using var handler = new RecordingHandler();
        using var http = new HttpClient(handler);
        using var api = new ApiClient(http, ApiClientTests.Info(), new InMemorySecretStore(), clock: clock);
        var entered = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        using var release = new ManualResetEventSlim();
        api.SessionChanged += (_, e) =>
        {
            if (e.Generation == (clearing ? 2 : 1)) { entered.TrySetResult(); release.Wait(Ct); }
        };
        using var account = new AccountStore(api, clock);
        if (clearing) api.Install(Payload("user-1", 1));
        var old = Task.Run(() => { if (clearing) api.ClearSession(); else api.Install(Payload("user-1", 1)); }, Ct);
        try
        {
            await entered.Task.WaitAsync(TimeSpan.FromSeconds(5), Ct);
            api.Install(Payload("user-2", 9));
        }
        finally { release.Set(); }
        await old;
        Assert.Equal("user-2", account.Me?.User.Id);
        Assert.Equal(9, account.Balance?.Credits);
    }

    private static async Task AwaitPaymentAsync(AccountStore account)
    {
        var run = account.PaymentRefreshTask;
        if (run is not null) await run.WaitAsync(TimeSpan.FromSeconds(5), Ct);
    }
    private static async Task AllowSessionCancellationAsync(Task action)
    {
        try { await action; }
        catch (ApiException ex) when (ex.Failure is ApiFailure.Cancelled or ApiFailure.SignedOut) { }
        catch (PaymentHandoffException ex) when (ex.Failure == PaymentHandoffFailure.AccountChanged) { }
        catch (OperationCanceledException) { }
    }
    private static string MeJson(int credits, string user = "user-1", bool unlimited = false, string plan = "free") =>
        JsonSerializer.Serialize(new { user = new { id = user }, usage = new { credits, remaining_total = credits, unlimited }, subscription = new { plan = new { code = plan }, status = "active" } });
    private static SessionPayload Payload(string user = "user-1", int credits = 0) => new()
    {
        AccessToken = Guid.NewGuid().ToString(), RefreshToken = Guid.NewGuid().ToString(), ExpiresIn = 3600,
        DeviceId = "server-device", User = new() { Id = user, Email = "test@example.invalid" },
        Usage = new() { Credits = credits, RemainingTotal = credits },
        Subscription = new() { Plan = new() { Code = "free" }, Status = "active" },
    };

    private sealed class Fixture : IDisposable
    {
        private readonly HttpClient http;
        private int meCalls;
        public FakeClock Clock { get; } = new();
        public DateTimeOffset StartedAt { get; }
        public ConcurrentQueue<string> MeResponses { get; } = new();
        public ConcurrentQueue<TimeSpan> Delays { get; } = new();
        public RecordingHandler Handler { get; }
        public ApiClient Api { get; }
        public AccountStore Account { get; }
        public int MeCalls => Volatile.Read(ref meCalls);
        public Fixture(Func<TimeSpan, CancellationToken, Task>? delay = null, bool advanceClock = true, string backend = "https://ai-reply.kz")
        {
            StartedAt = Clock.Now;
            Handler = new((request, _) =>
            {
                if (request.Url.EndsWith("/payment-handoff", StringComparison.Ordinal))
                    return Task.FromResult(RecordingHandler.Json(JsonSerializer.Serialize(new { url = backend.TrimEnd('/') + "/payment#handoff=" + Code })));
                Interlocked.Increment(ref meCalls);
                return Task.FromResult(RecordingHandler.Json(MeResponses.TryDequeue(out var response) ? response : MeJson(0)));
            });
            http = new(Handler);
            Api = new(http, ApiClientTests.Info(backend), new InMemorySecretStore(), clock: Clock);
            Account = new(Api, Clock, delay ?? (async (span, ct) =>
            {
                ct.ThrowIfCancellationRequested(); Delays.Enqueue(span);
                if (advanceClock) Clock.Now += span;
                await Task.Yield();
            }));
            Api.Install(Payload());
        }
        public void Dispose() { Account.Dispose(); Api.Dispose(); http.Dispose(); }
    }
}
