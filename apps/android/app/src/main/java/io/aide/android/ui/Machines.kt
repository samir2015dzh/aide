package io.aide.android.ui

import androidx.compose.foundation.layout.Box
import androidx.compose.animation.core.tween
import androidx.compose.animation.animateContentSize
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Card
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import io.aide.android.api.AgentStatus
import io.aide.android.api.Machine
import io.aide.android.model.AppModel
import io.aide.android.model.ConnState
import io.aide.android.model.agentTitle
import io.aide.android.model.agoText
import io.aide.android.model.machineConnections
import io.aide.android.model.instantOrNull
import io.aide.android.model.pausedUntil
import io.aide.android.model.usageResetAt
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import kotlinx.coroutines.launch

/** The machines and their coding agents (the devices and this phone are in Settings). */
@Composable
fun MachinesTab(model: AppModel) {
    val world by model.world.collectAsState()
    Column(Modifier.fillMaxSize()) {
        PageTitle(t("Machines", "机器"))
        SyncLine(model)
        LazyColumn(Modifier.fillMaxSize(), contentPadding = androidx.compose.foundation.layout.PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            if (world.machines.isEmpty() && world.watched) item { Text(t("No machines yet.", "还没有机器。"), color = MaterialTheme.colorScheme.onSurfaceVariant) }
            items(world.machines, key = { it.machineId }) {
                Box(Modifier.animateItem(fadeInSpec = tween(Motion.Medium), placementSpec = tween(Motion.Medium, easing = Motion.EaseOut))) { MachineCard(model, it) }
            }
            // Coding agents are connected where they run: on a computer, through the desktop app's guide.
            item { SectionHeader(t("Connecting a coding agent", "连接编码 Agent")) }
            item {
                Text(
                    t(
                        "Coding agents run on your computers, not on this phone. Install DeepSeek Harness (DSH), the Claude app (open its Code tab once) or Codex on a computer, then open Aide there: Settings › Connect walks you through each one, says whether it is detected and runs a test. Each computer above lists the agents it detected, and their tasks show under Sessions here.",
                        "编码 Agent 在你的电脑上运行，不在这台手机上。先在电脑上装好 DeepSeek Harness（DSH）、Claude 应用（打开一次它的 Code 标签页）或 Codex，再打开那台电脑上的 Aide：设置 › 连接 会一步步带你连接、告诉你是否检测到，并运行测试。上面每台电脑都列出了它检测到的 Agent，它们的任务会显示在这里的「会话」里。",
                    ),
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 4.dp),
                )
            }
        }
    }
}

