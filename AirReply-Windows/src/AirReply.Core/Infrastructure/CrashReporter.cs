using System.Text.Json;
using AirReply.Core.Networking;

namespace AirReply.Core.Infrastructure;

/// <summary>Stores only anonymous exception types/HResults; messages, stack traces and request bodies never leave the process.</summary>
public sealed class CrashReporter(ApiClient api, string path, IClock? clock = null)
{
    private readonly IClock clock = clock ?? SystemClock.Instance;
    private readonly Lock gate = new();
    public void RecordFatal(Exception exception)
    {
        ArgumentNullException.ThrowIfNull(exception);
        lock (gate)
        {
            try
            {
                var reports = Load();
                var code = Clean($"{exception.GetType().FullName}:0x{exception.HResult:X8}", 80, code: true);
                var version = Clean($"{api.Info.Version} ({api.Info.Build})", 40);
                var prior = reports.FindIndex(r => r.Code == code && r.AppVersion == version);
                if (prior >= 0) { reports[prior] = reports[prior] with { Count = Math.Min(10000, reports[prior].Count + 1) }; }
                else { reports.Add(new(code, version, Clean("Windows " + api.Info.OsVersion, 40), 1, clock.Now)); }
                Save(reports);
            }
            catch (Exception ex) when (ex is IOException or UnauthorizedAccessException or JsonException) { /* A crash handler must never throw. */ }
        }
    }
    public async Task SendPendingAsync(CancellationToken ct = default)
    {
        List<PendingReport> snapshot;
        lock (gate) { snapshot = Load(); }
        foreach (var report in snapshot)
        {
            var discard = clock.Now - report.RecordedAt > TimeSpan.FromDays(7);
            if (!discard)
            {
                try
                {
                    await api.SendAsync(new(HttpMethod.Post, "/api/v1/client-errors")
                    {
                        Authenticated = false,
                        Body = new { platform = AppInfo.Platform, kind = "crash", report.Code, report.AppVersion, report.OsVersion, report.Count },
                    }, ct).ConfigureAwait(false);
                    discard = true;
                }
                catch (ApiException ex) when (ex.Failure != ApiFailure.Cancelled) { discard = ex.Status == 400; }
            }
            if (discard)
            {
                lock (gate)
                {
                    var latest = Load();
                    var index = latest.FindIndex(r => r == report);
                    if (index >= 0) { latest.RemoveAt(index); Save(latest); }
                }
            }
        }
    }
    private List<PendingReport> Load()
    {
        try { return File.Exists(path) ? JsonSerializer.Deserialize<List<PendingReport>>(File.ReadAllText(path), ApiJson.Options) ?? [] : []; }
        catch (Exception ex) when (ex is IOException or UnauthorizedAccessException or JsonException) { return []; }
    }
    private void Save(List<PendingReport> reports)
    {
        Directory.CreateDirectory(Path.GetDirectoryName(Path.GetFullPath(path))!);
        var temporary = path + "." + Guid.NewGuid().ToString("N") + ".tmp";
        File.WriteAllText(temporary, JsonSerializer.Serialize(reports, ApiJson.Options));
        File.Move(temporary, path, overwrite: true);
    }
    private static string Clean(string value, int max, bool code = false) => new(value.Where(c =>
        char.IsAsciiLetterOrDigit(c) || (code ? "_.:/-" : " ._()+-").Contains(c, StringComparison.Ordinal)).Take(max).ToArray());
    private sealed record PendingReport(string Code, string AppVersion, string OsVersion, int Count, DateTimeOffset RecordedAt);
}
