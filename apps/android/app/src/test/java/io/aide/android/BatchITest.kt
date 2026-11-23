package io.aide.android

import io.aide.android.api.AgentMode
import io.aide.android.api.AgentStatus
import io.aide.android.api.Client
import io.aide.android.api.Machine
import io.aide.android.api.MachineStatus
import io.aide.android.api.Persona
import io.aide.android.api.Project
import io.aide.android.model.NotReady
import io.aide.android.model.aboveProject
import io.aide.android.model.modeChoice
import io.aide.android.model.modeLabel
import io.aide.android.model.personaSavable
import io.aide.android.model.roster
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.time.Instant
import java.util.Locale

/** 批 I: the roster by machine (工单 #75), saving a contact for any change (工单 #78), DSH's presets (工单 #89). */
class BatchITest {
    private val now = Instant.parse("2026-11-23T10:00:00Z")
    private val fresh = "2026-11-23T09:58:00Z"

    @Before
    fun chinese() = Locale.setDefault(Locale.SIMPLIFIED_CHINESE)

    @Test
    fun aGroupPerMachineHeadedByItsNameRowsNameTheAgentAlone() {
        val machines = listOf(
            Machine("m1", "work-pc", online = true, status = MachineStatus(agents = listOf(AgentStatus("deepseek", available = true, loggedIn = true, checkedAt = fresh)))),
            Machine("m2", "laptop", online = false, status = MachineStatus(agents = listOf(AgentStatus("claude", available = true, loggedIn = true, checkedAt = fresh)))),
        )
        val r = roster(machines, listOf(Client(clientId = "c2", label = "Codex on work-pc")), emptyList(), now)
        assertEquals(listOf("work-pc", "laptop"), r.machines.map { it.machine.machineName })
        assertEquals(listOf("DSH" to "DSH · work-pc", "Codex" to "Codex on work-pc"), r.machines[0].bots.map { it.shortName to it.name })
        // A machine away keeps its group: its bots say so.
        assertEquals(listOf("Claude Code"), r.machines[1].bots.map { it.shortName })
        assertEquals(NotReady.OFFLINE, r.machines[1].bots[0].state.why)
    }

    @Test
    fun aContactIsSavedForAnyChangeOnlyItsNameToo() {
        val saved = Persona(personaId = "p1", name = "DSH · work-pc", machineId = "m1", agent = "deepseek", projectId = "project")
        assertFalse(personaSavable(saved, saved))
        assertTrue(personaSavable(saved, saved.copy(name = "DSH 助手")))
        assertTrue(personaSavable(saved, saved.copy(icon = "fox")))
        assertFalse(personaSavable(saved, saved.copy(name = " ")))
        assertTrue(personaSavable(Persona(name = "DSH", machineId = "m1", agent = "deepseek", projectId = "project"), Persona(name = "DSH", machineId = "m1", agent = "deepseek", projectId = "project")))
    }

    @Test
    fun dshsPresetsByDshsNamesTheProjectsDefaultAndAChoiceAboveItSaid() {
        val modes = listOf(AgentMode("read-only"), AgentMode("workspace-write", "Workspace"), AgentMode("danger-full-access"), AgentMode("plan", "Plan"))
        assertEquals(listOf("仅可查看", "工作区内修改", "完全权限", "Plan"), modes.map { modeLabel("deepseek", it) })
        assertEquals("Workspace", modeLabel("codex", modes[1]))
        assertTrue(aboveProject("deepseek", "danger-full-access", "read-only"))
        assertFalse(aboveProject("deepseek", "read-only", "workspace-write"))
        assertFalse(aboveProject("claude", "bypassPermissions", "read-only"))
        val biz = Project("m1", "project-2", name = "商务", dshPreset = "read-only")
        val dflt = modeChoice("deepseek", "", biz, modes)
        assertEquals("项目默认：仅可查看", dflt.defaultLabel)
        assertNull(dflt.above)
        assertEquals("比项目「商务」定的 DSH 权限「仅可查看」高：这次会按你选的「完全权限」来", modeChoice("deepseek", "danger-full-access", biz, modes).above)
        assertNull(modeChoice("claude", "", biz, modes).defaultLabel)
    }
}
