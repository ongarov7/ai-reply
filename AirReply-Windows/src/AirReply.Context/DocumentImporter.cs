using System.Globalization;
using System.Security.Cryptography;
using System.Text;
using System.Text.RegularExpressions;
using DocumentFormat.OpenXml;
using DocumentFormat.OpenXml.Packaging;
using DocumentFormat.OpenXml.Spreadsheet;
using UglyToad.PdfPig;
using UglyToad.PdfPig.DocumentLayoutAnalysis.TextExtractor;
using W = DocumentFormat.OpenXml.Wordprocessing;

namespace AirReply.Context;

/// <summary>Imports local text only; never evaluates formulas, follows links, or sends source files.</summary>
public sealed partial class DocumentImporter
{
    private const int MaxBytes = 8 * 1024 * 1024;
    private static readonly char[] Separators = [';', ',', '\t'];
    private readonly TimeSpan importTimeout = TimeSpan.FromSeconds(60);
    private static readonly OpenSettings OpenSettings = new() { AutoSave = false, MaxCharactersInPart = 16_777_216 };

    public async Task<ContextDocument> ImportAsync(string path, CancellationToken cancellationToken = default)
    {
        ArgumentException.ThrowIfNullOrWhiteSpace(path);
        var format = Format(Path.GetExtension(path));
        byte[] bytes;
        try
        {
            var info = new FileInfo(path);
            if ((info.Attributes & (FileAttributes.Directory | FileAttributes.ReparsePoint)) != 0) throw Error(ContextImportError.FileUnreadable);
            if (info.Length > MaxBytes) throw Error(ContextImportError.FileTooLarge);
            await using var stream = new FileStream(path, FileMode.Open, FileAccess.Read, FileShare.ReadWrite | FileShare.Delete, 8192, FileOptions.Asynchronous);
            using var memory = new MemoryStream();
            var buffer = new byte[8192];
            int read;
            while ((read = await stream.ReadAsync(buffer, cancellationToken).ConfigureAwait(false)) > 0)
            {
                if (memory.Length + read > MaxBytes) throw Error(ContextImportError.FileTooLarge);
                await memory.WriteAsync(buffer.AsMemory(0, read), cancellationToken).ConfigureAwait(false);
            }
            bytes = memory.ToArray();
        }
        catch (IOException ex) { throw new ContextImportException((ex.HResult & 0xffff) is 32 or 33 ? ContextImportError.FileLocked : ContextImportError.FileUnreadable, inner: ex); }
        catch (UnauthorizedAccessException ex) { throw new ContextImportException(ContextImportError.FileUnreadable, inner: ex); }
        return await ImportBytesAsync(Path.GetFileName(path), format, bytes, cancellationToken).ConfigureAwait(false);
    }

