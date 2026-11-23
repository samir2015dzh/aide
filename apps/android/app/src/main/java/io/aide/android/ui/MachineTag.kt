package io.aide.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.ui.Alignment
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.aide.android.model.World
import io.aide.android.model.manyMachines

// Whose sessions are whose (the owner's rule, 2026-11-21; model/MachineScope.kt): shown only while there
// are several machines.

/** A session's machine where its title is: with several machines, which one it is on; with one, nothing. */
@Composable
fun MachineTag(world: World, machineId: String) {
    if (!manyMachines(world.machines.size)) return
    Text(
        world.machineName(machineId), Modifier.clip(CircleShape).background(aide.fill2).padding(horizontal = 8.dp, vertical = 1.dp),
        style = MaterialTheme.typography.labelSmall, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
}

/** Where what is written goes, by the way to write it (take over, message), while there are several machines. */
@Composable
fun WritesTo(world: World, machineId: String, modifier: Modifier = Modifier) {
    if (!manyMachines(world.machines.size)) return
    val name = world.machineName(machineId)
    Text(t("Goes to the session on $name", "发往 $name 上的这个会话"), modifier, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
}

/**
 * One machine's sessions, or all: a row of capsules, each with its count (BotArk reference §5: the picked one filled,
 * the rest quiet), while there are several machines.
 */
@Composable
fun MachineFilter(world: World, picked: String?, pick: (String?) -> Unit) {
    if (!manyMachines(world.machines.size)) return
    val counts = world.sessions.values.groupingBy { it.machineId }.eachCount()
    val machines = world.machines.sortedBy { it.machineName.lowercase() }
    // A few machines: iOS's segmented control (a scope bar); more than fit, the capsules scroll.
    if (machines.size <= 3) {
        Segmented(
            listOf<Pair<String?, String>>(null to "${t("All", "全部")}  ${world.sessions.size}") + machines.map { m -> m.machineId to "${m.machineName.ifEmpty { m.machineId.take(12) }}  ${counts[m.machineId] ?: 0}" },
            picked, Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
        ) { pick(it) }
        return
    }
    Row(Modifier.horizontalScroll(rememberScrollState()).padding(horizontal = 16.dp, vertical = 4.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Capsule(t("All", "全部"), world.sessions.size, picked == null) { pick(null) }
        world.machines.sortedBy { it.machineName.lowercase() }.forEach { m ->
            Capsule(m.machineName.ifEmpty { m.machineId.take(12) }, counts[m.machineId] ?: 0, picked == m.machineId) { pick(m.machineId) }
        }
    }
}

@Composable
private fun Capsule(label: String, count: Int, picked: Boolean, onClick: () -> Unit) {
    val fg = if (picked) MaterialTheme.colorScheme.surface else MaterialTheme.colorScheme.onSurface
    Row(
        Modifier.clip(CircleShape).background(if (picked) MaterialTheme.colorScheme.onSurface else aide.card)
            .border(0.5.dp, if (picked) Color.Transparent else aide.cardEdge, CircleShape)
            .dimPress(onClick = onClick).padding(horizontal = 14.dp, vertical = 7.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(label, style = MaterialTheme.typography.labelLarge, fontWeight = FontWeight.SemiBold, color = fg, maxLines = 1)
        Text("  $count", style = MaterialTheme.typography.labelLarge, color = fg.copy(alpha = 0.55f))
    }
}

/** A list's group of one machine's sessions. */
@Composable
fun MachineGroupHeader(name: String, count: Int) {
    Text("$name · $count", Modifier.padding(start = 16.dp, top = 14.dp, bottom = 4.dp), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
}
