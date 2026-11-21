package io.aide.android.ui

import androidx.compose.animation.core.tween
import io.aide.android.model.onMachine
import io.aide.android.model.Buckets
import androidx.compose.runtime.setValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.material3.Card
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.aide.android.api.Session
import io.aide.android.api.SessionStatus
import io.aide.android.model.AppModel
import io.aide.android.model.SessionLink
import io.aide.android.model.World
import io.aide.android.model.agentTitle
import io.aide.android.model.displayTitle
import io.aide.android.model.isLive
import io.aide.android.model.needsYou
import io.aide.android.model.quietText
import io.aide.android.model.stateLine
import kotlinx.coroutines.launch

/** Every machine's sessions: those that need you, those at work, the rest. */
@Composable
fun SessionsTab(model: AppModel, openDSH: () -> Unit, open: (String) -> Unit) {
    val world by model.world.collectAsState()
    // Several machines: the list of one, or all (the owner's rule, 2026-11-21); the buckets stay as they are.
    var picked by rememberSaveable { mutableStateOf<String?>(null) }
    val ids = world.machines.map { it.machineId }
    val all = world.buckets
    val on = { l: List<Session> -> onMachine(l, { it.machineId }, picked, ids) }
    val b = Buckets(on(all.needsYou), on(all.unseen), on(all.inProgress), on(all.recent))
    val scope = rememberCoroutineScope()
    Column(Modifier.fillMaxSize()) {
        PageTitle(t("Sessions", "会话"))
        SyncLine(model)
        MachineFilter(world, picked) { picked = it }
        LazyColumn(Modifier.fillMaxSize()) {
            // DeepSeek Harness's own sessions, on a machine that has it.
            if (world.machines.any { m -> m.status?.agents.orEmpty().any { it.name == "deepseek" } }) {
                item(key = "dsh") {
                    TextButton(onClick = openDSH, modifier = Modifier.padding(horizontal = 8.dp)) { Text(t("DeepSeek’s own sessions ›", "DeepSeek 自己的会话 ›")) }
                }
            }
            section(t("Needs You", "需要你处理"), b.needsYou, world, open, Warning)
            section(t("New Results", "有新结果未看"), b.unseen, world, open, null)
            section(t("In Progress", "进行中"), b.inProgress, world, open, null)
            section(t("Recent", "最近"), b.recent, world, open, Color.Gray)
            if (world.sessions.isEmpty() && world.watched) {
                item { Text(t("No sessions yet.", "还没有会话。"), Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
            }
            if (world.next != null) {
                item {
                    TextButton(onClick = { scope.launch { model.loadOlder() } }, modifier = Modifier.padding(8.dp)) { Text(t("Load older sessions", "加载更早的会话")) }
                }
            }
        }
    }
}

/** The sessions waiting for you: open one to answer it there. */
@Composable
fun NeedsYouTab(model: AppModel, open: (String) -> Unit) {
    val world by model.world.collectAsState()
    val sessions = world.buckets.needsYou
    Column(Modifier.fillMaxSize()) {
        PageTitle(t("Needs you", "需要你"))
        SyncLine(model)
        LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            if (sessions.isEmpty()) {
                item { Text(t("Nothing waits for you.", "没有等你处理的事。"), color = MaterialTheme.colorScheme.onSurfaceVariant) }
            } else {
                item(key = "h-sessions") { SectionHeader(t("Tasks waiting for you", "等你处理的任务") + " · ${sessions.size}") }
            }
            items(sessions, key = { it.sessionId }) { s ->
                Card(Modifier.fillMaxWidth()) { SessionRow(s, world) { open(s.sessionId) } }
            }
        }
    }
}

private fun androidx.compose.foundation.lazy.LazyListScope.section(title: String, sessions: List<Session>, world: World, open: (String) -> Unit, color: Color?) {
    if (sessions.isEmpty()) return
    item(key = "h-$title") { SectionHeader("$title · ${sessions.size}", color ?: MaterialTheme.colorScheme.primary) }
    items(sessions, key = { it.sessionId }) { s ->
        // A session moving between groups, or arriving, eases into place rather than jumping (Motion, as the desktop's).
        Column(Modifier.animateItem(fadeInSpec = tween(Motion.Medium), placementSpec = tween(Motion.Medium, easing = Motion.EaseOut), fadeOutSpec = tween(Motion.Short))) {
            SessionRow(s, world) { open(s.sessionId) }
            HorizontalDivider(Modifier.padding(start = 16.dp))
        }
    }
}

@Composable
fun SessionRow(s: Session, world: World, onClick: () -> Unit) {
    Row(Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
        // Waiting for the owner: a bar at the row's start, as on the desktop.
        if (needsYou(s)) {
            Box(Modifier.width(3.dp).height(36.dp).background(Warning, RoundedCornerShape(2.dp)))
            Spacer(Modifier.width(9.dp))
        }
        StatusDot(s)
        Spacer(Modifier.width(12.dp))
        Column(Modifier.weight(1f)) {
            Text(displayTitle(s), maxLines = 1, overflow = TextOverflow.Ellipsis, fontWeight = if (needsYou(s)) FontWeight.SemiBold else FontWeight.Normal)
            Text(
                "${world.senderName(s)} → ${agentTitle(s.agent)} · ${s.projectId}",
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis,
            )
            Text(
                listOf(world.machineName(s.machineId), statusLine(s, world), relativeTime(s.updatedAt)).filter { it.isNotEmpty() }.joinToString(" · "),
                style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1,
            )
        }
    }
}

@Composable
fun StatusDot(s: Session) {
    val color = when {
        needsYou(s) -> Warning
        isLive(s) -> MaterialTheme.colorScheme.primary
        s.status == SessionStatus.CRASHED || s.progress?.lastFailure != null -> MaterialTheme.colorScheme.error
        else -> Color.Gray.copy(alpha = 0.5f)
    }
    Box(Modifier.size(10.dp).background(color, CircleShape))
}

/**
 * A session's state as rows and pages say it, both parts (docs/session-states.md): its status, or, while its
 * machine is not live, what was last known and since when; a running turn quiet past the stall limit says so.
 */
fun statusLine(s: Session, world: World): String {
    if (s.status != SessionStatus.UNKNOWN && s.status != SessionStatus.RUNNING) return statusText(s)
    val v = world.view(s)
    return when {
        s.status == SessionStatus.UNKNOWN -> stateLine(v)
        v.link == SessionLink.STALLED -> "${statusText(s)} · ${quietText(v)}"
        else -> statusText(s)
    }
}

fun statusText(s: Session): String = when (s.status) {
    SessionStatus.WAITING_INPUT -> t("asks you", "在问你")
    SessionStatus.RUNNING -> s.progress?.currentTool?.takeIf { it.isNotEmpty() }?.let { t("running $it", "正在运行 $it") } ?: t("working", "工作中")
    SessionStatus.PREPARING -> t("preparing", "准备中")
    SessionStatus.QUEUED -> t("queued", "排队中")
    SessionStatus.CRASHED -> t("stopped", "已中断")
    SessionStatus.ARCHIVED -> t("archived", "已归档")
    SessionStatus.IDLE -> if (s.progress?.lastFailure != null) t("failed", "失败") else t("done", "完成")
    SessionStatus.UNKNOWN -> t("state unknown", "状态未知")
    else -> ""
}

@Composable
fun PageTitle(text: String) {
    Text(text, style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.SemiBold, modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 16.dp))
}

@Composable
fun SectionHeader(text: String, color: Color = MaterialTheme.colorScheme.primary) {
    Text(
        text, style = MaterialTheme.typography.labelLarge, color = color,
        modifier = Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 16.dp, bottom = 4.dp),
    )
}

@Composable
fun Labeled(label: String, value: String) {
    Row(Modifier.fillMaxWidth().padding(vertical = 2.dp), horizontalArrangement = Arrangement.SpaceBetween) {
        Text(label, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodyMedium)
        Spacer(Modifier.width(12.dp))
        Text(value, style = MaterialTheme.typography.bodyMedium, maxLines = 2, overflow = TextOverflow.Ellipsis)
    }
}
