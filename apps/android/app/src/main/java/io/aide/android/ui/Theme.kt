package io.aide.android.ui

import androidx.compose.animation.core.CubicBezierEasing
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/** Black and white, as the desktop app (packages/ui/src/brand.css, after Grok's look), and the colours of states it uses sparingly. */
val Brand = Color(0xFF111113)
val BrandCyan = Color(0xFF0891B2)
val Positive = Color(0xFF1F9D55)
val Warning = Color(0xFFD97706)

/**
 * The surfaces and lines the desktop app draws with (apps/desktop/src/renderer/src/styles/app.css: --win,
 * --win2, --win3, --bub, --sep, --fill), so a page here reads as the same app: in dark mode every surface is
 * dark, steps of lightness apart, never a white block on a dark page (the owner's ruling, 2026-11-21).
 */
@Immutable
data class AideTokens(
    /** The owner's message, as the desktop's --bub: a step above the page, never the accent. */
    val bubble: Color,
    val onBubble: Color,
    /** A hairline between things, as --sep. */
    val separator: Color,
    /** A quiet fill under a row or a chip, as --fill and --fill2. */
    val fill: Color,
    val fill2: Color,
)

private val DarkTokens = AideTokens(
    bubble = Color(0xFF232326), onBubble = Color(0xEBFFFFFF),
    separator = Color(0x14FFFFFF), fill = Color(0x0DFFFFFF), fill2 = Color(0x16FFFFFF),
)
private val LightTokens = AideTokens(
    bubble = Color(0xFFF0F0F1), onBubble = Color(0xE6000000),
    separator = Color(0x14000000), fill = Color(0x0A000000), fill2 = Color(0x12000000),
)

val LocalAideTokens = staticCompositionLocalOf { LightTokens }

/** The app's surfaces and lines (AideTokens) where the theme is. */
val aide: AideTokens
    @Composable get() = LocalAideTokens.current

/** Motion, as the desktop's: one curve (--ease-out) and a few durations, so nothing jumps and nothing drags. */
object Motion {
    val EaseOut = CubicBezierEasing(0.2f, 0f, 0f, 1f)
    const val Short = 150
    const val Medium = 240
    const val Long = 320
}

/** Rounding, as the desktop's --r-*: small for chips, large for cards and the owner's message. */
object Radius {
    val Sm = 8.dp
    val Md = 10.dp
    val Lg = 14.dp
    val Xl = 18.dp
}

// Every surface slot set, in both modes: a slot left out falls back to Material's purple-grey, which showed
// as tinted cards and bars in dark mode.
private val Dark = darkColorScheme(
    primary = Color(0xFFF2F2F3), onPrimary = Color(0xFF0B0B0D),
    primaryContainer = Color(0xFF232326), onPrimaryContainer = Color(0xFFF2F2F3),
    secondary = Color(0xFF9B9BA3), onSecondary = Color(0xFF0B0B0D),
    secondaryContainer = Color(0xFF232326), onSecondaryContainer = Color(0xFFF2F2F3),
    tertiary = Color(0xFFF5A050), onTertiary = Color(0xFF0B0B0D),
    tertiaryContainer = Color(0xFF2A2117), onTertiaryContainer = Color(0xFFF5C08A),
    background = Color(0xFF0A0A0B), onBackground = Color(0xFFF2F2F3),
    surface = Color(0xFF0A0A0B), onSurface = Color(0xFFF2F2F3),
    surfaceVariant = Color(0xFF141416), onSurfaceVariant = Color(0xFF9B9BA3),
    surfaceDim = Color(0xFF070708), surfaceBright = Color(0xFF26262A),
    surfaceContainerLowest = Color(0xFF060607), surfaceContainerLow = Color(0xFF0F0F11),
    surfaceContainer = Color(0xFF141416), surfaceContainerHigh = Color(0xFF1A1A1D), surfaceContainerHighest = Color(0xFF202023),
    surfaceTint = Color(0xFF0A0A0B),
    inverseSurface = Color(0xFFF2F2F3), inverseOnSurface = Color(0xFF111113), inversePrimary = Color(0xFF111113),
    outline = Color(0xFF3A3A3F), outlineVariant = Color(0xFF232326),
    error = Color(0xFFFF6B6B), onError = Color(0xFF0B0B0D),
    errorContainer = Color(0xFF2E1517), onErrorContainer = Color(0xFFFFB3B3),
    scrim = Color(0xFF000000),
)

private val Light = lightColorScheme(
    primary = Brand, onPrimary = Color.White,
    primaryContainer = Color(0xFFF0F0F1), onPrimaryContainer = Brand,
    secondary = Color(0xFF5F5F66), onSecondary = Color.White,
    secondaryContainer = Color(0xFFF0F0F1), onSecondaryContainer = Brand,
    tertiary = Color(0xFFB4691C), onTertiary = Color.White,
    tertiaryContainer = Color(0xFFFBEFE2), onTertiaryContainer = Color(0xFF5C340B),
    background = Color.White, onBackground = Color(0xFF111113),
    surface = Color.White, onSurface = Color(0xFF111113),
    surfaceVariant = Color(0xFFF7F7F8), onSurfaceVariant = Color(0xFF5F5F66),
    surfaceDim = Color(0xFFEDEDEF), surfaceBright = Color.White,
    surfaceContainerLowest = Color.White, surfaceContainerLow = Color(0xFFFBFBFC),
    surfaceContainer = Color(0xFFF7F7F8), surfaceContainerHigh = Color(0xFFF2F2F4), surfaceContainerHighest = Color(0xFFEFEFF1),
    surfaceTint = Color.White,
    inverseSurface = Color(0xFF1C1C1F), inverseOnSurface = Color(0xFFF2F2F3), inversePrimary = Color(0xFFF2F2F3),
    outline = Color(0xFFD4D4D8), outlineVariant = Color(0xFFE6E6E9),
    error = Color(0xFFE04848), onError = Color.White,
    errorContainer = Color(0xFFFDECEC), onErrorContainer = Color(0xFF7A1E1E),
    scrim = Color(0xFF000000),
)

// The type steps, as the desktop's: a reading size with room between lines, then quieter steps below it.
private val AideType = Typography().let { d ->
    d.copy(
        bodyLarge = d.bodyLarge.copy(fontSize = 15.sp, lineHeight = 24.sp),
        bodyMedium = d.bodyMedium.copy(fontSize = 14.sp, lineHeight = 21.sp),
        bodySmall = d.bodySmall.copy(fontSize = 12.5.sp, lineHeight = 18.sp),
        labelSmall = d.labelSmall.copy(fontSize = 11.5.sp, lineHeight = 16.sp, letterSpacing = 0.1.sp),
        titleMedium = d.titleMedium.copy(fontSize = 16.sp, lineHeight = 22.sp),
    )
}

@Composable
fun AideTheme(content: @Composable () -> Unit) {
    val dark = isSystemInDarkTheme()
    CompositionLocalProvider(LocalAideTokens provides if (dark) DarkTokens else LightTokens) {
        MaterialTheme(colorScheme = if (dark) Dark else Light, typography = AideType, content = content)
    }
}
