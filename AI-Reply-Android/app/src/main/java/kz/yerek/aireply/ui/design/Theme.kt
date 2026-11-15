package kz.yerek.aireply.ui.design

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.LocalContentColor
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color

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

/** Fixed light appearance for every app screen and keyboard panel. */
@Composable
fun AIReplyTheme(content: @Composable () -> Unit) {
    val extras = AppExtraColors(
        separator = AppColors.SeparatorLight,
        textSecondary = AppColors.TextSecondaryLight,
        textTertiary = AppColors.TextTertiaryLight,
        success = AppColors.Success,
        warning = AppColors.Warning
    )
    CompositionLocalProvider(LocalExtraColors provides extras) {
        MaterialTheme(
            colorScheme = LightScheme,
            typography = AppTypography
        ) {
            // Text outside a Surface or Scaffold also needs the page's content colour.
            CompositionLocalProvider(LocalContentColor provides LightScheme.onBackground, content = content)
        }
    }
}