@Composable
private fun MachineCard(model: AppModel, m: Machine) {
    val scope = rememberCoroutineScope()
    Box(Modifier.fillMaxWidth().aideCard()) {
        Column(Modifier.padding(16.dp).animateContentSize(tween(Motion.Medium, easing = Motion.EaseOut)), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Row {
                Text(m.machineName, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f))
                Text(
                    when {
                        m.syncing -> t("updating", "更新中")
                        m.online && m.unreachable.isEmpty() -> t("online", "在线")
                        m.online -> t("unreachable", "不可达")
                        else -> t("offline", "离线")
                    },
                    color = if (m.online && m.unreachable.isEmpty()) Positive else MaterialTheme.colorScheme.onSurfaceVariant,
                    style = MaterialTheme.typography.labelLarge,
                )
            }
            val s = m.status
            Text(
                listOfNotNull(
                    s?.platform?.takeIf { it.isNotEmpty() },
                    (s?.version ?: m.version).takeIf { it.isNotEmpty() }?.let { "v$it" },
                    s?.let { t("${it.runningSessions} running", "${it.runningSessions} 个运行中") },
                    if (s?.paused == true) t("taking no new sessions", "暂停接收新会话") else null,
                ).joinToString(" · "),
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            if (m.unreachable.isNotEmpty() && m.online) Text(m.unreachable, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
            ConnectionsBlock(m)
            s?.agents?.filter { it.available && !it.hidden }?.forEach { a -> AgentRow(model, m, a) }
            // Hidden by the owner (SetAgentHidden): named, to show again, as at the end of the desktop's list.
            s?.agents?.filter { it.hidden }?.takeIf { it.isNotEmpty() }?.let { hidden ->
                Text(t("${hidden.size} hidden:", "已隐藏 ${hidden.size} 个："), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                hidden.forEach { a ->
                    TextButton(onClick = { scope.launch { model.setAgentHidden(m.machineId, a.name, false) } }, enabled = m.online) {
                        Text(t("Show ${agentTitle(a.name)}", "显示 ${agentTitle(a.name)}"))
                    }
                }
            }
            if (m.online) TextButton(onClick = { scope.launch { model.recheck(m.machineId) } }) { Text(t("Check agents again", "重新检查 Agent")) }
        }
    }
}

/**
 * This machine's connections (工单 #49), in the same rows and words as the desktop's machine page: what is connected,
 * what is not, what that means, and when it was checked — read without opening anything.
 */
@Composable
private fun ConnectionsBlock(m: Machine) {
    val c = machineConnections(m, Instant.now())
    if (c.rows.isEmpty()) return
    Column(Modifier.fillMaxWidth().padding(top = 8.dp, bottom = 4.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(t("Connections on this machine", "本机连接状态"), style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
        c.rows.forEach { r ->
            Row(verticalAlignment = Alignment.Top) {
                AgentFace(r.agent, 24)
                Spacer(Modifier.width(10.dp))
                Column(Modifier.weight(1f)) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text(r.title, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f))
                        Text(r.text, style = MaterialTheme.typography.labelMedium, color = connColor(r.state))
                    }
                    r.lacks?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                    r.checkedAt?.let { Text(t("Checked ${agoText(it, Instant.now())}", "${agoText(it, Instant.now())}检测"), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.outline) }
                }
            }
        }
    }
}

@Composable
private fun connColor(s: ConnState) = when (s) {
    ConnState.OK -> Positive
    ConnState.PARTIAL -> Warning
    ConnState.MISSING -> MaterialTheme.colorScheme.error
    else -> MaterialTheme.colorScheme.onSurfaceVariant
}

private val whenFormat = DateTimeFormatter.ofLocalizedDateTime(FormatStyle.SHORT).withZone(ZoneId.systemDefault())

/**
 * One of a machine's coding agents, with its pause (SetAgentPaused), as on the desktop: paused, no new session starts
 * with it there, those under way run on, and nothing is removed. Where the machine read when its plan's usage resets
 * (Claude Code), it can be paused until then, to resume by itself.
 */
@Composable
private fun AgentRow(model: AppModel, m: Machine, a: AgentStatus) {
    val scope = rememberCoroutineScope()
    var asking by remember { mutableStateOf(false) }
    val until = pausedUntil(a)
    Row(verticalAlignment = Alignment.CenterVertically) {
        AgentFace(a.name, 28)
        Spacer(Modifier.width(10.dp))
        Column(Modifier.weight(1f)) {
            Text(agentTitle(a.name), style = MaterialTheme.typography.bodyMedium)
            Text(
                listOfNotNull(a.version.takeIf { it.isNotEmpty() }, if (a.loggedIn) t("signed in", "已登录") else t("not signed in", "未登录")).joinToString(" · "),
                style = MaterialTheme.typography.bodySmall, color = if (a.loggedIn) Positive else Warning,
            )
            when {
                a.pause != null && until != null -> Text(t("Paused · resumes ${whenFormat.format(until)}", "已暂停 · ${whenFormat.format(until)} 自动恢复"), style = MaterialTheme.typography.bodySmall, color = Warning)
                a.pause != null -> Text(t("Paused", "已暂停"), style = MaterialTheme.typography.bodySmall, color = Warning)
                else -> instantOrNull(a.autoResumedAt)?.let { Text(t("Last resumed by itself at ${whenFormat.format(it)}", "上次于 ${whenFormat.format(it)} 到点自动恢复"), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
            }
        }
        Spacer(Modifier.width(8.dp))
        if (m.online) Box {
            TextButton(onClick = { asking = true }) { Text(t("Manage", "管理")) }
            DropdownMenu(expanded = asking, onDismissRequest = { asking = false }) {
                fun act(block: suspend () -> Unit) {
                    asking = false
                    scope.launch { block() }
                }
                if (a.pause != null) {
                    DropdownMenuItem(text = { Text(t("Resume", "恢复")) }, onClick = { act { model.setAgentPaused(m.machineId, a.name, false) } })
                } else {
                    DropdownMenuItem(text = { Text(t("Pause until I resume it", "暂停，直到我恢复")) }, onClick = { act { model.setAgentPaused(m.machineId, a.name, true) } })
                    usageResetAt(a, Instant.now())?.let { r ->
                        DropdownMenuItem(text = { Text(t("Pause until usage resets (${whenFormat.format(r)})", "暂停到额度重置（${whenFormat.format(r)}）")) }, onClick = {
                            act { model.setAgentPaused(m.machineId, a.name, true, r.toString()) }
                        })
                    }
                }
                // Hiding is separate from pausing: it only takes the entry out of the list (desktop: Hide from the List).
                DropdownMenuItem(text = { Text(t("Hide from the list", "从列表中隐藏")) }, onClick = { act { model.setAgentHidden(m.machineId, a.name, true) } })
            }
        }
    }
}
