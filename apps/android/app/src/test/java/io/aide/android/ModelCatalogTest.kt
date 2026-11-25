package io.aide.android

import io.aide.android.api.AgentModel
import io.aide.android.model.formatContextWindow
import io.aide.android.model.modelMetaSummary
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ModelCatalogTest {
    @Test
    fun formatsContextWindowAndSummaryForKnownModelsOnly() {
        assertEquals("", formatContextWindow(0))
        assertEquals("200K", formatContextWindow(200_000))
        assertEquals("1M", formatContextWindow(1_048_576))

        val known = AgentModel(
            id = "sonnet",
            name = "Anthropic · claude-sonnet-4-5",
            resolved = "claude-sonnet-4-5",
            contextWindow = 200_000,
            maxOutputTokens = 64_000,
            capabilities = listOf("reasoning", "tools", "vision"),
            inputCostPerMillion = 3.0,
            outputCostPerMillion = 15.0,
        )
        val summary = modelMetaSummary(known)
        assertTrue(summary.contains("200K"))
        assertTrue(summary.contains("reasoning/tools/vision"))
        assertTrue(summary.contains("~$3/$15"))

        val unknown = AgentModel(id = "private-model-1", name = "Custom · private-model-1")
        assertEquals("", modelMetaSummary(unknown))
    }
}
