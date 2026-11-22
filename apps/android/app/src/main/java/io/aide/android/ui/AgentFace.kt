package io.aide.android.ui

import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ColorFilter
import androidx.compose.ui.graphics.ColorMatrix
import androidx.compose.ui.graphics.PathFillType
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.rotate
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.addPathNodes
import androidx.compose.ui.graphics.vector.rememberVectorPainter
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import io.aide.android.R
import io.aide.android.api.Client
import io.aide.android.model.Bot
import io.aide.android.model.ConnState
import io.aide.android.model.NotReady
import io.aide.android.model.personaColor
import io.aide.android.model.personaIcons

// A bot's face (工单 #62), the same on every screen as on the desktop: its app's own icon, round — a coding agent's app
// (Claude, Codex, DSH, WorkBuddy…), a personal agent's (Muse, Grok Bot…), or the avatar the owner gave it; a small
// robot on its colour where its app has none, never initials. Its real state moves it: a ring turns round it while its
// sessions really run (and stands still while a list scrolls, 工单 #55), it greys while it is away or unknown, and the
// dot on its corner is its state's colour.

/** The app icons (res/drawable-nodpi, from packages/ui agent-logo-images.ts) and drawn logos (res/drawable). */
private val pictures = mapOf(
    "antigravity" to R.drawable.agent_antigravity, "claude" to R.drawable.agent_claude, "codebuddy" to R.drawable.agent_codebuddy,
    "codex" to R.drawable.agent_codex, "cursor" to R.drawable.agent_cursor, "deepseek" to R.drawable.agent_deepseek,
    "gemini" to R.drawable.agent_gemini, "grok" to R.drawable.agent_grok, "grokbot" to R.drawable.agent_grokbot,
    "kimi" to R.drawable.agent_kimi, "kiro" to R.drawable.agent_kiro, "muse" to R.drawable.agent_muse,
    "openclaw" to R.drawable.agent_openclaw, "opencode" to R.drawable.agent_opencode, "qoder" to R.drawable.agent_qoder,
)
private val logos = mapOf("copilot" to R.drawable.agent_copilot, "qwen" to R.drawable.agent_qwen, "goose" to R.drawable.agent_goose, "auggie" to R.drawable.agent_auggie)

/** A personal agent's app by the name its grant has (as packages/ui personal.ts: a hosted one by its app's own name). */
fun personalApp(label: String): String? = when (Regex("^(.+) \\(([^()\\s]+)\\)$").find(label)?.groupValues?.get(1) ?: label) {
    "Grok Bot" -> "grokbot"
    "Muse" -> "muse"
    "ChatGPT" -> "codex"
    "Claude" -> "claude"
    "OpenClaw" -> "openclaw"
    "Cursor" -> "cursor"
    else -> null
}

/** The colour a face without an app is drawn on, from its name. */
fun nameColor(name: String): Color {
    var h = 0
    for (c in name) h = (h * 31 + c.code) and 0xffff
    return Color.hsl((h % 360).toFloat(), 0.55f, 0.5f)
}

/** The robot drawn where an app has no icon (the desktop's BotGlyph). */
private val robot: ImageVector by lazy {
    ImageVector.Builder("robot", 24.dp, 24.dp, 24f, 24f)
        .addPath(addPathNodes("M10.65 2.9a1.35 1.35 0 1 0 2.7 0a1.35 1.35 0 1 0 -2.7 0Z M11.3 3.6h1.4v3h-1.4Z M2.4 10.3h2.3v4.6h-2.3Z M19.3 10.3h2.3v4.6h-2.3Z"), fill = SolidColor(Color.White))
        .addPath(
            addPathNodes("M8.6 6.2h6.8a4.6 4.6 0 0 1 4.6 4.6v2.8a4.6 4.6 0 0 1-4.6 4.6H8.6A4.6 4.6 0 0 1 4 13.6v-2.8a4.6 4.6 0 0 1 4.6-4.6Zm.9 4.2a1.7 1.7 0 1 0 0 3.4 1.7 1.7 0 0 0 0-3.4Zm5 0a1.7 1.7 0 1 0 0 3.4 1.7 1.7 0 0 0 0-3.4Zm-4.6 5.1a.6.6 0 0 0 0 1.2h4.2a.6.6 0 0 0 0-1.2H9.9Z"),
            fill = SolidColor(Color.White), pathFillType = PathFillType.EvenOdd,
        )
        .build()
}

