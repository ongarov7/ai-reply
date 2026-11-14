using System.Buffers.Binary;
using System.IO.Compression;
using System.Text;
using System.Text.RegularExpressions;
using System.Xml;

namespace AirReply.Context;

internal static partial class OfficePreflight
{
    private const int ExpandedCap = 16 * 1024 * 1024;
    public static void Validate(byte[] bytes, CancellationToken ct)
    {
        var span = bytes.AsSpan();
        if (span.StartsWith(new byte[] { 0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1 })) throw Error(ContextImportError.OfficeProtectedOrLegacy);
        if (!span.StartsWith("PK\x03\x04"u8)) throw Error(ContextImportError.Malformed);
        var end = -1;
        for (var i = bytes.Length - 22; i >= Math.Max(0, bytes.Length - 65557); i--)
            if (U32(span, i) == 0x06054b50 && i + 22 + U16(span, i + 20) == bytes.Length) { end = i; break; }
        if (end < 0) throw Error(ContextImportError.Malformed);
        var count = U16(span, end + 10);
        if (count > 2048) throw Error(ContextImportError.ArchiveTooLarge);
        if (U16(span, end + 4) != 0 || U16(span, end + 6) != 0 || U16(span, end + 8) != count) throw Error(ContextImportError.UnsafeContent);
        var offset = checked((int)U32(span, end + 16));
        var size = U32(span, end + 12);
        if (offset < 0 || (long)offset + size != end) throw Error(ContextImportError.Malformed);
        var entries = new List<Entry>(count);
        var names = new HashSet<string>(StringComparer.Ordinal);
        long expanded = 0;
        for (var n = 0; n < count; n++)
        {
            ct.ThrowIfCancellationRequested();
            if (offset + 46 > end || U32(span, offset) != 0x02014b50) throw Error(ContextImportError.Malformed);
            var flags = U16(span, offset + 8);
            var method = U16(span, offset + 10);
            var crc = U32(span, offset + 16);
            var compressed = U32(span, offset + 20);
            var uncompressed = U32(span, offset + 24);
            var nameLength = U16(span, offset + 28);
            var extraLength = U16(span, offset + 30);
            var commentLength = U16(span, offset + 32);
            var attributes = U32(span, offset + 38);
            var local = U32(span, offset + 42);
            if ((flags & 0x41) != 0 || method is not (0 or 8) || U16(span, offset + 34) != 0 ||
                compressed == uint.MaxValue || uncompressed == uint.MaxValue || local >= U32(span, end + 16) || nameLength == 0 ||
                offset + 46 + nameLength + extraLength + commentLength > end) throw Error(ContextImportError.UnsafeContent);
            string name;
            try { name = new UTF8Encoding(false, true).GetString(span.Slice(offset + 46, nameLength)); }
            catch (DecoderFallbackException) { throw Error(ContextImportError.UnsafeContent); }
            if (name.StartsWith('/') || name.IndexOfAny(['\\', ':', '\0']) >= 0 || name.Split('/').Any(p => p is "." or "..") || !names.Add(name) ||
                ((attributes >> 16) & 0xf000) is not (0 or 0x8000 or 0x4000) || name.Contains("vbaproject", StringComparison.OrdinalIgnoreCase) || name.EndsWith(".exe", StringComparison.OrdinalIgnoreCase))
                throw Error(ContextImportError.UnsafeContent);
            expanded += uncompressed;
            if (expanded > ExpandedCap) throw Error(ContextImportError.ArchiveTooLarge);
            entries.Add(new Entry(name, flags, method, crc, compressed, uncompressed, (int)local));
            offset += 46 + nameLength + extraLength + commentLength;
        }
        if (offset != end) throw Error(ContextImportError.Malformed);
        foreach (var entry in entries)
        {
            ct.ThrowIfCancellationRequested();
            var local = entry.Offset;
            if (local < 0 || local + 30 > end || U32(span, local) != 0x04034b50 || U16(span, local + 6) != entry.Flags || U16(span, local + 8) != entry.Method)
                throw Error(ContextImportError.Malformed);
            var nameLength = U16(span, local + 26);
            var dataOffset = local + 30 + nameLength + U16(span, local + 28);
            if (dataOffset + (long)entry.Compressed > U32(span, end + 16) || local + 30 + nameLength > bytes.Length ||
                Encoding.UTF8.GetString(span.Slice(local + 30, nameLength)) != entry.Name) throw Error(ContextImportError.Malformed);
            using var input = new MemoryStream(bytes, dataOffset, (int)entry.Compressed, false);
            using var output = new MemoryStream();
            if (entry.Method == 0)
            {
                if (entry.Compressed != entry.Uncompressed) throw Error(ContextImportError.Malformed);
                output.Write(span.Slice(dataOffset, (int)entry.Compressed));
            }
            else
            {
                using var deflate = new DeflateStream(input, CompressionMode.Decompress, true);
                var buffer = new byte[8192];
                int read;
                while ((read = deflate.Read(buffer)) > 0)
                {
                    ct.ThrowIfCancellationRequested();
                    if (output.Length + read > entry.Uncompressed) throw Error(ContextImportError.Malformed);
                    output.Write(buffer, 0, read);
                }
            }
            if (output.Length != entry.Uncompressed) throw Error(ContextImportError.Malformed);
            var data = output.ToArray();
            if (Crc(data) != entry.Crc) throw Error(ContextImportError.Malformed);
            if (entry.Name.EndsWith(".xml", StringComparison.OrdinalIgnoreCase) || entry.Name.EndsWith(".rels", StringComparison.OrdinalIgnoreCase)) ValidateXml(data, ct);
        }
    }

