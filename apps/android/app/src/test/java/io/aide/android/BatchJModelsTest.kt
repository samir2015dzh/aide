package io.aide.android

import io.aide.android.api.AgentDefaults
import io.aide.android.api.AgentModel
import io.aide.android.api.AgentStatus
import io.aide.android.api.Session
import io.aide.android.model.ConvParams
import io.aide.android.model.ModelChoice
import io.aide.android.model.changedParams
import io.aide.android.model.defaultModelChoice
import io.aide.android.model.listedModel
import io.aide.android.model.modelChoices
import io.aide.android.model.modelTitle
import io.aide.android.model.paramsOf
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Before
import org.junit.Test
import java.util.Locale

/** 批 J, the models a conversation runs on (工单 #98, #99), as the desktop's @aide/core has them. */
class BatchJModelsTest {
    @Before
    fun chinese() = Locale.setDefault(Locale.SIMPLIFIED_CHINESE)

    private val claude = AgentStatus(
        "claude", available = true,
        models = listOf(
            AgentModel("opus", "Opus", resolved = "claude-opus-5-5"),
            AgentModel("fable", "Fable", resolved = "claude-fable-5-2"),
            AgentModel("sonnet[1m]", "Sonnet (1M context)", resolved = "claude-sonnet-5-5[1m]"),
            AgentModel("haiku", "Haiku"),
        ),
        own = AgentDefaults(model = "claude-sonnet-5-5"),
    )

    // Each model its specific one, as the machine found it; a family alone is said to be one whose model is unknown.
    @Test
    fun claudeModelsAreTheirSpecificOnes() {
        assertEquals(
            listOf(
                ModelChoice("opus", "Opus 5.5", "claude-opus-5-5"),
                ModelChoice("fable", "Fable 5.2", "claude-fable-5-2"),
                ModelChoice("sonnet[1m]", "Sonnet 5.5 (1M)", "claude-sonnet-5-5[1m]"),
                ModelChoice("haiku", "Haiku", "", unknown = true),
            ),
            modelChoices("claude", claude.models),
        )
        assertEquals(listOf("Opus 5.5 · claude-opus-5-5", "Haiku · 具体型号未知"), modelChoices("claude", claude.models).let { listOf(it[0].label(), it[3].label()) })
        assertEquals("Opus 5.5", modelTitle("opus", claude.models))
        assertEquals(ModelChoice("claude-sonnet-5-5", "Sonnet 5.5", "claude-sonnet-5-5"), defaultModelChoice(claude))
        assertEquals("Opus 5.5", defaultModelChoice(claude.copy(defaults = AgentDefaults(model = "opus")))?.title)
        assertNull(defaultModelChoice(AgentStatus("claude")))
        // One a conversation runs that is not listed is listed first.
        assertEquals(ModelChoice("claude-opus-4-1", "Opus 4.1", "claude-opus-4-1"), modelChoices("claude", claude.models, "claude-opus-4-1").first())
    }

    // DSH's: its own names, its IDs read out; its default the one its sessions run unless told.
    @Test
    fun dshModelsAreItsOwn() {
        val dsh = AgentStatus("deepseek", models = listOf(AgentModel("[\"deepseek\",\"deepseek-v4-pro\"]", "DeepSeek V4 Pro (DeepSeek)"), AgentModel("magpie/v4-flash")), own = AgentDefaults(model = "[\"deepseek\",\"deepseek-v4-pro\"]"))
        assertEquals(
            listOf(ModelChoice("[\"deepseek\",\"deepseek-v4-pro\"]", "DeepSeek V4 Pro (DeepSeek)", "deepseek/deepseek-v4-pro"), ModelChoice("magpie/v4-flash", "magpie/v4-flash", "")),
            modelChoices("deepseek", dsh.models),
        )
        assertEquals("DeepSeek V4 Pro (DeepSeek)", defaultModelChoice(dsh)?.title)
    }

    // 工单 #67 rule 4 with #99: one that runs the model an alias resolves to has that alias: no new conversation.
    @Test
    fun theModelItRunsIsTheAliasThatRunsIt() {
        val ran = Session(sessionId = "s1", machineId = "m1", projectId = "web", agent = "claude", model = "claude-opus-5-5")
        assertEquals("opus", listedModel("claude-opus-5-5", claude.models))
        assertEquals(ConvParams("web", "opus"), paramsOf(ran, claude.models))
        assertEquals(emptyList<String>(), changedParams(ran, ConvParams("web", "opus"), claude.models))
        assertEquals(listOf("model"), changedParams(ran, ConvParams("web", "fable"), claude.models))
    }
}
