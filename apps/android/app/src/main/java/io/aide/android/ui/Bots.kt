package io.aide.android.ui

import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.wrapContentWidth
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
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
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.aide.android.api.Machine
import io.aide.android.api.Persona
import io.aide.android.api.Session
import io.aide.android.api.SessionStatus
import io.aide.android.api.title
import io.aide.android.model.AppModel
import io.aide.android.model.Bot
import io.aide.android.model.ConvParams
import io.aide.android.model.Stuck
import io.aide.android.model.Usable
import io.aide.android.model.agoText
import io.aide.android.model.botConversations
import io.aide.android.model.changedParams
import io.aide.android.model.currentConversation
import io.aide.android.model.displayTitle
import io.aide.android.model.instant
import io.aide.android.model.instantOrNull
import io.aide.android.model.paramsOf
import io.aide.android.model.personaColors
import io.aide.android.model.roster
import io.aide.android.model.stuckWhy
import io.aide.android.model.withContext
import java.time.Instant
import java.util.UUID
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

// The roster (工单 #57, #63, BOTARK-REF 产品结构纲要), the app's first tab: bots and rooms, nothing else — the rooms,
// the personal agents (they follow the account), the coding agents (each named by its machine when there are several).
// A row is its face with its state on the corner, its one name, its state as a pill, one line of what it does lately.
// No machine card or count is here: a machine shows only through its bots' states; the devices tab has the machines.

/** A bot's face, the same on every screen (工单 #62): BotAvatar with its state's colour on the corner. */
@Composable
fun BotFace(b: Bot, size: Int, dot: Color? = null) = BotAvatar(b, size, dot)

/** Its state as a pill, the same words as everywhere (工单 #56). */
fun botTone(b: Bot): Tone = when (b.state.usable) {
    Usable.YES -> Tone.Good
    Usable.UNVERIFIED -> if (b.state.why == io.aide.android.model.NotReady.CHECKING) Tone.Live else Tone.Quiet
    Usable.NO -> if (b.state.why == io.aide.android.model.NotReady.OFFLINE) Tone.Quiet else Tone.Warn
}

@Composable
fun BotsTab(model: AppModel, openRoom: (io.aide.android.api.Room) -> Unit = {}, newRoom: () -> Unit = {}, open: (String) -> Unit) {
    val world by model.world.collectAsState()
    // Findings too old to show as now are checked again while the roster is open (工单 #53), as the desktop does.
    LaunchedEffect(Unit) {
        while (true) {
            model.checkStaleAgents(Instant.now())
            delay(60_000)
        }
    }
    val personas by model.personas.collectAsState()
    LaunchedEffect(Unit) { model.loadPersonas() }
    val r = roster(world.machines, world.clients, world.sessions.values, Instant.now(), personas.orEmpty())
    val list = rememberLazyListState()
    Column(Modifier.fillMaxSize()) {
        PageTitle("Bot")
        SyncLine(model)
        CompositionLocalProvider(LocalScrolling provides list.isScrollInProgress) {
            LazyColumn(Modifier.fillMaxSize(), state = list, contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 4.dp, bottom = 24.dp)) {
                if (r.all.isEmpty() && world.watched) item {
                    Text(t("No bot yet: connect a coding agent on a computer (Aide › Settings › Connect), or a personal agent to your account.", "还没有 bot：先在电脑上的 Aide 里连接一个编码 agent（设置 › 连接），或把个人 agent 连到你的账号。"), color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(vertical = 12.dp))
                }
                // Rooms beside the bots, first (ROOMS-DESIGN §2).
                if (r.all.isNotEmpty()) roomItems(world.rooms, openRoom, newRoom, world.machines.flatMap { it.status?.roomOutbox.orEmpty() })
                botGroup("personal", t("Personal agents · follow your account", "个人 agent · 跟账号走"), r.personal, open)
                botGroup("coding", t("Coding agents · on your machines", "编码 agent · 跟机器走"), r.coding, open)
            }
        }
    }
}

