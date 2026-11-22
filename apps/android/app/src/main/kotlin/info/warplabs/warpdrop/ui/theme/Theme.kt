package info.warplabs.warpdrop.ui.theme

import android.os.Build
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.ExperimentalMaterial3ExpressiveApi
import androidx.compose.material3.MaterialExpressiveTheme
import androidx.compose.material3.MotionScheme
import androidx.compose.material3.dynamicDarkColorScheme
import androidx.compose.material3.dynamicLightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import info.warplabs.warpdrop.core.ThemeMode

/** Whether [mode] draws dark: AMOLED is always dark, System follows the OS. */
@Composable
@ReadOnlyComposable
fun ThemeMode.isDark() = when (this) {
    ThemeMode.Light -> false
    ThemeMode.Dark, ThemeMode.Amoled -> true
    ThemeMode.System -> isSystemInDarkTheme()
}

/**
 * Material 3 Expressive theme. With [dynamicColor] on Android 12+ the scheme comes from the wallpaper
 * (Material You); otherwise the teal fallback. AMOLED keeps the scheme's accents and blacks out the surfaces.
 */
@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
fun WarpTheme(mode: ThemeMode, dynamicColor: Boolean, content: @Composable () -> Unit) {
    val dark = mode.isDark()
    val context = LocalContext.current
    val colors = when {
        dynamicColor && Build.VERSION.SDK_INT >= Build.VERSION_CODES.S ->
            if (dark) dynamicDarkColorScheme(context) else dynamicLightColorScheme(context)
        dark -> DarkColors
        else -> LightColors
    }
    val scheme = if (mode == ThemeMode.Amoled) colors.copy(
        background = Color.Black, surface = Color.Black, surfaceDim = Color.Black, surfaceContainerLowest = Color.Black,
        surfaceContainerLow = Color(0xFF080808), surfaceContainer = Color(0xFF121212),
        surfaceContainerHigh = Color(0xFF1A1A1A), surfaceContainerHighest = Color(0xFF242424),
    ) else colors
    MaterialExpressiveTheme(colorScheme = scheme, motionScheme = MotionScheme.expressive(), typography = WarpTypography, content = content)
}
