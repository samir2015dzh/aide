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
    /** Installing it can be handed to DSH from here (the DSH bridge plugin, 工单 #59). */
    val install: Boolean = false,
    /** The DSH session installing it, or that last tried. */
    val installSession: String = "",
    /** Its dot when that is not what its state alone says (rowPresence): set where the label is about the sign-in. */
    val presence: Presence? = null,
)

data class MachineConnections(val reachable: Boolean, val rows: List<ConnRow>, val state: ConnState)

private const val NOT_FOUND = "AGENT_PROBLEM_NOT_FOUND"
private const val APP_WITHOUT_CLI = "AGENT_PROBLEM_APP_WITHOUT_CLI"
private const val TIMED_OUT = "AGENT_PROBLEM_TIMED_OUT"

/** A found agent's state with its version, or that its check could not read one (工单 #176: --version is no verdict). */
private fun versioned(en: String, zh: String, version: String) =
    if (version.isNotEmpty()) t("$en · $version", "$zh · $version") else t("$en · version unknown", "$zh · 版本未知")

/** Why an agent is not available, in a word or two: a check with no answer is not a program that fails (工单 #175). */
private fun unavailableLabel(a: AgentStatus) = when (a.problem) {
    NOT_FOUND -> t("Not installed", "未安装")
    TIMED_OUT -> t("No answer to the check", "检查无回应")
    else -> t("Can't run", "运行不了")
}

/** What a check with no answer means, else what the machine said. */
private fun unavailableDetail(a: AgentStatus): String? =
    if (a.problem == TIMED_OUT) t(
        "Found, but it didn't answer Aide's check within 20 seconds: the check got no answer, which doesn't mean it's broken. Check again.",
        "找到了，但 Aide 检查时它 20 秒内没有回应：这是检查没得到回答，不一定是它坏了。可以再检查一次。",
    ) else a.detail.ifEmpty { null }

enum class BridgeView { UNPREPARED, PREPARED, INSTALLED, NOT_RUNNING, OUTDATED, UNKNOWN, INSTALLING, INSTALL_FAILED, INSTALL_RESTART }

/** As the desktop's bridgeView: the install handed to DSH (工单 #59) while the plugin does not answer yet. */
fun bridgeView(b: DSHBridge?): BridgeView = if (b != null && b.state != "DSH_BRIDGE_STATE_RUNNING" && b.install?.state in setOf("DSH_BRIDGE_INSTALL_STATE_RUNNING", "DSH_BRIDGE_INSTALL_STATE_FAILED", "DSH_BRIDGE_INSTALL_STATE_RESTART")) {
    when (b.install?.state) {
        "DSH_BRIDGE_INSTALL_STATE_RUNNING" -> BridgeView.INSTALLING
        "DSH_BRIDGE_INSTALL_STATE_FAILED" -> BridgeView.INSTALL_FAILED
        else -> BridgeView.INSTALL_RESTART
    }
} else when (b?.state) {
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
            presence = null,
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
        a.available && a.loggedIn -> ConnRow("claude-cli", "claude", cliTitle, ConnState.OK, versioned("Signed in", "已登录", a.version), null, at)
        a.available && a.login?.state == "AGENT_LOGIN_STATE_WAITING" -> ConnRow(
            "claude-cli", "claude", cliTitle, ConnState.CHECKING, t("Signing in…", "正在登录…"),
            // 工单 #118: no browser is claimed; its address, once printed, is opened on any device.
            when {
                a.login.codeSent -> t("The code went to Claude Code; checking it…", "授权码已交给 Claude Code，正在核对…")
                a.login.url.isNotEmpty() -> t("Open the sign-in address below on any device, sign in, then enter the code the page shows. Good for 15 minutes.", "在任意设备上打开下面的登录地址，登录后把页面给出的授权码填回这里。15 分钟内有效。")
                else -> t("Starting Claude Code's sign-in, waiting for its sign-in address…", "正在启动 Claude Code 的登录，等它给出登录地址…")
            },
            at, login = true,
        )
        // Its own `auth status` said nothing clear (工单 #108): unconfirmed, never the desktop app's sign-in in its place.
        a.available && a.loginUnconfirmed -> ConnRow(
            "claude-cli", "claude", cliTitle, ConnState.UNKNOWN, t("Unconfirmed", "未确认"),
            t(
                "The Claude Code command line did not say whether it is signed in, and Aide does not take the Claude desktop app's sign-in for it: whether tasks can run with it is not confirmed. Sign it in below (it gives a sign-in address to open on any device), or check again.",
                "Claude Code 命令行没说清自己登没登录，Aide 不拿 Claude Desktop 的登录代替它：派出的任务能不能跑还没确认。点下面的按钮给它登录（会给出登录地址，在任意设备上打开），或者重新检测。",
            ),
            at, login = true, presence = Presence.UNCONFIRMED,
        )
        a.available && a.login?.state in setOf("AGENT_LOGIN_STATE_FAILED", "AGENT_LOGIN_STATE_TIMED_OUT", "AGENT_LOGIN_STATE_CANCELLED") -> ConnRow(
            "claude-cli", "claude", cliTitle, ConnState.MISSING, t("Not signed in", "未登录"),
            when (a.login?.state) {
                "AGENT_LOGIN_STATE_FAILED" -> t("The last sign-in didn't finish: ${a.login.detail.ifEmpty { "no reason given" }}. Try again.", "上次登录没完成：${a.login.detail.ifEmpty { "没有给出原因" }}。可以再试一次。")
                "AGENT_LOGIN_STATE_TIMED_OUT" -> t("The last sign-in was not finished within 15 minutes and was stopped. Try again.", "上次登录 15 分钟内没有完成，已停止。可以再试一次。")
                else -> t("The last sign-in was cancelled.", "上次登录已取消。")
            },
            at, login = true,
        )
        a.available -> ConnRow(
            "claude-cli", "claude", cliTitle, ConnState.MISSING, t("Not signed in", "未登录"),
            if (d?.signedIn == true) t(
                "Claude Desktop is signed in, but tasks Aide hands out run with the Claude Code command line, which keeps its own sign-in: the app's does not carry over. Sign it in below (it gives a sign-in address to open on any device); the app and its sessions are not touched.",
                "Claude Desktop 已登录，但 Aide 派出的任务是用 Claude Code 命令行跑的，它有自己的登录，Desktop 的登录不会续给它。点下面的按钮给它登录（会给出登录地址，在任意设备上打开）；Claude Desktop 和它的会话不受影响。",
            ) else t("Tasks Aide hands out run with the Claude Code command line, which is not signed in: sign it in below (it gives a sign-in address to open on any device).", "Aide 派出的任务用 Claude Code 命令行跑，它还没登录：点下面的按钮给它登录（会给出登录地址，在任意设备上打开）。"),
            at, login = true,
        )
        a.problem == APP_WITHOUT_CLI -> ConnRow(
            "claude-cli", "claude", cliTitle, ConnState.MISSING, t("Not installed", "未安装"),
            t("Only the Claude desktop app is here, not the Claude Code command line Aide runs: open Code in the Claude app once and it fetches it.", "这台机器只有 Claude Desktop，没有 Aide 要用的 Claude Code 命令行：在 Claude Desktop 里打开一次 Code 页，它会自动下载。"),
            at,
        )
        else -> ConnRow(
            "claude-cli", "claude", cliTitle, ConnState.MISSING,
            unavailableLabel(a),
            if (a.problem == NOT_FOUND) t("Tasks for Claude run with the Claude Code command line, which is not on this machine.", "派给 Claude 的任务要用 Claude Code 命令行跑，这台机器上没有。") else unavailableDetail(a),
            at,
        )
    }
    return rows
}

