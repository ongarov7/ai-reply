using System.Text.Json;
using System.Text.Json.Serialization;
using AirReply.Core.Infrastructure;

namespace AirReply.Core.Networking;

public static class ApiJson
{
    public static JsonSerializerOptions Options { get; } = new()
    {
        PropertyNamingPolicy = JsonNamingPolicy.SnakeCaseLower,
        PropertyNameCaseInsensitive = true,
        DefaultIgnoreCondition = JsonIgnoreCondition.WhenWritingNull,
        Encoder = System.Text.Encodings.Web.JavaScriptEncoder.UnsafeRelaxedJsonEscaping,
    };
}

public enum ApiFailure { Offline, Cancelled, Server, InvalidResponse, SignedOut }

public sealed class ApiException : Exception
{
    public ApiException(ApiFailure failure, int? status = null, string? code = null, Exception? innerException = null)
        : base(code ?? failure.ToString(), innerException) { Failure = failure; Status = status; Code = code; }
    public ApiFailure Failure { get; }
    public int? Status { get; }
    public string? Code { get; }
    public IReadOnlyDictionary<string, JsonElement> Details { get; init; } = new Dictionary<string, JsonElement>();
    public TimeSpan? RetryAfter { get; init; }
    public bool MaybeDelivered { get; init; }
    public string? RequestId { get; init; }
    public string? DetailString(string key) => Details.TryGetValue(key, out var value)
        ? value.ValueKind == JsonValueKind.String ? value.GetString() : value.ToString() : null;
    public int? DetailInt(string key) => int.TryParse(DetailString(key), System.Globalization.CultureInfo.InvariantCulture, out var value) ? value : null;
    public bool IsExpiredSession => Failure == ApiFailure.Server && Status == 401 && Code is "UNAUTHORIZED" or "TOKEN_EXPIRED";
}

/// <summary>A body factory allows a retry to create fresh content, including multipart streams.</summary>
public sealed record ApiRequest(HttpMethod Method, string Path)
{
    public object? Body { get; init; }
    public Func<HttpContent>? ContentFactory { get; init; }
    public bool Authenticated { get; init; } = true;
    public bool? SendInstallationId { get; init; }
    public string? IdempotencyKey { get; init; }
    public TimeSpan Timeout { get; init; } = TimeSpan.FromSeconds(30);
}

public enum SignOutReason { User, RefreshRefused, TokenRejected }
public sealed record SessionEvent(SessionPayload? Payload = null, SignOutReason? SignOutReason = null, long? Generation = null);

public sealed record SessionTokens(string AccessToken, string RefreshToken, DateTimeOffset AccessExpiresAt,
    DateTimeOffset? RefreshExpiresAt, string DeviceId, string UserId, string? Email, string InstallationId)
{
    public bool NeedsRefresh(DateTimeOffset now) => AccessExpiresAt - now < TimeSpan.FromSeconds(60);
}

public sealed record DeviceDescriptor(string InstallationId, string DeviceId, string Platform, string AppVersion,
    string OsVersion, string Model, string Locale, string Timezone);

public sealed record DeviceMetadata(string Model = "", string Manufacturer = "", string? Timezone = null);

public sealed class UserDto
{
    public required string Id { get; init; }
    public string? Email { get; init; }
}

public sealed class SessionPayload
{
    public required string AccessToken { get; init; }
    public required string RefreshToken { get; init; }
    public int ExpiresIn { get; init; } = 900;
    public DateTimeOffset? RefreshExpiresAt { get; init; }
    public string DeviceId { get; init; } = "";
    public required UserDto User { get; init; }
    public SubscriptionDto? Subscription { get; init; }
    public UsageDto? Usage { get; init; }
    public LegalConsentDto? LegalConsent { get; init; }
    public MeResponse ToMe() => new() { User = User, Subscription = Subscription, Usage = Usage, LegalConsent = LegalConsent };
}

public sealed class PlanDto
{
    public string Code { get; init; } = "free";
    public Dictionary<string, string> Name { get; init; } = new(StringComparer.Ordinal);
    public string DisplayName(string language) => new[] { language, "ru", "en" }
        .Select(key => Name.GetValueOrDefault(key)).FirstOrDefault(value => !string.IsNullOrWhiteSpace(value)) ?? Code;
}
public sealed class SubscriptionDto
{
    public PlanDto Plan { get; init; } = new();
    public string? Status { get; init; }
}
public sealed class CreditBucketDto
{
    public int Remaining { get; init; }
    public int Total { get; init; }
}
public sealed class UsageDto
{
    public bool Unlimited { get; init; }
    public int DailyLimit { get; init; }
    public int RemainingToday { get; init; }
    public int PurchasedCredits { get; init; }
    public int AdminCredits { get; init; }
    public int? Credits { get; init; }
    public int? RemainingTotal { get; init; }
    public DateTimeOffset? ResetsAt { get; init; }
    public CreditBucketDto? Welcome { get; init; }
    public CreditBucketDto? Pass { get; init; }
    [JsonIgnore] public int EffectiveCredits => Credits ?? PurchasedCredits + AdminCredits;
    [JsonIgnore] public int EffectiveRemaining => RemainingTotal ?? RemainingToday + EffectiveCredits + (Welcome?.Remaining ?? 0) + (Pass?.Remaining ?? 0);
}
public sealed class LegalConsentDto
{
    public string TermsVersion { get; init; } = "";
    public string PrivacyVersion { get; init; } = "";
}
public sealed class MeResponse
{
    public required UserDto User { get; init; }
    public SubscriptionDto? Subscription { get; init; }
    public UsageDto? Usage { get; set; }
    public LegalConsentDto? LegalConsent { get; init; }
}
public sealed class LegalConfig
{
    public string TermsVersion { get; init; } = "";
    public string PrivacyVersion { get; init; } = "";
    public string? TermsUrl { get; init; }
    public string? PrivacyUrl { get; init; }
    public string? SupportUrl { get; init; }
    public string? AccountDeletionUrl { get; init; }
    public string AiProvider { get; init; } = "OpenAI";
}
public sealed class ContextLimits
{
    public int ProfileCharacters { get; init; } = 1500;
    public int ReferenceCharacters { get; init; } = 4000;
    public int References { get; init; } = 8;
    public int ContextBytes { get; init; } = 4096;
    public int InputTokenBudget { get; init; } = 4096;
    public int[] OutputCharacters { get; init; } = [400, 600];
}
public sealed class ServerConfig
{
    public int MaxSourceCharacters { get; init; } = 400;
    public int MaxInstructionLength { get; init; } = 400;
    public Dictionary<string, bool> Features { get; init; } = new(StringComparer.Ordinal);
    public LegalConfig? Legal { get; init; }
    public ContextLimits MacContextLimits { get; init; } = new();
    public bool Feature(string key) => Features.GetValueOrDefault(key);
}

public static class WebsitePaths
{
    public const string Windows = "";
    public const string Payment = "payment";
    public const string AccountDeletion = "account/delete";
}
