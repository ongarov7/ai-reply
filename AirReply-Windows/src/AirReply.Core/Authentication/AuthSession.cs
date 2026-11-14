using AirReply.Core.Accounts;
using AirReply.Core.Infrastructure;
using AirReply.Core.Installations;
using AirReply.Core.Networking;

namespace AirReply.Core.Authentication;

public enum AuthErrorKind { InvalidEmail, InvalidCode, CodeExpired, CodeAlreadyUsed, TooManyAttempts, Cooldown, RateLimited, EmailDelivery, AccountDisabled, Offline, Server }
public sealed class AuthException(AuthErrorKind kind, int? cooldownSeconds = null, Exception? innerException = null)
    : Exception(kind.ToString(), innerException)
{
    public AuthErrorKind Kind { get; } = kind;
    public int? CooldownSeconds { get; } = cooldownSeconds;
}
public sealed record OtpChallengeState(string Email, string MaskedEmail, int CodeLength, DateTimeOffset ResendAvailableAt);

public sealed class AuthSession(ApiClient api, AccountStore account, InstallationRegistrar registrar, IClock? clock = null)
{
    private readonly IClock clock = clock ?? SystemClock.Instance;
    private readonly Lock flowGate = new();
    private long flowRevision;
    private long? verificationInFlight;
    private OtpChallengeState? challenge;
    private string lastEmail = api.Session?.Email ?? "";
    public OtpChallengeState? Challenge { get { lock (flowGate) { return challenge; } } }
    public bool IsSignedIn => api.Session is not null;
    public string LastEmail { get { lock (flowGate) { return lastEmail; } } }

    private void RequireCurrentFlow(long revision, long sessionGeneration, CancellationToken ct)
    {
        if (ct.IsCancellationRequested || flowRevision != revision || api.SessionGeneration != sessionGeneration)
        {
            if (flowRevision == revision) { flowRevision++; challenge = null; }
            throw new ApiException(ApiFailure.Cancelled);
        }
    }

    private void CancelFlow(long revision)
    {
        lock (flowGate)
        {
            if (flowRevision != revision) return;
            flowRevision++;
            challenge = null;
        }
    }