private fun LazyListScope.botGroup(id: String, title: String, bots: List<Bot>, open: (String) -> Unit) {
    if (bots.isEmpty()) return
    item(key = "$id-h") { Box(Modifier.padding(top = 12.dp)) { SectionHeader(title, count = bots.size) } }
    bots.forEachIndexed { i, b ->
        item(key = b.key, contentType = "bot") {
            Box(Modifier.animateItem(fadeInSpec = tween(Motion.Medium), placementSpec = tween(Motion.Medium, easing = Motion.EaseOut)).groupRow(i, bots.size)) { BotRow(b) { open(b.key) } }
        }
    }
}

/** A roster row (BOTARK-REF §1): face with its state on the corner, the one name with its state as a pill, one line under it. */
@Composable
fun BotRow(b: Bot, onClick: () -> Unit) {
    Row(Modifier.fillMaxWidth().pressable(onClick).padding(horizontal = 16.dp, vertical = 12.dp), verticalAlignment = Alignment.CenterVertically) {
        BotAvatar(b, 40, toneColor(botTone(b)))
        HSpace(12)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(b.name, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                HSpace(8)
                StatusPill(b.state.word, botTone(b))
            }
            Text(b.doing.ifEmpty { b.state.line }, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        if (b.waiting > 0) { HSpace(8); CountBadge(b.waiting, Tone.Warn) }
        HSpace(4)
        Text("›", style = MaterialTheme.typography.titleLarge, color = aide.faint)
    }
}

/**
 * A bot's own page (工单 #57, #63): who it is and how it stands at the top; its conversations (a coding agent, 工单 #67)
 * or its chat (a personal agent) in the middle, with the box to write to it at the foot (工单 #66); its names and faces
 * and its grant under them. What its machine reports in full (versions, checks, sign-in, the bridge) is on the devices tab.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun BotPage(model: AppModel, key: String, openSession: (String) -> Unit, edit: (Persona) -> Unit, openDevices: () -> Unit = {}, back: () -> Unit) {
    val world by model.world.collectAsState()
    val personas by model.personas.collectAsState()
    LaunchedEffect(Unit) { model.loadPersonas() }
    val now = Instant.now()
    val b = roster(world.machines, world.clients, world.sessions.values, now, personas.orEmpty()).all.firstOrNull { it.key == key }
    val m = b?.let { x -> world.machines.firstOrNull { it.machineId == x.machineId } }
    Scaffold(
        snackbarHost = { PageFeedback(model) },
        topBar = {
            TopAppBar(
                title = { Text(b?.name.orEmpty(), maxLines = 1, overflow = TextOverflow.Ellipsis) },
                navigationIcon = { IconButton(onClick = back) { Icon(Icons.AutoMirrored.Filled.ArrowBack, t("Back", "返回")) } },
            )
        },
    ) { pad ->
        if (b == null) {
            Centered { Text(t("This bot is gone.", "这个 bot 已经不在了。")) }
            return@Scaffold
        }
        val chat = if (!b.coding && b.client != null) rememberDirectChat(model, b.client.clientId) else null
        val conv = if (m != null && b.coding) rememberConversation(model, b, m) else null
        Column(Modifier.fillMaxSize().padding(pad)) {
            LazyColumn(Modifier.weight(1f), contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 16.dp)) {
                item(key = "head") {
                    Row(Modifier.fillMaxWidth().padding(vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                        BotAvatar(b, 56, toneColor(botTone(b)))
                        HSpace(14)
                        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                Text(b.name, style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                                HSpace(8)
                                StatusPill(b.state.word, botTone(b))
                            }
                            Text(b.state.line, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2)
                            if (b.coding && b.state.usable == Usable.NO && b.state.why != io.aide.android.model.NotReady.PAUSED) {
                                Text(t("Fix it on the Devices tab ›", "去设备页处理 ›"), style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary, modifier = Modifier.pressable(openDevices).padding(vertical = 2.dp))
                            }
                        }
                    }
                }
                if (chat != null) chatItems(chat, b)
                if (conv != null) conversationItems(model, conv, b, openSession)
                if (!b.coding) {
                    val work = world.sessions.values.filter { it.status != SessionStatus.ARCHIVED && it.createdBy == b.client?.clientId }.sortedByDescending { instant(it.updatedAt) }
                    item(key = "work-h") { Box(Modifier.padding(top = 12.dp)) { SectionHeader(t("Work it handed out", "它派出的任务"), count = work.size) } }
                    if (work.isEmpty()) item(key = "work-none") {
                        Text(t("Nothing from ${b.name} yet.", "${b.name} 还没有派出过任务。"), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(vertical = 8.dp, horizontal = 4.dp))
                    }
                    work.forEachIndexed { i, s -> item(key = "s-${s.sessionId}", contentType = "session") { Box(Modifier.groupRow(i, work.size)) { SessionRow(s, world) { openSession(s.sessionId) } } } }
                }
                if (m != null && b.coding) item(key = "identity") { Identity(b, m, edit) }
                b.client?.let { c ->
                    item(key = "grant") {
                        Column(Modifier.fillMaxWidth().padding(top = 12.dp)) {
                            SectionHeader(t("Its grant", "接入凭据"))
                            Column(Modifier.fillMaxWidth().aideCard().padding(horizontal = 16.dp, vertical = 12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                                Text(c.label, style = MaterialTheme.typography.bodyMedium)
                                instantOrNull(c.expiresAt)?.takeIf { c.herMachineId.isEmpty() }?.let { Text(t("Valid until ${it.toString().take(10)}", "有效期至 ${it.toString().take(10)}"), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                                Text(t("Its kind, projects and expiry are set in Aide on a computer: Settings › Access.", "它的类别、可用项目和有效期在电脑上的 Aide 里设置：设置 › 授权。"), style = MaterialTheme.typography.labelSmall, color = aide.faint)
                            }
                        }
                    }
                }
            }
            if (conv != null && m != null) ConversationComposer(model, conv, b, m)
            if (chat != null) ChatCompose(model, chat, b)
        }
    }
}

/** A coding agent's conversations on its page (工单 #67): the chosen one (this phone's), the composer's choices, what was said. */
class Conversation(val botKey: String) {
    var chosen by mutableStateOf<String?>(null)
    var params by mutableStateOf<ConvParams?>(null)
    var state by mutableStateOf<ComposerState>(ComposerState.Idle)
    var showArchived by mutableStateOf(false)
}

@Composable
private fun rememberConversation(model: AppModel, b: Bot, m: Machine): Conversation {
    val c = remember(b.key) { Conversation(b.key).also { it.chosen = model.chosenConversation(b.key) } }
    return c
}

private fun LazyListScope.conversationItems(model: AppModel, c: Conversation, b: Bot, openSession: (String) -> Unit) {
    item(key = "conv") {
        val world by model.world.collectAsState()
        val scope = rememberCoroutineScope()
        val convs = botConversations(b.machineId, b.agent, world.sessions.values)
        val cur = currentConversation(convs, c.chosen)
        val now = Instant.now()
        Column(Modifier.fillMaxWidth().padding(top = 4.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            // Rule 3: which conversation the next message goes on in.
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(if (cur != null) t("Talking in", "正在聊") else if (convs.open.isEmpty()) t("Write the first message to start", "写第一条，就开始和它的对话") else t("The next message starts a new conversation", "下一条会开新对话"),
                        style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    if (cur != null) Text("${displayTitle(cur)} · ${t("started", "开始于")} ${agoText(instant(cur.createdAt), now)}", style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.pressable { openSession(cur.sessionId) })
                }
                if (cur != null) Row(
                    Modifier.background(aide.fill2, CircleShape).pressable { c.chosen = "new"; model.chooseConversation(b.key, "new"); c.state = ComposerState.Idle }.padding(horizontal = 10.dp, vertical = 6.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Icon(Icons.Filled.Add, null, Modifier.size(16.dp))
                    HSpace(4)
                    Text(t("New conversation", "开启新对话"), style = MaterialTheme.typography.labelLarge)
                }
            }
            SectionHeader(t("Conversations", "对话"), count = convs.open.size)
            if (convs.open.isEmpty() && convs.archived.isEmpty()) Text(t("Nothing with ${b.name} yet: write below.", "还没和 ${b.name} 聊过：在下面写一条。"), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(4.dp))
            val rows = convs.open + if (c.showArchived) convs.archived else emptyList()
            rows.forEachIndexed { i, s ->
                Box(Modifier.groupRow(i, rows.size)) {
                    ConversationRow(s, s.sessionId == cur?.sessionId, world, onChoose = { c.chosen = s.sessionId; model.chooseConversation(b.key, s.sessionId) }, onOpen = { openSession(s.sessionId) }, onArchive = {
                        scope.launch { model.archive(s, s.status != SessionStatus.ARCHIVED)?.let { why -> c.state = ComposerState.Failed(why) } }
                    })
                }
            }
            if (convs.archived.isNotEmpty()) Text(
                if (c.showArchived) t("Hide archived", "收起已归档") else t("Archived (${convs.archived.size})", "已归档（${convs.archived.size}）"),
                style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary, modifier = Modifier.pressable { c.showArchived = !c.showArchived }.padding(vertical = 6.dp, horizontal = 4.dp),
            )
            if (convs.others.isNotEmpty()) {
                SectionHeader(t("Handed to it by other agents", "别的 agent 派给它的"), count = convs.others.size)
                convs.others.forEachIndexed { i, s -> Box(Modifier.groupRow(i, convs.others.size)) { SessionRow(s, world) { openSession(s.sessionId) } } }
            }
        }
    }
}

@Composable
private fun ConversationRow(s: Session, current: Boolean, world: io.aide.android.model.World, onChoose: () -> Unit, onOpen: () -> Unit, onArchive: () -> Unit) {
    Row(Modifier.fillMaxWidth().background(if (current) aide.fill else Color.Transparent).pressable(onChoose).padding(start = 16.dp, end = 4.dp, top = 10.dp, bottom = 10.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(displayTitle(s), style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                if (current) { HSpace(6); StatusPill(t("Current", "当前"), Tone.Live) }
            }
            Text("${stateWord(s)} · ${agoText(instant(s.updatedAt), Instant.now())}", style = MaterialTheme.typography.labelSmall, color = aide.faint, maxLines = 1)
        }
        TextLink(t("Open", "打开"), onOpen)
        TextLink(if (s.status == SessionStatus.ARCHIVED) t("Unarchive", "取消归档") else t("Archive", "归档"), onArchive)
    }
}

@Composable
private fun TextLink(text: String, onClick: () -> Unit) {
    Text(text, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.primary, modifier = Modifier.pressable(onClick).padding(horizontal = 8.dp, vertical = 6.dp))
}

private fun stateWord(s: Session): String = when (s.status) {
    SessionStatus.RUNNING -> t("Running", "进行中")
    SessionStatus.WAITING_INPUT -> t("Waiting for you", "等你回答")
    SessionStatus.ARCHIVED -> t("Archived", "已归档")
    SessionStatus.CRASHED -> t("Failed", "出错")
    else -> t("Idle", "空闲")
}

/** The box to write to a coding agent (工单 #66), going on in its conversation or starting one (工单 #67). */
@Composable
private fun ConversationComposer(model: AppModel, c: Conversation, b: Bot, m: Machine) {
    val world by model.world.collectAsState()
    val scope = rememberCoroutineScope()
    val convs = botConversations(b.machineId, b.agent, world.sessions.values)
    val cur = currentConversation(convs, c.chosen)
    val projects = world.projects.filter { it.machineId == m.machineId && (it.agents.isEmpty() || b.agent in it.agents) }
    val persona = b.personas.firstOrNull()
    val base = cur?.let { paramsOf(it) } ?: ConvParams(persona?.projectId?.ifEmpty { null } ?: projects.firstOrNull()?.projectId.orEmpty(), persona?.model.orEmpty(), persona?.effort.orEmpty(), persona?.mode.orEmpty())
    LaunchedEffect(cur?.sessionId, c.chosen) { c.params = base }
    val p = c.params ?: base
    val a = m.status?.agents?.firstOrNull { it.name == b.agent }
    var text by rememberSaveable(b.key) { mutableStateOf("") }
    var key by rememberSaveable(b.key) { mutableStateOf(UUID.randomUUID().toString()) }
    val attach = rememberAttachments(b.key)
    val stuck = cur?.let { stuckWhy(it, world.projects, m) }
    val changed = cur?.let { changedParams(it, p) }.orEmpty()
    val fresh = cur == null || changed.isNotEmpty()
    val names = mapOf("project" to t("project", "项目"), "model" to t("model", "模型"), "effort" to t("effort", "推理强度"), "mode" to t("mode", "模式"))
    fun send(contextOf: Session? = null) {
        val body = text.trim()
        if (body.isEmpty() && attach.files.isEmpty()) return
        c.state = ComposerState.Sending
        scope.launch {
            val to = if (contextOf == null && !fresh) cur else null
            val prompt = contextOf?.let { withContext(it, displayTitle(it), body) } ?: body
            model.converse(m.machineId, b.agent, to, p, prompt, key, attach.wire()).fold({ s ->
                if (to == null) { c.chosen = s.sessionId; model.chooseConversation(b.key, s.sessionId) }
                c.state = ComposerState.Done(
                    when {
                        to != null -> t("Sent in “${displayTitle(to)}”", "已发到「${displayTitle(to)}」")
                        cur != null && changed.isNotEmpty() -> t("New conversation: you changed the ${changed.joinToString(", ") { names[it]!! }}", "已开新对话：换了${changed.joinToString("、") { names[it]!! }}")
                        else -> t("New conversation started", "已开新对话")
                    },
                )
                text = ""
                attach.clear()
                key = UUID.randomUUID().toString()
            }, { e -> c.state = ComposerState.Failed(e.message.orEmpty()) { send(contextOf) } })
        }
    }
    val why = when {
        b.state.usable == Usable.NO -> ComposerWhy(t("${b.state.word}: can't send now", "${b.state.word}：现在派不了"), warn = true)
        stuck == Stuck.ARCHIVED -> ComposerWhy(t("Archived: sending unarchives it first", "这条对话已归档：发消息会先取消归档，再接着聊"))
        stuck != null -> ComposerWhy(
            if (stuck == Stuck.PROJECT_GONE) t("Its project is gone: read only", "这条对话的项目已经不在了：只能看、不能续") else t("This agent is no longer there: read only", "这台机器上已经没有这个 agent：只能看、不能续"),
            warn = true, action = t("New one with its context", "带着它的上下文开新对话") to { send(cur) },
        )
        projects.isEmpty() -> ComposerWhy(t("No project here for this agent yet: add one in Aide on the computer.", "这台电脑上还没有给它用的项目：在电脑上的 Aide 里添加一个。"))
        b.state.usable == Usable.UNVERIFIED -> ComposerWhy(t("Not confirmed lately — it can be sent, but whether it is there is not checked", "还没确认状态——能派，但对方在不在没核实"))
        cur != null && changed.isNotEmpty() -> ComposerWhy(t("You changed the ${changed.joinToString(", ") { names[it]!! }}: the next message starts a new conversation", "换了${changed.joinToString("、") { names[it]!! }}：下一条会开新对话"))
        else -> null
    }
    Composer(
        text, { text = it; if (c.state is ComposerState.Done || c.state is ComposerState.Failed) c.state = ComposerState.Idle }, { send() },
        t("Message ${b.name}", "给 ${b.name} 发消息"), c.state,
        enabled = b.state.usable != Usable.NO && projects.isNotEmpty() && (stuck == null || stuck == Stuck.ARCHIVED),
        why = why,
        plus = listOf(PlusItem(t("New conversation", "开启新对话"), cur != null) { c.chosen = "new"; model.chooseConversation(b.key, "new") }),
        attach = attach,
        // The machine's work-order macros (!plan, !implement), and a new conversation (工单 #92).
        commands = listOf(
            SlashCommand("plan", t("Plan only: no changes until you say so", "只做计划：你点头前不改任何东西")) { text = "!plan " },
            SlashCommand("implement", t("Implement it, run the tests, and sum up", "直接实现，跑测试，最后小结")) { text = "!implement " },
        ) + (if (cur != null) listOf(SlashCommand("new", t("Start a new conversation", "开启新对话")) { c.chosen = "new"; model.chooseConversation(b.key, "new") }) else emptyList()),
        chips = {
            if (projects.isNotEmpty()) ChipPicker(t("Project", "项目"), p.projectId, projects.map { it.projectId to it.title() }) { c.params = p.copy(projectId = it) }
            if (a != null && a.models.isNotEmpty()) ChipPicker(t("Model", "模型"), p.model, listOf("" to t("Default model", "默认模型")) + a.models.map { it.id to it.name.ifEmpty { it.id } }) { c.params = p.copy(model = it, effort = "") }
            a?.models?.firstOrNull { it.id == p.model }?.efforts?.takeIf { it.isNotEmpty() }?.let { ef ->
                ChipPicker(t("Effort", "推理强度"), p.effort, listOf("" to t("Default effort", "默认强度")) + ef.map { it to it }) { c.params = p.copy(effort = it) }
            }
            if (a != null && a.modes.isNotEmpty()) ChipPicker(t("Mode", "模式"), p.mode, listOf("" to t("Default mode", "默认模式")) + a.modes.map { it.id to it.name.ifEmpty { it.id } }) { c.params = p.copy(mode = it) }
        },
    )
}

/**
 * Its names and faces (docs/personas.md): each is its name with a pencil to change it (工单 #65); none yet is one
 * short button, never a sentence standing in for a name.
 */
@Composable
private fun Identity(b: Bot, m: Machine, edit: (Persona) -> Unit) {
    Column(Modifier.fillMaxWidth().padding(top = 12.dp)) {
        SectionHeader(t("Names and faces", "名字与头像"))
        Column(Modifier.fillMaxWidth().aideCard()) {
            b.personas.forEach { p ->
                Row(Modifier.fillMaxWidth().pressable { edit(p) }.padding(horizontal = 16.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
                    PersonaAvatar(p, 32)
                    HSpace(10)
                    Column(Modifier.weight(1f)) {
                        Text(p.name, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.SemiBold)
                        Text(listOf(p.projectId, p.model, p.effort, p.mode).filter { it.isNotEmpty() }.joinToString(" · "), style = MaterialTheme.typography.labelSmall, color = aide.faint)
                    }
                    Icon(Icons.Filled.Edit, t("Edit ${p.name}", "修改「${p.name}」"), tint = MaterialTheme.colorScheme.primary, modifier = Modifier.size(18.dp))
                }
            }
            Row(
                Modifier.fillMaxWidth().pressable { edit(Persona(icon = "robot", color = personaColors.first(), machineId = m.machineId, agent = b.agent, name = if (b.personas.isEmpty()) b.name else "")) }.padding(horizontal = 16.dp, vertical = 12.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Icon(Icons.Filled.Add, null, tint = MaterialTheme.colorScheme.primary, modifier = Modifier.size(18.dp))
                HSpace(8)
                Text(if (b.personas.isEmpty()) t("Give it a name and a face", "给它起名字、换头像") else t("Another name and face", "再加一个名字和头像"), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.primary)
            }
        }
    }
}

/** The owner's conversation with a personal agent: its direct room on its host, read again every few seconds. */
class DirectChat {
    var room by mutableStateOf<io.aide.android.api.Room?>(null)
    var messages by mutableStateOf<List<io.aide.android.api.RoomMessage>>(emptyList())
    var failed by mutableStateOf<String?>(null)
    var state by mutableStateOf<ComposerState>(ComposerState.Idle)
}

@Composable
private fun rememberDirectChat(model: AppModel, clientId: String): DirectChat {
    val chat = remember(clientId) { DirectChat() }
    LaunchedEffect(clientId) {
        // Why it can't be had is said here, on this page (工单 #60, #61), in the person's words.
        val r = model.openDirectRoomOr(clientId) { chat.failed = it }
        chat.room = r
        while (r != null) {
            model.roomMessages(r)?.let { chat.messages = it }
            delay(3_000)
        }
    }
    return chat
}

/** What became of the owner's message for a member, in words (工单 #71: sent → it took it → it answered; #70: queued, quiet). */
fun deliveryWords(state: String, name: String, reason: String, reminded: Boolean): Pair<String, Boolean> = when (state) {
    "ROOM_DELIVERY_STATE_SENT" -> if (reminded) t("$name has been quiet for two minutes: you were told", "$name 两分钟没动静，已提醒你") to true else t("Sent: waiting for $name to take it", "已发出：等 $name 取信") to false
    "ROOM_DELIVERY_STATE_TAKEN" -> if (reminded) t("$name has been quiet for two minutes: you were told", "$name 两分钟没动静，已提醒你") to true else t("$name took it", "$name 已取信") to false
    "ROOM_DELIVERY_STATE_QUEUED" -> t("Queued: as many bots as can work at once are busy; $name is woken when its turn comes", "排队中：同时在忙的 bot 已到上限，轮到时自动叫醒 $name") to false
    "ROOM_DELIVERY_STATE_REPLIED" -> t("$name answered", "$name 已回复") to false
    else -> reason to true
}

private fun LazyListScope.chatItems(chat: DirectChat, b: Bot) {
    item(key = "chat-h") { SectionHeader(t("Conversation", "对话")) }
    chat.failed?.let { why -> item(key = "chat-failed") { Text(why, style = MaterialTheme.typography.bodySmall, color = Warning) } }
    if (chat.failed == null && chat.room != null && chat.messages.isEmpty()) item(key = "chat-none") {
        Text(t("Write to ${b.name}: it answers here.", "给 ${b.name} 写点什么：它的回信会出现在这里。"), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(vertical = 8.dp, horizontal = 4.dp))
    }
    chat.messages.forEach { m ->
        item(key = "m-${m.messageId}", contentType = "chat") {
            val mine = m.from == "owner"
            Column(Modifier.fillMaxWidth().padding(vertical = 4.dp), horizontalAlignment = if (mine) Alignment.End else Alignment.Start, verticalArrangement = Arrangement.spacedBy(3.dp)) {
                if (!mine) Text(m.fromName, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                Box(Modifier.fillMaxWidth(0.82f).wrapContentWidth(if (mine) Alignment.End else Alignment.Start)
                    .background(if (mine) aide.bubble else aide.inset, RoundedCornerShape(16.dp)).padding(horizontal = 12.dp, vertical = 8.dp)) {
                    Text(m.text, style = MaterialTheme.typography.bodyMedium)
                }
                if (mine) m.deliveries.forEach { d ->
                    val (words, bad) = deliveryWords(d.state, b.name, d.reason, d.reminded)
                    Text(words, style = MaterialTheme.typography.labelSmall, color = if (bad) Warning else aide.faint)
                }
            }
        }
    }
}

@Composable
private fun ChatCompose(model: AppModel, chat: DirectChat, b: Bot) {
    val scope = rememberCoroutineScope()
    var text by rememberSaveable { mutableStateOf("") }
    var key by rememberSaveable { mutableStateOf(UUID.randomUUID().toString()) }
    fun send() {
        val r = chat.room ?: return
        chat.state = ComposerState.Sending
        scope.launch {
            model.postRoomMessageOr(r, text.trim(), key, failed = { chat.state = ComposerState.Failed(it) { send() } })?.let { m ->
                chat.messages = chat.messages.filter { it.messageId != m.messageId } + m
                text = ""
                key = UUID.randomUUID().toString()
                chat.state = ComposerState.Idle
            }
        }
    }
    Composer(text, { text = it }, { send() }, t("Message ${b.name}", "给 ${b.name} 发消息"), chat.state, enabled = chat.room != null)
}
