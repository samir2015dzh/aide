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

    // 审计 A（#132）：单行 ```bash ls``` 的信息串含反引号，不是围栏，不能吞掉后面全文。
    @Test
    fun singleLineTripleBacktickIsInlineCodeNotAFence() {
        val blocks = parseMarkdownBlocks("先运行 ```bash ls``` 看看\n\n```bash ls```\n\n## 后续标题\n\n后续正文")
        assertEquals(
            listOf(
                MdBlock.Paragraph("先运行 ```bash ls``` 看看"),
                MdBlock.Paragraph("```bash ls```"),
                MdBlock.Heading(2, "后续标题"),
                MdBlock.Paragraph("后续正文"),
            ),
            blocks,
        )
        assertEquals(listOf(MdInline.Code("bash ls")), parseInlineMarkdown("```bash ls```"))
    }

    // 审计 A：列表项里缩进 2–4 格的围栏代码属于该项，不打断列表，也不并进项文字。
    @Test
    fun fencedCodeInsideListItemsBelongsToTheItem() {
        val md = listOf(
            "1. 安装依赖：",
            "    ```bash",
            "    pnpm install",
            "",
            "      --frozen-lockfile",
            "    ```",
            "2. 运行：",
            "   ```",
            "   make test",
            "   ```",
            "   然后查看输出",
            "3. 完成",
        ).joinToString("\n")
        val blocks = parseMarkdownBlocks(md)
        assertEquals(1, blocks.size)
        val list = blocks[0] as MdBlock.ListBlock
        assertEquals(true, list.ordered)
        assertEquals(3, list.items.size)
        assertEquals(
            MdListItem(
                "安装依赖：",
                number = 1,
                blocks = listOf(MdBlock.CodeBlock("bash", "pnpm install\n\n  --frozen-lockfile")),
            ),
            list.items[0],
        )
        assertEquals(
            MdListItem(
                "运行：",
                number = 2,
                blocks = listOf(MdBlock.CodeBlock("", "make test"), MdBlock.Paragraph("然后查看输出")),
            ),
            list.items[1],
        )
        assertEquals(MdListItem("完成", number = 3), list.items[2])

        // 顶层围栏缩进的空格从代码行里去掉（CommonMark：去掉不超过围栏缩进的前导空格）。
        assertEquals(
            listOf(MdBlock.CodeBlock("sh", "echo 1\n  echo 2")),
            parseMarkdownBlocks("  ```sh\n  echo 1\n    echo 2\n  ```"),
        )
    }

    // 审计 A：URL 里成对的括号是 URL 的一部分。
    @Test
    fun linksKeepBalancedParenthesesInUrl() {
        assertEquals(
            listOf(
                MdInline.Text("见 "),
                MdInline.Link("词条", "https://example.com/wiki/Foo_(bar)"),
                MdInline.Text("）。"),
            ),
            parseInlineMarkdown("见 [词条](https://example.com/wiki/Foo_(bar))）。"),
        )
        assertEquals(
            listOf(MdInline.Text("("), MdInline.Link("a", "https://example.com/a"), MdInline.Text(")")),
            parseInlineMarkdown("([a](https://example.com/a))"),
        )
        // 括号不配对（缺右括号）时整段保持原文。
        assertEquals(
            listOf(MdInline.Text("[a](https://example.com/x_(y)")),
            parseInlineMarkdown("[a](https://example.com/x_(y)"),
        )
    }

    // 审计 A：4 空格缩进是下一层，不是跳到第二层；层级按祖先缩进算。
    @Test
    fun fourSpaceIndentNestsOneLevel() {
        val md = listOf("- a", "    - b", "        - c", "    - d", "- e", " - f").joinToString("\n")
        val list = parseMarkdownBlocks(md).single() as MdBlock.ListBlock
        assertEquals(listOf(0, 1, 2, 1, 0, 0), list.items.map { it.level })
        val tabbed = parseMarkdownBlocks("1. a\n\t1. b").single() as MdBlock.ListBlock
        assertEquals(listOf(0, 1), tabbed.items.map { it.level })
    }

    // 审计 A：病态输入（长串反引号、大量未闭合的 [ 或 *）不能是 O(n²)——流式输出每个 delta 都会整段重解析。
    @Test(timeout = 5_000)
    fun pathologicalInlineInputIsLinear() {
        val n = 200_000
        val ticks = "`".repeat(n)
        assertEquals(listOf(MdInline.Text(ticks)), parseInlineMarkdown(ticks))
        val brackets = "[".repeat(n) + "]"
        assertEquals(listOf(MdInline.Text(brackets)), parseInlineMarkdown(brackets))
        val bracketParen = "[".repeat(n) + "](" + "x".repeat(10)
        assertEquals(listOf(MdInline.Text(bracketParen)), parseInlineMarkdown(bracketParen))
        val unclosedRuns = (1..600).joinToString("a") { "`".repeat(it) }
        assertEquals(listOf(MdInline.Text(unclosedRuns)), parseInlineMarkdown(unclosedRuns))
        val stars = "**a ~~b ".repeat(n / 8)
        assertTrue(parseInlineMarkdown(stars).isNotEmpty())
        val quotes = ">".repeat(n / 10) + " 深层引用"
        assertTrue(parseMarkdownBlocks(quotes).isNotEmpty())
    }
}