private fun dshRows(a: AgentStatus): List<ConnRow> {
    val at = checked(a)
    if (a.checking && at == null) return listOf(ConnRow("deepseek", "deepseek", "DSH", ConnState.CHECKING, t("Checking…", "正在检测…")))
    if (!a.available) {
        return listOf(ConnRow("deepseek", "deepseek", "DSH", ConnState.MISSING, unavailableLabel(a), if (a.problem == NOT_FOUND) null else unavailableDetail(a), at))
    }
    val b = a.dshBridge
    val v = bridgeView(b)
    val gap = t(
        "Works: DSH tasks Aide starts. Doesn't: seeing or taking over the sessions you open in DSH Desktop yourself, until the plugin is installed. Press “Have DSH install it”: DSH installs it, restarts, and tells Aide when it is done.",
        "能用：Aide 发起的 DSH 任务。不能用：看见、接管你在 DSH Desktop 里自己开的会话——装好插件后才行。点「让 DSH 装好它」，DSH 会自己装、自己重启、装完告诉 Aide。",
    )
    val bridgeTitle = t("Aide plugin for DSH", "DSH 的 Aide 插件")
    val session = b?.install?.sessionId.orEmpty()
    val bridge = when (v) {
        BridgeView.INSTALLED -> ConnRow("deepseek-bridge", "deepseek", bridgeTitle, ConnState.OK, t("Installed · ${b?.version}", "已安装 · ${b?.version}"), null, at)
        BridgeView.NOT_RUNNING -> ConnRow("deepseek-bridge", "deepseek", bridgeTitle, ConnState.PARTIAL, t("Installed, DSH Desktop not open", "已安装，DSH Desktop 没开"), t("Open DSH Desktop (with the plugin turned on) to see and take over its own sessions.", "打开 DSH Desktop（插件保持开启），才能看见、接管它自己开的会话。"), at)
        BridgeView.UNPREPARED, BridgeView.PREPARED -> ConnRow("deepseek-bridge", "deepseek", bridgeTitle, ConnState.PARTIAL, t("Not installed", "未安装"), gap, at, install = true)
        BridgeView.OUTDATED -> ConnRow("deepseek-bridge", "deepseek", bridgeTitle, ConnState.PARTIAL, t("Out of date (${b?.version}; latest ${b?.latestVersion})", "版本过旧（${b?.version}，最新 ${b?.latestVersion}）"), t("The old one can't take over sessions: press “Have DSH install it” and DSH puts in the new one.", "旧版接管不了会话：点「让 DSH 装好它」，DSH 会自己换上新版。"), at, install = true)
        BridgeView.INSTALLING -> ConnRow("deepseek-bridge", "deepseek", bridgeTitle, ConnState.CHECKING, t("DSH is installing it…", "DSH 正在安装…"), t("DSH installs the plugin on that computer and restarts DSH Desktop; it tells Aide when it is done, and this line updates by itself.", "DSH 在那台电脑上装插件、重启 DSH Desktop，装完会告诉 Aide，这一行会自己更新。"), at, installSession = session)
        BridgeView.INSTALL_FAILED -> ConnRow("deepseek-bridge", "deepseek", bridgeTitle, ConnState.PARTIAL, t("DSH couldn't install it", "DSH 没装成功"), b?.install?.reason?.ifEmpty { null } ?: t("DSH didn't install it and didn't say why: open its install task to see how far it got.", "DSH 没装上，也没说原因：打开它的安装任务看做到了哪一步。"), at, install = true, installSession = session)
        BridgeView.INSTALL_RESTART -> ConnRow("deepseek-bridge", "deepseek", bridgeTitle, ConnState.PARTIAL, t("Installed, waiting for DSH Desktop to restart", "已装好，等 DSH Desktop 重启"), listOfNotNull(t("Takes effect once DSH Desktop restarts (this line turns “Installed” as soon as the plugin answers).", "重启 DSH Desktop 后生效（插件一开始应答，这一行会自己变成「已安装」）。"), b?.install?.reason?.ifEmpty { null }).joinToString(" "), at, installSession = session)
        BridgeView.UNKNOWN -> ConnRow("deepseek-bridge", "deepseek", bridgeTitle, ConnState.UNKNOWN, t("Unknown", "状态未知"), null, at)
    }
    val dshUnconfirmed = !a.loggedIn && a.loginUnconfirmed
    val dshText = when {
        a.loggedIn -> versioned("Signed in", "已登录", a.version)
        dshUnconfirmed -> versioned("Unconfirmed", "未确认", a.version)
        else -> versioned("Detected", "已检测到", a.version)
    }
    val dsh = ConnRow(
        "deepseek", "deepseek", "DSH",
        if (!dshUnconfirmed && bridge.state == ConnState.OK) ConnState.OK else ConnState.PARTIAL,
        dshText,
        if (dshUnconfirmed) t("DSH was detected, but its sign-in state is unconfirmed. Send a task or run a test task to verify automatically.", "DSH 已检测到，但登录状态未确认。可照常派活或发一轮测试任务自动验证。") else null,
        at,
        // Its dot follows its own label, whatever the plugin's state makes the row's: "已登录" is signed in here as
        // everywhere, "未确认" / "已检测到" unconfirmed (审计 B / P25).
        presence = if (a.loggedIn) Presence.SIGNED_IN else Presence.UNCONFIRMED,
    )
    return listOf(dsh, bridge)
}

