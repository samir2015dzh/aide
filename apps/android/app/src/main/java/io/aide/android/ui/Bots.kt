package io.aide.android.ui

import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
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
import androidx.compose.material3.TextButton
import io.aide.android.model.personaColors
import io.aide.android.model.displayTitle
import io.aide.android.api.SessionStatus
import io.aide.android.api.title
import io.aide.android.model.AppModel
import io.aide.android.model.Bot
import io.aide.android.model.Usable
import io.aide.android.model.botCounts
import io.aide.android.model.instant
import io.aide.android.model.instantOrNull
import io.aide.android.model.machineAgents
import io.aide.android.model.roster
import java.time.Instant
import java.util.UUID
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

// The roster (工单 #57, BOTARK-REF 产品结构纲要), the app's first tab: every bot, synced from the account (personal
// agents) and the machines (their coding agents), a row each — its face with its state on the corner, its one name,
// its state as a pill, one line of what it does lately. A machine is a group's head (online, version, what runs) and
// foot (check again, hidden ones): the device view, no second list of its agents. A tap opens the bot's own page.

/** A bot's face: its coding agent's, or a personal agent's initial on its colour. */
@Composable
fun BotFace(b: Bot, size: Int, dot: Color? = null) {
    // The owner's contact for it is its face (docs/personas.md).
    b.personas.firstOrNull()?.let { p ->
        Box {
            PersonaAvatar(p, size)
            if (dot != null) Box(Modifier.align(Alignment.BottomEnd).size((size / 4).coerceAtLeast(9).dp).background(MaterialTheme.colorScheme.background, CircleShape).padding(2.dp).background(dot, CircleShape))
        }
        return
    }
    if (b.agent.isNotEmpty()) {
        if (dot != null) FaceWithDot(b.agent, size, dot) else AgentFace(b.agent, size)
        return
    }
    Box {
        Box(Modifier.size(size.dp).background(personalColor(b.name), CircleShape), contentAlignment = Alignment.Center) {
            Text(b.name.take(1).uppercase(), color = Color.White, fontWeight = FontWeight.SemiBold)
        }
        if (dot != null) Box(Modifier.align(Alignment.BottomEnd).size((size / 4).coerceAtLeast(9).dp).background(MaterialTheme.colorScheme.background, CircleShape).padding(2.dp).background(dot, CircleShape))
    }
}

private fun personalColor(name: String): Color {
    var h = 0
    for (c in name) h = (h * 31 + c.code) and 0xffff
    return Color.hsl((h % 360).toFloat(), 0.55f, 0.5f)
}

/** Its state as a pill, the same words as everywhere (工单 #56). */
fun botTone(b: Bot): Tone = when (b.state.usable) {
    Usable.YES -> Tone.Good
    Usable.UNVERIFIED -> if (b.state.why == io.aide.android.model.NotReady.CHECKING) Tone.Live else Tone.Quiet
    Usable.NO -> if (b.state.why == io.aide.android.model.NotReady.OFFLINE) Tone.Quiet else Tone.Warn
}

