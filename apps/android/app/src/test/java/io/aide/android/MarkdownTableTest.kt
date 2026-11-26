package io.aide.android

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import io.aide.android.ui.AideTheme
import io.aide.android.ui.MarkdownText
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/**
 * 审计 A（#132）：Markdown 表格里一格折行、同行其他格不折行时，旧布局会把矮的格子再测一次以撑满行高，
 * Compose 对同一子项第二次 measure() 抛 IllegalStateException。现在每格只测一次，这里真的渲染一张这样的表
 * （NATIVE 图形模式下文字按真实字宽排版，长格确实折行）。
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE) // real text measurement, so the long cell truly wraps
@Config(sdk = [35], application = android.app.Application::class, qualifiers = "zh-rCN-w392dp-h850dp-port-xxhdpi")
class MarkdownTableTest {
    @get:Rule val rule = createComposeRule()

    private val long = "这一格很长很长，" + "超过列宽上限之后一定会折成好几行，".repeat(6) + "结束"
    private val table = """
        | 名称 | 说明 | 状态 |
        | :--- | :--- | ---: |
        | 短 | $long | 好 |
        | 甲 | 乙 | 丙 |
    """.trimIndent()

    @Test fun rendersTableWhereOneCellWrapsAndOthersDoNot() {
        rule.setContent {
            AideTheme { Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) { MarkdownText(table) } }
        }
        rule.waitForIdle()
        rule.onNodeWithText("名称").assertIsDisplayed()
        rule.onNodeWithText("丙").assertIsDisplayed()
        val tall = rule.onNodeWithText(long).fetchSemanticsNode().boundsInRoot
        val short = rule.onNodeWithText("短").fetchSemanticsNode().boundsInRoot
        val right = rule.onNodeWithText("好").fetchSemanticsNode().boundsInRoot
        // The long cell really wraps (several lines), its neighbours stay one line, and all three share the row's top.
        assertTrue("long cell wraps: ${tall.height} vs ${short.height}", tall.height > short.height * 2)
        assertTrue("short cells keep one line", right.height < tall.height)
        assertTrue("row-aligned tops", kotlin.math.abs(tall.top - short.top) < 1f && kotlin.math.abs(right.top - short.top) < 1f)
    }

    @Test fun rendersWrappingTableUnselectable() {
        rule.setContent { AideTheme { MarkdownText(table, selectable = false) } }
        rule.waitForIdle()
        rule.onNodeWithText(long).assertIsDisplayed()
    }
}
