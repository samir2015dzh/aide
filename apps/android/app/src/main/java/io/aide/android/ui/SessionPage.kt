package io.aide.android.ui

import androidx.compose.ui.draw.rotate
import androidx.compose.ui.draw.clip
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.size
import androidx.compose.animation.shrinkVertically
import androidx.compose.animation.fadeOut
import androidx.compose.animation.fadeIn
import androidx.compose.animation.expandVertically
import androidx.compose.animation.core.tween
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.unit.sp
import io.aide.android.api.AttachmentInfo
import io.aide.android.model.AttachKind
import io.aide.android.model.PendingFile
import io.aide.android.model.SentPictures
import io.aide.android.model.attachKind
import io.aide.android.model.kindWord
import io.aide.android.model.sizeText
import io.aide.android.model.OutboxCodec
import io.aide.android.model.OutboxMessage
import io.aide.android.model.OutboxStatus
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.IntSize
import androidx.compose.animation.core.VisibilityThreshold
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.material.icons.automirrored.filled.List
import androidx.compose.material.icons.filled.Settings
import androidx.compose.runtime.saveable.rememberSaveable
import io.aide.android.api.Question
import io.aide.android.api.title
import io.aide.android.api.Session
import io.aide.android.model.AppModel
import io.aide.android.model.Item
import io.aide.android.model.SessionFeed
import io.aide.android.model.agentTitle
import io.aide.android.model.failureAdvice
import io.aide.android.model.failureHeadline
import io.aide.android.model.toldApart
import io.aide.android.model.buildAnswers
import io.aide.android.model.choose
import io.aide.android.model.codingBotState
import io.aide.android.model.displayTitle
import io.aide.android.model.isLive
import io.aide.android.model.ConvParams
import io.aide.android.model.listedModel
import io.aide.android.model.lostSpanText
import io.aide.android.model.modelTitle
import io.aide.android.model.openedNews
import io.aide.android.model.sessionsByProject
import kotlinx.coroutines.launch
import java.time.Instant
import java.util.UUID

