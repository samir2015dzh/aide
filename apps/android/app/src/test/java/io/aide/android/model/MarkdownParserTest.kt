package io.aide.android.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class MarkdownParserTest {

    @Test
    fun parsesHeadingsParagraphsRulesAndBlockquotes() {
        val md = """
            # 主标题

            ## 二级标题

            普通段落包含 **真实加粗**、*斜体*、~~删除线~~ 与 `inline_code`。

            > 引用段落说明
            > 第二行内容

            ---
        """.trimIndent()

        val blocks = parseMarkdownBlocks(md)
        assertEquals(5, blocks.size)
        assertEquals(MdBlock.Heading(1, "主标题"), blocks[0])
        assertEquals(MdBlock.Heading(2, "二级标题"), blocks[1])
        assertTrue(blocks[2] is MdBlock.Paragraph)
        val quote = blocks[3] as MdBlock.BlockQuote
        assertEquals(1, quote.blocks.size)
        assertEquals(MdBlock.Rule, blocks[4])
    }

    @Test
    fun parsesInlineFormattingAndLinks() {
        val nodes = parseInlineMarkdown("见 **加粗 `code`** 与 [文档](https://example.com/docs) 及 *斜体*")
        assertEquals(
            listOf(
                MdInline.Text("见 "),
                MdInline.Bold(listOf(MdInline.Text("加粗 "), MdInline.Code("code"))),
                MdInline.Text(" 与 "),
                MdInline.Link("文档", "https://example.com/docs"),
                MdInline.Text(" 及 "),
                MdInline.Italic(listOf(MdInline.Text("斜体"))),
            ),
            nodes,
        )
    }

    @Test
    fun parsesListsAndTaskCheckboxes() {
        val md = """
            - 第一项
              - 嵌套子项
            - [x] 已完成
            - [ ] 待完成
        """.trimIndent()

        val blocks = parseMarkdownBlocks(md)
        assertEquals(1, blocks.size)
        val list = blocks[0] as MdBlock.ListBlock
        assertEquals(false, list.ordered)
        assertEquals(4, list.items.size)
        assertEquals(MdListItem("第一项", level = 0), list.items[0])
        assertEquals(MdListItem("嵌套子项", level = 1), list.items[1])
        assertEquals(MdListItem("已完成", level = 0, checked = true), list.items[2])
        assertEquals(MdListItem("待完成", level = 0, checked = false), list.items[3])
    }

    @Test
    fun parsesFencedCodeBlocksIncludingUnclosedStreamingFence() {
        val closed = parseMarkdownBlocks("```kotlin\nval x = 1\n```\n\n后续正文")
        assertEquals(2, closed.size)
        assertEquals(MdBlock.CodeBlock("kotlin", "val x = 1"), closed[0])
        assertEquals(MdBlock.Paragraph("后续正文"), closed[1])

        val streaming = parseMarkdownBlocks("```go\nfmt.Println(1)")
        assertEquals(listOf(MdBlock.CodeBlock("go", "fmt.Println(1)")), streaming)
    }

    @Test
    fun parsesGfmTablesWithAlignmentAndEscapedPipes() {
        val md = """
            | 模块 | 状态 | 耗时 |
            | :--- | :---: | ---: |
            | **桌面端** | 通过 | `120ms` |
            | 安卓端 \| 表格 | 通过 | `95ms` |
        """.trimIndent()

        val blocks = parseMarkdownBlocks(md)
        assertEquals(1, blocks.size)
        val table = blocks[0] as MdBlock.Table
        assertEquals(listOf("模块", "状态", "耗时"), table.headers)
        assertEquals(listOf(MdAlign.Left, MdAlign.Center, MdAlign.Right), table.alignments)
        assertEquals(2, table.rows.size)
        assertEquals(listOf("**桌面端**", "通过", "`120ms`"), table.rows[0])
        assertEquals(listOf("安卓端 | 表格", "通过", "`95ms`"), table.rows[1])
    }

    @Test
    fun preservesPipesInsideInlineCodeAndMidWordDoubleUnderscores() {
        val md = """
            | 表达式 | 说明 |
            | --- | --- |
            | `a | b` | 位或运算 |
        """.trimIndent()
        val blocks = parseMarkdownBlocks(md)
        assertEquals(1, blocks.size)
        val table = blocks[0] as MdBlock.Table
        assertEquals(listOf("`a | b`", "位或运算"), table.rows[0])

        val notTable = parseMarkdownBlocks("foo | bar\n--")
        assertEquals(1, notTable.size)
        assertTrue(notTable[0] is MdBlock.Paragraph)

        val inline = parseInlineMarkdown("keep foo__bar__baz literal, __bold__ works")
        assertEquals(
            listOf(
                MdInline.Text("keep foo__bar__baz literal, "),
                MdInline.Bold(listOf(MdInline.Text("bold"))),
                MdInline.Text(" works"),
            ),
            inline,
        )
    }
}
