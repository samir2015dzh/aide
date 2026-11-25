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

// Fluent design tokens for Android (工单 #106 / #109), matching Desktop (#107):
// Accent fill #0078d4 (white text 4.53:1 in both light and dark), contrast-safe darkened text shades
// (#005a9e light / #2899f5 dark; #0e700e green text, #8a5100 caution text, #c50f1f error text),
// rounded Fluent geometry (control 10dp, card/flyout 16dp, bubble 18dp + 6dp tail, sheet/dialog 24dp),
// and Fluent decelerate motion (150 / 200 / 250ms, cubic-bezier(0.1, 0.9, 0.2, 1)).

/** Status text colours with >= 4.5:1 contrast on page and card surfaces (工单 #106). */
val Positive: Color @Composable @ReadOnlyComposable get() = LocalAideTokens.current.greenText
val Warning: Color @Composable @ReadOnlyComposable get() = LocalAideTokens.current.orangeText
val BrandCyan: Color @Composable @ReadOnlyComposable get() = LocalAideTokens.current.blue

/**
 * Fluent Design surface, border, fill and semantic tokens for Android (工单 #106).
 * Vivid status fills (green / orange / red / accent) are paired with contrast-checked text tokens
 * (greenText / orangeText / redText / blue / blueOnSubtle) so every piece of readable text meets >= 4.5:1.
 */
@Immutable
data class AideTokens(
    /** Primary Fluent accent fill (#0078d4): filled buttons, active indicators, sent bubbles. */
    val accent: Color,
    /** Pressed Fluent accent fill (#106ebe). */
    val accentPressed: Color,
    /** Sent message bubble fill (#0078d4) and white text (4.53:1 in light and dark). */
    val bubble: Color,
    val onBubble: Color,
    /** Row divider (#edebe9 light / #3b3a39 dark). */
    val separator: Color,
    /** Control border (#d1d1d1 light / #484644 dark), input bottom accent line, and toggle/check outline. */
    val controlBorder: Color,
    val controlAccentLine: Color,
    val controlStrong: Color,
    /** Neutral subtle fills for quiet pills, secondary chips, and disabled buttons. */
    val fill: Color,
    val fill2: Color,
    /** Card surface (70% acrylic composited on page: #fbfafa light / #282726 dark). */
    val card: Color,
    /** Card & floating panel hairline stroke. */
    val cardEdge: Color,
    /** Sunken block inside a card (code blocks, tool details, nested workbenches). */
    val inset: Color,
    /** Secondary metadata text colour (#605e5c light / #c8c6c4 dark, >= 5.77:1). */
    val faint: Color,
    /** Disabled text & offline/idle dots (#a19f9d light / #797775 dark). */
    val disabled: Color,
    /** Page background (#f3f2f1 light / #1b1a19 dark). */
    val grouped: Color,
    /** Pressed row/control overlay. */
    val pressed: Color,
    /** Acrylic bar surface (app bar, bottom navigation bar). */
    val bar: Color,
    /** Dialog / flyout surface (#ffffff light / #2d2c2b dark). */
    val menu: Color,
    /** Switch off track fill (transparent outlined in Fluent). */
    val switchOff: Color,
    /** Segmented control active pill. */
    val segment: Color,
    /** Link / interactive blue text (#005a9e light 6.35:1 / #2899f5 dark 5.76:1). */
    val blue: Color,
    /** Blue text on blue subtle pill (#005a9e light / #6cb8f6 dark 5.63:1). */
    val blueOnSubtle: Color,
    val accentSubtle: Color,
    /** Vivid status fills (dots, filled danger/caution badges). */
    val green: Color,
    val red: Color,
    val orange: Color,
    /** Contrast-checked status text colours (>= 4.5:1 on page, card, and subtle pill). */
    val greenText: Color,
    val redText: Color,
    val orangeText: Color,
    /** Subtle pill backgrounds. */
    val greenSubtle: Color,
    val orangeSubtle: Color,
    val redSubtle: Color,
    /** Text on #ffb900 caution badge (#201f1e, 9.55:1). */
    val onCaution: Color,
    /** Unselected tab icon & label colour (#605e5c light / #c8c6c4 dark). */
    val gray: Color,
)

