package io.aide.android.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.aide.android.api.Client
import io.aide.android.api.Machine
import io.aide.android.api.Persona
import io.aide.android.model.AppModel
import io.aide.android.model.Bot
import io.aide.android.model.instantOrNull
import io.aide.android.model.roster
import java.time.Instant

// A bot's settings on a page of their own (工单 #97): its page keeps who it is, what it is doing and talking to it; what is
// set about it is here, each once — its names and faces, its settings (#93), its grant, taking it off Aide. What each
// does is unchanged (#52, #82, #89, #93): only where it is shown moved. The controls are the app's own (#95); their new
// look waits for the Android Fluent design (工单 #106).

/** The page: from the bot page's Settings, back to it. */
@Composable
fun BotSettingsPage(model: AppModel, key: String, edit: (Persona) -> Unit, back: () -> Unit) {
    val world by model.world.collectAsState()
    val personas by model.personas.collectAsState()
    LaunchedEffect(Unit) { model.loadPersonas() }
    val b = roster(world.machines, world.clients, world.sessions.values, Instant.now(), personas.orEmpty()).all.firstOrNull { it.key == key }
    val m = b?.let { x -> world.machines.firstOrNull { it.machineId == x.machineId } }
    var removing by remember { mutableStateOf(false) }
    Scaffold(
        snackbarHost = { PageFeedback(model) },
        topBar = {
            NavBar(back) {
                Column(horizontalAlignment = Alignment.CenterHorizontally) {
                    Text(t("Settings", "设置"), maxLines = 1)
                    b?.let { Text(it.name, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis) }
                }
            }
        },
    ) { pad ->
        if (b == null) {
            Centered { Text(t("This bot is gone.", "这个 bot 已经不在了。")) }
            return@Scaffold
        }
        val agent = m?.status?.agents.orEmpty().firstOrNull { it.name == b.agent }
        LazyColumn(Modifier.fillMaxSize().padding(pad), contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 32.dp)) {
            if (m != null && b.coding) item(key = "names") { NamesAndFaces(b, m, edit) }
            // A coding agent's own settings, taking it off Aide among them (#93).
            if (m != null && b.coding && agent != null) item(key = "agent") { AgentSettings(model, b, m) }
            b.client?.let { c -> item(key = "grant") { GrantCard(c) } }
            // Any other is deleted from the roster as a long press there does (#82): a grant of its own revoked.
            if (!(m != null && b.coding && agent != null)) item(key = "remove") {
                Text(
                    if (b.key.startsWith("m:")) t("Remove from the roster…", "从名册移除…") else t("Delete…", "删除…"),
                    style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.error,
                    modifier = Modifier.padding(top = 20.dp).fillMaxWidth().aideCard().rowPress { removing = true }.padding(horizontal = 16.dp, vertical = 11.dp),
                )
            }
        }
    }
    if (removing && b != null) RemoveBotDialog(model, b, m?.machineName.orEmpty()) { removing = false }
}

/**
 * Its names and faces (docs/personas.md), once (工单 #97: the row in its settings and the block under them were two): each
 * is its face and name with a pencil (工单 #65); a row adds one — none yet, the first takes its name and face (工单 #83).
 */
@Composable
private fun NamesAndFaces(b: Bot, m: Machine, edit: (Persona) -> Unit) {
    Column(Modifier.fillMaxWidth()) {
        SectionHeader(t("Name and avatar", "名字和头像"))
        Column(Modifier.fillMaxWidth().aideCard()) {
            b.personas.forEach { p ->
                Row(Modifier.fillMaxWidth().rowPress { edit(p) }.padding(horizontal = 16.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
                    PersonaAvatar(b, p, 32)
                    HSpace(10)
                    Column(Modifier.weight(1f)) {
                        Text(p.name, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.SemiBold)
                        Text(listOf(p.projectId, p.model, p.effort, p.mode).filter { it.isNotEmpty() }.joinToString(" · "), style = MaterialTheme.typography.labelSmall, color = aide.faint)
                    }
                    Icon(Icons.Filled.Edit, t("Edit ${p.name}", "修改「${p.name}」"), tint = MaterialTheme.colorScheme.primary, modifier = Modifier.size(18.dp))
                }
                Hairline(Modifier.padding(start = 58.dp))
            }
            Row(Modifier.fillMaxWidth().rowPress { edit(newPersona(b, m)) }.padding(horizontal = 16.dp, vertical = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                Icon(Icons.Filled.Add, null, tint = MaterialTheme.colorScheme.primary, modifier = Modifier.size(18.dp))
                HSpace(8)
                Text(if (b.personas.isEmpty()) t("Give it a name and a face", "给它起名字、换头像") else t("Another name and face", "再加一个名字和头像"), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.primary)
            }
        }
    }
}

/** Its grant (工单 #97 补充: off its page, here): what it is called, until when; the rest is set on a computer. */
@Composable
private fun GrantCard(c: Client) {
    Column(Modifier.fillMaxWidth()) {
        SectionHeader(t("Its grant", "接入凭据"))
        Column(Modifier.fillMaxWidth().aideCard().padding(horizontal = 16.dp, vertical = 12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(c.label, style = MaterialTheme.typography.bodyMedium)
            instantOrNull(c.expiresAt)?.takeIf { c.herMachineId.isEmpty() }?.let { Text(t("Valid until ${it.toString().take(10)}", "有效期至 ${it.toString().take(10)}"), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
            Text(t("Its kind, projects and expiry are set in Aide on a computer: Settings › Access.", "它的类别、可用项目和有效期在电脑上的 Aide 里设置：设置 › 授权。"), style = MaterialTheme.typography.labelSmall, color = aide.faint)
        }
    }
}
