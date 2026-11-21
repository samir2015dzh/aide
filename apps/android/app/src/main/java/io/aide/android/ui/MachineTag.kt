package io.aide.android.ui

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
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
    Surface(shape = RoundedCornerShape(50), color = MaterialTheme.colorScheme.secondaryContainer) {
        Text(
            world.machineName(machineId), Modifier.padding(horizontal = 8.dp, vertical = 1.dp),
            style = MaterialTheme.typography.labelMedium, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis,
            color = MaterialTheme.colorScheme.onSecondaryContainer,
        )
    }
}

/** Where what is written goes, by the way to write it (take over, message), while there are several machines. */
@Composable
fun WritesTo(world: World, machineId: String, modifier: Modifier = Modifier) {
    if (!manyMachines(world.machines.size)) return
    val name = world.machineName(machineId)
    Text(t("Goes to the session on $name", "发往 $name 上的这个会话"), modifier, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
}

/** One machine's sessions, or all: a row of chips over a list, while there are several machines. */
@Composable
fun MachineFilter(world: World, picked: String?, pick: (String?) -> Unit) {
    if (!manyMachines(world.machines.size)) return
    Row(Modifier.horizontalScroll(rememberScrollState()).padding(horizontal = 12.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        FilterChip(selected = picked == null, onClick = { pick(null) }, label = { Text(t("All machines", "全部机器")) })
        world.machines.sortedBy { it.machineName.lowercase() }.forEach { m ->
            FilterChip(selected = picked == m.machineId, onClick = { pick(m.machineId) }, label = { Text(m.machineName.ifEmpty { m.machineId.take(12) }) })
        }
    }
}

/** A list's group of one machine's sessions. */
@Composable
fun MachineGroupHeader(name: String, count: Int) {
    Text("$name · $count", Modifier.padding(start = 16.dp, top = 14.dp, bottom = 4.dp), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
}
