package io.aide.android.model

import io.aide.android.api.Persona
import io.aide.android.api.Session
import io.aide.android.api.SessionStatus
import io.aide.android.ui.t
import java.time.Instant

// A bot's face (工单 #83), as the desktop's @aide/core avatar.ts: the Grok Bot character ported from BotArk
// (packages/ui/src/grok), drawn on Android by the same engine in one hidden page (ui/GrokFace.kt), in a shape and a
// colour, moved by the bot's real state. The tables are the engine's (GrokBotMark.tsx BOT_SHAPES, BOT_COLORS) and so is
// the seed's default (grokMotionEngine.js defaultShapeForSeed, defaultColorForSeed), here in Kotlin so a screen knows a
// face's look at once.

/** The 26 shapes, in the editor's order. */
val botShapes = listOf(
    "blob", "pebble", "bean", "egg", "squircle", "tablet", "capsule", "cylinder", "hex", "gem", "crystal", "wedge", "shield",
    "dome", "arch", "cloud", "teardrop", "leaf", "square", "sparkle", "clover", "flower", "heart", "house", "star", "pumpkin",
)

/** A palette colour: its ink on light and on dark, and its swatch (what a contact keeps, "#rrggbb"). */
data class BotColor(val id: String, val light: Long, val dark: Long, val swatch: String)

/** The 11 colours (Grok Bot's Sand palette). */
val botColors = listOf(
    BotColor("black", 0xFF000000, 0xFFFFFFFF, "#000000"),
    BotColor("brown", 0xFFA06B39, 0xFFA16B39, "#936439"),
    BotColor("red", 0xFFFB2B3E, 0xFFFC2C3E, "#FF263C"),
    BotColor("orange", 0xFFFD690F, 0xFFF9670E, "#FF6700"),
    BotColor("yellow", 0xFFF59312, 0xFFF69412, "#FF9800"),
    BotColor("green", 0xFF14C16F, 0xFF14C370, "#00C972"),
    BotColor("cyan", 0xFF13B7A2, 0xFF13B8A3, "#00BCA6"),
    BotColor("blue", 0xFF1E86FC, 0xFF1884FC, "#1084FE"),
    BotColor("violet", 0xFF8F59F9, 0xFF905AFB, "#9159FE"),
    BotColor("magenta", 0xFFF93498, 0xFFFB3499, "#FF309B"),
    BotColor("gray", 0xFF959595, 0xFF777777, "#777777"),
)

/** The shapes the seed picks from, and the colours (all but black): the engine's GROK_DEFAULT_SHAPES, non-black colours. */
private val defaultShapes = listOf("blob", "pebble", "squircle", "tablet", "wedge", "hex", "cloud", "teardrop")
private val defaultColors = botColors.map { it.id }.filter { it != "black" }

private fun fnv(s: String): Int {
    var t = 2166136261L.toInt()
    for (c in s) t = (t xor c.code) * 16777619
    return t
}

/** The engine's defaultShapeForSeed. */
fun defaultShapeForSeed(seed: String): String {
    var t = fnv(seed)
    t = (t xor (t ushr 16)) * 73244475
    t = (t xor (t ushr 13)) * 3266489909L.toInt()
    val h = (t xor (t ushr 16)).toLong() and 0xFFFFFFFFL
    return defaultShapes[(h % defaultShapes.size).toInt()]
}

/** The engine's defaultColorForSeed. */
fun defaultColorForSeed(seed: String): String {
    val k = 2654435769L.toInt()
    val x = fnv(seed) xor k // xn: the hash, salted with version 1
    var t = x xor k // Cn: seeded with version 1 again
    t += 1831565813
    var e = (t xor (t ushr 15)) * (1 or t)
    e = (e + (e xor (e ushr 7)) * (61 or e)) xor e
    val r = ((e xor (e ushr 14)).toLong() and 0xFFFFFFFFL) / 4294967296.0
    return defaultColors[(r * defaultColors.size).toInt()]
}

data class BotLook(val shape: String, val color: String)

fun resolveShape(seed: String, shape: String?): String = if (shape != null && shape in botShapes) shape else defaultShapeForSeed(seed)
fun resolveColor(seed: String, color: String?): String = if (color != null && botColors.any { it.id == color }) color else defaultColorForSeed(seed)

/** The palette colour a stored "#rrggbb" is: its own swatch, else the nearest (a contact's colour from before #83). */
fun colorOfHex(hex: String): String? {
    if (!Regex("^#[0-9a-fA-F]{6}$").matches(hex)) return null
    fun rgb(h: String) = listOf(1, 3, 5).map { h.substring(it, it + 2).toInt(16) }
    val (r, g, b) = rgb(hex)
    return botColors.minBy { c -> rgb(c.swatch).let { (cr, cg, cb) -> (r - cr) * (r - cr) + (g - cg) * (g - cg) + (b - cb) * (b - cb) } }.id
}

fun colorHex(id: String): String = botColors.first { it.id == id }.swatch

/** The seed a bot's default look comes from: its grant's when it has one, else its key (as the desktop). */
fun botSeed(b: Bot): String = b.client?.let { "c:${it.clientId}" } ?: b.key

/** A contact's look: its icon its shape, its colour its swatch; one from before #83 keeps its colour (nearest), takes the seed's shape. */
fun personaLook(p: Persona, seed: String): BotLook = BotLook(resolveShape(seed, p.icon), resolveColor(seed, colorOfHex(p.color)))