    private static void ValidateXml(byte[] bytes, CancellationToken ct)
    {
        var text = bytes.AsSpan().StartsWith(new byte[] { 0xff, 0xfe }) ? Encoding.Unicode.GetString(bytes) :
            bytes.AsSpan().StartsWith(new byte[] { 0xfe, 0xff }) ? Encoding.BigEndianUnicode.GetString(bytes) : Encoding.UTF8.GetString(bytes);
        if (text.Contains("<!DOCTYPE", StringComparison.OrdinalIgnoreCase) || text.Contains("<!ENTITY", StringComparison.OrdinalIgnoreCase) ||
            text.Contains('\0') && !bytes.AsSpan().StartsWith(new byte[] { 0xff, 0xfe }) && !bytes.AsSpan().StartsWith(new byte[] { 0xfe, 0xff }))
            throw Error(ContextImportError.UnsafeContent);
        var encoding = XmlEncoding().Match(text);
        if (encoding.Success && !encoding.Groups[1].Value.Equals("UTF-8", StringComparison.OrdinalIgnoreCase) && !encoding.Groups[1].Value.Equals("UTF-16", StringComparison.OrdinalIgnoreCase))
            throw Error(ContextImportError.UnsafeContent);
        using var input = new MemoryStream(bytes, false);
        using var reader = XmlReader.Create(input, new XmlReaderSettings { DtdProcessing = DtdProcessing.Prohibit, XmlResolver = null, MaxCharactersFromEntities = 0, MaxCharactersInDocument = ExpandedCap });
        var elements = 0;
        while (reader.Read())
        {
            if ((elements & 1023) == 0) ct.ThrowIfCancellationRequested();
            if (reader.Depth > 64 || reader.NodeType == XmlNodeType.Element && ++elements > 4000000) throw Error(ContextImportError.ArchiveTooLarge);
        }
    }
    [GeneratedRegex("<\\?xml[^>]*encoding\\s*=\\s*['\"]([^'\"]+)['\"]", RegexOptions.IgnoreCase | RegexOptions.CultureInvariant)] private static partial Regex XmlEncoding();
    private static ushort U16(ReadOnlySpan<byte> bytes, int offset) => BinaryPrimitives.ReadUInt16LittleEndian(bytes.Slice(offset, 2));
    private static uint U32(ReadOnlySpan<byte> bytes, int offset) => BinaryPrimitives.ReadUInt32LittleEndian(bytes.Slice(offset, 4));
    private static uint Crc(byte[] bytes)
    {
        uint crc = uint.MaxValue;
        foreach (var b in bytes)
        {
            crc ^= b;
            for (var i = 0; i < 8; i++) crc = (crc >> 1) ^ ((crc & 1) == 0 ? 0 : 0xedb88320);
        }
        return ~crc;
    }
    private static ContextImportException Error(ContextImportError error) => new(error);
    private sealed record Entry(string Name, int Flags, int Method, uint Crc, uint Compressed, uint Uncompressed, int Offset);
}

internal static partial class PdfSafety
{
    public static void Check(byte[] bytes, CancellationToken ct)
    {
        var text = Encoding.Latin1.GetString(bytes);
        var position = 0;
        long total = 0;
        while ((position = text.IndexOf("stream", position, StringComparison.Ordinal)) >= 0)
        {
            ct.ThrowIfCancellationRequested();
            var dictionary = text.LastIndexOf("<<", position, Math.Min(position + 1, 4096), StringComparison.Ordinal);
            var end = text.IndexOf("endstream", position + 6, StringComparison.Ordinal);
            if (end < 0) break;
            var dictionaryText = dictionary >= 0 ? text[dictionary..position] : "";
            if (FlateFilter().IsMatch(dictionaryText) && !ImageSubtype().IsMatch(dictionaryText))
            {
                var start = position + 6;
                if (start < text.Length && text[start] == '\r') start++;
                if (start < text.Length && text[start] == '\n') start++;
                var counted = Count(bytes, start, end - start, true, ct);
                if (!counted.HasValue) counted = Count(bytes, start, end - start, false, ct);
                total += counted ?? 0;
                if (total > 128 * 1024 * 1024) throw new ContextImportException(ContextImportError.UnsafeContent);
            }
            position = end + 9;
        }
    }
    private static long? Count(byte[] bytes, int start, int length, bool zlib, CancellationToken ct)
    {
        try
        {
            using var input = new MemoryStream(bytes, start, length, false);
            using Stream inflate = zlib ? new ZLibStream(input, CompressionMode.Decompress) : new DeflateStream(input, CompressionMode.Decompress);
            var buffer = new byte[8192];
            long size = 0;
            int read;
            while ((read = inflate.Read(buffer)) > 0)
            {
                ct.ThrowIfCancellationRequested();
                size += read;
                if (size > 32 * 1024 * 1024) throw new ContextImportException(ContextImportError.UnsafeContent);
            }
            return size;
        }
        catch (InvalidDataException) { return null; }
    }
    [GeneratedRegex(@"/(?:FlateDecode|Fl)\b", RegexOptions.CultureInvariant)] private static partial Regex FlateFilter();
    [GeneratedRegex(@"/Subtype\s*/Image\b", RegexOptions.CultureInvariant)] private static partial Regex ImageSubtype();
}