@Composable
fun BotsTab(model: AppModel, open: (String) -> Unit) {
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
    val now = Instant.now()
    val r = roster(world.machines, world.clients, world.sessions.values, now, personas.orEmpty())
    val counts = botCounts(r.all)
    val list = rememberLazyListState()
    Column(Modifier.fillMaxSize()) {
        PageTitle("Bot", if (r.all.isEmpty()) null else counts.text)
        SyncLine(model)
        CompositionLocalProvider(LocalScrolling provides list.isScrollInProgress) {
            LazyColumn(Modifier.fillMaxSize(), state = list, contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 4.dp, bottom = 24.dp)) {
                if (r.all.isNotEmpty()) item(key = "overview") {
                    // The counts at the top, of the same state as each row (工单 #56): ready over all, the unknown apart.
                    Row(Modifier.fillMaxWidth().padding(top = 4.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        StatTile(t("Ready", "可用"), "${counts.ready}", Modifier.weight(1f), of = "${counts.total}", tone = if (counts.ready > 0) Tone.Good else Tone.Quiet)
                        StatTile(t("Unknown", "未知"), "${counts.unknown}", Modifier.weight(1f))
                        StatTile(t("Running", "运行中"), "${r.all.sumOf { it.running }}", Modifier.weight(1f), tone = Tone.Live)
                    }
                }
                if (r.all.isEmpty() && world.watched) item {
                    Text(t("No bot yet: connect a coding agent on a computer (Aide › Settings › Connect), or a personal agent to your account.", "还没有 bot：先在电脑上的 Aide 里连接一个编码 agent（设置 › 连接），或把个人 agent 连到你的账号。"), color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(vertical = 12.dp))
                }
                if (r.personal.isNotEmpty()) {
                    item(key = "personal-h") { Box(Modifier.padding(top = 12.dp)) { SectionHeader(t("Personal agents · follow your account", "个人 agent · 跟账号走"), count = r.personal.size) } }
                    r.personal.forEachIndexed { i, b ->
                        item(key = b.key, contentType = "bot") { Box(Modifier.groupRow(i, r.personal.size)) { BotRow(b) { open(b.key) } } }
                    }
                }
                r.machines.forEach { g ->
                    val m = g.machine
                    val count = g.bots.size + 2
                    item(key = "h-${m.machineId}", contentType = "machine-head") {
                        Box(Modifier.animateItem(fadeInSpec = tween(Motion.Medium), placementSpec = tween(Motion.Medium, easing = Motion.EaseOut)).padding(top = 12.dp)) {
                            MachineHead(m, Modifier.groupRow(0, count))
                        }
                    }
                    g.bots.forEachIndexed { i, b ->
                        item(key = b.key, contentType = "bot") {
                            Box(Modifier.animateItem(fadeInSpec = tween(Motion.Medium), placementSpec = tween(Motion.Medium, easing = Motion.EaseOut)).groupRow(i + 1, count)) { BotRow(b) { open(b.key) } }
                        }
                    }
                    item(key = "f-${m.machineId}", contentType = "machine-foot") {
                        Box(Modifier.groupRow(count - 1, count)) { MachineFoot(model, m, empty = g.bots.isEmpty()) }
                    }
                }
            }
        }
    }
}

