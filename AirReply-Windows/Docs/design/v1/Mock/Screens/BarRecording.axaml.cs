using Avalonia.Controls;
namespace Mock.Screens;
public partial class BarRecording : UserControl
{
    // ~48 level samples (oldest → newest), heights in px inside the 40 px field.
    static readonly double[] Levels =
    [
        3, 3, 4, 6, 9, 13, 10, 7, 11, 17, 22, 18, 12, 8, 5, 4,
        6, 10, 15, 21, 25, 19, 13, 9, 6, 5, 8, 13, 19, 24, 18, 11,
        7, 5, 4, 6, 10, 16, 20, 15, 10, 13, 17, 12, 8, 5, 4, 3,
    ];
    public BarRecording()
    {
        InitializeComponent();
        foreach (var h in Levels) Wave.Children.Add(new Border { Height = h, Classes = { "wave" } });
    }
}
