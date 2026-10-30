import AppKit
import Carbon.HIToolbox
import Foundation
import Testing
@testable import AirReplySystem

func combo(_ key: Int, _ modifiers: Int = 0) -> KeyCombo {
    KeyCombo(keyCode: UInt32(key), modifiers: UInt32(modifiers))
}

private let reservedCombos = [
    combo(kVK_Space, cmdKey), combo(kVK_Space, controlKey), combo(kVK_Space, cmdKey | optionKey),
    combo(kVK_Space, controlKey | cmdKey), combo(kVK_Tab, cmdKey), combo(kVK_Tab, cmdKey | shiftKey),
    combo(kVK_ANSI_Grave, cmdKey), combo(kVK_ANSI_Q, cmdKey), combo(kVK_ANSI_W, cmdKey), combo(kVK_ANSI_H, cmdKey),
    combo(kVK_ANSI_M, cmdKey), combo(kVK_Escape, cmdKey | optionKey), combo(kVK_ANSI_Q, controlKey | cmdKey),
    combo(kVK_ANSI_3, cmdKey | shiftKey), combo(kVK_ANSI_4, cmdKey | shiftKey), combo(kVK_ANSI_5, cmdKey | shiftKey),
    combo(kVK_LeftArrow, controlKey), combo(kVK_RightArrow, controlKey), combo(kVK_UpArrow, controlKey),
    combo(kVK_DownArrow, controlKey), combo(kVK_ANSI_C, cmdKey), combo(kVK_ANSI_V, cmdKey),
]

@Suite("Key combos")
struct KeyComboTests {
    @Test func namedKeysInMacModifierOrder() {
        #expect(KeyCombo.optionSpace.displayString == "⌥Space")
        #expect(KeyCombo.controlOptionSpace.displayString == "⌃⌥Space")
        #expect(combo(kVK_F5).displayString == "F5")
        #expect(combo(kVK_LeftArrow).displayString == "←")
        #expect(combo(kVK_LeftArrow, optionKey | controlKey).displayString == "⌃⌥←")
        #expect(combo(kVK_Return, cmdKey | shiftKey | optionKey | controlKey).displayString == "⌃⌥⇧⌘Return")
        #expect(combo(kVK_Escape, cmdKey | optionKey).displayString == "⌥⌘Esc")
        #expect(combo(kVK_Delete, cmdKey).displayString == "⌘Delete")
        #expect(combo(kVK_Tab, controlKey).displayString == "⌃Tab")
        #expect(combo(kVK_F20, shiftKey).displayString == "⇧F20")
    }

    @Test func characterKeysShowTheLayoutLetterInUpperCase() {
        let commandShiftK = combo(kVK_ANSI_K, cmdKey | shiftKey)
        #expect(commandShiftK.displayString(translate: { _ in "k" }) == "⇧⌘K")
        // Without a layout the US key caps stand in.
        #expect(commandShiftK.displayString(translate: { _ in nil }) == "⇧⌘K")
        #expect(combo(kVK_ANSI_Slash, optionKey).displayString(translate: { _ in nil }) == "⌥/")
        #expect(combo(kVK_ANSI_7, controlKey).displayString(translate: { _ in nil }) == "⌃7")
        #expect(combo(0x6E, cmdKey).displayString(translate: { _ in nil }) == "⌘Key 110")
        // A key cap is one character even where the upper case is two.
        #expect(combo(kVK_ANSI_Minus, cmdKey).displayString(translate: { _ in "ß" }) == "⌘ß")
        // Named keys never ask the layout.
        let space = KeyCombo.optionSpace.displayString { _ in
            Issue.record("the layout was asked for Space")
            return "?"
        }
        #expect(space == "⌥Space")
    }

    @MainActor
    @Test func onTheMainThreadLettersComeFromTheASCIICapableLayout() {
        // ABC, US or Dvorak: one capital Latin letter, also while a Russian or Kazakh layout is selected.
        let shown = combo(kVK_ANSI_K, cmdKey | shiftKey).displayString
        #expect(shown.hasPrefix("⇧⌘"))
        let key = shown.dropFirst(2)
        #expect(key.count == 1)
        #expect(key.allSatisfy { $0.isASCII && $0.isUppercase })
    }

    @Test func offTheMainThreadUSKeyCapsStandIn() async {
        let shown = await Task.detached { combo(kVK_ANSI_K, cmdKey | shiftKey).displayString }.value
        #expect(shown == "⇧⌘K")
    }

    @Test func allowedCombos() {
        let allowed = [
            KeyCombo.optionSpace, .controlOptionSpace, combo(kVK_ANSI_K, cmdKey | shiftKey), combo(kVK_F5, controlKey),
            combo(kVK_Space, shiftKey | cmdKey), combo(kVK_LeftArrow, controlKey | optionKey),
            combo(kVK_ANSI_6, shiftKey | cmdKey), combo(kVK_ANSI_R, optionKey | cmdKey), combo(kVK_Return, optionKey),
        ]
        for shortcut in allowed {
            #expect(shortcut.isAllowedAsGlobalShortcut, "\(shortcut.displayString)")
        }
    }

