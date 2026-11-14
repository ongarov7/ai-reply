using AirReply.Core.Localization;

namespace AirReply.Context;

public static class ContextStrings
{
    /// <summary>Call at startup before creating a Localizer, and in headless presentation tests.</summary>
    public static void Include() => Localizer.Include(typeof(ContextStrings).Assembly, "AirReply.Context.Localization.strings.json");
}
