package io.aide.android.ui

import androidx.compose.foundation.border

import androidx.compose.ui.graphics.graphicsLayer

import androidx.compose.animation.core.animateFloat

import androidx.compose.ui.unit.sp
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
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
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.layout.offset
import androidx.compose.runtime.CompositionLocalProvider
import io.aide.android.model.manyMachines
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
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
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.SizeTransform
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.material3.pulltorefresh.rememberPullToRefreshState
import androidx.compose.runtime.derivedStateOf
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.IntOffset
import androidx.compose.animation.core.VisibilityThreshold

/** Every machine's sessions: those that need you, those at work, the rest. */
@OptIn(androidx.compose.material3.ExperimentalMaterial3Api::class)
@Composable
fun SessionsTab(model: AppModel, open: (String) -> Unit) {
    val world by model.world.collectAsState()
    // Several machines: the list of one, or all (the owner's rule, 2026-11-21); the buckets stay as they are.
    var picked by rememberSaveable { mutableStateOf<String?>(null) }
    val ids = world.machines.map { it.machineId }
    val all = world.buckets
    val on = { l: List<Session> -> onMachine(l, { it.machineId }, picked, ids) }
    val b = Buckets(on(all.needsYou), on(all.unseen), on(all.inProgress), on(all.recent))
    val scope = rememberCoroutineScope()
    val list = rememberLazyListState()
    Column(Modifier.fillMaxSize()) {
        val working = all.inProgress.size
        // Fluent Home tab (工单 #106): overview, needs you, and live task feed on the default opening screen.
        PageTitle(t("Home", "首页"), collapsed = list.collapsed(), summary = listOfNotNull(
            all.needsYou.size.takeIf { it > 0 }?.let { t("$it need you", "$it 个等你处理") },
            t("$working at work", "$working 个进行中"),
        ).joinToString(" · "))
        SyncLine(model)
        MachineFilter(world, picked) { picked = it }
        // Pulled down: read again now (the Watch keeps the list current otherwise).
        var refreshing by remember { mutableStateOf(false) }
        val pull = rememberPullToRefreshState()
        PullToRefreshBox(isRefreshing = refreshing, onRefresh = { scope.launch { refreshing = true; model.refresh(); refreshing = false } }, modifier = Modifier.fillMaxSize(), state = pull, indicator = {
            val shown by remember { derivedStateOf { refreshing || pull.distanceFraction > 0f } }
            if (shown) ActivityIndicator(Modifier.align(Alignment.TopCenter).graphicsLayer { translationY = pull.distanceFraction.coerceAtMost(1f) * 56.dp.toPx() - 8.dp.toPx(); alpha = if (refreshing) 1f else pull.distanceFraction.coerceAtMost(1f) }, size = 24.dp)
        }) {
        CompositionLocalProvider(LocalScrolling provides list.isScrollInProgress) {
        LazyColumn(Modifier.fillMaxSize(), state = list, contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 96.dp)) {
            item(key = "overview") { Column {
                SectionHeader(t("Overview", "概览"))
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    StatTile(t("Need you", "等你处理"), "${b.needsYou.size}", Modifier.weight(1f), tone = if (b.needsYou.isEmpty()) Tone.Quiet else Tone.Warn)
                    StatTile(t("At work", "进行中"), "${b.inProgress.size}", Modifier.weight(1f), tone = if (b.inProgress.isEmpty()) Tone.Quiet else Tone.Live)
                    StatTile(t("New results", "新结果"), "${b.unseen.size}", Modifier.weight(1f))
                }
            } }
            section(t("Needs You", "需要你处理"), b.needsYou, world, open, Tone.Warn)
            section(t("New Results", "有新结果未看"), b.unseen, world, open, Tone.Live)
            section(t("In Progress", "进行中"), b.inProgress, world, open, Tone.Live)
            section(t("Recent", "最近"), b.recent, world, open, Tone.Quiet)
            // A year of tokens by coding agent or by machine, last as on the desktop's overview (工单 #142).
            item(key = "usage") { UsageCard(model) }
            if (world.sessions.isEmpty() && world.watched) {
                item { Text(t("No sessions yet.", "还没有会话。"), Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
            }
            if (world.next != null) {
                item {
                    PlainButton(onClick = { scope.launch { model.loadOlder() } }, modifier = Modifier.padding(8.dp)) { Text(t("Load older sessions", "加载更早的会话")) }
                }
            }
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
    val list = rememberLazyListState()
    Column(Modifier.fillMaxSize()) {
        PageTitle(t("Needs you", "需要你"), if (sessions.isEmpty()) null else t("${sessions.size} waiting for an answer", "${sessions.size} 个在等你回答"), collapsed = list.collapsed())
        SyncLine(model)
        CompositionLocalProvider(LocalScrolling provides list.isScrollInProgress) {
        LazyColumn(Modifier.fillMaxSize(), state = list, contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 24.dp)) {
            if (sessions.isEmpty()) {
                item { Text(t("Nothing waits for you.", "没有等你处理的事。"), Modifier.padding(top = 16.dp, start = 16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
            } else {
                section(t("Tasks waiting for you", "等你处理的任务"), sessions, world, open, Tone.Warn)
            }
        }
        }
    }
}

/** A group of sessions: a heading with its count, then its rows as one card (each row a list item of its own). */
private fun androidx.compose.foundation.lazy.LazyListScope.section(title: String, sessions: List<Session>, world: World, open: (String) -> Unit, tone: Tone) {
    if (sessions.isEmpty()) return
    item(key = "h-$title", contentType = "heading") { SectionHeader(title, count = sessions.size, tone = tone) }
    itemsIndexed(sessions, key = { _, s -> s.sessionId }, contentType = { _, _ -> "session" }) { i, s ->
        Column(Modifier.animateItem(fadeInSpec = tween(Motion.Medium), placementSpec = Motion.spring(IntOffset.VisibilityThreshold), fadeOutSpec = tween(Motion.Short)).groupRow(i, sessions.size, inset = 68.dp)) {
            SessionRow(s, world) { open(s.sessionId) }
        }
    }
}

@Composable
fun SessionRow(s: Session, world: World, onClick: () -> Unit) {
    val waits = needsYou(s)
    Row(Modifier.fillMaxWidth().rowPress(onClick).padding(start = 16.dp, end = 16.dp, top = 11.dp, bottom = 11.dp), verticalAlignment = Alignment.Top) {
        Box {
            AgentFace(s.agent, 40)
            Box(Modifier.align(Alignment.BottomEnd).offset(x = 3.dp, y = 3.dp)) { StatusDot(s) }
        }
        HSpace(12)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(displayTitle(s), style = MaterialTheme.typography.titleMedium, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                HSpace(8)
                Text(relativeTime(s.updatedAt), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
            }
            Text(
                "${world.senderName(s)} → ${agentTitle(s.agent)} · ${world.projectTitle(s.machineId, s.projectId)}",
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis,
            )
            Row(Modifier.padding(top = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                StatusPill(statusLine(s, world), sessionTone(s))
                if (manyMachines(world.machines.size)) {
                    HSpace(8)
                    Text(world.machineName(s.machineId), style = MaterialTheme.typography.labelSmall, color = aide.faint, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
            }
        }
        if (waits) {
            HSpace(8)
            Box(Modifier.padding(top = 4.dp).width(3.dp).height(32.dp).background(aide.orange, CircleShape))
        }
    }
}

/** The weight a session's state is said with. */
fun sessionTone(s: Session): Tone = when {
    needsYou(s) -> Tone.Warn
    isLive(s) -> Tone.Live
    s.status == SessionStatus.CRASHED || s.progress?.lastFailure != null -> Tone.Bad
    s.status == SessionStatus.IDLE -> Tone.Good
    else -> Tone.Quiet
}

/**
 * A session's state as a dot; while it works, a ring grows out of it and fades (the desktop's running ping, 工单 #47):
 * motion only while something runs, none when the system asks for less.
 */
@Composable
fun StatusDot(s: Session) {
    val color = when {
        needsYou(s) -> aide.orange
        isLive(s) -> aide.accent
        s.status == SessionStatus.CRASHED || s.progress?.lastFailure != null -> aide.red
        else -> aide.disabled
    }
    val ctx = androidx.compose.ui.platform.LocalContext.current
    // Read once, not on every composition (a call into the system's settings, 工单 #55).
    val still = remember(ctx) { android.provider.Settings.Global.getFloat(ctx.contentResolver, android.provider.Settings.Global.ANIMATOR_DURATION_SCALE, 1f) == 0f }
    // While the list scrolls, the ring stops: nothing moves by itself then (工单 #55 §4).
    val scrolling = LocalScrolling.current
    Box(Modifier.size(12.dp), contentAlignment = Alignment.Center) {
        if (isLive(s) && !still && !scrolling) {
            val ping by androidx.compose.animation.core.rememberInfiniteTransition(label = "live").animateFloat(
                0f, 1f, androidx.compose.animation.core.infiniteRepeatable(androidx.compose.animation.core.tween(2000, easing = Motion.EaseOut)), label = "ping",
            )
            Box(Modifier.size(10.dp).graphicsLayer { scaleX = 1f + ping * 1.3f; scaleY = 1f + ping * 1.3f; alpha = 0.45f * (1f - ping) }.background(color, CircleShape))
        }
        Box(Modifier.size(12.dp).background(aide.card, CircleShape).padding(2.dp).background(color, CircleShape))
    }
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

/** Whether a tab's list has scrolled past its large title: the title then collapses into the bar. */
@Composable
fun LazyListState.collapsed(): Boolean {
    val past by remember(this) { derivedStateOf { firstVisibleItemIndex > 0 || firstVisibleItemScrollOffset > 24 } }
    return past
}

/**
 * A tab's title as iOS's large title (HIG Navigation bars, "large titles"): Large Title 34 bold over one quiet line of
 * what is on it now; scrolled, it collapses into a 44 bar with the title centred in Headline and a hairline under it.
 */
@Composable
fun PageTitle(text: String, summary: String? = null, collapsed: Boolean = false) {
    AnimatedContent(
        collapsed,
        transitionSpec = { (fadeIn(tween(Motion.Short)) togetherWith fadeOut(tween(Motion.Short))) using SizeTransform(clip = false) { _, _ -> Motion.snappy() } },
        label = "title",
    ) { small ->
        if (small) Column(Modifier.fillMaxWidth().background(aide.bar)) {
            Text(text, style = MaterialTheme.typography.titleMedium, textAlign = TextAlign.Center, maxLines = 1, modifier = Modifier.fillMaxWidth().padding(vertical = 11.dp))
            Hairline()
        } else Column(Modifier.fillMaxWidth().padding(start = 20.dp, end = 20.dp, top = 12.dp, bottom = 6.dp)) {
            Text(text, style = MaterialTheme.typography.headlineLarge)
            summary?.takeIf { it.isNotEmpty() }?.let { Text(it, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant) }
        }
    }
}

/**
 * An inset group's header (HIG Lists and tables): Footnote in the secondary label colour above the card, its words in
 * line with the rows' words, its count beside it.
 */
@Composable
fun SectionHeader(text: String, color: Color = MaterialTheme.colorScheme.onSurfaceVariant, count: Int? = null, tone: Tone = Tone.Quiet) {
    Row(Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 22.dp, bottom = 7.dp), verticalAlignment = Alignment.CenterVertically) {
        Text(text, style = MaterialTheme.typography.bodySmall, color = color)
        count?.let {
            HSpace(6)
            CountBadge(it, tone)
        }
    }
}

@Composable
fun Labeled(label: String, value: String) {
    Row(Modifier.fillMaxWidth().padding(vertical = 2.dp), horizontalArrangement = Arrangement.SpaceBetween) {
        Text(label, style = MaterialTheme.typography.bodyMedium)
        Spacer(Modifier.width(12.dp))
        Text(value, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2, overflow = TextOverflow.Ellipsis, textAlign = TextAlign.End)
    }
}
