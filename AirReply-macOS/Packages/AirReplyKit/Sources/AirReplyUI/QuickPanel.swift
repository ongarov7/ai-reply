import AppKit
import Carbon.HIToolbox
import SwiftUI

/// A floating, non-activating panel: it takes keyboard focus while the app the
/// user was in stays active, so ⌘V after closing lands back in that app.
final class QuickPanel: NSPanel {
    var onEscape: (() -> Void)?

    init() {
        super.init(contentRect: NSRect(x: 0, y: 0, width: BarMetrics.width + 2 * BarMetrics.shadowInset, height: 100),
                   styleMask: [.nonactivatingPanel, .borderless, .fullSizeContentView],
                   backing: .buffered, defer: true)
        isFloatingPanel = true
        level = .floating
        collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .ignoresCycle]
        hidesOnDeactivate = false
        isMovableByWindowBackground = true
        isOpaque = false
        backgroundColor = .clear
        // The cards draw their own soft shadow inside the transparent margin.
        hasShadow = false
        animationBehavior = .utilityWindow
        isReleasedWhenClosed = false
        titleVisibility = .hidden
        titlebarAppearsTransparent = true
    }

    override var canBecomeKey: Bool { true }
    override var canBecomeMain: Bool { false }

    override func cancelOperation(_ sender: Any?) {
        onEscape?()
    }

    /// An agent app has no visible main menu, so the editing shortcuts are routed
    /// here by key code (they must also work with a Russian or Kazakh layout).
    override func performKeyEquivalent(with event: NSEvent) -> Bool {
        let flags = event.modifierFlags.intersection(.deviceIndependentFlagsMask)
        let action: Selector? = switch (Int(event.keyCode), flags) {
        case (kVK_ANSI_X, .command): #selector(NSText.cut(_:))
        case (kVK_ANSI_C, .command): #selector(NSText.copy(_:))
        case (kVK_ANSI_V, .command): #selector(NSText.paste(_:))
        case (kVK_ANSI_A, .command): #selector(NSText.selectAll(_:))
        case (kVK_ANSI_Z, .command): Selector(("undo:"))
        case (kVK_ANSI_Z, [.command, .shift]): Selector(("redo:"))
        default: nil
        }
        if let action, NSApp.sendAction(action, to: nil, from: self) {
            return true
        }
        return super.performKeyEquivalent(with: event)
    }
}

/// Shows, places and sizes the quick panel.
@MainActor
final class QuickPanelController: NSObject, NSWindowDelegate {
    private let panel = QuickPanel()
    private let settings: AppSettings
    private var contentHeight: CGFloat = 100
    private var anchoredToBottom = false
    private var isPlacing = false

    /// Asked before hiding on focus loss: true while the microphone or a system prompt needs the panel.
    var holdsOpen: () -> Bool = { false }
    var willShow: (_ growsUpward: Bool) -> Void = { _ in }
    var didHide: () -> Void = {}
    var onEscape: () -> Void = {}

    init(settings: AppSettings) {
        self.settings = settings
        super.init()
        panel.delegate = self
        panel.onEscape = { [weak self] in self?.onEscape() }
    }

    func setContent<Content: View>(_ view: Content) {
        let hosting = NSHostingView(rootView: view)
        hosting.sizingOptions = []
        panel.contentView = hosting
    }

    var isVisible: Bool { panel.isVisible }

    func toggle() {
        isVisible ? hide() : show()
    }

    func show() {
        let screen = Self.screenUnderPointer()
        let visible = screen.visibleFrame
        let width = BarMetrics.width + 2 * BarMetrics.shadowInset
        let inset = BarMetrics.shadowInset
        var origin: CGPoint
        switch settings.placement {
        case .bottom:
            anchoredToBottom = true
            origin = CGPoint(x: visible.midX - width / 2, y: visible.minY + 24 - inset)
        case .top:
            anchoredToBottom = false
            origin = CGPoint(x: visible.midX - width / 2, y: visible.maxY - visible.height * 0.22 - contentHeight + inset)
        case .remembered:
            let relative = settings.rememberedPosition ?? CGPoint(x: 0.5, y: 0.78)
            let x = visible.minX + relative.x * visible.width - width / 2
            let top = visible.minY + relative.y * visible.height
            anchoredToBottom = relative.y < 0.5
            origin = anchoredToBottom ? CGPoint(x: x, y: top - inset) : CGPoint(x: x, y: top - contentHeight + inset)
        }
        willShow(anchoredToBottom)
        origin.x = min(max(origin.x, visible.minX - inset), visible.maxX - width + inset)
        place(NSRect(origin: origin, size: CGSize(width: width, height: contentHeight)), on: visible)
        panel.orderFrontRegardless()
        panel.makeKey()
    }

    func hide() {
        guard panel.isVisible else { return }
        panel.orderOut(nil)
        didHide()
    }

    /// The SwiftUI content changed height: grow away from the anchored edge.
    func contentHeightChanged(_ height: CGFloat) {
        guard height > 0, abs(height - contentHeight) > 0.5 else { return }
        contentHeight = height
        guard panel.isVisible || panel.frame.height != height else { return }
        var frame = panel.frame
        if !anchoredToBottom { frame.origin.y += frame.height - height }
        frame.size.height = height
        let visible = (panel.screen ?? Self.screenUnderPointer()).visibleFrame
        place(frame, on: visible)
    }

    private func place(_ frame: NSRect, on visible: NSRect) {
        var frame = frame
        let inset = BarMetrics.shadowInset
        if frame.maxY > visible.maxY + inset { frame.origin.y = visible.maxY + inset - frame.height }
        if frame.minY < visible.minY - inset { frame.origin.y = visible.minY - inset }
        isPlacing = true
        panel.setFrame(frame, display: true, animate: false)
        isPlacing = false
    }

    private static func screenUnderPointer() -> NSScreen {
        let pointer = NSEvent.mouseLocation
        return NSScreen.screens.first { NSMouseInRect(pointer, $0.frame, false) } ?? NSScreen.main ?? NSScreen.screens[0]
    }

    // MARK: NSWindowDelegate

    func windowDidResignKey(_ notification: Notification) {
        guard !holdsOpen() else { return }
        hide()
    }

    /// A drag by the user: remember where the bar now lives.
    func windowDidMove(_ notification: Notification) {
        guard !isPlacing, panel.isVisible, let screen = panel.screen else { return }
        let visible = screen.visibleFrame
        let inset = BarMetrics.shadowInset
        let anchorY = anchoredToBottom ? panel.frame.minY + inset : panel.frame.maxY - inset
        settings.rememberedPosition = CGPoint(x: (panel.frame.midX - visible.minX) / visible.width,
                                              y: (anchorY - visible.minY) / visible.height)
        settings.placement = .remembered
    }
}
