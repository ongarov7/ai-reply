using System.Globalization;
using AirReply.Core.Localization;
using Xunit;

namespace AirReply.Core.Tests;

public class LocalizerTests
{
    [Fact]
    public void EveryKeyHasAllThreeLanguages()
    {
        var ru = new Localizer(UiLanguage.Ru);
        var kk = new Localizer(UiLanguage.Kk);
        var en = new Localizer(UiLanguage.En);
        Assert.True(ru.Keys.Count() >= 170);
        foreach (var key in ru.Keys)
        {
            Assert.False(string.IsNullOrWhiteSpace(ru[key]), key);
            Assert.False(string.IsNullOrWhiteSpace(kk[key]), key);
            Assert.False(string.IsNullOrWhiteSpace(en[key]), key);
        }
    }

    [Fact]
    public void PlaceholdersFormatInEveryLanguage()
    {
        foreach (var language in new[] { UiLanguage.Ru, UiLanguage.Kk, UiLanguage.En })
        {
            var localizer = new Localizer(language);
            foreach (var key in localizer.Keys)
            {
                // Every value must be a valid composite format with at most two arguments.
                var formatted = localizer.Format(key, 12, 400);
                Assert.DoesNotContain("{0}", formatted);
            }
        }
        Assert.Equal("12 из 400 символов", new Localizer(UiLanguage.Ru).Format("bar.counter", 12, 400));
    }

    [Fact]
    public void UnknownKeyFallsBackToTheKey() => Assert.Equal("no.such.key", new Localizer(UiLanguage.En)["no.such.key"]);

    [Theory]
    [InlineData("kk-KZ", UiLanguage.Kk)]
    [InlineData("en-US", UiLanguage.En)]
    [InlineData("ru-RU", UiLanguage.Ru)]
    [InlineData("de-DE", UiLanguage.Ru)]
    public void WindowsDisplayLanguagePicksTheInterface(string culture, UiLanguage expected) =>
        Assert.Equal(expected, Localizer.FromCulture(CultureInfo.GetCultureInfo(culture)));
}
