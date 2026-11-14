using AirReply.Core.Settings;

namespace AirReply.Presentation.Layout;

/// <summary>The selected monitor's work area in physical pixels, including a possibly negative screen origin.</summary>
public readonly record struct WorkAreaPixels(double X, double Y, double Width, double Height)
{
    public double Right => X + Width;
    public double Bottom => Y + Height;
}

/// <summary>The visible card's horizontal centre and anchored edge in physical pixels. Keep this between resizes.</summary>
public readonly record struct BarAnchor(double CenterX, double EdgeY, bool GrowsUpward);

/// <summary>A clamped physical window rectangle plus the actual card anchor after clamping.</summary>
public readonly record struct BarLayout(double X, double Y, double Width, double Height, double MaxHeightDip, BarAnchor Anchor);

/// <summary>Only a user drag produces these preferences; resizing and monitor changes never save them.</summary>
public readonly record struct RememberedBarAnchor(double XFraction, double YFraction, BarAnchor Anchor);

/// <summary>Pure placement math; the host chooses a monitor under the cursor on show and the current monitor on resize.</summary>
public static class WindowPlacement
{
    public static BarAnchor ForShow(WorkAreaPixels area, double scale, PanelPlacement placement,
        double? rememberedX = null, double? rememberedY = null, double bottomGapDip = 24)
    {
        ValidateArea(area, scale);
        ValidateNonnegative(bottomGapDip, nameof(bottomGapDip));
        var x = placement == PanelPlacement.Remembered ? Fraction(rememberedX, .5) : .5;
        var y = placement == PanelPlacement.Remembered ? Fraction(rememberedY, .22) : .22;
        return placement == PanelPlacement.Bottom
            ? new BarAnchor(area.X + area.Width / 2, area.Bottom - bottomGapDip * scale, true)
            : new BarAnchor(area.X + x * area.Width, area.Y + y * area.Height,
                placement == PanelPlacement.Remembered && y > .5);
    }

    /// <summary>
    /// Width/height include transparent shadow margins. The visible card stays inside the work area; shadow may extend
    /// by <paramref name="shadowInsetDip"/>. Use the returned Anchor for the next resize so its actual edge stays fixed.
    /// The host applies MaxHeightDip to its scrolling window and Width / scale when a narrow monitor limits width.
    /// </summary>
    public static BarLayout Calculate(WorkAreaPixels area, double scale, double widthDip, double heightDip,
        BarAnchor anchor, double shadowInsetDip = 18)
    {
        ValidateArea(area, scale);
        ValidatePositive(widthDip, nameof(widthDip));
        ValidatePositive(heightDip, nameof(heightDip));
        ValidateNonnegative(shadowInsetDip, nameof(shadowInsetDip));
        if (!double.IsFinite(anchor.CenterX) || !double.IsFinite(anchor.EdgeY))
            throw new ArgumentException("The card anchor must be finite.", nameof(anchor));
        var inset = shadowInsetDip * scale;
        var width = Math.Min(Round(widthDip * scale), area.Width + 2 * inset);
        var height = Math.Min(Round(heightDip * scale), area.Height + 2 * inset);
        var x = Math.Clamp(anchor.CenterX - width / 2, area.X - inset, area.Right - width + inset);
        var y = anchor.GrowsUpward ? anchor.EdgeY - height + inset : anchor.EdgeY - inset;
        y = Math.Clamp(y, area.Y - inset, area.Bottom - height + inset);
        // Round only the final position so DPI conversion does not accumulate drift between resizes.
        x = Round(x);
        y = Round(y);
        var actual = new BarAnchor(x + width / 2, anchor.GrowsUpward ? y + height - inset : y + inset, anchor.GrowsUpward);
        return new BarLayout(x, y, width, height, area.Height / scale + 2 * shadowInsetDip, actual);
    }

    /// <summary>
    /// Recompute growth direction from the dropped card's centre, then save that card edge. This prevents a jump by
    /// one card height after dragging across the monitor's midline. No bottom-placement gap is added to remembered edges.
    /// </summary>
    public static RememberedBarAnchor CaptureDrag(WorkAreaPixels area, double scale, double xPixels, double yPixels,
        double widthDip, double heightDip, double shadowInsetDip = 18)
    {
        ValidateArea(area, scale);
        ValidatePositive(widthDip, nameof(widthDip));
        ValidatePositive(heightDip, nameof(heightDip));
        ValidateNonnegative(shadowInsetDip, nameof(shadowInsetDip));
        if (!double.IsFinite(xPixels) || !double.IsFinite(yPixels))
            throw new ArgumentException("The dropped window position must be finite.", nameof(xPixels));
        var width = Round(widthDip * scale);
        var height = Round(heightDip * scale);
        var inset = shadowInsetDip * scale;
        var upward = yPixels + height / 2 > area.Y + area.Height / 2;
        var x = Math.Clamp((xPixels + width / 2 - area.X) / area.Width, 0, 1);
        var y = Math.Clamp(((upward ? yPixels + height - inset : yPixels + inset) - area.Y) / area.Height, 0, 1);
        return new RememberedBarAnchor(x, y, new BarAnchor(area.X + x * area.Width, area.Y + y * area.Height, upward));
    }

    private static double Fraction(double? value, double fallback) => value is { } number && double.IsFinite(number)
        ? Math.Clamp(number, 0, 1) : fallback;
    private static double Round(double value) => Math.Round(value, MidpointRounding.AwayFromZero);
    private static void ValidateArea(WorkAreaPixels area, double scale)
    {
        ValidatePositive(scale, nameof(scale));
        ValidatePositive(area.Width, nameof(area));
        ValidatePositive(area.Height, nameof(area));
        if (!double.IsFinite(area.X) || !double.IsFinite(area.Y)) throw new ArgumentException("Work area origin must be finite.", nameof(area));
    }
    private static void ValidatePositive(double value, string name)
    {
        if (!double.IsFinite(value) || value <= 0) throw new ArgumentOutOfRangeException(name);
    }
    private static void ValidateNonnegative(double value, string name)
    {
        if (!double.IsFinite(value) || value < 0) throw new ArgumentOutOfRangeException(name);
    }
}
