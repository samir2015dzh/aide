package io.hermesh.android.ui

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

/** Black and white, as the desktop app (packages/ui/src/brand.css, after Grok's look), and the colours of states it uses sparingly. */
val Brand = Color(0xFF111113)
val BrandCyan = Color(0xFF0891B2)
val Positive = Color(0xFF1F9D55)
val Warning = Color(0xFFD97706)

@Composable
fun HermeshTheme(content: @Composable () -> Unit) {
    val colors = if (isSystemInDarkTheme()) {
        darkColorScheme(
            primary = Color(0xFFF2F2F3), onPrimary = Color(0xFF0B0B0D), secondary = Color(0xFF9B9BA3), tertiary = Color(0xFFF5A050),
            primaryContainer = Color(0xFF232326), onPrimaryContainer = Color(0xFFF2F2F3), secondaryContainer = Color(0xFF232326), onSecondaryContainer = Color(0xFFF2F2F3),
            background = Color(0xFF000000), surface = Color(0xFF0A0A0B), surfaceVariant = Color(0xFF141416), surfaceContainer = Color(0xFF141416),
            onSurface = Color(0xFFF2F2F3), onSurfaceVariant = Color(0xFF9B9BA3), outline = Color(0xFF3A3A3F), error = Color(0xFFFF6B6B),
        )
    } else {
        lightColorScheme(
            primary = Brand, onPrimary = Color.White, secondary = Color(0xFF5F5F66), tertiary = Color(0xFFB4691C),
            primaryContainer = Color(0xFFF0F0F1), onPrimaryContainer = Brand, secondaryContainer = Color(0xFFF0F0F1), onSecondaryContainer = Brand,
            background = Color.White, surface = Color.White, surfaceVariant = Color(0xFFF7F7F8), surfaceContainer = Color(0xFFF7F7F8),
            onSurface = Color(0xFF111113), onSurfaceVariant = Color(0xFF5F5F66), outline = Color(0xFFD4D4D8), error = Color(0xFFE04848),
        )
    }
    MaterialTheme(colorScheme = colors, content = content)
}
