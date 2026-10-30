import AppKit
import AirReplyCore

/// The menu bar icon and its menu, rebuilt each time it opens so the account
/// and balance lines are current.
@MainActor
final class StatusItemController: NSObject, NSMenuDelegate {
    struct Snapshot {
        var shortcut: String
        var email: String?
        var signedIn: Bool
        var balance: BalanceSummary?
        var voiceAvailable: Bool
    }

    struct Actions {
        var open: () -> Void
        var dictate: () -> Void
        var signIn: () -> Void
        var plans: () -> Void
        var settings: () -> Void
        var checkForUpdates: () -> Void
        var quit: () -> Void
    }

    private let item = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
    private let snapshot: () -> Snapshot
    private let actions: Actions

    init(snapshot: @escaping () -> Snapshot, actions: Actions) {
        self.snapshot = snapshot
        self.actions = actions
        super.init()
        if let button = item.button {
            button.image = Self.logoGlyph
            button.toolTip = "AI Reply"
        }
        let menu = NSMenu()
        menu.delegate = self
        item.menu = menu
    }

    func menuNeedsUpdate(_ menu: NSMenu) {
        let state = snapshot()
        menu.removeAllItems()

        let open = entry(String(localized: "menu.open"), #selector(openPanel))
        open.toolTip = state.shortcut
        open.attributedTitle = NSAttributedString(string: String(localized: "menu.open") + "    " + state.shortcut)
        menu.addItem(open)
        if state.voiceAvailable && state.signedIn {
            menu.addItem(entry(String(localized: "menu.dictate"), #selector(dictate)))
        }
        menu.addItem(.separator())

        if state.signedIn {
            menu.addItem(disabled(state.email ?? String(localized: "menu.account")))
            if let balance = state.balance {
                menu.addItem(disabled(Self.balanceLine(balance)))
            }
        } else {
            menu.addItem(entry(String(localized: "menu.sign_in"), #selector(signIn)))
        }
        menu.addItem(entry(String(localized: "menu.plans"), #selector(plans)))
        menu.addItem(.separator())
        let settings = entry(String(localized: "menu.settings"), #selector(openSettings))
        settings.keyEquivalent = ","
        menu.addItem(settings)
        menu.addItem(entry(String(localized: "menu.updates"), #selector(checkForUpdates)))
        menu.addItem(.separator())
        let quit = entry(String(localized: "menu.quit"), #selector(quit))
        quit.keyEquivalent = "q"
        menu.addItem(quit)
    }

    static let logoGlyph = BrandGlyph.image

    static func balanceLine(_ balance: BalanceSummary) -> String {
        let plan = balance.planName.isEmpty ? "" : balance.planName + " · "
        if balance.isUnlimited { return plan + String(localized: "menu.balance.unlimited") }
        return plan + String(localized: "menu.balance.remaining \(balance.remainingTotal)")
    }

    private func entry(_ title: String, _ action: Selector) -> NSMenuItem {
        let item = NSMenuItem(title: title, action: action, keyEquivalent: "")
        item.target = self
        return item
    }

    private func disabled(_ title: String) -> NSMenuItem {
        let item = NSMenuItem(title: title, action: nil, keyEquivalent: "")
        item.isEnabled = false
        return item
    }

    @objc private func openPanel() { actions.open() }
    @objc private func dictate() { actions.dictate() }
    @objc private func signIn() { actions.signIn() }
    @objc private func plans() { actions.plans() }
    @objc private func openSettings() { actions.settings() }
    @objc private func checkForUpdates() { actions.checkForUpdates() }
    @objc private func quit() { actions.quit() }
}
