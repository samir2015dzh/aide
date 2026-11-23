package io.aide.android.ui

import androidx.compose.animation.core.CubicBezierEasing
import androidx.compose.animation.core.SpringSpec
import androidx.compose.animation.core.spring
import androidx.compose.foundation.LocalIndication
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.LocalRippleConfiguration
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

// The app's look is Apple's (工单 #68, #95): iOS's system colours, grouped backgrounds, label steps, hairlines and type
// ramp, drawn here with Compose (no SwiftUI, nothing translated). The desktop app takes the same values at macOS density
// (apps/desktop/src/renderer/src/styles/app.css), so the two read as one app.

/** The states' colours: iOS's system green, orange and blue, light or dark as the theme is (工单 #95). */
val Positive: Color @Composable @ReadOnlyComposable get() = LocalAideTokens.current.green
val Warning: Color @Composable @ReadOnlyComposable get() = LocalAideTokens.current.orange
val BrandCyan: Color @Composable @ReadOnlyComposable get() = LocalAideTokens.current.blue

/**
 * The surfaces, lines and fills of an iOS grouped screen (HIG Color, "Background colors", "Fill colors"): the grouped
 * background behind, the inset card a step above it, a third step inside a card; the label steps; the hairline
 * between rows; and what a pressed row, a bar or a menu is drawn with.
 */
@Immutable
data class AideTokens(
    /** The owner's message: a grey bubble a step above the page, never the accent (in dark mode it was white). */
    val bubble: Color,
    val onBubble: Color,
    /** A hairline between rows (separator). */
    val separator: Color,
    /** Fills under a control: quaternary (a quiet row) and tertiary (a chip, a segmented control's track, a field). */
    val fill: Color,
    val fill2: Color,
    /** An inset grouped card (secondarySystemGroupedBackground). */
    val card: Color,
    /** The edge of something floating (a menu, a toast): a hairline where its shadow is. */
    val cardEdge: Color,
    /** A block inside a card, the third step (tertiarySystemGroupedBackground). */
    val inset: Color,
    /** The third label step (tertiaryLabel): times, counts, chevrons. */
    val faint: Color,
    /** The page behind the cards (systemGroupedBackground). */
    val grouped: Color,
    /** A row under the finger, laid over it (the cell's highlight). */
    val pressed: Color,
    /** A bar's material (tab bar, navigation bar): the page shows faintly through. */
    val bar: Color,
    /** A menu's, an alert's and a toast's material. */
    val menu: Color,
    /** A switch's track while off. */
    val switchOff: Color,
    /** A segmented control's picked segment. */
    val segment: Color,
    val blue: Color,
    val green: Color,
    val red: Color,
    val orange: Color,
    /** An unpicked tab's grey (systemGray). */
    val gray: Color,
)

private val DarkTokens = AideTokens(
    bubble = Color(0xFF2C2C2E), onBubble = Color.White,
    separator = Color(0xA6545458), fill = Color(0x2E767680), fill2 = Color(0x3D767680),
    card = Color(0xFF1C1C1E), cardEdge = Color(0x1FFFFFFF), inset = Color(0xFF2C2C2E), faint = Color(0x4DEBEBF5),
    grouped = Color(0xFF000000), pressed = Color(0x24FFFFFF), bar = Color(0xF0161618), menu = Color(0xFF2A2A2C),
    switchOff = Color(0xFF39393D), segment = Color(0xFF636366),
    blue = Color(0xFF0A84FF), green = Color(0xFF30D158), red = Color(0xFFFF453A), orange = Color(0xFFFF9F0A), gray = Color(0xFF8E8E93),
)
private val LightTokens = AideTokens(
    bubble = Color(0xFFE9E9EB), onBubble = Color.Black,
    separator = Color(0x4A3C3C43), fill = Color(0x14747480), fill2 = Color(0x1F767680),
    card = Color.White, cardEdge = Color(0x14000000), inset = Color(0xFFF2F2F7), faint = Color(0x4D3C3C43),
    grouped = Color(0xFFF2F2F7), pressed = Color(0x2E000000), bar = Color(0xF0F9F9F9), menu = Color(0xFFF9F9F9),
    switchOff = Color(0xFFE9E9EA), segment = Color.White,
    blue = Color(0xFF007AFF), green = Color(0xFF34C759), red = Color(0xFFFF3B30), orange = Color(0xFFFF9500), gray = Color(0xFF8E8E93),
)

