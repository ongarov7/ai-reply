using AirReply.Core.Settings;
using AirReply.Presentation.Layout;
using Xunit;

namespace AirReply.Presentation.Tests;

public sealed class PlacementTests
{
    private static readonly WorkAreaPixels FullHd = new(0, 0, 1920, 1040);

    [Fact]
    public void TopAndBottomMatchSpecifiedCardGeometry()
    {
        var top = WindowPlacement.Calculate(FullHd, 1, 716, 132, WindowPlacement.ForShow(FullHd, 1, PanelPlacement.Top));
        Assert.Equal(602, top.X);
        Assert.Equal(211, top.Y);
        Assert.False(top.Anchor.GrowsUpward);
        var bottom = WindowPlacement.Calculate(FullHd, 1, 716, 132, WindowPlacement.ForShow(FullHd, 1, PanelPlacement.Bottom));
        Assert.Equal(1034, bottom.Y + bottom.Height);
        Assert.Equal(1016, bottom.Anchor.EdgeY);
        Assert.True(bottom.Anchor.GrowsUpward);
    }

    [Theory]
    [InlineData(PanelPlacement.Top)]
    [InlineData(PanelPlacement.Bottom)]
    public void WindowGrowthRetainsItsVisibleAnchoredEdge(PanelPlacement placement)
    {
        var initial = WindowPlacement.Calculate(FullHd, 1, 716, 132, WindowPlacement.ForShow(FullHd, 1, placement));
        var expanded = WindowPlacement.Calculate(FullHd, 1, 716, 450, initial.Anchor);
        var shrunk = WindowPlacement.Calculate(FullHd, 1, 716, 132, expanded.Anchor);
        Assert.Equal(initial.Anchor.EdgeY, expanded.Anchor.EdgeY);
        Assert.Equal(initial.X, expanded.X);
        Assert.Equal(initial, shrunk);
        Assert.Equal(placement == PanelPlacement.Bottom ? initial.Y - 318 : initial.Y, expanded.Y);
    }

    [Fact]
    public void RememberedBottomEdgeHasNoExtraBottomPlacementGap()
    {
        var anchor = WindowPlacement.ForShow(FullHd, 1, PanelPlacement.Remembered, .5, .8);
        var layout = WindowPlacement.Calculate(FullHd, 1, 716, 200, anchor);
        Assert.Equal(832, layout.Y + layout.Height - 18);
        Assert.Equal(650, layout.Y);
    }

    [Theory]
    [InlineData(100, false)]
    [InlineData(700, true)]
    public void DragReopenKeepsDroppedOriginAndRecomputesGrowthDirection(double y, bool growsUpward)
    {
        var saved = WindowPlacement.CaptureDrag(FullHd, 1, 360, y, 716, 200);
        Assert.Equal(growsUpward, saved.Anchor.GrowsUpward);
        var reopened = WindowPlacement.Calculate(FullHd, 1, 716, 200,
            WindowPlacement.ForShow(FullHd, 1, PanelPlacement.Remembered, saved.XFraction, saved.YFraction));
        Assert.Equal(360, reopened.X);
        Assert.Equal(y, reopened.Y);
        Assert.Equal(growsUpward, reopened.Anchor.GrowsUpward);
        var expanded = WindowPlacement.Calculate(FullHd, 1, 716, 260, saved.Anchor);
        Assert.Equal(growsUpward ? y - 60 : y, expanded.Y);
    }

    [Fact]
    public void DragAcrossMidlineSwitchesTheAnchorImmediately()
    {
        var before = WindowPlacement.CaptureDrag(FullHd, 1, 300, 100, 716, 200);
        var after = WindowPlacement.CaptureDrag(FullHd, 1, 300, 720, 716, 200);
        Assert.False(before.Anchor.GrowsUpward);
        Assert.True(after.Anchor.GrowsUpward);
        Assert.Equal(902d / 1040, after.YFraction, 8);
        var grown = WindowPlacement.Calculate(FullHd, 1, 716, 300, after.Anchor);
        Assert.Equal(620, grown.Y);
    }

