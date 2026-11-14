using System.ComponentModel;
using System.Runtime.InteropServices;
using System.Runtime.Versioning;
using System.Security.Cryptography;
using System.Text;
using AirReply.Core.Infrastructure;

namespace AirReply.Platform.Windows;

[SupportedOSPlatform("windows")]
public sealed class WindowsDataProtector(string purpose) : IDataProtector
{
    private readonly byte[] entropy = Encoding.UTF8.GetBytes(purpose);
    public byte[] Protect(ReadOnlySpan<byte> plaintext)
    {
        var bytes = plaintext.ToArray();
        try { return ProtectedData.Protect(bytes, entropy, DataProtectionScope.CurrentUser); }
        finally { CryptographicOperations.ZeroMemory(bytes); }
    }
    public byte[] Unprotect(ReadOnlySpan<byte> protectedData) => ProtectedData.Unprotect(protectedData.ToArray(), entropy, DataProtectionScope.CurrentUser);
}

/// <summary>Machine-local CurrentUser DPAPI storage. Full keys are entropy-bound and filenames never contain user paths.</summary>
[SupportedOSPlatform("windows")]
public sealed class WindowsSecretStore : ISecretStore
{
    private readonly string environment;
    private readonly string directory;
    private readonly Lock gate = new();
    public WindowsSecretStore(string environment = "production", string? rootDirectory = null)
    {
        WindowsMessageWindow.ValidateEnvironment(environment);
        this.environment = environment;
        directory = Path.Combine(rootDirectory ?? Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "AI Reply"), environment, "secure");
    }
    internal static string FileName(string key)
    {
        ArgumentException.ThrowIfNullOrWhiteSpace(key);
        // ApiClient scopes keys with "environment/key". Accept exactly those safe segments, never path traversal.
        if (key.Length > 160 || key.Split('/').Any(segment => segment.Length is 0 or > 64 ||
            !char.IsAsciiLetterOrDigit(segment[0]) || segment.Any(c => !char.IsAsciiLetterOrDigit(c) && c is not '.' and not '-' and not '_') || segment is "." or ".."))
            throw new ArgumentException("Invalid secret key.", nameof(key));
        return Convert.ToHexStringLower(SHA256.HashData(Encoding.UTF8.GetBytes(key))) + ".bin";
    }
    public string? Read(string key)
    {
        var path = Path.Combine(directory, FileName(key));
        lock (gate)
        {
            byte[]? plaintext = null;
            try
            {
                if (!File.Exists(path)) return null;
                plaintext = ProtectedData.Unprotect(File.ReadAllBytes(path), Entropy(key), DataProtectionScope.CurrentUser);
                return Encoding.UTF8.GetString(plaintext);
            }
            catch (Exception exception) when (exception is CryptographicException or IOException or UnauthorizedAccessException)
            { return null; }
            finally { if (plaintext != null) CryptographicOperations.ZeroMemory(plaintext); }
        }
    }
    public void Write(string key, string value)
    {
        ArgumentNullException.ThrowIfNull(value);
        var path = Path.Combine(directory, FileName(key));
        lock (gate)
        {
            Directory.CreateDirectory(directory);
            var plaintext = Encoding.UTF8.GetBytes(value);
            var temporary = path + "." + Guid.NewGuid().ToString("N") + ".tmp";
            try
            {
                var encrypted = ProtectedData.Protect(plaintext, Entropy(key), DataProtectionScope.CurrentUser);
                using (var file = new FileStream(temporary, FileMode.CreateNew, FileAccess.Write, FileShare.None))
                { file.Write(encrypted); file.Flush(true); }
                if (!NativeMethods.MoveFileEx(temporary, path, 1 | 8)) throw new Win32Exception(Marshal.GetLastPInvokeError());
            }
            finally
            {
                CryptographicOperations.ZeroMemory(plaintext);
                if (File.Exists(temporary)) File.Delete(temporary);
            }
        }
    }
    public void Delete(string key)
    {
        var path = Path.Combine(directory, FileName(key));
        lock (gate) { File.Delete(path); }
    }
    private byte[] Entropy(string key) => Encoding.UTF8.GetBytes($"AIReply/{environment}/{key}/v1");
}
