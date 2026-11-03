using Avalonia.Controls;
namespace Mock;
/// Every screen to render: name, factory, canvas width and height (device-independent pixels).
public static class Gallery
{
    public static IReadOnlyList<(string Name, Func<Control> Build, double Width, double Height)> All { get; } =
    [
        ("bar-compose", () => new Screens.BarCompose(), 760, 160),
        ("bar-reply", () => new Screens.BarReply(), 760, 222),
        ("bar-result", () => new Screens.BarResult(), 760, 279),
        ("bar-recording", () => new Screens.BarRecording(), 760, 160),
        ("bar-issue", () => new Screens.BarIssue(), 760, 196),
        ("bar-signin", () => new Screens.BarSignin(), 760, 264),
        ("bar-context", () => new Screens.BarContext(), 760, 403),
        ("settings", () => new Screens.Settings(), 800, 600),
        ("tray-menu", () => new Screens.TrayMenu(), 760, 388),
    ];
}
