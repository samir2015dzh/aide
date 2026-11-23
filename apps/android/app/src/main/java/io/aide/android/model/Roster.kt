package io.aide.android.model

import io.aide.android.api.Client
import io.aide.android.api.Machine
import io.aide.android.api.Persona
import io.aide.android.api.Session
import io.aide.android.api.SessionStatus
import io.aide.android.ui.t
import java.time.Instant

// The roster (工单 #57, BOTARK-REF 产品结构纲要), as the desktop's @aide/core roster.ts: a bot is the unit the app is
// shown in. Each coding agent a machine detected is one bot, under its machine; each personal agent (a grant that
// follows the account) is one bot, on the account. Nobody files them: they come from what the machines and the account
// report. And one state per bot (工单 #56): the roster, Hand Work To and the counts all read botState, in the same words.

/** Why work can't go to it, or why it is not confirmed. */
enum class NotReady { OFFLINE, SIGNED_OUT, PAUSED, MISSING, UNKNOWN, CHECKING }

/** Whether work can go to it now: yes; unverified (it can be sent, but whether it is there is not confirmed); no. */
enum class Usable { YES, UNVERIFIED, NO }

data class BotState(
    val state: ConnState,
    /** Its state in a word or two, the same on every page. */
    val word: String,
    /** One sentence of what is known, the same on every page. */
    val line: String,
    val usable: Usable,
    val why: NotReady? = null,
)

/** A coding agent on a machine as one state (工单 #56); null: the machine reports no such agent (hidden or absent). */
fun codingBotState(m: Machine, agent: String, now: Instant, c: MachineConnections = machineConnections(m, now)): BotState? {
    val rows = c.rows.filter { it.agent == agent }
    if (rows.isEmpty()) return null
    val main = rows.firstOrNull { it.id == agent || it.id == "$agent-cli" } ?: rows.first()
    val paused = m.status?.agents?.firstOrNull { it.name == agent }?.pause != null
    val word = main.text.substringBefore(" · ")
    // Paused is the owner's own say, known whatever the machine last reported: it comes first.
    if (paused && main.state != ConnState.MISSING) return BotState(main.state, t("Paused", "已暂停"), t("Paused: no new task starts with it until it resumes", "已暂停：恢复之前不会有新任务交给它"), Usable.NO, NotReady.PAUSED)
    return when (main.state) {
        ConnState.OK, ConnState.PARTIAL -> BotState(main.state, word, main.text, Usable.YES)
        ConnState.CHECKING -> BotState(ConnState.CHECKING, word, main.lacks ?: main.text, Usable.UNVERIFIED, NotReady.CHECKING)
        ConnState.UNKNOWN ->
            if (c.reachable) BotState(ConnState.UNKNOWN, main.text, main.lacks ?: main.text, Usable.UNVERIFIED, NotReady.UNKNOWN)
            else BotState(ConnState.UNKNOWN, main.text, main.lacks ?: main.text, Usable.NO, NotReady.OFFLINE)
        ConnState.MISSING -> BotState(ConnState.MISSING, word, main.lacks ?: main.text, Usable.NO, if (main.text == t("Not signed in", "未登录")) NotReady.SIGNED_OUT else NotReady.MISSING)
    }
}

/** A personal agent's state: online now, or when it was last seen (it reports nothing else). */
fun personalBotState(c: Client, now: Instant): BotState {
    if (c.online) return BotState(ConnState.OK, t("Online", "在线"), t("Connected to your account now", "现在连着你的账号"), Usable.YES)
    val seen = instantOrNull(c.lastSeen)
    return BotState(ConnState.UNKNOWN, t("Not online", "不在线"), seen?.let { t("Last seen ${agoText(it, now)}", "上次见到：${agoText(it, now)}") } ?: t("Not connected yet", "还没连上过"), Usable.UNVERIFIED, NotReady.UNKNOWN)
}

data class Bot(
    /** "m:<machine id>/<agent>" or "c:<client id>": the same bot on every page. */
    val key: String,
    val personal: Boolean,
    /** Its one name: "DSH", "DSH · work-pc" while there are several machines, a personal agent's own name. */
    val name: String,
    /** The coding agent whose face it has ("" for a personal agent). */
    val agent: String,
    val machineId: String,
    /** A personal agent's grant; a coding agent's own grant, when it has one (DSH on work-pc). */
    val client: Client?,
    val state: BotState,
    /** What it is doing lately, in one line, or "". */
    val doing: String,
    val running: Int,
    val waiting: Int,
    /** The owner's contacts for it (docs/personas.md): the first is its name and face; there is no Hand Work To page. */
    val personas: List<Persona> = emptyList(),
    /** Its name under its machine's head in the roster (工单 #75): "DSH", never "DSH · work-pc"; "" for its one name. */
    val short: String = "",
) {
    /** Its name in a roster row: under its machine's head, without the machine. */
    val shortName: String get() = short.ifEmpty { name }

    /** A coding agent its machine lists (not only a grant): its connections, its projects, its tasks to hand out. */
    val coding: Boolean get() = !personal && agent.isNotEmpty() && key.startsWith("m:")
}

