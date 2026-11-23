package io.aide.android.model

import io.aide.android.api.AgentStatus
import io.aide.android.api.Persona
import io.aide.android.api.Project
import io.aide.android.api.TalkToPersonaResponse
import io.aide.android.ui.t

// Contacts (docs/personas.md): what the screens work out of them. Their avatars: model/Avatar.kt (工单 #83).

/**
 * What a contact being added or changed can pick, on its machine: the
 * coding agents there, the projects that agent may work in, the agent's
 * models, the efforts of the model picked (or of all), and its modes.
 */
data class PersonaChoices(
    val agents: List<AgentStatus>,
    val agent: AgentStatus?,
    val projects: List<Project>,
    val efforts: List<String>,
)

fun personaChoices(world: World, p: Persona): PersonaChoices {
    val agents = world.machines.firstOrNull { it.machineId == p.machineId }?.status?.agents.orEmpty().filter { it.available || it.name == p.agent }
    val a = agents.firstOrNull { it.name == p.agent }
    val projects = world.projects.filter { it.machineId == p.machineId && a != null && (it.agents.isEmpty() || a.name in it.agents) }
    val efforts = a?.let { ag -> ag.models.firstOrNull { it.id == p.model }?.efforts ?: ag.models.flatMap { it.efforts }.distinct() }.orEmpty()
    return PersonaChoices(agents, a, projects, efforts)
}

/** Whether it can be kept: a name, and where its sessions run. */
fun personaReady(p: Persona): Boolean = p.name.isNotBlank() && p.machineId.isNotEmpty() && p.agent.isNotEmpty() && p.projectId.isNotEmpty()

/** What the app says once a message went to a contact. */
fun talkedText(name: String, r: TalkToPersonaResponse): String {
    val title = r.session?.let(::displayTitle).orEmpty()
    if (!r.started) return t("Sent to $name (“$title”)", "已发给 $name（「$title」）")
    val why = when (r.reason) {
        "archived" -> t(": its task was archived", "：原任务已归档")
        "gone" -> t(": its task was deleted", "：原任务已删除")
        "changed" -> t(": its settings changed", "：它的设置改了")
        else -> ""
    }
    return t("$name started “$title”$why", "$name 新开了任务「$title」$why")
}
