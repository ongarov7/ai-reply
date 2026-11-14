using AirReply.Core.Accounts;
using AirReply.Core.Authentication;
using AirReply.Core.Generation;
using AirReply.Core.Installations;
using AirReply.Core.Localization;
using AirReply.Core.Networking;
using AirReply.Core.Voice;
using AirReply.Presentation.Models;

namespace AirReply.App;

internal sealed class BackendAdapter : IQuickBarBackend
{
    private readonly ApiClient api;
    private readonly AccountStore account;
    private readonly AuthSession auth;
    private readonly InstallationRegistrar registrar;
    private readonly Generator generator;
    private readonly Transcriber transcriber;
    private readonly Func<Localizer> localizer;
    private bool contextUnavailable;

    public BackendAdapter(ApiClient api, AccountStore account, InstallationRegistrar registrar, Func<Localizer> localizer)
    {
        this.api = api;
        this.account = account;
        this.registrar = registrar;
        this.localizer = localizer;
        auth = new AuthSession(api, account, registrar);
        generator = new Generator(api, account, registrar);
        transcriber = new Transcriber(api, registrar);
    }

    public AccountSnapshot Snapshot
    {
        get
        {
            var l = localizer();
            var balance = account.Balance;
            var balanceText = balance is null ? "" : balance.IsUnlimited ? l.Format("balance.unlimited", balance.PlanName)
                : balance.IsExhausted ? l["balance.empty"] : l.Format("balance.remaining", balance.RemainingTotal);
            return new(api.Session is not null, account.Me is not null, account.HasConsent == true,
                api.Session?.UserId, account.Me?.User.Email, balanceText, account.Config.Feature("voice_transcription"),
                !contextUnavailable && account.Config.Feature("desktop_context_generation"), account.Config.Feature("sender_profile"),
                account.Config.MaxSourceCharacters, account.Config.MaxInstructionLength, account.Config.Legal?.AiProvider ?? "OpenAI");
        }
    }

    public Task InitializeAsync(CancellationToken cancellationToken = default) => Wrap(async () =>
    {
        await account.RefreshConfigAsync(ct: cancellationToken);
        if (api.Session is not null)
        {
            await account.RefreshMeAsync(ct: cancellationToken);
            await registrar.SyncAsync(ct: cancellationToken);
        }
    });
    public Task RefreshAsync(CancellationToken cancellationToken = default) => Wrap(async () =>
    {
        await account.RefreshConfigAsync(ct: cancellationToken);
        await account.RefreshMeAsync(TimeSpan.FromSeconds(10), cancellationToken);
    });
    public Task RequestCodeAsync(string email, CancellationToken cancellationToken = default) => Wrap(async () => { await auth.RequestCodeAsync(email, cancellationToken); });
    public Task VerifyAsync(string code, CancellationToken cancellationToken = default) => Wrap(async () => { await auth.VerifyAsync(code, cancellationToken); });
    public Task ResendAsync(CancellationToken cancellationToken = default) => Wrap(async () => { await auth.ResendCodeAsync(cancellationToken); });
    public Task SignOutAsync(CancellationToken cancellationToken = default) => Wrap(() => auth.SignOutAsync(cancellationToken));
    public Task AcceptConsentAsync(CancellationToken cancellationToken = default) => Wrap(() => account.AcceptConsentAsync(cancellationToken));
    public Task<Uri> PlansUriAsync(CancellationToken cancellationToken = default) => account.PlansUriAsync(cancellationToken);

    public async Task<string> GenerateAsync(GenerationRequest request, CancellationToken cancellationToken = default)
    {
        try
        {
            var response = request.IsReply
                ? await generator.ReplyAsync(request.Source, request.Recipient, request.Instruction, request.InputLanguage, request.IdempotencyKey, request.Options, cancellationToken)
                : await generator.ComposeAsync(request.Instruction, request.Regenerate, request.InputLanguage, request.IdempotencyKey, request.Options, cancellationToken);
            return response.Text;
        }
        catch (GenerationException ex)
        {
            if (ex.Kind == GenerationErrorKind.ContextUnavailable) contextUnavailable = true;
            var key = ex.Kind switch
            {
                GenerationErrorKind.SourceTooLong => localizer().Format("issue.source_too_long", ex.Limit ?? account.Config.MaxSourceCharacters),
                GenerationErrorKind.InstructionTooLong => localizer().Format("issue.instruction_too_long", ex.Limit ?? account.Config.MaxInstructionLength),
                GenerationErrorKind.LimitReached when ex.ResetsAt is { } reset => localizer().Format("issue.limit.daily", reset.ToLocalTime().ToString("t", System.Globalization.CultureInfo.GetCultureInfo(localizer().LanguageCode))),
                _ => GenerationKey(ex.Kind),
            };
            throw new QuickBarException(key,
                ex.Kind is GenerationErrorKind.Offline or GenerationErrorKind.Server or GenerationErrorKind.AiUnavailable or GenerationErrorKind.RateLimited or GenerationErrorKind.OutputTooLong, ex, ex.KeySpent);
        }
        catch (ApiException ex) { throw Map(ex); }
    }

