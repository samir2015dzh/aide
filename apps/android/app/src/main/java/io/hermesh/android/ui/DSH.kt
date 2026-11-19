package io.hermesh.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.hermesh.android.api.ApiError
import io.hermesh.android.api.DSHBridge
import io.hermesh.android.api.DSHSession
import io.hermesh.android.api.DSHSessionEntry
import io.hermesh.android.api.SessionEvent
import io.hermesh.android.model.AppModel
import io.hermesh.android.model.Transcript
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

// DeepSeek Harness's own sessions on the owner's machines: those opened in DSH, or by another tool, which
// Hermesh did not start: the list by last activity, and what DSH wrote of each. Read only until the owner
// takes one over (docs/dsh-takeover.md): then their messages go to it through the Hermesh plugin in DSH
// Desktop, queued after a turn that runs, as DSH queues its own.

/** What the page says of the bridge plugin, or null when messages can go. */
fun dshBridgeProblem(b: DSHBridge?): String? = when (b?.state) {
    "DSH_BRIDGE_STATE_RUNNING" -> null
    "DSH_BRIDGE_STATE_NOT_INSTALLED" -> t("DSH Desktop doesn't have the Aide plugin yet, so messages can't go.", "DSH Desktop 里还没有 Aide 插件，发不了消息。")
    "DSH_BRIDGE_STATE_NOT_RUNNING" -> t("The Aide plugin in DSH Desktop doesn't answer, so messages can't go.", "DSH Desktop 里的 Aide 插件没有响应，发不了消息。")
    "DSH_BRIDGE_STATE_INCOMPATIBLE" -> t("The Aide plugin in DSH Desktop can't be used.", "DSH Desktop 里的 Aide 插件用不了。")
    else -> t("Not known yet whether the Aide plugin in DSH Desktop runs.", "还不知道 DSH Desktop 里的 Aide 插件是否在运行。")
}

/** A message's id for DSH ("hermesh-" + 26 lowercase letters and digits), which drops a second one with it. */
fun newDSHRequestId(): String {
    val alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
    val r = java.security.SecureRandom()
    return "hermesh-" + (1..26).map { alphabet[r.nextInt(alphabet.length)] }.joinToString("")
}

fun dshStateText(s: DSHSession): String = when (s.state) {
    "DSH_SESSION_STATE_RUNNING" -> t("Running", "运行中")
    "DSH_SESSION_STATE_IDLE" -> t("Open in DSH", "在 DSH 中打开")
    "DSH_SESSION_STATE_ENDED" -> if (s.turnOpen) t("Ended mid-turn", "上一轮中断") else t("Ended", "已结束")
    else -> t("State unknown", "状态未知")
}

