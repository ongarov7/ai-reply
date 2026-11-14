using Avalonia;
using Avalonia.Markup.Xaml;
using Avalonia.Controls.ApplicationLifetimes;

namespace AirReply.App;

public partial class App : Application
{
    public override void Initialize() => AvaloniaXamlLoader.Load(this);

    public override void OnFrameworkInitializationCompleted()
    {
        if (ApplicationLifetime is IClassicDesktopStyleApplicationLifetime desktop)
        {
            desktop.ShutdownMode = Avalonia.Controls.ShutdownMode.OnExplicitShutdown;
            var host = new DesktopHost(desktop);
            desktop.Exit += (_, _) => host.Dispose();
            host.Start(desktop.Args ?? []);
        }
        base.OnFrameworkInitializationCompleted();
    }
}
