package io.aide.android.ui

import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsPressedAsState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ripple
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.composed
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.unit.dp

// The surfaces the pages are built of, as the desktop's (Theme.kt's tokens): a card a step above the page,
// with a hairline edge and a restrained shadow; a row that answers a press. One look and one motion for
// every page, rather than each its own (the owner's ruling, 2026-11-21: "要有质感").

/** A card: a step above the page in either mode, a hairline edge, a soft shadow, the large rounding. */
fun Modifier.aideCard(): Modifier = composed {
    val shape = RoundedCornerShape(Radius.Lg)
    this
        .shadow(1.dp, shape, clip = false, ambientColor = aide.separator, spotColor = aide.separator)
        .clip(shape)
        .background(MaterialTheme.colorScheme.surfaceContainerLow)
        .border(0.5.dp, aide.separator, shape)
}

/** A row or card that opens something: a ripple, and a slight give under the finger while pressed. */
fun Modifier.pressable(onClick: () -> Unit): Modifier = composed {
    val interaction = remember { MutableInteractionSource() }
    val pressed by interaction.collectIsPressedAsState()
    val scale by animateFloatAsState(if (pressed) 0.985f else 1f, tween(Motion.Short, easing = Motion.EaseOut), label = "press")
    this
        .graphicsLayer { scaleX = scale; scaleY = scale }
        .clickable(interactionSource = interaction, indication = ripple(), onClick = onClick)
}
