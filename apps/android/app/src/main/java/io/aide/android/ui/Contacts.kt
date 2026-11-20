package io.aide.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.filled.Add
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
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
import io.aide.android.api.Persona
import io.aide.android.model.AppModel
import io.aide.android.model.agentTitle
import io.aide.android.model.displayTitle
import io.aide.android.model.personaChoices
import io.aide.android.model.personaColor
import io.aide.android.model.personaColors
import io.aide.android.model.personaGlyph
import io.aide.android.model.personaIcons
import io.aide.android.model.personaReady
import kotlinx.coroutines.launch
import java.util.UUID

// Contacts (docs/personas.md): to hand work over, the owner picks a contact,
// then writes to it. The message goes on in the session the contact talks
// in, or starts one with its presets; the session opens as any other.

@Composable
fun PersonaAvatar(p: Persona, size: Int = 40) {
    Box(Modifier.size(size.dp).background(Color(personaColor(p)), CircleShape), contentAlignment = Alignment.Center) {
        Text(personaGlyph(p), color = Color.White, fontWeight = FontWeight.SemiBold)
    }
}

/** The contacts, to pick one to write to. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ContactsPage(model: AppModel, open: (Persona) -> Unit, add: () -> Unit, back: () -> Unit) {
    val world by model.world.collectAsState()
    val list by model.personas.collectAsState()
    LaunchedEffect(Unit) { model.loadPersonas() }
    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(t("Hand Work To", "派活给谁")) },
                navigationIcon = { IconButton(onClick = back) { Icon(Icons.AutoMirrored.Filled.ArrowBack, t("Back", "返回")) } },
                actions = { IconButton(onClick = add) { Icon(Icons.Filled.Add, t("Add Contact", "添加联系人")) } },
            )
        },
    ) { pad ->
        LazyColumn(Modifier.fillMaxSize().padding(pad)) {
            if (list?.isEmpty() == true) {
                item {
                    Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        Text(t("No contacts yet. Add one: name it, pick its machine, project and agent.", "还没有联系人。添加一个：给它起名，选好机器、项目和 Agent。"), color = MaterialTheme.colorScheme.onSurfaceVariant)
                        Button(onClick = add) { Text(t("Add Contact", "添加联系人")) }
                    }
                }
            }
            items(list.orEmpty(), key = { it.personaId }) { p ->
                Row(Modifier.fillMaxWidth().clickable { open(p) }.padding(horizontal = 16.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
                    PersonaAvatar(p)
                    Spacer(Modifier.width(12.dp))
                    Column(Modifier.weight(1f)) {
                        Text(p.name, fontWeight = FontWeight.SemiBold)
                        Text(
                            "${agentTitle(p.agent)} · ${p.projectId} · ${world.machineName(p.machineId)}",
                            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis,
                        )
                    }
                }
            }
        }
    }
}

/** Writing to a contact; the session it went to then opens. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ContactPage(model: AppModel, id: String, openSession: (String) -> Unit, edit: (Persona) -> Unit, back: () -> Unit) {
    val world by model.world.collectAsState()
    val list by model.personas.collectAsState()
    val p = list?.firstOrNull { it.personaId == id }
    val scope = rememberCoroutineScope()
    var text by rememberSaveable { mutableStateOf("") }
    var key by rememberSaveable { mutableStateOf(UUID.randomUUID().toString()) }
    var busy by remember { mutableStateOf(false) }
    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        if (p != null) PersonaAvatar(p, 32)
                        Spacer(Modifier.width(8.dp))
                        Text(p?.name.orEmpty(), maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                },
                navigationIcon = { IconButton(onClick = back) { Icon(Icons.AutoMirrored.Filled.ArrowBack, t("Back", "返回")) } },
                actions = { if (p != null) TextButton(onClick = { edit(p) }) { Text(t("Edit", "编辑")) } },
            )
        },
    ) { pad ->
        if (p == null) {
            Centered { Text(t("This contact is gone.", "这个联系人已经不在了。")) }
            return@Scaffold
        }
        Column(Modifier.fillMaxSize().padding(pad)) {
            Column(Modifier.weight(1f).padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Labeled(t("Works with", "用"), listOf(agentTitle(p.agent), p.model, p.effort).filter { it.isNotEmpty() }.joinToString(" · "))
                Labeled(t("In", "在"), "${p.projectId} · ${world.machineName(p.machineId)}")
                if (p.mode.isNotEmpty()) Labeled(t("Mode", "模式"), p.mode)
                // The real session underneath, which the next message goes on in.
                val s = world.sessions[p.sessionId]
                if (s != null) {
                    TextButton(onClick = { openSession(s.sessionId) }) { Text(t("Working in: ", "正在用的任务：") + displayTitle(s) + " ›") }
                } else {
                    Text(t("The next message starts a new task with its settings.", "下一条消息会按它的设置新开一个任务。"), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
            Row(Modifier.fillMaxWidth().imePadding().padding(8.dp), verticalAlignment = Alignment.CenterVertically) {
                OutlinedTextField(text, { text = it }, Modifier.weight(1f), placeholder = { Text(t("Message ${p.name}", "发给 ${p.name}")) }, maxLines = 6)
                IconButton(enabled = text.isNotBlank() && !busy, onClick = {
                    busy = true
                    scope.launch {
                        val sid = model.talk(p, text.trim(), key)
                        busy = false
                        if (sid != null) {
                            text = ""
                            key = UUID.randomUUID().toString()
                            openSession(sid)
                        }
                    }
                }) { Icon(Icons.AutoMirrored.Filled.Send, t("Send", "发送")) }
            }
        }
    }
}

/** Adding (an empty contact) or changing one. */
@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
fun ContactEditPage(model: AppModel, start: Persona, done: (Persona?) -> Unit) {
    val world by model.world.collectAsState()
    var p by remember { mutableStateOf(start.copy(machineId = start.machineId.ifEmpty { world.machines.firstOrNull { it.local }?.machineId ?: world.machines.firstOrNull()?.machineId.orEmpty() })) }
    val c = personaChoices(world, p)
    // What can't be picked any more falls back to the first that can.
    if (c.agent == null && c.agents.isNotEmpty()) p = p.copy(agent = c.agents.first().name, model = "", effort = "", mode = "")
    if (c.projects.none { it.projectId == p.projectId } && c.projects.isNotEmpty()) p = p.copy(projectId = c.projects.first().projectId)
    val scope = rememberCoroutineScope()
    var busy by remember { mutableStateOf(false) }
    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(if (start.personaId.isEmpty()) t("Add Contact", "添加联系人") else t("Edit Contact", "编辑联系人")) },
                navigationIcon = { IconButton(onClick = { done(null) }) { Icon(Icons.AutoMirrored.Filled.ArrowBack, t("Back", "返回")) } },
            )
        },
    ) { pad ->
        Column(Modifier.fillMaxSize().padding(pad).verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedTextField(p.name, { p = p.copy(name = it.take(64)) }, Modifier.fillMaxWidth(), label = { Text(t("Name", "名字")) }, singleLine = true)
            SectionHeader(t("Avatar", "头像"))
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                personaIcons.forEach { (id, glyph) -> FilterChip(p.icon == id, { p = p.copy(icon = id) }, label = { Text(glyph) }) }
            }
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                personaColors.forEach { col ->
                    FilterChip(p.color == col, { p = p.copy(color = col) }, label = { PersonaAvatar(Persona(name = " ", color = col), 16) })
                }
            }
            SectionHeader(t("Machine", "机器"))
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                world.machines.forEach { m ->
                    FilterChip(p.machineId == m.machineId, { p = p.copy(machineId = m.machineId, agent = "", projectId = "", model = "", effort = "", mode = "") }, label = { Text(m.machineName) })
                }
            }
            SectionHeader(t("Agent", "Agent"))
            if (c.agents.isEmpty()) Text(t("No coding agent on this machine.", "这台机器上没有可用的编程 Agent。"), color = MaterialTheme.colorScheme.onSurfaceVariant)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                c.agents.forEach { a -> FilterChip(p.agent == a.name, { p = p.copy(agent = a.name, projectId = "", model = "", effort = "", mode = "") }, label = { Text(agentTitle(a.name)) }) }
            }
            SectionHeader(t("Project", "项目"))
            if (c.agent != null && c.projects.isEmpty()) Text(t("No project here for this agent: add one on a computer.", "这里还没有给它用的项目：在电脑上添加。"), color = MaterialTheme.colorScheme.onSurfaceVariant)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                c.projects.forEach { pr -> FilterChip(p.projectId == pr.projectId, { p = p.copy(projectId = pr.projectId) }, label = { Text(pr.projectId) }) }
            }
            val a = c.agent
            if (a != null && a.models.isNotEmpty()) {
                SectionHeader(t("Model", "模型"))
                FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    FilterChip(p.model.isEmpty(), { p = p.copy(model = "", effort = "") }, label = { Text(t("Default", "默认")) })
                    a.models.forEach { m -> FilterChip(p.model == m.id, { p = p.copy(model = m.id, effort = "") }, label = { Text(m.name.ifEmpty { m.id }) }) }
                }
            }
            if (c.efforts.isNotEmpty()) {
                SectionHeader(t("Reasoning effort", "推理强度"))
                FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    FilterChip(p.effort.isEmpty(), { p = p.copy(effort = "") }, label = { Text(t("Default", "默认")) })
                    c.efforts.forEach { e -> FilterChip(p.effort == e, { p = p.copy(effort = e) }, label = { Text(e) }) }
                }
            }
            if (a != null && a.modes.isNotEmpty()) {
                // The agent's own per-session presets: nothing else.
                SectionHeader(t("Mode", "模式"))
                FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    FilterChip(p.mode.isEmpty(), { p = p.copy(mode = "") }, label = { Text(t("Default", "默认")) })
                    a.modes.forEach { m -> FilterChip(p.mode == m.id, { p = p.copy(mode = m.id) }, label = { Text(m.name.ifEmpty { m.id }) }) }
                }
            }
            Text(
                t("Changing its machine, project, agent, model, effort or mode starts a new task with the next message; a new name or avatar keeps the task. Deleting a contact keeps its tasks.",
                    "改机器、项目、Agent、模型、推理强度或模式后，下一条消息会新开任务；只改名字或头像会接着用原来的任务。删除联系人不会删除它的任务。"),
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Button(enabled = personaReady(p) && !busy, onClick = {
                    busy = true
                    scope.launch { model.savePersona(p).also { busy = false }?.let { done(it) } }
                }) { Text(t("Save", "保存")) }
                if (start.personaId.isNotEmpty()) {
                    OutlinedButton(enabled = !busy, onClick = { scope.launch { if (model.deletePersona(p)) done(null) } }) { Text(t("Delete Contact", "删除联系人")) }
                }
            }
        }
    }
}
