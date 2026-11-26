package io.aide.android.ui

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.IndicationNodeFactory
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.InteractionSource
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.PressInteraction
import androidx.compose.foundation.interaction.collectIsPressedAsState
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.graphics.drawscope.ContentDrawScope
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.node.DelegatableNode
import androidx.compose.ui.node.DrawModifierNode
import androidx.compose.ui.node.invalidateDraw
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import io.aide.android.model.DotHue
import io.aide.android.model.Presence
import io.aide.android.model.presenceDot
import kotlinx.coroutines.launch

// Fluent Design surfaces for Android (工单 #106 / #109): 16dp rounded cards with subtle hairline edge,
// 10dp sunken inner blocks, contrast-checked status pills (vivid dot + darkened text on subtle tint),
// and zero-recomposition press highlights for smooth scrolling (工单 #55).

/** A Fluent card: 16dp rounded, subtle light-mode ambient elevation, card surface fill with 1dp hairline edge. */
@Composable
fun Modifier.aideCard(shape: Shape = RoundedCornerShape(Radius.Lg)): Modifier {
    val dark = isSystemInDarkTheme()
    val elevated = if (dark) this else this.shadow(
        elevation = 2.dp,
        shape = shape,
        ambientColor = Color.Black.copy(alpha = 0.08f),
        spotColor = Color.Black.copy(alpha = 0.08f),
    )
    return elevated.clip(shape).background(aide.card).border(1.dp, aide.cardEdge, shape)
}

/** A sunken block inside a card (an agent workbench, code block, call detail): 10dp rounded. */
@Composable
fun Modifier.aideInset(): Modifier =
    this.clip(RoundedCornerShape(Radius.Md)).background(aide.inset)

/**
 * One row of a grouped Fluent card, each row its own LazyColumn item: 16dp outer corners,
 * hairline divider indented from [inset] between rows.
 */
@Composable
fun Modifier.groupRow(index: Int, count: Int, inset: Dp = 16.dp): Modifier {
    val r = Radius.Lg
    val shape = RoundedCornerShape(
        topStart = if (index == 0) r else 0.dp, topEnd = if (index == 0) r else 0.dp,
        bottomStart = if (index == count - 1) r else 0.dp, bottomEnd = if (index == count - 1) r else 0.dp,
    )
    val line = aide.separator
    val base = this.clip(shape).background(aide.card)
    return if (index == 0) base else base.drawBehind { drawLine(line, Offset(inset.toPx(), 0f), Offset(size.width, 0f), 1f) }
}

/** Press highlight drawn by a Modifier.Node without recomposing list rows (工单 #55). */
data class Highlight(val color: Color) : IndicationNodeFactory {
    override fun create(interactionSource: InteractionSource): DelegatableNode = HighlightNode(interactionSource, color)
}

private class HighlightNode(private val source: InteractionSource, private val color: Color) : Modifier.Node(), DrawModifierNode {
    private val alpha = Animatable(0f)
    override fun onAttach() {
        coroutineScope.launch {
            source.interactions.collect { i ->
                when (i) {
                    is PressInteraction.Press -> launch { alpha.snapTo(1f); invalidateDraw() }
                    is PressInteraction.Release, is PressInteraction.Cancel -> launch { alpha.animateTo(0f, tween(Motion.Long, easing = Motion.EaseOut)) { invalidateDraw() } }
                }
            }
        }
    }
    override fun ContentDrawScope.draw() {
        if (alpha.value > 0f) drawRect(color, alpha = alpha.value)
        drawContent()
    }
}

/** A pressable card, tile, or chip: subtle 0.97 scale and Fluent press overlay. */
@Composable
fun Modifier.pressable(onClick: () -> Unit): Modifier {
    val interaction = remember { MutableInteractionSource() }
    val pressed by interaction.collectIsPressedAsState()
    val scale by animateFloatAsState(if (pressed) 0.97f else 1f, Motion.snappy(), label = "press")
    return this
        .graphicsLayer { scaleX = scale; scaleY = scale }
        .clickable(interactionSource = interaction, indication = Highlight(aide.pressed), role = Role.Button, onClick = onClick)
}

/** A list row that opens something: highlighted while pressed; does not shrink. */
@Composable
fun Modifier.rowPress(onClick: () -> Unit): Modifier =
    this.clickable(interactionSource = remember { MutableInteractionSource() }, indication = Highlight(aide.pressed), role = Role.Button, onClick = onClick)

/** Plain text/icon button press feedback: dimmed while pressed. */
@Composable
fun Modifier.dimPress(enabled: Boolean = true, role: Role = Role.Button, onClick: () -> Unit): Modifier {
    val interaction = remember { MutableInteractionSource() }
    val pressed by interaction.collectIsPressedAsState()
    val alpha by animateFloatAsState(if (pressed) 0.35f else 1f, if (pressed) tween(0) else tween(Motion.Short, easing = Motion.EaseOut), label = "dim")
    return this.graphicsLayer { this.alpha = alpha }.clickable(interactionSource = interaction, indication = null, enabled = enabled, role = role, onClick = onClick)
}

/** The semantic weight a state is said with. */
enum class Tone { Good, Warn, Bad, Live, Quiet }

/** Contrast-checked text colour for [Tone] (>= 4.5:1 on page and card). */
@Composable
fun toneColor(t: Tone): Color = when (t) {
    Tone.Good -> aide.greenText
    Tone.Warn -> aide.orangeText
    Tone.Bad -> aide.redText
    Tone.Live -> aide.blueOnSubtle
    Tone.Quiet -> MaterialTheme.colorScheme.onSurfaceVariant
}

