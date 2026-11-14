using System.Runtime.InteropServices;
using System.Runtime.Versioning;

namespace AirReply.Platform.Windows;

[SupportedOSPlatform("windows")]
internal static unsafe partial class NativeMethods
{
    internal const uint HotKey = 0x0312, ClipboardUpdate = 0x031D;
    internal const uint UnicodeText = 13, FileDrop = 15;
    internal const uint KeyUp = 2;
    internal const nuint InputMarker = 0x41495270;
    [StructLayout(LayoutKind.Sequential)]
    internal struct WindowClass
    {
        internal uint Size, Style;
        internal delegate* unmanaged<nint, uint, nint, nint, nint> Procedure;
        internal int ClassExtra, WindowExtra;
        internal nint Instance, Icon, Cursor, Background;
        internal char* MenuName;
        internal char* ClassName;
        internal nint SmallIcon;
    }
    [StructLayout(LayoutKind.Sequential)]
    internal struct Input { internal uint Type; internal InputUnion Data; }
    [StructLayout(LayoutKind.Explicit)]
    internal struct InputUnion
    {
        [FieldOffset(0)] internal MouseInput Mouse;
        [FieldOffset(0)] internal KeyboardInput Keyboard;
        [FieldOffset(0)] internal HardwareInput Hardware;
    }
    [StructLayout(LayoutKind.Sequential)]
    internal struct MouseInput { internal int X, Y; internal uint MouseData, Flags, Time; internal nuint ExtraInfo; }
    [StructLayout(LayoutKind.Sequential)]
    internal struct KeyboardInput { internal ushort VirtualKey, Scan; internal uint Flags, Time; internal nuint ExtraInfo; }
    [StructLayout(LayoutKind.Sequential)]
    internal struct HardwareInput { internal uint Message; internal ushort ParamLow, ParamHigh; }
    [StructLayout(LayoutKind.Sequential)]
    internal struct SidAttributes { internal nint Sid; internal uint Attributes; }

