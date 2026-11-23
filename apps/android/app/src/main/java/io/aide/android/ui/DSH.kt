package io.aide.android.ui

import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
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
import io.aide.android.api.ApiError
import io.aide.android.api.DSHBridge
import io.aide.android.api.DSHSession
import io.aide.android.api.DSHSessionEntry
import io.aide.android.api.SessionEvent
import io.aide.android.model.AppModel
import io.aide.android.model.Transcript
import io.aide.android.model.durationText
import io.aide.android.model.instantOrNull
import java.time.Duration
import java.time.Instant
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

// DeepSeek Harness's own sessions on the owner's machines: those opened in DSH, or by another tool, which
// Aide did not start, listed on DSH's bot page (工单 #76, OwnSessions.kt), and here what DSH wrote of each. Read only until the owner
// takes one over (docs/dsh-takeover.md): then their messages go to it through the Aide plugin in DSH
// Desktop, queued after a turn that runs, as DSH queues its own. A machine that can't be reached keeps
// its sessions here as last read, lost: their state last known, never shown as now (docs/session-states.md).

/** What the page says of the bridge plugin, or null when messages can go. */
fun dshBridgeProblem(b: DSHBridge?): String? = when (b?.state) {
    "DSH_BRIDGE_STATE_RUNNING" -> null
    "DSH_BRIDGE_STATE_NOT_INSTALLED" -> t("DSH Desktop doesn't have the Aide plugin yet, so messages can't go.", "DSH Desktop 里还没有 Aide 插件，发不了消息。")
    "DSH_BRIDGE_STATE_NOT_RUNNING" -> t("The Aide plugin in DSH Desktop doesn't answer, so messages can't go.", "DSH Desktop 里的 Aide 插件没有响应，发不了消息。")
    "DSH_BRIDGE_STATE_INCOMPATIBLE" -> t("The Aide plugin in DSH Desktop can't be used.", "DSH Desktop 里的 Aide 插件用不了。")
    else -> t("Not known yet whether the Aide plugin in DSH Desktop runs.", "还不知道 DSH Desktop 里的 Aide 插件是否在运行。")
}

/** A message's id for DSH ("aide-" + 26 lowercase letters and digits), which drops a second one with it. */
fun newDSHRequestId(): String {
    val alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
    val r = java.security.SecureRandom()
    return "aide-" + (1..26).map { alphabet[r.nextInt(alphabet.length)] }.joinToString("")
}

fun dshStateText(s: DSHSession): String = when (s.state) {
    "DSH_SESSION_STATE_RUNNING" -> t("Running", "运行中")
    "DSH_SESSION_STATE_IDLE" -> t("Open in DSH", "在 DSH 中打开")
    "DSH_SESSION_STATE_ENDED" -> if (s.turnOpen) t("Ended mid-turn", "上一轮中断") else t("Ended", "已结束")
    else -> t("State unknown", "状态未知")
}

/** The state line of a listed session: DSH's word for it, or, its machine lost, that word as last known. */
fun dshEntryStateText(e: DSHSessionEntry, now: Instant = Instant.now()): String {
    val st = dshStateText(e.session)
    if (!e.lost) return st
    val silent = instantOrNull(e.readAt)?.let { Duration.between(it, now) } ?: Duration.ZERO
    return if (silent.isZero || silent.isNegative) t("Lost · last known $st", "失联 · 最后已知 $st")
    else t("Lost · last known $st · ${durationText(silent)} ago", "失联 · 最后已知 $st · ${durationText(silent)}前")
}

