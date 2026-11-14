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
    public OtpChallengeState? Challenge { get; private set; }
    public bool IsSignedIn => api.Session is not null;
    public string LastEmail { get; private set; } = api.Session?.Email ?? "";

    public async Task RequestCodeAsync(string email, CancellationToken ct = default)
    {
        ArgumentNullException.ThrowIfNull(email);
        email = email.Trim();
        if (!email.Contains('@', StringComparison.Ordinal) || email.Length > 254) { throw new AuthException(AuthErrorKind.InvalidEmail); }
        LastEmail = email;
        try
        {
            var response = await api.SendAsync<OtpResponse>(new(HttpMethod.Post, "/api/v1/auth/email/otp/request")
            { Authenticated = false, Body = new { email, locale = api.Language } }, ct).ConfigureAwait(false);
            Challenge = new(email, response.MaskedEmail, Math.Clamp(response.CodeLength, 1, 12), clock.Now.AddSeconds(Math.Max(0, response.ResendAfter)));
        }
        catch (ApiException ex) when (ex.Failure != ApiFailure.Cancelled) { throw Map(ex); }
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
        var challenge = Challenge ?? throw new AuthException(AuthErrorKind.InvalidCode);
        var digits = new string(code.Where(char.IsAsciiDigit).ToArray());
        if (digits.Length != challenge.CodeLength) { throw new AuthException(AuthErrorKind.InvalidCode); }
        try
        {
            var response = await api.SendAsync<SessionPayload>(new(HttpMethod.Post, "/api/v1/auth/email/otp/verify")
            { Authenticated = false, Body = new { email = challenge.Email, code = digits, device = api.CurrentDevice() } }, ct).ConfigureAwait(false);
            api.Install(response, challenge.Email);
            Challenge = null;
            // Registration precedes consent and any AI call, including on accounts without consent.
            await registrar.SyncAsync(force: true, ct).ConfigureAwait(false);
            await account.RefreshConfigAsync(ct: ct).ConfigureAwait(false);
            return true;
        }
        catch (ApiException ex) when (ex.Failure != ApiFailure.Cancelled) { throw Map(ex); }
        catch (Exception ex) when (ex is IOException or UnauthorizedAccessException or System.Security.Cryptography.CryptographicException)
        { throw new AuthException(AuthErrorKind.Server, innerException: ex); }
    }
    public void UseAnotherEmail() => Challenge = null;
    public Task SignOutAsync(CancellationToken ct = default)
    {
        Challenge = null;
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
