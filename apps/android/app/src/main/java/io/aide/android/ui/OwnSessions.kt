package io.aide.android.ui

import androidx.compose.foundation.border
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.aide.android.api.ApiError
import io.aide.android.api.ClaudeSession
import io.aide.android.api.ClaudeSessionEntry
import io.aide.android.api.DSHSessionEntry
import io.aide.android.api.SessionEvent
import io.aide.android.model.AppModel
import io.aide.android.model.Bot
import io.aide.android.model.Transcript
import io.aide.android.model.agentTitle
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

// A coding agent's own sessions (工单 #76), as on the desktop: those it has in its own app on its machine — DSH's in
// DSH, Claude Code's in the Claude desktop app — which Aide did not start, on its bot page under its conversations
// (#67: title, state, last activity, the archived apart), each opening over it. Every coding agent has this place;
// where Aide can't read an agent's own sessions (yet), it says so in plain words.

/** One of its own sessions, as its app keeps it. */
sealed interface OwnEntry {
    val id: String
    val title: String
    val archived: Boolean
    val lost: Boolean
    data class Dsh(val entry: DSHSessionEntry) : OwnEntry {
        override val id get() = entry.session.sessionId
        override val title get() = entry.session.title.ifEmpty { entry.session.sessionId }
        override val archived get() = entry.session.archived
        override val lost get() = entry.lost
    }
    data class Claude(val entry: ClaudeSessionEntry) : OwnEntry {
        override val id get() = entry.session.sessionId
        override val title get() = entry.session.title.ifEmpty { entry.session.sessionId }
        override val archived get() = entry.session.archived
        override val lost get() = entry.lost
    }
}

/** Which agents' own sessions Aide reads. */
fun readsOwn(agent: String): Boolean = agent == "deepseek" || agent == "claude"

fun claudeStatusText(s: ClaudeSession): String = when (s.status) {
    "completed" -> t("Completed", "已完成")
    "blocked" -> t("Waiting for you", "等你处理")
    else -> s.status
}

/** Its own sessions on its machine, read again every 15 s while its page is open. */
class OwnSessions {
    var entries by mutableStateOf<List<OwnEntry>?>(null)
    var lost by mutableStateOf(false)
    var error by mutableStateOf<String?>(null)
    /** Where its app's data was read on that machine (DSH's home, the Claude app's index folders), "" when none. */
    var readFrom by mutableStateOf("")
    var showArchived by mutableStateOf(false)
}

@Composable
fun rememberOwnSessions(model: AppModel, b: Bot): OwnSessions {
    val own = remember(b.key) { OwnSessions() }
    LaunchedEffect(b.key) {
        if (!readsOwn(b.agent)) return@LaunchedEffect
        while (true) {
            try {
                val api = model.api
                if (api != null && b.agent == "deepseek") {
                    val r = api.listDSHSessions()
                    own.entries = r.sessions.filter { it.machineId == b.machineId }.map { OwnEntry.Dsh(it) }
                    own.lost = r.unreachable.any { it.machineId == b.machineId }
                    own.readFrom = r.bridges.firstOrNull { it.machineId == b.machineId }?.dshHome.orEmpty()
                } else if (api != null) {
                    val r = api.listClaudeSessions()
                    own.entries = r.sessions.filter { it.machineId == b.machineId }.map { OwnEntry.Claude(it) }
                    own.lost = r.unreachable.any { it.machineId == b.machineId }
                    own.readFrom = r.indexes.firstOrNull { it.machineId == b.machineId }?.indexes.orEmpty().joinToString(", ")
                }
                own.error = null
            } catch (e: ApiError) {
                own.error = e.message.ifEmpty { e.code }
            }
            delay(15_000)
        }
    }
    return own
}

/** The place on its page: a header, a plain line of what it is (or that Aide can't read it), the rows, the archived apart. */
fun LazyListScope.ownSessionItems(own: OwnSessions, b: Bot, machineName: String, open: (OwnEntry) -> Unit) {
    item(key = "own") {
        val app = agentTitle(b.agent)
        val list = own.entries.orEmpty()
        val shown = list.filter { !it.archived }
        val archived = list.filter { it.archived }
        Column(Modifier.fillMaxWidth().padding(top = 12.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            SectionHeader(t("Its own sessions", "它自己的会话"), count = if (readsOwn(b.agent) && own.entries != null) shown.size else null)
            val note = when {
                !readsOwn(b.agent) -> t("Aide can't read the sessions $app has in its own app yet.", "Aide 还读不到 $app 在它自己应用里开的会话。")
                b.agent == "deepseek" -> t("Opened in DSH itself (or by another tool), not by Aide. Read only until you take one over.", "在 DSH 里开的（或经其他工具派的），不是 Aide 开的。默认只读，开启「接管」后可以发消息。")
                else -> t("Claude Code sessions the Claude desktop app has on this machine, not started by Aide. Read only.", "Claude 桌面应用在这台机器上的 Claude Code 会话，不是 Aide 开的。只读。")
            }
            Text(note, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 16.dp))
            own.error?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(horizontal = 16.dp)) }
            if (own.lost) Text(
                if (list.isNotEmpty()) t("$machineName is lost: these are as last read.", "$machineName 失联：下面是最后一次读到的会话。") else t("$machineName doesn’t answer.", "$machineName 没有响应。"),
                style = MaterialTheme.typography.bodySmall, color = Warning, modifier = Modifier.padding(horizontal = 16.dp),
            )
            if (own.entries != null && list.isEmpty()) Text(
                t("No sessions of $app's own here.", "这里没有 $app 自己的会话。") + if (own.readFrom.isNotEmpty()) t(" Read from on $machineName: ${own.readFrom}", " $machineName 上读取的目录：${own.readFrom}") else "",
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 16.dp),
            )
            val rows = shown + if (own.showArchived) archived else emptyList()
            rows.forEachIndexed { i, e -> Box(Modifier.groupRow(i, rows.size, inset = 38.dp)) { OwnRow(e) { open(e) } } }
            if (archived.isNotEmpty()) Text(
                if (own.showArchived) t("Hide archived", "收起已归档") else t("Archived (${archived.size})", "已归档（${archived.size}）"),
                style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary, modifier = Modifier.dimPress { own.showArchived = !own.showArchived }.padding(vertical = 6.dp, horizontal = 16.dp),
            )
        }
    }
}