    [StructLayout(LayoutKind.Sequential)]
    internal struct Point { internal int X, Y; }
    [StructLayout(LayoutKind.Sequential)]
    internal struct MessageRecord { internal nint Window; internal uint Message; internal nuint WParam; internal nint LParam; internal uint Time; internal Point Point; internal uint Private; }
    [LibraryImport("user32.dll", EntryPoint = "PeekMessageW")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool PeekMessage(out MessageRecord message, nint window, uint min, uint max, uint flags);
    [LibraryImport("user32.dll", EntryPoint = "DispatchMessageW")] internal static partial nint DispatchMessage(in MessageRecord message);
    [LibraryImport("user32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool GetCursorPos(out Point point);
    [LibraryImport("user32.dll")] internal static partial nint RegisterSuspendResumeNotification(nint recipient, uint flags);
    [LibraryImport("user32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool UnregisterSuspendResumeNotification(nint registration);
    [LibraryImport("wtsapi32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool WTSRegisterSessionNotification(nint window, uint flags);
    [LibraryImport("wtsapi32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool WTSUnRegisterSessionNotification(nint window);
    [LibraryImport("user32.dll", EntryPoint = "RegisterClassExW", SetLastError = true)] internal static partial ushort RegisterClass(WindowClass* value);
    [LibraryImport("user32.dll", EntryPoint = "UnregisterClassW", StringMarshalling = StringMarshalling.Utf16)] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool UnregisterClass(string name, nint instance);
    [LibraryImport("user32.dll", EntryPoint = "CreateWindowExW", StringMarshalling = StringMarshalling.Utf16, SetLastError = true)] internal static partial nint CreateWindow(uint exStyle, string className, string title, uint style, int x, int y, int width, int height, nint parent, nint menu, nint instance, nint parameter);
    [LibraryImport("user32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool DestroyWindow(nint window);
    [LibraryImport("user32.dll", EntryPoint = "DefWindowProcW")] internal static partial nint DefWindowProc(nint window, uint message, nint wParam, nint lParam);
    [LibraryImport("kernel32.dll", EntryPoint = "GetModuleHandleW", StringMarshalling = StringMarshalling.Utf16)] internal static partial nint GetModuleHandle(string? name);
    [LibraryImport("user32.dll", EntryPoint = "RegisterWindowMessageW", StringMarshalling = StringMarshalling.Utf16)] internal static partial uint RegisterWindowMessage(string message);
    [LibraryImport("user32.dll", EntryPoint = "FindWindowExW", StringMarshalling = StringMarshalling.Utf16)] internal static partial nint FindWindowEx(nint parent, nint after, string? className, string? title);
    [LibraryImport("user32.dll", EntryPoint = "PostMessageW")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool PostMessage(nint window, uint message, nint wParam, nint lParam);
    [LibraryImport("user32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool ChangeWindowMessageFilterEx(nint window, uint message, uint action, nint filter);
    [LibraryImport("user32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool AllowSetForegroundWindow(uint process);
    [LibraryImport("user32.dll", SetLastError = true)] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool RegisterHotKey(nint window, int id, uint modifiers, uint key);
    [LibraryImport("user32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool UnregisterHotKey(nint window, int id);
    [LibraryImport("user32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool AddClipboardFormatListener(nint window);
    [LibraryImport("user32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool RemoveClipboardFormatListener(nint window);
    [LibraryImport("user32.dll")] internal static partial uint GetClipboardSequenceNumber();
    [LibraryImport("user32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool OpenClipboard(nint window);
    [LibraryImport("user32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool CloseClipboard();
    [LibraryImport("user32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool EmptyClipboard();
    [LibraryImport("user32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool IsClipboardFormatAvailable(uint format);
    [LibraryImport("user32.dll")] internal static partial nint GetClipboardData(uint format);
    [LibraryImport("user32.dll")] internal static partial nint SetClipboardData(uint format, nint memory);
    [LibraryImport("user32.dll")] internal static partial nint GetClipboardOwner();
    [LibraryImport("user32.dll", EntryPoint = "RegisterClipboardFormatW", StringMarshalling = StringMarshalling.Utf16)] internal static partial uint RegisterClipboardFormat(string format);
    [LibraryImport("kernel32.dll")] internal static partial nint GlobalAlloc(uint flags, nuint bytes);
    [LibraryImport("kernel32.dll")] internal static partial void* GlobalLock(nint memory);
    [LibraryImport("kernel32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool GlobalUnlock(nint memory);
    [LibraryImport("kernel32.dll")] internal static partial nuint GlobalSize(nint memory);
    [LibraryImport("kernel32.dll")] internal static partial nint GlobalFree(nint memory);
    [LibraryImport("user32.dll")] internal static partial nint GetForegroundWindow();
    [LibraryImport("user32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool SetForegroundWindow(nint window);
    [LibraryImport("user32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool BringWindowToTop(nint window);
    [LibraryImport("user32.dll")] internal static partial uint GetWindowThreadProcessId(nint window, out uint processId);
    [LibraryImport("user32.dll")] internal static partial nint GetAncestor(nint window, uint flags);
    [LibraryImport("user32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool IsWindow(nint window);
    [LibraryImport("user32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool IsIconic(nint window);
    [LibraryImport("user32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool IsHungAppWindow(nint window);
    [LibraryImport("user32.dll", EntryPoint = "GetClassNameW")] internal static partial int GetClassName(nint window, char* buffer, int count);
    [LibraryImport("user32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool AttachThreadInput(uint current, uint other, [MarshalAs(UnmanagedType.Bool)] bool attach);
    [LibraryImport("kernel32.dll")] internal static partial uint GetCurrentThreadId();
    [LibraryImport("user32.dll")] internal static partial nint SetWinEventHook(uint min, uint max, nint module, delegate* unmanaged<nint, uint, nint, int, int, uint, uint, void> callback, uint process, uint thread, uint flags);
    [LibraryImport("user32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool UnhookWinEvent(nint hook);
    [LibraryImport("user32.dll")] internal static partial nint GetKeyboardLayout(uint thread);
    [LibraryImport("user32.dll", EntryPoint = "MapVirtualKeyExW")] internal static partial uint MapVirtualKeyEx(uint code, uint type, nint layout);
    [LibraryImport("user32.dll")] internal static partial short GetAsyncKeyState(int key);
    [LibraryImport("user32.dll", SetLastError = true)] internal static partial uint SendInput(uint count, Input* inputs, int size);
    [LibraryImport("kernel32.dll", SetLastError = true)] internal static partial nint OpenProcess(uint access, [MarshalAs(UnmanagedType.Bool)] bool inherit, uint process);
    [LibraryImport("kernel32.dll")] internal static partial nint GetCurrentProcess();
    [LibraryImport("kernel32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool CloseHandle(nint handle);
    [LibraryImport("advapi32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool OpenProcessToken(nint process, uint access, out nint token);
    [LibraryImport("advapi32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool GetTokenInformation(nint token, int information, void* buffer, uint length, out uint needed);
    [LibraryImport("advapi32.dll")] internal static partial byte* GetSidSubAuthorityCount(nint sid);
    [LibraryImport("advapi32.dll")] internal static partial uint* GetSidSubAuthority(nint sid, uint index);
    [LibraryImport("kernel32.dll", EntryPoint = "MoveFileExW", StringMarshalling = StringMarshalling.Utf16, SetLastError = true)] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool MoveFileEx(string source, string target, uint flags);
}