/** A roster row (BOTARK-REF §1): face with its state on the corner, the one name with its state as a pill, one line under it. */
@Composable
fun BotRow(b: Bot, onClick: () -> Unit) {
    Row(Modifier.fillMaxWidth().pressable(onClick).padding(horizontal = 16.dp, vertical = 12.dp), verticalAlignment = Alignment.CenterVertically) {
        BotFace(b, 40, toneColor(botTone(b)))
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
 * A bot's own page (工单 #57): who it is, where, and how it stands at the top; its work in the middle; a box to hand it
 * work at the foot; its workbench (its connection, sign-in, pause and Manage, or its grant) under its work.
 */
@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
fun BotPage(model: AppModel, key: String, openSession: (String) -> Unit, edit: (Persona) -> Unit, back: () -> Unit) {
    val world by model.world.collectAsState()
    val personas by model.personas.collectAsState()
    LaunchedEffect(Unit) { model.loadPersonas() }
    val now = Instant.now()
    val b = roster(world.machines, world.clients, world.sessions.values, now, personas.orEmpty()).all.firstOrNull { it.key == key }
    val m = b?.let { x -> world.machines.firstOrNull { it.machineId == x.machineId } }
    Scaffold(
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
        val work = world.sessions.values
            .filter { it.status != SessionStatus.ARCHIVED && if (b.coding) it.machineId == b.machineId && it.agent == b.agent else it.createdBy == b.client?.clientId }
            .sortedByDescending { instant(it.updatedAt) }
        Column(Modifier.fillMaxSize().padding(pad)) {
            LazyColumn(Modifier.weight(1f), contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 16.dp)) {
                item(key = "head") {
                    Row(Modifier.fillMaxWidth().padding(vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                        BotFace(b, 56, toneColor(botTone(b)))
                        HSpace(14)
                        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                Text(b.name, style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                                HSpace(8)
                                StatusPill(b.state.word, botTone(b))
                            }
                            Text(m?.machineName ?: t("Follows your account", "跟账号走"), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                            Text(b.state.line, style = MaterialTheme.typography.bodySmall, color = aide.faint, maxLines = 2)
                        }
                    }
                }
                item(key = "work-h") { SectionHeader(t("Its work", "它的任务"), count = work.size) }
                if (work.isEmpty()) item(key = "work-none") {
                    Text(
                        if (b.coding) t("Nothing for ${b.name} yet: write a task below.", "${b.name} 还没有任务：在下面写一个派给它。") else t("Nothing from ${b.name} yet.", "${b.name} 还没有派出过任务。"),
                        style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(vertical = 8.dp, horizontal = 4.dp),
                    )
                }
                work.forEachIndexed { i, s ->
                    item(key = "s-${s.sessionId}", contentType = "session") {
                        Box(Modifier.groupRow(i, work.size)) { SessionRow(s, world) { openSession(s.sessionId) } }
                    }
                }
                if (!b.coding) item(key = "chat") {
                    Text(t("Talking with ${b.name} directly comes with rooms; for now this lists the work it handed out.", "和 ${b.name} 直接对话会随协作房间一起上线；这里先列它派出、经手的任务。"), style = MaterialTheme.typography.bodySmall, color = aide.faint, modifier = Modifier.padding(top = 12.dp, start = 4.dp, end = 4.dp))
                }
                item(key = "bench-h") { Box(Modifier.padding(top = 12.dp)) { SectionHeader(t("Workbench", "工作台")) } }
                if (m != null && b.coding) {
                    item(key = "identity") { Identity(b, m, edit) }
                    item(key = "bench") {
                        machineAgents(m, now).firstOrNull { it.agent == b.agent }?.let { AgentWorkbench(model, m, it, openSession) }
                    }
                }
                b.client?.let { c ->
                    item(key = "grant") {
                        Column(Modifier.fillMaxWidth().padding(top = 8.dp).aideInset().padding(12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                            Text(t("Its grant", "接入凭据"), style = MaterialTheme.typography.labelLarge)
                            Text(c.label, style = MaterialTheme.typography.bodySmall)
                            instantOrNull(c.expiresAt)?.takeIf { c.herMachineId.isEmpty() }?.let { Text(t("Valid until ${it.toString().take(10)}", "有效期至 ${it.toString().take(10)}"), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                            Text(t("Its kind, projects and expiry are set in Aide on a computer: Settings › Access.", "它的类别、可用项目和有效期在电脑上的 Aide 里设置：设置 › 授权。"), style = MaterialTheme.typography.labelSmall, color = aide.faint)
                        }
                    }
                }
            }
            if (m != null && b.coding) Compose(model, b, m, openSession)
        }
    }
}

/** Handing it work from its page: a project, then the task; the session then opens (as Hand Work To does). */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun Compose(model: AppModel, b: Bot, m: Machine, openSession: (String) -> Unit) {
    val world by model.world.collectAsState()
    val scope = rememberCoroutineScope()
    val projects = world.projects.filter { it.machineId == m.machineId && (it.agents.isEmpty() || b.agent in it.agents) }
    var project by rememberSaveable { mutableStateOf("") }
    val chosen = projects.firstOrNull { it.projectId == project } ?: projects.firstOrNull()
    var text by rememberSaveable { mutableStateOf("") }
    var key by rememberSaveable { mutableStateOf(UUID.randomUUID().toString()) }
    var busy by remember { mutableStateOf(false) }
    // With a contact, writing goes on in the session it talks in (or starts one with its presets); null: a new task.
    var chosenVia by rememberSaveable { mutableStateOf<String?>(null) }
    val via = chosenVia?.takeIf { v -> v == "new" || b.personas.any { it.personaId == v } } ?: b.personas.firstOrNull()?.personaId ?: "new"
    val persona = b.personas.firstOrNull { it.personaId == via }
    Column(Modifier.fillMaxWidth().imePadding()) {
        HorizontalDivider(thickness = 0.5.dp, color = aide.separator)
        Column(Modifier.padding(start = 12.dp, end = 8.dp, top = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            when (b.state.usable) {
                Usable.NO -> Text(t("${b.state.word}: can't hand work now", "${b.state.word}：现在派不了"), style = MaterialTheme.typography.bodySmall, color = Warning)
                Usable.UNVERIFIED -> Text(t("Not confirmed lately — it can be sent, but whether it is there is not checked", "还没确认状态——能派，但对方在不在没核实"), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                Usable.YES -> {}
            }
            if (b.personas.isNotEmpty()) FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                b.personas.forEach { p -> FilterChip(selected = via == p.personaId, onClick = { chosenVia = p.personaId }, label = { Text(t("Go on as ${p.name}", "接着「${p.name}」")) }) }
                FilterChip(selected = via == "new", onClick = { chosenVia = "new" }, label = { Text(t("A new task", "开新任务")) })
            }
            persona?.let { p ->
                val s = world.sessions[p.sessionId]
                if (s != null) Text(t("Goes on in: ${displayTitle(s)}", "接着：${displayTitle(s)}"), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.primary, modifier = Modifier.pressable { openSession(s.sessionId) })
                else Text(t("Its next message starts a task with its presets", "下一条会按它的设置新开一个任务"), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            if (persona == null && projects.isEmpty()) Text(t("No project here for this agent yet: add one in Aide on the computer.", "这台电脑上还没有给它用的项目：在电脑上的 Aide 里添加一个。"), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            if (persona == null && projects.size > 1) FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                projects.forEach { pr -> FilterChip(selected = pr.projectId == chosen?.projectId, onClick = { project = pr.projectId }, label = { Text(pr.title()) }) }
            }
        }
        Row(Modifier.fillMaxWidth().padding(8.dp), verticalAlignment = Alignment.CenterVertically) {
            val can = b.state.usable != Usable.NO && (persona != null || chosen != null)
            OutlinedTextField(text, { text = it }, Modifier.weight(1f), placeholder = { Text(t("A task for ${b.name}…", "写给 ${b.name} 的任务…")) }, maxLines = 6, enabled = can)
            Spacer(Modifier.width(4.dp))
            IconButton(enabled = text.isNotBlank() && !busy && can, onClick = {
                busy = true
                scope.launch {
                    val sid = if (persona != null) model.talk(persona, text.trim(), key) else model.startTask(m.machineId, chosen!!.projectId, b.agent, text.trim(), key)
                    busy = false
                    if (sid != null) {
                        text = ""
                        key = UUID.randomUUID().toString()
                        openSession(sid)
                    }
                }
            }) { Icon(Icons.AutoMirrored.Filled.Send, t("Hand Work", "派活")) }
        }
    }
}

/** Its names and faces (docs/personas.md, once Hand Work To's contacts): the first is its name and face in the roster. */
@Composable
private fun Identity(b: Bot, m: Machine, edit: (Persona) -> Unit) {
    Column(Modifier.fillMaxWidth().padding(top = 8.dp, bottom = 8.dp).aideInset().padding(12.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(t("Names and faces", "名字与头像"), style = MaterialTheme.typography.labelLarge, modifier = Modifier.weight(1f))
            TextButton(onClick = { edit(Persona(icon = "robot", color = personaColors.first(), machineId = m.machineId, agent = b.agent, name = if (b.personas.isEmpty()) b.name else "")) }) { Text(t("Add", "新建")) }
        }
        if (b.personas.isEmpty()) Text(t("Give it a name, an avatar and presets to talk to it as that, in one session that goes on.", "给它起个名字、换个头像、设好项目和模型，就能以这个身份和它接着同一个会话聊。"), style = MaterialTheme.typography.bodySmall, color = aide.faint)
        b.personas.forEach { p ->
            Row(Modifier.fillMaxWidth().pressable { edit(p) }.padding(vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                PersonaAvatar(p, 32)
                HSpace(10)
                Column(Modifier.weight(1f)) {
                    Text(p.name, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.SemiBold)
                    Text(listOf(p.projectId, p.model, p.effort, p.mode).filter { it.isNotEmpty() }.joinToString(" · "), style = MaterialTheme.typography.labelSmall, color = aide.faint)
                }
                Text(t("Edit", "编辑"), style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary)
            }
        }
    }
}