private val grey = ColorFilter.colorMatrix(ColorMatrix().apply { setToSaturation(0f) })

/** An app's face in a circle: its icon edge to edge, a drawn logo on white, or the robot on grey for an app without one. */
@Composable
fun AppFace(app: String, size: Int, off: Boolean = false) {
    val filter = if (off) grey else null
    val m = Modifier.size(size.dp).clip(CircleShape)
    pictures[app]?.let {
        Image(painterResource(it), null, m, contentScale = ContentScale.Crop, colorFilter = filter)
        return
    }
    logos[app]?.let {
        Box(m.background(Color.White).border(0.5.dp, Color(0x1F000000), CircleShape), contentAlignment = Alignment.Center) {
            Image(painterResource(it), null, Modifier.size((size * 0.62).dp), colorFilter = filter)
        }
        return
    }
    RobotFace(Color(0xFF8E8E93), size)
}

@Composable
private fun RobotFace(color: Color, size: Int) {
    Box(Modifier.size(size.dp).background(color, CircleShape), contentAlignment = Alignment.Center) {
        Image(rememberVectorPainter(robot), null, Modifier.size((size * 0.7).dp))
    }
}

/** A coding agent's face, where no bot is at hand (a session's row): its app's icon. */
@Composable
fun AgentFace(agent: String, size: Int = 32) = AppFace(agent, size)

/** A grant's face: the app it is (its name says), else the robot on its colour. */
@Composable
fun GrantFace(c: Client, size: Int, off: Boolean = false) {
    personalApp(c.label)?.let { AppFace(it, size, off); return }
    RobotFace(nameColor(c.label), size)
}

/**
 * A bot's face with what its real state says (工单 #62): dot, its state's colour on the corner; running, a ring turning
 * round it while its sessions run, stopped while a list scrolls; away or unknown, grey.
 */
@Composable
fun BotAvatar(b: Bot, size: Int, dot: Color? = null, still: Boolean = false) {
    val off = b.state.why == NotReady.OFFLINE || b.state.state == ConnState.UNKNOWN
    val running = !still && b.running > 0
    val scrolling = LocalScrolling.current
    Box(Modifier.size(size.dp)) {
        if (running) RunningRing(size, moving = !scrolling)
        Box(Modifier.graphicsLayer { alpha = if (off) 0.55f else 1f }) {
            val p = b.personas.firstOrNull()
            when {
                p != null -> Box(Modifier.size(size.dp).background(Color(personaColor(p)), CircleShape), contentAlignment = Alignment.Center) {
                    val glyph = personaIcons[p.icon]
                    if (glyph != null) Text(glyph, fontSize = (size * 0.48).sp) else Image(rememberVectorPainter(robot), null, Modifier.size((size * 0.7).dp))
                }
                b.agent.isNotEmpty() -> AppFace(b.agent, size, off)
                b.client != null -> GrantFace(b.client, size, off)
                else -> RobotFace(nameColor(b.name), size)
            }
        }
        if (dot != null) {
            val d = (size / 4).coerceIn(9, 13)
            Box(Modifier.align(Alignment.BottomEnd).offset(x = 1.dp, y = 1.dp).size(d.dp).background(aide.card, CircleShape).padding(2.dp).background(dot, CircleShape))
        }
    }
}

/** The ring round a face at work: an arc that turns, held still (and drawn whole) while moving is off. */
@Composable
private fun RunningRing(size: Int, moving: Boolean) {
    val accent = BrandCyan
    val angle = if (moving) {
        val t = rememberInfiniteTransition(label = "ring")
        t.animateFloat(0f, 360f, infiniteRepeatable(tween(1100, easing = LinearEasing), RepeatMode.Restart), label = "turn").value
    } else {
        0f
    }
    val brush = remember(accent) { Brush.sweepGradient(listOf(Color.Transparent, accent.copy(alpha = 0.15f), accent)) }
    Box(
        Modifier.size(size.dp).drawBehind {
            val w = 2.dp.toPx()
            val inset = -3.dp.toPx() + w / 2
            rotate(angle) {
                drawArc(
                    brush, 0f, 360f, false, topLeft = Offset(inset, inset), size = Size(this.size.width - 2 * inset, this.size.height - 2 * inset),
                    style = Stroke(w),
                )
            }
        },
    )
}
