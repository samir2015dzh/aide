package io.aide.android.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import io.aide.android.api.AgentStatus
import io.aide.android.api.Machine
import io.aide.android.model.AppModel
import io.aide.android.model.Bot
import io.aide.android.model.agoText
import io.aide.android.model.instantOrNull
import io.aide.android.model.usageResetAt
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import kotlinx.coroutines.launch

// A coding agent's own settings (工单 #93), on its bot's settings page (工单 #97, BotSettings.kt), as the desktop's and as a
// connected agent's grant page is laid out: what it can use (its machine, its projects), its kind (told from what it is
// until the owner says otherwise, #52, kept on its machine), stopping it and taking it off Aide's lists; its names and
// faces are the page's own section. What only a grant has (expiry, the exec
// channel) is not shown; an agent with a grant of its own (DSH) keeps its kind on that grant.

private val whenFormat = DateTimeFormatter.ofLocalizedDateTime(FormatStyle.SHORT).withZone(ZoneId.systemDefault())

/** Claude's plan usage in one line with when it was read (工单 #86); kept after a refresh that failed, "last reading". */
fun usageLine(a: AgentStatus, now: Instant = Instant.now()): String {
    val u = a.usage ?: return ""
    if (u.windows.isEmpty()) return if (u.unavailable.isNotEmpty()) t("Plan usage not read yet", "套餐用量还没读到") else ""
    val label = { k: String -> when (k) { "five_hour" -> "5h"; "seven_day" -> t("Week", "周"); else -> k.replace("seven_day_", "7d ") } }
    val parts = u.windows.joinToString(" · ") { "${label(it.key)} ${Math.round(it.utilization)}%" }
    val at = instantOrNull(u.fetchedAt)?.let { agoText(it, now) }
    val read = when {
        at == null -> ""
        u.stale -> t(" · last reading, $at", " · 上次读数 · $at")
        else -> t(" · read $at", " · ${at}读数")
    }
    return t("Plan ", "套餐 ") + parts + read
}

@Composable
fun AgentSettings(model: AppModel, b: Bot, m: Machine) {
    val world = model.world.value
    val a = m.status?.agents.orEmpty().firstOrNull { it.name == b.agent } ?: return
    val scope = rememberCoroutineScope()
    val projects = world.projects.filter { it.machineId == m.machineId && (it.agents.isEmpty() || b.agent in it.agents) }
    var kindOpen by remember { mutableStateOf(false) }
    var pauseOpen by remember { mutableStateOf(false) }
    var hiding by remember { mutableStateOf(false) }
    val kindText = { k: String -> when (k) { "personal" -> t("Personal agent", "个人 Agent"); "local" -> t("Coding agent · ${m.machineName}", "本机编码 Agent · ${m.machineName}"); else -> t("Told from what it is · coding agent", "自动判断 · 本机编码 Agent") } }
    if (hiding) IosAlert(
        onDismissRequest = { hiding = false },
        title = t("Remove ${b.name} from Aide?", "把 ${b.name} 从 Aide 里移除？"),
        message = t("It leaves Aide's lists; nothing on ${m.machineName} is uninstalled or deleted, and its history stays. A recheck of the machine shows it again.", "它会从 Aide 的列表里拿掉；${m.machineName} 上什么都不会卸载或删除，它的历史也都在。到设备页重新检查这台机器，它就会回来。"),
        actions = listOf(
            AlertAction(t("Cancel", "取消"), AlertRole.Cancel) { hiding = false },
            AlertAction(t("Remove", "移除"), AlertRole.Destructive) { hiding = false; scope.launch { model.setAgentHidden(m.machineId, a.name, true) } },
        ),
    )
    Column(Modifier.fillMaxWidth()) {
        SectionHeader(t("Its settings", "它的设置"))
        Column(Modifier.fillMaxWidth().aideCard()) {
            SettingRow(t("Machine", "机器"), t("${m.machineName} (where it runs)", "${m.machineName}（它在这台上运行）"))
            Hairline(Modifier.padding(start = 16.dp))
            SettingRow(t("Projects", "项目"), projects.joinToString(t(", ", "、")) { world.projectTitle(it.machineId, it.projectId) }.ifEmpty { t("No project yet", "还没有项目") })
            usageLine(a).takeIf { b.agent == "claude" && it.isNotEmpty() }?.let {
                Hairline(Modifier.padding(start = 16.dp))
                SettingRow(t("Plan usage", "套餐用量"), it)
            }
            // Its kind: on its grant's page when it has one of its own.
            if (b.client == null) {
                Hairline(Modifier.padding(start = 16.dp))
                Box {
                    Column(Modifier.fillMaxWidth().rowPress { kindOpen = true }.padding(horizontal = 16.dp, vertical = 11.dp)) {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Text(t("Kind", "类别"), style = MaterialTheme.typography.bodyLarge, modifier = Modifier.weight(1f))
                            Text(kindText(a.category), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                            HSpace(4)
                            Chevron()
                        }
                        Text(
                            if (a.category == "personal") t("Listed with your personal agents; it still runs on its machine.", "列在你的个人 agent 里；它仍在原来的机器上运行。") else t("A coding agent of its machine: work for it goes to that machine.", "本机编码 Agent 归属它所在的机器，派活要先选那台机器。"),
                            style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    IosMenu(kindOpen, { kindOpen = false }, listOf("", "personal", "local").map { k ->
                        MenuEntry(kindText(k), checked = k == a.category) {
                            kindOpen = false
                            scope.launch { if (model.setAgentCategory(m.machineId, a.name, k)) model.say(if (k == "") t("Told from what it is again", "已恢复自动判断") else if (k == "personal") t("Now a personal agent", "已改为个人 Agent") else t("Now a coding agent", "已改为本机编码 Agent")) }
                        }
                    })
                }
            }
        }
        Column(Modifier.fillMaxWidth().padding(top = 8.dp).aideCard()) {
            if (a.pause != null) {
                ActionRow(t("Resume", "恢复")) { scope.launch { model.setAgentPaused(m.machineId, a.name, false) } }
            } else Box {
                ActionRow(t("Pause", "暂停")) { pauseOpen = true }
                IosMenu(pauseOpen, { pauseOpen = false }, listOfNotNull(
                    MenuEntry(t("Pause until I resume it", "暂停，直到我恢复")) { pauseOpen = false; scope.launch { model.setAgentPaused(m.machineId, a.name, true) } },
                    usageResetAt(a, Instant.now())?.let { r ->
                        MenuEntry(t("Pause until usage resets (${whenFormat.format(r)})", "暂停到额度重置（${whenFormat.format(r)}）")) { pauseOpen = false; scope.launch { model.setAgentPaused(m.machineId, a.name, true, r.toString()) } }
                    },
                ))
            }
            Hairline(Modifier.padding(start = 16.dp))
            // Deleting its entry for good goes here too (工单 #82).
            ActionRow(t("Remove from Aide…", "从 Aide 里移除…"), danger = true) { hiding = true }
        }
    }
}

@Composable
private fun SettingRow(k: String, v: String) {
    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 11.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        Text(k, style = MaterialTheme.typography.bodyLarge)
        Text(v, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.weight(1f), textAlign = androidx.compose.ui.text.style.TextAlign.End)
    }
}

@Composable
private fun ActionRow(text: String, danger: Boolean = false, onClick: () -> Unit) {
    Text(text, style = MaterialTheme.typography.bodyLarge, color = if (danger) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.primary,
        modifier = Modifier.fillMaxWidth().rowPress(onClick).padding(horizontal = 16.dp, vertical = 11.dp))
}
