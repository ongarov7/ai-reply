using AirReply.Core.Localization;
using Xunit;

namespace AirReply.Context.Tests;

public sealed class ContextLocalizationTests
{
    [Theory]
    [InlineData(UiLanguage.Ru)]
    [InlineData(UiLanguage.Kk)]
    [InlineData(UiLanguage.En)]
    public void EveryImportErrorHasAnActionableMessage(UiLanguage language)
    {
        ContextStrings.Include();
        var localizer = new Localizer(language);
        foreach (var error in Enum.GetValues<ContextImportError>())
        {
            var key = new ContextImportException(error).MessageKey;
            Assert.True(localizer.Contains(key), "Missing " + key);
            Assert.NotEqual(key, localizer[key]);
            Assert.DoesNotContain("{", localizer[key], StringComparison.Ordinal);
        }
        Assert.True(localizer.Contains(new ContextImportException(ContextImportError.Malformed, "csv_quote line 2").MessageKey));
        Assert.Contains("500", localizer["bar.context.instructions"], StringComparison.Ordinal);
        Assert.Contains("8", localizer["context.limits"], StringComparison.Ordinal);
    }
}
