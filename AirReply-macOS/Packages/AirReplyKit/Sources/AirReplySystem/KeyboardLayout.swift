import Carbon.HIToolbox

/// The most recently used ASCII-capable keyboard layout (ABC, US, Dvorak…). macOS resolves ⌘
/// shortcuts through it while a Russian or Kazakh layout is active, so key caps come from it too.
/// Text Input Sources must be used on the main thread.
@MainActor
struct KeyboardLayout {
    private let layoutData: CFData

    static func currentASCIICapable() -> KeyboardLayout? {
        guard let source = TISCopyCurrentASCIICapableKeyboardLayoutInputSource()?.takeRetainedValue(),
              let property = TISGetInputSourceProperty(source, kTISPropertyUnicodeKeyLayoutData) else { return nil }
        return KeyboardLayout(layoutData: Unmanaged<CFData>.fromOpaque(property).takeUnretainedValue())
    }

    /// The printable text a key types with `carbonModifiers` held; nil for dead, control and blank keys.
    func character(forKeyCode keyCode: UInt16, carbonModifiers: UInt32 = 0) -> String? {
        guard let bytes = CFDataGetBytePtr(layoutData) else { return nil }
        let layout = UnsafeRawPointer(bytes).assumingMemoryBound(to: UCKeyboardLayout.self)
        let maxLength = 4
        var characters = [UniChar](repeating: 0, count: maxLength)
        var length = 0
        var deadKeyState: UInt32 = 0
        let status = UCKeyTranslate(
            layout, keyCode, UInt16(kUCKeyActionDisplay), (carbonModifiers >> 8) & 0xFF, UInt32(LMGetKbdType()),
            OptionBits(kUCKeyTranslateNoDeadKeysMask), &deadKeyState, maxLength, &length, &characters
        )
        guard status == noErr, length > 0 else { return nil }
        let text = String(decoding: characters.prefix(length), as: UTF16.self)
        let printable = text.unicodeScalars.allSatisfy {
            $0.properties.generalCategory != .control && !$0.properties.isWhitespace
        }
        return printable ? text : nil
    }

    /// The key that types `character` while ⌘ is held: `preferred` when it does (ABC, US, and
    /// every Mac on a Russian or Kazakh layout), otherwise the key that does (V moves in Dvorak).
    func keyCode(typingWithCommand character: String, preferring preferred: UInt16) -> UInt16? {
        let command = UInt32(cmdKey)
        func types(_ keyCode: UInt16) -> Bool {
            self.character(forKeyCode: keyCode, carbonModifiers: command)?.lowercased() == character
        }
        return types(preferred) ? preferred : (0..<128).first(where: types)
    }
}