/**
 * A session's whole conversation, live, with the agent's question to answer and a way to interrupt it; the owner's
 * own goes on from the box at its foot (工单 #91/#92). One tap away, the sessions to switch to, by project; its
 * settings in a sheet of their own (工单 #90).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SessionPage(model: AppModel, id: String, back: () -> Unit, open: (String) -> Unit = {}) {
    val world by model.world.collectAsState()
    val s = world.sessions[id]?.let(world::shown)
    val names = remember(world.clients) { world.clients.associate { it.clientId to it.label } }
    val scope = rememberCoroutineScope()
    val feed = remember(id) { SessionFeed(model, id) }
    DisposableEffect(feed) {
        val job = feed.start(scope)
        onDispose { job.cancel() }
    }
    val items by feed.items.collectAsState()
    val loading by feed.loading.collectAsState()
    val list = rememberLazyListState()
    var switching by remember { mutableStateOf(false) }
    var settings by remember { mutableStateOf(false) }
    // In front of the owner: what is new here is seen, on every device.
    val news = s?.let(::openedNews).orEmpty()
    val front by model.foreground.collectAsState()
    LaunchedEffect(id, news, front) {
        if (front && news.isNotEmpty()) world.sessions[id]?.let { model.markSeen(it) }
    }
    LaunchedEffect(items.size, items.lastOrNull()) {
        // Follow the end while it is in view (or on first load).
        val last = list.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: -1
        // The first time, straight to the end; after, a message arriving while the owner is at the end eases into view.
        if (items.isNotEmpty() && last < 0) list.scrollToItem(items.size - 1)
        else if (items.isNotEmpty() && last >= items.size - 3) list.animateScrollToItem(items.size - 1)
    }
    Scaffold(
        snackbarHost = { PageFeedback(model) },
        topBar = {
            NavBar(
                back,
                actions = {
                    if (s != null) {
                        BarButton(onClick = { switching = true }) { Icon(Icons.AutoMirrored.Filled.List, t("Sessions", "会话列表")) }
                        BarButton(onClick = { settings = true }) { Icon(Icons.Filled.Settings, t("Session settings", "会话设置")) }
                    }
                },
            ) {
                    Column(horizontalAlignment = Alignment.CenterHorizontally) {
                        Text(s?.let(::displayTitle) ?: id.take(12), maxLines = 1, overflow = TextOverflow.Ellipsis)
                        if (s != null) MachineTag(world, s.machineId)
                        if (s != null) {
                            // Both parts of its state, and when its machine was last out of touch (docs/session-states.md).
                            Text(
                                listOf(statusLine(s, world), lostSpanText(world.machines.firstOrNull { it.machineId == s.machineId }, s)).filter { it.isNotEmpty() }.joinToString(" · "),
                                style = MaterialTheme.typography.labelSmall, maxLines = 1, overflow = TextOverflow.Ellipsis,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
            }
        },
        bottomBar = {
            if (s != null) {
                val q = s.progress?.pendingQuestion
                Column {
                    when {
                        q != null -> QuestionPanel(model, s, q)
                        isLive(s) -> Column(Modifier.background(aide.bar)) {
                            Hairline()
                            Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                                StatusDot(s)
                                HSpace(8)
                                Text(statusLine(s, world), Modifier.weight(1f), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                                TintedButton(onClick = { scope.launch { model.interrupt(s) } }, destructive = true) { Text(t("Interrupt", "中断")) }
                            }
                        }
                        // Archived with its worktree kept (Session.workspace_kept): said here, as on the desktop.
                        s.status == io.aide.android.api.SessionStatus.ARCHIVED && s.workspaceKept -> Column(Modifier.background(aide.bar)) {
                            Hairline()
                            Text(
                                t("Archived: read-only. Its worktree is kept: it had uncommitted changes, not in the last turn's snapshot. Unarchiving goes on with them.",
                                    "已归档：只读。工作区保留着：归档时里面有未提交、不在最后一轮快照里的改动，取消归档即可接着用。"),
                                Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp),
                                style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                    // A session the owner started takes the owner's messages, here, as on the desktop.
                    if (q == null && s.createdBy == "owner" && s.status != io.aide.android.api.SessionStatus.ARCHIVED) SessionComposer(model, s, open, lastSeq = { feed.lastSeq })
                }
            }
        },
    ) { pad ->
        if (s != null && switching) SwitchSheet(model, s, open) { switching = false }
        if (s != null && settings) SettingsSheet(model, s) { settings = false }
        Box(Modifier.fillMaxSize().padding(pad)) {
            val outboxAll by model.outbox.collectAsState()
            val memoryOnly by model.outboxMemoryOnly.collectAsState()
            val sid = s?.sessionId.orEmpty()
            val queued = remember(outboxAll, sid) { if (sid.isEmpty()) emptyList() else OutboxCodec.forSession(outboxAll, sid) }
            if (loading && items.isEmpty() && queued.isEmpty()) {
                ActivityIndicator(Modifier.align(Alignment.Center), size = 28.dp)
            } else {
                // Its last failure not signed in, while Claude Code still is not: signed in from here (工单 #51, #108).
                val m = world.machines.firstOrNull { it.machineId == s?.machineId }
                val signIn = items.lastOrNull { it is Item.Error && it.auth || it is Item.TurnEnd && it.auth }?.key
                    ?.takeIf { s?.agent == "claude" && m != null && codingBotState(m, "claude", Instant.now())?.login == true }
                LazyColumn(state = list, modifier = Modifier.fillMaxSize(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    items(items, key = { it.key }) {
                        Column(Modifier.animateItem(fadeInSpec = tween(Motion.Medium), placementSpec = Motion.spring(IntOffset.VisibilityThreshold))) {
                            ItemView(it, starter = s?.createdBy.orEmpty(), names = names, agent = agentTitle(s?.agent.orEmpty()))
                            if (it.key == signIn && m != null) Column(Modifier.padding(horizontal = 12.dp)) {
                                Text(
                                    t("Tasks Aide hands out run with the Claude Code command line, which is not signed in on that machine (the Claude desktop app's sign-in does not carry over). The sign-in button below gives a sign-in address to open on any device; sign in, enter the code back, then send a message to go on.",
                                        "Aide 派出的任务用 Claude Code 命令行跑，它在那台机器上没登录（Claude Desktop 的登录不会续给它）。点下面的登录按钮会给出登录地址，在任意设备上打开、登录并填回授权码，再发一条消息即可继续。"),
                                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                                ClaudeLogin(model, m)
                            }
                        }
                    }
                    items(queued, key = { "outbox:${it.id}" }) { msg ->
                        OutboxItemView(
                            msg = msg,
                            // null: the queue could not be written at all; then none of it outlasts the app.
                            inMemoryOnly = memoryOnly?.contains(msg.id) ?: true,
                            onRetry = { model.retryOutbox(msg.id) },
                            onCancel = { model.removeOutbox(msg.id) },
                        )
                    }
                    item { Box(Modifier.heightIn(min = 8.dp)) }
                }
            }
        }
    }
}

/** The box under the owner's own session (工单 #91/#92): words and files at once, the macros after /. */
@Composable
private fun SessionComposer(model: AppModel, s: Session, open: (String) -> Unit, lastSeq: () -> Long = { 0L }) {
    val scope = rememberCoroutineScope()
    var text by rememberSaveable(s.sessionId) { mutableStateOf("") }
    var state by remember(s.sessionId) { mutableStateOf<ComposerState>(ComposerState.Idle) }
    val attach = rememberAttachments(s.sessionId)
    // Its model, as the agent's models list it (工单 #98 补充): another one picked here, the next message starts a new
    // conversation on it (工单 #67 rule 4), said before and after; never locked to the one it began with.
    val world by model.world.collectAsState()
    val a = world.machines.firstOrNull { it.machineId == s.machineId }?.status?.agents?.firstOrNull { it.name == s.agent }
    val runs = listedModel(s.model, a?.models.orEmpty())
    var chosen by remember(s.sessionId, runs) { mutableStateOf(runs) }
    var key by remember(s.sessionId) { mutableStateOf(UUID.randomUUID().toString()) }
    val other = chosen != runs
    fun send() {
        if (state == ComposerState.Sending || (text.isBlank() && attach.files.isEmpty())) return
        state = ComposerState.Sending
        scope.launch {
            if (other) {
                model.converse(s.machineId, s.agent, null, ConvParams(s.projectId, chosen), text, key, attach.wire()).fold({ n ->
                    model.chooseConversation("m:${s.machineId}/${s.agent}", n.sessionId)
                    text = ""; attach.clear(); key = UUID.randomUUID().toString(); state = ComposerState.Idle
                    model.say(t("New conversation: you changed the model", "已开新对话：换了模型"))
                    open(n.sessionId)
                }, { e -> state = ComposerState.Failed(e.message.orEmpty()) { send() } })
                return@launch
            }
            model.enqueueMessage(s.sessionId, text, attach.wire(), afterSeq = lastSeq())
            text = ""; attach.clear(); state = ComposerState.Idle
        }
    }
    Composer(
        text, { text = it; if (state is ComposerState.Failed) state = ComposerState.Idle }, { send() },
        t("Message to ${agentTitle(s.agent)}", "发给 ${agentTitle(s.agent)} 的消息"), state,
        attach = attach,
        commands = listOfNotNull(
            SlashCommand("plan", t("Plan only: no changes until you say so", "只做计划：你点头前不改任何东西")) { text = "!plan " },
            SlashCommand("implement", t("Implement it, run the tests, and sum up", "直接实现，跑测试，最后小结")) { text = "!implement " },
            if (isLive(s)) SlashCommand("stop", t("Interrupt this turn", "打断这一轮")) { scope.launch { model.interrupt(s) } } else null,
            SlashCommand("archive", t("Archive this session", "归档这个会话")) { scope.launch { model.archive(s, true)?.let { state = ComposerState.Failed(it) } } },
        ),
        why = if (other) ComposerWhy(t("You changed the model: the next message starts a new conversation", "换了模型：下一条会开新对话")) else null,
        // Its model, to pick; the rest it runs with, told.
        chips = {
            if (a != null && a.models.isNotEmpty()) {
                ModelChip(s.agent, a, chosen, ranHere = true) { chosen = it }
                Text(s.effort, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            } else Text(sessionParams(s), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        },
    )
}


/** Local outbound queue item rendered inline with truthful delivery state (工单 #115, Fluent #106). */
@Composable
private fun OutboxItemView(msg: OutboxMessage, inMemoryOnly: Boolean, onRetry: () -> Unit, onCancel: () -> Unit) {
    val statusLabel = when (msg.status) {
        OutboxStatus.SENDING -> t("Sending…", "发送中…")
        OutboxStatus.SENT -> t("Delivered", "已送达")
        OutboxStatus.FAILED -> t("Failed to send", "发送失败")
        OutboxStatus.PENDING -> if (msg.error != null) t("Queued · waiting to reconnect", "待发 · 等待重连") else t("Queued", "待发")
    }
    val failed = msg.status == OutboxStatus.FAILED
    val bubbleShape = RoundedCornerShape(topStart = Radius.Bubble, topEnd = Radius.Bubble, bottomStart = Radius.Bubble, bottomEnd = Radius.BubbleTail)
    Row(Modifier.fillMaxWidth().padding(horizontal = 12.dp), horizontalArrangement = Arrangement.End) {
        Surface(
            shape = bubbleShape,
            color = if (failed) aide.card else aide.bubble,
            contentColor = if (failed) MaterialTheme.colorScheme.onSurface else aide.onBubble,
            modifier = Modifier.widthIn(max = 310.dp),
        ) {
            Column(Modifier.padding(horizontal = 14.dp, vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                if (msg.text.isNotBlank()) {
                    Text(msg.text, style = MaterialTheme.typography.bodyLarge, color = if (failed) MaterialTheme.colorScheme.onSurface else aide.onBubble)
                }
                if (msg.attachments.isNotEmpty()) {
                    Text(
                        t("${msg.attachments.size} attachment(s)", "${msg.attachments.size} 个附件"),
                        style = MaterialTheme.typography.labelSmall,
                        color = if (failed) MaterialTheme.colorScheme.onSurfaceVariant else aide.onBubble,
                    )
                }
                if (msg.attachmentsLost) {
                    Text(
                        t("The files were too large to keep and were lost when the app restarted: cancel and attach them again", "附件太大没能存下，重启后已丢失：请取消后重新附加"),
                        style = MaterialTheme.typography.bodySmall,
                        color = if (failed) aide.redText else aide.onBubble,
                    )
                } else if (inMemoryOnly && msg.status != OutboxStatus.SENT) {
                    Text(
                        t("Kept in memory only: lost if the app closes before it goes", "只在内存里：关闭应用前没发出就会丢失"),
                        style = MaterialTheme.typography.bodySmall,
                        color = if (failed) MaterialTheme.colorScheme.onSurfaceVariant else aide.onBubble,
                    )
                }
                Text(
                    "#${msg.clientSeq} · $statusLabel",
                    style = MaterialTheme.typography.labelSmall,
                    color = if (failed) aide.redText else aide.onBubble,
                )
                if (!msg.error.isNullOrBlank() && msg.status != OutboxStatus.SENT) {
                    Text(
                        msg.error,
                        style = MaterialTheme.typography.bodySmall,
                        color = if (failed) MaterialTheme.colorScheme.onSurfaceVariant else aide.onBubble,
                    )
                }
                if (msg.status == OutboxStatus.FAILED || msg.status == OutboxStatus.PENDING) {
                    Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                        if (!msg.attachmentsLost) {
                            TextButton(onClick = onRetry, contentPadding = PaddingValues(horizontal = 8.dp, vertical = 0.dp)) {
                                Text(t("Retry", "重试"), style = MaterialTheme.typography.labelSmall, color = if (failed) MaterialTheme.colorScheme.primary else aide.onBubble)
                            }
                        }
                        // A send tried before may have reached the machine without its answer coming back: said so.
                        TextButton(onClick = onCancel, contentPadding = PaddingValues(horizontal = 8.dp, vertical = 0.dp)) {
                            Text(
                                if (msg.attempted) t("Stop resending (may have arrived)", "不再重发（可能已送达）") else t("Cancel", "取消"),
                                style = MaterialTheme.typography.labelSmall,
                                color = if (failed) MaterialTheme.colorScheme.onSurfaceVariant else aide.onBubble,
                            )
                        }
                    }
                }
            }
        }
    }
}
// The model it runs, as the agent reported it: its specific one, as people call it and in full (工单 #99).
private fun sessionParams(s: Session) = listOf(s.model.ifEmpty { t("as ${agentTitle(s.agent)} is set", "跟随 ${agentTitle(s.agent)}") }.let { id -> modelTitle(id).let { if (it == id) id else "$it · $id" } }, s.effort).filter { it.isNotEmpty() }.joinToString(" · ")

/** The sessions to switch to (工单 #90): this agent's on this machine, by project; a tap opens one. */
@Composable
private fun SwitchSheet(model: AppModel, s: Session, open: (String) -> Unit, close: () -> Unit) {
    val world by model.world.collectAsState()
    IosSheet(onDismissRequest = close, title = t("Sessions", "会话列表")) {
        LazyColumn(Modifier.fillMaxWidth().heightIn(max = 520.dp), contentPadding = PaddingValues(horizontal = 16.dp)) {
            sessionsByProject(s, world.sessions.values).forEach { g ->
                item(key = "p:" + g.projectId) {
                    SectionHeader(world.projects.firstOrNull { it.machineId == s.machineId && it.projectId == g.projectId }?.title() ?: g.projectId)
                }
                itemsIndexed(g.sessions, key = { _, x -> x.sessionId }) { i, x ->
                    val cur = x.sessionId == s.sessionId
                    Row(
                        Modifier.groupRow(i, g.sessions.size, inset = 38.dp).rowPress { close(); if (!cur) open(x.sessionId) }.padding(horizontal = 16.dp, vertical = 12.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        StatusDot(x)
                        Spacer(Modifier.width(10.dp))
                        Text(displayTitle(x), Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge, fontWeight = if (cur) FontWeight.SemiBold else FontWeight.Normal, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        Text(relativeTime(x.updatedAt), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        if (cur) { HSpace(8); Icon(CheckMark, null, Modifier.size(16.dp), tint = MaterialTheme.colorScheme.primary) }
                    }
                }
            }
            item { Spacer(Modifier.heightIn(min = 24.dp)) }
        }
    }
}

/** The session's settings (工单 #90): who started it, which agent on which machine, in which project, with what. */
@Composable
private fun SettingsSheet(model: AppModel, s: Session, close: () -> Unit) {
    val world by model.world.collectAsState()
    IosSheet(onDismissRequest = close, title = t("Session settings", "会话设置")) {
        GroupRows(
            listOf(
                t("Started by", "发起") to world.senderName(s),
                t("Agent", "执行") to agentTitle(s.agent),
                t("Machine", "机器") to world.machineName(s.machineId),
                t("Project", "项目") to (world.projects.firstOrNull { it.machineId == s.machineId && it.projectId == s.projectId }?.title() ?: s.projectId),
                t("Model", "模型") to sessionParams(s),
            ),
            Modifier.padding(start = 16.dp, end = 16.dp, top = 6.dp, bottom = 32.dp),
        ) { (k, v) ->
            Row(Modifier.padding(horizontal = 16.dp, vertical = 11.dp)) {
                Text(k, Modifier.widthIn(min = 72.dp), style = MaterialTheme.typography.bodyLarge)
                Text(v, Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurfaceVariant, textAlign = TextAlign.End, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}

/**
 * One conversation row. A message from someone other than the session's
 * starter (the owner in a session an agent started; another agent) is
 * named; dsh: a DSH session's page, where every message is the owner's and
 * those sent from Aide are marked so.
 */
@Composable
internal fun ItemView(item: Item, starter: String = "", dsh: Boolean = false, names: Map<String, String> = emptyMap(), agent: String = "") {
    val pad = Modifier.padding(horizontal = 12.dp)
    when (item) {
        is Item.User -> Column(pad.fillMaxWidth(), horizontalAlignment = Alignment.End) {
            val by = when {
                dsh -> when (item.from) {
                    "owner" -> t("Sent from Aide", "经 Aide 发送")
                    "" -> null
                    else -> item.fromName.ifEmpty { item.from }.let { t("$it via Aide", "$it 经 Aide 发送") }
                }
                item.from.isEmpty() || item.from == starter -> null
                item.from == "owner" -> t("You", "你")
                // By the name its connection has now, not the one kept with the message (工单 #101: "Muse (<its address>)" from before #85).
                else -> names[item.from]?.ifEmpty { null } ?: item.fromName.ifEmpty { item.from }
            }
            by?.let { Text(it, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(end = 6.dp, bottom = 2.dp)) }
            // Fluent #106 user bubble: #0078d4 accent fill + white text + 18dp radius with 6dp bottom-end tail (both light & dark).
            val shape = RoundedCornerShape(topStart = Radius.Bubble, topEnd = Radius.Bubble, bottomStart = Radius.Bubble, bottomEnd = Radius.BubbleTail)
            // The files that went with it (工单 #143), over its words: a picture sent from this phone as its thumbnail.
            if (item.files.isNotEmpty()) SentFiles(item.files)
            if (item.text.isNotEmpty()) Surface(color = aide.bubble, contentColor = aide.onBubble, shape = shape, modifier = Modifier.widthIn(max = 320.dp)) {
                SelectionContainer { Text(item.text, Modifier.padding(horizontal = 14.dp, vertical = 10.dp), style = MaterialTheme.typography.bodyLarge, color = aide.onBubble) }
            }
        }
        is Item.Agent -> MarkdownText(item.text, pad)
        is Item.Draft -> MarkdownText(item.text, pad, color = MaterialTheme.colorScheme.onSurfaceVariant, selectable = false)
        is Item.Tools -> ToolsView(item)
        is Item.Asked -> Surface(pad, color = MaterialTheme.colorScheme.secondaryContainer, shape = RoundedCornerShape(Radius.Lg)) {
            Column(Modifier.padding(12.dp)) {
                Text(t("Question", "提问") + (item.resolution?.let { " · " + resolutionText(it) } ?: ""), style = MaterialTheme.typography.labelMedium)
                item.question.items.forEach { Text(it.text.ifEmpty { it.header }) }
            }
        }
        is Item.Message -> Text(
            (if (item.sent) "→ ${item.from}: " else "${item.from} →: ") + item.text, pad,
            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.tertiary,
        )
        is Item.Workspace -> Column(pad) {
            val r = item.request
            val by = if (r.from == "agent") t("asked by this task’s agent", "由这个任务的 agent 提出") else r.fromName.ifEmpty { r.from }
            Text(
                t("Workspace request: ", "工作区申请：") + workspaceKind(r.kind) + " " + r.item + " · " + workspaceStatus(r.status) + " · " + by,
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.tertiary,
            )
            if (r.note.isNotBlank()) MarkdownText(r.note)
        }
        is Item.Error -> FailureView(item.kind, item.provider, item.text, agent)
        is Item.TurnEnd -> FailureView(item.kind, item.provider, item.text, agent)
        is Item.Rollback -> Text(
            t("Rolled back to turn ${item.turn}: turns ${item.turn + 1} to ${item.lastTurn} above were discarded.", "已回滚到第 ${item.turn} 轮：上面第 ${item.turn + 1}–${item.lastTurn} 轮已丢弃。"),
            pad, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

private fun workspaceKind(k: String) = when (k) {
    "tool" -> t("tool", "工具")
    "permission" -> t("permission", "权限")
    "info" -> t("information", "信息")
    "system_change" -> t("system change", "系统变更")
    else -> k
}

private fun workspaceStatus(s: String) = when (s) {
    "pending" -> t("pending", "待批")
    "approved" -> t("approved", "已批准")
    "rejected" -> t("rejected", "已拒绝")
    "provisioned" -> t("provisioned", "已就位")
    "confirmed" -> t("confirmed", "已确认")
    else -> s
}

private fun resolutionText(r: String) = when (r) {
    "QUESTION_RESOLUTION_ANSWERED" -> t("answered", "已回答")
    "QUESTION_RESOLUTION_DECLINED" -> t("declined", "已拒绝")
    else -> r.removePrefix("QUESTION_RESOLUTION_").lowercase()
}

/**
 * Fluent error card (工单 #105 / #106): 16dp card with 3dp red left bar, clear headline + advice,
 * raw provider message collapsed under Details.
 */
@Composable
private fun FailureView(kind: String, provider: String, raw: String, agent: String) {
    var open by remember { mutableStateOf(false) }
    val turn by animateFloatAsState(if (open) 90f else 0f, tween(Motion.Short, easing = Motion.EaseOut), label = "chevron")
    val who = agent.ifEmpty { t("The agent", "agent") }
    Row(Modifier.padding(horizontal = 12.dp).fillMaxWidth().aideCard()) {
        Box(Modifier.width(3.dp).heightIn(min = 56.dp).background(aide.red))
        Column(Modifier.weight(1f).padding(horizontal = 12.dp, vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(failureHeadline(kind, who, provider), color = aide.redText, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.SemiBold)
            failureAdvice(kind, who, provider).takeIf { it.isNotEmpty() && (toldApart(kind) || raw.isNotBlank()) }?.let {
                Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            if (raw.isNotBlank()) {
                Row(Modifier.clip(RoundedCornerShape(Radius.Sm)).clickable { open = !open }.padding(vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                    Text(t("Details", "详情"), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.primary, fontWeight = FontWeight.SemiBold)
                    Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, null, Modifier.size(16.dp).rotate(turn), tint = MaterialTheme.colorScheme.primary)
                }
                AnimatedVisibility(
                    open,
                    enter = expandVertically(Motion.spring(IntSize.VisibilityThreshold)) + fadeIn(tween(Motion.Medium)),
                    exit = shrinkVertically(Motion.snappy(IntSize.VisibilityThreshold)) + fadeOut(tween(Motion.Short)),
                ) {
                    Surface(color = aide.inset, shape = RoundedCornerShape(Radius.Sm)) {
                        SelectionContainer {
                            Text(raw, Modifier.fillMaxWidth().padding(8.dp), style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun ToolsView(item: Item.Tools) {
    var open by remember { mutableStateOf(false) }
    val turn by animateFloatAsState(if (open) 90f else 0f, Motion.snappy(), label = "chevron")
    // One quiet row until opened, as on the desktop: a fill a step above the page, the calls folding out under it.
    Surface(
        color = aide.fill, shape = RoundedCornerShape(Radius.Lg),
        modifier = Modifier.padding(horizontal = 12.dp).fillMaxWidth().clip(RoundedCornerShape(Radius.Lg)).clickable { open = !open },
    ) {
        Column(Modifier.padding(horizontal = 12.dp, vertical = 8.dp)) {
            val first = item.calls.first()
            Row(verticalAlignment = Alignment.CenterVertically) {
                Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, null, Modifier.size(16.dp).rotate(turn), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                Spacer(Modifier.width(6.dp))
                Text(
                    if (item.calls.size == 1) listOf(first.name, first.line).filter { it.isNotEmpty() }.joinToString(" · ") else t("${item.calls.size} tool calls", "${item.calls.size} 次工具调用") + " · ${item.calls.last().name}",
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis,
                )
            }
            AnimatedVisibility(
                open,
                enter = expandVertically(Motion.spring(IntSize.VisibilityThreshold)) + fadeIn(tween(Motion.Medium)),
                exit = shrinkVertically(Motion.snappy(IntSize.VisibilityThreshold)) + fadeOut(tween(Motion.Short)),
            ) {
                Column(Modifier.padding(top = 6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    item.calls.forEach { c ->
                        Hairline()
                        Text(listOf(c.name, c.line).filter { it.isNotEmpty() }.joinToString(" · "), style = MaterialTheme.typography.bodySmall, fontWeight = FontWeight.Medium, maxLines = 2, overflow = TextOverflow.Ellipsis)
                        // The raw arguments, under the line, when the line is not all they say (#37), in the same block as the result.
                        if (c.summary.isNotBlank() && c.summary != c.line) {
                            Surface(color = aide.fill2, shape = RoundedCornerShape(Radius.Sm)) {
                                Text(
                                    c.summary, Modifier.fillMaxWidth().padding(8.dp), style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 8, overflow = TextOverflow.Ellipsis,
                                )
                            }
                        }
                        c.result?.let {
                            Surface(color = aide.fill2, shape = RoundedCornerShape(Radius.Sm)) {
                                Text(
                                    it, Modifier.fillMaxWidth().padding(8.dp), style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace,
                                    color = if (c.isError) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 12, overflow = TextOverflow.Ellipsis,
                                )
                            }
                        }
                    }
                }
            }
        }
    }
}

/** The agent's question, each item with its options or a field, answered or left to the agent. */
@Composable
private fun QuestionPanel(model: AppModel, s: Session, q: Question) {
    val chosen = remember(q.questionId) { mutableStateMapOf<String, Set<String>>() }
    val typed = remember(q.questionId) { mutableStateMapOf<String, String>() }
    var busy by remember(q.questionId) { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    // Over the page as a sheet would be (the grouped background, a hairline on top): the question, its options as an
    // inset group with a tick on the picked, a field for its own answer, the buttons under them.
    Column(Modifier.imePadding().background(aide.grouped)) {
        Hairline()
        Column(Modifier.fillMaxWidth().heightIn(max = 420.dp).verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(t("${agentTitle(s.agent)} asks", "${agentTitle(s.agent)} 在问"), style = MaterialTheme.typography.labelLarge, color = Warning)
            q.items.forEach { item ->
                if (item.header.isNotEmpty()) Text(item.header, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                Text(item.text, style = MaterialTheme.typography.titleMedium)
                GroupRows(item.options) { o ->
                    val on = o.label in chosen[item.id].orEmpty()
                    CheckRow(o.label, on, sub = o.description) { chosen[item.id] = choose(item, chosen[item.id].orEmpty(), o.label) }
                }
                if (item.options.isEmpty() || item.allowOther) {
                    IosField(
                        value = typed[item.id].orEmpty(), onValueChange = { typed[item.id] = it },
                        label = if (item.options.isEmpty()) t("Answer", "回答") else t("Other", "其他"), singleLine = false,
                        visualTransformation = if (item.secret) PasswordVisualTransformation() else androidx.compose.ui.text.input.VisualTransformation.None,
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
            }
            val answers = buildAnswers(q.items, chosen, typed)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                FilledButton(onClick = {
                    busy = true
                    scope.launch { model.answer(s, answers ?: return@launch, decline = false); busy = false }
                }, enabled = answers != null && !busy, large = true, modifier = Modifier.weight(1f)) { Text(t("Answer", "回答")) }
                PlainButton(onClick = {
                    busy = true
                    scope.launch { model.answer(s, emptyList(), decline = true); busy = false }
                }, enabled = !busy) { Text(t("Let it decide", "让它自己决定")) }
            }
        }
    }
}

/** The files that went with a message (工单 #143): a picture this phone sent as its thumbnail, any other by name. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun SentFiles(files: List<AttachmentInfo>) {
    FlowRow(Modifier.widthIn(max = 320.dp).padding(bottom = 4.dp), horizontalArrangement = Arrangement.spacedBy(6.dp, Alignment.End), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        for (f in files) {
            val kind = attachKind(PendingFile(f.name, f.mediaType, f.size))
            val pic = if (kind == AttachKind.IMAGE) SentPictures.of(f.sha256) else null
            Box(Modifier.heightIn(min = 52.dp).clip(RoundedCornerShape(12.dp)).background(aide.fill2)) {
                if (pic != null) Image(pic, f.name, Modifier.size(96.dp), contentScale = ContentScale.Crop)
                else Column(Modifier.widthIn(max = 200.dp).padding(horizontal = 10.dp, vertical = 8.dp)) {
                    Text(f.name, fontSize = 12.sp, fontWeight = FontWeight.Medium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    Text("${kindWord(kind)} · ${sizeText(f.size)}", fontSize = 11.sp, color = aide.faint, maxLines = 1)
                }
            }
        }
    }
}
