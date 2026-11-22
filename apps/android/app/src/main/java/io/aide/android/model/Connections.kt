package io.aide.android.model

import io.aide.android.api.AgentStatus
import io.aide.android.api.DSHBridge
import io.aide.android.api.Machine
import io.aide.android.ui.t
import java.time.Duration
import java.time.Instant

// A machine's connections (工单 #49), as the desktop's @aide/core connections.ts works them out, row for row and in
// the same words: Claude Desktop and the Claude Code command line apart (#44), DSH and its bridge plugin apart (#46),
// then each other agent found; a finding the machine has not confirmed lately, or one of a machine out of reach, is
// unknown, with when it was checked, never passed off as now.

/** A finding older than this shows as unknown. */
val STALE_AFTER: Duration = Duration.ofMinutes(15)

enum class ConnState { OK, PARTIAL, MISSING, UNKNOWN, CHECKING }

data class ConnRow(
    val id: String,
    val agent: String,
    val title: String,
    val state: ConnState,
    val text: String,
    val lacks: String? = null,
    val checkedAt: Instant? = null,
    /** Signing it in from Aide is offered (Claude Code's command line, 工单 #51). */
    val login: Boolean = false,
    /** Not known now: what it said when last checked (工单 #53: said as such under its entry, never as now). */
    val was: String? = null,
)

data class MachineConnections(val reachable: Boolean, val rows: List<ConnRow>, val state: ConnState)

private const val NOT_FOUND = "AGENT_PROBLEM_NOT_FOUND"
private const val APP_WITHOUT_CLI = "AGENT_PROBLEM_APP_WITHOUT_CLI"

enum class BridgeView { UNPREPARED, PREPARED, INSTALLED, NOT_RUNNING, OUTDATED, UNKNOWN }

fun bridgeView(b: DSHBridge?): BridgeView = when (b?.state) {
    null -> BridgeView.UNKNOWN
    "DSH_BRIDGE_STATE_RUNNING" -> if (b.latestVersion.isNotEmpty() && b.version.isNotEmpty() && newer(b.latestVersion, b.version)) BridgeView.OUTDATED else BridgeView.INSTALLED
    "DSH_BRIDGE_STATE_INCOMPATIBLE" -> BridgeView.OUTDATED
    "DSH_BRIDGE_STATE_NOT_RUNNING" -> BridgeView.NOT_RUNNING
    "DSH_BRIDGE_STATE_NOT_INSTALLED" -> if (b.packagePath.isNotEmpty()) BridgeView.PREPARED else BridgeView.UNPREPARED
    else -> BridgeView.UNKNOWN
}

private fun newer(a: String, b: String): Boolean {
    val pa = a.split(".").map { it.toIntOrNull() ?: 0 }
    val pb = b.split(".").map { it.toIntOrNull() ?: 0 }
    for (i in 0 until maxOf(pa.size, pb.size)) {
        val x = pa.getOrElse(i) { 0 }
        val y = pb.getOrElse(i) { 0 }
        if (x != y) return x > y
    }
    return false
}

private fun checked(a: AgentStatus): Instant? = instantOrNull(a.checkedAt)

fun machineConnections(m: Machine, now: Instant): MachineConnections {
    val reachable = m.online && m.unreachable.isEmpty()
    val rows = mutableListOf<ConnRow>()
    for (a in m.status?.agents.orEmpty()) {
        if (a.hidden) continue
        when {
            a.name == "claude" -> rows += claudeRows(a)
            a.name == "deepseek" -> rows += dshRows(a)
            a.available || a.problem.isNotEmpty() && a.problem != NOT_FOUND || a.checking -> rows += agentRow(a)
        }
    }
    val out = rows.map { r ->
        if (r.state == ConnState.CHECKING) return@map r
        val old = r.checkedAt == null || Duration.between(r.checkedAt, now) > STALE_AFTER
        if (reachable && !old) return@map r
        val whenText = r.checkedAt?.let { agoText(it, now) } ?: t("never", "从未检测")
        r.copy(
            state = ConnState.UNKNOWN,
            text = t("Unknown", "状态未知"),
            was = r.text,
            lacks = if (!reachable) t("The machine can't be reached now: last known “${r.text}” ($whenText)", "机器暂时连不上：上次知道的是「${r.text}」（$whenText）")
            else t("Last checked $whenText, not confirmed since (it said “${r.text}”)", "上次检测是${whenText}，之后没再确认（当时是「${r.text}」）"),
        )
    }
    val order = listOf(ConnState.MISSING, ConnState.UNKNOWN, ConnState.PARTIAL, ConnState.CHECKING, ConnState.OK)
    val state = if (out.isEmpty()) (if (reachable) ConnState.OK else ConnState.UNKNOWN) else order.first { s -> out.any { it.state == s } }
    return MachineConnections(reachable, out, state)
}

