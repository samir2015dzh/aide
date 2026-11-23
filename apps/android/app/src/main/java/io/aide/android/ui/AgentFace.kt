package io.aide.android.ui

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.PathFillType
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.addPathNodes
import androidx.compose.ui.graphics.vector.rememberVectorPainter
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.unit.dp
import io.aide.android.R
import io.aide.android.model.Bot
import io.aide.android.model.REACT_MS
import io.aide.android.model.botLook
import io.aide.android.model.botMood
import io.aide.android.model.botSeed
import kotlinx.coroutines.delay
import java.time.Instant

// A bot's face (工单 #83), the same on every screen as on the desktop: its Grok Bot character (GrokFace.kt), with its
// app's own icon (Claude, Codex, DSH, WorkBuddy…, Muse, Grok Bot…) small on its corner where there is room. A session's
// row, where no bot is at hand, still shows its coding agent's app icon (AgentFace).

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

/** An app's face in a circle: its icon edge to edge, a drawn logo on white, or the robot on grey for an app without one. */
@Composable
fun AppFace(app: String, size: Int) {
    val m = Modifier.size(size.dp).clip(CircleShape)
    pictures[app]?.let {
        Image(painterResource(it), null, m, contentScale = ContentScale.Crop)
        return
    }
    logos[app]?.let {
        Box(m.background(Color.White).border(0.5.dp, Color(0x1F000000), CircleShape), contentAlignment = Alignment.Center) {
            Image(painterResource(it), null, Modifier.size((size * 0.62).dp))
        }
        return
    }
    RobotFace(size)
}

@Composable
private fun RobotFace(size: Int) {
    val color = Color(0xFF8E8E93)
    Box(Modifier.size(size.dp).background(color, CircleShape), contentAlignment = Alignment.Center) {
        Image(rememberVectorPainter(robot), null, Modifier.size((size * 0.7).dp))
    }
}

/** A coding agent's face, where no bot is at hand (a session's row): its app's icon. */
@Composable
fun AgentFace(agent: String, size: Int = 32) = AppFace(agent, size)

/**
 * A bot's face (工单 #83): its Grok Bot character (GrokMark), in the look the owner gave it (its contact's, else its
 * grant's), else its seed's, moved only by its real state (botMood, the desktop's table); away or unknown it rests,
 * faded. Its app's logo on its corner where there is room; dot, its state's colour on the other. still: it rests
 * (where many show at once). poke: a count, each new one spins and bounces it.
 */
@Composable
fun BotAvatar(b: Bot, size: Int, dot: Color? = null, still: Boolean = false, poke: Int = 0) {
    // A moment of done or failed is read again once it is over.
    var tick by remember { mutableIntStateOf(0) }
    val mood = botMood(b, LocalSessions.current, Instant.now()).also { tick }
    LaunchedEffect(mood.state) {
        if (mood.state == "finished" || mood.state == "failed") {
            delay(REACT_MS)
            tick++
        }
    }
    val app = b.agent.ifEmpty { b.client?.let { personalApp(it.label) }.orEmpty() }
    Box(Modifier.size(size.dp)) {
        GrokMark(botSeed(b), botLook(b), mood, size, still = still, poke = poke)
        if (size >= 34 && (pictures[app] ?: logos[app]) != null) {
            val s = (size * 0.3).toInt()
            Box(Modifier.align(Alignment.BottomStart).offset(x = (-2).dp, y = 2.dp).background(Color.White, CircleShape).padding(2.dp)) { AppFace(app, s) }
        }
        if (dot != null) {
            val d = (size / 4).coerceIn(9, 13)
            Box(Modifier.align(Alignment.BottomEnd).offset(x = 1.dp, y = 1.dp).size(d.dp).background(aide.card, CircleShape).padding(2.dp).background(dot, CircleShape))
        }
    }
}