data class MachineBots(val machine: Machine, val bots: List<Bot>)

/** A connection of the account that is no agent (工单 #64): Aide's own client on a machine ("" when not known). */
data class AsideClient(val client: Client, val machineId: String)

data class Roster(val personal: List<Bot>, val machines: List<MachineBots>, val aside: List<AsideClient> = emptyList()) {
    val all: List<Bot> get() = personal + machines.flatMap { it.bots }
    /** The coding agents, every machine's, one list. */
    val coding: List<Bot> get() = machines.flatMap { it.bots }
}

fun botName(agent: String, m: Machine, machines: Int): String = if (manyMachines(machines)) "${agentTitle(agent)} · ${m.machineName}" else agentTitle(agent)

private val localWords = Regex("\\b(dsh|deepseek|claude code|codex|workbuddy|codebuddy|gemini cli|copilot cli|opencode|qwen code|kimi cli|aide cli)\\b", RegexOption.IGNORE_CASE)

/** Which kind of agent a grant is (工单 #52), as the desktop's clientCategory: the owner's choice, else its name. */
fun clientCategory(c: Client, machines: List<Machine>): Pair<Boolean, String?> {
    if (c.category == "personal") return true to null
    if (c.category == "local") return false to machines.firstOrNull { it.machineId == c.categoryMachineId }?.machineName
    val tail = Regex("^(.+?)\\s+on\\s+(.+)$", RegexOption.IGNORE_CASE).find(c.label)?.groupValues?.get(2) ?: Regex("^[^@\\s]+@(\\S+)$").find(c.label)?.groupValues?.get(1)
    val machine = tail?.let { x -> machines.firstOrNull { it.machineName.equals(x.trim(), ignoreCase = true) }?.machineName }
    if (machine != null) return false to machine
    if (localWords.containsMatchIn(c.label) || c.label.startsWith("client@", ignoreCase = true)) return false to tail?.trim()
    return true to null
}

private fun grantAgent(label: String): String? {
    val l = label.lowercase()
    return when {
        Regex("\\b(dsh|deepseek)\\b").containsMatchIn(l) -> "deepseek"
        Regex("\\bclaude\\b").containsMatchIn(l) -> "claude"
        Regex("\\bcodex\\b").containsMatchIn(l) -> "codex"
        Regex("\\b(workbuddy|codebuddy)\\b").containsMatchIn(l) -> "codebuddy"
        Regex("\\bgemini\\b").containsMatchIn(l) -> "gemini"
        Regex("\\bcopilot\\b").containsMatchIn(l) -> "copilot"
        Regex("\\bopencode\\b").containsMatchIn(l) -> "opencode"
        Regex("\\bqwen\\b").containsMatchIn(l) -> "qwen"
        Regex("\\bkimi\\b").containsMatchIn(l) -> "kimi"
        else -> null
    }
}

/** Aide's own client logged in on a machine under the name it makes up ("client@<machine>"): no agent (工单 #64). The same rule as the CLI's roster (Go identity.IsAideClient, 工单 #79). */
fun isAideClient(c: Client): Boolean = c.label.trim().startsWith("client@", ignoreCase = true)

/** A grant filed as a coding agent is one on the roster when it names a known coding agent, or the owner filed it so. */
private fun admitted(c: Client): Boolean = grantAgent(c.label) != null || c.category == "local"

/** A hosted app's own name, without where it signs in ("Claude (claude.ai)" → "Claude"). */
private fun grantName(label: String): String = Regex("^(.+) \\(([^()\\s]+)\\)$").find(label)?.groupValues?.get(1) ?: label

