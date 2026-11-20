package io.aide.android.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Tab
import androidx.compose.material3.TabRow
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import io.aide.android.her.HerLine
import io.aide.android.her.HerMemory
import io.aide.android.her.HerPanel
import io.aide.android.model.AppModel
import kotlinx.coroutines.launch

/** The machines that run Her (any platform her runs on), to open her conversation. */
@Composable
fun HerTab(model: AppModel, open: (String) -> Unit) {
    val world by model.world.collectAsState()
    val hers = world.machines.filter { it.her?.enabled == true }
    Column(Modifier.fillMaxSize()) {
        PageTitle("Her")
        SyncLine(model)
        if (hers.isEmpty()) {
            Text(
                t("No machine runs Her. Turn Her on for a machine in the desktop app.", "还没有运行 Her 的机器。请在桌面端为某台机器开启 Her。"),
                Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        LazyColumn(contentPadding = androidx.compose.foundation.layout.PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            items(hers, key = { it.machineId }) { m ->
                Card(Modifier.fillMaxWidth().clickable { open(m.machineId) }) {
                    Column(Modifier.padding(16.dp)) {
                        Text(m.machineName, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
                        val h = m.her!!
                        Text(
                            listOfNotNull(
                                if (h.running) t("running", "运行中") else t("not running", "未运行"),
                                if (!m.online) t("machine offline", "机器离线") else null,
                                h.unread.takeIf { it > 0 }?.let { t("$it unread", "$it 条未读") },
                                h.error.takeIf { it.isNotEmpty() },
                            ).joinToString(" · "),
                            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }
        }
    }
}

/** Her's conversation on a machine, with her routines and memory to read. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HerPage(model: AppModel, machineId: String, back: () -> Unit) {
    val world by model.world.collectAsState()
    val scope = rememberCoroutineScope()
    val panel = remember(machineId) { HerPanel(model, machineId) }
    DisposableEffect(panel) {
        val job = panel.start(scope)
        onDispose { job.cancel() }
    }
    var tab by remember { mutableIntStateOf(0) }
    val connected by panel.connected.collectAsState()
    val error by panel.error.collectAsState()
    val bots by panel.bots.collectAsState()
    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(bots.firstOrNull { it.isDefault }?.name?.ifEmpty { null } ?: "Her")
                        Text(
                            world.machineName(machineId) + if (!connected) " · " + t("connecting…", "正在连接…") else "",
                            style = MaterialTheme.typography.labelSmall,
                        )
                    }
                },
                navigationIcon = { IconButton(onClick = back) { Icon(Icons.AutoMirrored.Filled.ArrowBack, t("Back", "返回")) } },
            )
        },
    ) { pad ->
        Column(Modifier.fillMaxSize().padding(pad)) {
            TabRow(selectedTabIndex = tab) {
                Tab(tab == 0, { tab = 0 }, text = { Text(t("Chat", "对话")) })
                Tab(tab == 1, { tab = 1 }, text = { Text(t("Routines", "例程")) })
                Tab(tab == 2, { tab = 2 }, text = { Text(t("Memory", "记忆")) })
            }
            error?.let { Text(it, Modifier.padding(8.dp), color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
            when (tab) {
                0 -> HerChat(panel)
                1 -> HerRoutines(panel)
                else -> HerMemoryView(panel)
            }
        }
    }
}

@Composable
private fun HerChat(panel: HerPanel) {
    val lines by panel.lines.collectAsState()
    val working by panel.working.collectAsState()
    var text by remember { mutableStateOf("") }
    val scope = rememberCoroutineScope()
    val list = rememberLazyListState()
    LaunchedEffect(lines.size) { if (lines.isNotEmpty()) list.scrollToItem(lines.size - 1) }
    Column(Modifier.fillMaxSize()) {
        LazyColumn(Modifier.weight(1f), state = list, contentPadding = androidx.compose.foundation.layout.PaddingValues(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            items(lines, key = { it.key }) { l ->
                when (l) {
                    is HerLine.Owner -> Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                        Surface(color = MaterialTheme.colorScheme.primary, shape = RoundedCornerShape(16.dp), modifier = Modifier.widthIn(max = 320.dp)) {
                            SelectionContainer { Text(l.text, Modifier.padding(10.dp), color = MaterialTheme.colorScheme.onPrimary) }
                        }
                    }
                    is HerLine.Her -> SelectionContainer { Text(l.text) }
                    is HerLine.Failed -> Text(l.text, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
                }
            }
            if (working) item { Text(t("Her is working…", "Her 正在处理…"), color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall) }
        }
        Row(Modifier.fillMaxWidth().imePadding().padding(8.dp), verticalAlignment = Alignment.CenterVertically) {
            OutlinedTextField(text, { text = it }, Modifier.weight(1f), placeholder = { Text(t("Message Her", "给 Her 发消息")) }, maxLines = 5)
            IconButton(enabled = text.isNotBlank(), onClick = {
                val sent = text.trim()
                scope.launch { if (panel.send(sent)) text = "" }
            }) { Icon(Icons.AutoMirrored.Filled.Send, t("Send", "发送")) }
        }
    }
}

@Composable
private fun HerRoutines(panel: HerPanel) {
    val bots by panel.bots.collectAsState()
    val bot = bots.firstOrNull { it.isDefault } ?: bots.firstOrNull()
    LazyColumn(contentPadding = androidx.compose.foundation.layout.PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        bot?.routinesError?.let { item { Text(it, color = MaterialTheme.colorScheme.error) } }
        if (bot != null && bot.routines.isEmpty()) item { Text(t("No routines.", "没有例程。"), color = MaterialTheme.colorScheme.onSurfaceVariant) }
        items(bot?.routines.orEmpty(), key = { it.id }) { r ->
            Column {
                Text(r.name, fontWeight = FontWeight.SemiBold)
                Text(
                    listOfNotNull(
                        r.schedule.takeIf { it.isNotEmpty() },
                        if (r.patrol) t("patrol: only looks", "只读巡查") else null,
                        when (r.state) { "paused" -> t("paused", "已暂停"); "finished" -> t("finished", "已结束"); else -> null },
                        if (r.running) t("running", "运行中") else null,
                        r.nextRun?.let { t("next ", "下次 ") + it.take(16).replace('T', ' ') },
                        r.lastOutcome?.let { t("last: $it", "上次：$it") },
                    ).joinToString(" · "),
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

@Composable
private fun HerMemoryView(panel: HerPanel) {
    val bots by panel.bots.collectAsState()
    var memory by remember { mutableStateOf<HerMemory?>(null) }
    var loaded by remember { mutableStateOf(false) }
    var editing by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    // The owner's word on a proposal or an entry, then the memory as it now is.
    val act: (suspend () -> Boolean) -> Unit = { call ->
        scope.launch {
            if (call()) {
                editing = null
                memory = panel.memory()
            }
        }
    }
    LaunchedEffect(bots.isNotEmpty()) {
        if (bots.isNotEmpty()) {
            memory = panel.memory()
            loaded = true
        }
    }
    LazyColumn(contentPadding = androidx.compose.foundation.layout.PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        val m = memory
        when {
            !loaded -> item { Text(t("Reading memory…", "正在读取记忆…"), color = MaterialTheme.colorScheme.onSurfaceVariant) }
            m == null -> item { Text(t("Memory could not be read.", "无法读取记忆。"), color = MaterialTheme.colorScheme.error) }
            m.text.isBlank() && m.pages.isEmpty() && m.entries.isEmpty() && m.proposals.isEmpty() ->
                item { Text(t("Nothing yet.", "还没有内容。"), color = MaterialTheme.colorScheme.onSurfaceVariant) }
            else -> {
                if (m.proposals.isNotEmpty()) {
                    item { SectionHeader(t("Proposed to remember", "待确认的提议")) }
                    items(m.proposals, key = { it.id }) { p ->
                        Card(Modifier.fillMaxWidth()) {
                            Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                                EntryText(p.name, p.trigger, p.text)
                                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                    Button(onClick = { act { panel.answerProposal(p.id, true) } }) { Text(t("Save", "保存")) }
                                    OutlinedButton(onClick = { act { panel.answerProposal(p.id, false) } }) { Text(t("Decline", "不要")) }
                                }
                            }
                        }
                    }
                    item {
                        Text(
                            t("It does not use any of it until you save it.", "你保存之前它不会用。"),
                            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
                if (m.text.isNotBlank()) item { SelectionContainer { Text(m.text) } }
                if (m.pages.isNotEmpty()) {
                    item { SectionHeader(t("Pages", "页面")) }
                    items(m.pages) { Text(it) }
                }
                if (m.entries.isNotEmpty()) {
                    item { SectionHeader(t("Entries recalled when needed", "按需想起的条目")) }
                    items(m.entries, key = { it.name }) { e ->
                        Card(Modifier.fillMaxWidth()) {
                            Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                                if (editing == e.name) {
                                    var trigger by remember { mutableStateOf(e.trigger) }
                                    var text by remember { mutableStateOf(e.text) }
                                    Text(e.name, fontWeight = FontWeight.SemiBold)
                                    OutlinedTextField(trigger, { trigger = it }, Modifier.fillMaxWidth(), label = { Text(t("Trigger phrases, separated by ;", "触发词，用 ; 分开")) }, singleLine = true)
                                    OutlinedTextField(text, { text = it }, Modifier.fillMaxWidth(), label = { Text(t("Text", "内容")) }, minLines = 3)
                                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                        Button(onClick = { act { panel.saveEntry(e.name, trigger, text) } }, enabled = trigger.isNotBlank() && text.isNotBlank()) { Text(t("Save", "保存")) }
                                        OutlinedButton(onClick = { editing = null }) { Text(t("Cancel", "取消")) }
                                    }
                                } else {
                                    EntryText(e.name, e.trigger, e.text)
                                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                        OutlinedButton(onClick = { editing = e.name }) { Text(t("Edit", "修改")) }
                                        OutlinedButton(onClick = { act { panel.deleteEntry(e.name) } }) { Text(t("Delete", "删除")) }
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun EntryText(name: String, trigger: String, text: String) {
    Text(name, fontWeight = FontWeight.SemiBold)
    Text(
        if (trigger.isBlank()) t("No trigger: never recalled by itself", "没有触发词，不会自己想起") else t("Trigger: $trigger", "触发词：$trigger"),
        style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
    SelectionContainer { Text(text) }
}
