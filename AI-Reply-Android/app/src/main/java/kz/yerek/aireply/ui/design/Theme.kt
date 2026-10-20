package kz.yerek.aireply.ui.design

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color
import kz.yerek.aireply.data.settings.AppearancePreference

/**
 * Colours the app uses that Material's scheme has no slot for, kept beside it
 * rather than squeezed into `surfaceTint` and friends where the next reader
 * would have to guess what they meant.
 */
data class AppExtraColors(
    val separator: Color,
    val textSecondary: Color,
    val textTertiary: Color,
    val success: Color,
    val warning: Color
)

val LocalExtraColors = staticCompositionLocalOf {
    AppExtraColors(
        separator = AppColors.SeparatorLight,
        textSecondary = AppColors.TextSecondaryLight,
        textTertiary = AppColors.TextTertiaryLight,
        success = AppColors.Success,
        warning = AppColors.Warning
    )
}

/*
 * Every role is set, not only the ones the screens name: Material's
 * components reach for secondaryContainer (segmented buttons), the
 * surfaceContainer family (dialogs, sheets, menus, a scrolled top bar) and
 * surfaceTint, and their defaults are a lilac palette that has nothing to do
 * with this app. Here they are the same blue and the same greys.
 */
private val LightScheme = lightColorScheme(
    primary = AppColors.AccentLight,
    onPrimary = Color.White,
    primaryContainer = AppColors.AccentFillLight,
    onPrimaryContainer = AppColors.TextPrimaryLight,
    inversePrimary = AppColors.AccentDark,
    secondary = AppColors.AccentLight,
    onSecondary = Color.White,
    secondaryContainer = AppColors.AccentFillLight,
    onSecondaryContainer = AppColors.TextPrimaryLight,
    tertiary = AppColors.AccentLight,
    onTertiary = Color.White,
    tertiaryContainer = AppColors.AccentFillLight,
    onTertiaryContainer = AppColors.TextPrimaryLight,
    background = AppColors.BackgroundLight,
    onBackground = AppColors.TextPrimaryLight,
    surface = AppColors.SurfaceLight,
    onSurface = AppColors.TextPrimaryLight,
    surfaceVariant = AppColors.BackgroundLight,
    onSurfaceVariant = AppColors.TextSecondaryLight,
    surfaceTint = Color.Transparent,
    inverseSurface = AppColors.SurfaceDark,
    inverseOnSurface = AppColors.TextPrimaryDark,
    outline = AppColors.SeparatorLight,
    outlineVariant = AppColors.SeparatorLight,
    error = AppColors.DangerLight,
    onError = Color.White,
    errorContainer = AppColors.DangerLight.copy(alpha = 0.12f),
    onErrorContainer = AppColors.DangerLight,
    scrim = Color.Black.copy(alpha = 0.32f),
    surfaceBright = AppColors.SurfaceLight,
    surfaceDim = AppColors.BackgroundLight,
    surfaceContainerLowest = AppColors.SurfaceLight,
    surfaceContainerLow = AppColors.BackgroundLight,
    surfaceContainer = AppColors.BackgroundLight,
    surfaceContainerHigh = AppColors.RaisedLight,
    surfaceContainerHighest = AppColors.RaisedLight
)

private val DarkScheme = darkColorScheme(
    primary = AppColors.AccentDark,
    onPrimary = Color.White,
    primaryContainer = AppColors.AccentFillDark,
    onPrimaryContainer = AppColors.TextPrimaryDark,
    inversePrimary = AppColors.AccentLight,
    secondary = AppColors.AccentDark,
    onSecondary = Color.White,
    secondaryContainer = AppColors.AccentFillDark,
    onSecondaryContainer = AppColors.TextPrimaryDark,
    tertiary = AppColors.AccentDark,
    onTertiary = Color.White,
    tertiaryContainer = AppColors.AccentFillDark,
    onTertiaryContainer = AppColors.TextPrimaryDark,
    background = AppColors.BackgroundDark,
    onBackground = AppColors.TextPrimaryDark,
    surface = AppColors.SurfaceDark,
    onSurface = AppColors.TextPrimaryDark,
    surfaceVariant = AppColors.SurfaceDark,
    onSurfaceVariant = AppColors.TextSecondaryDark,
    surfaceTint = Color.Transparent,
    inverseSurface = AppColors.SurfaceLight,
    inverseOnSurface = AppColors.TextPrimaryLight,
    outline = AppColors.SeparatorDark,
    outlineVariant = AppColors.SeparatorDark,
    error = AppColors.DangerDark,
    onError = Color.White,
    errorContainer = AppColors.DangerDark.copy(alpha = 0.18f),
    onErrorContainer = AppColors.DangerDark,
    scrim = Color.Black.copy(alpha = 0.5f),
    surfaceBright = AppColors.RaisedDark,
    surfaceDim = AppColors.BackgroundDark,
    surfaceContainerLowest = AppColors.BackgroundDark,
    surfaceContainerLow = AppColors.SurfaceDark,
    surfaceContainer = AppColors.BackgroundDark,
    surfaceContainerHigh = AppColors.RaisedDark,
    surfaceContainerHighest = AppColors.RaisedDark
)

/**
 * @param appearance the user's explicit choice. [AppearancePreference.SYSTEM]
 *   hands the decision back to the device, which is what `nil` does in the iOS
 *   `colorScheme` property.
 */
@Composable
fun AIReplyTheme(
    appearance: AppearancePreference = AppearancePreference.SYSTEM,
    content: @Composable () -> Unit
) {
    val dark = when (appearance) {
        AppearancePreference.SYSTEM -> isSystemInDarkTheme()
        AppearancePreference.LIGHT -> false
        AppearancePreference.DARK -> true
    }

    val extras = if (dark) {
        AppExtraColors(
            separator = AppColors.SeparatorDark,
            textSecondary = AppColors.TextSecondaryDark,
            textTertiary = AppColors.TextTertiaryDark,
            success = AppColors.Success,
            warning = AppColors.Warning
        )
    } else {
        AppExtraColors(
            separator = AppColors.SeparatorLight,
            textSecondary = AppColors.TextSecondaryLight,
            textTertiary = AppColors.TextTertiaryLight,
            success = AppColors.Success,
            warning = AppColors.Warning
        )
    }

    CompositionLocalProvider(LocalExtraColors provides extras) {
        MaterialTheme(
            colorScheme = if (dark) DarkScheme else LightScheme,
            typography = AppTypography,
            content = content
        )
    }
}