    public async Task<ContextDocument> ImportBytesAsync(string name, ContextDocumentFormat format, byte[] bytes, CancellationToken cancellationToken = default)
    {
        ArgumentNullException.ThrowIfNull(bytes);
        if (bytes.Length > MaxBytes) throw Error(ContextImportError.FileTooLarge);
        using var timeout = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken);
        timeout.CancelAfter(importTimeout);
        try
        {
            return await Task.Run(() => Parse(name, format, bytes, timeout.Token), timeout.Token).ConfigureAwait(false);
        }
        catch (OperationCanceledException) when (!cancellationToken.IsCancellationRequested) { throw Error(ContextImportError.TimedOut); }
    }

    private static ContextDocument Parse(string name, ContextDocumentFormat format, byte[] bytes, CancellationToken ct)
    {
        var state = new ImportState(ct);
        try
        {
            switch (format)
            {
                case ContextDocumentFormat.Txt:
                case ContextDocumentFormat.Csv:
                    var text = Decode(bytes, state);
                    if (format == ContextDocumentFormat.Csv || IsTabular(text)) ReadCsv(text, state);
                    else ReadText(text, "line", state);
                    break;
                case ContextDocumentFormat.Xlsx:
                    OfficePreflight.Validate(bytes, ct);
                    ReadXlsx(bytes, state);
                    break;
                case ContextDocumentFormat.Docx:
                    OfficePreflight.Validate(bytes, ct);
                    ReadDocx(bytes, state);
                    break;
                case ContextDocumentFormat.Pdf:
                    ReadPdf(bytes, state);
                    break;
                default: throw Error(ContextImportError.UnsupportedFormat);
            }
        }
        catch (Exception ex) when (ex is OpenXmlPackageException or InvalidDataException or System.Xml.XmlException or FormatException or ArgumentOutOfRangeException or OverflowException or IndexOutOfRangeException or InvalidOperationException)
        { throw new ContextImportException(ContextImportError.Malformed, inner: ex); }
        state.Check();
        if (state.Segments.Count == 0) throw Error(ContextImportError.NoReadableText);
        var id = Guid.NewGuid();
        var summary = new ContextImportSummary
        {
            Rows = state.Segments.Count, Sheets = state.Sheets, Pages = state.Pages, FormulaValues = state.FormulaValues,
            FormulasSkipped = state.FormulasSkipped, HiddenSkipped = state.HiddenSkipped, Shortened = state.Shortened,
            InternalColumnsSkipped = state.InternalColumns.Distinct(StringComparer.OrdinalIgnoreCase).ToArray(), Encoding = state.Encoding,
        };
        return new ContextDocument(new ContextAttachment(id, ContextText.Take(Path.GetFileName(name), 200), format,
            DateTimeOffset.Now, bytes.Length, Convert.ToHexStringLower(SHA256.HashData(bytes)), 0, state.Segments.Count,
            state.Characters, summary), state.Segments.ToArray());
    }

    private static ContextDocumentFormat Format(string extension) => extension.ToLowerInvariant() switch
    {
        ".xlsx" => ContextDocumentFormat.Xlsx, ".docx" => ContextDocumentFormat.Docx, ".pdf" => ContextDocumentFormat.Pdf,
        ".txt" => ContextDocumentFormat.Txt, ".csv" => ContextDocumentFormat.Csv,
        ".xls" or ".xlt" => throw Error(ContextImportError.LegacyExcel),
        ".doc" or ".dot" => throw Error(ContextImportError.LegacyWord),
        ".xlsm" or ".xlsb" or ".xltm" or ".docm" or ".dotm" => throw Error(ContextImportError.MacroFormat),
        _ => throw Error(ContextImportError.UnsupportedFormat),
    };

    private static string Decode(byte[] bytes, ImportState state)
    {
        Encoding.RegisterProvider(CodePagesEncodingProvider.Instance);
        string text;
        try
        {
            if (bytes.AsSpan().StartsWith(new byte[] { 0xff, 0xfe, 0, 0 })) text = new UTF32Encoding(false, true, true).GetString(bytes, 4, bytes.Length - 4);
            else if (bytes.AsSpan().StartsWith(new byte[] { 0, 0, 0xfe, 0xff })) text = new UTF32Encoding(true, true, true).GetString(bytes, 4, bytes.Length - 4);
            else if (bytes.AsSpan().StartsWith(new byte[] { 0xef, 0xbb, 0xbf })) text = new UTF8Encoding(false, true).GetString(bytes, 3, bytes.Length - 3);
            else if (bytes.AsSpan().StartsWith(new byte[] { 0xff, 0xfe })) text = new UnicodeEncoding(false, true, true).GetString(bytes, 2, bytes.Length - 2);
            else if (bytes.AsSpan().StartsWith(new byte[] { 0xfe, 0xff })) text = new UnicodeEncoding(true, true, true).GetString(bytes, 2, bytes.Length - 2);
            else if (LooksUtf16(bytes, false)) text = new UnicodeEncoding(false, false, true).GetString(bytes);
            else if (LooksUtf16(bytes, true)) text = new UnicodeEncoding(true, false, true).GetString(bytes);
            else
            {
                try { text = new UTF8Encoding(false, true).GetString(bytes); }
                catch (DecoderFallbackException) { text = Encoding.GetEncoding(1251, EncoderFallback.ExceptionFallback, DecoderFallback.ExceptionFallback).GetString(bytes); state.Encoding = "Windows-1251"; }
            }
        }
        catch (DecoderFallbackException) { throw Error(ContextImportError.BinaryOrUnknownEncoding); }
        foreach (var c in text)
            if ((c < ' ' && c is not ('\t' or '\n' or '\v' or '\f' or '\r')) || (c >= '\x7f' && c <= '\x9f' && c != '\x85'))
                throw Error(ContextImportError.BinaryOrUnknownEncoding);
        return text.Replace("\r\n", "\n", StringComparison.Ordinal).Replace('\r', '\n').Replace('\x85', '\n').Replace('\u2028', '\n').Replace('\u2029', '\n')
            .Replace("\u00ad", "", StringComparison.Ordinal).Replace("\u200b", "", StringComparison.Ordinal).Replace("\u2060", "", StringComparison.Ordinal).Replace("\ufeff", "", StringComparison.Ordinal);
    }

    private static bool LooksUtf16(byte[] bytes, bool bigEndian)
    {
        if (bytes.Length < 4 || bytes.Length % 2 != 0) return false;
        var pairs = Math.Min(4096, bytes.Length) / 2;
        var high = 0;
        var lowNull = 0;
        for (var i = 0; i < pairs; i++)
        {
            if (bytes[i * 2 + (bigEndian ? 0 : 1)] is 0 or 4) high++;
            if (bytes[i * 2 + (bigEndian ? 1 : 0)] == 0) lowNull++;
        }
        return high >= pairs * .6 && lowNull < pairs * .05;
    }

    private static bool IsTabular(string text)
    {
        var lines = text.Split('\n', StringSplitOptions.RemoveEmptyEntries).Take(20).ToArray();
        return lines.Length > 0 && lines.Count(l => l.Contains('\t', StringComparison.Ordinal)) >= lines.Length * .8;
    }

    private static void ReadText(string text, string unit, ImportState state, int? page = null)
    {
        var lines = text.Split('\n');
        string? heading = null;
        for (var i = 0; i < lines.Length; i++)
        {
            state.Check();
            var line = ContextText.Compact(lines[i]);
            if (line.Length == 0) { heading = null; continue; }
            if (IsHeading(line) && i + 1 < lines.Length && (ContextText.Runes(lines[i + 1]) > 60 || lines[i + 1].TrimEnd().EndsWith('.')))
            { heading = line; continue; }
            AddText(line, page.HasValue ? $"page {page.Value}" : $"{unit} {i + 1}", state, heading);
        }
    }

    private static bool IsHeading(string line) => ContextText.Runes(line) <= 60 && line.Split(' ').Length <= 6 && !".!?;,".Contains(line[^1], StringComparison.Ordinal);

    private static void AddText(string text, string locator, ImportState state, string? heading = null)
    {
        var words = text.Split((char[]?)null, StringSplitOptions.RemoveEmptyEntries);
        var chunks = new List<string>();
        var chunk = "";
        foreach (var word in words)
        {
            var length = ContextText.Runes(word);
            if (length > 2000) { state.Shortened++; continue; }
            if (length > 1200)
            {
                if (chunk.Length > 0) { chunks.Add(chunk); chunk = ""; }
                chunks.Add(ContextText.Take(word, 1200));
                state.Shortened++;
                continue;
            }
            if (ContextText.Runes(chunk) + length + 1 > 1200) { chunks.Add(chunk); chunk = ""; }
            chunk += (chunk.Length > 0 ? " " : "") + word;
        }
        if (chunk.Length > 0) chunks.Add(chunk);
        for (var i = 0; i < chunks.Count; i++)
        {
            var body = heading is null ? chunks[i] : heading + ": " + chunks[i];
            state.Add(new ContextSegment(body, locator + (i == 0 ? "" : $" (part {i + 1})")));
        }
    }

    private static void ReadCsv(string text, ImportState state)
    {
        var startLine = 1;
        char separator;
        if (text.StartsWith("sep=", StringComparison.OrdinalIgnoreCase) && text.Length > 5 && text[5] == '\n')
        { separator = text[4]; text = text[6..]; startLine = 2; }
        else
        {
            separator = Separators.Select((c, order) =>
            {
                var rows = CsvRows(text, c, false, state.Token).Take(20).ToArray();
                var mode = rows.Select(r => r.Values.Count).Where(n => n >= 2).GroupBy(n => n).OrderByDescending(g => g.Count()).ThenByDescending(g => g.Key).FirstOrDefault();
                return (c, order, consistency: mode?.Count() ?? 0, width: mode?.Key ?? 0);
            }).OrderByDescending(x => x.consistency).ThenByDescending(x => x.width).ThenBy(x => x.order).First().c;
        }
        var records = CsvRows(text, separator, true, state.Token).Take(50001).Select(r => new ImportRow(r.Line + startLine - 1, r.Values.Select((v, i) => new Value(i, Shorten(v, state), false, !NumericLike(v))).ToArray())).ToArray();
        AddTable(records, null, "", state);
    }

    private static IEnumerable<CsvRecord> CsvRows(string text, char separator, bool strict, CancellationToken ct)
    {
        var values = new List<string>();
        var value = new StringBuilder();
        var quoted = false;
        var line = 1;
        var rowLine = 1;
        for (var i = 0; i <= text.Length; i++)
        {
            if ((i & 1023) == 0) ct.ThrowIfCancellationRequested();
            var end = i == text.Length;
            var c = end ? '\n' : text[i];
            if (quoted && !end)
            {
                if (c == '"')
                {
                    if (i + 1 < text.Length && text[i + 1] == '"') { value.Append('"'); i++; }
                    else quoted = false;
                }
                else { value.Append(c); if (c == '\n') line++; }
                continue;
            }
            if (quoted && end && strict) throw new ContextImportException(ContextImportError.Malformed, "csv_quote line " + rowLine.ToString(CultureInfo.InvariantCulture));
            if (c == '"' && value.ToString().Trim().Length == 0) { quoted = true; value.Clear(); continue; }
            if (c == separator || c == '\n')
            {
                if (values.Count < 256) values.Add(value.ToString().Trim());
                value.Clear();
                if (c == '\n')
                {
                    if (values.Any(v => v.Length > 0)) yield return new CsvRecord(rowLine, values.ToArray());
                    values.Clear(); line++; rowLine = line;
                }
            }
            else value.Append(c);
        }
    }

    private static void AddTable(IReadOnlyList<ImportRow> rows, string? sheet, string locatorPrefix, ImportState state)
    {
        if (rows.Count == 0) return;
        if (rows.Count > 50000) throw Error(ContextImportError.TooManyRows);
        var width = rows.Take(30).Max(r => r.Values.Count(v => v.Text.Length > 0));
        var header = rows.Take(10).FirstOrDefault(r => r.Values.Count(v => v.Text.Length > 0) >= Math.Max(2, (width + 1) / 2) &&
            r.Values.Where(v => v.Text.Length > 0).All(v => v.IsText && ContextText.Runes(v.Text) <= 64) &&
            r.Values.Select(v => v.Text).Where(v => v.Length > 0).Distinct(StringComparer.OrdinalIgnoreCase).Count() == r.Values.Count(v => v.Text.Length > 0) && r != rows[^1]);
        var headers = header?.Values.ToDictionary(v => v.Column, v => v.Text) ?? [];
        var internalColumns = headers.Where(v => ContainsAny(ContextText.Normalize(v.Value), InternalWords)).Select(v => v.Key).ToHashSet();
        state.InternalColumns.AddRange(internalColumns.Select(i => headers[i]));
        var sku = headers.FirstOrDefault(v => !internalColumns.Contains(v.Key) && ContainsAny(ContextText.Normalize(v.Value), SkuWords));
        var name = headers.FirstOrDefault(v => !internalColumns.Contains(v.Key) && v.Key != sku.Key && ContainsAny(ContextText.Normalize(v.Value), NameWords) && !ContainsAny(ContextText.Normalize(v.Value), PriceWords));
        int? skuColumn = sku.Value is null ? null : sku.Key;
        int? nameColumn = name.Value is null ? null : name.Key;
        nameColumn ??= rows.SelectMany(r => r.Values).Where(v => !internalColumns.Contains(v.Column) && v.Column != skuColumn && v.IsText && v.Text.Length > 0).GroupBy(v => v.Column).OrderByDescending(g => g.Count()).Select(g => (int?)g.Key).FirstOrDefault();
        foreach (var row in rows)
        {
            state.Check();
            if (row == header) continue;
            if (header is not null && row.Number < header.Number) { AddText(string.Join(' ', row.Values.Select(v => v.Text)), locatorPrefix + "row " + row.Number.ToString(CultureInfo.InvariantCulture), state); continue; }
            var cells = row.Values.Where(v => v.Text.Length > 0 && !internalColumns.Contains(v.Column)).Select(v =>
                new ContextCell(v.Column, headers.GetValueOrDefault(v.Column, "Column " + (v.Column + 1).ToString(CultureInfo.InvariantCulture)), v.Text, v.Formula)).ToArray();
            if (cells.Length == 0) continue;
            var body = header is null && cells.Length == 1 ? cells[0].Value : Render(cells);
            var nameValue = cells.FirstOrDefault(c => c.Column == nameColumn)?.Value;
            var key = cells.FirstOrDefault(c => c.Column == skuColumn)?.Value ?? nameValue;
            state.Add(new ContextSegment(body, (sheet is null ? "" : sheet + " · ") + locatorPrefix + "row " + row.Number.ToString(CultureInfo.InvariantCulture))
            { Table = sheet ?? locatorPrefix, Name = nameValue, Key = key, Cells = cells });
        }
    }

    internal static string Render(IEnumerable<ContextCell> cells) => string.Join("; ", cells.OrderBy(c => c.Column).Select(c => c.Header + ": " + c.Value + (c.Formula ? " (формула)" : "")));
    private static bool ContainsAny(string value, IEnumerable<string> words) => words.Any(w => value.Contains(w, StringComparison.Ordinal));
    internal static readonly string[] PriceWords = ["цен", "стоим", "price", "cost", "баға", "құн", "сум", "тенге", "₸", "руб", "остат", "налич", "stock", "qty", "колич", "кол-во", "саны", "қалды", "available"];
    private static readonly string[] InternalWords = ["закуп", "себестоим", "маржа", "маржин", "наценк", "поставщик", "комментари", "для внутреннего", "cost price", "purchase", "margin", "markup", "supplier", "internal", "өзіндік құн", "сатып алу", "үстеме", "жеткізуші"];
    private static readonly string[] SkuWords = ["артикул", "sku", "код", "article", "art.", "part no", "vendor code", "штрихкод"];
    private static readonly string[] NameWords = ["наимен", "назван", "товар", "продукт", "модел", "name", "product", "item", "model", "атау", "тауар", "өнім"];
    [GeneratedRegex(@"^[+-]?\d+([.,]\d+)*$", RegexOptions.CultureInvariant)] private static partial Regex NumberPattern();
    private static bool NumericLike(string value)
    {
        var compact = Regex.Replace(value, @"\s", "", RegexOptions.CultureInvariant, TimeSpan.FromSeconds(1));
        compact = Regex.Replace(compact, @"(%|₸|тг\.?|руб|₽|\$|€)$", "", RegexOptions.IgnoreCase | RegexOptions.CultureInvariant, TimeSpan.FromSeconds(1));
        return NumberPattern().IsMatch(compact);
    }
    private static string Shorten(string value, ImportState state)
    {
        value = ContextText.Compact(value);
        if (ContextText.Runes(value) <= 4000) return value;
        state.Shortened++;
        var prefix = ContextText.Take(value, 3999);
        var last = prefix.LastIndexOf(' ');
        return (last > 0 ? prefix[..last] : prefix) + "…";
    }

    private static void ReadXlsx(byte[] bytes, ImportState state)
    {
        using var stream = new MemoryStream(bytes, false);
        using var document = SpreadsheetDocument.Open(stream, false, OpenSettings);
        var workbook = document.WorkbookPart ?? throw Error(ContextImportError.Malformed);
        var shared = workbook.SharedStringTablePart?.SharedStringTable?.Elements<SharedStringItem>().Select(item => string.Concat(item.Descendants<Text>().Where(t => !t.Ancestors<PhoneticRun>().Any()).Select(t => t.Text))).ToArray() ?? [];
        var sheets = workbook.Workbook?.Sheets?.Elements<Sheet>().ToArray() ?? [];
        if (sheets.Length > 128) throw Error(ContextImportError.TooManySheets);
        foreach (var sheet in sheets)
        {
            state.Check();
            if (sheet.State?.Value == SheetStateValues.Hidden || sheet.State?.Value == SheetStateValues.VeryHidden) { state.HiddenSkipped++; continue; }
            if (sheet.Id?.Value is not { } id) throw Error(ContextImportError.Malformed);
            if (workbook.GetPartById(id) is not WorksheetPart part) { state.HiddenSkipped++; continue; }
            state.Sheets++;
            var hidden = (part.Worksheet ?? throw Error(ContextImportError.Malformed)).Elements<Columns>().SelectMany(c => c.Elements<Column>()).Where(c => c.Hidden?.Value == true).ToArray();
            var rows = new List<ImportRow>();
            var rowNumber = 0;
            foreach (var row in part.Worksheet.Descendants<Row>())
            {
                state.Check();
                rowNumber = checked((int)(row.RowIndex?.Value ?? (uint)(rowNumber + 1)));
                var values = new List<Value>();
                var column = -1;
                foreach (var cell in row.Elements<Cell>())
                {
                    column = cell.CellReference?.Value is { } reference ? ColumnIndex(reference) : column + 1;
                    if (column >= 256 || hidden.Any(h => column + 1 >= h.Min?.Value && column + 1 <= h.Max?.Value)) { state.HiddenSkipped++; continue; }
                    var formula = cell.CellFormula is not null;
                    if (formula && cell.CellValue is null) { state.FormulasSkipped++; continue; }
                    var type = cell.DataType?.Value;
                    if (type == CellValues.Error) continue;
                    var raw = cell.CellValue?.Text ?? "";
                    var text = type == CellValues.SharedString ? shared[int.Parse(raw, CultureInfo.InvariantCulture)] :
                        type == CellValues.InlineString ? cell.InlineString?.InnerText ?? "" :
                        type == CellValues.Boolean ? (raw == "1" ? "TRUE" : "FALSE") :
                        type == CellValues.Date && DateTime.TryParse(raw, CultureInfo.InvariantCulture, DateTimeStyles.RoundtripKind, out var date) ? date.ToString("yyyy-MM-dd", CultureInfo.InvariantCulture) :
                        type is null || type == CellValues.Number ? DisplayNumber(raw, cell.StyleIndex?.Value, workbook) : raw;
                    if (formula) state.FormulaValues++;
                    values.Add(new Value(column, Shorten(text, state), formula, type == CellValues.SharedString || type == CellValues.InlineString || type == CellValues.String));
                }
                if (values.Any(v => v.Text.Length > 0)) rows.Add(new ImportRow(rowNumber, values));
                if (rows.Count > 50000) throw Error(ContextImportError.TooManyRows);
            }
            AddTable(rows, sheet.Name?.Value ?? "Sheet", "", state);
        }
    }

    private static string DisplayNumber(string raw, uint? styleIndex, WorkbookPart workbook)
    {
        if (!double.TryParse(raw, NumberStyles.Float, CultureInfo.InvariantCulture, out var number)) return raw;
        var styles = workbook.WorkbookStylesPart?.Stylesheet;
        var format = styleIndex.HasValue ? styles?.CellFormats?.Elements<CellFormat>().ElementAtOrDefault((int)styleIndex.Value)?.NumberFormatId?.Value ?? 0 : 0;
        var custom = styles?.NumberingFormats?.Elements<NumberingFormat>().FirstOrDefault(n => n.NumberFormatId?.Value == format)?.FormatCode?.Value ?? "";
        var dateFormat = format is >= 14 and <= 17 or 22 or >= 27 and <= 36 or >= 50 and <= 58 || Regex.IsMatch(custom, @"[yd]", RegexOptions.IgnoreCase | RegexOptions.CultureInvariant, TimeSpan.FromSeconds(1));
        var timeFormat = format is >= 18 and <= 21 or >= 45 and <= 47;
        if ((dateFormat || timeFormat) && number >= 0 && number <= 2958465)
        {
            var date = workbook.Workbook?.WorkbookProperties?.Date1904?.Value == true ? new DateTime(1904, 1, 1).AddDays(number) : DateTime.FromOADate(number + (number < 61 ? 1 : 0));
            return date.ToString(timeFormat ? "HH:mm" : date.TimeOfDay == TimeSpan.Zero ? "yyyy-MM-dd" : "yyyy-MM-dd HH:mm", CultureInfo.InvariantCulture);
        }
        var percent = format is 9 or 10 || custom.Contains('%', StringComparison.Ordinal);
        if (percent) number *= 100;
        var result = number.ToString(Math.Abs(number) < 1e15 && number == Math.Truncate(number) ? "0" : "0.###############", CultureInfo.InvariantCulture);
        if (percent) result += "%";
        var currency = Regex.Match(custom, "\\[\\$([^]\\-]+)-[^]]*\\]|\"(₸|тг|₽|руб|\\$|€|£|¥)\"", RegexOptions.CultureInvariant, TimeSpan.FromSeconds(1));
        if (currency.Success) result += " " + (currency.Groups[1].Success ? currency.Groups[1].Value : currency.Groups[2].Value);
        return result;
    }
    private static int ColumnIndex(string reference)
    {
        var value = 0;
        foreach (var c in reference.TakeWhile(char.IsAsciiLetter)) value = checked(value * 26 + char.ToUpperInvariant(c) - 'A' + 1);
        return value - 1;
    }

    private static void ReadDocx(byte[] bytes, ImportState state)
    {
        using var stream = new MemoryStream(bytes, false);
        using var doc = WordprocessingDocument.Open(stream, false, OpenSettings);
        var body = doc.MainDocumentPart?.Document?.Body ?? throw Error(ContextImportError.Malformed);
        var paragraph = 0;
        var table = 0;
        string? heading = null;
        var elements = body.ChildElements.ToArray();
        for (var i = 0; i < elements.Length; i++)
        {
            state.Check();
            var element = elements[i];
            if (element is W.Table wordTable)
            {
                table++;
                var rows = wordTable.Elements<W.TableRow>().Select((row, index) => new ImportRow(index + 1, row.Elements<W.TableCell>().Select((cell, j) => new Value(j, Shorten(WordText(cell), state), false, true)).ToArray())).ToArray();
                AddTable(rows, null, $"table {table}, ", state);
                continue;
            }
            // textutil exports an HTML table as centered header paragraphs followed by row-major cells.
            // Recover only this unambiguous shape: >=2 centered headers including a price and a name.
            if (element is W.Paragraph first && first.ParagraphProperties?.Justification?.Val?.Value == W.JustificationValues.Center)
            {
                var headerParagraphs = elements.Skip(i).TakeWhile(e => e is W.Paragraph p && p.ParagraphProperties?.Justification?.Val?.Value == W.JustificationValues.Center).Cast<W.Paragraph>().ToArray();
                var headerText = headerParagraphs.Select(WordText).ToArray();
                var following = elements.Skip(i + headerText.Length).TakeWhile(e => e is W.Paragraph p && !p.Descendants<W.Bold>().Any() && p.ParagraphProperties?.ParagraphStyleId is null).Cast<W.Paragraph>().ToArray();
                if (headerText.Length >= 2 && headerText.Any(h => ContainsAny(ContextText.Normalize(h), PriceWords)) &&
                    headerText.Any(h => ContainsAny(ContextText.Normalize(h), NameWords)) && following.Length >= headerText.Length && following.Length % headerText.Length == 0)
                {
                    table++;
                    var recovered = new List<ImportRow> { new(1, headerText.Select((text, col) => new Value(col, text, false, true)).ToArray()) };
                    for (var row = 0; row < following.Length / headerText.Length; row++)
                        recovered.Add(new ImportRow(row + 2, following.Skip(row * headerText.Length).Take(headerText.Length).Select((p, col) => new Value(col, Shorten(WordText(p), state), false, !NumericLike(WordText(p)))).ToArray()));
                    AddTable(recovered, null, $"table {table}, ", state);
                    paragraph += headerParagraphs.Length + following.Length;
                    i += headerParagraphs.Length + following.Length - 1;
                    continue;
                }
            }
            foreach (var p in (element is W.Paragraph one ? new[] { one } : element.Descendants<W.Paragraph>()))
            {
                var text = WordText(p).Trim();
                if (text.Length == 0) { heading = null; continue; }
                paragraph++;
                var style = p.ParagraphProperties?.ParagraphStyleId?.Val?.Value ?? "";
                var nextText = i + 1 < elements.Length && elements[i + 1] is W.Paragraph next ? WordText(next).Trim() : "";
                if (style.StartsWith("Heading", StringComparison.OrdinalIgnoreCase) || style.Equals("Title", StringComparison.OrdinalIgnoreCase) || p.ParagraphProperties?.OutlineLevel?.Val?.Value is >= 0 and <= 8 ||
                    IsHeading(text) && nextText.Length > 0 && (ContextText.Runes(nextText) > 60 || nextText.EndsWith('.')))
                { heading = text; continue; }
                AddText(text, "paragraph " + paragraph.ToString(CultureInfo.InvariantCulture), state, heading);
            }
        }
    }
    private static string WordText(OpenXmlElement element) => string.Concat(element.Descendants().Where(e => !e.Ancestors<AlternateContentFallback>().Any()).Select(e => e switch
    {
        W.Text t => t.Text, W.TabChar or W.Break or W.CarriageReturn => " ", W.NoBreakHyphen => "-", _ => "",
    }));

    private static void ReadPdf(byte[] bytes, ImportState state)
    {
        if (!Encoding.Latin1.GetString(bytes, 0, Math.Min(bytes.Length, 1024)).Contains("%PDF-", StringComparison.Ordinal)) throw Error(ContextImportError.Malformed);
        PdfSafety.Check(bytes, state.Token);
        try
        {
            using var document = PdfDocument.Open(bytes, new ParsingOptions { UseLenientParsing = true });
            if (document.NumberOfPages > 1000) throw Error(ContextImportError.PdfTooManyPages);
            state.Pages = document.NumberOfPages;
            var all = new StringBuilder();
            foreach (var page in document.GetPages())
            {
                state.Check();
                var text = ContentOrderTextExtractor.GetText(page);
                all.Append(text);
                ReadText(text, "page", state, page.Number);
            }
            var visible = all.ToString().EnumerateRunes().Where(r => !Rune.IsWhiteSpace(r)).ToArray();
            if (visible.Length < 20 || visible.Count(r => r.Value == 0xfffd || r.Value is >= 0xe000 and <= 0xf8ff) > visible.Length * .3) throw Error(ContextImportError.PdfNoText);
        }
        catch (ContextImportException) { throw; }
        catch (Exception ex) when (ex.GetType().Name.Contains("Encrypted", StringComparison.Ordinal)) { throw new ContextImportException(ContextImportError.PdfProtected, inner: ex); }
        catch (Exception ex) when (ex is not OperationCanceledException and not OutOfMemoryException) { throw new ContextImportException(ContextImportError.Malformed, inner: ex); }
    }

    private static ContextImportException Error(ContextImportError error) => new(error);
    private sealed record Value(int Column, string Text, bool Formula, bool IsText);
    private sealed record ImportRow(int Number, IReadOnlyList<Value> Values);
    private sealed record CsvRecord(int Line, IReadOnlyList<string> Values);
    private sealed class ImportState(CancellationToken token)
    {
        public CancellationToken Token { get; } = token;
        public List<ContextSegment> Segments { get; } = [];
        public List<string> InternalColumns { get; } = [];
        public int Characters { get; private set; }
        public int Sheets { get; set; }
        public int Pages { get; set; }
        public int FormulaValues { get; set; }
        public int FormulasSkipped { get; set; }
        public int HiddenSkipped { get; set; }
        public int Shortened { get; set; }
        public string? Encoding { get; set; }
        public void Check() => Token.ThrowIfCancellationRequested();
        public void Add(ContextSegment segment)
        {
            Check();
            Characters += ContextText.Runes(segment.Text);
            if (Characters > 2000000) throw Error(ContextImportError.TooMuchText);
            if (Segments.Count >= 50000) throw Error(ContextImportError.TooManyRows);
            Segments.Add(segment);
        }
    }
}