private fun agentRow(a: AgentStatus): ConnRow {
    val at = checked(a)
    val title = agentTitle(a.name)
    return when {
        a.checking && at == null -> ConnRow(a.name, a.name, title, ConnState.CHECKING, t("Checking…", "正在检测…"))
        !a.available -> ConnRow(a.name, a.name, title, ConnState.MISSING, unavailableLabel(a), unavailableDetail(a), at)
        // Its check cannot tell (工单 #112: an ACP agent's sign-in shows only in a session): unconfirmed, never a guess.
        !a.loggedIn && a.loginUnconfirmed -> ConnRow(
            a.name, a.name, title, ConnState.UNKNOWN, t("Unconfirmed", "未确认"),
            t(
                "Whether $title is signed in shows only when it runs a task: not confirmed yet. Tasks can go to it; once one runs, this says signed in, and if it is not signed in the task says so.",
                "$title 的登录状态只有跑任务时才知道：还没确认。可以照常派活，跑通一轮后这里会显示已登录；如果它没登录，任务会如实报出来。",
            ),
            at, presence = Presence.UNCONFIRMED,
        )
        !a.loggedIn -> ConnRow(a.name, a.name, title, ConnState.MISSING, t("Not signed in", "未登录"), t("$title is installed but not signed in: no task can go to it here.", "$title 装了但没登录：这台机器上暂时不能把任务派给它。"), at)
        else -> ConnRow(a.name, a.name, title, ConnState.OK, versioned("Ready", "可以用", a.version), null, at)
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
    "deepseek-bridge" -> t("Aide plugin", "Aide 插件")
    else -> r.title
}
