package io.aide.android.ui

import androidx.compose.ui.draw.rotate
import androidx.compose.ui.draw.clip
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.size
import androidx.compose.animation.shrinkVertically
import androidx.compose.animation.fadeOut
import androidx.compose.animation.fadeIn
import androidx.compose.animation.expandVertically
import androidx.compose.animation.core.tween
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.aide.android.api.Question
import io.aide.android.api.Session
import io.aide.android.model.AppModel
import io.aide.android.model.Item
import io.aide.android.model.SessionFeed
import io.aide.android.model.agentTitle
import io.aide.android.model.buildAnswers
import io.aide.android.model.choose
import io.aide.android.model.displayTitle
import io.aide.android.model.isLive
import io.aide.android.model.lostSpanText
import io.aide.android.model.openedNews
import kotlinx.coroutines.launch

/** A session's whole conversation, live, with the agent's question to answer and a way to interrupt it. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SessionPage(model: AppModel, id: String, back: () -> Unit) {
    val world by model.world.collectAsState()
    val s = world.sessions[id]?.let(world::shown)
    val scope = rememberCoroutineScope()
    val feed = remember(id) { SessionFeed(model, id) }
    DisposableEffect(feed) {
        val job = feed.start(scope)
        onDispose { job.cancel() }
    }
    val items by feed.items.collectAsState()
    val loading by feed.loading.collectAsState()
    val list = rememberLazyListState()
    // In front of the owner: what is new here is seen, on every device.
    val news = s?.let(::openedNews).orEmpty()
    val front by model.foreground.collectAsState()
    LaunchedEffect(id, news, front) {
        if (front && news.isNotEmpty()) world.sessions[id]?.let { model.markSeen(it) }
    }
    LaunchedEffect(items.size, items.lastOrNull()) {
        // Follow the end while it is in view (or on first load).
        val last = list.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: -1
        // The first time, straight to the end; after, a message arriving while the owner is at the end eases into view.
        if (items.isNotEmpty() && last < 0) list.scrollToItem(items.size - 1)
        else if (items.isNotEmpty() && last >= items.size - 3) list.animateScrollToItem(items.size - 1)
    }
    Scaffold(
        snackbarHost = { PageFeedback(model) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(s?.let(::displayTitle) ?: id.take(12), maxLines = 1, overflow = TextOverflow.Ellipsis)
                        if (s != null) MachineTag(world, s.machineId)
                        if (s != null) {
                            Text(
                                "${world.senderName(s)} → ${agentTitle(s.agent)} · ${s.projectId} · ${world.machineName(s.machineId)}",
                                style = MaterialTheme.typography.labelSmall, maxLines = 1, overflow = TextOverflow.Ellipsis,
                            )
                            // Both parts of its state, and when its machine was last out of touch (docs/session-states.md).
                            Text(
                                listOf(statusLine(s, world), lostSpanText(world.machines.firstOrNull { it.machineId == s.machineId }, s)).filter { it.isNotEmpty() }.joinToString(" · "),
                                style = MaterialTheme.typography.labelSmall, maxLines = 1, overflow = TextOverflow.Ellipsis,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                },
                navigationIcon = { IconButton(onClick = back) { Icon(Icons.AutoMirrored.Filled.ArrowBack, t("Back", "返回")) } },
            )
        },
        bottomBar = {
            if (s != null) {
                val q = s.progress?.pendingQuestion
                when {
                    q != null -> QuestionPanel(model, s, q)
                    isLive(s) -> Surface(tonalElevation = 3.dp) {
                        Row(Modifier.fillMaxWidth().padding(12.dp), verticalAlignment = Alignment.CenterVertically) {
                            Text(statusLine(s, world), Modifier.weight(1f), color = MaterialTheme.colorScheme.primary)
                            OutlinedButton(onClick = { scope.launch { model.interrupt(s) } }) { Text(t("Interrupt", "中断")) }
                        }
                    }
                }
            }
        },
    ) { pad ->
        Box(Modifier.fillMaxSize().padding(pad)) {
            if (loading && items.isEmpty()) {
                CircularProgressIndicator(Modifier.align(Alignment.Center))
            } else {
                LazyColumn(state = list, modifier = Modifier.fillMaxSize(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    items(items, key = { it.key }) { Box(Modifier.animateItem(fadeInSpec = tween(Motion.Medium), placementSpec = tween(Motion.Medium, easing = Motion.EaseOut))) { ItemView(it, starter = s?.createdBy.orEmpty()) } }
                    item { Box(Modifier.heightIn(min = 8.dp)) }
                }
            }
        }
    }
}

/**
 * One conversation row. A message from someone other than the session's
 * starter (the owner in a session an agent started; another agent) is
 * named; dsh: a DSH session's page, where every message is the owner's and
 * those sent from Aide are marked so.
 */
