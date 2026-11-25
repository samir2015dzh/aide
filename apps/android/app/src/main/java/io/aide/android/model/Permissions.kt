package io.aide.android.model

import io.aide.android.api.AgentMode
import io.aide.android.api.Project
import io.aide.android.api.title
import io.aide.android.ui.t

// DSH's permission presets (工单 #89), as the desktop's (packages/core permissions.ts): DSH's own, none made up by
// Aide. A session's preset is one of its parameters; a project can have one its DSH sessions start with when none is
// chosen (Project.dshPreset). Other agents keep their own modes, untouched by this.

/** DSH's name among the coding agents. */
const val DSH_AGENT = "deepseek"

/** Coding agents that share the three-tier permission presets (DSH + Antigravity, 工单 #89 / #111). */
fun usesPermissionPresets(agent: String): Boolean = agent == DSH_AGENT || agent == "antigravity"

/** DSH's own presets, least allowed first; any other its configuration has keeps its name. */
private val dshPresets = listOf("read-only", "workspace-write", "danger-full-access")

/** A mode as people call it: a DSH preset by the name DSH gives it ("仅可查看"), else its agent's name for it, else its id. */
fun modeLabel(agent: String, m: AgentMode): String = if (usesPermissionPresets(agent)) when (m.id) {
    "read-only" -> t("Read only", "仅可查看")
    "workspace-write" -> t("Changes in the workspace", "工作区内修改")
    "danger-full-access" -> t("Full access", "完全权限")
    else -> m.name.ifEmpty { m.id }
} else m.name.ifEmpty { m.id }

/** Whether a session's mode allows more than its project's preset: further down the list, or one that can't be told apart. */
fun aboveProject(agent: String, mode: String, preset: String): Boolean {
    if (!usesPermissionPresets(agent) || mode.isEmpty() || preset.isEmpty() || mode == preset) return false
    val a = dshPresets.indexOf(mode)
    val b = dshPresets.indexOf(preset)
    return a < 0 || b < 0 || a > b
}

/** What the box says of a mode next to the project's preset: the default choice's label, and the line when above it. */
data class ModeChoice(val defaultLabel: String?, val above: String?)

fun modeChoice(agent: String, mode: String, project: Project?, modes: List<AgentMode>): ModeChoice {
    val preset = if (usesPermissionPresets(agent)) project?.dshPreset.orEmpty() else ""
    fun name(id: String) = modeLabel(agent, modes.firstOrNull { it.id == id } ?: AgentMode(id))
    return ModeChoice(
        preset.takeIf { it.isNotEmpty() }?.let { t("Project's default: ${name(it)}", "项目默认：${name(it)}") },
        if (aboveProject(agent, mode, preset)) {
            if (agent == DSH_AGENT) {
                t(
                    "Above the DSH permission project “${project?.title()}” sets (“${name(preset)}”): this task runs with “${name(mode)}”, as you chose",
                    "比项目「${project?.title()}」定的 DSH 权限「${name(preset)}」高：这次会按你选的「${name(mode)}」来",
                )
            } else {
                t(
                    "Above the permission project “${project?.title()}” sets (“${name(preset)}”): this task runs with “${name(mode)}”, as you chose",
                    "比项目「${project?.title()}」定的权限「${name(preset)}」高：这次会按你选的「${name(mode)}」来",
                )
            }
        } else null,
    )
}
