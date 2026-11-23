package io.aide.android.ui

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsFocusedAsState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.aide.android.api.Client
import io.aide.android.api.Machine
import io.aide.android.api.Persona
import io.aide.android.model.AppModel
import io.aide.android.model.Bot
import io.aide.android.model.instantOrNull
import io.aide.android.model.roster
import java.time.Instant

// A bot's settings on a page of their own (工单 #97): its page keeps who it is, what it is doing and talking to it; what is
// set about it is here, each once — its names and faces, what it uses, its kind, pausing it, its grant, taking it off Aide.
// What each does is unchanged (#52, #82, #89, #93): only where it is shown moved.
//
// The controls here follow the Fluent reference (工单 #97 补充三), on this page alone, the app's theme untouched: cards
// with a 1dp line and 8dp corners, a choice as a list of radio rows (the picked one ticked, tinted, a bar of the accent at
// its left), a switch 40×20, a 2dp ring around what has the keyboard's focus. No pill with a small arrow.

/** The page: from the bot page's Settings, back to it. */
@Composable
fun BotSettingsPage(model: AppModel, key: String, edit: (Persona) -> Unit, back: () -> Unit) {
    val world by model.world.collectAsState()
    val personas by model.personas.collectAsState()
    LaunchedEffect(Unit) { model.loadPersonas() }
    val b = roster(world.machines, world.clients, world.sessions.values, Instant.now(), personas.orEmpty()).all.firstOrNull { it.key == key }
    val m = b?.let { x -> world.machines.firstOrNull { it.machineId == x.machineId } }
    var removing by remember { mutableStateOf(false) }
    Scaffold(
        snackbarHost = { PageFeedback(model) },
        topBar = {
            NavBar(back) {
                Column(horizontalAlignment = Alignment.CenterHorizontally) {
                    Text(t("Settings", "设置"), maxLines = 1)
                    b?.let { Text(it.name, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis) }
                }
            }
        },
    ) { pad ->
        if (b == null) {
            Centered { Text(t("This bot is gone.", "这个 bot 已经不在了。")) }
            return@Scaffold
        }
        val agent = m?.status?.agents.orEmpty().firstOrNull { it.name == b.agent }
        LazyColumn(Modifier.fillMaxSize().padding(pad), contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 32.dp)) {
            if (m != null && b.coding) item(key = "names") { NamesAndFaces(b, m, edit) }
            if (m != null && b.coding && agent != null) item(key = "agent") { AgentSettings(model, b, m) }
            b.client?.let { c -> item(key = "grant") { GrantCard(c) } }
            // Taking it off Aide, last: a coding agent is hidden on its machine (#93), a grant of its own revoked (#82).
            item(key = "remove") {
                Box(Modifier.padding(top = 24.dp)) {
                    if (m != null && b.coding && agent != null) RemoveAgent(model, b, m)
                    else FluentDangerButton(if (b.key.startsWith("m:")) t("Remove from the roster…", "从名册移除…") else t("Delete…", "删除…")) { removing = true }
                }
            }
        }
    }
    if (removing && b != null) RemoveBotDialog(model, b, m?.machineName.orEmpty()) { removing = false }
}

/**
 * Its names and faces (docs/personas.md), once (工单 #97: the row in its settings and the block under them were two): each
 * is its face and name with a pencil (工单 #65); a row adds one — none yet, the first takes its name and face (工单 #83).
 */