fun roster(machines: List<Machine>, clients: List<Client>, sessions: Collection<Session>, now: Instant, personas: List<Persona> = emptyList()): Roster {
    val sorted = machines.sortedWith(compareByDescending<Machine> { it.local }.thenByDescending { it.online }.thenBy { it.machineName })
    val personal = mutableListOf<Bot>()
    val aside = mutableListOf<AsideClient>()
    val localGrants = mutableMapOf<String, MutableList<Client>>()
    // Who gets on the roster (工单 #64), as the desktop's: personal agents, and coding agents (detected by their machine,
    // a grant naming a known one, or filed so by the owner). Aide's own client and grants of no known kind are set aside.
    for (c in clients) {
        if (c.revoked || c.herMachineId.isNotEmpty()) continue
        if (isAideClient(c)) {
            val tail = Regex("^[^@\\s]+@(\\S+)$").find(c.label.trim())?.groupValues?.get(1)
            aside += AsideClient(c, machines.firstOrNull { it.machineName.equals(tail, ignoreCase = true) }?.machineId ?: "")
            continue
        }
        val (isPersonal, machine) = clientCategory(c, machines)
        if (isPersonal) {
            personal += bot("c:${c.clientId}", true, grantName(c.label), "", "", c, personalBotState(c, now), sessions.filter { it.createdBy == c.clientId }, now)
            continue
        }
        localGrants.getOrPut(machines.firstOrNull { it.machineName == machine }?.machineId ?: "") { mutableListOf() } += c
    }
    personal.sortWith(compareByDescending<Bot> { it.client?.online == true }.thenBy { it.name })
    val groups = sorted.map { m ->
        val c = machineConnections(m, now)
        val grants = localGrants[m.machineId] ?: mutableListOf()
        val bots = mutableListOf<Bot>()
        for (agent in c.rows.map { it.agent }.distinct()) {
            val g = grants.firstOrNull { grantAgent(it.label) == agent }?.also { grants.remove(it) }
            val own = personas.filter { it.machineId == m.machineId && it.agent == agent }
            bots += bot("m:${m.machineId}/$agent", false, own.firstOrNull()?.name?.ifEmpty { null } ?: botName(agent, m, machines.size), agent, m.machineId, g, codingBotState(m, agent, now, c)!!, sessions.filter { it.machineId == m.machineId && it.agent == agent }, now)
                .copy(personas = own, short = own.firstOrNull()?.name?.ifEmpty { null } ?: agentTitle(agent))
        }
        // One the owner took off the roster (hidden on its machine, 工单 #82) stays off with its grant.
        val hidden = m.status?.agents.orEmpty().filter { it.hidden }.map { it.name }.toSet()
        val kept = { g: Client -> admitted(g) && grantAgent(g.label) !in hidden }
        for (g in grants.filter { !kept(it) }) aside += AsideClient(g, m.machineId)
        for (g in grants.filter(kept)) bots += bot("c:${g.clientId}", false, grantName(g.label), grantAgent(g.label) ?: "", m.machineId, g, personalBotState(g, now), sessions.filter { it.createdBy == g.clientId }, now)
            .copy(short = grantName(g.label).removeSuffix(" on ${m.machineName}").ifEmpty { grantName(g.label) })
        MachineBots(m, bots)
    }
    for (g in localGrants[""].orEmpty().filter { !admitted(it) }) aside += AsideClient(g, "")
    for (g in localGrants[""].orEmpty().filter { admitted(it) }) personal += bot("c:${g.clientId}", false, grantName(g.label), grantAgent(g.label) ?: "", "", g, personalBotState(g, now), sessions.filter { it.createdBy == g.clientId }, now)
    return Roster(personal, groups, aside)
}

private fun bot(key: String, personal: Boolean, name: String, agent: String, machineId: String, client: Client?, state: BotState, list: List<Session>, now: Instant): Bot {
    val live = list.filter { it.status != SessionStatus.ARCHIVED }
    val running = live.filter { isLive(it) }
    val waiting = live.count { it.status == SessionStatus.WAITING_INPUT }
    val latest = live.maxByOrNull { instant(it.updatedAt) }
    val parts = listOfNotNull(
        if (waiting > 0) t("$waiting waiting for you", "$waiting 个等你回答") else null,
        if (running.isNotEmpty()) t("${running.size} running", "${running.size} 个进行中") else null,
    )
    val doing = when {
        latest == null -> ""
        parts.isNotEmpty() -> (parts + displayTitle(running.firstOrNull() ?: latest)).joinToString(" · ")
        else -> t("Last: ${displayTitle(latest)} · ${agoText(instant(latest.updatedAt), now)}", "最近：${displayTitle(latest)} · ${agoText(instant(latest.updatedAt), now)}")
    }
    return Bot(key, personal, name, agent, machineId, client, state, doing, running.size, waiting)
}

/** The counts at the top (工单 #56): ready ones, unknown ones apart, of all. */
data class BotCounts(val ready: Int, val unknown: Int, val total: Int) {
    val text: String get() = listOfNotNull(t("$ready ready", "可用 $ready"), if (unknown > 0) t("$unknown unknown", "未知 $unknown") else null, t("of $total", "共 $total")).joinToString(" · ")
}

fun botCounts(bots: List<Bot>): BotCounts = BotCounts(
    bots.count { it.state.usable == Usable.YES },
    bots.count { it.state.state == ConnState.UNKNOWN || it.state.state == ConnState.CHECKING },
    bots.size,
)
