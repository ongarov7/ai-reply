import AppKit
import Carbon.HIToolbox

/// A global shortcut in Carbon terms (virtual key code + Carbon modifier mask),
/// stored as-is in UserDefaults.
public struct KeyCombo: Codable, Hashable, Sendable {
    /// Carbon virtual key code (`kVK_*`), layout-independent.
    public var keyCode: UInt32
    /// Carbon modifier mask: `cmdKey | optionKey | controlKey | shiftKey`.
    public var modifiers: UInt32

    public init(keyCode: UInt32, modifiers: UInt32) {
        self.keyCode = keyCode
        self.modifiers = modifiers
    }

    /// ⌥Space — the default, as in other AI quick bars.
    public static let optionSpace = KeyCombo(keyCode: UInt32(kVK_Space), modifiers: UInt32(optionKey))
    /// ⌃⌥Space — used when ⌥Space cannot be registered on this Mac.
    public static let controlOptionSpace = KeyCombo(keyCode: UInt32(kVK_Space), modifiers: UInt32(optionKey | controlKey))

    /// Human-readable form in the macOS order ⌃⌥⇧⌘ + key, e.g. "⌥Space".
    public var displayString: String {
        displayString { keyCode in
            // Text Input Sources may only be used on the main thread; elsewhere the US key caps stand in.
            guard Thread.isMainThread else { return nil }
            return MainActor.assumeIsolated { KeyboardLayout.currentASCIICapable()?.character(forKeyCode: keyCode) }
        }
    }

    /// Whether the combination may be used as a global shortcut: needs ⌘, ⌥ or ⌃,
    /// and must not collide with shortcuts macOS reserves (⌘Space, ⌃Space, ⌘Tab, ⌘Q, …).
    public var isAllowedAsGlobalShortcut: Bool {
        modifiers & Self.requiredModifiers != 0
            && modifiers & ~Self.knownModifiers == 0
            && !Self.modifierKeyCodes.contains(keyCode)
            && !Self.reserved.contains(self)
    }

    /// Whether an enabled shortcut in System Settings → Keyboard → Keyboard Shortcuts uses this
    /// combination. macOS handles those keystrokes itself, so a hotkey registered on one may never
    /// fire: ⌃⌥Space, for example, selects the next input source on Macs with several layouts.
    @MainActor
    public var isUsedBySystem: Bool {
        var array: Unmanaged<CFArray>?
        guard CopySymbolicHotKeys(&array) == noErr,
              let entries = array?.takeRetainedValue() as? [[String: Any]] else { return false }
        return Self.enabledSymbolicHotKeys(entries).contains(self)
    }

    /// Builds a combo from a key-down event captured by the shortcut recorder.
    public init?(event: NSEvent) {
        guard event.type == .keyDown else { return nil }
        let keyCode = UInt32(event.keyCode)
        guard !Self.modifierKeyCodes.contains(keyCode) else { return nil }
        // Caps Lock, fn and the keypad flag describe the key, not the shortcut.
        let flags = event.modifierFlags
        var modifiers: UInt32 = 0
        if flags.contains(.command) { modifiers |= UInt32(cmdKey) }
        if flags.contains(.option) { modifiers |= UInt32(optionKey) }
        if flags.contains(.control) { modifiers |= UInt32(controlKey) }
        if flags.contains(.shift) { modifiers |= UInt32(shiftKey) }
        self.init(keyCode: keyCode, modifiers: modifiers)
    }

    /// `displayString` with `translate` giving the character a key types in the user's layout, or nil.
    func displayString(translate: (UInt16) -> String?) -> String {
        var symbols = ""
        if modifiers & UInt32(controlKey) != 0 { symbols += "⌃" }
        if modifiers & UInt32(optionKey) != 0 { symbols += "⌥" }
        if modifiers & UInt32(shiftKey) != 0 { symbols += "⇧" }
        if modifiers & UInt32(cmdKey) != 0 { symbols += "⌘" }
        return symbols + keyName(translate: translate)
    }

    private func keyName(translate: (UInt16) -> String?) -> String {
        if let name = Self.namedKeys[keyCode] { return name }
        if let code = UInt16(exactly: keyCode), let typed = translate(code) {
            let upper = typed.uppercased()
            // "ß" would become "SS": a key cap shows one character.
            return upper.count == typed.count ? upper : typed
        }
        return Self.ansiKeyCaps[keyCode] ?? "Key \(keyCode)"
    }

