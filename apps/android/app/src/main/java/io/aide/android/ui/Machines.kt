package io.aide.android.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.aide.android.api.AgentStatus
import io.aide.android.api.Machine
import io.aide.android.model.AgentEntry
import io.aide.android.model.AppModel
import io.aide.android.model.ConnState
import io.aide.android.model.agentTitle
import io.aide.android.model.agoText
import io.aide.android.model.instantOrNull
import io.aide.android.model.partTitle
import io.aide.android.model.pausedUntil
import io.aide.android.model.usageResetAt
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import kotlinx.coroutines.launch

// A machine as a device (工单 #57: the roster's machine group heads and foot): online, version, what runs, checking
// its agents again and showing hidden ones. Its agents are bots, listed and opened in the roster (Bots.kt); what the
// machine page managed of an agent is its bot's workbench (AgentWorkbench), not a second list here.

@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun MachineHead(m: Machine, modifier: Modifier) {
    Column(modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 16.dp, bottom = 10.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            MachineMark(m.machineName, m.online && m.unreachable.isEmpty())
            HSpace(12)
            Column(Modifier.weight(1f)) {
                Text(m.machineName, style = MaterialTheme.typography.titleLarge, maxLines = 1, overflow = TextOverflow.Ellipsis)
                m.status?.platform?.takeIf { it.isNotEmpty() }?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = aide.faint) }
            }
            when {
                m.syncing -> StatusPill(t("Updating", "更新中"), Tone.Live)
                m.online && m.unreachable.isEmpty() -> StatusPill(t("Online", "在线"), Tone.Good)
                m.online -> StatusPill(t("Unreachable", "不可达"), Tone.Warn)
                else -> StatusPill(t("Offline", "离线"), Tone.Quiet)
            }
        }
        val s = m.status
        FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            s?.let { FactChip(t("${it.runningSessions} running", "${it.runningSessions} 个运行中")) }
            s?.maxSessions?.takeIf { it > 0 }?.let { FactChip(t("up to $it", "上限 $it 个")) }
            (s?.version ?: m.version).takeIf { it.isNotEmpty() }?.let { FactChip("v$it") }
            if (s?.paused == true) FactChip(t("taking no new sessions", "暂停接收新会话"))
        }
        if (m.unreachable.isNotEmpty() && m.online) Text(m.unreachable, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
    }
}

