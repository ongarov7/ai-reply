package kz.yerek.aireply.ui.design

import androidx.compose.ui.graphics.Color

/**
 * The palette, the iOS app's system colours written out.
 *
 * iOS spends most of its colour budget on semantic system colours
 * (`systemGroupedBackground`, `secondarySystemGroupedBackground`, `separator`),
 * which is why its design system file is so small. Those values are stable and
 * published, so they are written out here rather than approximated with
 * Material's generated tonal palettes — which would produce a different app
 * that happened to use the same accent.
 *
 * The accent is the system blue the iOS app uses (light #007AFF, dark
 * #0A84FF), and dynamic colour is deliberately off: the product has an
 * identity, and inheriting the user's wallpaper would erase the one thing that
 * makes the two platforms recognisably the same app. Red is kept for real
 * errors; a gradient appears only in the app mark.
 */
object AppColors {

    // Brand ------------------------------------------------------------------

    /** systemBlue, light appearance. */
    val AccentLight = Color(0xFF007AFF)

    /** systemBlue, dark appearance. */
    val AccentDark = Color(0xFF0A84FF)

    /**
     * A quiet blue fill - a selected card, a suggested reply - solid rather
     * than the accent at low alpha, so it reads the same on the page and on a
     * card.
     */
    val AccentFillLight = Color(0xFFE5F1FF)
    val AccentFillDark = Color(0xFF15365A)

    /** The app mark's gradient, top and bottom. */
    val MarkGradientTop = Color(0xFF5B4BE8)
    val MarkGradientBottom = Color(0xFF1F6FEB)

    // Light ------------------------------------------------------------------

    /** systemGroupedBackground */
    val BackgroundLight = Color(0xFFF2F2F7)

    /** secondarySystemGroupedBackground — the raised card surface */
    val SurfaceLight = Color(0xFFFFFFFF)

    /** tertiarySystemGroupedBackground: a dialog or a sheet over the page. */
    val RaisedLight = Color(0xFFFFFFFF)

    val SeparatorLight = Color(0xFFC6C6C8)
    val TextPrimaryLight = Color(0xFF000000)
    val TextSecondaryLight = Color(0xFF8A8A8E)
    val TextTertiaryLight = Color(0xFFC0C0C4)

    // Dark -------------------------------------------------------------------

    val BackgroundDark = Color(0xFF000000)
    val SurfaceDark = Color(0xFF1C1C1E)
    val RaisedDark = Color(0xFF2C2C2E)
    val SeparatorDark = Color(0xFF38383A)
    val TextPrimaryDark = Color(0xFFFFFFFF)
    val TextSecondaryDark = Color(0xFF98989F)
    val TextTertiaryDark = Color(0xFF636366)

    // Status -----------------------------------------------------------------

    val Success = Color(0xFF34C759)
    val Warning = Color(0xFFFF9500)
    val DangerLight = Color(0xFFFF3B30)
    val DangerDark = Color(0xFFFF453A)
}
