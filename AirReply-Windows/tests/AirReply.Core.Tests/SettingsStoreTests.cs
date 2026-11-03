using AirReply.Core.Settings;
using Xunit;

namespace AirReply.Core.Tests;

public class SettingsStoreTests
{
    [Fact]
    public void RoundTripsAndToleratesDamage()
    {
        var path = Path.Combine(Path.GetTempPath(), $"airreply-settings-{Guid.NewGuid():N}", "settings.json");
        var store = new JsonSettingsStore(path);
        Assert.Equal(KeyGesture.AltSpace, store.Load().Shortcut);
        store.Save(new AppSettings { Placement = PanelPlacement.Bottom, PasteAfterCopy = true, Shortcut = KeyGesture.ControlAltSpace });
        var loaded = store.Load();
        Assert.Equal(PanelPlacement.Bottom, loaded.Placement);
        Assert.True(loaded.PasteAfterCopy);
        Assert.Equal(KeyGesture.ControlAltSpace, loaded.Shortcut);
        File.WriteAllText(path, "{ not json");
        Assert.Equal(PanelPlacement.Top, store.Load().Placement);
    }
}
