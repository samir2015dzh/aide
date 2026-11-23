package io.aide.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
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
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import io.aide.android.api.Persona
import io.aide.android.model.AppModel
import io.aide.android.model.Bot
import io.aide.android.model.BotLook
import io.aide.android.model.BotMood
import io.aide.android.model.agentTitle
import io.aide.android.model.botColors
import io.aide.android.model.botMood
import io.aide.android.model.botSeed
import io.aide.android.model.botShapes
import io.aide.android.model.colorName
import io.aide.android.model.personaChoices
import io.aide.android.model.personaLook
import io.aide.android.model.personaReady
import io.aide.android.model.shapeName
import io.aide.android.model.withLook
import io.aide.android.api.title
import kotlinx.coroutines.launch
import java.time.Instant

// Contacts (docs/personas.md): a name, an avatar and presets for one coding agent on one machine, and the session they
// talk in. Made and changed from the bot's page (工单 #57): there is no Hand Work To page beside the roster.

/** A contact's face on its bot's page: its own look, its bot's state (工单 #83). */
@Composable
fun PersonaAvatar(b: Bot, p: Persona, size: Int = 40) {
    GrokMark(botSeed(b), personaLook(p, botSeed(b)), botMood(b, LocalSessions.current, Instant.now()), size)
}

/**
 * The one avatar editor (工单 #83), as the desktop's (BotArk's BotAvatarEditorDialog, its Bot tab): the character
 * large and live on its stage (it looks where a finger goes, a tap or a pick pokes it), the 26 shapes five a row,
 * resting, and the 11 colours.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun AvatarEditor(seed: String, look: BotLook, onChange: (BotLook) -> Unit) {
    var poke by remember { mutableIntStateOf(0) }
    val dark = isSystemInDarkTheme()
    val line = MaterialTheme.colorScheme.onSurface
    Column(Modifier.fillMaxWidth(), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Box(
            Modifier.size(96.dp).clip(RoundedCornerShape(16.dp)).background(aide.fill2).semantics { contentDescription = t("Poke it", "戳它一下") }.pressable { poke++ },
            contentAlignment = Alignment.Center,
        ) { GrokMark(seed, look, BotMood("idle"), 72, follow = true, poke = poke) }
        Column(Modifier.clip(RoundedCornerShape(12.dp)).background(aide.fill2).padding(8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            botShapes.chunked(5).forEach { row ->
                Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    row.forEach { s ->
                        val on = s == look.shape
                        Box(
                            Modifier.size(48.dp).clip(RoundedCornerShape(12.dp)).background(if (on) aide.card else Color.Transparent)
                                .then(if (on) Modifier.border(2.dp, line, RoundedCornerShape(12.dp)) else Modifier)
                                .semantics { contentDescription = shapeName(s); selected = on }
                                .pressable { onChange(look.copy(shape = s)); poke++ },
                            contentAlignment = Alignment.Center,
                        ) { GrokMark(seed, look.copy(shape = s), BotMood("idle"), 36, still = true) }
                    }
                }
            }
        }
        FlowRow(horizontalArrangement = Arrangement.spacedBy(4.dp, Alignment.CenterHorizontally), modifier = Modifier.fillMaxWidth()) {
            botColors.forEach { c ->
                val on = c.id == look.color
                Box(
                    Modifier.size(26.dp).then(if (on) Modifier.border(2.dp, line, CircleShape) else Modifier).padding(4.dp)
                        .background(Color(if (dark) c.dark else c.light), CircleShape)
                        .semantics { contentDescription = colorName(c.id); selected = on }
                        .pressable { onChange(look.copy(color = c.id)); poke++ },
                )
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
    // Its look (工单 #83): the seed is its bot's; the look kept is the one shown (the seed's until another is picked).
    val seed = "m:${p.machineId}/${p.agent}"
    val look = personaLook(p, seed)
    // What can't be picked any more falls back to the first that can.
    if (c.agent == null && c.agents.isNotEmpty()) p = p.copy(agent = c.agents.first().name, model = "", effort = "", mode = "")
    if (c.projects.none { it.projectId == p.projectId } && c.projects.isNotEmpty()) p = p.copy(projectId = c.projects.first().projectId)
    val scope = rememberCoroutineScope()
    var busy by remember { mutableStateOf(false) }
    // What saving says is said here, on this page (工单 #61): its failure under the button, never later on the tabs.
    var error by remember { mutableStateOf<String?>(null) }
    // Known before anything is filled in: the server keeps no contacts (工单 #61 §2).
    val unavailable by model.personasError.collectAsState()
    Scaffold(
        snackbarHost = { PageFeedback(model) },
        topBar = {
            TopAppBar(
                title = { Text(if (start.personaId.isEmpty()) t("Add Contact", "添加联系人") else t("Edit Contact", "编辑联系人")) },
                navigationIcon = { IconButton(onClick = { done(null) }) { Icon(Icons.AutoMirrored.Filled.ArrowBack, t("Back", "返回")) } },
            )
        },
    ) { pad ->
        Column(Modifier.fillMaxSize().padding(pad).verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            unavailable?.let { why ->
                Text(why, style = MaterialTheme.typography.bodySmall, color = Warning, modifier = Modifier.fillMaxWidth().aideInset().padding(12.dp))
            }
            OutlinedTextField(p.name, { p = p.copy(name = it.take(64)) }, Modifier.fillMaxWidth(), label = { Text(t("Name", "名字")) }, singleLine = true)
            SectionHeader(t("Avatar", "头像"))
            AvatarEditor(seed, look) { p = p.withLook(it) }
            SectionHeader(t("Machine", "机器"))
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                world.machines.forEach { m ->
                    FilterChip(p.machineId == m.machineId, { p = p.copy(machineId = m.machineId, agent = "", projectId = "", model = "", effort = "", mode = "") }, label = { Text(m.machineName) })
                }
            }
            SectionHeader(t("Agent", "Agent"))
            if (c.agents.isEmpty()) Text(t("No coding agent on this machine.", "这台机器上没有可用的编程 Agent。"), color = MaterialTheme.colorScheme.onSurfaceVariant)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                c.agents.forEach { a -> FilterChip(p.agent == a.name, { p = p.copy(agent = a.name, projectId = "", model = "", effort = "", mode = "") }, label = { Text(if (a.pause != null) agentTitle(a.name) + t(" · paused", " · 已暂停") else agentTitle(a.name)) }) }
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
                    error = null
                    scope.launch { model.savePersonaOr(p.withLook(look)) { error = it }.also { busy = false }?.let { done(it) } }
                }) { Text(if (busy) t("Saving…", "正在保存…") else t("Save", "保存")) }
                if (start.personaId.isNotEmpty()) {
                    OutlinedButton(enabled = !busy, onClick = { scope.launch { if (model.deletePersona(p)) done(null) } }) { Text(t("Delete Contact", "删除联系人")) }
                }
            }
            error?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = Warning) }
        }
    }
}
