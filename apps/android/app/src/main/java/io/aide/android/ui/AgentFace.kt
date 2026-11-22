package io.aide.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

// Each coding agent one face, the same on every screen (Grok Bot reference note §1, 工单 #45/#48): a colour of its
// own and a short mark, so it is told apart without reading; none is left without one.

private val agentColors = mapOf(
    "claude" to 0xFFD97757, "codex" to 0xFF10A37F, "deepseek" to 0xFF4D6BFE, "gemini" to 0xFF4285F4,
    "codebuddy" to 0xFF6C5CE7, "copilot" to 0xFF6E40C9, "qwen" to 0xFF615CED, "kimi" to 0xFF2B2B2B,
    "cursor" to 0xFF3A3A3F, "opencode" to 0xFF5B6472, "grok" to 0xFF3A3A3F,
)

private val agentMarks = mapOf("claude" to "Cl", "codex" to "Cx", "deepseek" to "DS", "gemini" to "Ge", "codebuddy" to "WB", "copilot" to "Co", "qwen" to "Qw", "kimi" to "Ki", "cursor" to "Cu", "opencode" to "OC")

fun agentColor(agent: String): Color = Color(agentColors[agent] ?: (0xFF000000 or ((agent.hashCode().toLong() and 0x7F7F7F) + 0x303030)))

@Composable
fun AgentFace(agent: String, size: Int = 32) {
    Box(Modifier.size(size.dp).background(agentColor(agent), RoundedCornerShape((size * 0.28).dp)), contentAlignment = Alignment.Center) {
        Text(agentMarks[agent] ?: agent.take(2).replaceFirstChar { it.uppercase() }, color = Color.White, fontWeight = FontWeight.Bold, fontSize = (size * 0.36).sp)
    }
}
