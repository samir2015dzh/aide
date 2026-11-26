package io.aide.android.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.aide.android.model.AppModel
import io.aide.android.model.AsideClient
import io.aide.android.model.agoText
import io.aide.android.model.botCounts
import io.aide.android.model.instantOrNull
import io.aide.android.model.machineAgents
import io.aide.android.model.roster
import java.time.Instant

// The devices (工单 #63): what left the Bot tab — the counts at the top, and each machine as a device: online, version,
// what runs, each coding agent's connection in full (versions, parts, sign-in, the bridge plugin, pause, checked when),
// checking again and the hidden ones, and Aide's own client connected there (工单 #64: a connection, no bot).

@Composable
fun DevicesTab(model: AppModel, openSession: (String) -> Unit) {
    val world by model.world.collectAsState()
    val personas by model.personas.collectAsState()
    LaunchedEffect(Unit) { model.loadPersonas() }
    val now = Instant.now()
    val r = roster(world.machines, world.clients, world.sessions.values, now, personas.orEmpty())
    val counts = botCounts(r.all)
    val list = rememberLazyListState()
    Column(Modifier.fillMaxSize()) {
        PageTitle(t("Machines", "机器"), if (r.all.isEmpty()) null else counts.text, collapsed = list.collapsed())
        SyncLine(model)
        CompositionLocalProvider(LocalScrolling provides list.isScrollInProgress) {
            LazyColumn(Modifier.fillMaxSize(), state = list, contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 4.dp, bottom = 24.dp)) {
                if (r.all.isNotEmpty()) item(key = "counts") {
                    // The counts (工单 #56), of the same state as each bot: ready over all, the unknown apart.
                    Row(Modifier.fillMaxWidth().padding(top = 4.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        StatTile(t("Ready", "可用"), "${counts.ready}", Modifier.weight(1f), of = "${counts.total}", tone = if (counts.ready > 0) Tone.Good else Tone.Quiet)
                        StatTile(t("Unknown", "未知"), "${counts.unknown}", Modifier.weight(1f))
                        StatTile(t("Running", "运行中"), "${r.all.sumOf { it.running }}", Modifier.weight(1f), tone = Tone.Live)
                    }
                }
                if (world.machines.isEmpty() && world.watched) item(key = "none") {
                    Text(t("No machine yet: open Aide on a computer and log in with this account.", "还没有机器：在电脑上打开 Aide，用这个账号登录。"), color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(vertical = 12.dp))
                }
                r.machines.forEach { g ->
                    val m = g.machine
                    val clients = r.aside.filter { it.machineId == m.machineId }
                    item(key = "h-${m.machineId}", contentType = "machine-head") { Box(Modifier.padding(top = 12.dp)) { MachineHead(m, world.devices, Modifier.groupRow(0, 2)) } }
                    item(key = "a-${m.machineId}", contentType = "machine-agents") {
                        Column(Modifier.groupRow(1, 2).padding(horizontal = 12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                            g.bots.filter { it.coding }.forEach { b ->
                                Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                                    Row(verticalAlignment = Alignment.CenterVertically) {
                                        BotAvatar(b, 28, still = true)
                                        HSpace(10)
                                        Text(b.name, style = MaterialTheme.typography.titleSmall, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                                        BotPill(b)
                                    }
                                    machineAgents(m, now).firstOrNull { it.agent == b.agent }?.let { AgentWorkbench(model, m, it, openSession) }
                                }
                            }
                            clients.forEach { AideClientRow(it, now) }
                            MachineFoot(model, m, empty = g.bots.isEmpty())
                        }
                    }
                }
                val elsewhere = r.aside.filter { a -> a.machineId.isEmpty() || world.machines.none { it.machineId == a.machineId } }
                if (elsewhere.isNotEmpty()) {
                    item(key = "clients-h") { Box(Modifier.padding(top = 12.dp)) { SectionHeader(t("Aide clients", "Aide 客户端连接"), count = elsewhere.size) } }
                    elsewhere.forEachIndexed { i, a -> item(key = "c-${a.client.clientId}") { Box(Modifier.groupRow(i, elsewhere.size).padding(horizontal = 16.dp, vertical = 8.dp)) { AideClientRow(a, now) } } }
                }
            }
        }
    }
}

/** Aide's own client logged in on a machine (client@…): a connection of the account on that device, not an agent (工单 #64). */
@Composable
private fun AideClientRow(a: AsideClient, now: Instant) {
    val c = a.client
    Column(Modifier.fillMaxWidth().aideInset().padding(12.dp), verticalArrangement = Arrangement.spacedBy(3.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(t("Aide client", "Aide 客户端"), style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f))
            if (c.online) StatusPill(t("Connected now", "现在连着账号"), Tone.Good)
            else StatusPill(instantOrNull(c.lastSeen)?.let { t("Last ${agoText(it, now)}", "上次 ${agoText(it, now)}") } ?: t("Not yet", "还没连上过"), Tone.Quiet)
        }
        Text(t("Aide’s command line here, logged in as “${c.label}”: a connection of this device, not an agent.", "这台机器上的 Aide 命令行以「${c.label}」登录你的账号：它是这台设备的连接，不是 agent。"), style = MaterialTheme.typography.labelSmall, color = aide.faint)
    }
}
