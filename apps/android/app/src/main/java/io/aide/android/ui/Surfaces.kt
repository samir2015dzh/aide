package io.aide.android.ui

import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
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
import androidx.compose.material3.ripple
import androidx.compose.runtime.Composable
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

// The surfaces the pages are built of (工单 #54, after #48 was not seen on the owner's phone): a card is clearly a
// step above the page, with an edge you can see; inside it, a block a step above again; states are tinted pills,
// not grey words; a list's rows sit in one card, as a group. Nothing here draws a shadow or a blur, and nothing is a
// `composed` modifier: a list scrolling draws many of these each frame (工单 #55).

/** A card: a step above the page in either mode, a visible edge, the large rounding. */
@Composable
fun Modifier.aideCard(shape: Shape = RoundedCornerShape(Radius.Lg)): Modifier =
    this.clip(shape).background(aide.card).border(0.5.dp, aide.cardEdge, shape)

/** A block inside a card (an agent under its machine): a step above the card, no edge. */
@Composable
fun Modifier.aideInset(): Modifier = this.clip(RoundedCornerShape(Radius.Md)).background(aide.inset)

/**
 * One row of a group that reads as one card (a list's section), each row its own list item: the first has the top
 * corners, the last the bottom ones, the rest none, so a long list composes row by row rather than a card at once.
 */
@Composable
fun Modifier.groupRow(index: Int, count: Int): Modifier {
    val r = Radius.Lg
    val shape = RoundedCornerShape(
        topStart = if (index == 0) r else 0.dp, topEnd = if (index == 0) r else 0.dp,
        bottomStart = if (index == count - 1) r else 0.dp, bottomEnd = if (index == count - 1) r else 0.dp,
    )
    return this.clip(shape).background(aide.card)
}

/** A row or card that opens something: a ripple, and a slight give under the finger while pressed. */
@Composable
fun Modifier.pressable(onClick: () -> Unit): Modifier {
    val interaction = remember { MutableInteractionSource() }
    val pressed by interaction.collectIsPressedAsState()
    val scale by animateFloatAsState(if (pressed) 0.985f else 1f, tween(Motion.Short, easing = Motion.EaseOut), label = "press")
    return this
        .graphicsLayer { scaleX = scale; scaleY = scale }
        .clickable(interactionSource = interaction, indication = ripple(), onClick = onClick)
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

/**
 * A state as a pill (Grok reference note §5): a tinted capsule with a dot, the one place a row says its state, so it
 * is seen before it is read.
 */
@Composable
fun StatusPill(text: String, tone: Tone, modifier: Modifier = Modifier) {
    val c = toneColor(tone)
    Row(
        modifier.clip(CircleShape).background(c.copy(alpha = if (tone == Tone.Quiet) 0.12f else 0.16f)).padding(start = 7.dp, end = 9.dp, top = 3.dp, bottom = 3.dp),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(5.dp),
    ) {
        Box(Modifier.size(6.dp).background(c, CircleShape))
        Text(text, color = c, fontSize = 12.sp, lineHeight = 16.sp, fontWeight = FontWeight.SemiBold, maxLines = 1)
    }
}

/** A small count beside a heading. */
@Composable
fun CountBadge(n: Int, tone: Tone = Tone.Quiet) {
    val c = toneColor(tone)
    Box(Modifier.clip(CircleShape).background(c.copy(alpha = 0.16f)).padding(horizontal = 7.dp, vertical = 1.dp)) {
        Text("$n", color = c, fontSize = 11.5.sp, lineHeight = 16.sp, fontWeight = FontWeight.Bold)
    }
}

/** A quiet fact in a row of them (a version, a count): a small grey chip. */
@Composable
fun FactChip(text: String) {
    Text(
        text, Modifier.clip(RoundedCornerShape(Radius.Sm)).background(aide.fill2).padding(horizontal = 8.dp, vertical = 3.dp),
        style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1,
    )
}

/** A machine's mark: its first letter on a quiet tile, as an agent has its face. */
@Composable
fun MachineMark(name: String, online: Boolean, size: Int = 40) {
    Box(Modifier.size(size.dp).clip(RoundedCornerShape((size * 0.28).dp)).background(aide.fill2), contentAlignment = Alignment.Center) {
        Text(name.take(1).uppercase(), fontWeight = FontWeight.Bold, fontSize = (size * 0.42).sp, color = MaterialTheme.colorScheme.onSurface)
        Box(Modifier.align(Alignment.BottomEnd).padding(2.dp).size(10.dp).background(aide.card, CircleShape).padding(2.dp).background(if (online) Positive else aide.faint, CircleShape))
    }
}

/** Whether the list this sits in is being scrolled: what moves by itself stops meanwhile (工单 #55 §4). */
val LocalScrolling = compositionLocalOf { false }

@Composable
fun HSpace(w: Int) = Spacer(Modifier.width(w.dp))

/** One number of a page's overview (BotArk reference §4: the count large, what it counts small above it). */
@Composable
fun StatTile(label: String, value: String, modifier: Modifier = Modifier, of: String? = null, tone: Tone = Tone.Quiet) {
    androidx.compose.foundation.layout.Column(modifier.aideCard().padding(horizontal = 14.dp, vertical = 12.dp)) {
        Text(label, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
        Row(verticalAlignment = Alignment.Bottom) {
            Text(value, fontSize = 26.sp, lineHeight = 32.sp, fontWeight = FontWeight.Bold, color = if (tone == Tone.Quiet) MaterialTheme.colorScheme.onSurface else toneColor(tone))
            of?.let { Text(" / $it", style = MaterialTheme.typography.bodyMedium, color = aide.faint, modifier = Modifier.padding(bottom = 4.dp)) }
        }
    }
}

/** A face with its state on the corner (BotArk reference §1): the dot is seen before the name is read. */
@Composable
fun FaceWithDot(agent: String, size: Int, dot: Color) {
    Box {
        AgentFace(agent, size)
        Box(Modifier.align(Alignment.BottomEnd).offset(x = 2.dp, y = 2.dp).size(12.dp).background(aide.inset, CircleShape).padding(2.dp).background(dot, CircleShape))
    }
}
