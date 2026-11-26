package io.aide.android.ui

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.wrapContentWidth
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.selection.selectable
import androidx.compose.ui.semantics.Role
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.aide.android.api.CreateRoomMember
import io.aide.android.api.CreateRoomRequest
import io.aide.android.api.Room
import io.aide.android.api.RoomMessage
import io.aide.android.api.RoomPatch
import io.aide.android.model.AppModel
import io.aide.android.model.Bot
import io.aide.android.model.roster
import java.time.Instant
import java.util.UUID
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

// Rooms (ROOMS-DESIGN-2026-11-22, G2-b/c) on the phone, as on the desktop: in the roster above the bots, each with
// its line and what the owner has not read; a room's page with its members, how it is run, and its timeline.

const val MANUAL = "ROOM_MODE_MANUAL"
const val ROUNDTABLE = "ROOM_MODE_ROUNDTABLE"
const val LIGHT = "ROOM_MODE_LIGHT"

/** A room's state in a line, in the desktop's words. A round is a hand-on from bot to bot. */
fun roomLine(r: Room): String = when {
    r.shadow -> r.lastLine.ifEmpty { t("Agent conversation · read-only", "Agent 间对话 · 只读") }
    r.run == "ROOM_RUN_RUNNING" -> when {
        r.mode == LIGHT -> t("Light chat · waiting for reply", "轻聊中 · 等回复")
        r.mode != ROUNDTABLE -> t("Waiting for a bot", "等 bot 回话")
        r.round == 0 -> t("Round table started · up to ${r.maxRounds} hand-ons", "圆桌开始 · 最多接力 ${r.maxRounds} 轮")
        else -> t("Round table · hand-on ${r.round}/${r.maxRounds}", "圆桌接力中 · 第 ${r.round}/${r.maxRounds} 轮")
    }
    r.run == "ROOM_RUN_STOPPED_LIMIT" -> t("Stopped at the round limit", "到达轮次上限，已停")
    r.run == "ROOM_RUN_STOPPED_OWNER" -> t("You stopped it", "你停下了")
    else -> r.lastLine.ifEmpty { t("Waiting for you", "等你") }
}

/** Replies waiting at the post office for a room's away host (工单 #74), from the other machines' status. */
private fun waitingLine(r: Room, outbox: List<io.aide.android.api.RoomOutbox>): String? = outbox.filter { it.roomId == r.roomId }.sumOf { it.replies }.takeIf { it > 0 }?.let {
    t("$it replies wait for the machine keeping the room", "$it 条回复等负责房间的机器上线")
}

