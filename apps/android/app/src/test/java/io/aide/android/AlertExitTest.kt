package io.aide.android

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import io.aide.android.ui.AideTheme
import io.aide.android.ui.AlertAction
import io.aide.android.ui.AlertRole
import io.aide.android.ui.IosAlert
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * 工单 #174: an alert closed by its Cancel scales and fades out first (工单 #173 said so; it vanished at once), and
 * is told only then; an action that does something is told at once, as it may keep the alert open while it works.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class)
class AlertExitTest {
    @get:Rule val rule = createComposeRule()

    @Test fun cancelLeavesThenCloses() {
        var cancelled = 0
        var done = 0
        rule.setContent {
            AideTheme {
                var open by remember { mutableStateOf(true) }
                if (open) IosAlert(
                    onDismissRequest = { open = false },
                    title = "Remove it?",
                    actions = listOf(
                        AlertAction("Cancel", AlertRole.Cancel) { cancelled++; open = false },
                        AlertAction("Remove", AlertRole.Destructive) { done++ },
                    ),
                )
            }
        }
        rule.waitForIdle()
        // An action that works: told at once, the alert stays.
        rule.onNodeWithText("Remove").performClick()
        rule.waitForIdle()
        assertEquals(1, done)
        assertEquals(1, rule.onAllNodesWithText("Remove it?").fetchSemanticsNodes().size)

        rule.mainClock.autoAdvance = false
        rule.onNodeWithText("Cancel").performClick()
        rule.mainClock.advanceTimeBy(50)
        assertEquals("not before it has gone", 0, cancelled)
        rule.mainClock.advanceTimeBy(400)
        rule.mainClock.autoAdvance = true
        rule.waitForIdle()
        assertEquals(1, cancelled)
        assertEquals(0, rule.onAllNodesWithText("Remove it?").fetchSemanticsNodes().size)
    }
}