    [Fact]
    public void DpiAndNegativeMonitorOriginUsePhysicalCoordinates()
    {
        var area = new WorkAreaPixels(-2560, -120, 2560, 1400);
        var bottom = WindowPlacement.Calculate(area, 1.5, 716, 200, WindowPlacement.ForShow(area, 1.5, PanelPlacement.Bottom));
        Assert.Equal(1074, bottom.Width);
        Assert.Equal(300, bottom.Height);
        Assert.Equal(-1817, bottom.X);
        Assert.Equal(1244, bottom.Anchor.EdgeY);
        var saved = WindowPlacement.CaptureDrag(area, 1.5, bottom.X, bottom.Y, 716, 200);
        var reopened = WindowPlacement.Calculate(area, 1.5, 716, 200,
            WindowPlacement.ForShow(area, 1.5, PanelPlacement.Remembered, saved.XFraction, saved.YFraction));
        Assert.Equal(bottom.X, reopened.X);
        Assert.Equal(bottom.Y, reopened.Y);
    }

    [Fact]
    public void LargeResultsAreCappedToWorkAreaAndKeepTheMainCardVisible()
    {
        var anchor = WindowPlacement.ForShow(FullHd, 1, PanelPlacement.Top);
        var giant = WindowPlacement.Calculate(FullHd, 1, 716, 3000, anchor);
        Assert.Equal(1076, giant.Height);
        Assert.Equal(1076, giant.MaxHeightDip);
        Assert.Equal(-18, giant.Y);
        Assert.Equal(FullHd.Bottom + 18, giant.Y + giant.Height);
        var reduced = WindowPlacement.Calculate(FullHd, 1, 716, 300, giant.Anchor);
        Assert.Equal(giant.Y, reduced.Y);
    }

    [Fact]
    public void NarrowHighDpiMonitorClampsBothDimensionsAndShadowOnlyMayExtendOutside()
    {
        var area = new WorkAreaPixels(1920, 100, 800, 600);
        var layout = WindowPlacement.Calculate(area, 2, 716, 700,
            new BarAnchor(5000, -5000, false));
        Assert.Equal(872, layout.Width);
        Assert.Equal(672, layout.Height);
        Assert.Equal(336, layout.MaxHeightDip);
        Assert.Equal(area.X - 36, layout.X);
        Assert.Equal(area.Y - 36, layout.Y);
        Assert.Equal(area.Right + 36, layout.X + layout.Width);
        Assert.Equal(area.Bottom + 36, layout.Y + layout.Height);
    }

    [Fact]
    public void InvalidSavedCoordinatesFallBackToTheDefaultWhileFiniteValuesAreClamped()
    {
        Assert.Equal(WindowPlacement.ForShow(FullHd, 1, PanelPlacement.Top),
            WindowPlacement.ForShow(FullHd, 1, PanelPlacement.Remembered, double.NaN, double.PositiveInfinity));
        var clamped = WindowPlacement.ForShow(FullHd, 1, PanelPlacement.Remembered, -4, 6);
        Assert.Equal(0, clamped.CenterX);
        Assert.Equal(1040, clamped.EdgeY);
        Assert.True(clamped.GrowsUpward);
    }

    [Fact]
    public void FractionalDpiRoundingDoesNotAccumulateAcrossRepeatedGrowth()
    {
        var area = new WorkAreaPixels(-1920, 0, 1920, 1040);
        var initial = WindowPlacement.Calculate(area, 1.25, 680, 201,
            WindowPlacement.ForShow(area, 1.25, PanelPlacement.Bottom), shadowInsetDip: 16);
        var current = initial;
        for (var i = 0; i < 100; i++)
        {
            current = WindowPlacement.Calculate(area, 1.25, 680, 377, current.Anchor, shadowInsetDip: 16);
            current = WindowPlacement.Calculate(area, 1.25, 680, 201, current.Anchor, shadowInsetDip: 16);
        }
        Assert.Equal(initial, current);
    }
}
