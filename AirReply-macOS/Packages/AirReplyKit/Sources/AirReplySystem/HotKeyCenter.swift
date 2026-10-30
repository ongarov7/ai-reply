import AppKit
import Carbon.HIToolbox

/// One global hotkey through `RegisterEventHotKey`. It needs no Accessibility or
/// Input Monitoring permission and does not see any other keystrokes.
@MainActor
public final class HotKeyCenter {
    public enum RegistrationError: Error, Equatable, Sendable {
        case notAllowed
        case alreadyTaken
        case system(OSStatus)
    }

    /// Called on the main actor each time the registered combination is pressed.
    public var onPress: (() -> Void)?
    public private(set) var registered: KeyCombo?

    /// 'AIRp': tells our hotkey events apart from others on the shared application event target.
    nonisolated static let signature: OSType = 0x4149_5270
    /// Changes with every registration, so a press queued for a replaced hotkey is ignored.
    private(set) var currentID: UInt32 = 0
    private var hotKeyRef: EventHotKeyRef?
    private var handlerRef: EventHandlerRef?

    public init() {}

    isolated deinit {
        unregister()
        if let handlerRef { RemoveEventHandler(handlerRef) }
    }

    /// Replaces any previous registration. On failure nothing stays registered.
    @discardableResult
    public func register(_ combo: KeyCombo) -> Result<Void, RegistrationError> {
        unregister()
        guard combo.isAllowedAsGlobalShortcut else { return .failure(.notAllowed) }
        let installed = installHandlerIfNeeded()
        guard installed == noErr else { return .failure(.system(installed)) }
        currentID &+= 1
        var ref: EventHotKeyRef?
        // Exclusive: if another app already registered this combination we hear about it,
        // instead of both apps reacting to every press.
        let status = RegisterEventHotKey(
            combo.keyCode, combo.modifiers, EventHotKeyID(signature: Self.signature, id: currentID),
            GetApplicationEventTarget(), OptionBits(kEventHotKeyExclusive), &ref
        )
        if status == eventHotKeyExistsErr { return .failure(.alreadyTaken) }
        guard status == noErr, let ref else { return .failure(.system(status)) }
        hotKeyRef = ref
        registered = combo
        return .success(())
    }

    public func unregister() {
        if let hotKeyRef { UnregisterEventHotKey(hotKeyRef) }
        hotKeyRef = nil
        registered = nil
    }

    /// Returns whether the press belonged to the current registration.
    func handlePress(id: UInt32) -> Bool {
        guard registered != nil, id == currentID else { return false }
        onPress?()
        return true
    }

    /// One handler for the object's life; `deinit` removes it, so `self` can be passed unretained.
    private func installHandlerIfNeeded() -> OSStatus {
        guard handlerRef == nil else { return OSStatus(noErr) }
        var eventType = EventTypeSpec(eventClass: OSType(kEventClassKeyboard), eventKind: UInt32(kEventHotKeyPressed))
        return InstallEventHandler(
            GetApplicationEventTarget(), hotKeyPressed, 1, &eventType,
            Unmanaged.passUnretained(self).toOpaque(), &handlerRef
        )
    }
}

/// Carbon delivers hotkey events through the application event target on the main thread.
private func hotKeyPressed(_ nextHandler: EventHandlerCallRef?, _ event: EventRef?, _ userData: UnsafeMutableRawPointer?) -> OSStatus {
    guard let event, let userData else { return OSStatus(eventNotHandledErr) }
    var hotKeyID = EventHotKeyID()
    let status = GetEventParameter(
        event, EventParamName(kEventParamDirectObject), EventParamType(typeEventHotKeyID),
        nil, MemoryLayout<EventHotKeyID>.size, nil, &hotKeyID
    )
    guard status == noErr, hotKeyID.signature == HotKeyCenter.signature else { return OSStatus(eventNotHandledErr) }
    let id = hotKeyID.id
    let center = Unmanaged<HotKeyCenter>.fromOpaque(userData).takeUnretainedValue()
    let handled = MainActor.assumeIsolated { center.handlePress(id: id) }
    return handled ? OSStatus(noErr) : OSStatus(eventNotHandledErr)
}
