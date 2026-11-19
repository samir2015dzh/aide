package io.hermesh.android.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import io.hermesh.android.api.Machine
import io.hermesh.android.model.AppModel
import io.hermesh.android.model.agentTitle
import io.hermesh.android.signer.RootKeys
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** The machines and their coding agents, the trusted devices, and this phone. */
@Composable
fun MachinesTab(model: AppModel) {
    val world by model.world.collectAsState()
    val st = world.status
    val keep by model.keepalive.enabled.collectAsState()
    var keyLevel by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(st.deviceKeyId) {
        if (st.deviceKeyId.isNotEmpty()) keyLevel = withContext(Dispatchers.IO) { runCatching { RootKeys.securityLevel(st.deviceKeyId) }.getOrNull() }
    }
    Column(Modifier.fillMaxSize()) {
        PageTitle(t("Machines", "机器"))
        SyncLine(model)
        LazyColumn(Modifier.fillMaxSize(), contentPadding = androidx.compose.foundation.layout.PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            if (world.machines.isEmpty() && world.watched) item { Text(t("No machines yet.", "还没有机器。"), color = MaterialTheme.colorScheme.onSurfaceVariant) }
            items(world.machines, key = { it.machineId }) { MachineCard(model, it) }
            // Coding agents are connected where they run: on a computer, through the desktop app's guide.
            item { SectionHeader(t("Connecting a coding agent", "连接编码 Agent")) }
            item {
                Text(
                    t(
                        "Coding agents run on your computers, not on this phone. Install DeepSeek Harness (DSH), the Claude app (open its Code tab once) or Codex on a computer, then open Aide there: Settings › Connect walks you through each one, says whether it is detected and runs a test. Each computer above lists the agents it detected, and their tasks show under Sessions here.",
                        "编码 Agent 在你的电脑上运行，不在这台手机上。先在电脑上装好 DeepSeek Harness（DSH）、Claude 应用（打开一次它的 Code 标签页）或 Codex，再打开那台电脑上的 Aide：设置 › 连接 会一步步带你连接、告诉你是否检测到，并运行测试。上面每台电脑都列出了它检测到的 Agent，它们的任务会显示在这里的「会话」里。",
                    ),
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 4.dp),
                )
            }
            item { SectionHeader(t("Trusted devices", "可信设备")) }
            items(world.devices, key = { it.keyId }) { d ->
                Text(d.label + (if (d.thisDevice) t(" (this phone)", "（这台手机）") else "") + " · " + d.platform, Modifier.padding(horizontal = 4.dp))
            }
            item {
                SectionHeader(t("This phone", "这台手机"))
                Column(Modifier.padding(horizontal = 4.dp)) {
                    Labeled(t("Server", "服务器"), st.relayUrl)
                    if (st.connectAddress.isNotEmpty()) Labeled(t("Connection address", "连接地址"), st.connectAddress)
                    if (st.account.isNotEmpty()) Labeled(t("Account", "账号"), st.account)
                    keyLevel?.let { Labeled(t("Root key kept in", "根密钥存放于"), it) }
                    Labeled(t("Core version", "核心版本"), st.version)
                    KeepaliveSettings(model)
                    Labeled(
                        t("Notifications", "通知"),
                        if (keep) t("through Aide’s own connection", "经 Aide 自己的连接") else t("while Aide is open", "Aide 打开时"),
                    )
                }
            }
        }
    }
}

@Composable
private fun MachineCard(model: AppModel, m: Machine) {
    val scope = rememberCoroutineScope()
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Row {
                Text(m.machineName, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f))
                Text(
                    when {
                        m.syncing -> t("updating", "更新中")
                        m.online && m.unreachable.isEmpty() -> t("online", "在线")
                        m.online -> t("unreachable", "不可达")
                        else -> t("offline", "离线")
                    },
                    color = if (m.online && m.unreachable.isEmpty()) Positive else MaterialTheme.colorScheme.onSurfaceVariant,
                    style = MaterialTheme.typography.labelLarge,
                )
            }
            val s = m.status
            Text(
                listOfNotNull(
                    s?.platform?.takeIf { it.isNotEmpty() },
                    (s?.version ?: m.version).takeIf { it.isNotEmpty() }?.let { "v$it" },
                    s?.let { t("${it.runningSessions} running", "${it.runningSessions} 个运行中") },
                    if (s?.paused == true) t("taking no new sessions", "暂停接收新会话") else null,
                ).joinToString(" · "),
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            if (m.unreachable.isNotEmpty() && m.online) Text(m.unreachable, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
            s?.agents?.filter { it.available }?.forEach { a ->
                Row {
                    Text(agentTitle(a.name), style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f))
                    Spacer(Modifier.width(8.dp))
                    Text(
                        listOfNotNull(a.version.takeIf { it.isNotEmpty() }, if (a.loggedIn) t("signed in", "已登录") else t("not signed in", "未登录")).joinToString(" · "),
                        style = MaterialTheme.typography.bodySmall, color = if (a.loggedIn) Positive else Warning,
                    )
                }
            }
            m.her?.takeIf { it.enabled }?.let { h ->
                Text("Her · " + (if (h.running) t("running", "运行中") else t("not running", "未运行")) + (h.error.takeIf { it.isNotEmpty() }?.let { " · $it" } ?: ""), style = MaterialTheme.typography.bodySmall)
            }
            if (m.online) TextButton(onClick = { scope.launch { model.recheck(m.machineId) } }) { Text(t("Check agents again", "重新检查 Agent")) }
        }
    }
}
