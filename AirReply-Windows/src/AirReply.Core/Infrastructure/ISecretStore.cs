namespace AirReply.Core.Infrastructure;

/// <summary>
/// Small secrets that must stay on this machine for this Windows user: tokens, device id, installation id,
/// encryption keys. The Windows implementation protects them with DPAPI (CurrentUser).
/// </summary>
public interface ISecretStore
{
    string? Read(string key);
    void Write(string key, string value);
    void Delete(string key);
}

/// <summary>For tests and for running the UI on non-Windows development machines.</summary>
public sealed class InMemorySecretStore : ISecretStore
{
    private readonly Dictionary<string, string> values = new(StringComparer.Ordinal);
    private readonly Lock gate = new();

    public string? Read(string key) { lock (gate) { return values.GetValueOrDefault(key); } }
    public void Write(string key, string value) { lock (gate) { values[key] = value; } }
    public void Delete(string key) { lock (gate) { values.Remove(key); } }
}
