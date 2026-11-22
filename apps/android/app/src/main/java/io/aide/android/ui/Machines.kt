package io.aide.android.ui

import androidx.compose.animation.core.tween
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
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
import io.aide.android.model.machineAgents
import io.aide.android.model.partTitle
import io.aide.android.model.pausedUntil
import io.aide.android.model.usageResetAt
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/**
 * The machines and their coding agents (the devices and this phone are in Settings). Each machine is one card with one
 * list of its agents (工单 #53): an agent once, by one name, with one state, and what can be done with it on the same
 * entry. The card is drawn as rows of the list, its header, each agent, its foot, so scrolling composes one row at a
 * time rather than a whole machine at once (工单 #55).
 */
@Composable
fun MachinesTab(model: AppModel) {
    val world by model.world.collectAsState()
    // Findings too old to show as now are checked again while the page is open (工单 #53), as the desktop does.
    LaunchedEffect(Unit) {
        while (true) {
            model.checkStaleAgents(Instant.now())
            delay(60_000)
        }
    }
    val list = rememberLazyListState()
    Column(Modifier.fillMaxSize()) {
        val online = world.machines.count { it.online }
        PageTitle(t("Machines", "机器"), if (world.machines.isEmpty()) null else t("$online of ${world.machines.size} online", "${world.machines.size} 台中 $online 台在线"))
        SyncLine(model)
        CompositionLocalProvider(LocalScrolling provides list.isScrollInProgress) {
            LazyColumn(Modifier.fillMaxSize(), state = list, contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 4.dp, bottom = 24.dp)) {
                if (world.machines.isEmpty() && world.watched) item { Text(t("No machines yet.", "还没有机器。"), color = MaterialTheme.colorScheme.onSurfaceVariant) }
                // The overview first (BotArk reference §4): how many are online, how many agents work, how much runs.
                if (world.machines.isNotEmpty()) item(key = "overview") {
                    val entries = world.machines.flatMap { machineAgents(it, Instant.now()) }
                    Row(Modifier.fillMaxWidth().padding(top = 4.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        StatTile(t("Online", "在线机器"), "$online", Modifier.weight(1f), of = "${world.machines.size}", tone = if (online > 0) Tone.Good else Tone.Quiet)
                        StatTile(t("Agents ready", "可用 Agent"), "${entries.count { it.state == ConnState.OK }}", Modifier.weight(1f), of = "${entries.size}")
                        StatTile(t("Running", "运行中"), "${world.machines.sumOf { it.status?.runningSessions ?: 0 }}", Modifier.weight(1f), tone = Tone.Live)
                    }
                }
                world.machines.forEach { m ->
                    val entries = machineAgents(m, Instant.now()).filter { it.status?.hidden != true }
                    val count = entries.size + 2
                    item(key = "h-${m.machineId}", contentType = "machine-head") {
                        Box(Modifier.animateItem(fadeInSpec = tween(Motion.Medium), placementSpec = tween(Motion.Medium, easing = Motion.EaseOut)).padding(top = 12.dp)) {
                            MachineHead(m, Modifier.groupRow(0, count))
                        }
                    }
                    entries.forEachIndexed { i, e ->
                        item(key = "a-${m.machineId}-${e.agent}", contentType = "agent") {
                            Box(Modifier.animateItem(fadeInSpec = tween(Motion.Medium), placementSpec = tween(Motion.Medium, easing = Motion.EaseOut)).groupRow(i + 1, count).padding(horizontal = 12.dp, vertical = 4.dp)) {
                                AgentEntryView(model, m, e)
                            }
                        }
                    }
                    item(key = "f-${m.machineId}", contentType = "machine-foot") {
                        Box(Modifier.animateItem(fadeInSpec = tween(Motion.Medium), placementSpec = tween(Motion.Medium, easing = Motion.EaseOut)).groupRow(count - 1, count)) {
                            MachineFoot(model, m, empty = entries.isEmpty())
                        }
                    }
                }
                // Coding agents are connected where they run: on a computer, through the desktop app's guide.
                item(key = "how") { SectionHeader(t("Connecting a coding agent", "连接编码 Agent")) }
                item(key = "how-text") {
                    Text(
                        t(
                            "Coding agents run on your computers, not on this phone. Install DSH, the Claude app (open its Code tab once) or Codex on a computer, then open Aide there: Settings › Connect walks you through each one, says whether it is detected and runs a test. Each computer above lists the agents it detected, and their tasks show under Sessions here.",
                            "编码 Agent 在你的电脑上运行，不在这台手机上。先在电脑上装好 DSH、Claude 应用（打开一次它的 Code 标签页）或 Codex，再打开那台电脑上的 Aide：设置 › 连接 会一步步带你连接、告诉你是否检测到，并运行测试。上面每台电脑都列出了它检测到的 Agent，它们的任务会显示在这里的「会话」里。",
                        ),
                        style = MaterialTheme.typography.bodySmall, color = aide.faint, modifier = Modifier.padding(horizontal = 4.dp),
                    )
                }
            }
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun MachineHead(m: Machine, modifier: Modifier) {
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
private fun MachineFoot(model: AppModel, m: Machine, empty: Boolean) {
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
 * One coding agent on a machine (工单 #53): its face, its one name and its one state; under it, quietly, its parts
 * (Claude Desktop and the command line, DSH's bridge plugin) and its account (DSH's DeepSeek); then its pause and
 * Manage, as on the desktop: paused, no new session starts with it there, those under way run on, nothing is removed.
 */
@Composable
private fun AgentEntryView(model: AppModel, m: Machine, e: AgentEntry) {
    val scope = rememberCoroutineScope()
    val now = Instant.now()
    Column(Modifier.fillMaxWidth().aideInset().padding(12.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        // A roster row (BotArk reference §1): face with its state on the corner, the one name with the state as a pill
        // beside it, and one small line under it (its version, DSH's models).
        val word = e.text.substringBefore(" · ")
        val rest = e.text.substringAfter(" · ", "")
        Row(verticalAlignment = Alignment.CenterVertically) {
            FaceWithDot(e.agent, 40, toneColor(connTone(e.state)))
            HSpace(12)
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(e.title, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold, maxLines = 1)
                    HSpace(8)
                    StatusPill(word, connTone(e.state))
                }
                listOfNotNull(rest.ifEmpty { null }?.let { "v$it" }, e.account).joinToString(" · ").ifEmpty { null }?.let {
                    Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2)
                }
            }
        }
        e.lacks?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
        // Its parts: what each is, in plain words, without a second state beside the one above.
        if (e.parts.isNotEmpty()) {
            Column(Modifier.fillMaxWidth().padding(start = 48.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                e.parts.forEach { p ->
                    Row {
                        Text(partTitle(p), style = MaterialTheme.typography.bodySmall, color = aide.faint, modifier = Modifier.weight(0.42f), maxLines = 1)
                        Text(p.was?.let { t("Last: $it", "上次：$it") } ?: p.text, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurface, modifier = Modifier.weight(0.58f))
                    }
                    if (p.was == null) p.lacks?.let { Text(it, style = MaterialTheme.typography.labelSmall, color = aide.faint) }
                }
            }
        }
        e.status?.let { a -> PauseLine(a) }
        // Signed in on that computer, in a browser that opens there: the phone starts it and shows how it goes (工单 #51).
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