@Composable
internal fun MachineFoot(model: AppModel, m: Machine, empty: Boolean) {
    val scope = rememberCoroutineScope()
    Column(Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 6.dp, bottom = 14.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        if (empty) Text(t("No coding agent found here.", "这台机器上没有检测到编码 Agent。"), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        // Hidden by the owner (SetAgentHidden): named, to show again, as at the end of the desktop's list.
        m.status?.agents?.filter { it.hidden }?.takeIf { it.isNotEmpty() }?.let { hidden ->
            Text(t("${hidden.size} hidden:", "已隐藏 ${hidden.size} 个："), style = MaterialTheme.typography.bodySmall, color = aide.faint)
            hidden.forEach { a ->
                TextButton(onClick = { scope.launch { model.setAgentHidden(m.machineId, a.name, false) } }, enabled = m.online) {
                    Text(t("Show ${agentTitle(a.name)}", "显示 ${agentTitle(a.name)}"))
                }
            }
        }
        if (m.online) OutlinedButton(onClick = { scope.launch { model.recheck(m.machineId) } }, contentPadding = PaddingValues(horizontal = 14.dp, vertical = 4.dp)) {
            Text(t("Check agents again", "重新检查 Agent"), style = MaterialTheme.typography.labelLarge)
        }
    }
}

fun connTone(s: ConnState): Tone = when (s) {
    ConnState.OK -> Tone.Good
    ConnState.PARTIAL -> Tone.Warn
    ConnState.MISSING -> Tone.Bad
    ConnState.CHECKING -> Tone.Live
    ConnState.UNKNOWN -> Tone.Quiet
}

private val whenFormat = DateTimeFormatter.ofLocalizedDateTime(FormatStyle.SHORT).withZone(ZoneId.systemDefault())

/**
 * What a coding agent's bot page keeps of it (工单 #57, from the machine page's entry, 工单 #53): what its state means,
 * its parts (Claude Desktop and the command line, DSH's bridge plugin) and its account (DSH's DeepSeek), said quietly
 * without a second state; signing it in from here (工单 #51); its pause and Manage — paused, no new session starts
 * with it there, those under way run on, nothing is removed.
 */
@Composable
internal fun AgentWorkbench(model: AppModel, m: Machine, e: AgentEntry, openSession: (String) -> Unit = {}) {
    val scope = rememberCoroutineScope()
    val now = Instant.now()
    Column(Modifier.fillMaxWidth().aideInset().padding(12.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        val rest = e.text.substringAfter(" · ", "")
        listOfNotNull(rest.ifEmpty { null }?.let { "v$it" }, e.account).joinToString(" · ").ifEmpty { null }?.let {
            Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2)
        }
        // What its state means is said once, under its name at the top of its page (BotState.line).
        if (e.parts.isNotEmpty()) {
            Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                e.parts.forEach { p ->
                    Row {
                        Text(partTitle(p), style = MaterialTheme.typography.bodySmall, color = aide.faint, modifier = Modifier.weight(0.42f), maxLines = 1)
                        Text(p.was?.let { t("Last: $it", "上次：$it") } ?: p.text, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurface, modifier = Modifier.weight(0.58f))
                    }
                    if (p.was == null) p.lacks?.let { Text(it, style = MaterialTheme.typography.labelSmall, color = aide.faint) }
                    // The bridge plugin (工单 #59): handed to DSH with one press; its task to open while it works or after.
                    if (p.was == null && (p.install || p.installSession.isNotEmpty())) Row(verticalAlignment = Alignment.CenterVertically) {
                        if (p.install && m.online && m.unreachable.isEmpty()) TextButton(onClick = { scope.launch { model.installDSHBridge(m.machineId) } }) {
                            Text(if (p.installSession.isNotEmpty()) t("Have DSH try again", "再让 DSH 试一次") else t("Have DSH install it", "让 DSH 装好它"))
                        }
                        if (p.installSession.isNotEmpty()) TextButton(onClick = { openSession(p.installSession) }) { Text(t("Open the install task", "看代装任务")) }
                    }
                }
            }
        }
        e.status?.let { a -> PauseLine(a) }
        if (e.login && m.online && m.unreachable.isEmpty()) {
            if (e.state == ConnState.CHECKING) {
                TextButton(onClick = { scope.launch { model.startAgentLogin(m.machineId, e.agent, cancel = true) } }) { Text(t("Cancel sign-in", "取消登录")) }
            } else {
                TextButton(onClick = { scope.launch { model.startAgentLogin(m.machineId, e.agent) } }) { Text(t("Sign in on ${m.machineName}", "在 ${m.machineName} 上登录")) }
                Text(t("A browser opens on ${m.machineName}; you sign in there. Aide never handles the account or the token.", "会在 ${m.machineName} 上打开浏览器，你在那里登录；Aide 不经手账号和令牌。"), style = MaterialTheme.typography.labelSmall, color = aide.faint)
            }
        }
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(e.checkedAt?.let { t("Checked ${agoText(it, now)}", "${agoText(it, now)}检测") } ?: "", style = MaterialTheme.typography.labelSmall, color = aide.faint, modifier = Modifier.weight(1f))
            if (m.online) TextButton(onClick = { scope.launch { model.recheck(m.machineId) } }) { Text(t("Check again", "重新检测")) }
            e.status?.let { a -> if (m.online) ManageMenu(model, m, a) }
        }
    }
}

@Composable
private fun PauseLine(a: AgentStatus) {
    val until = pausedUntil(a)
    when {
        a.pause != null && until != null -> Text(t("Paused · resumes ${whenFormat.format(until)}", "已暂停 · ${whenFormat.format(until)} 自动恢复"), style = MaterialTheme.typography.bodySmall, color = Warning)
        a.pause != null -> Text(t("Paused", "已暂停"), style = MaterialTheme.typography.bodySmall, color = Warning)
        else -> instantOrNull(a.autoResumedAt)?.let { Text(t("Last resumed by itself at ${whenFormat.format(it)}", "上次于 ${whenFormat.format(it)} 到点自动恢复"), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
    }
}

/** Pause (until resumed, or until Claude's usage resets where the machine read when), resume, or hide it. */
@Composable
private fun ManageMenu(model: AppModel, m: Machine, a: AgentStatus) {
    val scope = rememberCoroutineScope()
    var asking by remember { mutableStateOf(false) }
    Box {
        TextButton(onClick = { asking = true }, contentPadding = PaddingValues(horizontal = 10.dp, vertical = 0.dp)) { Text(t("Manage", "管理")) }
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
