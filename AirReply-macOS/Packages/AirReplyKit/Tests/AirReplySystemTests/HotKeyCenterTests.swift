import Carbon.HIToolbox
import Testing
@testable import AirReplySystem

/// Combinations nobody presses, so registering them while the tests run takes nothing from the user.
private let unusual = combo(kVK_F19, controlKey | optionKey | shiftKey | cmdKey)
private let otherUnusual = combo(kVK_F18, controlKey | optionKey | shiftKey | cmdKey)

private extension Result where Success == Void {
    var succeeded: Bool {
        if case .success = self { true } else { false }
    }

    var failure: Failure? {
        if case .failure(let error) = self { error } else { nil }
    }
}

/// Sends the event Carbon sends when a registered hotkey is pressed.
@MainActor
@discardableResult
private func simulatePress(id: UInt32, signature: OSType = HotKeyCenter.signature) -> OSStatus {
    var event: EventRef?
    let created = CreateEvent(
        nil, OSType(kEventClassKeyboard), UInt32(kEventHotKeyPressed), GetCurrentEventTime(),
        EventAttributes(kEventAttributeNone), &event
    )
    guard created == noErr, let event else { return created }
    defer { ReleaseEvent(event) }
    var hotKeyID = EventHotKeyID(signature: signature, id: id)
    let set = SetEventParameter(
        event, EventParamName(kEventParamDirectObject), EventParamType(typeEventHotKeyID),
        MemoryLayout<EventHotKeyID>.size, &hotKeyID
    )
    guard set == noErr else { return set }
    return SendEventToEventTarget(event, GetApplicationEventTarget())
}

@MainActor
@Suite("Global hotkey", .serialized)
struct HotKeyCenterTests {
    @Test func combosThatAreNotAllowedAreNotRegistered() {
        let center = HotKeyCenter()
        #expect(center.register(combo(kVK_ANSI_Q, cmdKey)).failure == .notAllowed)
        #expect(center.register(combo(kVK_F5)).failure == .notAllowed)
        #expect(center.register(combo(kVK_ANSI_K, shiftKey)).failure == .notAllowed)
        #expect(center.registered == nil)
    }

    @Test func failedRegistrationDropsThePreviousOne() throws {
        let center = HotKeyCenter()
        try #require(center.register(unusual).succeeded, "RegisterEventHotKey needs a login session")
        #expect(center.register(combo(kVK_Tab, cmdKey)).failure == .notAllowed)
        #expect(center.registered == nil)
        // Really unregistered: a new center can take the combination.
        let other = HotKeyCenter()
        #expect(other.register(unusual).succeeded)
        other.unregister()
    }

    @Test func registersReplacesAndUnregistersIdempotently() throws {
        let center = HotKeyCenter()
        center.unregister()
        center.unregister()
        #expect(center.registered == nil)
        try #require(center.register(unusual).succeeded, "RegisterEventHotKey needs a login session")
        #expect(center.registered == unusual)
        // The same combination again replaces it instead of reporting it taken.
        #expect(center.register(unusual).succeeded)
        #expect(center.register(otherUnusual).succeeded)
        #expect(center.registered == otherUnusual)
        center.unregister()
        center.unregister()
        #expect(center.registered == nil)
        #expect(center.register(unusual).succeeded)
        center.unregister()
    }

    @Test func pressReachesOnPressOnlyForTheCurrentRegistration() throws {
        let center = HotKeyCenter()
        var presses = 0
        center.onPress = { presses += 1 }
        try #require(center.register(unusual).succeeded, "RegisterEventHotKey needs a login session")
        let first = center.currentID
        #expect(simulatePress(id: first) == noErr)
        #expect(presses == 1)
        try #require(center.register(otherUnusual).succeeded)
        // Queued for the replaced hotkey, or another app's hotkey ('XXXX').
        simulatePress(id: first)
        simulatePress(id: center.currentID, signature: 0x5858_5858)
        #expect(presses == 1)
        simulatePress(id: center.currentID)
        #expect(presses == 2)
        center.unregister()
        simulatePress(id: center.currentID)
        #expect(presses == 2)
    }

    @Test func releasingTheCenterRemovesItsHandlerAndHotkey() throws {
        var presses = 0
        var center: HotKeyCenter? = HotKeyCenter()
        center?.onPress = { presses += 1 }
        try #require(center?.register(unusual).succeeded == true, "RegisterEventHotKey needs a login session")
        let id = try #require(center?.currentID)
        weak var released = center
        center = nil
        #expect(released == nil)
        // No handler may still point at the freed center.
        simulatePress(id: id)
        #expect(presses == 0)
        let next = HotKeyCenter()
        #expect(next.register(unusual).succeeded)
        next.unregister()
    }
}