private fun claudeRows(a: AgentStatus): List<ConnRow> {
    val at = checked(a)
    val cliTitle = t("Claude Code (command line)", "Claude Code 命令行")
    if (a.checking && at == null) return listOf(ConnRow("claude-cli", "claude", cliTitle, ConnState.CHECKING, t("Checking…", "正在检测…")))
    val rows = mutableListOf<ConnRow>()
    val d = a.desktop
    if (d?.installed == true || a.problem == APP_WITHOUT_CLI) {
        rows += ConnRow(
            "claude-desktop", "claude", "Claude Desktop",
            if (d?.signedIn == true) ConnState.OK else ConnState.MISSING,
            if (d?.signedIn == true) t("Signed in", "已登录") else t("Not signed in", "未登录"),
            if (d?.signedIn == true) null else t("Sign in in the Claude desktop app: its own sessions show in Aide only then.", "请在 Claude Desktop 里登录：登录后它自己的会话才会出现在 Aide 里。"),
            at,
        )
    }
    rows += when {
        a.available && a.loggedIn -> ConnRow("claude-cli", "claude", cliTitle, ConnState.OK, if (a.version.isNotEmpty()) t("Signed in · ${a.version}", "已登录 · ${a.version}") else t("Signed in", "已登录"), null, at)
        a.available && a.login?.state == "AGENT_LOGIN_STATE_WAITING" -> ConnRow(
            "claude-cli", "claude", cliTitle, ConnState.CHECKING, t("Signing in…", "正在登录…"),
            t("A browser opened on that computer: finish signing in there. This line updates by itself.", "那台电脑上已打开浏览器：请在那里完成登录。这一行会自己更新。"),
            at, login = true,
        )
        a.available && a.login?.state in setOf("AGENT_LOGIN_STATE_FAILED", "AGENT_LOGIN_STATE_TIMED_OUT", "AGENT_LOGIN_STATE_CANCELLED") -> ConnRow(
            "claude-cli", "claude", cliTitle, ConnState.MISSING, t("Not signed in", "未登录"),
            when (a.login?.state) {
                "AGENT_LOGIN_STATE_FAILED" -> t("The last sign-in didn't finish: ${a.login.detail.ifEmpty { "no reason given" }}. Try again.", "上次登录没完成：${a.login.detail.ifEmpty { "没有给出原因" }}。可以再试一次。")
                "AGENT_LOGIN_STATE_TIMED_OUT" -> t("The last sign-in waited 10 minutes for the browser and was stopped. Try again.", "上次登录等浏览器等了 10 分钟，已停止。可以再试一次。")
                else -> t("The last sign-in was cancelled.", "上次登录已取消。")
            },
            at, login = true,
        )
        a.available -> ConnRow(
            "claude-cli", "claude", cliTitle, ConnState.MISSING, t("Not signed in", "未登录"),
            if (d?.signedIn == true) t(
                "Claude Desktop is signed in, but tasks Aide hands out run with the Claude Code command line, which keeps its own sign-in: the app's does not carry over. Sign it in below (a browser opens on that computer); the app and its sessions are not touched.",
                "Claude Desktop 已登录，但 Aide 派出的任务是用 Claude Code 命令行跑的，它有自己的登录，Desktop 的登录不会续给它。点下面的按钮给它登录（会在那台电脑上打开浏览器）；Claude Desktop 和它的会话不受影响。",
            ) else t("Tasks Aide hands out run with the Claude Code command line, which is not signed in: sign it in below (a browser opens on that computer).", "Aide 派出的任务用 Claude Code 命令行跑，它还没登录：点下面的按钮给它登录（会在那台电脑上打开浏览器）。"),
            at, login = true,
        )
        a.problem == APP_WITHOUT_CLI -> ConnRow(
            "claude-cli", "claude", cliTitle, ConnState.MISSING, t("Not installed", "未安装"),
            t("Only the Claude desktop app is here, not the Claude Code command line Aide runs: open Code in the Claude app once and it fetches it.", "这台机器只有 Claude Desktop，没有 Aide 要用的 Claude Code 命令行：在 Claude Desktop 里打开一次 Code 页，它会自动下载。"),
            at,
        )
        else -> ConnRow(
            "claude-cli", "claude", cliTitle, ConnState.MISSING,
            if (a.problem == NOT_FOUND) t("Not installed", "未安装") else t("Can't run", "运行不了"),
            if (a.problem == NOT_FOUND) t("Tasks for Claude run with the Claude Code command line, which is not on this machine.", "派给 Claude 的任务要用 Claude Code 命令行跑，这台机器上没有。") else a.detail.ifEmpty { null },
            at,
        )
    }
    return rows
}

