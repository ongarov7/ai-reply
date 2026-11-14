using System.Buffers.Binary;
using System.Runtime.InteropServices;
using System.Runtime.Versioning;
using System.Security.Cryptography;
using AirReply.Core.Settings;
using AirReply.Platform.Windows;
using AirReply.Presentation.Platform;
using Xunit;

namespace AirReply.PlatformWindows.Tests;

public sealed class HotKeyRulesTests
{
    [Theory]
    [InlineData(0x20, KeyModifiers.Alt, true)]
    [InlineData(0x20, KeyModifiers.Control | KeyModifiers.Alt | KeyModifiers.Shift, true)]
    [InlineData(0x4B, KeyModifiers.Control | KeyModifiers.Shift, true)]
    [InlineData(0x20, KeyModifiers.Control, false)]
    [InlineData(0x43, KeyModifiers.Control, false)]
    [InlineData(0x56, KeyModifiers.Control, false)]
    [InlineData(0x09, KeyModifiers.Control | KeyModifiers.Shift, false)]
    [InlineData(0x1B, KeyModifiers.Control | KeyModifiers.Shift, false)]
    [InlineData(0x2E, KeyModifiers.Control | KeyModifiers.Alt, false)]
    [InlineData(0x7B, KeyModifiers.Control | KeyModifiers.Alt, false)]
    [InlineData(0x2C, KeyModifiers.Control | KeyModifiers.Alt, false)]
    [InlineData(0x4B, KeyModifiers.Win | KeyModifiers.Control, false)]
    [InlineData(0x4B, KeyModifiers.Shift, false)]
    [InlineData(0x10, KeyModifiers.Control | KeyModifiers.Alt, false)]
    [InlineData(0xE5, KeyModifiers.Control | KeyModifiers.Alt, false)]
    [InlineData(0x01, KeyModifiers.Control | KeyModifiers.Alt, false)]
    [InlineData(0x4B, (KeyModifiers)0x4002, false)]
    public void RejectsSystemAndEditingShortcuts(uint key, KeyModifiers modifiers, bool allowed) =>
        Assert.Equal(allowed, HotKeyRules.IsAllowed(new KeyGesture(key, modifiers)));
    [Fact]
    public void FallbackChainPreservesSavedPreference()
    {
        Assert.Equal([KeyGesture.AltSpace, KeyGesture.ControlAltSpace, HotKeyRules.ControlAltShiftSpace], HotKeyRules.StartupCandidates(KeyGesture.AltSpace));
        var custom = new KeyGesture(0x4B, KeyModifiers.Control | KeyModifiers.Shift);
        Assert.Equal([custom], HotKeyRules.StartupCandidates(custom));
    }
    [Theory]
    [InlineData(0x20, KeyModifiers.Alt, "Alt+Space")]
    [InlineData(0x0D, KeyModifiers.Control | KeyModifiers.Alt | KeyModifiers.Shift, "Ctrl+Alt+Shift+Enter")]
    [InlineData(0x4B, KeyModifiers.Control | KeyModifiers.Shift, "Ctrl+Shift+K")]
    [InlineData(0x25, KeyModifiers.Control | KeyModifiers.Alt, "Ctrl+Alt+←")]
    public void DisplayIsStableAcrossKeyboardLayouts(uint key, KeyModifiers modifiers, string expected) =>
        Assert.Equal(expected, HotKeyRules.Display(new KeyGesture(key, modifiers)));
}

public sealed class KeyboardLanguageTests
{
    [Theory]
    [InlineData(0x04090409L, "en")]
    [InlineData(0x04190419L, "ru")]
    [InlineData(0x043F043FL, "kk")]
    [InlineData(0x043F0419L, "kk")]
    [InlineData(0xF0010419L, "ru")]
    [InlineData(0xE0010409L, "en")]
    [InlineData(0x00000409L, "en")]
    [InlineData(0x04070407L, null)]
    public void UsesLayoutLanguageIncludingKazakhUnderRussian(long layout, string? expected) =>
        Assert.Equal(expected, KeyboardLanguageMap.FromLayout((nint)layout));
}

