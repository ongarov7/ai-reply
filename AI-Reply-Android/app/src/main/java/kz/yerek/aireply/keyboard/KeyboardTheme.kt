package kz.yerek.aireply.keyboard

import androidx.compose.ui.graphics.Color

/**
 * Colour palette for the keyboard.
 *
 * Every value is the exact RGB the iOS `KeyboardTheme` uses, so the two
 * keyboards are the same object in two places rather than two designs that
 * happen to share a name. No platform asset is used on either side.
 *
 * HOW DARK IS DECIDED. On iOS the host app declares what it wants through
 * `UITextDocumentProxy.keyboardAppearance`, and Telegram, WhatsApp and
 * Instagram all set it. Android has no equivalent: an `EditorInfo` carries no
 * appearance hint, and an IME window is not inside the host's theme. So the
 * honest signals here are the system's own dark mode and the user's explicit
 * override in AI Reply's settings — which is what [resolve] uses, in that
 * order of specificity.
 */
data class KeyboardTheme(val isDark: Boolean) {

    // Surfaces

    val background: Color
        get() = if (isDark) Color(0xFF1B1B1D) else Color(0xFFD1D4DA)

    /** Letter and number keys: slightly lighter than the backdrop. */
    val letterKey: Color
        get() = if (isDark) Color(0xFF4A4A4E) else Color(0xFFFFFFFF)

    /** Shift, delete, plane switch, globe, language, return. */
    val specialKey: Color
        get() = if (isDark) Color(0xFF2C2C30) else Color(0xFFACB0BA)

    /**
     * Native behaviour: pressing a letter key darkens it to the special shade,
     * pressing a special key lightens it to the letter shade.
     */
    val letterKeyPressed: Color get() = specialKey
    val specialKeyPressed: Color get() = letterKey

    /** Shift / caps-lock in the engaged state. */
    val engagedKey: Color
        get() = if (isDark) Color(0xFFEFEFF2) else Color(0xFFFFFFFF)

    val engagedKeyGlyph: Color get() = Color(0xFF141416)

    // Text

    val primaryText: Color
        get() = if (isDark) Color(0xFFFFFFFF) else Color(0xFF000000)

    val secondaryText: Color
        get() = if (isDark) Color(0xFFEBEBEB).copy(alpha = 0.62f) else Color(0xFF333333).copy(alpha = 0.60f)

    val accent: Color
        get() = if (isDark) Color(0xFF0A84FF) else Color(0xFF007AFF)

    // Reply panel

    val panelBackground: Color
        get() = if (isDark) Color(0xFF2C2C30) else Color(0xFFBEC2CB)
    val panelField: Color get() = letterKey
    val fieldBackground: Color
        get() = if (isDark) Color(0xFF3A3A3E) else Color(0xFFFFFFFF)
    val fieldBorder: Color
        get() = if (isDark) Color.White.copy(alpha = 0.10f) else Color.Black.copy(alpha = 0.08f)
    val quoteBackground: Color
        get() = if (isDark) Color(0xFF38383C) else Color(0xFFE4E6EB)
    val destructive: Color
        get() = if (isDark) Color(0xFFFF6961) else Color(0xFFD70015)
    val divider: Color get() = secondaryText.copy(alpha = 0.22f)

    val keyShadow: Color
        get() = if (isDark) Color.Black.copy(alpha = 0.55f) else Color.Black.copy(alpha = 0.32f)

    companion object {
        /**
         * @param systemIsDark the device's own dark-mode state.
         * @param override the user's explicit appearance choice, or null to
         *   follow the system.
         */
        fun resolve(systemIsDark: Boolean, override: Boolean?): KeyboardTheme =
            KeyboardTheme(isDark = override ?: systemIsDark)
    }
}