private fun dshRows(a: AgentStatus): List<ConnRow> {
    val at = checked(a)
    if (a.checking && at == null) return listOf(ConnRow("deepseek", "deepseek", "DSH", ConnState.CHECKING, t("Checking…", "正在检测…")))
    if (!a.available) {
        return listOf(ConnRow("deepseek", "deepseek", "DSH", ConnState.MISSING, if (a.problem == NOT_FOUND) t("Not installed", "未安装") else t("Can't run", "运行不了"), if (a.problem == NOT_FOUND) null else a.detail.ifEmpty { null }, at))
    }
    val b = a.dshBridge
    val v = bridgeView(b)
    val gap = t(
        "Works: DSH tasks Aide starts. Doesn't: seeing or taking over the sessions you open in DSH Desktop yourself, until the plugin is installed.",
        "能用：Aide 发起的 DSH 任务。不能用：看见、接管你在 DSH Desktop 里自己开的会话——装好插件后才行。",
    )
    val bridgeTitle = t("DSH bridge plugin", "DSH 桥接插件")
    val bridge = when (v) {
        BridgeView.INSTALLED -> ConnRow("deepseek-bridge", "deepseek", bridgeTitle, ConnState.OK, t("Installed · ${b?.version}", "已安装 · ${b?.version}"), null, at)
        BridgeView.NOT_RUNNING -> ConnRow("deepseek-bridge", "deepseek", bridgeTitle, ConnState.PARTIAL, t("Installed, DSH Desktop not open", "已安装，DSH Desktop 没开"), t("Open DSH Desktop (with the plugin turned on) to see and take over its own sessions.", "打开 DSH Desktop（插件保持开启），才能看见、接管它自己开的会话。"), at)
        BridgeView.UNPREPARED -> ConnRow("deepseek-bridge", "deepseek", bridgeTitle, ConnState.PARTIAL, t("Not prepared", "未备包"), "$gap " + t("Prepare it in Aide on the computer: Settings › Connect › DSH.", "在电脑上的 Aide 里备包：设置 › 连接 › DSH。"), at)
        BridgeView.PREPARED -> ConnRow("deepseek-bridge", "deepseek", bridgeTitle, ConnState.PARTIAL, t("Prepared, not installed", "已备包，未安装"), "$gap " + t("Install ${b?.packagePath} in DSH Desktop › Plugins.", "到 DSH Desktop › 插件 里安装 ${b?.packagePath}。"), at)
        BridgeView.OUTDATED -> ConnRow("deepseek-bridge", "deepseek", bridgeTitle, ConnState.PARTIAL, t("Out of date (${b?.version}; latest ${b?.latestVersion})", "版本过旧（${b?.version}，最新 ${b?.latestVersion}）"), t("Prepare the new package and install it in DSH Desktop › Plugins: the old one can't be used to take over sessions.", "重新备包，到 DSH Desktop › 插件 里装新包：旧版接管不了会话。"), at)
        BridgeView.UNKNOWN -> ConnRow("deepseek-bridge", "deepseek", bridgeTitle, ConnState.UNKNOWN, t("Unknown", "状态未知"), null, at)
    }
    val dsh = ConnRow(
        "deepseek", "deepseek", "DSH", if (bridge.state == ConnState.OK) ConnState.OK else ConnState.PARTIAL,
        if (a.version.isNotEmpty()) t("Detected · ${a.version}", "已检测到 · ${a.version}") else t("Detected", "已检测到"), null, at,
    )
    return listOf(dsh, bridge)
}