public sealed class AudioFormatTests
{
    [Fact]
    public void WaveHeaderAndHardLimitMatchBackendPcmContract()
    {
        var path = Path.GetTempFileName();
        try
        {
            using (var writer = new PcmWaveWriter(File.Open(path, FileMode.Create), TimeSpan.FromSeconds(1)))
            {
                Assert.InRange(writer.Append(Enumerable.Repeat((short)16384, 20000).ToArray()), 0.99f, 1f);
                Assert.True(writer.AtLimit);
                Assert.Equal(TimeSpan.FromSeconds(1), writer.Duration);
            }
            var data = File.ReadAllBytes(path);
            Assert.Equal(32044, data.Length);
            Assert.Equal("RIFF"u8.ToArray(), data[..4]);
            Assert.Equal("WAVE"u8.ToArray(), data[8..12]);
            Assert.Equal(32036u, BinaryPrimitives.ReadUInt32LittleEndian(data.AsSpan(4)));
            Assert.Equal(16000u, BinaryPrimitives.ReadUInt32LittleEndian(data.AsSpan(24)));
            Assert.Equal(1, BinaryPrimitives.ReadUInt16LittleEndian(data.AsSpan(22)));
            Assert.Equal(16, BinaryPrimitives.ReadUInt16LittleEndian(data.AsSpan(34)));
            Assert.Equal(32000u, BinaryPrimitives.ReadUInt32LittleEndian(data.AsSpan(40)));
        }
        finally { File.Delete(path); }
    }
    [Fact]
    public void ResamplingPreservesPhaseAcrossPacketsAndDownmixesAllChannels()
    {
        var bytes = new byte[4800 * 2 * 4];
        for (var frame = 0; frame < 4800; frame++)
        {
            BinaryPrimitives.WriteSingleLittleEndian(bytes.AsSpan(frame * 8), 0.75f);
            BinaryPrimitives.WriteSingleLittleEndian(bytes.AsSpan(frame * 8 + 4), 0.25f);
        }
        var whole = new Pcm16MonoConverter(48000, 2, 32, true).Convert(bytes, false);
        var chunked = new Pcm16MonoConverter(48000, 2, 32, true);
        var parts = chunked.Convert(bytes.AsSpan(0, 2408), false).Concat(chunked.Convert(bytes.AsSpan(2408), false)).ToArray();
        Assert.Equal(1600, whole.Length);
        Assert.Equal(whole, parts);
        Assert.All(whole.Skip(32), sample => Assert.InRange(sample, (short)16382, (short)16386));
    }
    [Fact]
    public void MixFormatResamplingFiltersFrequenciesAboveSpeechNyquist()
    {
        static double RmsAt(int frequency)
        {
            var bytes = new byte[4800 * 4];
            for (var i = 0; i < 4800; i++)
                BinaryPrimitives.WriteSingleLittleEndian(bytes.AsSpan(i * 4), (float)Math.Sin(2 * Math.PI * frequency * i / 48000));
            var output = new Pcm16MonoConverter(48000, 1, 32, true).Convert(bytes, false).Skip(64).ToArray();
            return Math.Sqrt(output.Average(sample => (double)sample * sample));
        }
        Assert.True(RmsAt(18000) < RmsAt(1000) * .25);
    }
    [Fact]
    public void SilentDevicePacketsProduceActualZeroSamples()
    {
        var converter = new Pcm16MonoConverter(16000, 1, 16, false);
        var samples = converter.Convert(Enumerable.Repeat((byte)255, 3200).ToArray(), true);
        Assert.Equal(1600, samples.Length);
        Assert.All(samples, sample => Assert.Equal((short)0, sample));
    }
}

[CollectionDefinition("Windows native", DisableParallelization = true)]
public sealed class WindowsNativeTestGroup;

