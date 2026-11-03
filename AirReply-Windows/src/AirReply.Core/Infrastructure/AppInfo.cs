using System.Reflection;
using System.Runtime.InteropServices;

namespace AirReply.Core.Infrastructure;

/// <summary>What the app tells the server about itself. Platform is always "windows".</summary>
public sealed record AppInfo(Uri BackendBaseUrl, string Version, string Build, string OsVersion, string Architecture)
{
    public const string Platform = "windows";

    public static readonly Uri ProductionBackend = new("https://ai-reply.kz/");

    /// <summary>"AIReply-Windows/1.0.0 (1; Windows 10.0.26100; x64)" — no personal data.</summary>
    public string UserAgent => $"AIReply-Windows/{Version} ({Build}; Windows {OsVersion}; {Architecture})";

    public static AppInfo Current(Uri? backend = null)
    {
        var assembly = Assembly.GetEntryAssembly() ?? typeof(AppInfo).Assembly;
        var version = assembly.GetName().Version ?? new Version(1, 0, 0, 0);
        var informational = assembly.GetCustomAttribute<AssemblyInformationalVersionAttribute>()?.InformationalVersion;
        var build = version.Revision > 0 ? version.Revision.ToString(System.Globalization.CultureInfo.InvariantCulture) : "1";
        var shortVersion = informational?.Split('+')[0] ?? $"{version.Major}.{version.Minor}.{version.Build}";
        return new AppInfo(backend ?? ProductionBackend, shortVersion, build, Environment.OSVersion.Version.ToString(),
            RuntimeInformation.OSArchitecture.ToString().ToLowerInvariant());
    }
}