/** Vivid status dot colour for [Tone] (工单 #106: raw status hue on the 6dp/8dp dot, darkened hue on text). */
@Composable
fun toneDotColor(t: Tone): Color = when (t) {
    Tone.Good -> aide.green
    Tone.Warn -> aide.orange
    Tone.Bad -> aide.red
    Tone.Live -> aide.accent
    Tone.Quiet -> aide.disabled
}

/** Subtle pill fill for [Tone]. */
@Composable
fun toneSubtleFill(t: Tone): Color = when (t) {
    Tone.Good -> aide.greenSubtle
    Tone.Warn -> aide.orangeSubtle
    Tone.Bad -> aide.redSubtle
    Tone.Live -> aide.accentSubtle
    Tone.Quiet -> aide.fill
}

/** A Fluent state pill: subtle tinted capsule, 6dp vivid status dot (a hollow ring for ring), contrast-safe SemiBold label. */
@Composable
fun StatusPill(text: String, tone: Tone, modifier: Modifier = Modifier, ring: Boolean = false) {
    Row(
        modifier.heightIn(min = 24.dp).clip(CircleShape).background(toneSubtleFill(tone)).padding(start = 8.dp, end = 9.dp, top = 3.dp, bottom = 3.dp),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Box(Modifier.size(6.dp).then(if (ring) Modifier.border(1.5.dp, toneDotColor(tone), CircleShape) else Modifier.background(toneDotColor(tone), CircleShape)))
        Text(text, color = toneColor(tone), fontSize = 12.sp, lineHeight = 16.sp, fontWeight = FontWeight.SemiBold, maxLines = 1)
    }
}

/**
 * The tone a presence is said in (工单 #135 P25 / 审计 B): read from its one dot (model presenceDot), so the same label is
 * the same colour on the roster, a bot's page, a room's members and the devices tab — never from the label's words.
 */
fun presenceTone(p: Presence): Tone = when (presenceDot(p).hue) {
    DotHue.GREEN -> Tone.Good
    DotHue.BLUE -> Tone.Live
    DotHue.ORANGE -> Tone.Warn
    DotHue.RED -> Tone.Bad
    DotHue.GREY -> Tone.Quiet
}

/** A presence's state pill: its word, its dot (a hollow ring while checking, as the desktop's). */
@Composable
fun StatusPill(text: String, presence: Presence, modifier: Modifier = Modifier) = StatusPill(text, presenceTone(presence), modifier, ring = presenceDot(presence).ring)

/** A presence's dot on a face's corner, ringed in the card's colour: filled, or a hollow ring while checking. */
@Composable
fun Modifier.cornerDot(p: Presence): Modifier {
    val c = toneDotColor(presenceTone(p))
    val base = this.background(aide.card, CircleShape).padding(2.dp)
    return if (presenceDot(p).ring) base.border(1.5.dp, c, CircleShape) else base.background(c, CircleShape)
}

/** A Fluent count badge: yellow #ffb900 with dark #201f1e text for attention, #0078d4 with white for live, neutral subtle otherwise. */
@Composable
fun CountBadge(n: Int, tone: Tone = Tone.Quiet) {
    val (bg, fg) = when (tone) {
        Tone.Warn -> aide.orange to aide.onCaution
        Tone.Live -> aide.accent to Color.White
        Tone.Bad -> aide.red to Color.White
        Tone.Good -> aide.greenSubtle to aide.greenText
        Tone.Quiet -> aide.fill to MaterialTheme.colorScheme.onSurfaceVariant
    }
    Box(Modifier.heightIn(min = 20.dp).widthIn(min = 20.dp).clip(CircleShape).background(bg).padding(horizontal = 6.dp, vertical = 2.dp), contentAlignment = Alignment.Center) {
        Text("$n", color = fg, fontSize = 12.sp, lineHeight = 16.sp, fontWeight = FontWeight.SemiBold)
    }
}

/** A quiet fact in a row of them (a version, a count): a neutral subtle pill. */
@Composable
fun FactChip(text: String) {
    Text(
        text, Modifier.clip(CircleShape).background(aide.fill).padding(horizontal = 9.dp, vertical = 3.dp),
        style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1,
    )
}

/** Whether the list this sits in is being scrolled: what moves by itself stops meanwhile (工单 #55 §4). */
val LocalScrolling = compositionLocalOf { false }

@Composable
fun HSpace(w: Int) = Spacer(Modifier.width(w.dp))

/** One number of a page's overview: what it counts small above, the count large, on a Fluent card. */
@Composable
fun StatTile(label: String, value: String, modifier: Modifier = Modifier, of: String? = null, tone: Tone = Tone.Quiet, onClick: (() -> Unit)? = null) {
    val barColor = if (tone == Tone.Warn) aide.orange else Color.Transparent
    val cardMod = modifier
        .aideCard()
        .then(if (onClick != null) Modifier.pressable(onClick) else Modifier)
        .then(if (barColor != Color.Transparent) Modifier.drawBehind { drawRect(barColor, Offset.Zero, Size(3.dp.toPx(), size.height)) } else Modifier)
        .padding(horizontal = 14.dp, vertical = 12.dp)
    androidx.compose.foundation.layout.Column(cardMod) {
        Text(label, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
        Row(verticalAlignment = Alignment.Bottom) {
            Text(value, style = IosType.Title1, color = if (tone == Tone.Quiet) MaterialTheme.colorScheme.onSurface else toneColor(tone))
            of?.let { Text(" / $it", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(bottom = 4.dp)) }
        }
    }
}

/** A face with its state on the corner: the dot is seen before the name is read. */
@Composable
fun FaceWithDot(agent: String, size: Int, presence: Presence) {
    Box {
        AgentFace(agent, size)
        Box(Modifier.align(Alignment.BottomEnd).offset(x = 2.dp, y = 2.dp).size(12.dp).cornerDot(presence))
    }
}