    public async Task<string> TranscribeAsync(byte[] wav, TimeSpan duration, IReadOnlyList<string> languages, CancellationToken cancellationToken = default)
    {
        try { return await transcriber.TranscribeAsync(wav, duration, languages, ct: cancellationToken); }
        catch (VoiceException ex)
        {
            var key = ex.Kind switch
            {
                VoiceErrorKind.TooShort => "voice.issue.too_short",
                VoiceErrorKind.Offline => "issue.offline",
                VoiceErrorKind.SignedOut => "issue.signed_out",
                VoiceErrorKind.Unavailable => "voice.issue.unavailable",
                VoiceErrorKind.LimitReached => "voice.issue.limit",
                VoiceErrorKind.RateLimited => "issue.rate_limited",
                VoiceErrorKind.ConsentRequired => "issue.consent",
                VoiceErrorKind.AccountDisabled => "issue.account_disabled",
                VoiceErrorKind.ProDeviceLimit => "issue.pro_device_limit",
                VoiceErrorKind.ProDeviceIdentity => "issue.pro_device_identity",
                VoiceErrorKind.ProDeviceRevoked => "issue.pro_device_revoked",
                _ => "voice.issue.server",
            };
            throw new QuickBarException(key, inner: ex);
        }
        catch (ApiException ex) { throw Map(ex); }
    }

    private async Task Wrap(Func<Task> action)
    {
        try { await action(); }
        catch (AuthException ex)
        {
            var key = ex.Kind switch
            {
                AuthErrorKind.InvalidEmail => "signin.error.email",
                AuthErrorKind.InvalidCode => "signin.error.code",
                AuthErrorKind.CodeExpired => "signin.error.expired",
                AuthErrorKind.CodeAlreadyUsed => "signin.error.used",
                AuthErrorKind.TooManyAttempts => "signin.error.attempts",
                AuthErrorKind.Cooldown => localizer().Format("signin.error.cooldown", ex.CooldownSeconds ?? 30),
                AuthErrorKind.RateLimited => "issue.rate_limited",
                AuthErrorKind.AccountDisabled => "issue.account_disabled",
                AuthErrorKind.EmailDelivery => "signin.error.delivery",
                AuthErrorKind.Offline => "issue.offline",
                _ => "issue.server",
            };
            throw new QuickBarException(key, inner: ex);
        }
        catch (ApiException ex) { throw Map(ex); }
    }

    private static QuickBarException Map(ApiException ex)
    {
        if (ex.Failure == ApiFailure.Cancelled) throw new OperationCanceledException("Operation cancelled.", ex);
        return new(ex.Failure == ApiFailure.Offline ? "issue.offline" : ex.Failure == ApiFailure.SignedOut ? "issue.signed_out" : "issue.server", true, ex);
    }

    private static string GenerationKey(GenerationErrorKind kind) => kind switch
    {
        GenerationErrorKind.SignedOut => "issue.signed_out",
        GenerationErrorKind.ConsentRequired => "issue.consent",
        GenerationErrorKind.AccountDisabled => "issue.account_disabled",
        GenerationErrorKind.ProDeviceLimit => "issue.pro_device_limit",
        GenerationErrorKind.ProDeviceIdentity => "issue.pro_device_identity",
        GenerationErrorKind.ProDeviceRevoked => "issue.pro_device_revoked",
        GenerationErrorKind.ContextTooLarge => "issue.context_too_large",
        GenerationErrorKind.ContextUnavailable => "issue.context_unavailable",
        GenerationErrorKind.OutputTooLong => "issue.output_too_long",
        GenerationErrorKind.LimitReached => "issue.limit.empty",
        GenerationErrorKind.RateLimited => "issue.rate_limited",
        GenerationErrorKind.AiUnavailable => "issue.ai_unavailable",
        GenerationErrorKind.InstructionMissing => "issue.instruction_missing",
        GenerationErrorKind.Offline => "issue.offline",
        _ => "issue.server",
    };
}
