package io.aide.android.ui

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.IndicationNodeFactory
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.InteractionSource
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.PressInteraction
import androidx.compose.foundation.interaction.collectIsPressedAsState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
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
import androidx.compose.ui.geometry.Offset
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
import kotlinx.coroutines.launch

// The surfaces the pages are built of, as iOS's inset grouped lists (工单 #95, HIG Lists and tables): cards a step above
// the grouped background, rounded 10, no edge and no shadow; rows in one card, a hairline between them indented to where
// the row's words start; states as tinted pills. Nothing here draws a shadow or a blur, and nothing is a `composed`
// modifier: a list scrolling draws many of these each frame (工单 #55).

/** An inset grouped card: a step above the page in either mode, rounded 10, no edge (iOS draws none). */
@Composable
fun Modifier.aideCard(shape: Shape = RoundedCornerShape(Radius.Lg)): Modifier = this.clip(shape).background(aide.card)

/** A block inside a card (an agent under its machine): the third step, rounded 8. */
@Composable
fun Modifier.aideInset(): Modifier = this.clip(RoundedCornerShape(Radius.Md)).background(aide.inset)

/**
 * One row of an inset grouped card (a list's section), each row its own list item: the first has the top corners, the
 * last the bottom ones; every row after the first has the hairline over it, from [inset] (where its words start) to the
 * card's edge, so a long list composes row by row rather than a card at once.
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

/**
 * The highlight of what is pressed, as iOS's (the cell's selected background): laid under the content at once on the
 * press, fading as the finger lifts. Drawn by a node, not recomposed: a list's rows each have one (工单 #55).
 */
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

/**
 * A card, tile or chip that opens something (工单 #95): under the finger it gives a little (scale 0.97, the snappy spring)
 * and is highlighted, then springs back.
 */
@Composable
fun Modifier.pressable(onClick: () -> Unit): Modifier {
    val interaction = remember { MutableInteractionSource() }
    val pressed by interaction.collectIsPressedAsState()
    val scale by animateFloatAsState(if (pressed) 0.97f else 1f, Motion.snappy(), label = "press")
    return this
        .graphicsLayer { scaleX = scale; scaleY = scale }
        .clickable(interactionSource = interaction, indication = Highlight(aide.pressed), role = Role.Button, onClick = onClick)
}

/** A list row that opens something: highlighted while pressed, as an iOS cell; a row does not shrink. */
@Composable
fun Modifier.rowPress(onClick: () -> Unit): Modifier =
    this.clickable(interactionSource = remember { MutableInteractionSource() }, indication = Highlight(aide.pressed), role = Role.Button, onClick = onClick)

/** Words or an icon that act (a plain button): dimmed while pressed, as iOS's plain buttons, no shape around them. */
@Composable
fun Modifier.dimPress(enabled: Boolean = true, role: Role = Role.Button, onClick: () -> Unit): Modifier {
    val interaction = remember { MutableInteractionSource() }
    val pressed by interaction.collectIsPressedAsState()
    val alpha by animateFloatAsState(if (pressed) 0.3f else 1f, if (pressed) tween(0) else tween(Motion.Medium), label = "dim")
    return this.graphicsLayer { this.alpha = alpha }.clickable(interactionSource = interaction, indication = null, enabled = enabled, role = role, onClick = onClick)
}

/** The weight a state is said with: a colour, used for a pill's tint and its words. */
enum class Tone { Good, Warn, Bad, Live, Quiet }

@Composable
fun toneColor(t: Tone): Color = when (t) {
    Tone.Good -> Positive
    Tone.Warn -> Warning
    Tone.Bad -> MaterialTheme.colorScheme.error
    Tone.Live -> BrandCyan
    Tone.Quiet -> MaterialTheme.colorScheme.onSurfaceVariant
}

/** A state as a pill: a capsule tinted in its system colour with a dot, the one place a row says its state. */
@Composable
fun StatusPill(text: String, tone: Tone, modifier: Modifier = Modifier) {
    val c = toneColor(tone)
    Row(
        modifier.clip(CircleShape).background(c.copy(alpha = if (tone == Tone.Quiet) 0.14f else 0.16f)).padding(start = 7.dp, end = 9.dp, top = 2.dp, bottom = 2.dp),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(5.dp),
    ) {
        Box(Modifier.size(6.dp).background(c, CircleShape))
        Text(text, color = c, fontSize = 12.sp, lineHeight = 16.sp, fontWeight = FontWeight.SemiBold, maxLines = 1)
    }
}

/** A small count beside a heading or a row (as iOS's badges): a capsule. */
@Composable
fun CountBadge(n: Int, tone: Tone = Tone.Quiet) {
    val c = toneColor(tone)
    Box(Modifier.clip(CircleShape).background(c.copy(alpha = 0.16f)).padding(horizontal = 7.dp, vertical = 1.dp)) {
        Text("$n", color = c, fontSize = 12.sp, lineHeight = 16.sp, fontWeight = FontWeight.SemiBold)
    }
}

/** A quiet fact in a row of them (a version, a count): a small grey capsule. */
@Composable
fun FactChip(text: String) {
    Text(
        text, Modifier.clip(CircleShape).background(aide.fill2).padding(horizontal = 9.dp, vertical = 3.dp),
        style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1,
    )
}

/** A machine's mark: its first letter on a quiet tile (an app icon's rounding), as an agent has its face. */
@Composable
fun MachineMark(name: String, online: Boolean, size: Int = 40) {
    Box(Modifier.size(size.dp).clip(RoundedCornerShape((size * 0.225).dp)).background(aide.fill2), contentAlignment = Alignment.Center) {
        Text(name.take(1).uppercase(), fontWeight = FontWeight.SemiBold, fontSize = (size * 0.42).sp, color = MaterialTheme.colorScheme.onSurface)
        Box(Modifier.align(Alignment.BottomEnd).padding(2.dp).size(10.dp).background(aide.card, CircleShape).padding(2.dp).background(if (online) Positive else aide.faint, CircleShape))
    }
}

/** Whether the list this sits in is being scrolled: what moves by itself stops meanwhile (工单 #55 §4). */
val LocalScrolling = compositionLocalOf { false }

@Composable
fun HSpace(w: Int) = Spacer(Modifier.width(w.dp))

/** One number of a page's overview: what it counts small above, the count large, on an inset card. */
@Composable
fun StatTile(label: String, value: String, modifier: Modifier = Modifier, of: String? = null, tone: Tone = Tone.Quiet) {
    androidx.compose.foundation.layout.Column(modifier.aideCard().padding(horizontal = 14.dp, vertical = 11.dp)) {
        Text(label, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
        Row(verticalAlignment = Alignment.Bottom) {
            Text(value, style = IosType.Title1, color = if (tone == Tone.Quiet) MaterialTheme.colorScheme.onSurface else toneColor(tone))
            of?.let { Text(" / $it", style = MaterialTheme.typography.bodyMedium, color = aide.faint, modifier = Modifier.padding(bottom = 4.dp)) }
        }
    }
}

/** A face with its state on the corner: the dot is seen before the name is read. */
@Composable
fun FaceWithDot(agent: String, size: Int, dot: Color) {
    Box {
        AgentFace(agent, size)
        Box(Modifier.align(Alignment.BottomEnd).offset(x = 2.dp, y = 2.dp).size(12.dp).background(aide.card, CircleShape).padding(2.dp).background(dot, CircleShape))
    }
}