@Composable
private fun OwnRow(e: OwnEntry, onClick: () -> Unit) {
    Row(Modifier.fillMaxWidth().rowPress(onClick).padding(start = 16.dp, end = 12.dp, top = 10.dp, bottom = 10.dp), verticalAlignment = Alignment.CenterVertically) {
        when (e) {
            is OwnEntry.Dsh -> DSHDot(e.entry.session, e.lost)
            is OwnEntry.Claude -> if (e.lost) Box(Modifier.size(10.dp).border(1.5.dp, MaterialTheme.colorScheme.outlineVariant, CircleShape)) else Box(Modifier.size(10.dp).background(MaterialTheme.colorScheme.outlineVariant, CircleShape))
        }
        HSpace(12)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(e.title, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
            val line = when (e) {
                is OwnEntry.Dsh -> listOf(dshEntryStateText(e.entry), relativeTime(e.entry.session.lastActivityAt), folder(e.entry.session.cwd))
                is OwnEntry.Claude -> listOf(claudeStatusText(e.entry.session), relativeTime(e.entry.session.lastActivityAt), e.entry.session.repo.ifEmpty { folder(e.entry.session.cwd) })
            }.filter { it.isNotEmpty() }.joinToString(" · ")
            Text(line, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        Chevron()
    }
}

private fun folder(cwd: String): String = cwd.split('/', '\\').lastOrNull { it.isNotEmpty() }.orEmpty()

/** One of the Claude desktop app's sessions, opened over Claude Code's page: what Claude Code wrote of it, read only. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ClaudeSessionPage(model: AppModel, entry: ClaudeSessionEntry, back: () -> Unit) {
    val s = entry.session
    var events by remember { mutableStateOf<List<SessionEvent>>(emptyList()) }
    var more by remember { mutableStateOf(false) }
    var loading by remember { mutableStateOf(true) }
    var error by remember { mutableStateOf(s.error.ifEmpty { null }) }
    val scope = rememberCoroutineScope()
    suspend fun read(before: Long) {
        try {
            val r = model.api?.readClaudeSession(entry.machineId, s.sessionId, before) ?: return
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
    Scaffold(snackbarHost = { PageFeedback(model) }, topBar = {
        NavBar(back) {
            Column(horizontalAlignment = Alignment.CenterHorizontally) {
                Text(s.title.ifEmpty { s.sessionId }, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(listOf(claudeStatusText(s), entry.machineName, s.repo.ifEmpty { folder(s.cwd) }).filter { it.isNotEmpty() }.joinToString(" · "), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }) { pad ->
        Box(Modifier.fillMaxSize().padding(pad)) {
            if (loading) {
                ActivityIndicator(Modifier.align(Alignment.Center), size = 28.dp)
            } else {
                LazyColumn(Modifier.fillMaxSize(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    error?.let { item { Text(it, Modifier.padding(12.dp), color = MaterialTheme.colorScheme.error) } }
                    if (more) item {
                        PlainButton(onClick = { scope.launch { read(events.firstOrNull()?.seq ?: 0) } }, modifier = Modifier.padding(horizontal = 16.dp)) { Text(t("Earlier", "更早")) }
                    }
                    items(items, key = { it.key }) { ItemView(it, dsh = true) }
                    if (entry.lost) {
                        val name = entry.machineName.ifEmpty { entry.machineId }
                        item { Text(t("$name can't be reached: what is shown is as last read. Its conversation can be read once it is back.", "$name 连不上：这里显示的是最后一次读到的样子，内容要等它重新连上才能读。"), Modifier.padding(12.dp)) }
                    } else if (items.isEmpty() && error == null) item { Text(t("Nothing written yet.", "还没有内容。"), Modifier.padding(12.dp)) }
                }
            }
        }
    }
}