    public async Task RequestCodeAsync(string email, CancellationToken ct = default)
    {
        ArgumentNullException.ThrowIfNull(email);
        email = email.Trim();
        if (!email.Contains('@', StringComparison.Ordinal) || email.Length > 254) { throw new AuthException(AuthErrorKind.InvalidEmail); }
        long revision, sessionGeneration;
        lock (flowGate)
        {
            revision = ++flowRevision;
            sessionGeneration = api.SessionGeneration;
            lastEmail = email;
            challenge = null;
        }
        using var cancellation = ct.Register(() => CancelFlow(revision));
        try
        {
            var response = await api.SendAsync<OtpResponse>(new(HttpMethod.Post, "/api/v1/auth/email/otp/request")
            { Authenticated = false, Body = new { email, locale = api.Language } }, ct).ConfigureAwait(false);
            lock (flowGate)
            {
                RequireCurrentFlow(revision, sessionGeneration, ct);
                challenge = new(email, response.MaskedEmail, Math.Clamp(response.CodeLength, 1, 12), clock.Now.AddSeconds(Math.Max(0, response.ResendAfter)));
            }
        }
        catch (ApiException ex) when (ex.Failure != ApiFailure.Cancelled)
        {
            lock (flowGate) { RequireCurrentFlow(revision, sessionGeneration, ct); }
            throw Map(ex);
        }
    }
    public async Task ResendCodeAsync(CancellationToken ct = default)
    {
        var challenge = Challenge ?? throw new AuthException(AuthErrorKind.Server);
        if (clock.Now < challenge.ResendAvailableAt)
        {
            throw new AuthException(AuthErrorKind.Cooldown, (int)Math.Ceiling((challenge.ResendAvailableAt - clock.Now).TotalSeconds));
        }
        await RequestCodeAsync(challenge.Email, ct).ConfigureAwait(false);
    }
    public async Task<bool> VerifyAsync(string code, CancellationToken ct = default)
    {
        ArgumentNullException.ThrowIfNull(code);
        OtpChallengeState pending;
        long revision, sessionGeneration;
        var digits = new string(code.Where(char.IsAsciiDigit).ToArray());
        lock (flowGate)
        {
            pending = challenge ?? throw new AuthException(AuthErrorKind.InvalidCode);
            if (digits.Length != pending.CodeLength) throw new AuthException(AuthErrorKind.InvalidCode);
            revision = flowRevision;
            sessionGeneration = api.SessionGeneration;
            if (verificationInFlight == revision) throw new AuthException(AuthErrorKind.RateLimited);
            verificationInFlight = revision;
        }
        using var cancellation = ct.Register(() => CancelFlow(revision));
        try
        {
            var response = await api.SendAsync<SessionPayload>(new(HttpMethod.Post, "/api/v1/auth/email/otp/verify")
            { Authenticated = false, Body = new { email = pending.Email, code = digits, device = api.CurrentDevice() } }, ct).ConfigureAwait(false);
            long installedGeneration, installedRevision;
            lock (flowGate)
            {
                RequireCurrentFlow(revision, sessionGeneration, ct);
                if (!api.TryInstall(response, sessionGeneration, pending.Email, ct))
                    throw new ApiException(ApiFailure.Cancelled);
                challenge = null;
                installedRevision = ++flowRevision;
                installedGeneration = api.SessionGeneration;
            }
            // Registration precedes consent and any AI call, including on accounts without consent.
            await registrar.SyncAsync(force: true, ct).ConfigureAwait(false);
            await account.RefreshConfigAsync(ct: ct).ConfigureAwait(false);
            lock (flowGate) { RequireCurrentFlow(installedRevision, installedGeneration, ct); }
            return true;
        }
        catch (ApiException ex) when (ex.Failure != ApiFailure.Cancelled)
        {
            lock (flowGate) { RequireCurrentFlow(revision, sessionGeneration, ct); }
            throw Map(ex);
        }
        catch (Exception ex) when (ex is IOException or UnauthorizedAccessException or System.Security.Cryptography.CryptographicException)
        { throw new AuthException(AuthErrorKind.Server, innerException: ex); }
        finally
        {
            lock (flowGate) { if (verificationInFlight == revision) verificationInFlight = null; }
        }
    }
    public void UseAnotherEmail()
    {
        lock (flowGate) { flowRevision++; challenge = null; }
    }
    public Task SignOutAsync(CancellationToken ct = default)
    {
        UseAnotherEmail();
        return api.SignOutAsync(ct);
    }
    private static AuthException Map(ApiException ex) => new(ex.Code switch
    {
        "INVALID_EMAIL" => AuthErrorKind.InvalidEmail,
        "INVALID_OTP" => AuthErrorKind.InvalidCode,
        "OTP_EXPIRED" => AuthErrorKind.CodeExpired,
        "OTP_ALREADY_USED" => AuthErrorKind.CodeAlreadyUsed,
        "OTP_ATTEMPTS_EXCEEDED" => AuthErrorKind.TooManyAttempts,
        "OTP_RESEND_COOLDOWN" => AuthErrorKind.Cooldown,
        "RATE_LIMITED" => AuthErrorKind.RateLimited,
        "EMAIL_DELIVERY_FAILED" => AuthErrorKind.EmailDelivery,
        "ACCOUNT_DISABLED" => AuthErrorKind.AccountDisabled,
        _ when ex.Failure == ApiFailure.Offline => AuthErrorKind.Offline,
        _ => AuthErrorKind.Server,
    }, ex.RetryAfter is { } retry ? (int)Math.Ceiling(retry.TotalSeconds) : null, ex);
    private sealed class OtpResponse
    {
        public string MaskedEmail { get; init; } = "";
        public int ResendAfter { get; init; } = 30;
        public int CodeLength { get; init; } = 4;
    }
}
