package io.aide.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.KeyboardArrowUp
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import io.aide.android.model.AppModel
import kotlinx.coroutines.launch

// The box to write to a bot (工单 #66), as the desktop's and BotArk's composer: one rounded capsule at the foot — + on
// the left for what goes with the message, the line to write in ("Message <bot>"), a round send button on the right,
// lit once there is something to send — with its choices (project, model…) as small chips in it. What became of a
// message is said in it (工单 #61): sending, sent, why not.

sealed interface ComposerState {
    data object Idle : ComposerState
    data object Sending : ComposerState
    data class Done(val text: String) : ComposerState
    data class Failed(val text: String, val retry: (() -> Unit)? = null) : ComposerState
}

data class PlusItem(val label: String, val enabled: Boolean = true, val run: () -> Unit)

/** A line over the box: why it can't send, or what the next message does. */
data class ComposerWhy(val text: String, val warn: Boolean = false, val action: Pair<String, () -> Unit>? = null)

@OptIn(ExperimentalLayoutApi::class)
@Composable
fun Composer(
    value: String,
    onChange: (String) -> Unit,
    onSend: () -> Unit,
    placeholder: String,
    state: ComposerState,
    enabled: Boolean = true,
    why: ComposerWhy? = null,
    plus: List<PlusItem> = emptyList(),
    chips: (@Composable () -> Unit)? = null,
) {
    val sending = state is ComposerState.Sending
    val can = enabled && !sending && value.isNotBlank()
    Column(Modifier.fillMaxWidth().imePadding().padding(start = 10.dp, end = 10.dp, top = 6.dp, bottom = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        why?.let { w ->
            Row(Modifier.padding(horizontal = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(w.text, style = MaterialTheme.typography.labelMedium, color = if (w.warn) Warning else MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.weight(1f, fill = false))
                w.action?.let { (label, run) ->
                    HSpace(8)
                    Text(label, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.primary, fontWeight = FontWeight.SemiBold, modifier = Modifier.pressable(run))
                }
            }
        }
        Column(
            Modifier.fillMaxWidth().clip(RoundedCornerShape(24.dp)).background(aide.card).border(0.5.dp, aide.cardEdge, RoundedCornerShape(24.dp)).padding(6.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            chips?.let {
                FlowRow(Modifier.padding(start = 42.dp, end = 4.dp, top = 2.dp), horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) { it() }
            }
            Row(verticalAlignment = Alignment.Bottom) {
                if (plus.isNotEmpty()) {
                    var open by remember { mutableStateOf(false) }
                    Box {
                        RoundButton(enabled = enabled, filled = false, onClick = { open = true }) { Icon(Icons.Filled.Add, t("More", "更多"), Modifier.size(20.dp)) }
                        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
                            plus.forEach { p -> DropdownMenuItem(text = { Text(p.label) }, enabled = p.enabled, onClick = { open = false; p.run() }) }
                        }
                    }
                }
                Box(Modifier.weight(1f).padding(horizontal = 8.dp, vertical = 9.dp)) {
                    if (value.isEmpty()) Text(placeholder, style = MaterialTheme.typography.bodyLarge, color = aide.faint, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    BasicTextField(
                        value, onChange, Modifier.fillMaxWidth().heightIn(max = 140.dp), enabled = enabled,
                        textStyle = MaterialTheme.typography.bodyLarge.copy(color = MaterialTheme.colorScheme.onSurface),
                        cursorBrush = SolidColor(MaterialTheme.colorScheme.primary), maxLines = 6,
                    )
                }
                RoundButton(enabled = can, filled = true, onClick = onSend) {
                    if (sending) CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp, color = Color.White)
                    else Icon(Icons.Filled.KeyboardArrowUp, t("Send", "发送"), Modifier.size(22.dp))
                }
            }
        }
        // What became of the last message, here (工单 #61).
        when (state) {
            ComposerState.Sending -> StateLine(t("Sending…", "正在发送…"), aide.faint)
            is ComposerState.Done -> StateLine(state.text, Positive)
            is ComposerState.Failed -> Row(Modifier.padding(horizontal = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(state.text, style = MaterialTheme.typography.labelMedium, color = Warning, modifier = Modifier.weight(1f, fill = false))
                state.retry?.let { HSpace(8); Text(t("Retry", "重试"), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.primary, modifier = Modifier.pressable(it)) }
            }
            ComposerState.Idle -> {}
        }
    }
}

@Composable
private fun StateLine(text: String, color: Color) {
    Text(text, style = MaterialTheme.typography.labelMedium, color = color, modifier = Modifier.padding(horizontal = 12.dp))
}

@Composable
private fun RoundButton(enabled: Boolean, filled: Boolean, onClick: () -> Unit, content: @Composable () -> Unit) {
    val bg = when {
        filled && enabled -> MaterialTheme.colorScheme.primary
        else -> aide.fill2
    }
    val fg = if (filled && enabled) MaterialTheme.colorScheme.onPrimary else if (enabled) MaterialTheme.colorScheme.onSurface else aide.faint
    Box(
        Modifier.size(36.dp).clip(CircleShape).background(bg).then(if (enabled) Modifier.pressable(onClick) else Modifier),
        contentAlignment = Alignment.Center,
    ) {
        androidx.compose.runtime.CompositionLocalProvider(androidx.compose.material3.LocalContentColor provides fg) { content() }
    }
}

/** A choice in the composer as a quiet chip that opens its options (an app's own control, not a form's, 工单 #68). */
@Composable
fun <T> ChipPicker(label: String, value: T, options: List<Pair<T, String>>, onChange: (T) -> Unit) {
    var open by remember { mutableStateOf(false) }
    Box {
        Row(
            Modifier.clip(CircleShape).background(aide.fill2).pressable { open = true }.padding(start = 10.dp, end = 6.dp, top = 4.dp, bottom = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(options.firstOrNull { it.first == value }?.second ?: label, fontSize = 12.sp, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
            Icon(Icons.Filled.KeyboardArrowDown, label, Modifier.size(14.dp), tint = aide.faint)
        }
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            options.forEach { (v, l) -> DropdownMenuItem(text = { Text(if (v == value) "✓  $l" else "    $l") }, onClick = { open = false; onChange(v) }) }
        }
    }
}

/** A chip that is on or off (a room's @): a capsule, filled while on. */
@Composable
fun ToggleChip(label: String, on: Boolean, onClick: () -> Unit) {
    // On: filled in the theme's strong colour with a tick, as Material's selected filter chip, so it reads at a glance.
    Text(
        if (on) "✓ $label" else label,
        Modifier.clip(CircleShape).background(if (on) MaterialTheme.colorScheme.secondaryContainer else aide.fill2).pressable(onClick).padding(horizontal = 10.dp, vertical = 4.dp),
        fontSize = 12.sp, fontWeight = if (on) FontWeight.SemiBold else FontWeight.Normal,
        color = if (on) MaterialTheme.colorScheme.onSecondaryContainer else MaterialTheme.colorScheme.onSurfaceVariant,
    )
}

/**
 * A page's own place to say things (工单 #61): what an action on this page says shows here, at the foot of this page,
 * while it is open — not on the tabs underneath once it is closed. Put in its Scaffold's snackbarHost.
 */
@Composable
fun PageFeedback(model: AppModel) {
    val host = remember { SnackbarHostState() }
    val scope = rememberCoroutineScope()
    DisposableEffect(model) {
        val sink: (String) -> Unit = { text -> scope.launch { host.showSnackbar(text) } }
        model.pushSink(sink)
        onDispose { model.popSink(sink) }
    }
    SnackbarHost(host)
}