    /// Enabled entries of `CopySymbolicHotKeys`, reduced to the four modifiers (arrow and function
    /// keys also carry the fn bit there).
    static func enabledSymbolicHotKeys(_ entries: [[String: Any]]) -> Set<KeyCombo> {
        Set(entries.compactMap { entry in
            guard entry[kHISymbolicHotKeyEnabled] as? Bool == true,
                  let code = (entry[kHISymbolicHotKeyCode] as? Int).flatMap(UInt32.init(exactly:)),
                  let mask = (entry[kHISymbolicHotKeyModifiers] as? Int).flatMap(UInt32.init(exactly:))
            else { return nil }
            return KeyCombo(keyCode: code, modifiers: mask & knownModifiers)
        })
    }

    private static let requiredModifiers = UInt32(cmdKey | optionKey | controlKey)
    private static let knownModifiers = UInt32(cmdKey | optionKey | controlKey | shiftKey)

    private static let modifierKeyCodes = Set([
        kVK_Command, kVK_RightCommand, kVK_Shift, kVK_RightShift, kVK_Option, kVK_RightOption,
        kVK_Control, kVK_RightControl, kVK_CapsLock, kVK_Function,
    ].map { UInt32($0) })

    /// System shortcuts on by default, and the editing keys everyone relies on — including the
    /// ⌘C and ⌘V of AI Reply's own copy-and-paste flow.
    private static let reserved: Set<KeyCombo> = {
        func combo(_ key: Int, _ modifiers: Int) -> KeyCombo {
            KeyCombo(keyCode: UInt32(key), modifiers: UInt32(modifiers))
        }
        return [
            // Spotlight, input sources, Finder search, emoji & symbols.
            combo(kVK_Space, cmdKey), combo(kVK_Space, controlKey),
            combo(kVK_Space, cmdKey | optionKey), combo(kVK_Space, controlKey | cmdKey),
            // App and window switching.
            combo(kVK_Tab, cmdKey), combo(kVK_Tab, shiftKey | cmdKey),
            combo(kVK_ANSI_Grave, cmdKey), combo(kVK_ANSI_Grave, shiftKey | cmdKey),
            // Quit, close, hide, minimize; force quit, lock screen, log out; Dock hiding.
            combo(kVK_ANSI_Q, cmdKey), combo(kVK_ANSI_W, cmdKey), combo(kVK_ANSI_H, cmdKey), combo(kVK_ANSI_M, cmdKey),
            combo(kVK_Escape, optionKey | cmdKey), combo(kVK_ANSI_Q, controlKey | cmdKey),
            combo(kVK_ANSI_Q, shiftKey | cmdKey), combo(kVK_ANSI_D, optionKey | cmdKey),
            // Screenshots and screen recording.
            combo(kVK_ANSI_3, shiftKey | cmdKey), combo(kVK_ANSI_4, shiftKey | cmdKey), combo(kVK_ANSI_5, shiftKey | cmdKey),
            combo(kVK_ANSI_3, controlKey | shiftKey | cmdKey), combo(kVK_ANSI_4, controlKey | shiftKey | cmdKey),
            // Mission Control, App Exposé and switching spaces.
            combo(kVK_LeftArrow, controlKey), combo(kVK_RightArrow, controlKey),
            combo(kVK_UpArrow, controlKey), combo(kVK_DownArrow, controlKey),
            // Editing.
            combo(kVK_ANSI_C, cmdKey), combo(kVK_ANSI_V, cmdKey), combo(kVK_ANSI_X, cmdKey),
            combo(kVK_ANSI_A, cmdKey), combo(kVK_ANSI_Z, cmdKey), combo(kVK_ANSI_Z, shiftKey | cmdKey),
        ]
    }()

    private static func keyTable(_ pairs: [(Int, String)]) -> [UInt32: String] {
        Dictionary(uniqueKeysWithValues: pairs.map { (UInt32($0.0), $0.1) })
    }