[Collection("Windows native")]
[SupportedOSPlatform("windows")]
public sealed class WindowsNativeTests
{
    [Fact]
    public void InputLayoutHasFullMouseUnionAndControlVReleaseSequence()
    {
        if (!OperatingSystem.IsWindows()) { Assert.Skip("Win32 ABI checks require Windows."); return; }
        Assert.Equal(Environment.Is64BitProcess ? 40 : 28, Marshal.SizeOf<NativeMethods.Input>());
        Assert.Equal(Environment.Is64BitProcess ? 80 : 48, Marshal.SizeOf<NativeMethods.WindowClass>());
        var inputs = WindowsPasteService.CreateControlVInputs(0);
        Assert.Equal(new ushort[] { 0x11, 0x56, 0x56, 0x11 }, inputs.Select(input => input.Data.Keyboard.VirtualKey));
        Assert.Equal(new uint[] { 0, 0, 2, 2 }, inputs.Select(input => input.Data.Keyboard.Flags));
        Assert.All(inputs, input => { Assert.Equal(1u, input.Type); Assert.Equal((nuint)0x41495270, input.Data.Keyboard.ExtraInfo); });
    }
    [Fact]
    public void DpapiSecretsAreKeyAndEnvironmentBoundAndAtomic()
    {
        if (!OperatingSystem.IsWindows()) { Assert.Skip("DPAPI CurrentUser requires Windows."); return; }
        var root = Path.Combine(Path.GetTempPath(), "AIReply-test-" + Guid.NewGuid().ToString("N"));
        try
        {
            var store = new WindowsSecretStore("test", root);
            Assert.Null(store.Read("production/session"));
            store.Write("production/session", "token-кириллица");
            Assert.Equal("token-кириллица", new WindowsSecretStore("test", root).Read("production/session"));
            Assert.Empty(Directory.GetFiles(root, "*.tmp", SearchOption.AllDirectories));
            var source = Directory.GetFiles(root, "*.bin", SearchOption.AllDirectories).Single();
            Assert.DoesNotContain("token-кириллица", System.Text.Encoding.UTF8.GetString(File.ReadAllBytes(source)), StringComparison.Ordinal);
            var destination = Path.Combine(Path.GetDirectoryName(source)!, WindowsSecretStore.FileName("sandbox/session"));
            File.Copy(source, destination);
            Assert.Null(store.Read("sandbox/session"));
            File.WriteAllText(source, "damaged");
            Assert.Null(store.Read("production/session"));
            store.Delete("production/session");
            store.Delete("production/session");
            Assert.Throws<ArgumentException>(() => store.Write("../session", "x"));
            var protector = new WindowsDataProtector("account-a");
            var blob = protector.Protect("secret"u8);
            Assert.Equal("secret"u8.ToArray(), protector.Unprotect(blob));
            Assert.Throws<CryptographicException>(() => new WindowsDataProtector("account-b").Unprotect(blob));
        }
        finally { Directory.Delete(root, true); }
    }
    [Fact]
    public void SingleInstanceKeepsNameUntilDisposed()
    {
        if (!OperatingSystem.IsWindows()) { Assert.Skip("Named Windows session mutex requires Windows."); return; }
        var environment = "test-" + Guid.NewGuid().ToString("N");
        Assert.True(WindowsSingleInstance.TryAcquire(environment, out var first));
        using (first)
        {
            Assert.False(WindowsSingleInstance.TryAcquire(environment, out var second));
            Assert.Null(second);
        }
        Assert.True(WindowsSingleInstance.TryAcquire(environment, out var third));
        third!.Dispose();
    }
    [Fact]
    public void HotKeyRejectsReservedCombosAndPreservesPreviousRegistration()
    {
        if (!OperatingSystem.IsWindows()) { Assert.Skip("RegisterHotKey requires Windows desktop."); return; }
        using var window = new WindowsMessageWindow("test-" + Guid.NewGuid().ToString("N"));
        using var service = new WindowsHotKeyService(window);
        var gesture = new KeyGesture(0x82, KeyModifiers.Control | KeyModifiers.Alt | KeyModifiers.Shift);
        var result = service.Register(gesture);
        if (service.LastError == 1459) { Assert.Skip("No interactive window station."); return; }
        Assert.Equal(HotKeyResult.Ok, result);
        Assert.Equal(HotKeyResult.NotAllowed, service.Register(new KeyGesture(0x43, KeyModifiers.Control)));
        Assert.Equal(gesture, service.Current);
        Assert.Equal(HotKeyResult.Ok, service.Register(gesture));
        service.Unregister(); service.Unregister();
        Assert.Null(service.Current);
    }
    [Fact]
    public void MessageWindowDispatchesCurrentHotkeyAndSecondLaunchButIgnoresStaleIds()
    {
        if (!OperatingSystem.IsWindows()) { Assert.Skip("Win32 message dispatch requires Windows desktop."); return; }
        using var window = new WindowsMessageWindow("test-" + Guid.NewGuid().ToString("N"));
        using var service = new WindowsHotKeyService(window);
        var gesture = new KeyGesture(0x81, KeyModifiers.Control | KeyModifiers.Alt | KeyModifiers.Shift);
        var result = service.Register(gesture);
        if (service.LastError == 1459) { Assert.Skip("No interactive window station."); return; }
        Assert.Equal(HotKeyResult.Ok, result);
        var presses = 0;
        var shows = 0;
        var suspends = 0;
        window.ShowRequested += (_, _) => shows++;
        window.SuspendOrLock += (_, _) => suspends++;
        service.Pressed += (_, _) => presses++;
        NativeMethods.PostMessage(window.Handle, NativeMethods.HotKey, service.CurrentId + 1, 0);
        NativeMethods.PostMessage(window.Handle, NativeMethods.HotKey, service.CurrentId, 0);
        NativeMethods.PostMessage(window.Handle, NativeMethods.RegisterWindowMessage("AIReply.ShowBar"), 0, 0);
        NativeMethods.PostMessage(window.Handle, 0x02B1, 7, 0);
        while (NativeMethods.PeekMessage(out var message, window.Handle, 0, 0, 1)) NativeMethods.DispatchMessage(in message);
        Assert.Equal(1, presses);
        Assert.Equal(1, shows);
        Assert.Equal(1, suspends);
        service.Unregister();
        NativeMethods.PostMessage(window.Handle, NativeMethods.HotKey, service.CurrentId, 0);
        while (NativeMethods.PeekMessage(out var message, window.Handle, 0, 0, 1)) NativeMethods.DispatchMessage(in message);
        Assert.Equal(1, presses);
    }
    [Fact]
    public async Task OwnClipboardCopyIsNeverOfferedBack()
    {
        if (!OperatingSystem.IsWindows()) { Assert.Skip("System clipboard requires Windows desktop."); return; }
        using var window = new WindowsMessageWindow("test-" + Guid.NewGuid().ToString("N"));
        using var clipboard = new WindowsClipboardService(window);
        Assert.Equal(CopiedTextStatus.None, clipboard.TakeFreshCopiedText(TimeSpan.FromMinutes(10)).Status);
        Assert.True(await clipboard.CopyAsync("AI Reply native clipboard test"));
        Assert.Equal(CopiedTextStatus.None, clipboard.TakeFreshCopiedText(TimeSpan.FromMinutes(10)).Status);
    }
}
