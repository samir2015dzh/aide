package io.aide.android.model

/**
 * Lightweight GFM Markdown parser for Aide Android conversations and long reports (工单 #132).
 * Pure Kotlin (no Android framework dependencies) so block & inline parsing are directly unit-tested.
 */
sealed interface MdBlock {
    data class Heading(val level: Int, val text: String) : MdBlock
    data class Paragraph(val text: String) : MdBlock
    data class CodeBlock(val language: String, val code: String) : MdBlock
    data class BlockQuote(val blocks: List<MdBlock>) : MdBlock
    data class ListBlock(val ordered: Boolean, val items: List<MdListItem>) : MdBlock
    data class Table(
        val headers: List<String>,
        val alignments: List<MdAlign>,
        val rows: List<List<String>>,
    ) : MdBlock
    data object Rule : MdBlock
}

enum class MdAlign { Left, Center, Right }

data class MdListItem(
    val text: String,
    val level: Int = 0,
    val number: Int? = null,
    val checked: Boolean? = null,
)

sealed interface MdInline {
    data class Text(val text: String) : MdInline
    data class Bold(val children: List<MdInline>) : MdInline
    data class Italic(val children: List<MdInline>) : MdInline
    data class Strike(val children: List<MdInline>) : MdInline
    data class Code(val code: String) : MdInline
    data class Link(val label: String, val url: String) : MdInline
}

private val headingRegex = Regex("""^\s{0,3}(#{1,6})(?:\s+(.*?))?\s*#*\s*$""")
private val fenceRegex = Regex("""^\s{0,3}(`{3,}|~{3,})\s*([^\s`~]*).*$""")
private val ruleRegex = Regex("""^\s{0,3}(?:(\*\s*){3,}|(-\s*){3,}|(_\s*){3,})$""")
private val bulletRegex = Regex("""^(\s*)[-*+]\s+(.*)$""")
private val orderedRegex = Regex("""^(\s*)(\d+)[.)]\s+(.*)$""")
private val taskRegex = Regex("""^\[([ xX])]\s+(.*)$""")

