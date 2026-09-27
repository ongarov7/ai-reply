import UIKit

// MARK: - Theme

/// Colour palette for the keyboard.
///
/// The keyboard is dark-first, but it resolves correctly for light hosts too.
/// The host application expresses the appearance it wants through
/// `UITextDocumentProxy.keyboardAppearance`. Telegram, WhatsApp and Instagram
/// all set `.dark` while they are in dark mode, so that value is the primary
/// signal. When the host stays on `.default` we fall back to the trait
/// collection.
///
/// No Apple assets are used. Every value is an approximation authored here.
struct KeyboardTheme: Equatable {
    let isDark: Bool

    static func resolve(appearance: UIKeyboardAppearance, traits: UITraitCollection) -> KeyboardTheme {
        switch appearance {
        case .dark:
            return KeyboardTheme(isDark: true)
        case .light:
            return KeyboardTheme(isDark: false)
        default:
            return KeyboardTheme(isDark: traits.userInterfaceStyle == .dark)
        }
    }

    // Surfaces

    var background: UIColor {
        isDark ? .reply(0.106, 0.106, 0.114) : .reply(0.820, 0.831, 0.855)
    }

    /// Letter / number keys: slightly lighter than the backdrop.
    var letterKey: UIColor {
        isDark ? .reply(0.290, 0.290, 0.306) : .reply(1.000, 1.000, 1.000)
    }

    /// Shift, delete, plane switch, globe, language, return.
    var specialKey: UIColor {
        isDark ? .reply(0.173, 0.173, 0.188) : .reply(0.675, 0.690, 0.729)
    }

    /// Native behaviour: pressing a letter key darkens it to the special shade,
    /// pressing a special key lightens it to the letter shade.
    var letterKeyPressed: UIColor { specialKey }
    var specialKeyPressed: UIColor { letterKey }

    /// Shift / caps-lock in the engaged state.
    var engagedKey: UIColor {
        isDark ? .reply(0.937, 0.937, 0.949) : .reply(1.000, 1.000, 1.000)
    }

    var engagedKeyGlyph: UIColor { .reply(0.078, 0.078, 0.086) }

    // Text

    var primaryText: UIColor {
        isDark ? .reply(1.000, 1.000, 1.000) : .reply(0.000, 0.000, 0.000)
    }

    var secondaryText: UIColor {
        isDark ? UIColor(white: 0.92, alpha: 0.62) : UIColor(white: 0.20, alpha: 0.60)
    }

    var accent: UIColor {
        isDark ? .reply(0.039, 0.518, 1.000) : .reply(0.000, 0.478, 1.000)
    }

    // Composer surfaces
    //
    // Three surfaces, three jobs, and they must never be confused for each
    // other: the QUOTED source is recessed and low contrast, an EDITABLE FIELD
    // is raised and reads like a key, and the panel sits between them.

    /// Raised surface for a field the user types into.
    var fieldBackground: UIColor { letterKey }

    /// Recessed surface for the quoted source message.
    var quoteBackground: UIColor {
        isDark ? UIColor(white: 0.0, alpha: 0.22) : UIColor(white: 1.0, alpha: 0.42)
    }

    /// Quoted text: readable, but deliberately quieter than anything the user
    /// is about to send.
    var quoteText: UIColor {
        isDark ? UIColor(white: 1.0, alpha: 0.80) : UIColor(white: 0.0, alpha: 0.72)
    }

    var fieldBorder: UIColor {
        isDark ? UIColor(white: 1.0, alpha: 0.10) : UIColor(white: 0.0, alpha: 0.10)
    }

    /// Focus ring on the field the keys are currently editing.
    var fieldBorderFocused: UIColor { accent.withAlphaComponent(0.9) }

    /// Errors and the over-limit character counter.
    var destructive: UIColor {
        isDark ? .reply(1.000, 0.412, 0.380) : .reply(0.804, 0.153, 0.129)
    }

    // Action bar

    var actionBarKey: UIColor { specialKey }
    var actionBarKeyPressed: UIColor { letterKey }
    var panelBackground: UIColor { specialKey }

    var keyShadow: UIColor {
        isDark ? UIColor.black.withAlphaComponent(0.55) : UIColor.black.withAlphaComponent(0.32)
    }
}

private extension UIColor {
    static func reply(_ r: CGFloat, _ g: CGFloat, _ b: CGFloat) -> UIColor {
        UIColor(red: r, green: g, blue: b, alpha: 1.0)
    }
}