@Composable
private fun NamesAndFaces(b: Bot, m: Machine, edit: (Persona) -> Unit) {
    Column(Modifier.fillMaxWidth()) {
        SectionHeader(t("Name and avatar", "名字和头像"))
        Column(Modifier.fillMaxWidth().fluentCard()) {
            b.personas.forEach { p ->
                Row(Modifier.fillMaxWidth().heightIn(min = 48.dp).rowPress { edit(p) }.padding(horizontal = 16.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
                    PersonaAvatar(b, p, 32)
                    HSpace(12)
                    Column(Modifier.weight(1f)) {
                        Text(p.name, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.SemiBold)
                        Text(listOf(p.projectId, p.model, p.effort, p.mode).filter { it.isNotEmpty() }.joinToString(" · "), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                    Icon(Icons.Filled.Edit, t("Edit ${p.name}", "修改「${p.name}」"), tint = MaterialTheme.colorScheme.primary, modifier = Modifier.size(18.dp))
                }
                FluentDivider()
            }
            Row(Modifier.fillMaxWidth().heightIn(min = 48.dp).rowPress { edit(newPersona(b, m)) }.padding(horizontal = 16.dp, vertical = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                Icon(Icons.Filled.Add, null, tint = MaterialTheme.colorScheme.primary, modifier = Modifier.size(18.dp))
                HSpace(8)
                Text(if (b.personas.isEmpty()) t("Give it a name and a face", "给它起名字、换头像") else t("Another name and face", "再加一个名字和头像"), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.primary)
            }
        }
    }
}

/** Its grant (工单 #97 补充: off its page, here): what it is called, until when; the rest is set on a computer. */
@Composable
private fun GrantCard(c: Client) {
    Column(Modifier.fillMaxWidth()) {
        SectionHeader(t("Its grant", "接入凭据"))
        Column(Modifier.fillMaxWidth().fluentCard().padding(horizontal = 16.dp, vertical = 12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(c.label, style = MaterialTheme.typography.bodyMedium)
            instantOrNull(c.expiresAt)?.takeIf { c.herMachineId.isEmpty() }?.let { Text(t("Valid until ${it.toString().take(10)}", "有效期至 ${it.toString().take(10)}"), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
            Text(t("Its kind, projects and expiry are set in Aide on a computer: Settings › Access.", "它的类别、可用项目和有效期在电脑上的 Aide 里设置：设置 › 授权。"), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

// —— The Fluent controls of this page (工单 #97 补充三) ——

/** A settings card: the card's colour, a 1dp line round it, 8dp corners (Fluent's cards), no shadow. */
@Composable
internal fun Modifier.fluentCard(): Modifier {
    val shape = RoundedCornerShape(8.dp)
    return this.clip(shape).background(aide.card).border(1.dp, MaterialTheme.colorScheme.outlineVariant, shape)
}

/** A 1dp line between two rows of a card. */
@Composable
internal fun FluentDivider() = Box(Modifier.padding(start = 16.dp).fillMaxWidth().height(1.dp).background(MaterialTheme.colorScheme.outlineVariant))

/** The 2dp ring in the accent, 2dp inside the row's edge, while it has the keyboard's focus; nothing moves for it. */
@Composable
private fun Modifier.focusRing(focused: Boolean): Modifier {
    val color = MaterialTheme.colorScheme.primary
    return this.drawWithContent {
        drawContent()
        if (focused) {
            val inset = 3.dp.toPx()
            drawRoundRect(color, Offset(inset, inset), Size(size.width - 2 * inset, size.height - 2 * inset), CornerRadius(4.dp.toPx()), Stroke(2.dp.toPx()))
        }
    }
}

/** A fact of a card: its name, its value at the end. */
@Composable
internal fun FluentInfoRow(k: String, v: String) {
    Row(Modifier.fillMaxWidth().heightIn(min = 48.dp).padding(horizontal = 16.dp, vertical = 12.dp), horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) {
        Text(k, style = MaterialTheme.typography.bodyLarge)
        Text(v, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.weight(1f), textAlign = TextAlign.End)
    }
}

/** One choice, its words and what it means under them. */
data class FluentChoice<T>(val value: T, val text: String, val sub: String = "")

/**
 * A choice laid out flat (no pill, no menu): one radio row each, 48dp or more. The picked one has the accent's bar at its
 * left (3×20dp), a filled radio (20dp ring, 10dp dot) and a faint tint of the accent; the others a grey ring.
 */
@Composable
internal fun <T> FluentRadioList(choices: List<FluentChoice<T>>, selected: T, onSelect: (T) -> Unit) {
    Column(Modifier.fillMaxWidth().fluentCard().selectableGroup()) {
        choices.forEachIndexed { i, c ->
            if (i > 0) FluentDivider()
            val on = c.value == selected
            val interaction = remember { MutableInteractionSource() }
            val focused by interaction.collectIsFocusedAsState()
            val tint by animateColorAsState(if (on) MaterialTheme.colorScheme.primary.copy(alpha = 0.10f) else Color.Transparent, tween(Motion.Short), label = "choice")
            Box(Modifier.fillMaxWidth().heightIn(min = 48.dp).background(tint).selectable(on, interaction, Highlight(aide.pressed), role = Role.RadioButton) { if (!on) onSelect(c.value) }.focusRing(focused)) {
                if (on) Box(Modifier.align(Alignment.CenterStart).size(3.dp, 20.dp).background(MaterialTheme.colorScheme.primary, RoundedCornerShape(2.dp)))
                Row(Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 10.dp, bottom = 10.dp), verticalAlignment = Alignment.CenterVertically) {
                    FluentRadio(on)
                    HSpace(12)
                    Column(Modifier.weight(1f)) {
                        Text(c.text, style = MaterialTheme.typography.bodyLarge, fontWeight = if (on) FontWeight.SemiBold else FontWeight.Normal, color = if (on) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurface)
                        if (c.sub.isNotEmpty()) Text(c.sub, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
            }
        }
    }
}

@Composable
private fun FluentRadio(on: Boolean) {
    val ring = if (on) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant
    Box(Modifier.size(20.dp).border(1.dp, ring, CircleShape), contentAlignment = Alignment.Center) {
        if (on) Box(Modifier.size(10.dp).background(MaterialTheme.colorScheme.primary, CircleShape))
    }
}

/** A setting that is on or off: its words (and a line under them), the switch at the end; the whole row toggles it. */
@Composable
internal fun FluentSwitchRow(text: String, sub: String, checked: Boolean, onCheckedChange: (Boolean) -> Unit) {
    val interaction = remember { MutableInteractionSource() }
    val focused by interaction.collectIsFocusedAsState()
    Row(
        Modifier.fillMaxWidth().heightIn(min = 48.dp).toggleable(checked, interaction, Highlight(aide.pressed), role = Role.Switch, onValueChange = onCheckedChange).focusRing(focused)
            .padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(text, style = MaterialTheme.typography.bodyLarge)
            if (sub.isNotEmpty()) Text(sub, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        HSpace(12)
        FluentSwitch(checked)
    }
}

/** Fluent's toggle: a 40×20 track; off, a 1dp grey line with a grey 12dp knob at the left; on, the accent with a white knob at the right. */
@Composable
private fun FluentSwitch(on: Boolean) {
    val x by animateDpAsState(if (on) 24.dp else 4.dp, tween(Motion.Short), label = "knob")
    val track by animateColorAsState(if (on) MaterialTheme.colorScheme.primary else Color.Transparent, tween(Motion.Short), label = "track")
    val line = if (on) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant
    Box(Modifier.size(40.dp, 20.dp).clip(CircleShape).background(track).border(1.dp, line, CircleShape)) {
        Box(Modifier.align(Alignment.CenterStart).offset(x = x).size(12.dp).background(if (on) Color.White else MaterialTheme.colorScheme.onSurfaceVariant, CircleShape))
    }
}

/** A row that does something once (no state of its own): the words in the accent. */
@Composable
internal fun FluentActionRow(text: String, onClick: () -> Unit) {
    Text(text, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.primary,
        modifier = Modifier.fillMaxWidth().heightIn(min = 48.dp).rowPress(onClick).padding(horizontal = 16.dp, vertical = 13.dp))
}

/** What can't be undone from here: a 40dp button, a 1dp red line, 4dp corners, the words in red. */
@Composable
internal fun FluentDangerButton(text: String, onClick: () -> Unit) {
    val shape = RoundedCornerShape(4.dp)
    val interaction = remember { MutableInteractionSource() }
    val focused by interaction.collectIsFocusedAsState()
    Box(
        Modifier.fillMaxWidth().heightIn(min = 40.dp).clip(shape).background(aide.card).border(if (focused) 2.dp else 1.dp, MaterialTheme.colorScheme.error, shape)
            .clickable(interaction, Highlight(aide.pressed), role = Role.Button, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) { Text(text, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(vertical = 10.dp)) }
}
