using Avalonia;
using Avalonia.Controls;
using Avalonia.Headless;
using Avalonia.Headless.XUnit;
using Avalonia.Styling;
using Avalonia.Threading;
using Xunit;
[assembly: AvaloniaTestApplication(typeof(TestAppBuilder))]
public class TestAppBuilder
{
    public static AppBuilder BuildAvaloniaApp() => AppBuilder.Configure<Mock.App>()
        .UseSkia().UseHeadless(new AvaloniaHeadlessPlatformOptions { UseHeadlessDrawing = false }).WithInterFont();
}
public class RenderAll
{
    [AvaloniaFact]
    public void RenderGallery()
    {
        var output = Environment.GetEnvironmentVariable("MOCK_OUT") ?? Path.Combine(AppContext.BaseDirectory, "renders");
        Directory.CreateDirectory(output);
        foreach (var (name, build, width, height) in Mock.Gallery.All)
        {
            foreach (var variant in new[] { ThemeVariant.Light, ThemeVariant.Dark })
            {
                var window = new Window { Width = width, Height = height, RequestedThemeVariant = variant, Content = build() };
                window.Bind(Window.BackgroundProperty, window.GetResourceObservable("Wallpaper"));
                window.Show();
                Dispatcher.UIThread.RunJobs();
                var frame = window.CaptureRenderedFrame();
                Assert.NotNull(frame);
                frame!.Save(Path.Combine(output, $"{name}-{(variant == ThemeVariant.Dark ? "dark" : "light")}.png"));
                window.Close();
            }
        }
    }
}
