using System.Globalization;
using Avalonia;
using Avalonia.Controls;
using Avalonia.Controls.Shapes;
using Avalonia.Layout;
using Avalonia.Media;
using Path = Avalonia.Controls.Shapes.Path;

namespace Mock;

/// Geometry helpers for the hand-drawn glyphs (16 px grid unless noted).
public static class Geo
{
    static string F(double v) => v.ToString("0.###", CultureInfo.InvariantCulture);

    public static string RoundRect(double x, double y, double w, double h, double r) =>
        $"M{F(x + r)},{F(y)} H{F(x + w - r)} A{F(r)},{F(r)} 0 0 1 {F(x + w)},{F(y + r)} V{F(y + h - r)} " +
        $"A{F(r)},{F(r)} 0 0 1 {F(x + w - r)},{F(y + h)} H{F(x + r)} A{F(r)},{F(r)} 0 0 1 {F(x)},{F(y + h - r)} " +
        $"V{F(y + r)} A{F(r)},{F(r)} 0 0 1 {F(x + r)},{F(y)} Z";

    public static string Circle(double cx, double cy, double r) =>
        $"M{F(cx - r)},{F(cy)} A{F(r)},{F(r)} 0 1 1 {F(cx + r)},{F(cy)} A{F(r)},{F(r)} 0 1 1 {F(cx - r)},{F(cy)} Z";

    static Geometry P(string d) => Geometry.Parse(d);

    /// Brand glyph on an 18 px grid: back bubble as a 1.5 px outline offset up-right (knocked out around the
    /// front bubble), front bubble solid with a bottom-left tail and three round dot cut-outs.
    public static Geometry Brand(double size = 18)
    {
        var body = new CombinedGeometry(GeometryCombineMode.Union,
            P(RoundRect(0.85, 5.6, 11.6, 8.4, 3)),
            P("M2.3,12.2 L2.0,16.9 L6.6,13.6 Z"));
        var dots = new GeometryGroup
        {
            FillRule = FillRule.NonZero,
            Children = new GeometryCollection { P(Circle(3.65, 9.8, 1.05)), P(Circle(6.65, 9.8, 1.05)), P(Circle(9.65, 9.8, 1.05)) },
        };
        var front = new CombinedGeometry(GeometryCombineMode.Exclude, body, dots);
        var ring = new CombinedGeometry(GeometryCombineMode.Exclude,
            P(RoundRect(4.6, 0.8, 12.6, 10.4, 3.6)),
            P(RoundRect(6.1, 2.3, 9.6, 7.4, 2.1)));
        var knockout = P(RoundRect(-0.65, 4.1, 14.6, 11.4, 4.5));
        var back = new CombinedGeometry(GeometryCombineMode.Exclude, ring, knockout);
        var g = new GeometryGroup { FillRule = FillRule.NonZero, Children = new GeometryCollection { front, back } };
        if (Math.Abs(size - 18) > 0.01) g.Transform = new ScaleTransform(size / 18, size / 18);
        return g;
    }

    /// Outline gear on a 16 px grid.
    public static Geometry Gear()
    {
        const double cx = 8, cy = 8, ro = 6.9, ri = 5.2;
        const int n = 8;
        var sb = new System.Text.StringBuilder();
        double step = 360.0 / n, ho = 9.0, hi = 15.0;
        (double, double) Pt(double r, double deg)
        {
            var a = deg * Math.PI / 180;
            return (cx + r * Math.Cos(a), cy + r * Math.Sin(a));
        }
        for (int i = 0; i < n; i++)
        {
            double a = i * step - 90;
            var p1 = Pt(ri, a - hi); var p2 = Pt(ro, a - ho); var p3 = Pt(ro, a + ho); var p4 = Pt(ri, a + hi);
            var next = Pt(ri, a + step - hi);
            if (i == 0) sb.Append($"M{F(p1.Item1)},{F(p1.Item2)} ");
            sb.Append($"L{F(p2.Item1)},{F(p2.Item2)} L{F(p3.Item1)},{F(p3.Item2)} L{F(p4.Item1)},{F(p4.Item2)} ");
            sb.Append($"A{F(ri)},{F(ri)} 0 0 1 {F(next.Item1)},{F(next.Item2)} ");
        }
        sb.Append("Z ");
        sb.Append(Circle(cx, cy, 2.25));
        return P(sb.ToString());
    }
}

/// The brand glyph, tinted with the accent through the `brand` class.
public class BrandGlyph : Path
{
    protected override Type StyleKeyOverride => typeof(Path);

    public BrandGlyph()
    {
        Data = Geo.Brand();
        Width = 18; Height = 18;
        Stretch = Stretch.None;
        Classes.Add("brand");
    }

    public double Size
    {
        get => Width;
        set { Width = value; Height = value; Data = Geo.Brand(value); }
    }
}

/// Gear icon (generated geometry), styled like the other outline icons.
public class GearIcon : Path
{
    protected override Type StyleKeyOverride => typeof(Path);

    public GearIcon()
    {
        Data = Geo.Gear();
        Classes.Add("ic");
    }
}

/// Keycap hint: Combo="Ctrl Enter" renders two keycaps.
public class Keys : StackPanel
{
    string _combo = "";

    public Keys()
    {
        Orientation = Orientation.Horizontal;
        Spacing = 3;
        VerticalAlignment = VerticalAlignment.Center;
    }

    public string Combo
    {
        get => _combo;
        set
        {
            _combo = value;
            Children.Clear();
            foreach (var key in value.Split(' ', StringSplitOptions.RemoveEmptyEntries))
                Children.Add(new Border { Classes = { "keycap" }, Child = new TextBlock { Text = key } });
        }
    }
}

/// Windows 11 toggle switch (40 x 20).
public class Toggle : Border
{
    bool _on;

    public Toggle()
    {
        Child = new Ellipse();
    }

    public bool IsOn
    {
        get => _on;
        set { _on = value; Classes.Set("on", value); }
    }
}

/// Live dictation waveform: 40 rounded bars, oldest first (matches IVoiceRecorder.Levels).
public class Wave : StackPanel
{
    static readonly double[] Levels =
    [
        0.08, 0.14, 0.30, 0.52, 0.40, 0.26, 0.58, 0.78, 0.64, 0.38,
        0.20, 0.10, 0.06, 0.12, 0.34, 0.66, 0.90, 0.72, 0.50, 0.58,
        0.84, 0.62, 0.34, 0.18, 0.12, 0.24, 0.46, 0.66, 0.56, 0.42,
        0.70, 0.96, 0.80, 0.52, 0.30, 0.44, 0.64, 0.48, 0.26, 0.14,
    ];

    public Wave()
    {
        Orientation = Orientation.Horizontal;
        Spacing = 6;
        VerticalAlignment = VerticalAlignment.Center;
        foreach (var l in Levels)
        {
            Children.Add(new Border
            {
                Width = 4,
                Height = Math.Round(4 + l * 26),
                CornerRadius = new CornerRadius(2),
                VerticalAlignment = VerticalAlignment.Center,
                Classes = { "wavebar" },
            });
        }
    }
}