/** What a sheet or an alert is drawn on: iOS's elevated steps in dark mode (the card a step lighter), as they float. */
fun AideTokens.elevated(dark: Boolean): AideTokens = if (!dark) this else copy(grouped = Color(0xFF1C1C1E), card = Color(0xFF2C2C2E), inset = Color(0xFF3A3A3C))

val LocalAideTokens = staticCompositionLocalOf { LightTokens }

/** The app's surfaces and lines (AideTokens) where the theme is. */
val aide: AideTokens
    @Composable @ReadOnlyComposable get() = LocalAideTokens.current

/**
 * Motion (工单 #95), as SwiftUI's: the default spring (.spring(response: 0.55, dampingFraction: 0.825)) for what moves
 * into place, the snappy one for what answers a finger, and a smooth one (no overshoot) for a page pushed across. Tweens
 * keep the desktop's curve (cubic-bezier(0.32, 0.72, 0, 1)) for fades. A list scrolling moves nothing by itself (工单 #55).
 */
object Motion {
    val EaseOut = CubicBezierEasing(0.32f, 0.72f, 0f, 1f)
    const val Short = 150
    const val Medium = 250
    const val Long = 350
    const val Damping = 0.825f
    const val Stiffness = 380f
    fun <T> spring(visibilityThreshold: T? = null): SpringSpec<T> = spring(Damping, Stiffness, visibilityThreshold)
    fun <T> snappy(visibilityThreshold: T? = null): SpringSpec<T> = spring(0.85f, 700f, visibilityThreshold)
    fun <T> smooth(visibilityThreshold: T? = null): SpringSpec<T> = spring(1f, Stiffness, visibilityThreshold)
}

/** Rounding (工单 #95, iOS density): controls 8, inset grouped cards 10, alerts, menus and a sheet's top 14. */
object Radius {
    val Sm = 6.dp
    val Md = 8.dp
    val Lg = 10.dp
    val Xl = 14.dp
}

private fun scheme(dark: Boolean, k: AideTokens) = if (dark) {
    darkColorScheme(
        primary = k.blue, onPrimary = Color.White,
        primaryContainer = k.blue.copy(alpha = 0.18f), onPrimaryContainer = k.blue,
        secondary = Color(0x99EBEBF5), onSecondary = Color.Black,
        secondaryContainer = Color(0xFF2C2C2E), onSecondaryContainer = Color.White,
        tertiary = k.orange, onTertiary = Color.Black,
        tertiaryContainer = Color(0xFF2C2C2E), onTertiaryContainer = k.orange,
        background = k.grouped, onBackground = Color.White,
        surface = k.grouped, onSurface = Color.White,
        surfaceVariant = k.card, onSurfaceVariant = Color(0x99EBEBF5),
        surfaceDim = k.grouped, surfaceBright = Color(0xFF3A3A3C),
        surfaceContainerLowest = k.grouped, surfaceContainerLow = k.card,
        surfaceContainer = k.card, surfaceContainerHigh = Color(0xFF2C2C2E), surfaceContainerHighest = Color(0xFF3A3A3C),
        surfaceTint = Color.Transparent,
        inverseSurface = Color.White, inverseOnSurface = Color.Black, inversePrimary = Color(0xFF007AFF),
        outline = k.separator, outlineVariant = Color(0xFF3A3A3C),
        error = k.red, onError = Color.White,
        errorContainer = k.red.copy(alpha = 0.18f), onErrorContainer = k.red,
        scrim = Color.Black,
    )
} else {
    lightColorScheme(
        primary = k.blue, onPrimary = Color.White,
        primaryContainer = k.blue.copy(alpha = 0.12f), onPrimaryContainer = k.blue,
        secondary = Color(0x993C3C43), onSecondary = Color.White,
        secondaryContainer = Color(0xFFE5E5EA), onSecondaryContainer = Color.Black,
        tertiary = k.orange, onTertiary = Color.White,
        tertiaryContainer = Color(0xFFE5E5EA), onTertiaryContainer = k.orange,
        background = k.grouped, onBackground = Color.Black,
        surface = k.grouped, onSurface = Color.Black,
        surfaceVariant = k.card, onSurfaceVariant = Color(0x993C3C43),
        surfaceDim = k.grouped, surfaceBright = Color.White,
        surfaceContainerLowest = Color.White, surfaceContainerLow = Color.White,
        surfaceContainer = Color.White, surfaceContainerHigh = Color(0xFFF2F2F7), surfaceContainerHighest = Color(0xFFE5E5EA),
        surfaceTint = Color.Transparent,
        inverseSurface = Color(0xFF1C1C1E), inverseOnSurface = Color.White, inversePrimary = Color(0xFF0A84FF),
        outline = k.separator, outlineVariant = Color(0xFFC6C6C8),
        error = k.red, onError = Color.White,
        errorContainer = k.red.copy(alpha = 0.12f), onErrorContainer = k.red,
        scrim = Color.Black,
    )
}