@Composable
internal fun DSHDot(s: DSHSession, lost: Boolean) {
    if (lost) {
        Box(Modifier.size(10.dp).border(1.5.dp, MaterialTheme.colorScheme.outlineVariant, CircleShape))
        return
    }
    val color = when (s.state) {
        "DSH_SESSION_STATE_RUNNING" -> Positive
        "DSH_SESSION_STATE_IDLE" -> Warning
        else -> MaterialTheme.colorScheme.outlineVariant
    }
    Box(Modifier.size(10.dp).background(color, CircleShape))
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
    LaunchedEffect(entry) { if (entry.lost) loading = false else read(0) }
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
        IosAlert(
            onDismissRequest = { asking = false },
            title = t("Take over this session?", "开启接管？"),
            actions = listOf(
                AlertAction(t("Cancel", "取消"), AlertRole.Cancel) { asking = false },
                AlertAction(t("Take Over", "开启接管"), AlertRole.Preferred) { scope.launch { setOn(true) } },
            ),
        ) {
                WritesTo(model.world.value, entry.machineId)
                Text(
                    t(
                        "You can send this session messages and stop its turn from Aide; they go to DSH through the Aide plugin in DSH Desktop and show there too.\n\nWhile a turn runs, a message waits for the next turn, as one typed in DSH would.\n\nTurning it off makes it read only again; the session in DSH is unaffected.",
                        "开启后可以从 Aide 给这个会话发消息、打断它；消息经 DSH Desktop 里的 Aide 插件交给 DSH，DSH Desktop 里同步可见。\n\n会话正在运行时，消息会排到这一轮之后，和在 DSH 里输入一样。\n\n关闭接管后回到只读，DSH 里的会话不受影响。",
                    ),
                    textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                )
        }
    }
    Scaffold(snackbarHost = { PageFeedback(model) }, topBar = {
        NavBar(back) {
            Column(horizontalAlignment = Alignment.CenterHorizontally) {
                Text(s.title.ifEmpty { s.sessionId }, maxLines = 1, overflow = TextOverflow.Ellipsis)
                MachineTag(model.world.value, entry.machineId)
                Text(listOf(dshEntryStateText(entry), entry.machineName).joinToString(" · "), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }, bottomBar = {
        // Always there (the owner's ruling, 2026-11-21): read only until taken over, the switch in it, beside sending.
        // Lost: nothing can be sent to it, nor its switch changed, until its machine is back.
        if (!entry.lost) DSHComposer(
            model, entry, bridge, takeover,
            setTakeover = { on -> if (on) asking = true else scope.launch { setOn(false) } },
            sent = { scope.launch { delay(1500); read(0) } }, failed = { scope.launch { readBridge() } },
        )
    }) { pad ->
        Box(Modifier.fillMaxSize().padding(pad)) {
            if (loading) {
                ActivityIndicator(Modifier.align(Alignment.Center), size = 28.dp)
            } else {
                val list = rememberLazyListState()
                LaunchedEffect(items.size) {
                    val info = list.layoutInfo
                    val last = info.visibleItemsInfo.lastOrNull()?.index ?: return@LaunchedEffect
                    if (info.totalItemsCount > 0 && last >= info.totalItemsCount - 3) list.animateScrollToItem(info.totalItemsCount - 1)
                }
                LazyColumn(Modifier.fillMaxSize(), state = list, verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    error?.let { item { Text(it, Modifier.padding(12.dp), color = MaterialTheme.colorScheme.error) } }
                    if (more) {
                        item {
                            PlainButton(onClick = { scope.launch { read(events.firstOrNull()?.seq ?: 0) } }, modifier = Modifier.padding(horizontal = 4.dp)) {
                                Text(t("Earlier", "更早"))
                            }
                        }
                    }
                    items(items, key = { it.key }) {
                        Box(Modifier.animateItem(fadeInSpec = tween(Motion.Medium), placementSpec = tween(Motion.Medium, easing = Motion.EaseOut))) { ItemView(it, dsh = true, agent = "DSH") }
                    }
                    if (entry.lost) {
                        val name = entry.machineName.ifEmpty { entry.machineId }
                        item { Text(t("$name can't be reached: what is shown is as last read. Its conversation can be read once it is back.", "$name 连不上：这里显示的是最后一次读到的样子，内容要等它重新连上才能读。"), Modifier.padding(12.dp)) }
                    } else if (items.isEmpty() && error == null) item { Text(t("Nothing written yet.", "还没有内容。"), Modifier.padding(12.dp)) }
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
private fun DSHComposer(model: AppModel, entry: DSHSessionEntry, bridge: DSHBridge?, takeover: Boolean, setTakeover: (Boolean) -> Unit, sent: () -> Unit, failed: () -> Unit) {
    val s = entry.session
    val running = s.state == "DSH_SESSION_STATE_RUNNING"
    val problem = if (takeover) dshBridgeProblem(bridge) else null
    var text by remember { mutableStateOf("") }
    var reqId by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var note by remember { mutableStateOf<String?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    Column(Modifier.fillMaxWidth().background(aide.bar)) {
        Hairline()
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp)) {
        WritesTo(model.world.value, entry.machineId, Modifier.padding(bottom = 4.dp))
        problem?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = Warning) }
        error?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error) }
        note?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
        IosField(
            value = text, onValueChange = { text = it; reqId = "" }, enabled = takeover && !busy, modifier = Modifier.fillMaxWidth(), singleLine = false,
            label = t("Message to DSH", "给 DSH 的消息"),
            placeholder = when {
                !takeover -> t("Turn on Take Over to send this session messages", "开启接管后可向此会话发送消息")
                running -> t("A turn is running: this waits for the next turn", "会话正在运行：这条消息会排到这一轮之后")
                else -> t("Message this session", "给这个会话发消息")
            },
        )
        Row(Modifier.fillMaxWidth().padding(top = 6.dp), horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
            // The switch, beside sending: off, the box above can't be written in.
            IosSwitch(checked = takeover, onCheckedChange = setTakeover)
            Text(t("Take over", "接管"), style = MaterialTheme.typography.bodyLarge, modifier = Modifier.weight(1f))
            if (takeover && running) {
                PlainButton(enabled = problem == null, onClick = {
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
            FilledButton(enabled = takeover && !busy && text.isNotBlank() && problem == null, onClick = {
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
}
