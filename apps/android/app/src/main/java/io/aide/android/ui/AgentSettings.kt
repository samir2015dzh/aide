package io.aide.android.ui

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import io.aide.android.api.AgentStatus
import io.aide.android.api.Machine
import io.aide.android.model.AppModel
import io.aide.android.model.Bot
import io.aide.android.model.agoText
import io.aide.android.model.instantOrNull
import io.aide.android.model.pausedUntil
import io.aide.android.model.usageResetAt
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import kotlinx.coroutines.launch

// A coding agent's own settings (工单 #93), on its bot's settings page (工单 #97, BotSettings.kt), as the desktop's and as a
// connected agent's grant page is laid out: what it can use (its machine, its projects), its kind (told from what it is
// until the owner says otherwise, #52, kept on its machine), pausing it and taking it off Aide's lists; its names and
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
    Column(Modifier.fillMaxWidth()) {
        // What it uses: its machine, its projects, its plan's usage.
        SectionHeader(t("What it uses", "它用什么"))
        Column(Modifier.fillMaxWidth().fluentCard()) {
            FluentInfoRow(t("Machine", "机器"), t("${m.machineName} (where it runs)", "${m.machineName}（它在这台上运行）"))
            FluentDivider()
            FluentInfoRow(t("Projects", "项目"), projects.joinToString(t(", ", "、")) { world.projectTitle(it.machineId, it.projectId) }.ifEmpty { t("No project yet", "还没有项目") })
            usageLine(a).takeIf { b.agent == "claude" && it.isNotEmpty() }?.let {
                FluentDivider()
                FluentInfoRow(t("Plan usage", "套餐用量"), it)
            }
        }
        // Its kind, its three choices laid out (工单 #97 补充三): on its grant's page when it has one of its own.
        if (b.client == null) {
            SectionHeader(t("Kind", "类别"))
            FluentRadioList(
                listOf(
                    FluentChoice("", t("Told from what it is", "自动判断"), t("As it is now: a coding agent of its machine", "按它是什么来定：现在是本机编码 Agent")),
                    FluentChoice("personal", t("Personal agent", "个人 Agent"), t("Listed with your personal agents; it still runs on its machine.", "列在你的个人 agent 里；它仍在原来的机器上运行。")),
                    FluentChoice("local", t("Coding agent · ${m.machineName}", "本机编码 Agent · ${m.machineName}"), t("A coding agent of its machine: work for it goes to that machine.", "本机编码 Agent 归属它所在的机器，派活要先选那台机器。")),
                ),
                a.category,
            ) { k -> scope.launch { if (model.setAgentCategory(m.machineId, a.name, k)) model.say(if (k == "") t("Told from what it is again", "已恢复自动判断") else if (k == "personal") t("Now a personal agent", "已改为个人 Agent") else t("Now a coding agent", "已改为本机编码 Agent")) } }
        }
        // Pausing it: a switch (on, until the owner resumes it); until its plan's usage resets, a row of its own.
        SectionHeader(t("Running", "运行"))
        Column(Modifier.fillMaxWidth().fluentCard()) {
            val until = pausedUntil(a)
            FluentSwitchRow(
                t("Paused", "暂停"),
                when {
                    a.pause == null -> t("No new task starts with it while it is paused.", "暂停期间不会有新任务交给它。")
                    until != null -> t("Resumes by itself at ${whenFormat.format(until)}", "到 ${whenFormat.format(until)} 自动恢复")
                    else -> t("Until you resume it", "直到你恢复它")
                },
                a.pause != null,
            ) { on -> scope.launch { model.setAgentPaused(m.machineId, a.name, on) } }
            if (a.pause == null) usageResetAt(a, Instant.now())?.let { r ->
                FluentDivider()
                FluentActionRow(t("Pause until usage resets (${whenFormat.format(r)})", "暂停到额度重置（${whenFormat.format(r)}）")) { scope.launch { model.setAgentPaused(m.machineId, a.name, true, r.toString()) } }
            }
        }
    }
}

/** Taking it off Aide's lists (工单 #93), asked first; deleting its entry for good goes here too (工单 #82). */
@Composable
fun RemoveAgent(model: AppModel, b: Bot, m: Machine) {
    val scope = rememberCoroutineScope()
    var hiding by remember { mutableStateOf(false) }
    if (hiding) IosAlert(
        onDismissRequest = { hiding = false },
        title = t("Remove ${b.name} from Aide?", "把 ${b.name} 从 Aide 里移除？"),
        message = t("It leaves Aide's lists; nothing on ${m.machineName} is uninstalled or deleted, and its history stays. A recheck of the machine shows it again.", "它会从 Aide 的列表里拿掉；${m.machineName} 上什么都不会卸载或删除，它的历史也都在。到设备页重新检查这台机器，它就会回来。"),
        actions = listOf(
            AlertAction(t("Cancel", "取消"), AlertRole.Cancel) { hiding = false },
            AlertAction(t("Remove", "移除"), AlertRole.Destructive) { hiding = false; scope.launch { model.setAgentHidden(m.machineId, b.agent, true) } },
        ),
    )
    FluentDangerButton(t("Remove from Aide…", "从 Aide 里移除…")) { hiding = true }
}
