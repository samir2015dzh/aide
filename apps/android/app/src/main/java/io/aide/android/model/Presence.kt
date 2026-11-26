package io.aide.android.model

// What a state dot says (工单 #135 P25 / 审计 B), as the desktop's @aide/core Presence: one colour per meaning, and the
// same label the same colour on every page (the roster, a bot's page, a room's members, the devices tab), read from
// this enum — never from the label's words.

/**
 * - ONLINE: a personal agent connected now ("在线" / "最近活跃"), a machine online;
 * - SIGNED_IN: a coding agent signed in or ready ("已登录" / "可以用");
 * - UNCONFIRMED: there, but its sign-in not confirmed ("未确认" — DSH, Claude Code and ACP agents alike — or DSH's
 *   "已检测到");
 * - SIGNED_OUT: does not work until fixed ("未登录" / "未安装" / "运行不了");
 * - CHECKING: being checked, signing in;
 * - UNKNOWN: not known now (stale, its machine away, a personal agent away, paused).
 */
enum class Presence { ONLINE, SIGNED_IN, UNCONFIRMED, SIGNED_OUT, CHECKING, UNKNOWN }

/**
 * A row's dot: its own presence where it has one, else what its state says. Claude Code's and an ACP agent's "未确认"
 * rows are UNKNOWN by state (tasks may go to them, unverified) but UNCONFIRMED by presence, like DSH's.
 */
fun rowPresence(r: ConnRow): Presence = r.presence ?: when (r.state) {
    ConnState.OK -> Presence.SIGNED_IN
    ConnState.PARTIAL -> Presence.UNCONFIRMED
    ConnState.MISSING -> Presence.SIGNED_OUT
    ConnState.CHECKING -> Presence.CHECKING
    ConnState.UNKNOWN -> Presence.UNKNOWN
}

/** The hue a presence's dot is drawn in: one each, the only place the two are tied (ui's presenceColor reads it). */
enum class DotHue { GREEN, BLUE, ORANGE, RED, GREY }

/** Each presence's dot: its hue, and whether it is a hollow ring (checking) rather than a filled dot. */
data class Dot(val hue: DotHue, val ring: Boolean = false)

/** The one mapping from a presence to its dot, as the desktop's presenceDot (on / ready / warn / bad / checking / grey). */
fun presenceDot(p: Presence): Dot = when (p) {
    Presence.ONLINE -> Dot(DotHue.GREEN)
    Presence.SIGNED_IN -> Dot(DotHue.BLUE)
    Presence.UNCONFIRMED -> Dot(DotHue.ORANGE)
    Presence.SIGNED_OUT -> Dot(DotHue.RED)
    Presence.CHECKING -> Dot(DotHue.BLUE, ring = true)
    Presence.UNKNOWN -> Dot(DotHue.GREY)
}