/**
 * iOS's type ramp (HIG Typography, "Specifications", large/default size), on the system font (Roboto and the system's
 * Chinese face; SF Pro is licensed for Apple's platforms only):
 * Large Title 34/41 bold, Title1 28/34, Title2 22/28, Title3 20/25, Headline 17/22 semibold, Body 17/22, Callout 16/21,
 * Subheadline 15/20, Footnote 13/18, Caption 12/16. Material's slots take the steps nearest their use.
 */
object IosType {
    val LargeTitle = TextStyle(fontSize = 34.sp, lineHeight = 41.sp, fontWeight = FontWeight.Bold)
    val Title1 = TextStyle(fontSize = 28.sp, lineHeight = 34.sp, fontWeight = FontWeight.Bold)
    val Title2 = TextStyle(fontSize = 22.sp, lineHeight = 28.sp, fontWeight = FontWeight.SemiBold)
    val Title3 = TextStyle(fontSize = 20.sp, lineHeight = 25.sp, fontWeight = FontWeight.SemiBold)
    val Headline = TextStyle(fontSize = 17.sp, lineHeight = 22.sp, fontWeight = FontWeight.SemiBold)
    val Body = TextStyle(fontSize = 17.sp, lineHeight = 22.sp)
    val Callout = TextStyle(fontSize = 16.sp, lineHeight = 21.sp)
    val Subheadline = TextStyle(fontSize = 15.sp, lineHeight = 20.sp)
    val Footnote = TextStyle(fontSize = 13.sp, lineHeight = 18.sp)
    val Caption = TextStyle(fontSize = 12.sp, lineHeight = 16.sp)
    val Caption2 = TextStyle(fontSize = 11.sp, lineHeight = 13.sp)
}

private val AideType = Typography().let { d ->
    fun TextStyle.on(base: TextStyle) = base.merge(this).copy(letterSpacing = 0.sp)
    d.copy(
        displaySmall = IosType.LargeTitle.on(d.displaySmall),
        headlineLarge = IosType.LargeTitle.on(d.headlineLarge),
        headlineMedium = IosType.Title1.on(d.headlineMedium),
        headlineSmall = IosType.Title2.on(d.headlineSmall),
        titleLarge = IosType.Title3.on(d.titleLarge),
        titleMedium = IosType.Headline.on(d.titleMedium),
        titleSmall = IosType.Subheadline.copy(fontWeight = FontWeight.SemiBold).on(d.titleSmall),
        bodyLarge = IosType.Body.on(d.bodyLarge),
        bodyMedium = IosType.Subheadline.on(d.bodyMedium),
        bodySmall = IosType.Footnote.on(d.bodySmall),
        labelLarge = IosType.Subheadline.copy(fontWeight = FontWeight.Medium).on(d.labelLarge),
        labelMedium = IosType.Footnote.on(d.labelMedium),
        labelSmall = IosType.Caption.on(d.labelSmall),
    )
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AideTheme(content: @Composable () -> Unit) {
    val dark = isSystemInDarkTheme()
    val k = if (dark) DarkTokens else LightTokens
    CompositionLocalProvider(LocalAideTokens provides k) {
        MaterialTheme(colorScheme = scheme(dark, k), typography = AideType) {
            // No ripple anywhere: what is pressed is highlighted as on iOS (Highlight), Material's ripple never shows.
            CompositionLocalProvider(LocalIndication provides Highlight(k.pressed), LocalRippleConfiguration provides null, content = content)
        }
    }
}
