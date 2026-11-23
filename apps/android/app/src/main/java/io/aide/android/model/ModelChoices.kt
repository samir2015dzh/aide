package io.aide.android.model

import io.aide.android.api.AgentModel
import io.aide.android.api.AgentStatus
import io.aide.android.ui.t
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive

// The models a conversation runs on (工单 #99), as the desktop's @aide/core machines.ts: each model in a list is its
// specific one, as the machine found it ("Opus 5.5", claude-opus-5-5 in full), the default naming the one it runs; a
// family name alone never is one.

private val claudeId = Regex("""^claude-([a-z]+)-(\d+)(?:-(\d{1,2}))?(?:-\d{8})?(\[1m])?$""")

/** A model as people call it: an alias's specific model, the name its agent gives it, a Claude model's ID read out ("claude-opus-5-5" → "Opus 5.5"), else the ID. */
fun modelTitle(id: String, models: List<AgentModel> = emptyList()): String {
    val m = models.firstOrNull { it.id == id }
    if (m != null && m.resolved.isNotEmpty()) return modelTitle(m.resolved)
    if (m != null && m.name.isNotEmpty()) return m.name
    val c = claudeId.matchEntire(id) ?: return id
    val (family, major, minor, long) = c.destructured
    return "${family.replaceFirstChar { it.uppercase() }} $major${if (minor.isNotEmpty()) ".$minor" else ""}${if (long.isNotEmpty()) " (1M)" else ""}"
}

/** A model in a list: what to pick, the specific model as people call it and in full; unknown, an alias whose specific model its agent did not say. */
data class ModelChoice(val value: String, val title: String, val detail: String, val unknown: Boolean = false) {
    /** As a list shows it: its title, the model in full beside it (or that it is not known). */
    fun label(): String = when {
        detail.isNotEmpty() -> "$title · $detail"
        unknown -> "$title · " + t("specific model unknown", "具体型号未知")
        else -> title
    }
}

/** A model ID in full as it reads: DSH's ["provider","model"] as provider/model. */
private fun idText(id: String): String {
    if (!id.startsWith("[")) return id
    val v = runCatching { Json.parseToJsonElement(id) }.getOrNull() as? JsonArray ?: return id
    return if (v.all { it is JsonPrimitive && it.isString }) v.joinToString("/") { (it as JsonPrimitive).content } else id
}

private fun choiceOf(agent: String, m: AgentModel): ModelChoice {
    if (m.resolved.isNotEmpty()) return ModelChoice(m.id, modelTitle(m.resolved), m.resolved)
    if (agent == "claude" && !m.id.startsWith("claude-")) return ModelChoice(m.id, m.name.ifEmpty { m.id }, "", unknown = true)
    // DSH's: its display name, its ID in full and where from (its provider; each line it comes by, 工单 #98 补充).
    val title = m.name.ifEmpty { modelTitle(m.id) }
    val id = idText(m.id)
    return ModelChoice(m.id, title, listOf(if (id == title) "" else id, m.source).filter { it.isNotEmpty() }.joinToString(" · "))
}

/** An agent's models as its lists show them; current, a model a conversation runs that is not among them, first. */
fun modelChoices(agent: String, models: List<AgentModel>, current: String = ""): List<ModelChoice> {
    val out = models.map { choiceOf(agent, it) }
    if (current.isEmpty() || out.any { it.value == current }) return out
    val title = modelTitle(current)
    return listOf(ModelChoice(current, title, idText(current).takeIf { it != title }.orEmpty())) + out
}

/** The model its sessions run when none is chosen: the machine's default for it, else the agent's own; null when neither is known. */
fun defaultModelChoice(a: AgentStatus?): ModelChoice? {
    if (a == null) return null
    val id = a.defaults?.model?.ifEmpty { null } ?: a.own?.model?.ifEmpty { null } ?: return null
    return a.models.firstOrNull { it.id == id || it.resolved == id }?.let { choiceOf(a.name, it) } ?: modelChoices(a.name, emptyList(), id).first()
}

/** A model a conversation runs, as its model list has it: the alias that resolves to it here, else itself. */
fun listedModel(id: String, models: List<AgentModel>): String =
    if (id.isEmpty() || models.any { it.id == id }) id else models.firstOrNull { it.resolved == id }?.id ?: id