    @Test(arguments: [kVK_Escape, kVK_Return, kVK_Tab, kVK_Space, kVK_ANSI_K, kVK_F5])
    func plainAndShiftOnlyAreNotEnough(key: Int) {
        #expect(!combo(key).isAllowedAsGlobalShortcut)
        #expect(!combo(key, shiftKey).isAllowedAsGlobalShortcut)
    }

    @Test(arguments: [
        kVK_Command, kVK_RightCommand, kVK_Shift, kVK_RightShift, kVK_Option, kVK_RightOption,
        kVK_Control, kVK_RightControl, kVK_CapsLock, kVK_Function,
    ])
    func modifierKeysAreNotShortcutKeys(key: Int) {
        #expect(!combo(key, cmdKey | optionKey).isAllowedAsGlobalShortcut)
    }

    @Test func unknownModifierBitsAreRejected() {
        #expect(!combo(kVK_ANSI_K, cmdKey | alphaLock).isAllowedAsGlobalShortcut)
        #expect(!combo(kVK_ANSI_K, cmdKey | rightShiftKey).isAllowedAsGlobalShortcut)
    }

    @Test(arguments: reservedCombos)
    func reservedCombosAreRejected(_ reserved: KeyCombo) {
        #expect(!reserved.isAllowedAsGlobalShortcut, "\(reserved.displayString)")
    }

    @Test func codableRoundTripKeepsTheStoredShape() throws {
        let original = combo(kVK_ANSI_K, cmdKey | shiftKey)
        #expect(try JSONDecoder().decode(KeyCombo.self, from: JSONEncoder().encode(original)) == original)
        let stored = Data(#"{"keyCode":49,"modifiers":2048}"#.utf8)
        #expect(try JSONDecoder().decode(KeyCombo.self, from: stored) == .optionSpace)
    }

    @MainActor
    @Test func buildsFromKeyDown() throws {
        #expect(KeyCombo(event: try keyEvent(.keyDown, kVK_ANSI_K, [.command, .shift])) == combo(kVK_ANSI_K, cmdKey | shiftKey))
        #expect(KeyCombo(event: try keyEvent(.keyDown, kVK_Space, [.control, .option, .shift, .command]))
            == combo(kVK_Space, controlKey | optionKey | shiftKey | cmdKey))
        #expect(KeyCombo(event: try keyEvent(.keyDown, kVK_F5, [])) == combo(kVK_F5))
    }

    @MainActor
    @Test func ignoresCapsLockFnAndKeypadFlags() throws {
        let event = try keyEvent(.keyDown, kVK_UpArrow, [.option, .capsLock, .function, .numericPad])
        #expect(KeyCombo(event: event) == combo(kVK_UpArrow, optionKey))
    }

    @MainActor
    @Test func rejectsKeyUpAndModifierOnlyEvents() throws {
        #expect(KeyCombo(event: try keyEvent(.keyUp, kVK_ANSI_K, .command)) == nil)
        #expect(KeyCombo(event: try keyEvent(.keyDown, kVK_Command, .command)) == nil)
        #expect(KeyCombo(event: try keyEvent(.flagsChanged, kVK_Option, .option)) == nil)
    }

    @Test func systemShortcutsAreReducedToEnabledCombos() {
        let entries: [[String: Any]] = [
            // ⌃⌥Space, "Select next source in Input menu".
            [kHISymbolicHotKeyCode: 49, kHISymbolicHotKeyModifiers: 0x1800, kHISymbolicHotKeyEnabled: true],
            // ⌃↑ Mission Control, stored with the fn bit.
            [kHISymbolicHotKeyCode: 126, kHISymbolicHotKeyModifiers: 0x21000, kHISymbolicHotKeyEnabled: true],
            [kHISymbolicHotKeyCode: 49, kHISymbolicHotKeyModifiers: 0x900, kHISymbolicHotKeyEnabled: false],
            [kHISymbolicHotKeyCode: 65535, kHISymbolicHotKeyEnabled: true],
        ]
        #expect(KeyCombo.enabledSymbolicHotKeys(entries) == [.controlOptionSpace, combo(kVK_UpArrow, controlKey)])
    }

    @MainActor
    @Test func systemShortcutLookupRunsOnThisMac() {
        #expect(!combo(kVK_F19, controlKey | optionKey | shiftKey | cmdKey).isUsedBySystem)
    }

    @MainActor
    @Test func layoutFindsTheKeyThatPastes() throws {
        let layout = try #require(KeyboardLayout.currentASCIICapable())
        let key = try #require(layout.keyCode(typingWithCommand: "v", preferring: UInt16(kVK_ANSI_V)))
        #expect(layout.character(forKeyCode: key, carbonModifiers: UInt32(cmdKey))?.lowercased() == "v")
        #expect(layout.character(forKeyCode: UInt16(kVK_Return)) == nil)
        #expect(layout.character(forKeyCode: UInt16(kVK_Space)) == nil)
    }

    private func keyEvent(_ type: NSEvent.EventType, _ key: Int, _ flags: NSEvent.ModifierFlags) throws -> NSEvent {
        try #require(NSEvent.keyEvent(
            with: type, location: .zero, modifierFlags: flags, timestamp: 0, windowNumber: 0, context: nil,
            characters: "", charactersIgnoringModifiers: "", isARepeat: false, keyCode: UInt16(key)
        ))
    }
}
