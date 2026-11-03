using Avalonia.Controls;
using Avalonia.Controls.Shapes;
using Avalonia.Layout;
namespace Mock.Screens;
public partial class BarRecording : UserControl
{
    // 40 level samples (px), newest on the right: a speech-like envelope, 2..26 px.
    static readonly double[] Levels =
    [
        3, 4, 6, 10, 14, 9, 6, 4, 7, 12, 18, 24, 20, 13, 8, 6, 10, 16, 22, 26,
        21, 15, 9, 5, 4, 6, 11, 17, 21, 16, 10, 7, 12, 18, 14, 9, 6, 4, 3, 2,
    ];

    public BarRecording()
    {
        InitializeComponent();
        foreach (var h in Levels)
        {
            var bar = new Rectangle { Width = 3, Height = Math.Max(3, h), RadiusX = 1.5, RadiusY = 1.5, VerticalAlignment = VerticalAlignment.Center };
            bar.Bind(Shape.FillProperty, bar.GetResourceObservable("Accent"));
            Wave.Children.Add(bar);
        }
    }
}
