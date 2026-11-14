using System.Text;
using AirReply.Core.Accounts;
using AirReply.Core.Infrastructure;
using AirReply.Core.Installations;
using AirReply.Core.Networking;

namespace AirReply.Core.Generation;

public enum GenerationErrorKind { SignedOut, ConsentRequired, AccountDisabled, ProDeviceLimit, ProDeviceIdentity, ProDeviceRevoked,
    SourceTooLong, InstructionTooLong, InstructionMissing, ContextTooLarge, ContextUnavailable, LimitReached, RateLimited,
    AiUnavailable, OutputTooLong, Offline, Server }
public sealed class GenerationException(GenerationErrorKind kind, Exception? innerException = null) : Exception(kind.ToString(), innerException)
{
    public GenerationErrorKind Kind { get; } = kind;
    public int? Limit { get; init; }
    public DateTimeOffset? ResetsAt { get; init; }
    public TimeSpan? RetryAfter { get; init; }
    public bool KeySpent { get; init; }
    public string? ServerCode { get; init; }
}
public sealed record GenerationResult(string Text, string? Language, UsageDto? Usage);

public sealed class Generator(ApiClient api, AccountStore account, InstallationRegistrar registrar,
    Func<TimeSpan, CancellationToken, Task>? delay = null)
{
    private readonly Func<TimeSpan, CancellationToken, Task> delay = delay ?? Task.Delay;
    public Task<GenerationResult> ReplyAsync(string sourceText, Recipient recipient, string? instruction, string? inputLanguage,
        string idempotencyKey, GenerationOptions options, CancellationToken ct = default)
    {
        ArgumentNullException.ThrowIfNull(sourceText);
        ValidateText(sourceText.Trim(), account.Config.MaxSourceCharacters, GenerationErrorKind.SourceTooLong);
        var body = BaseBody(options, inputLanguage);
        body["source_text"] = sourceText;
        body["template_id"] = recipient.ToWire();
        if (!string.IsNullOrWhiteSpace(instruction))
        {
            ValidateText(instruction.Trim(), account.Config.MaxInstructionLength, GenerationErrorKind.InstructionTooLong);
            body["instruction"] = instruction.Trim();
        }
        return GenerateAsync("reply", body, idempotencyKey, ct);
    }
    public Task<GenerationResult> ComposeAsync(string instruction, bool regenerate, string? inputLanguage,
        string idempotencyKey, GenerationOptions options, CancellationToken ct = default)
    {
        ArgumentNullException.ThrowIfNull(instruction);
        instruction = instruction.Trim();
        if (instruction.Length == 0) { throw new GenerationException(GenerationErrorKind.InstructionMissing); }
        ValidateText(instruction, account.Config.MaxInstructionLength, GenerationErrorKind.InstructionTooLong);
        var body = BaseBody(options, inputLanguage);
        body["instruction"] = instruction;
        body["regenerate"] = regenerate;
        return GenerateAsync("compose", body, idempotencyKey, ct);
    }
    private Dictionary<string, object> BaseBody(GenerationOptions options, string? inputLanguage)
    {
        ArgumentNullException.ThrowIfNull(options);
        var body = new Dictionary<string, object>(StringComparer.Ordinal)
        { ["language"] = api.Language, ["platform"] = AppInfo.Platform, ["app_version"] = api.Info.Version };
        if (account.Config.Feature("sender_profile") && ApiClient.NormalizeLanguage(inputLanguage) is string language)
        { body["input_language"] = language; }
        var context = options.Context;
        var hasContext = !string.IsNullOrWhiteSpace(context?.Profile) || context?.References.Any(value => !string.IsNullOrWhiteSpace(value)) == true;
        if ((hasContext || options.OutputCharacterLimit == 600) && !account.Config.Feature("desktop_context_generation"))
        { throw new GenerationException(GenerationErrorKind.ContextUnavailable); }
        if (hasContext)
        {
            var limits = account.Config.MacContextLimits;
            var references = context!.References.Where(value => !string.IsNullOrWhiteSpace(value)).ToArray();
            var profile = string.IsNullOrWhiteSpace(context.Profile) ? null : context.Profile;
            if ((profile?.EnumerateRunes().Count() ?? 0) > limits.ProfileCharacters || references.Length > limits.References ||
                references.Sum(value => value.EnumerateRunes().Count()) > limits.ReferenceCharacters ||
                (profile is null ? 0 : Encoding.UTF8.GetByteCount(profile)) + references.Sum(Encoding.UTF8.GetByteCount) > limits.ContextBytes)
            { throw new GenerationException(GenerationErrorKind.ContextTooLarge); }
            var value = new Dictionary<string, object>(StringComparer.Ordinal);
            if (profile is not null) { value["profile"] = profile; }
            if (references.Length > 0) { value["references"] = references; }
            body["context"] = value;
        }
        if (options.OutputCharacterLimit == 600) { body["output_char_limit"] = 600; }
        else if (options.OutputCharacterLimit is not (null or 400)) { throw new ArgumentOutOfRangeException(nameof(options)); }
        return body;
    }
    private static void ValidateText(string text, int maximum, GenerationErrorKind kind)
    {
        if (text.EnumerateRunes().Count() > maximum) { throw new GenerationException(kind) { Limit = maximum }; }
    }
    private async Task<GenerationResult> GenerateAsync(string mode, Dictionary<string, object> body, string key, CancellationToken ct)
    {
        if (string.IsNullOrWhiteSpace(key)) { throw new ArgumentException("An idempotency key is required.", nameof(key)); }
        var sessionGeneration = api.SessionGeneration;
        var userId = api.Session?.UserId ?? throw new GenerationException(GenerationErrorKind.SignedOut);
        RequireCurrentSession(sessionGeneration, userId, ct);
        await registrar.WaitForSessionAsync(ct: ct).ConfigureAwait(false);
        RequireCurrentSession(sessionGeneration, userId, ct);
        var healed = false;
        for (var conflicts = 0; ;)
        {
            RequireCurrentSession(sessionGeneration, userId, ct);
            var stateRevision = account.StateRevision;
            try
            {
                var result = await api.SendAsync<Response>(new(HttpMethod.Post, "/api/v1/ai/" + mode)
                { Body = body, IdempotencyKey = key, Timeout = TimeSpan.FromSeconds(45) }, ct).ConfigureAwait(false);
                RequireCurrentSession(sessionGeneration, userId, ct);
                var text = mode == "reply" ? result.Reply : result.Text;
                if (string.IsNullOrWhiteSpace(text)) { throw new ApiException(ApiFailure.InvalidResponse, 200); }
                if (result.Usage is not null && !account.TryApplyUsage(result.Usage, sessionGeneration, userId, stateRevision))
                {
                    RequireCurrentSession(sessionGeneration, userId, ct);
                    // A newer account snapshot won: obtain authoritative usage instead of overwriting it.
                    try { await account.RefreshMeAsync(TimeSpan.Zero, ct).ConfigureAwait(false); }
                    catch (ApiException ex) when (ex.Failure != ApiFailure.Cancelled) { }
                    RequireCurrentSession(sessionGeneration, userId, ct);
                }
                return new(text, result.DetectedLanguage, result.Usage);
            }
            catch (ApiException ex) when (ex.Code == "CONFLICT" && ex.DetailString("field") == "idempotency_key" &&
                ex.DetailString("reason") == "in_progress" && conflicts++ < 2)
            {
                RequireCurrentSession(sessionGeneration, userId, ct);
                await delay(TimeSpan.FromSeconds(2), ct).ConfigureAwait(false);
            }
            catch (ApiException ex) when (ex.Code == "PRO_DEVICE_IDENTITY_REQUIRED" && !healed &&
                api.Session?.InstallationId == api.InstallationId && !registrar.IsConfirmedForCurrentSession)
            {
                RequireCurrentSession(sessionGeneration, userId, ct);
                healed = true;
                var synced = await registrar.SyncAsync(force: true, ct).ConfigureAwait(false);
                if (synced is not (InstallationOutcome.Registered or InstallationOutcome.Unchanged)) { throw Map(ex); }
            }
            catch (ApiException ex) when (ex.Failure == ApiFailure.Cancelled)
            {
                RequireCurrentSession(sessionGeneration, userId, ct);
                throw;
            }
            catch (ApiException ex) when (ex.Failure != ApiFailure.Cancelled)
            {
                RequireCurrentSession(sessionGeneration, userId, ct);
                if (ex.Code is "CONSENT_REQUIRED" or "DAILY_LIMIT_REACHED" or "MONTHLY_LIMIT_REACHED" or "PAYMENT_REQUIRED" or "SUBSCRIPTION_EXPIRED")
                {
                    try { await account.RefreshMeAsync(ct: ct).ConfigureAwait(false); }
                    catch (ApiException) { }
                }
                RequireCurrentSession(sessionGeneration, userId, ct);
                throw Map(ex);
            }
        }
    }
    private void RequireCurrentSession(long generation, string userId, CancellationToken ct)
    {
        if (ct.IsCancellationRequested) throw new ApiException(ApiFailure.Cancelled);
        if (api.SessionGeneration != generation || api.Session?.UserId != userId)
            throw new GenerationException(GenerationErrorKind.SignedOut);
    }
    public static GenerationException Map(ApiException ex)
    {
        ArgumentNullException.ThrowIfNull(ex);
        var field = ex.DetailString("field");
        var kind = ex.Code switch
        {
            "UNAUTHORIZED" or "TOKEN_EXPIRED" => GenerationErrorKind.SignedOut,
            "CONSENT_REQUIRED" => GenerationErrorKind.ConsentRequired,
            "ACCOUNT_DISABLED" => GenerationErrorKind.AccountDisabled,
            "PRO_DEVICE_LIMIT_REACHED" => GenerationErrorKind.ProDeviceLimit,
            "PRO_DEVICE_IDENTITY_REQUIRED" => GenerationErrorKind.ProDeviceIdentity,
            "PRO_DEVICE_REVOKED" => GenerationErrorKind.ProDeviceRevoked,
            "DAILY_LIMIT_REACHED" or "MONTHLY_LIMIT_REACHED" or "PAYMENT_REQUIRED" or "SUBSCRIPTION_EXPIRED" => GenerationErrorKind.LimitReached,
            "RATE_LIMITED" => GenerationErrorKind.RateLimited,
            "AI_TIMEOUT" or "AI_PROVIDER_UNAVAILABLE" or "AI_EMPTY_RESPONSE" => GenerationErrorKind.AiUnavailable,
            "AI_OUTPUT_LIMIT_EXCEEDED" => GenerationErrorKind.OutputTooLong,
            "INVALID_REQUEST" when field == "source_text" => GenerationErrorKind.SourceTooLong,
            "INVALID_REQUEST" when field == "instruction" => ex.DetailInt("max_characters") is null ? GenerationErrorKind.InstructionMissing : GenerationErrorKind.InstructionTooLong,
            "INVALID_REQUEST" when field is "context" or "context.profile" or "context.references" =>
                ex.DetailString("reason") is "feature unavailable" or "requires an authenticated macOS session" or "requires an authenticated desktop session"
                    ? GenerationErrorKind.ContextUnavailable : GenerationErrorKind.ContextTooLarge,
            _ when ex.Failure == ApiFailure.SignedOut => GenerationErrorKind.SignedOut,
            _ when ex.Failure == ApiFailure.Offline => GenerationErrorKind.Offline,
            _ => GenerationErrorKind.Server,
        };
        return new(kind, ex)
        {
            Limit = ex.DetailInt("max_characters"), RetryAfter = ex.RetryAfter, ServerCode = ex.Code,
            ResetsAt = DateTimeOffset.TryParse(ex.DetailString("resets_at"), System.Globalization.CultureInfo.InvariantCulture, out var reset) ? reset : null,
            KeySpent = ex.Code == "CONFLICT" && ex.DetailString("reason") == "used",
        };
    }
    private sealed class Response
    {
        public string? Reply { get; init; }
        public string? Text { get; init; }
        public string? DetectedLanguage { get; init; }
        public UsageDto? Usage { get; init; }
    }
}
