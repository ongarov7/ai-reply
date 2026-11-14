using System.Globalization;
using System.Text;

namespace AirReply.Context;

public enum ContextDocumentFormat { Txt, Csv, Pdf, Docx, Xlsx }
public enum ContextImportError
{
    UnsupportedFormat, LegacyExcel, LegacyWord, MacroFormat, FileTooLarge, FileUnreadable, FileLocked,
    OfficeProtectedOrLegacy, PdfProtected, PdfNoText, PdfTooManyPages, Malformed, UnsafeContent,
    ArchiveTooLarge, TooMuchText, TooManyRows, TooManySheets, NoReadableText, BinaryOrUnknownEncoding,
    TooManyAttachments, ProfileAttachmentsFull, TooManyProfiles, StorageFull, StorageFailed, TimedOut,
}

public sealed class ContextImportException(ContextImportError error, string? detail = null, Exception? inner = null)
    : Exception(error.ToString() + (detail is null ? "" : ": " + detail), inner)
{
    public ContextImportError Error { get; } = error;
    public string? Detail { get; } = detail;
    public string MessageKey => "context.import.error." + (Error == ContextImportError.Malformed && Detail?.StartsWith("csv_quote", StringComparison.Ordinal) == true ? "csv_quote" : ToSnake(Error.ToString()));
    private static string ToSnake(string text) => string.Concat(text.Select((c, i) => char.IsUpper(c) && i > 0 ? "_" + char.ToLowerInvariant(c) : char.ToLowerInvariant(c).ToString()));
}

public sealed record ContextImportSummary
{
    public int Sheets { get; init; }
    public int Pages { get; init; }
    public int Rows { get; init; }
    public int FormulaValues { get; init; }
    public int FormulasSkipped { get; init; }
    public int HiddenSkipped { get; init; }
    public int Shortened { get; init; }
    public string? Encoding { get; init; }
    public IReadOnlyList<string> InternalColumnsSkipped { get; init; } = [];
}

public sealed record ContextCell(int Column, string Header, string Value, bool Formula = false);

public sealed record ContextSegment(string Text, string Locator)
{
    public string? Table { get; init; }
    public string? Name { get; init; }
    public string? Key { get; init; }
    public IReadOnlyList<ContextCell> Cells { get; init; } = [];
}

public sealed record ContextAttachment(Guid Id, string Name, ContextDocumentFormat Format, DateTimeOffset ImportedAt,
    long SourceBytes, string Sha256, long StoredBytes, int SegmentCount, int TextCharacters, ContextImportSummary Summary);

public sealed record ContextDocument(ContextAttachment Attachment, IReadOnlyList<ContextSegment> Segments);

public sealed record ContextProfile
{
    public Guid Id { get; init; } = Guid.NewGuid();
    public string Name { get; init; } = "";
    public string Role { get; init; } = "personal";
    public string Instructions { get; init; } = "";
    public string CommunicationStyle { get; init; } = "";
    public string PersonName { get; init; } = "";
    public string Occupation { get; init; } = "";
    public string Company { get; init; } = "";
    public string Industry { get; init; } = "";
    public string Policies { get; init; } = "";
    public string Products { get; init; } = "";
    public string Services { get; init; } = "";
    public string Description { get; init; } = "";
    public DateTimeOffset CreatedAt { get; init; } = DateTimeOffset.UtcNow;
    public DateTimeOffset UpdatedAt { get; init; } = DateTimeOffset.UtcNow;
    public IReadOnlyList<ContextAttachment> Attachments { get; init; } = [];

    public void Validate()
    {
        if (Id == Guid.Empty || string.IsNullOrWhiteSpace(Name) || new StringInfo(Name.Trim()).LengthInTextElements > 80 ||
            Role is not ("personal" or "business") || Attachments is null || Attachments.Count > 8 || Attachments.Any(a => a is null || a.Id == Guid.Empty || string.IsNullOrWhiteSpace(a.Name) || a.Name.IndexOfAny(['/', '\\']) >= 0 ||
                a.SourceBytes < 0 || a.SourceBytes > 8 * 1024 * 1024 || a.StoredBytes < 0 || a.StoredBytes > 16 * 1024 * 1024 || a.SegmentCount < 0 || a.SegmentCount > 50000) ||
            Attachments.Select(a => a.Id).Distinct().Count() != Attachments.Count)
            throw new ArgumentException("Invalid context profile.");
        foreach (var field in new[] { Instructions, Policies, Products, Services, Description }) Check(field, 4000);
        foreach (var field in new[] { PersonName, Occupation, Company, Industry }) Check(field, 120);
        Check(CommunicationStyle, 300);
    }

    private static void Check(string value, int limit)
    {
        if (value is null || ContextText.Runes(value) > limit) throw new ArgumentException("Profile field exceeds its character limit.");
    }
}

internal static class ContextText
{
    public static int Runes(string value) => value.EnumerateRunes().Count();
    public static string Take(string value, int runes) => string.Concat(value.EnumerateRunes().Take(Math.Max(0, runes)).Select(r => r.ToString()));
    public static string Valid(string text) => string.Concat(text.EnumerateRunes().Select(r => r.ToString()));
    public static string Compact(string text) => string.Join(' ', text.Split((char[]?)null, StringSplitOptions.RemoveEmptyEntries));
    public static string Normalize(string text)
    {
        var sb = new StringBuilder();
        foreach (var rune in Valid(text).Normalize(NormalizationForm.FormKD).EnumerateRunes())
            if (Rune.GetUnicodeCategory(rune) != UnicodeCategory.NonSpacingMark) sb.Append(Rune.ToLowerInvariant(rune));
        return sb.ToString().Normalize(NormalizationForm.FormC);
    }
}