private fun agentRow(a: AgentStatus): ConnRow {
    val at = checked(a)
    val title = agentTitle(a.name)
    return when {
        a.checking && at == null -> ConnRow(a.name, a.name, title, ConnState.CHECKING, t("Checking…", "正在检测…"))
        !a.available -> ConnRow(a.name, a.name, title, ConnState.MISSING, if (a.problem == NOT_FOUND) t("Not installed", "未安装") else t("Can't run", "运行不了"), a.detail.ifEmpty { null }, at)
        !a.loggedIn -> ConnRow(a.name, a.name, title, ConnState.MISSING, t("Not signed in", "未登录"), t("$title is installed but not signed in: no task can go to it here.", "$title 装了但没登录：这台机器上暂时不能把任务派给它。"), at)
        else -> ConnRow(a.name, a.name, title, ConnState.OK, if (a.version.isNotEmpty()) t("Ready · ${a.version}", "可以用 · ${a.version}") else t("Ready", "可以用"), null, at)
    }
}

/** "just now", "12 min ago", "3 hr ago", or the date. */
fun agoText(d: Instant, now: Instant): String {
    val s = Duration.between(d, now).seconds
    return when {
        s < 60 -> t("just now", "刚刚")
        s < 3600 -> t("${s / 60} min ago", "${s / 60} 分钟前")
        s < 86400 -> t("${s / 3600} hr ago", "${s / 3600} 小时前")
        else -> t("${s / 86400} days ago", "${s / 86400} 天前")
    }
}

/**
 * One coding agent on a machine, as its page lists it (工单 #53): one entry, one name, one state. The connection
 * rows of one agent (Claude Desktop and the command line; DSH and its bridge plugin) are its parts, under it, never
 * a second list beside it; its state is its main row's — the command line for Claude (what Aide runs), DSH itself
 * for DSH — so the page never says "unknown" above and "signed in" below of one thing.
 */
data class AgentEntry(
    val agent: String,
    val title: String,
    val state: ConnState,
    val text: String,
    /** What the state means, or why it is not known now. */
    val lacks: String?,
    val checkedAt: Instant?,
    /** What it works with, on a line of its own: DSH's models (DeepSeek), never as a second agent. */
    val account: String?,
    /** Its other rows, said quietly under it (Claude Desktop, the command line, DSH's bridge plugin). */
    val parts: List<ConnRow>,
    /** Its main row offers signing in from Aide (工单 #51). */
    val login: Boolean,
    /** What the machine said of it, for its pause and the Manage menu; null: nothing to manage. */
    val status: AgentStatus?,
)

/** A machine's agents, one entry each, in the order its connections list them. */
fun machineAgents(m: Machine, now: Instant): List<AgentEntry> {
    val c = machineConnections(m, now)
    val statuses = m.status?.agents.orEmpty().associateBy { it.name }
    return c.rows.groupBy { it.agent }.map { (agent, rows) ->
        val main = rows.firstOrNull { it.id == agent || it.id == "$agent-cli" } ?: rows.first()
        val s = statuses[agent]
        val unknown = main.state == ConnState.UNKNOWN
        AgentEntry(
            agent = agent,
            title = agentTitle(agent),
            state = main.state,
            text = main.text,
            // Not known now: said in the words every page uses (工单 #56), with when it was checked and what it said.
            lacks = main.lacks,
            checkedAt = main.checkedAt,
            // DSH's models (DeepSeek's), as the desktop says them: never "signed in", which the machine can't know
            // for DSH before a session (ACP reports it as signed in whatever the truth).
            account = if (agent == "deepseek" && s?.available == true && s.models.isNotEmpty()) {
                val models = t("Models: ", "模型：") + s.models.take(3).joinToString(" · ") { it.name.ifEmpty { it.id } }
                if (unknown) t("Last known: $models", "上次检测：$models") else models
            } else null,
            // Claude's two rows are its skeleton (工单 #53): the command line is the state above, said again here by name.
            parts = rows.filter { it !== main || agent == "claude" && rows.size > 1 },
            login = main.login,
            status = s?.takeIf { it.available },
        )
    }
}

/**
 * Whether a machine's findings are too old to show as now (or were never made): the machines page asks it to check
 * again by itself while it is open, as the desktop does on showing its own (工单 #53: every row "unknown" only
 * because nothing had asked lately).
 */
fun needsCheck(m: Machine, now: Instant): Boolean {
    if (!m.online || m.unreachable.isNotEmpty()) return false
    val agents = m.status?.agents.orEmpty().filter { !it.hidden }
    if (agents.any { it.checking }) return false
    return agents.any { a -> val at = checked(a); at == null || Duration.between(at, now) > STALE_AFTER }
}

/** A part's short name under its entry. */
fun partTitle(r: ConnRow): String = when (r.id) {
    "claude-cli" -> t("Command line", "命令行")
    "claude-desktop" -> "Claude Desktop"
    "deepseek-bridge" -> t("Bridge plugin", "桥接插件")
    else -> r.title
}