    /// Keys shown by name or symbol in any layout, as in macOS menus.
    private static let namedKeys = keyTable([
        (kVK_Space, "Space"), (kVK_Return, "Return"), (kVK_ANSI_KeypadEnter, "Enter"), (kVK_Tab, "Tab"),
        (kVK_Escape, "Esc"), (kVK_Delete, "Delete"), (kVK_ForwardDelete, "⌦"), (kVK_Help, "Help"),
        (kVK_LeftArrow, "←"), (kVK_RightArrow, "→"), (kVK_UpArrow, "↑"), (kVK_DownArrow, "↓"),
        (kVK_Home, "↖"), (kVK_End, "↘"), (kVK_PageUp, "⇞"), (kVK_PageDown, "⇟"), (kVK_ANSI_KeypadClear, "⌧"),
        (kVK_JIS_Eisu, "英数"), (kVK_JIS_Kana, "かな"),
        (kVK_Command, "⌘"), (kVK_RightCommand, "⌘"), (kVK_Shift, "⇧"), (kVK_RightShift, "⇧"),
        (kVK_Option, "⌥"), (kVK_RightOption, "⌥"), (kVK_Control, "⌃"), (kVK_RightControl, "⌃"),
        (kVK_CapsLock, "⇪"), (kVK_Function, "fn"),
        (kVK_F1, "F1"), (kVK_F2, "F2"), (kVK_F3, "F3"), (kVK_F4, "F4"), (kVK_F5, "F5"),
        (kVK_F6, "F6"), (kVK_F7, "F7"), (kVK_F8, "F8"), (kVK_F9, "F9"), (kVK_F10, "F10"),
        (kVK_F11, "F11"), (kVK_F12, "F12"), (kVK_F13, "F13"), (kVK_F14, "F14"), (kVK_F15, "F15"),
        (kVK_F16, "F16"), (kVK_F17, "F17"), (kVK_F18, "F18"), (kVK_F19, "F19"), (kVK_F20, "F20"),
    ])

    /// US key caps, used when the keyboard layout cannot be asked.
    private static let ansiKeyCaps: [UInt32: String] = {
        let letters = [
            kVK_ANSI_A, kVK_ANSI_B, kVK_ANSI_C, kVK_ANSI_D, kVK_ANSI_E, kVK_ANSI_F, kVK_ANSI_G, kVK_ANSI_H, kVK_ANSI_I,
            kVK_ANSI_J, kVK_ANSI_K, kVK_ANSI_L, kVK_ANSI_M, kVK_ANSI_N, kVK_ANSI_O, kVK_ANSI_P, kVK_ANSI_Q, kVK_ANSI_R,
            kVK_ANSI_S, kVK_ANSI_T, kVK_ANSI_U, kVK_ANSI_V, kVK_ANSI_W, kVK_ANSI_X, kVK_ANSI_Y, kVK_ANSI_Z,
        ]
        let digits = [
            kVK_ANSI_0, kVK_ANSI_1, kVK_ANSI_2, kVK_ANSI_3, kVK_ANSI_4,
            kVK_ANSI_5, kVK_ANSI_6, kVK_ANSI_7, kVK_ANSI_8, kVK_ANSI_9,
        ]
        let keypadDigits = [
            kVK_ANSI_Keypad0, kVK_ANSI_Keypad1, kVK_ANSI_Keypad2, kVK_ANSI_Keypad3, kVK_ANSI_Keypad4,
            kVK_ANSI_Keypad5, kVK_ANSI_Keypad6, kVK_ANSI_Keypad7, kVK_ANSI_Keypad8, kVK_ANSI_Keypad9,
        ]
        return keyTable(
            zip(letters, "ABCDEFGHIJKLMNOPQRSTUVWXYZ").map { ($0, String($1)) }
                + zip(digits, "0123456789").map { ($0, String($1)) }
                + zip(keypadDigits, "0123456789").map { ($0, String($1)) }
                + [
                    (kVK_ANSI_Minus, "-"), (kVK_ANSI_Equal, "="), (kVK_ANSI_LeftBracket, "["), (kVK_ANSI_RightBracket, "]"),
                    (kVK_ANSI_Backslash, "\\"), (kVK_ANSI_Semicolon, ";"), (kVK_ANSI_Quote, "'"), (kVK_ANSI_Comma, ","),
                    (kVK_ANSI_Period, "."), (kVK_ANSI_Slash, "/"), (kVK_ANSI_Grave, "`"), (kVK_ISO_Section, "§"),
                    (kVK_ANSI_KeypadDecimal, "."), (kVK_ANSI_KeypadMultiply, "*"), (kVK_ANSI_KeypadPlus, "+"),
                    (kVK_ANSI_KeypadDivide, "/"), (kVK_ANSI_KeypadMinus, "-"), (kVK_ANSI_KeypadEquals, "="),
                    (kVK_JIS_Yen, "¥"), (kVK_JIS_Underscore, "_"), (kVK_JIS_KeypadComma, ","),
                ]
        )
    }()
}
