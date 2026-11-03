using Avalonia.Controls;
namespace Mock;
/// Every screen to render: name, factory, canvas width and height (device-independent pixels).
public static class Gallery
{
    public static IReadOnlyList<(string Name, Func<Control> Build, double Width, double Height)> All { get; } =
    [
        ("bar-compose", () => new Screens.BarCompose(), 760, 216),
        ("bar-reply", () => new Screens.BarReply(), 760, 288),
        ("bar-result", () => new Screens.BarResult(), 760, 416),
        ("bar-recording", () => new Screens.BarRecording(), 760, 192),
        ("bar-issue", () => new Screens.BarIssue(), 760, 208),
        ("bar-signin", () => new Screens.BarSignin(), 760, 336),
        ("bar-context", () => new Screens.BarContext(), 760, 460),
        ("settings", () => new Screens.Settings(), 800, 600),
        ("tray-menu", () => new Screens.TrayMenu(), 760, 400),
    ];
}