fun parseMarkdownBlocks(input: String): List<MdBlock> {
    if (input.isBlank()) return emptyList()
    val lines = input.replace("\r\n", "\n").replace('\r', '\n').split('\n')
    val out = mutableListOf<MdBlock>()
    var i = 0

    while (i < lines.size) {
        val line = lines[i]
        if (line.isBlank()) {
            i++
            continue
        }

        // 1. Fenced code block (``` or ~~~)
        val fence = fenceRegex.matchEntire(line)
        if (fence != null) {
            val marker = fence.groupValues[1]
            val markerChar = marker[0]
            val markerLen = marker.length
            val lang = fence.groupValues[2].trim()
            val codeLines = mutableListOf<String>()
            i++
            while (i < lines.size) {
                val close = lines[i].trim()
                if (close.length >= markerLen && close.all { it == markerChar }) {
                    i++
                    break
                }
                codeLines.add(lines[i])
                i++
            }
            out.add(MdBlock.CodeBlock(lang, codeLines.joinToString("\n")))
            continue
        }

        // 2. ATX Heading (# .. ######)
        val head = headingRegex.matchEntire(line)
        if (head != null) {
            val level = head.groupValues[1].length
            val text = head.groupValues[2].trim()
            if (text.isNotEmpty()) {
                out.add(MdBlock.Heading(level, text))
                i++
                continue
            }
        }

        // 3. Horizontal rule
        if (ruleRegex.matches(line)) {
            out.add(MdBlock.Rule)
            i++
            continue
        }

        // 4. GFM pipe table (header row + separator row)
        if (i + 1 < lines.size && isTableSeparator(lines[i + 1]) && looksLikeTableRow(line)) {
            val headers = splitTableRow(line)
            val alignments = parseTableAlignments(lines[i + 1], headers.size)
            if (headers.isNotEmpty() && alignments.isNotEmpty()) {
                val colCount = maxOf(headers.size, alignments.size)
                val normHeaders = padRow(headers, colCount)
                val normAligns = alignments + List((colCount - alignments.size).coerceAtLeast(0)) { MdAlign.Left }
                val rows = mutableListOf<List<String>>()
                i += 2
                while (i < lines.size && lines[i].isNotBlank() && looksLikeTableRow(lines[i])) {
                    if (isTableSeparator(lines[i])) break
                    rows.add(padRow(splitTableRow(lines[i]), colCount))
                    i++
                }
                out.add(MdBlock.Table(normHeaders, normAligns, rows))
                continue
            }
        }

        // 5. Blockquote (> ...)
        if (line.trimStart().startsWith(">")) {
            val quoteLines = mutableListOf<String>()
            while (i < lines.size && lines[i].trimStart().startsWith(">")) {
                val stripped = lines[i].trimStart().removePrefix(">").removePrefix(" ")
                quoteLines.add(stripped)
                i++
            }
            out.add(MdBlock.BlockQuote(parseMarkdownBlocks(quoteLines.joinToString("\n"))))
            continue
        }

        // 6. Unordered / Ordered list
        val bullet = bulletRegex.matchEntire(line)
        val ordered = if (bullet == null) orderedRegex.matchEntire(line) else null
        if (bullet != null || ordered != null) {
            val isOrdered = ordered != null
            val items = mutableListOf<MdListItem>()
            while (i < lines.size) {
                val cur = lines[i]
                if (cur.isBlank()) break
                val bm = bulletRegex.matchEntire(cur)
                val om = if (bm == null) orderedRegex.matchEntire(cur) else null
                if (bm != null) {
                    val indent = bm.groupValues[1].replace("\t", "  ").length / 2
                    val raw = bm.groupValues[2]
                    val task = taskRegex.matchEntire(raw)
                    if (task != null) {
                        items.add(MdListItem(task.groupValues[2], level = indent.coerceAtMost(3), checked = task.groupValues[1].equals("x", ignoreCase = true)))
                    } else {
                        items.add(MdListItem(raw, level = indent.coerceAtMost(3)))
                    }
                    i++
                } else if (om != null) {
                    val indent = om.groupValues[1].replace("\t", "  ").length / 2
                    items.add(MdListItem(om.groupValues[3], level = indent.coerceAtMost(3), number = om.groupValues[2].toIntOrNull()))
                    i++
                } else if (items.isNotEmpty() && cur.startsWith("  ") && !startsNewBlock(lines, i)) {
                    val last = items.removeAt(items.lastIndex)
                    items.add(last.copy(text = last.text + "\n" + cur.trim()))
                    i++
                } else {
                    break
                }
            }
            out.add(MdBlock.ListBlock(ordered = isOrdered, items = items))
            continue
        }

        // 7. Paragraph (coalesce consecutive non-block lines)
        val para = mutableListOf<String>()
        while (i < lines.size && lines[i].isNotBlank()) {
            if (para.isNotEmpty() && startsNewBlock(lines, i)) break
            para.add(lines[i])
            i++
        }
        out.add(MdBlock.Paragraph(para.joinToString("\n")))
    }

    return out
}

private fun startsNewBlock(lines: List<String>, index: Int): Boolean {
    val line = lines[index]
    if (fenceRegex.matches(line)) return true
    if (headingRegex.matches(line)) return true
    if (ruleRegex.matches(line)) return true
    if (line.trimStart().startsWith(">")) return true
    if (bulletRegex.matches(line) || orderedRegex.matches(line)) return true
    if (index + 1 < lines.size && isTableSeparator(lines[index + 1]) && looksLikeTableRow(line)) return true
    return false
}

private fun looksLikeTableRow(line: String): Boolean {
    val t = line.trim()
    return t.contains('|') && t.any { it != '|' && !it.isWhitespace() }
}

private fun isTableSeparator(line: String): Boolean {
    val t = line.trim()
    if (!t.contains('|') || !t.contains('-')) return false
    val cells = splitTableRow(t)
    if (cells.isEmpty()) return false
    return cells.all { cell ->
        val c = cell.trim()
        c.length >= 2 && Regex("""^:?-+:?$""").matches(c) && c.count { it == '-' } >= 1
    }
}

private fun splitTableRow(line: String): List<String> {
    var s = line.trim()
    if (s.startsWith("|")) s = s.substring(1)
    if (s.endsWith("|") && !s.endsWith("\\|")) s = s.substring(0, s.length - 1)
    val cells = mutableListOf<String>()
    val buf = StringBuilder()
    var i = 0
    var inCode = false
    while (i < s.length) {
        val ch = s[i]
        if (ch == '\\' && i + 1 < s.length && s[i + 1] == '|') {
            buf.append('|')
            i += 2
        } else if (ch == '`') {
            inCode = !inCode
            buf.append(ch)
            i++
        } else if (ch == '|' && !inCode) {
            cells.add(buf.toString().trim())
            buf.setLength(0)
            i++
        } else {
            buf.append(ch)
            i++
        }
    }
    cells.add(buf.toString().trim())
    return cells
}