private val DarkTokens = AideTokens(
    accent = Color(0xFF0078D4), accentPressed = Color(0xFF106EBE),
    bubble = Color(0xFF0078D4), onBubble = Color.White,
    separator = Color(0xFF3B3A39),
    controlBorder = Color(0xFF484644), controlAccentLine = Color(0xFFA19F9D), controlStrong = Color(0xFFC8C6C4),
    fill = Color(0x14FFFFFF), fill2 = Color(0x1AFFFFFF),
    card = Color(0xFF282726), cardEdge = Color(0x14FFFFFF), inset = Color(0xFF201F1E),
    faint = Color(0xFFC8C6C4), disabled = Color(0xFF797775),
    grouped = Color(0xFF1B1A19), pressed = Color(0x0FFFFFFF),
    bar = Color(0xE0323130), menu = Color(0xFF2D2C2B),
    switchOff = Color.Transparent, segment = Color(0xFF3B3A39),
    blue = Color(0xFF2899F5), blueOnSubtle = Color(0xFF6CB8F6), accentSubtle = Color(0x380078D4),
    green = Color(0xFF00CC6A), red = Color(0xFFE81123), orange = Color(0xFFFFB900),
    greenText = Color(0xFF00CC6A), redText = Color(0xFFF1707B), orangeText = Color(0xFFFFB900),
    greenSubtle = Color(0x2900CC6A), orangeSubtle = Color(0x29FFB900), redSubtle = Color(0x2EE81123),
    onCaution = Color(0xFF201F1E), gray = Color(0xFFC8C6C4),
)

private val LightTokens = AideTokens(
    accent = Color(0xFF0078D4), accentPressed = Color(0xFF106EBE),
    bubble = Color(0xFF0078D4), onBubble = Color.White,
    separator = Color(0xFFEDEBE9),
    controlBorder = Color(0xFFD1D1D1), controlAccentLine = Color(0xFF8A8886), controlStrong = Color(0xFF605E5C),
    fill = Color(0x0F201F1E), fill2 = Color(0x14201F1E),
    card = Color(0xFFFBFAFA), cardEdge = Color(0x10000000), inset = Color(0xFFFAF9F8),
    faint = Color(0xFF605E5C), disabled = Color(0xFFA19F9D),
    grouped = Color(0xFFF3F2F1), pressed = Color(0x0D000000),
    bar = Color(0xD9FFFFFF), menu = Color.White,
    switchOff = Color.Transparent, segment = Color.White,
    blue = Color(0xFF005A9E), blueOnSubtle = Color(0xFF005A9E), accentSubtle = Color(0x1A0078D4),
    green = Color(0xFF00CC6A), red = Color(0xFFE81123), orange = Color(0xFFFFB900),
    greenText = Color(0xFF0E700E), redText = Color(0xFFC50F1F), orangeText = Color(0xFF8A5100),
    greenSubtle = Color(0x2400CC6A), orangeSubtle = Color(0x33FFB900), redSubtle = Color(0x1AE81123),
    onCaution = Color(0xFF201F1E), gray = Color(0xFF605E5C),
)

/** Elevated surface tokens for sheets and dialogs (Fluent surface level 2). */
fun AideTokens.elevated(dark: Boolean): AideTokens = if (!dark) {
    copy(grouped = Color(0xFFF3F2F1), card = Color.White, inset = Color(0xFFFAF9F8))
} else {
    copy(grouped = Color(0xFF282726), card = Color(0xFF2D2C2B), inset = Color(0xFF201F1E))
}

val LocalAideTokens = staticCompositionLocalOf { LightTokens }

/** The app's surfaces and lines (AideTokens) where the theme is. */
val aide: AideTokens
    @Composable @ReadOnlyComposable get() = LocalAideTokens.current

/**
 * Fluent motion tokens (工单 #106): decelerate curve cubic-bezier(0.1, 0.9, 0.2, 1),
 * durations 150ms (fast) / 200ms (mid) / 250ms (slow, cap).
 */
object Motion {
    val EaseOut = CubicBezierEasing(0.1f, 0.9f, 0.2f, 1f)
    const val Short = 150
    const val Medium = 200
    const val Long = 250
    const val Damping = 0.95f
    const val Stiffness = 460f
    fun <T> spring(visibilityThreshold: T? = null): SpringSpec<T> = spring(Damping, Stiffness, visibilityThreshold)
    fun <T> snappy(visibilityThreshold: T? = null): SpringSpec<T> = spring(1f, 700f, visibilityThreshold)
    fun <T> smooth(visibilityThreshold: T? = null): SpringSpec<T> = spring(1f, Stiffness, visibilityThreshold)
}

/**
 * Rounded Fluent radii (工单 #106, 2026-11-23 owner decision):
 * Sm 6dp (checkbox/inline code), Inner 8dp (nested send button), Md 10dp (controls),
 * Lg 16dp (cards/flyouts), Bubble 18dp (tail 6dp), Xl 24dp (sheets/dialogs).
 */
object Radius {
    val Sm = 6.dp
    val Inner = 8.dp
    val Md = 10.dp
    val Lg = 16.dp
    val Bubble = 18.dp
    val BubbleTail = 6.dp
    val Xl = 24.dp
}

