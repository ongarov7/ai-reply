using Avalonia;
using AirReply.Platform.Windows;

namespace AirReply.App;

internal static class Program
{
    [STAThread]
    public static int Main(string[] args)
    {
        WindowsSingleInstance? instance = null;
        if (OperatingSystem.IsWindows() && !WindowsSingleInstance.TryAcquire("production", out instance))
        {
            if (!args.Contains("--background", StringComparer.Ordinal)) WindowsSingleInstance.NotifyExisting("production");
            return 0;
        }
        try { return BuildAvaloniaApp().StartWithClassicDesktopLifetime(args); }
        finally { if (OperatingSystem.IsWindows()) instance?.Dispose(); }
    }

    public static AppBuilder BuildAvaloniaApp() => AppBuilder.Configure<App>().UsePlatformDetect().WithInterFont();
}
