using System.Runtime.Versioning;
using AirReply.Presentation.Platform;

namespace AirReply.Platform.Windows;

[SupportedOSPlatform("windows")]
public sealed class WindowsPasteService(IFocusTracker focus) : IPasteService
{
    public PasteAvailability GetAvailability(nint targetWindow)
    {
        if (targetWindow == 0 || targetWindow != focus.PreviousWindow || !NativeMethods.IsWindow(targetWindow) || NativeMethods.IsIconic(targetWindow))
            return PasteAvailability.NoTarget;
        NativeMethods.GetWindowThreadProcessId(targetWindow, out var processId);
        var process = NativeMethods.OpenProcess(0x1000, false, processId);
        if (process == 0) return PasteAvailability.TargetElevated;
        try
        {
            var targetIntegrity = Integrity(process);
            var ownIntegrity = Integrity(NativeMethods.GetCurrentProcess());
            return targetIntegrity == null || ownIntegrity == null || targetIntegrity > ownIntegrity
                ? PasteAvailability.TargetElevated : PasteAvailability.Available;
        }
        finally { NativeMethods.CloseHandle(process); }
    }
    private static unsafe uint? Integrity(nint process)
    {
        if (!NativeMethods.OpenProcessToken(process, 8, out var token)) return null;
        try
        {
            NativeMethods.GetTokenInformation(token, 25, null, 0, out var size);
            if (size is 0 or > 65536) return null;
            var buffer = new byte[size];
            fixed (byte* pointer = buffer)
            {
                if (!NativeMethods.GetTokenInformation(token, 25, pointer, size, out _)) return null;
                var sid = ((NativeMethods.SidAttributes*)pointer)->Sid;
                var count = NativeMethods.GetSidSubAuthorityCount(sid);
                if (count == null || *count == 0) return null;
                return *NativeMethods.GetSidSubAuthority(sid, (uint)*count - 1);
            }
        }
        finally { NativeMethods.CloseHandle(token); }
    }
    public async Task<bool> PasteAsync(nint targetWindow, CancellationToken cancellationToken = default)
    {
        if (GetAvailability(targetWindow) != PasteAvailability.Available || cancellationToken.IsCancellationRequested) return false;
        WindowsFocusTracker.Activate(targetWindow);
        try
        {
            var activated = false;
            for (var attempt = 0; attempt < 12; attempt++)
            {
                if (WindowsFocusTracker.IsForeground(targetWindow)) { activated = true; break; }
                await Task.Delay(50, cancellationToken);
            }
            if (!activated) return false;
            await Task.Delay(50, cancellationToken);
            for (var attempt = 0; attempt < 12 && ModifiersHeld(); attempt++) await Task.Delay(50, cancellationToken);
            cancellationToken.ThrowIfCancellationRequested();
            if (ModifiersHeld() || !WindowsFocusTracker.IsForeground(targetWindow) || GetAvailability(targetWindow) != PasteAvailability.Available) return false;
            return SendControlV(targetWindow);
        }
        catch (OperationCanceledException) { return false; }
    }
    private static readonly int[] ModifierKeys = [0x10, 0x11, 0x12, 0x5B, 0x5C];
    private static bool ModifiersHeld() => ModifierKeys.Any(key => NativeMethods.GetAsyncKeyState(key) < 0);
    internal static NativeMethods.Input[] CreateControlVInputs(nint layout)
    {
        var control = (ushort)NativeMethods.MapVirtualKeyEx(0x11, 0, layout);
        var v = (ushort)NativeMethods.MapVirtualKeyEx(0x56, 0, layout);
        return [Key(0x11, control, 0), Key(0x56, v, 0), Key(0x56, v, NativeMethods.KeyUp), Key(0x11, control, NativeMethods.KeyUp)];
        static NativeMethods.Input Key(ushort key, ushort scan, uint flags) => new()
        {
            Type = 1, Data = new NativeMethods.InputUnion { Keyboard = new NativeMethods.KeyboardInput
                { VirtualKey = key, Scan = scan, Flags = flags, ExtraInfo = NativeMethods.InputMarker } },
        };
    }
    private static unsafe bool SendControlV(nint target)
    {
        var thread = NativeMethods.GetWindowThreadProcessId(target, out _);
        var inputs = CreateControlVInputs(NativeMethods.GetKeyboardLayout(thread));
        fixed (NativeMethods.Input* pointer = inputs)
        {
            var count = NativeMethods.SendInput(4, pointer, sizeof(NativeMethods.Input));
            if (count == 4) return true;
            // A partial insertion must not leave Ctrl logically held down.
            var release = inputs[3];
            NativeMethods.SendInput(1, &release, sizeof(NativeMethods.Input));
            return false;
        }
    }
}