@Composable
internal fun ItemView(item: Item, starter: String = "", dsh: Boolean = false) {
    val pad = Modifier.padding(horizontal = 12.dp)
    when (item) {
        is Item.User -> Column(pad.fillMaxWidth(), horizontalAlignment = Alignment.End) {
            val by = when {
                dsh -> when (item.from) {
                    "owner" -> t("Sent from Aide", "经 Aide 发送")
                    "" -> null
                    else -> item.fromName.ifEmpty { item.from }.let { t("$it via Aide", "$it 经 Aide 发送") }
                }
                item.from.isEmpty() || item.from == starter -> null
                item.from == "owner" -> t("You", "你")
                else -> item.fromName.ifEmpty { item.from }
            }
            by?.let { Text(it, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(end = 6.dp, bottom = 2.dp)) }
            // The owner's message, as the desktop's: a step above the page in either mode, never the accent (in dark mode it was white).
            Surface(color = aide.bubble, contentColor = aide.onBubble, shape = RoundedCornerShape(Radius.Xl), modifier = Modifier.widthIn(max = 320.dp)) {
                SelectionContainer { Text(item.text, Modifier.padding(horizontal = 15.dp, vertical = 9.dp), style = MaterialTheme.typography.bodyLarge) }
            }
        }
        is Item.Agent -> SelectionContainer { Text(item.text, pad, style = MaterialTheme.typography.bodyLarge) }
        is Item.Draft -> Text(item.text, pad, color = MaterialTheme.colorScheme.onSurfaceVariant)
        is Item.Tools -> ToolsView(item)
        is Item.Asked -> Surface(pad, color = MaterialTheme.colorScheme.secondaryContainer, shape = RoundedCornerShape(12.dp)) {
            Column(Modifier.padding(10.dp)) {
                Text(t("Question", "提问") + (item.resolution?.let { " · " + resolutionText(it) } ?: ""), style = MaterialTheme.typography.labelMedium)
                item.question.items.forEach { Text(it.text.ifEmpty { it.header }) }
            }
        }
        is Item.Message -> Text(
            (if (item.sent) "→ ${item.from}: " else "${item.from} →: ") + item.text, pad,
            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.tertiary,
        )
        is Item.Workspace -> Column(pad) {
            val r = item.request
            val by = if (r.from == "agent") t("asked by this task’s agent", "由这个任务的 agent 提出") else r.fromName.ifEmpty { r.from }
            Text(
                t("Workspace request: ", "工作区申请：") + workspaceKind(r.kind) + " " + r.item + " · " + workspaceStatus(r.status) + " · " + by,
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.tertiary,
            )
            if (r.note.isNotBlank()) SelectionContainer { Text(r.note, style = MaterialTheme.typography.bodySmall) }
        }
        is Item.Error -> Text(item.text, pad, color = MaterialTheme.colorScheme.error)
        is Item.TurnEnd -> Text(t("The turn failed: ", "本轮失败：") + item.text, pad, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
        is Item.Rollback -> Text(
            t("Rolled back to turn ${item.turn}: turns ${item.turn + 1} to ${item.lastTurn} above were discarded.", "已回滚到第 ${item.turn} 轮：上面第 ${item.turn + 1}–${item.lastTurn} 轮已丢弃。"),
            pad, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

private fun workspaceKind(k: String) = when (k) {
    "tool" -> t("tool", "工具")
    "permission" -> t("permission", "权限")
    "info" -> t("information", "信息")
    "system_change" -> t("system change", "系统变更")
    else -> k
}

private fun workspaceStatus(s: String) = when (s) {
    "pending" -> t("pending", "待批")
    "approved" -> t("approved", "已批准")
    "rejected" -> t("rejected", "已拒绝")
    "provisioned" -> t("provisioned", "已就位")
    "confirmed" -> t("confirmed", "已确认")
    else -> s
}

private fun resolutionText(r: String) = when (r) {
    "QUESTION_RESOLUTION_ANSWERED" -> t("answered", "已回答")
    "QUESTION_RESOLUTION_DECLINED" -> t("declined", "已拒绝")
    else -> r.removePrefix("QUESTION_RESOLUTION_").lowercase()
}

@Composable
private fun ToolsView(item: Item.Tools) {
    var open by remember { mutableStateOf(false) }
    val turn by animateFloatAsState(if (open) 90f else 0f, tween(Motion.Medium, easing = Motion.EaseOut), label = "chevron")
    // One quiet row until opened, as on the desktop: a fill a step above the page, the calls folding out under it.
    Surface(
        color = aide.fill, shape = RoundedCornerShape(Radius.Md),
        modifier = Modifier.padding(horizontal = 12.dp).fillMaxWidth().clip(RoundedCornerShape(Radius.Md)).clickable { open = !open },
    ) {
        Column(Modifier.padding(horizontal = 12.dp, vertical = 8.dp)) {
            val first = item.calls.first()
            Row(verticalAlignment = Alignment.CenterVertically) {
                Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, null, Modifier.size(16.dp).rotate(turn), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                Spacer(Modifier.width(6.dp))
                Text(
                    if (item.calls.size == 1) listOf(first.name, first.line).filter { it.isNotEmpty() }.joinToString(" · ") else t("${item.calls.size} tool calls", "${item.calls.size} 次工具调用") + " · ${item.calls.last().name}",
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis,
                )
            }
            AnimatedVisibility(
                open,
                enter = expandVertically(tween(Motion.Medium, easing = Motion.EaseOut)) + fadeIn(tween(Motion.Medium)),
                exit = shrinkVertically(tween(Motion.Short, easing = Motion.EaseOut)) + fadeOut(tween(Motion.Short)),
            ) {
                Column(Modifier.padding(top = 6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    item.calls.forEach { c ->
                        HorizontalDivider(color = aide.separator)
                        Text(listOf(c.name, c.line).filter { it.isNotEmpty() }.joinToString(" · "), style = MaterialTheme.typography.bodySmall, fontWeight = FontWeight.Medium, maxLines = 2, overflow = TextOverflow.Ellipsis)
                        // The raw arguments, under the line, when the line is not all they say (#37), in the same block as the result.
                        if (c.summary.isNotBlank() && c.summary != c.line) {
                            Surface(color = aide.fill2, shape = RoundedCornerShape(Radius.Sm)) {
                                Text(
                                    c.summary, Modifier.fillMaxWidth().padding(8.dp), style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 8, overflow = TextOverflow.Ellipsis,
                                )
                            }
                        }
                        c.result?.let {
                            Surface(color = aide.fill2, shape = RoundedCornerShape(Radius.Sm)) {
                                Text(
                                    it, Modifier.fillMaxWidth().padding(8.dp), style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace,
                                    color = if (c.isError) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 12, overflow = TextOverflow.Ellipsis,
                                )
                            }
                        }
                    }
                }
            }
        }
    }
}

/** The agent's question, each item with its options or a field, answered or left to the agent. */
@Composable
private fun QuestionPanel(model: AppModel, s: Session, q: Question) {
    val chosen = remember(q.questionId) { mutableStateMapOf<String, Set<String>>() }
    val typed = remember(q.questionId) { mutableStateMapOf<String, String>() }
    var busy by remember(q.questionId) { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    Surface(tonalElevation = 3.dp, modifier = Modifier.imePadding()) {
        Column(Modifier.fillMaxWidth().heightIn(max = 420.dp).verticalScroll(rememberScrollState()).padding(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(t("${agentTitle(s.agent)} asks", "${agentTitle(s.agent)} 在问"), style = MaterialTheme.typography.labelLarge, color = Warning)
            q.items.forEach { item ->
                if (item.header.isNotEmpty()) Text(item.header, style = MaterialTheme.typography.labelMedium)
                Text(item.text, fontWeight = FontWeight.Medium)
                item.options.forEach { o ->
                    val on = o.label in chosen[item.id].orEmpty()
                    FilterChip(selected = on, onClick = { chosen[item.id] = choose(item, chosen[item.id].orEmpty(), o.label) }, label = {
                        Column(Modifier.padding(vertical = 4.dp)) {
                            Text(o.label)
                            if (o.description.isNotEmpty()) Text(o.description, style = MaterialTheme.typography.bodySmall)
                        }
                    })
                }
                if (item.options.isEmpty() || item.allowOther) {
                    OutlinedTextField(
                        value = typed[item.id].orEmpty(), onValueChange = { typed[item.id] = it },
                        label = { Text(if (item.options.isEmpty()) t("Answer", "回答") else t("Other", "其他")) },
                        visualTransformation = if (item.secret) PasswordVisualTransformation() else androidx.compose.ui.text.input.VisualTransformation.None,
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
            }
            val answers = buildAnswers(q.items, chosen, typed)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Button(onClick = {
                    busy = true
                    scope.launch { model.answer(s, answers ?: return@launch, decline = false); busy = false }
                }, enabled = answers != null && !busy, modifier = Modifier.weight(1f)) { Text(t("Answer", "回答")) }
                TextButton(onClick = {
                    busy = true
                    scope.launch { model.answer(s, emptyList(), decline = true); busy = false }
                }, enabled = !busy) { Text(t("Let it decide", "让它自己决定")) }
            }
        }
    }
}