@Composable
private fun DSHDot(s: DSHSession) {
    val color = when (s.state) {
        "DSH_SESSION_STATE_RUNNING" -> Color(0xFF17924F)
        "DSH_SESSION_STATE_IDLE" -> Warning
        else -> MaterialTheme.colorScheme.outlineVariant
    }
    Box(Modifier.size(10.dp).background(color, CircleShape))
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DSHListPage(model: AppModel, open: (DSHSessionEntry) -> Unit, back: () -> Unit) {
    var entries by remember { mutableStateOf<List<DSHSessionEntry>?>(null) }
    var unreachable by remember { mutableStateOf<List<String>>(emptyList()) }
    var error by remember { mutableStateOf<String?>(null) }
    var archived by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) {
        while (true) {
            try {
                val r = model.api?.listDSHSessions()
                if (r != null) {
                    entries = r.sessions
                    unreachable = r.unreachable.map { it.machineName.ifEmpty { it.machineId } }
                    error = null
                }
            } catch (e: ApiError) {
                error = e.message.ifEmpty { e.code }
            }
            delay(15_000)
        }
    }
    Scaffold(topBar = {
        TopAppBar(
            title = { Text(t("DeepSeek sessions", "DeepSeek 会话")) },
            navigationIcon = { IconButton(onClick = back) { Icon(Icons.AutoMirrored.Filled.ArrowBack, t("Back", "返回")) } },
        )
    }) { pad ->
        val all = entries
        if (all == null) {
            Box(Modifier.fillMaxSize().padding(pad)) { CircularProgressIndicator(Modifier.align(Alignment.Center)) }
            return@Scaffold
        }
        val shown = all.filter { archived || !it.session.archived }
        val hidden = all.size - shown.size
        LazyColumn(Modifier.fillMaxSize().padding(pad)) {
            item {
                Text(
                    t("Sessions DeepSeek Harness has of its own. Read only, until you turn on Take Over for one.", "DeepSeek Harness 自己的会话（在 DSH 里开的，或经其他工具派的）。默认只读；开启「接管」后可以给它发消息。"),
                    Modifier.padding(16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            error?.let { item { Text(it, Modifier.padding(horizontal = 16.dp), color = MaterialTheme.colorScheme.error) } }
            items(unreachable) { Text(t("$it doesn’t answer.", "$it 没有响应。"), Modifier.padding(horizontal = 16.dp), color = Warning) }
            if (shown.isEmpty()) {
                item { Text(t("No DeepSeek Harness sessions of its own.", "没有 DeepSeek Harness 自己的会话。"), Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
            }
            items(shown, key = { it.machineId + "/" + it.session.sessionId }) { e ->
                val s = e.session
                Row(Modifier.fillMaxWidth().clickable { open(e) }.padding(horizontal = 16.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
                    DSHDot(s)
                    Spacer(Modifier.width(12.dp))
                    Column(Modifier.weight(1f)) {
                        Text(s.title.ifEmpty { s.sessionId }, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        Text(
                            listOf(dshStateText(s), relativeTime(s.lastActivityAt), e.machineName, s.cwd.split('/', '\\').lastOrNull { it.isNotEmpty() }.orEmpty(),
                                if (s.archived) t("archived", "已归档") else "").filter { it.isNotEmpty() }.joinToString(" · "),
                            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis,
                        )
                    }
                }
                HorizontalDivider(Modifier.padding(start = 16.dp))
            }
            if (hidden > 0 || archived) {
                item {
                    TextButton(onClick = { archived = !archived }, modifier = Modifier.padding(8.dp)) {
                        Text(if (archived) t("Hide archived", "隐藏已归档") else t("Show $hidden archived", "显示 $hidden 个已归档"))
                    }
                }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DSHSessionPage(model: AppModel, entry: DSHSessionEntry, back: () -> Unit) {
    val s = entry.session
    var events by remember { mutableStateOf<List<SessionEvent>>(emptyList()) }
    var more by remember { mutableStateOf(false) }
    var loading by remember { mutableStateOf(true) }
    var error by remember { mutableStateOf(s.error.ifEmpty { null }) }
    val scope = rememberCoroutineScope()
    suspend fun read(before: Long) {
        try {
            val r = model.api?.readDSHSession(entry.machineId, s.sessionId, before) ?: return
            events = if (before > 0) r.events + events else r.events
            more = r.hasMoreBefore
            error = r.session.error.ifEmpty { null }
        } catch (e: ApiError) {
            error = e.message.ifEmpty { e.code }
        }
        loading = false
    }
    LaunchedEffect(entry) { read(0) }
    val items = remember(events) { Transcript().also { t -> events.forEach(t::add) }.items() }
    var takeover by remember { mutableStateOf(s.takeover) }
    var asking by remember { mutableStateOf(false) }
    var bridge by remember { mutableStateOf<DSHBridge?>(null) }
    suspend fun readBridge() {
        try {
            bridge = model.api?.listDSHSessions()?.bridges?.firstOrNull { it.machineId == entry.machineId }?.bridge
        } catch (_: ApiError) {
        }
    }
    LaunchedEffect(entry) { readBridge() }
    suspend fun setOn(on: Boolean) {
        try {
            takeover = model.api?.setDSHTakeover(entry.machineId, s.sessionId, on)?.session?.takeover ?: takeover
            asking = false
        } catch (e: ApiError) {
            error = e.message.ifEmpty { e.code }
        }
    }
    if (asking) {
        AlertDialog(
            onDismissRequest = { asking = false },
            title = { Text(t("Take over this session?", "开启接管？")) },
            text = {
                Text(
                    t(
                        "You can send this session messages and stop its turn from Aide; they go to DSH through the Aide plugin in DSH Desktop and show there too.\n\nWhile a turn runs, a message waits for the next turn, as one typed in DSH would.\n\nTurning it off makes it read only again; the session in DSH is unaffected.",
                        "开启后可以从 Aide 给这个会话发消息、打断它；消息经 DSH Desktop 里的 Aide 插件交给 DSH，DSH Desktop 里同步可见。\n\n会话正在运行时，消息会排到这一轮之后，和在 DSH 里输入一样。\n\n关闭接管后回到只读，DSH 里的会话不受影响。",
                    ),
                )
            },
            confirmButton = { TextButton(onClick = { scope.launch { setOn(true) } }) { Text(t("Take Over", "开启接管")) } },
            dismissButton = { TextButton(onClick = { asking = false }) { Text(t("Cancel", "取消")) } },
        )
    }
    Scaffold(topBar = {
        TopAppBar(
            title = {
                Column {
                    Text(s.title.ifEmpty { s.sessionId }, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    Text(listOf(dshStateText(s), entry.machineName).joinToString(" · "), style = MaterialTheme.typography.bodySmall)
                }
            },
            navigationIcon = { IconButton(onClick = back) { Icon(Icons.AutoMirrored.Filled.ArrowBack, t("Back", "返回")) } },
            actions = {
                Text(t("Take over", "接管"), style = MaterialTheme.typography.labelMedium)
                Switch(checked = takeover, onCheckedChange = { on -> if (on) asking = true else scope.launch { setOn(false) } }, modifier = Modifier.padding(horizontal = 8.dp))
            },
        )
    }, bottomBar = {
        if (takeover) DSHComposer(model, entry, bridge, sent = { scope.launch { delay(1500); read(0) } }, failed = { scope.launch { readBridge() } })
    }) { pad ->
        Box(Modifier.fillMaxSize().padding(pad)) {
            if (loading) {
                CircularProgressIndicator(Modifier.align(Alignment.Center))
            } else {
                LazyColumn(Modifier.fillMaxSize(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    error?.let { item { Text(it, Modifier.padding(12.dp), color = MaterialTheme.colorScheme.error) } }
                    if (more) {
                        item {
                            TextButton(onClick = { scope.launch { read(events.firstOrNull()?.seq ?: 0) } }, modifier = Modifier.padding(horizontal = 4.dp)) {
                                Text(t("Earlier", "更早"))
                            }
                        }
                    }
                    items(items, key = { it.key }) { ItemView(it, dsh = true) }
                    if (items.isEmpty() && error == null) item { Text(t("Nothing written yet.", "还没有内容。"), Modifier.padding(12.dp)) }
                }
            }
        }
    }
}

/**
 * Taken over: the owner's message to the session, through the bridge plugin in DSH Desktop. It waits for
 * the next turn when one runs. A failure says what to do and keeps the text; a timeout keeps the message's
 * id too, so sending again can't make two.
 */
@Composable
private fun DSHComposer(model: AppModel, entry: DSHSessionEntry, bridge: DSHBridge?, sent: () -> Unit, failed: () -> Unit) {
    val s = entry.session
    val running = s.state == "DSH_SESSION_STATE_RUNNING"
    val problem = dshBridgeProblem(bridge)
    var text by remember { mutableStateOf("") }
    var reqId by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var note by remember { mutableStateOf<String?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    Column(Modifier.fillMaxWidth().padding(12.dp)) {
        problem?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = Warning) }
        error?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error) }
        note?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
        OutlinedTextField(
            value = text, onValueChange = { text = it; reqId = "" }, enabled = !busy, modifier = Modifier.fillMaxWidth(),
            label = { Text(t("Message to DSH", "给 DSH 的消息")) },
            placeholder = { Text(if (running) t("A turn is running: this waits for the next turn", "会话正在运行：这条消息会排到这一轮之后") else t("Message this session", "给这个会话发消息")) },
        )
        Row(Modifier.fillMaxWidth().padding(top = 6.dp), horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End)) {
            if (running) {
                TextButton(enabled = problem == null, onClick = {
                    scope.launch {
                        error = null
                        try {
                            val r = model.api?.interruptDSHSession(entry.machineId, s.sessionId)
                            note = if (r?.interrupted == true) t("Stopped this turn.", "已打断这一轮。") else t("No turn of this session runs in DSH Desktop.", "DSH Desktop 里这个会话没有在运行的一轮。")
                            sent()
                        } catch (e: ApiError) {
                            error = e.message.ifEmpty { e.code }
                        }
                    }
                }) { Text(t("Stop", "打断")) }
            }
            Button(enabled = !busy && text.isNotBlank() && problem == null, onClick = {
                if (reqId.isEmpty()) reqId = newDSHRequestId()
                busy = true
                error = null
                note = null
                scope.launch {
                    try {
                        model.api?.sendDSHMessage(entry.machineId, s.sessionId, text, reqId)
                        text = ""
                        reqId = ""
                        note = if (running) t("Queued: DSH takes it up when this turn ends.", "已排队：这一轮结束后 DSH 接着处理。") else t("Sent.", "已发送。")
                        sent()
                    } catch (e: ApiError) {
                        if (e.code != "deadline_exceeded") reqId = ""
                        error = e.message.ifEmpty { e.code }
                        failed()
                    }
                    busy = false
                }
            }) { Text(if (busy) t("Sending…", "正在发送…") else t("Send", "发送")) }
        }
    }
}