private fun parseTableAlignments(line: String, expected: Int): List<MdAlign> {
    val cells = splitTableRow(line)
    if (cells.isEmpty()) return emptyList()
    return cells.map { raw ->
        val c = raw.trim()
        val left = c.startsWith(":")
        val right = c.endsWith(":")
        when {
            left && right -> MdAlign.Center
            right -> MdAlign.Right
            else -> MdAlign.Left
        }
    }.let { list ->
        if (list.size >= expected) list else list + List(expected - list.size) { MdAlign.Left }
    }
}

private fun padRow(row: List<String>, count: Int): List<String> =
    if (row.size >= count) row.take(count) else row + List(count - row.size) { "" }

fun parseInlineMarkdown(text: String): List<MdInline> {
    if (text.isEmpty()) return emptyList()
    val out = mutableListOf<MdInline>()
    val plain = StringBuilder()
    var i = 0

    fun flush() {
        if (plain.isNotEmpty()) {
            out.add(MdInline.Text(plain.toString()))
            plain.setLength(0)
        }
    }

    while (i < text.length) {
        val ch = text[i]

        // Escaped punctuation
        if (ch == '\\' && i + 1 < text.length && text[i + 1] in "\\`*_{}[]()#+-.!~|") {
            plain.append(text[i + 1])
            i += 2
            continue
        }

        // Inline code: `...` or ``...``
        if (ch == '`') {
            var tickLen = 1
            while (i + tickLen < text.length && text[i + tickLen] == '`') tickLen++
            val delim = "`".repeat(tickLen)
            val end = text.indexOf(delim, i + tickLen)
            if (end != -1) {
                flush()
                val inner = text.substring(i + tickLen, end)
                val trimmed = if (inner.length >= 2 && inner.startsWith(' ') && inner.endsWith(' ') && inner.any { it != ' ' }) {
                    inner.substring(1, inner.length - 1)
                } else inner
                out.add(MdInline.Code(trimmed))
                i = end + tickLen
                continue
            }
        }

        // Link: [label](url)
        if (ch == '[') {
            val closeBracket = text.indexOf(']', i + 1)
            if (closeBracket != -1 && closeBracket + 1 < text.length && text[closeBracket + 1] == '(') {
                val closeParen = text.indexOf(')', closeBracket + 2)
                if (closeParen != -1) {
                    val label = text.substring(i + 1, closeBracket)
                    val url = text.substring(closeBracket + 2, closeParen).trim()
                    if (url.startsWith("http://", ignoreCase = true) || url.startsWith("https://", ignoreCase = true)) {
                        flush()
                        out.add(MdInline.Link(label.ifEmpty { url }, url))
                        i = closeParen + 1
                        continue
                    }
                }
            }
        }

        // Bold + Italic: ***...***
        if (text.startsWith("***", i)) {
            val end = text.indexOf("***", i + 3)
            if (end > i + 3) {
                flush()
                out.add(MdInline.Bold(listOf(MdInline.Italic(parseInlineMarkdown(text.substring(i + 3, end))))))
                i = end + 3
                continue
            }
        }

        // Bold: **...** or __...__ (skip __ inside word identifiers like foo__bar__baz)
        if (text.startsWith("**", i) || (text.startsWith("__", i) && (i == 0 || !text[i - 1].isLetterOrDigit()))) {
            val delim = text.substring(i, i + 2)
            val end = text.indexOf(delim, i + 2)
            val okEnd = end > i + 2 && (delim == "**" || end + 2 >= text.length || !text[end + 2].isLetterOrDigit())
            if (okEnd) {
                flush()
                out.add(MdInline.Bold(parseInlineMarkdown(text.substring(i + 2, end))))
                i = end + 2
                continue
            }
        }

        // Strikethrough: ~~...~~
        if (text.startsWith("~~", i)) {
            val end = text.indexOf("~~", i + 2)
            if (end > i + 2) {
                flush()
                out.add(MdInline.Strike(parseInlineMarkdown(text.substring(i + 2, end))))
                i = end + 2
                continue
            }
        }

        // Italic: *...* (not followed by space so bullet/multiply signs stay literal)
        if (ch == '*' && i + 1 < text.length && !text[i + 1].isWhitespace()) {
            val end = text.indexOf('*', i + 1)
            if (end > i + 1 && !text[end - 1].isWhitespace()) {
                flush()
                out.add(MdInline.Italic(parseInlineMarkdown(text.substring(i + 1, end))))
                i = end + 1
                continue
            }
        }

        plain.append(ch)
        i++
    }

    flush()
    return out
}
