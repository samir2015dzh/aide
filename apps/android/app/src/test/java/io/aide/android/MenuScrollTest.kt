package io.aide.android

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import io.aide.android.ui.AideTheme
import io.aide.android.ui.ChipPicker
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * 工单 #174: the composer's model picker with a DSH gateway's many models (the owner's had a custom provider's under
 * DeepSeek's) on a phone: the list scrolls to its last model, which can be chosen. It was cut off at the screen's edge.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class, qualifiers = "w360dp-h640dp-port-xxhdpi")
class MenuScrollTest {
    @get:Rule val rule = createComposeRule()

    @Test fun theLastModelCanBeChosen() {
        val models = (1..30).map { "magpie · workbuddy-2/cn:model-$it" }
        var chosen = ""
        rule.setContent {
            AideTheme {
                var value by remember { mutableStateOf("") }
                Box(Modifier.fillMaxSize(), contentAlignment = Alignment.BottomStart) { // as the composer sits, at the foot
                    ChipPicker("模型", value, listOf("" to "默认模型") + models.map { it to it }) { value = it; chosen = it }
                }
            }
        }
        rule.onNodeWithText("默认模型").performClick()
        rule.onNodeWithText(models.last()).performScrollTo().assertIsDisplayed().performClick()
        rule.waitForIdle()
        assertEquals(models.last(), chosen)
    }
}