private fun scheme(dark: Boolean, k: AideTokens) = if (dark) {
    darkColorScheme(
        primary = k.blue, onPrimary = Color.White,
        primaryContainer = k.accentSubtle, onPrimaryContainer = k.blueOnSubtle,
        secondary = Color(0xFFC8C6C4), onSecondary = Color(0xFF1B1A19),
        secondaryContainer = Color(0xFF2D2C2B), onSecondaryContainer = Color(0xFFF3F2F1),
        tertiary = k.orangeText, onTertiary = k.onCaution,
        tertiaryContainer = k.orangeSubtle, onTertiaryContainer = k.orangeText,
        background = k.grouped, onBackground = Color(0xFFF3F2F1),
        surface = k.grouped, onSurface = Color(0xFFF3F2F1),
        surfaceVariant = k.card, onSurfaceVariant = Color(0xFFC8C6C4),
        surfaceDim = k.grouped, surfaceBright = Color(0xFF323130),
        surfaceContainerLowest = k.grouped, surfaceContainerLow = k.card,
        surfaceContainer = k.card, surfaceContainerHigh = k.menu, surfaceContainerHighest = Color(0xFF3B3A39),
        surfaceTint = Color.Transparent,
        inverseSurface = Color(0xFFF3F2F1), inverseOnSurface = Color(0xFF201F1E), inversePrimary = Color(0xFF005A9E),
        outline = k.controlBorder, outlineVariant = k.separator,
        error = k.redText, onError = Color.White,
        errorContainer = k.redSubtle, onErrorContainer = k.redText,
        scrim = Color.Black,
    )
} else {
    lightColorScheme(
        primary = k.blue, onPrimary = Color.White,
        primaryContainer = k.accentSubtle, onPrimaryContainer = k.blueOnSubtle,
        secondary = Color(0xFF605E5C), onSecondary = Color.White,
        secondaryContainer = Color(0xFFEDEBE9), onSecondaryContainer = Color(0xFF201F1E),
        tertiary = k.orangeText, onTertiary = Color.White,
        tertiaryContainer = k.orangeSubtle, onTertiaryContainer = k.orangeText,
        background = k.grouped, onBackground = Color(0xFF201F1E),
        surface = k.grouped, onSurface = Color(0xFF201F1E),
        surfaceVariant = k.card, onSurfaceVariant = Color(0xFF605E5C),
        surfaceDim = k.grouped, surfaceBright = Color.White,
        surfaceContainerLowest = Color.White, surfaceContainerLow = k.card,
        surfaceContainer = k.card, surfaceContainerHigh = k.inset, surfaceContainerHighest = Color(0xFFEDEBE9),
        surfaceTint = Color.Transparent,
        inverseSurface = Color(0xFF201F1E), inverseOnSurface = Color(0xFFF3F2F1), inversePrimary = Color(0xFF2899F5),
        outline = k.controlBorder, outlineVariant = k.separator,
        error = k.redText, onError = Color.White,
        errorContainer = k.redSubtle, onErrorContainer = k.redText,
        scrim = Color.Black,
    )
}

/**
 * Fluent type ramp on Android (工单 #106, system Roboto + Noto Sans CJK):
 * Heading 28/36 SemiBold, Subheading 20/28 SemiBold, Body Large (and report reading) 16/24,
 * Body 14/20, Label/Caption 12/16.
 */
object IosType {
    val LargeTitle = TextStyle(fontSize = 28.sp, lineHeight = 36.sp, fontWeight = FontWeight.SemiBold)
    val Title1 = TextStyle(fontSize = 28.sp, lineHeight = 36.sp, fontWeight = FontWeight.SemiBold)
    val Title2 = TextStyle(fontSize = 20.sp, lineHeight = 28.sp, fontWeight = FontWeight.SemiBold)
    val Title3 = TextStyle(fontSize = 20.sp, lineHeight = 28.sp, fontWeight = FontWeight.SemiBold)
    val Headline = TextStyle(fontSize = 16.sp, lineHeight = 24.sp, fontWeight = FontWeight.SemiBold)
    val Body = TextStyle(fontSize = 16.sp, lineHeight = 24.sp)
    val Callout = TextStyle(fontSize = 16.sp, lineHeight = 24.sp)
    val Subheadline = TextStyle(fontSize = 14.sp, lineHeight = 20.sp)
    val Footnote = TextStyle(fontSize = 13.sp, lineHeight = 18.sp)
    val Caption = TextStyle(fontSize = 12.sp, lineHeight = 16.sp)
    val Caption2 = TextStyle(fontSize = 12.sp, lineHeight = 16.sp)
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
        labelLarge = IosType.Subheadline.copy(fontWeight = FontWeight.SemiBold).on(d.labelLarge),
        labelMedium = IosType.Caption.copy(fontWeight = FontWeight.SemiBold).on(d.labelMedium),
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
            CompositionLocalProvider(LocalIndication provides Highlight(k.pressed), LocalRippleConfiguration provides null, content = content)
        }
    }
}