/** The roster's rooms: a header with a button to make one, a row each. */
fun LazyListScope.roomItems(rooms: List<Room>, open: (Room) -> Unit, create: () -> Unit, outbox: List<io.aide.android.api.RoomOutbox> = emptyList()) {
    item(key = "rooms-h") {
        Row(Modifier.padding(top = 12.dp), verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.weight(1f)) { SectionHeader(t("Rooms", "房间"), count = rooms.size.takeIf { it > 0 }) }
            PlainButton(onClick = create, modifier = Modifier.padding(top = 14.dp)) { Text(t("New room", "新建房间")) }
        }
    }
    if (rooms.isEmpty()) item(key = "rooms-none") {
        Text(t("No room yet: bring a few bots together.", "还没有房间：拉几个 bot 进来一起干活。"), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
    }
    rooms.forEachIndexed { i, r ->
        item(key = "room-${r.roomId}", contentType = "room") {
            Box(Modifier.groupRow(i, rooms.size, inset = 68.dp)) {
                Row(Modifier.fillMaxWidth().rowPress { open(r) }.padding(start = 16.dp, end = 12.dp, top = 11.dp, bottom = 11.dp), verticalAlignment = Alignment.CenterVertically) {
                    Box(Modifier.size(40.dp).clip(CircleShape).background(aide.inset), contentAlignment = Alignment.Center) {
                        Text("${r.members.size}", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
                    }
                    HSpace(12)
                    Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Text(r.name, style = MaterialTheme.typography.titleMedium, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                            if (r.shadow) {
                                HSpace(6)
                                StatusPill(t("Read-only", "只读"), Tone.Quiet)
                            }
                        }
                        Text(waitingLine(r, outbox) ?: roomLine(r), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                    if (r.unread > 0) { HSpace(8); CountBadge(r.unread, Tone.Live) }
                    HSpace(6)
                    Chevron()
                }
            }
        }
    }
}

/** A room's page: its members and how it is run at the top, its timeline, a box to say something and whom it wakes. */
@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
fun RoomPage(model: AppModel, machineId: String, roomId: String, openSession: (String) -> Unit, back: () -> Unit) {
    val world by model.world.collectAsState()
    var room by remember { mutableStateOf<Room?>(null) }
    var msgs by remember { mutableStateOf<List<RoomMessage>>(emptyList()) }
    var failed by remember { mutableStateOf(false) }
    var deleting by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    suspend fun read() {
        val r = model.readRoom(machineId, roomId)
        failed = r == null
        if (r != null) { room = r.room; msgs = r.messages }
    }
    DisposableEffect(roomId) {
        model.roomOpen = roomId
        onDispose { if (model.roomOpen == roomId) model.roomOpen = "" }
    }
    LaunchedEffect(roomId) {
        while (true) {
            read()
            delay(3_000)
        }
    }
    // Read up to the newest: what the roster counts as not read goes.
    val last = msgs.lastOrNull()?.messageId
    LaunchedEffect(last) { if (last != null && room?.seenMessageId != last) model.markRoomSeen(machineId, roomId) }
    Scaffold(
        snackbarHost = { PageFeedback(model) },
        topBar = {
            NavBar(back, actions = { if (room != null && !room!!.shadow) PlainButton(onClick = { deleting = true }) { Text(t("Delete", "删除"), style = IosType.Body, color = MaterialTheme.colorScheme.error) } }) {
                Text(room?.name.orEmpty(), maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        },
    ) { pad ->
        val r = room
        // The host away (工单 #74): said, with the replies that wait for it; what was read last stays shown.
        val host = world.machines.firstOrNull { it.machineId == machineId }
        val away = failed || (host != null && !host.online && !host.local)
        // What the post office counts for this room (every member's reply, a personal agent's too), else the machines' own count.
        var post by remember { mutableStateOf(0) }
        LaunchedEffect(away) {
            while (away) {
                post = model.roomWaiting(machineId)[roomId] ?: 0
                delay(15_000)
            }
        }
        val waiting = maxOf(post, world.machines.flatMap { it.status?.roomOutbox.orEmpty() }.filter { it.roomId == roomId }.sumOf { it.replies })
        if (r == null) {
            Centered {
                Text(if (failed) t("${host?.machineName ?: "The machine"} keeping this room is offline.", "负责这个房间的 ${host?.machineName ?: "机器"} 不在线。") else t("Reading the room…", "正在读取房间…"))
                if (waiting > 0) Text(t("$waiting replies wait at the post office for it to come back.", "有 $waiting 条回复在邮局等它上线后入房。"), color = Warning)
            }
            return@Scaffold
        }
        if (deleting) DeleteRoomDialog(model, r, { deleting = false }) { deleting = false; back() }
        val mode = r.mode.ifEmpty { MANUAL }
        val table = mode == ROUNDTABLE
        val bots = roster(world.machines, world.clients, world.sessions.values, Instant.now(), emptyList()).all
        fun nameOf(key: String) = r.members.firstOrNull { it.botKey == key }?.name ?: key
        fun update(p: RoomPatch) = scope.launch { model.updateRoom(machineId, p)?.let { room = it } }
        Column(Modifier.fillMaxSize().padding(pad)) {
            LazyColumn(Modifier.weight(1f), contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 16.dp)) {
                item(key = "head") {
                    Column(Modifier.fillMaxWidth().padding(vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            StatusPill(if (r.shadow) t("Read-only · Agent conversation", "只读 · Agent 间对话") else roomLine(r), if (!r.shadow && r.run == "ROOM_RUN_RUNNING") Tone.Live else Tone.Quiet)
                            Spacer(Modifier.weight(1f))
                            if (!r.shadow && r.run == "ROOM_RUN_RUNNING") TintedButton(onClick = { scope.launch { model.stopRoom(machineId, roomId)?.let { room = it }; read() } }, destructive = true) { Text(t("Stop", "停止")) }
                        }
                        FlowRow(horizontalArrangement = Arrangement.spacedBy(10.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                            r.members.forEach { m ->
                                val b = bots.firstOrNull { it.key == m.botKey }
                                Row(verticalAlignment = Alignment.CenterVertically) {
                                    if (b != null) BotFace(b, 22, toneDotColor(botTone(b))) else Box(Modifier.size(22.dp).clip(CircleShape).background(aide.inset))
                                    HSpace(5)
                                    Text(m.name, style = MaterialTheme.typography.bodySmall)
                                    if (b != null) { HSpace(4); Text(b.state.word, style = MaterialTheme.typography.labelSmall, color = aide.faint) }
                                }
                            }
                        }
                        if (!r.shadow) {
                            Row(horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.CenterVertically) {
                                Segmented(listOf(MANUAL to t("Manual @", "手动 @"), ROUNDTABLE to t("Round table", "自主圆桌"), LIGHT to t("Light chat", "轻聊")), mode, Modifier.weight(1f)) { update(RoomPatch(roomId, mode = it)) }
                                if (table) {
                                    Text(t("up to ${r.maxRounds}", "最多 ${r.maxRounds} 轮"), style = MaterialTheme.typography.bodySmall)
                                    IosStepper(r.maxRounds > 1, r.maxRounds < 20, { update(RoomPatch(roomId, maxRounds = r.maxRounds - 1)) }, { update(RoomPatch(roomId, maxRounds = r.maxRounds + 1)) })
                                }
                            }
                        }
                    }
                }
                if (away) item(key = "away") {
                    Text(
                        t("${host?.machineName ?: "The machine"} keeping this room is offline.", "负责这个房间的 ${host?.machineName ?: "机器"} 不在线。") +
                            if (waiting > 0) t(" $waiting replies wait at the post office to come in once it is back.", " 有 $waiting 条回复在邮局等它上线后入房。") else "",
                        style = MaterialTheme.typography.bodySmall, color = Warning, modifier = Modifier.fillMaxWidth().aideInset().padding(12.dp),
                    )
                }
                item(key = "timeline-h") { SectionHeader(t("Timeline", "时间线")) }
                if (msgs.isEmpty()) item(key = "none") {
                    Text(when {
                        r.shadow -> t("No agent messages recorded yet.", "暂时还没有记录到 agent 之间的消息。")
                        mode == LIGHT -> t("Light chat: quick back-and-forth without heavy task context.", "轻聊模式：快速交流，不拖带长上下文。")
                        table -> t("Say something: the ones you name (else the first member) act, and the bots hand on.", "说点什么：被 @ 的（没 @ 时第一个成员）先动，bot 之间会接力。")
                        else -> t("Say something: the bots you name act.", "说点什么：@ 谁，谁就会动。")
                    }, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
                }
                val byId = msgs.associateBy { it.messageId }
                msgs.forEach { m -> item(key = "m-${m.messageId}", contentType = "chat") { RoomMessageView(model, r, m, byId[m.inReplyTo], ::nameOf, openSession) } }
                if (waiting > 0) item(key = "waiting") {
                    Text(t("$waiting replies are on their way: they come in as soon as the machine keeping the room is back", "还有 $waiting 条回复在路上：负责房间的机器一上线就进房间"), style = MaterialTheme.typography.labelSmall, color = aide.faint, modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp))
                }
            }
            if (r.shadow) {
                Text(
                    t("Read-only shadow conversation · View messages between your agents here", "只读影子对话 · 仅用于查看 agent 之间的往来消息，不能在此发言"),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.fillMaxWidth().background(aide.inset).padding(horizontal = 16.dp, vertical = 12.dp),
                )
            } else {
                RoomCompose(model, r, enabled = !away) { m -> msgs = msgs.filter { it.messageId != m.messageId } + m; scope.launch { read() } }
            }
        }
    }
}

@Composable
private fun RoomMessageView(model: AppModel, r: Room, m: RoomMessage, repliedTo: RoomMessage?, nameOf: (String) -> String, openSession: (String) -> Unit) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    if (m.system) {
        Box(Modifier.fillMaxWidth().padding(vertical = 4.dp), contentAlignment = Alignment.Center) {
            Text(m.text, style = MaterialTheme.typography.labelSmall, color = aide.faint, modifier = Modifier.background(aide.inset, RoundedCornerShape(999.dp)).padding(horizontal = 10.dp, vertical = 4.dp))
        }
        return
    }
    if (m.isFinal) {
        val body = m.text.trim().removePrefix("[FINAL]").trim()
        Column(Modifier.fillMaxWidth().padding(vertical = 6.dp).aideCard().background(Positive.copy(alpha = 0.12f)).padding(start = 14.dp, end = 6.dp, top = 12.dp, bottom = 4.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(t("Agreed result · from ${m.fromName}", "共识结果 · 来自 ${m.fromName}"), style = MaterialTheme.typography.labelMedium, color = Positive, fontWeight = FontWeight.SemiBold)
            MarkdownText(body, Modifier.padding(end = 8.dp))
            PlainButton(contentPadding = PaddingValues(vertical = 8.dp), onClick = {
                (context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager).setPrimaryClip(ClipData.newPlainText(r.name, "# ${r.name}\n\n$body\n\n— ${m.fromName}\n"))
            }) { Text(t("Copy as Markdown", "复制为 Markdown")) }
        }
        return
    }
    val mine = m.from == "owner"
    val member = r.members.firstOrNull { it.botKey == m.from }
    val bubbleShape = RoundedCornerShape(
        topStart = Radius.Bubble, topEnd = Radius.Bubble,
        bottomStart = if (mine) Radius.Bubble else Radius.BubbleTail,
        bottomEnd = if (mine) Radius.BubbleTail else Radius.Bubble,
    )
    Column(Modifier.fillMaxWidth().padding(vertical = 4.dp), horizontalAlignment = if (mine) Alignment.End else Alignment.Start, verticalArrangement = Arrangement.spacedBy(3.dp)) {
        if (!mine) {
            val head = buildString {
                append(m.fromName)
                if (r.shadow && m.toName.isNotEmpty()) append(" → ${m.toName}")
                if (r.shadow && m.at.isNotEmpty()) append(" · ${io.aide.android.model.agoText(io.aide.android.model.instant(m.at), Instant.now())}")
                if (m.lightChat) append(t(" · Light", " · 轻聊"))
                if (m.late) append(t(" · came after stop", " · 停止后到达"))
            }
            Text(head, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        if (mine && (m.mentions.isNotEmpty() || m.lightChat)) {
            val meta = listOfNotNull(
                if (m.lightChat) t("Light chat", "轻聊") else null,
                m.mentions.takeIf { it.isNotEmpty() }?.joinToString(" ") { "@" + nameOf(it) },
            ).joinToString(" · ")
            Text(meta, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        Box(Modifier.fillMaxWidth(0.82f).wrapContentWidth(if (mine) Alignment.End else Alignment.Start)
            .background(if (mine) aide.bubble else aide.card, bubbleShape)
            .then(if (mine) Modifier else Modifier.border(1.dp, aide.cardEdge, bubbleShape))
            .padding(horizontal = 14.dp, vertical = 10.dp)) {
            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                if (m.subject.isNotEmpty()) {
                    Text(m.subject, style = MaterialTheme.typography.labelLarge, fontWeight = FontWeight.SemiBold, color = if (mine) aide.onBubble else MaterialTheme.colorScheme.onSurface)
                }
                if (m.inReplyTo.isNotEmpty()) {
                    val snippet = repliedTo?.let { "${it.fromName}: ${it.text.replace('\n', ' ').take(48)}" } ?: m.inReplyTo.take(12)
                    Text(
                        t("Reply to $snippet", "回复 $snippet"),
                        style = MaterialTheme.typography.labelSmall,
                        color = if (mine) aide.onBubble.copy(alpha = 0.85f) else MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
                if (mine) {
                    Text(m.text, style = MaterialTheme.typography.bodyLarge, color = aide.onBubble)
                } else {
                    MarkdownText(m.text, color = if (m.late) aide.faint else MaterialTheme.colorScheme.onSurface)
                }
            }
        }
        if (mine) m.deliveries.forEach { d ->
            val (words, bad) = deliveryWords(d.state, nameOf(d.botKey), d.reason, d.reminded)
            Text(words, style = MaterialTheme.typography.labelSmall, color = if (bad) Warning else aide.faint)
        }
        if (!mine && !r.shadow && m.deliveries.isNotEmpty()) Text(t("handed on to ${m.deliveries.joinToString(", ") { nameOf(it.botKey) }}", "转交给 ${m.deliveries.joinToString("、") { nameOf(it.botKey) }}"), style = MaterialTheme.typography.labelSmall, color = aide.faint)
        if (!mine && !r.shadow && member?.sessionId?.isNotEmpty() == true) PlainButton(onClick = { openSession(member.sessionId) }) { Text(t("Open its session", "打开它的会话")) }
        if (!r.shadow) m.asks.forEach { k ->
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(t("${m.fromName} asks ${nameOf(k)}", "${m.fromName} 想请 ${nameOf(k)}"), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                PlainButton(onClick = {
                    scope.launch { model.postRoomMessage(r, t("${m.fromName} asks you to take it from here:\n\n${m.text}", "${m.fromName} 想请你接着看：\n\n${m.text}"), UUID.randomUUID().toString(), listOf(k)) }
                }) { Text(t("Hand on to ${nameOf(k)}", "转给 ${nameOf(k)}")) }
            }
        }
    }
}

@Composable
private fun RoomCompose(model: AppModel, r: Room, enabled: Boolean, posted: (RoomMessage) -> Unit) {
    val scope = rememberCoroutineScope()
    var text by rememberSaveable { mutableStateOf("") }
    var mentions by rememberSaveable { mutableStateOf(listOf<String>()) }
    var key by rememberSaveable { mutableStateOf(UUID.randomUUID().toString()) }
    var state by remember { mutableStateOf<ComposerState>(ComposerState.Idle) }
    val light = r.mode == LIGHT
    val woken = mentions.ifEmpty { if (r.mode == ROUNDTABLE) r.members.take(1).map { it.botKey } else emptyList() }
    val names = woken.joinToString("、") { k -> r.members.firstOrNull { it.botKey == k }?.name ?: k }
    fun send() {
        state = ComposerState.Sending
        scope.launch {
            model.postRoomMessageOr(r, text.trim(), key, { state = ComposerState.Failed(it) { send() } }, mentions, lightChat = light)?.let { m ->
                posted(m)
                text = ""
                mentions = emptyList()
                key = UUID.randomUUID().toString()
                state = ComposerState.Idle
            }
        }
    }
    Composer(
        text, { text = it }, { send() }, if (light) t("Quick message in light chat…", "轻聊一句…") else t("Say something in the room…", "在房间里说…"), state, enabled = enabled,
        why = ComposerWhy(when {
            light && woken.isNotEmpty() -> t("Light chat · wakes: $names", "轻聊模式 · 将叫醒：$names")
            woken.isEmpty() -> t("Just saying it: wakes no one", "只是说一句，不叫醒任何人")
            else -> t("Wakes: $names", "将叫醒：$names")
        }),
        mentions = {
            r.members.forEach { m ->
                val on = m.botKey in mentions
                ToggleChip("@" + m.name, on) { mentions = if (on) mentions - m.botKey else mentions + m.botKey }
            }
        },
    )
}

/**
 * Deleting a room (工单 #73): what becomes of its members' sessions is said and chosen — the room alone (they stay in
 * their projects), or with the sessions made for it — and those sessions are listed.
 */
@Composable
private fun DeleteRoomDialog(model: AppModel, r: Room, cancel: () -> Unit, deleted: () -> Unit) {
    val scope = rememberCoroutineScope()
    val world by model.world.collectAsState()
    val sessions = r.members.filter { it.sessionId.isNotEmpty() }
    var withSessions by remember { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    IosAlert(
        onDismissRequest = { if (!busy) cancel() },
        title = t("Delete “${r.name}”?", "删除房间「${r.name}」？"),
        message = t("The room and its timeline go. What becomes of the sessions its members started in it is yours to choose:", "房间和它的时间线会删掉。成员在房间里新建的会话怎么处理，你来选："),
        actions = listOf(
            AlertAction(t("Cancel", "取消"), AlertRole.Cancel, enabled = !busy, onClick = cancel),
            AlertAction(if (busy) t("Deleting…", "正在删…") else if (withSessions) t("Delete Room and Sessions", "删除房间和会话") else t("Delete Room", "删除房间"), AlertRole.Destructive, enabled = !busy) {
                busy = true
                scope.launch {
                    val why = model.deleteRoom(r.hostMachineId, r.roomId, if (withSessions) sessions.map { it.sessionId } else emptyList())
                    busy = false
                    if (why == null) deleted() else error = why
                }
            },
        ),
    ) {
        ChoiceRow(t("The room only", "只删房间"), t("${sessions.size} member sessions stay in their projects", "${sessions.size} 个成员会话留在原来的项目里"), !withSessions) { withSessions = false }
        if (sessions.isNotEmpty()) ChoiceRow(t("The room and its members’ sessions", "连同成员会话一起删"), t("The ${sessions.size} sessions started in this room are deleted too, for good", "本房间里新建的 ${sessions.size} 个成员会话一并删除，不能恢复"), withSessions) { withSessions = true }
        if (sessions.isNotEmpty()) {
            Text(t("Sessions affected (${sessions.size})", "受影响的会话（${sessions.size}）"), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            sessions.forEach { m ->
                val s = world.sessions[m.sessionId]
                Text("${m.name} · ${s?.let { displayTitleOf(it) } ?: t("on another machine", "在另一台机器上")}", style = MaterialTheme.typography.bodySmall)
            }
        }
        error?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = Warning) }
    }
}

private fun displayTitleOf(s: io.aide.android.api.Session) = io.aide.android.model.displayTitle(s)

/** One choice of several, as Apple's lists draw it: a round check, its words, a line under. */
@Composable
fun ChoiceRow(title: String, sub: String, on: Boolean, onClick: () -> Unit) {
    Row(Modifier.fillMaxWidth().selectable(on, remember { MutableInteractionSource() }, Highlight(aide.pressed), role = Role.RadioButton, onClick = onClick).padding(vertical = 6.dp), verticalAlignment = Alignment.CenterVertically) {
        RoundCheck(on)
        HSpace(10)
        Column(Modifier.weight(1f)) {
            Text(title, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Medium)
            Text(sub, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

/** Making a room: its name, how it is run, the bots in it (a coding agent with the project it works in). */
@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
fun NewRoomPage(model: AppModel, opened: (Room) -> Unit, back: () -> Unit) {
    val world by model.world.collectAsState()
    val scope = rememberCoroutineScope()
    val bots = roster(world.machines, world.clients, world.sessions.values, Instant.now(), emptyList()).all
        .filter { it.personal || (it.coding && it.key.startsWith("m:")) }
    var name by rememberSaveable { mutableStateOf("") }
    var table by rememberSaveable { mutableStateOf(false) }
    var maxRounds by rememberSaveable { mutableStateOf(6) }
    var picked by remember { mutableStateOf(mapOf<String, String>()) } // bot key → project ("" for a personal agent)
    var busy by remember { mutableStateOf(false) }
    fun projectsOf(b: Bot) = world.projects.filter { it.machineId == b.machineId && (it.agents.isEmpty() || b.agent in it.agents) }
    val ready = name.isNotBlank() && picked.isNotEmpty() && picked.all { (k, p) -> k.startsWith("c:") || p.isNotEmpty() }
    Scaffold(
        snackbarHost = { PageFeedback(model) },
        topBar = {
            NavBar(
                back,
                actions = {
                    PlainButton(enabled = ready && !busy, onClick = {
                        busy = true
                        scope.launch {
                            val r = model.createRoom(CreateRoomRequest(name.trim(), picked.map { (k, p) -> CreateRoomMember(k, p, bots.firstOrNull { it.key == k }?.name.orEmpty()) }, if (table) ROUNDTABLE else MANUAL, maxRounds))
                            busy = false
                            if (r != null) opened(r)
                        }
                    }) { Text(if (busy) t("Creating…", "正在建…") else t("Create", "建房间"), style = IosType.Headline) }
                },
            ) { Text(t("New room", "新建房间")) }
        },
    ) { pad ->
        LazyColumn(Modifier.fillMaxSize().padding(pad), contentPadding = PaddingValues(16.dp)) {
            item(key = "name") { IosField(name, { name = it.take(64) }, Modifier.fillMaxWidth().padding(bottom = 14.dp), label = t("Name", "房间名")) }
            item(key = "mode") {
                Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Row(horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.CenterVertically) {
                        Segmented(listOf(false to t("Manual @", "手动 @"), true to t("Round table", "自主圆桌")), table, Modifier.weight(1f)) { table = it }
                        if (table) {
                            Text(t("up to $maxRounds", "最多 $maxRounds 轮"), style = MaterialTheme.typography.bodySmall)
                            IosStepper(maxRounds > 1, maxRounds < 20, { maxRounds-- }, { maxRounds++ })
                        }
                    }
                    Text(if (table) t("Round table: you speak, the bots you name (else the first member) act; a bot naming another hands it on, up to the round limit; stop it any time. A reply starting [FINAL] is the room's agreed result.", "自主圆桌：你说一句，被 @ 的（没 @ 时第一个成员）先动；bot 在回复里 @ 谁，就接力给谁，到轮次上限自动停，随时可以停止。以 [FINAL] 开头的回复是共识结果。")
                        else t("Manual @: only the bots you name act; a bot naming another only suggests it, for you to hand on.", "手动 @：只有你 @ 到的 bot 才会动；bot 之间 @ 只是建议，由你决定转不转。"),
                        style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 16.dp))
                }
            }
            item(key = "pick-h") { SectionHeader(t("Members", "成员")) }
            bots.forEachIndexed { i, b ->
                item(key = b.key) {
                    val on = b.key in picked
                    // One card of rows, as every list in the app (工单 #68).
                    Column(Modifier.groupRow(i, bots.size, inset = 86.dp)) {
                        Row(Modifier.fillMaxWidth().rowPress { picked = if (on) picked - b.key else picked + (b.key to (if (b.personal) "" else projectsOf(b).firstOrNull()?.projectId.orEmpty())) }.padding(horizontal = 16.dp, vertical = 10.dp),
                            verticalAlignment = Alignment.CenterVertically) {
                            RoundCheck(on)
                            HSpace(10)
                            BotFace(b, 28, toneColor(botTone(b)))
                            HSpace(10)
                            Text(b.name, style = MaterialTheme.typography.bodyLarge, modifier = Modifier.weight(1f))
                            Text(b.state.word, style = MaterialTheme.typography.labelSmall, color = aide.faint)
                        }
                        if (on && !b.personal) {
                            val ps = projectsOf(b)
                            if (ps.isEmpty()) Text(t("No project for it on that machine", "那台机器上没有给它用的项目"), style = MaterialTheme.typography.bodySmall, color = Warning, modifier = Modifier.padding(start = 86.dp, bottom = 10.dp))
                            else FlowRow(Modifier.padding(start = 86.dp, bottom = 10.dp), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                                ps.forEach { p -> ToggleChip(world.projectTitle(p.machineId, p.projectId), picked[b.key] == p.projectId) { picked = picked + (b.key to p.projectId) } }
                            }
                        }
                    }
                }
            }
        }
    }
}
