using System.Security.Cryptography;

namespace AirReply.Core.Infrastructure;

/// <summary>
/// Encrypts data so only this Windows user on this machine can read it back. Windows: DPAPI (CurrentUser) with
/// a purpose-specific entropy. Unprotect throws <see cref="CryptographicException"/> for foreign or damaged data.
/// </summary>
public interface IDataProtector
{
    byte[] Protect(ReadOnlySpan<byte> plaintext);
    byte[] Unprotect(ReadOnlySpan<byte> protectedData);
}

/// <summary>AES-GCM with an in-memory key: for tests and for running the UI on non-Windows development machines.</summary>
public sealed class EphemeralDataProtector : IDataProtector
{
    private const int NonceSize = 12;
    private const int TagSize = 16;
    private readonly byte[] key;

    public EphemeralDataProtector(byte[]? key = null) => this.key = key ?? RandomNumberGenerator.GetBytes(32);

    public byte[] Protect(ReadOnlySpan<byte> plaintext)
    {
        var output = new byte[NonceSize + TagSize + plaintext.Length];
        var nonce = output.AsSpan(0, NonceSize);
        RandomNumberGenerator.Fill(nonce);
        using var aes = new AesGcm(key, TagSize);
        aes.Encrypt(nonce, plaintext, output.AsSpan(NonceSize + TagSize), output.AsSpan(NonceSize, TagSize));
        return output;
    }

    public byte[] Unprotect(ReadOnlySpan<byte> protectedData)
    {
        if (protectedData.Length < NonceSize + TagSize)
        {
            throw new CryptographicException("Protected data is too short.");
        }
        var plaintext = new byte[protectedData.Length - NonceSize - TagSize];
        using var aes = new AesGcm(key, TagSize);
        aes.Decrypt(protectedData[..NonceSize], protectedData[(NonceSize + TagSize)..], protectedData.Slice(NonceSize, TagSize), plaintext);
        return plaintext;
    }
}
