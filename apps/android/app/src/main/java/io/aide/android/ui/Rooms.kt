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
import androidx.compose.material3.Checkbox
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
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

/** A room's state in a line, in the desktop's words. A round is a hand-on from bot to bot. */
fun roomLine(r: Room): String = when (r.run) {
    "ROOM_RUN_RUNNING" -> when {
        r.mode != ROUNDTABLE -> t("Waiting for a bot", "等 bot 回话")
        r.round == 0 -> t("Round table started · up to ${r.maxRounds} hand-ons", "圆桌开始 · 最多接力 ${r.maxRounds} 轮")
        else -> t("Round table · hand-on ${r.round}/${r.maxRounds}", "圆桌接力中 · 第 ${r.round}/${r.maxRounds} 轮")
    }
    "ROOM_RUN_STOPPED_LIMIT" -> t("Stopped at the round limit", "到达轮次上限，已停")
    "ROOM_RUN_STOPPED_OWNER" -> t("You stopped it", "你停下了")
    else -> r.lastLine.ifEmpty { t("Waiting for you", "等你") }
}

/** The roster's rooms: a header with a button to make one, a row each. */
fun LazyListScope.roomItems(rooms: List<Room>, open: (Room) -> Unit, create: () -> Unit) {
    item(key = "rooms-h") {
        Row(Modifier.padding(top = 12.dp), verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.weight(1f)) { SectionHeader(t("Rooms", "房间"), count = rooms.size.takeIf { it > 0 }) }
            TextButton(onClick = create) { Text(t("New room", "新建房间")) }
        }
    }
    if (rooms.isEmpty()) item(key = "rooms-none") {
        Text(t("No room yet: bring a few bots together.", "还没有房间：拉几个 bot 进来一起干活。"), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 4.dp, vertical = 4.dp))
    }
    rooms.forEachIndexed { i, r ->
        item(key = "room-${r.roomId}", contentType = "room") {
            Box(Modifier.groupRow(i, rooms.size)) {
                Row(Modifier.fillMaxWidth().pressable { open(r) }.padding(horizontal = 16.dp, vertical = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                    Box(Modifier.size(40.dp).clip(CircleShape).background(aide.inset), contentAlignment = Alignment.Center) {
                        Text("${r.members.size}", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
                    }
                    HSpace(12)
                    Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
                        Text(r.name, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        Text(roomLine(r), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                    if (r.unread > 0) { HSpace(8); CountBadge(r.unread, Tone.Live) }
                    HSpace(4)
                    Text("›", style = MaterialTheme.typography.titleLarge, color = aide.faint)
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
    val scope = rememberCoroutineScope()
    suspend fun read() {
        val r = model.readRoom(machineId, roomId)
        failed = r == null && room == null
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
        topBar = {
            TopAppBar(
                title = { Text(room?.name.orEmpty(), maxLines = 1, overflow = TextOverflow.Ellipsis) },
                navigationIcon = { IconButton(onClick = back) { Icon(Icons.AutoMirrored.Filled.ArrowBack, t("Back", "返回")) } },
            )
        },
    ) { pad ->
        val r = room
        if (r == null) {
            Centered { Text(if (failed) t("The room's machine is not online.", "保存这个房间的机器不在线。") else t("Reading the room…", "正在读取房间…")) }
            return@Scaffold
        }
        val table = r.mode == ROUNDTABLE
        val bots = roster(world.machines, world.clients, world.sessions.values, Instant.now(), emptyList()).all
        fun nameOf(key: String) = r.members.firstOrNull { it.botKey == key }?.name ?: key
        fun update(p: RoomPatch) = scope.launch { model.updateRoom(machineId, p)?.let { room = it } }
        Column(Modifier.fillMaxSize().padding(pad)) {
            LazyColumn(Modifier.weight(1f), contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 16.dp)) {
                item(key = "head") {
                    Column(Modifier.fillMaxWidth().padding(vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            StatusPill(roomLine(r), if (r.run == "ROOM_RUN_RUNNING") Tone.Live else Tone.Quiet)
                            Spacer(Modifier.weight(1f))
                            if (r.run == "ROOM_RUN_RUNNING") TextButton(onClick = { scope.launch { model.stopRoom(machineId, roomId)?.let { room = it }; read() } }) { Text(t("Stop", "停止")) }
                        }
                        FlowRow(horizontalArrangement = Arrangement.spacedBy(10.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                            r.members.forEach { m ->
                                val b = bots.firstOrNull { it.key == m.botKey }
                                Row(verticalAlignment = Alignment.CenterVertically) {
                                    if (b != null) BotFace(b, 22, toneColor(botTone(b))) else Box(Modifier.size(22.dp).clip(CircleShape).background(aide.inset))
                                    HSpace(5)
                                    Text(m.name, style = MaterialTheme.typography.bodySmall)
                                    if (b != null) { HSpace(4); Text(b.state.word, style = MaterialTheme.typography.labelSmall, color = aide.faint) }
                                }
                            }
                        }
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                            FilterChip(!table, { update(RoomPatch(roomId, mode = MANUAL)) }, { Text(t("Manual @", "手动 @")) })
                            FilterChip(table, { update(RoomPatch(roomId, mode = ROUNDTABLE)) }, { Text(t("Round table", "自主圆桌")) })
                            if (table) {
                                TextButton(enabled = r.maxRounds > 1, onClick = { update(RoomPatch(roomId, maxRounds = r.maxRounds - 1)) }) { Text("−") }
                                Text(t("up to ${r.maxRounds}", "最多 ${r.maxRounds} 轮"), style = MaterialTheme.typography.bodySmall)
                                TextButton(enabled = r.maxRounds < 20, onClick = { update(RoomPatch(roomId, maxRounds = r.maxRounds + 1)) }) { Text("+") }
                            }
                        }
                        Text(t("kept on ${world.machineName(r.hostMachineId)}", "保存在 ${world.machineName(r.hostMachineId)}"), style = MaterialTheme.typography.labelSmall, color = aide.faint)
                    }
                }
                item(key = "timeline-h") { SectionHeader(t("Timeline", "时间线")) }
                if (msgs.isEmpty()) item(key = "none") {
                    Text(if (table) t("Say something: the ones you name (else the first member) act, and the bots hand on.", "说点什么：被 @ 的（没 @ 时第一个成员）先动，bot 之间会接力。")
                        else t("Say something: the bots you name act.", "说点什么：@ 谁，谁就会动。"), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(4.dp))
                }
                msgs.forEach { m -> item(key = "m-${m.messageId}", contentType = "chat") { RoomMessageView(model, r, m, ::nameOf, openSession) } }
            }
            RoomCompose(model, r) { m -> msgs = msgs.filter { it.messageId != m.messageId } + m; scope.launch { read() } }
        }
    }
}

@Composable
private fun RoomMessageView(model: AppModel, r: Room, m: RoomMessage, nameOf: (String) -> String, openSession: (String) -> Unit) {
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
        Column(Modifier.fillMaxWidth().padding(vertical = 6.dp).border(1.dp, Positive, RoundedCornerShape(14.dp)).padding(12.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(t("Agreed result · from ${m.fromName}", "共识结果 · 来自 ${m.fromName}"), style = MaterialTheme.typography.labelMedium, color = Positive, fontWeight = FontWeight.SemiBold)
            Text(body, style = MaterialTheme.typography.bodyMedium)
            TextButton(onClick = {
                (context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager).setPrimaryClip(ClipData.newPlainText(r.name, "# ${r.name}\n\n$body\n\n— ${m.fromName}\n"))
            }) { Text(t("Copy as Markdown", "复制为 Markdown")) }
        }
        return
    }
    val mine = m.from == "owner"
    val member = r.members.firstOrNull { it.botKey == m.from }
    Column(Modifier.fillMaxWidth().padding(vertical = 4.dp), horizontalAlignment = if (mine) Alignment.End else Alignment.Start, verticalArrangement = Arrangement.spacedBy(3.dp)) {
        if (!mine) Text(m.fromName + if (m.late) t(" · came after stop", " · 停止后到达") else "", style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        if (mine && m.mentions.isNotEmpty()) Text(m.mentions.joinToString(" ") { "@" + nameOf(it) }, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Box(Modifier.fillMaxWidth(0.82f).wrapContentWidth(if (mine) Alignment.End else Alignment.Start)
            .background(if (mine) MaterialTheme.colorScheme.primary.copy(alpha = 0.22f) else aide.inset, RoundedCornerShape(14.dp)).padding(horizontal = 12.dp, vertical = 8.dp)) {
            Text(m.text, style = MaterialTheme.typography.bodyMedium, color = if (m.late) aide.faint else MaterialTheme.colorScheme.onSurface)
        }
        if (mine) m.deliveries.forEach { d ->
            val name = nameOf(d.botKey)
            val (words, bad) = when (d.state) {
                "ROOM_DELIVERY_STATE_SENT" -> t("Sent: waiting for $name to read it", "已发出：等 $name 取信") to false
                "ROOM_DELIVERY_STATE_REPLIED" -> t("$name answered", "$name 已回复") to false
                else -> d.reason to true
            }
            Text(words, style = MaterialTheme.typography.labelSmall, color = if (bad) Warning else aide.faint)
        }
        if (!mine && m.deliveries.isNotEmpty()) Text(t("handed on to ${m.deliveries.joinToString(", ") { nameOf(it.botKey) }}", "转交给 ${m.deliveries.joinToString("、") { nameOf(it.botKey) }}"), style = MaterialTheme.typography.labelSmall, color = aide.faint)
        if (!mine && member?.sessionId?.isNotEmpty() == true) TextButton(onClick = { openSession(member.sessionId) }) { Text(t("Open its session", "打开它的会话")) }
        m.asks.forEach { k ->
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(t("${m.fromName} asks ${nameOf(k)}", "${m.fromName} 想请 ${nameOf(k)}"), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                TextButton(onClick = {
                    scope.launch { model.postRoomMessage(r, t("${m.fromName} asks you to take it from here:\n\n${m.text}", "${m.fromName} 想请你接着看：\n\n${m.text}"), UUID.randomUUID().toString(), listOf(k)) }
                }) { Text(t("Hand on to ${nameOf(k)}", "转给 ${nameOf(k)}")) }
            }
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun RoomCompose(model: AppModel, r: Room, posted: (RoomMessage) -> Unit) {
    val scope = rememberCoroutineScope()
    var text by rememberSaveable { mutableStateOf("") }
    var mentions by rememberSaveable { mutableStateOf(listOf<String>()) }
    var key by rememberSaveable { mutableStateOf(UUID.randomUUID().toString()) }
    var busy by remember { mutableStateOf(false) }
    val woken = mentions.ifEmpty { if (r.mode == ROUNDTABLE) r.members.take(1).map { it.botKey } else emptyList() }
    val names = woken.joinToString("、") { k -> r.members.firstOrNull { it.botKey == k }?.name ?: k }
    Column(Modifier.fillMaxWidth().imePadding()) {
        HorizontalDivider(thickness = 0.5.dp, color = aide.separator)
        FlowRow(Modifier.padding(start = 12.dp, end = 8.dp, top = 6.dp), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            r.members.forEach { m ->
                val on = m.botKey in mentions
                FilterChip(on, { mentions = if (on) mentions - m.botKey else mentions + m.botKey }, { Text("@" + m.name) })
            }
        }
        Text(if (woken.isEmpty()) t("Just saying it: wakes no one", "只是说一句，不叫醒任何人") else t("Wakes: $names", "将叫醒：$names"),
            style = MaterialTheme.typography.labelSmall, color = aide.faint, modifier = Modifier.padding(horizontal = 16.dp))
        Row(Modifier.fillMaxWidth().padding(8.dp), verticalAlignment = Alignment.CenterVertically) {
            OutlinedTextField(text, { text = it }, Modifier.weight(1f), placeholder = { Text(t("Say something in the room…", "在房间里说…")) }, maxLines = 6)
            Spacer(Modifier.width(4.dp))
            IconButton(enabled = text.isNotBlank() && !busy, onClick = {
                busy = true
                scope.launch {
                    val m = model.postRoomMessage(r, text.trim(), key, mentions)
                    busy = false
                    if (m != null) {
                        posted(m)
                        text = ""
                        mentions = emptyList()
                        key = UUID.randomUUID().toString()
                    }
                }
            }) { Icon(Icons.AutoMirrored.Filled.Send, t("Send", "发送")) }
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
        topBar = {
            TopAppBar(
                title = { Text(t("New room", "新建房间")) },
                navigationIcon = { IconButton(onClick = back) { Icon(Icons.AutoMirrored.Filled.ArrowBack, t("Back", "返回")) } },
                actions = {
                    TextButton(enabled = ready && !busy, onClick = {
                        busy = true
                        scope.launch {
                            val r = model.createRoom(CreateRoomRequest(name.trim(), picked.map { (k, p) -> CreateRoomMember(k, p, bots.firstOrNull { it.key == k }?.name.orEmpty()) }, if (table) ROUNDTABLE else MANUAL, maxRounds))
                            busy = false
                            if (r != null) opened(r)
                        }
                    }) { Text(if (busy) t("Creating…", "正在建…") else t("Create", "建房间")) }
                },
            )
        },
    ) { pad ->
        LazyColumn(Modifier.fillMaxSize().padding(pad), contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            item(key = "name") { OutlinedTextField(name, { name = it.take(64) }, Modifier.fillMaxWidth(), label = { Text(t("Name", "房间名")) }, singleLine = true) }
            item(key = "mode") {
                Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                        FilterChip(!table, { table = false }, { Text(t("Manual @", "手动 @")) })
                        FilterChip(table, { table = true }, { Text(t("Round table", "自主圆桌")) })
                        if (table) {
                            TextButton(enabled = maxRounds > 1, onClick = { maxRounds-- }) { Text("−") }
                            Text(t("up to $maxRounds", "最多 $maxRounds 轮"), style = MaterialTheme.typography.bodySmall)
                            TextButton(enabled = maxRounds < 20, onClick = { maxRounds++ }) { Text("+") }
                        }
                    }
                    Text(if (table) t("Round table: you speak, the bots you name (else the first member) act; a bot naming another hands it on, up to the round limit; stop it any time. A reply starting [FINAL] is the room's agreed result.", "自主圆桌：你说一句，被 @ 的（没 @ 时第一个成员）先动；bot 在回复里 @ 谁，就接力给谁，到轮次上限自动停，随时可以停止。以 [FINAL] 开头的回复是共识结果。")
                        else t("Manual @: only the bots you name act; a bot naming another only suggests it, for you to hand on.", "手动 @：只有你 @ 到的 bot 才会动；bot 之间 @ 只是建议，由你决定转不转。"),
                        style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
            item(key = "pick-h") { SectionHeader(t("Members", "成员")) }
            bots.forEach { b ->
                item(key = b.key) {
                    val on = b.key in picked
                    Column {
                        Row(Modifier.fillMaxWidth().pressable { picked = if (on) picked - b.key else picked + (b.key to (if (b.personal) "" else projectsOf(b).firstOrNull()?.projectId.orEmpty())) },
                            verticalAlignment = Alignment.CenterVertically) {
                            Checkbox(on, null)
                            BotFace(b, 28, toneColor(botTone(b)))
                            HSpace(10)
                            Text(b.name, style = MaterialTheme.typography.bodyLarge, modifier = Modifier.weight(1f))
                            Text(b.state.word, style = MaterialTheme.typography.labelSmall, color = aide.faint)
                        }
                        if (on && !b.personal) {
                            val ps = projectsOf(b)
                            if (ps.isEmpty()) Text(t("No project for it on that machine", "那台机器上没有给它用的项目"), style = MaterialTheme.typography.bodySmall, color = Warning, modifier = Modifier.padding(start = 48.dp))
                            else FlowRow(Modifier.padding(start = 48.dp), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                                ps.forEach { p -> FilterChip(picked[b.key] == p.projectId, { picked = picked + (b.key to p.projectId) }, { Text(world.projectTitle(p.machineId, p.projectId)) }) }
                            }
                        }
                    }
                }
            }
        }
    }
}
