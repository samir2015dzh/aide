package io.aide.android.model

import io.aide.android.api.AgentModel
import io.aide.android.api.Machine
import io.aide.android.api.Project
import io.aide.android.api.Session
import io.aide.android.api.SessionStatus
import io.aide.android.ui.t

// A bot's conversations (工单 #67, the owner's six rules), as the desktop's @aide/core conversations.ts: behind a coding
// agent's bot are its sessions on its machine, each one the owner started a conversation. The bot's page goes on in its
// current one — the one chosen on this phone, else the one last active — until a new one is asked for, or its
// project, model, effort or mode change (said so). Archived ones are the machine's own archive: one source.

/** What a new conversation starts with: the composer's choices ("" the agent's own default). */
data class ConvParams(val projectId: String, val model: String = "", val effort: String = "", val mode: String = "")

enum class Stuck { ARCHIVED, PROJECT_GONE, AGENT_GONE }

data class BotConversations(val open: List<Session>, val archived: List<Session>, val others: List<Session>)

fun botConversations(machineId: String, agent: String, sessions: Collection<Session>): BotConversations {
    val its = sessions.filter { it.machineId == machineId && it.agent == agent }
    val mine = its.filter { it.createdBy == "owner" }
    val newest = compareByDescending<Session> { instant(it.updatedAt) }
    return BotConversations(
        mine.filter { it.status != SessionStatus.ARCHIVED }.sortedWith(newest),
        mine.filter { it.status == SessionStatus.ARCHIVED }.sortedWith(newest),
        its.filter { it.createdBy != "owner" && it.status != SessionStatus.ARCHIVED }.sortedWith(newest),
    )
}

/** How many there are of all (its own open and archived, those handed to it by other agents). */
val BotConversations.total: Int get() = open.size + archived.size + others.size

/**
 * The count beside the list when it has more than one (工单 #135 P33, the desktop's conv.countHint): how many are shown
 * of all and of which kinds, so the count and the list never disagree; null with one or none.
 */
fun convCountHint(c: BotConversations, shown: Int): String? {
    if (c.total <= 1) return null
    val detail = listOfNotNull(
        c.open.size.takeIf { it > 0 }?.let { t("$it open", "$it 条未归档") },
        c.archived.size.takeIf { it > 0 }?.let { t("$it archived", "$it 条已归档") },
        c.others.size.takeIf { it > 0 }?.let { t("$it handed over", "$it 条由其它 agent 派发") },
    ).joinToString(" · ")
    return t("Showing $shown of ${c.total} ($detail)", "当前展示 $shown 条，共 ${c.total} 条（$detail）")
}

/** The conversation the next message goes to: the one chosen ("new" for a new one), else the one last active. */
fun currentConversation(c: BotConversations, chosen: String?): Session? {
    if (chosen == "new") return null
    return chosen?.let { id -> (c.open + c.archived).firstOrNull { it.sessionId == id } } ?: c.open.firstOrNull()
}

fun stuckWhy(s: Session, projects: List<Project>, m: Machine?): Stuck? = when {
    s.status == SessionStatus.ARCHIVED -> Stuck.ARCHIVED
    m?.status != null && m.status.agents.none { it.name == s.agent } -> Stuck.AGENT_GONE
    projects.none { it.machineId == s.machineId && it.projectId == s.projectId } -> Stuck.PROJECT_GONE
    else -> null
}

/** Its choices; its model the one it runs, as the agent's models list it (an alias that runs it here, 工单 #99). */
fun paramsOf(s: Session, models: List<AgentModel> = emptyList()) = ConvParams(s.projectId, listedModel(s.model, models), s.effort, "")

/** Which choices differ from the conversation's own (its mode is not kept: any mode chosen is a change). */
fun changedParams(s: Session, p: ConvParams, models: List<AgentModel> = emptyList()): List<String> {
    val o = paramsOf(s, models)
    return listOfNotNull(
        "project".takeIf { o.projectId != p.projectId },
        "model".takeIf { o.model != p.model },
        "effort".takeIf { o.effort != p.effort },
        "mode".takeIf { p.mode.isNotEmpty() },
    )
}

/** A new conversation that carries an old one's context: its title, what it came to, then the new words. */
fun withContext(s: Session, title: String, text: String): String {
    val ended = s.result.trim().ifEmpty { s.progress?.lastMessageExcerpt?.trim().orEmpty() }
    return listOfNotNull("接着之前的对话《$title》继续。", ended.takeIf { it.isNotEmpty() }?.let { "它当时的结论：\n$it" }, "", text).joinToString("\n")
}