/** The look a grant's icon picked ("bot:<shape>:<colour>"), when it is one. */
fun grantLook(icon: String, seed: String): BotLook? =
    Regex("^bot:([a-z]+):([a-z]+)$").find(icon)?.let { BotLook(resolveShape(seed, it.groupValues[1]), resolveColor(seed, it.groupValues[2])) }

/** A bot's look: the owner's own (its first contact's, else its grant's) before the seed's (人工值优先). */
fun botLook(b: Bot): BotLook {
    val seed = botSeed(b)
    b.personas.firstOrNull()?.let { return personaLook(it, seed) }
    return b.client?.let { grantLook(it.icon, seed) } ?: BotLook(resolveShape(seed, null), resolveColor(seed, null))
}

/** A look as a contact keeps it. */
fun Persona.withLook(l: BotLook): Persona = copy(icon = l.shape, color = colorHex(l.color))

/** A bot's own sessions, as the roster counts them. */
fun botSessions(b: Bot, sessions: Collection<Session>): List<Session> =
    if (b.key.startsWith("m:")) sessions.filter { it.machineId == b.machineId && it.agent == b.agent }
    else b.client?.let { c -> sessions.filter { it.createdBy == c.clientId } }.orEmpty()

/** How long a finished or failed turn shows on the face (the engine's own clip then ends it). */
const val REACT_MS = 8_000L

/** What the face shows: the engine's state, and offline to rest it, faded. */
data class BotMood(val state: String, val offline: Boolean = false)

/** The face's state from the bot's real state (工单 #83 §2): the same table as the desktop's botMood. */
fun botMood(b: Bot, sessions: Collection<Session>, now: Instant): BotMood {
    if (b.state.why == NotReady.OFFLINE || b.state.state == ConnState.UNKNOWN) return BotMood("idle", offline = true)
    val live = botSessions(b, sessions).filter { it.status != SessionStatus.ARCHIVED }
    if (live.any { it.status == SessionStatus.WAITING_INPUT }) return BotMood("blocked")
    val newest = compareByDescending<Session> { lastEventAt(it)?.toEpochMilli() ?: 0 }
    live.filter { it.status == SessionStatus.RUNNING }.sortedWith(newest).firstOrNull()?.let { return BotMood(runningState(it)) }
    if (live.any { it.status == SessionStatus.QUEUED || it.status == SessionStatus.PREPARING }) return BotMood("waiting")
    if (b.state.why == NotReady.PAUSED) return BotMood("sleeping")
    if (b.state.why == NotReady.SIGNED_OUT || b.state.why == NotReady.MISSING) return BotMood("blocked")
    val last = live.sortedWith(newest).firstOrNull()
    val at = last?.let(::lastEventAt)
    if (last != null && at != null && now.toEpochMilli() - at.toEpochMilli() < REACT_MS) {
        when (stateOf(last)) {
            SessionState.DONE, SessionState.DONE_UNSEEN -> return BotMood("finished")
            SessionState.FAILED -> return BotMood("failed")
            else -> {}
        }
    }
    return BotMood("idle")
}

/** A running session's state by what it does now (its progress phase and current tool "<name>: <summary>"). */
fun runningState(s: Session): String {
    val p = s.progress
    val tool = p?.currentTool.orEmpty().substringBefore(":").trim().lowercase()
    return when {
        tool.isEmpty() -> if (p?.lastMessageExcerpt.isNullOrEmpty()) "thinking" else "streaming"
        p?.phase == "editing" -> "coding"
        p?.phase == "running_shell" -> "running"
        Regex("search|grep|glob|^ls$|find|fetch|web|browse").containsMatchIn(tool) -> "searching"
        Regex("read|view|open|cat").containsMatchIn(tool) -> "reading"
        else -> "working"
    }
}

/** A shape's name, for the editor. */
fun shapeName(s: String): String = when (s) {
    "blob" -> t("Blob", "圆团"); "pebble" -> t("Pebble", "卵石"); "bean" -> t("Bean", "豆子"); "egg" -> t("Egg", "蛋")
    "squircle" -> t("Squircle", "圆角方"); "tablet" -> t("Tablet", "药片"); "capsule" -> t("Capsule", "胶囊"); "cylinder" -> t("Cylinder", "圆柱")
    "hex" -> t("Hexagon", "六边形"); "gem" -> t("Gem", "宝石"); "crystal" -> t("Crystal", "水晶"); "wedge" -> t("Wedge", "楔形")
    "shield" -> t("Shield", "盾牌"); "dome" -> t("Dome", "圆顶"); "arch" -> t("Arch", "拱门"); "cloud" -> t("Cloud", "云朵")
    "teardrop" -> t("Teardrop", "水滴"); "leaf" -> t("Leaf", "叶子"); "square" -> t("Square", "方块"); "sparkle" -> t("Sparkle", "星芒")
    "clover" -> t("Clover", "四叶草"); "flower" -> t("Flower", "花朵"); "heart" -> t("Heart", "爱心"); "house" -> t("House", "小房子")
    "star" -> t("Star", "星星"); else -> t("Pumpkin", "南瓜")
}

/** A colour's name, for the editor. */
fun colorName(c: String): String = when (c) {
    "black" -> t("Black", "黑"); "brown" -> t("Brown", "棕"); "red" -> t("Red", "红"); "orange" -> t("Orange", "橙")
    "yellow" -> t("Yellow", "黄"); "green" -> t("Green", "绿"); "cyan" -> t("Cyan", "青"); "blue" -> t("Blue", "蓝")
    "violet" -> t("Violet", "紫"); "magenta" -> t("Magenta", "品红"); else -> t("Gray", "灰")
}
