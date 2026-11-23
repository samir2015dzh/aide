package io.aide.android

import io.aide.android.api.Client
import io.aide.android.api.Failure
import io.aide.android.api.Persona
import io.aide.android.api.Session
import io.aide.android.api.SessionProgress
import io.aide.android.api.SessionStatus
import io.aide.android.model.Bot
import io.aide.android.model.BotLook
import io.aide.android.model.BotMood
import io.aide.android.model.BotState
import io.aide.android.model.ConnState
import io.aide.android.model.NotReady
import io.aide.android.model.REACT_MS
import io.aide.android.model.Usable
import io.aide.android.model.botColors
import io.aide.android.model.botLook
import io.aide.android.model.botMood
import io.aide.android.model.botShapes
import io.aide.android.model.colorHex
import io.aide.android.model.colorOfHex
import io.aide.android.model.defaultColorForSeed
import io.aide.android.model.defaultShapeForSeed
import io.aide.android.model.grantLook
import io.aide.android.model.personaLook
import io.aide.android.model.withLook
import io.aide.android.ui.engineScript
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import java.time.Instant

/** The bots' faces (工单 #83): the same table as the desktop's (packages/core avatar.test.ts). */
class AvatarTest {
    private val now = Instant.parse("2026-11-23T10:00:00Z")
    private fun ago(ms: Long) = now.minusMillis(ms).toString()
    private val ok = BotState(ConnState.OK, "已登录", "已登录", Usable.YES)
    private fun claude(state: BotState = ok, personas: List<Persona> = emptyList(), client: Client? = null) =
        Bot("m:m1/claude", false, "Claude Code", "claude", "m1", client, state, "", 0, 0, personas)
    private fun s(status: String = SessionStatus.IDLE, agent: String = "claude", p: SessionProgress = SessionProgress(lastEventAt = ago(3_600_000))) =
        Session(sessionId = "s1", machineId = "m1", agent = agent, status = status, progress = p)
    private fun mood(b: Bot, vararg list: Session) = botMood(b, list.toList(), now).state

    @Test fun theEnginesSeedDefaultsAsTheDesktopHasThem() {
        // Worked out by the engine itself (grokMotionEngine.js defaultShapeForSeed / defaultColorForSeed) in Node.
        val cases = mapOf(
            "m:m1/claude" to ("teardrop" to "orange"), "c:k1" to ("hex" to "brown"), "x" to ("cloud" to "green"),
            "m:m1/deepseek" to ("squircle" to "orange"), "c:grok" to ("squircle" to "cyan"), "" to ("hex" to "blue"),
            "DSH · 工作电脑" to ("wedge" to "blue"), "p_abc123" to ("pebble" to "blue"),
        )
        for ((seed, want) in cases) assertEquals(seed, want, defaultShapeForSeed(seed) to defaultColorForSeed(seed))
        assertEquals(26, botShapes.size)
        assertEquals(11, botColors.size)
    }

    @Test fun theOwnersLookWinsOverTheSeeds() {
        assertEquals(BotLook("teardrop", "orange"), botLook(claude()))
        val p = Persona(name = "审阅").withLook(BotLook("heart", "magenta"))
        assertEquals("heart" to "#FF309B", p.icon to p.color)
        assertEquals(BotLook("heart", "magenta"), botLook(claude(personas = listOf(p))))
        // A contact from before #83 (an emoji's name, one of six colours): its colour, nearest; the seed's shape.
        assertEquals(BotLook("teardrop", "green"), personaLook(Persona(icon = "owl", color = "#10b981"), "m:m1/claude"))
        assertEquals("blue", colorOfHex("#3b82f6"))
        assertNull(colorOfHex(""))
        // A grant's: "bot:<shape>:<colour>"; its app's logo or a picture is not a look.
        assertEquals(BotLook("house", "red"), botLook(claude(client = Client(clientId = "k1", icon = "bot:house:red"))))
        assertNull(grantLook("app:claude", "s"))
        assertEquals("#00BCA6", colorHex("cyan"))
    }

    @Test fun theFacesStateComesFromTheRealState() {
        assertEquals(BotMood("idle"), botMood(claude(), emptyList(), now))
        assertEquals(BotMood("idle", offline = true), botMood(claude(BotState(ConnState.UNKNOWN, "", "", Usable.NO, NotReady.OFFLINE)), emptyList(), now))
        assertEquals("sleeping", mood(claude(ok.copy(usable = Usable.NO, why = NotReady.PAUSED))))
        assertEquals("blocked", mood(claude(BotState(ConnState.MISSING, "", "", Usable.NO, NotReady.SIGNED_OUT))))
        val run = { p: SessionProgress -> mood(claude(), s(SessionStatus.RUNNING, p = p.copy(lastEventAt = ago(1000)))) }
        assertEquals("thinking", run(SessionProgress()))
        assertEquals("streaming", run(SessionProgress(lastMessageExcerpt = "Here is the fix")))
        assertEquals("coding", run(SessionProgress(phase = "editing", currentTool = "Edit: a.kt")))
        assertEquals("running", run(SessionProgress(phase = "running_shell", currentTool = "Bash: go test")))
        assertEquals("reading", run(SessionProgress(phase = "running", currentTool = "Read: a.kt")))
        assertEquals("searching", run(SessionProgress(phase = "running", currentTool = "Grep: TODO")))
        assertEquals("working", run(SessionProgress(phase = "running", currentTool = "Task: review")))
        assertEquals("waiting", mood(claude(), s(SessionStatus.QUEUED)))
        assertEquals("blocked", mood(claude(), s(SessionStatus.RUNNING), s(SessionStatus.WAITING_INPUT)))
        assertEquals("finished", mood(claude(), s(p = SessionProgress(turnCount = 1, lastEventAt = ago(2000)))))
        assertEquals("idle", mood(claude(), s(p = SessionProgress(turnCount = 1, lastEventAt = ago(REACT_MS + 1000)))))
        assertEquals("failed", mood(claude(), s(SessionStatus.CRASHED, p = SessionProgress(lastEventAt = ago(2000)))))
        assertEquals("idle", mood(claude(), s(p = SessionProgress(lastEventAt = ago(2000), lastFailure = Failure("FAILURE_KIND_INTERRUPTED")))))
        // Only its own sessions move it.
        assertEquals("idle", mood(claude(), s(SessionStatus.RUNNING, agent = "codex")))
        assertEquals("idle", mood(claude(), s(SessionStatus.ARCHIVED)))
    }

    @Test fun theEnginePageGetsTheSameEngineFileAsTheDesktop() {
        val module = File("../../../packages/ui/src/grok/grokMotionEngine.js").readText()
        val script = engineScript(module)
        assertTrue(script.endsWith("var G={GROK_COLORS:nn,GROK_NON_BLACK_COLORS:an,GROK_STATES:on,GROK_SHAPES:sn,GROK_ALL_SHAPES:ln,GROK_DEFAULT_SHAPES:dn,isGrokShape:mn,defaultColorForSeed:Tn,defaultShapeForSeed:Dn,getColorGradient:ur,GROK_INK_COLORS:dr,getColorLightDark:fr,getColorForTheme:pr,computeGazeFromRect:fa,mapShapeToBody:i_,createAppearance:o_,createMotionEngine:p_,renderFrameToSvgString:Yg,renderFrameToTree:Xg,createRig:eo,computeCanvasScale:no,createStateController:go};"))
        assertTrue(!script.contains("export {"))
    }
}
