using AirReply.Core.Generation;

namespace AirReply.Presentation.Models;

public sealed record AccountSnapshot(bool SignedIn = false, bool Ready = false, bool HasConsent = false,
    string? UserId = null, string? Email = null, string Balance = "", bool VoiceAvailable = false,
    bool ContextAvailable = false, bool SenderProfile = false, int SourceLimit = 400, int InstructionLimit = 400,
    string Provider = "OpenAI");

/// <summary>An immutable snapshot: retry must use exactly the original request and idempotency key.</summary>
public sealed record GenerationRequest(string Source, string Instruction, Recipient Recipient, bool IsReply,
    bool Regenerate, string? InputLanguage, string IdempotencyKey, GenerationOptions Options);

public sealed class QuickBarException(string key, bool retryable = false, Exception? inner = null, bool keySpent = false)
    : Exception(key, inner)
{
    public string Key { get; } = key;
    public bool Retryable { get; } = retryable;
    public bool KeySpent { get; } = keySpent;
}

public interface IQuickBarBackend
{
    AccountSnapshot Snapshot { get; }
    Task InitializeAsync(CancellationToken cancellationToken = default);
    Task RefreshAsync(CancellationToken cancellationToken = default);
    Task RequestCodeAsync(string email, CancellationToken cancellationToken = default);
    Task VerifyAsync(string code, CancellationToken cancellationToken = default);
    Task ResendAsync(CancellationToken cancellationToken = default);
    Task SignOutAsync(CancellationToken cancellationToken = default);
    Task AcceptConsentAsync(CancellationToken cancellationToken = default);
    Task<string> GenerateAsync(GenerationRequest request, CancellationToken cancellationToken = default);
    Task<string> TranscribeAsync(byte[] wav, TimeSpan duration, IReadOnlyList<string> languages,
        CancellationToken cancellationToken = default);
    Task<Uri> PlansUriAsync(CancellationToken cancellationToken = default);
}
