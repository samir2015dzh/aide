package io.aide.android

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.getUnclippedBoundsInRoot
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.unit.dp
import io.aide.android.ui.AideTheme
import io.aide.android.ui.PageTitle
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * 工单 #167: a main tab's title, scrolled, is no centred banner. It stays where the large title began, at the
 * page's left edge like everything under it, and it is the one title there: the summary line goes with the large one.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], application = android.app.Application::class, qualifiers = "zh-rCN-w392dp-h850dp-port-xxhdpi")
class PageTitleTest {
    @get:Rule val rule = createComposeRule()

    @Test fun collapsedTitleStaysWhereTheLargeOneBegan() {
        var collapsed by mutableStateOf(false)
        rule.setContent { AideTheme { Column(Modifier.fillMaxWidth()) { PageTitle("Bot", "2 个在线", collapsed = collapsed) } } }
        val large = rule.onNodeWithText("Bot").getUnclippedBoundsInRoot()
        assertEquals(20.dp, large.left)

        collapsed = true
        rule.mainClock.advanceTimeBy(1_000)
        rule.waitForIdle()
        rule.onAllNodesWithText("Bot").fetchSemanticsNodes().let { assertEquals("one title once scrolled", 1, it.size) }
        val small = rule.onNodeWithText("Bot").getUnclippedBoundsInRoot()
        // Not a full-width centred bar: it begins where the large title began.
        assertEquals(20.dp, small.left)
        assertTrue("smaller than the large title: $small vs $large", small.bottom - small.top < large.bottom - large.top)
        assertEquals("no summary once scrolled", 0, rule.onAllNodesWithText("2 个在线").fetchSemanticsNodes().size)
    }
}
