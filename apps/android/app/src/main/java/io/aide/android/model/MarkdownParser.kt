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
    /** Blocks nested under the item text, e.g. an indented fenced code block (审计 A). */
    val blocks: List<MdBlock> = emptyList(),
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
private val fenceRegex = Regex("""^( *)(`{3,}|~{3,})(.*)$""")
private val ruleRegex = Regex("""^\s{0,3}(?:(\*\s*){3,}|(-\s*){3,}|(_\s*){3,})$""")
private val bulletRegex = Regex("""^(\s*)[-*+]\s+(.*)$""")
private val orderedRegex = Regex("""^(\s*)(\d+)[.)]\s+(.*)$""")
private val taskRegex = Regex("""^\[([ xX])]\s+(.*)$""")

/** Nested blockquotes deeper than this are kept as text, so hostile input can't blow the stack. */
private const val MAX_BLOCK_DEPTH = 8

/** Inline emphasis nested deeper than this is kept as plain text (same reason). */
private const val MAX_INLINE_DEPTH = 16

private class FenceOpen(val indent: Int, val markerChar: Char, val markerLen: Int, val lang: String)

/**
 * An opening code fence indented by at most [maxIndent] spaces. As in CommonMark, a backtick
 * fence's info string may not contain a backtick, so a one-line "```bash ls```" is inline code,
 * not a fence that would swallow the rest of the message (审计 A).
 */
private fun matchFenceOpen(line: String, maxIndent: Int = 3): FenceOpen? {
    val m = fenceRegex.matchEntire(line) ?: return null
    val indent = m.groupValues[1].length
    if (indent > maxIndent) return null
    val marker = m.groupValues[2]
    val info = m.groupValues[3]
    if (marker[0] == '`' && info.contains('`')) return null
    val lang = info.trim().substringBefore(' ').substringBefore('\t')
    return FenceOpen(indent, marker[0], marker.length, lang)
}

/**
 * Reads the fenced code block opened at lines[start]; returns it and the index after it. Code lines
 * lose up to the fence's own indentation. An unclosed fence (streaming) runs to the end.
 */
private fun readFence(lines: List<String>, start: Int, open: FenceOpen): Pair<MdBlock.CodeBlock, Int> {
    val codeLines = mutableListOf<String>()
    var i = start + 1
    while (i < lines.size) {
        val close = lines[i].trim()
        if (close.length >= open.markerLen && close.all { it == open.markerChar }) {
            i++
            break
        }
        val l = lines[i]
        var strip = 0
        while (strip < open.indent && strip < l.length && l[strip] == ' ') strip++
        codeLines.add(l.substring(strip))
        i++
    }
    return MdBlock.CodeBlock(open.lang, codeLines.joinToString("\n")) to i
}

/** Leading whitespace width, a tab counting as 4 columns. */
private fun indentWidth(ws: String): Int = ws.fold(0) { acc, ch -> acc + if (ch == '\t') 4 else 1 }

fun parseMarkdownBlocks(input: String): List<MdBlock> = parseBlocks(input, 0)

private fun parseBlocks(input: String, depth: Int): List<MdBlock> {
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
        val fence = matchFenceOpen(line)
        if (fence != null) {
            val (code, next) = readFence(lines, i, fence)
            out.add(code)
            i = next
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
        if (depth < MAX_BLOCK_DEPTH && line.trimStart().startsWith(">")) {
            val quoteLines = mutableListOf<String>()
            while (i < lines.size && lines[i].trimStart().startsWith(">")) {
                val stripped = lines[i].trimStart().removePrefix(">").removePrefix(" ")
                quoteLines.add(stripped)
                i++
            }
            out.add(MdBlock.BlockQuote(parseBlocks(quoteLines.joinToString("\n"), depth + 1)))
            continue
        }

        // 6. Unordered / Ordered list
        val bullet = bulletRegex.matchEntire(line)
        val ordered = if (bullet == null) orderedRegex.matchEntire(line) else null
        if (bullet != null || ordered != null) {
            val isOrdered = ordered != null
            val items = mutableListOf<MdListItem>()
            // Indent widths of the currently open ancestor items: an item is one level deeper than
            // the nearest ancestor it is indented past by at least 2 columns, so 2- and 4-space
            // nesting styles both go 0, 1, 2 … (审计 A: 4 spaces used to jump straight to level 2).
            val indentStack = mutableListOf<Int>()
            fun levelFor(ws: String): Int {
                val w = indentWidth(ws)
                while (indentStack.isNotEmpty() && indentStack[indentStack.lastIndex] > w) indentStack.removeAt(indentStack.lastIndex)
                if (indentStack.isEmpty() || w >= indentStack[indentStack.lastIndex] + 2) indentStack.add(w)
                return (indentStack.size - 1).coerceAtMost(3)
            }
            // Column where the last item's text starts ("1. foo" -> 3); its nested blocks may be
            // indented from 2 up to that plus 3 more spaces.
            var contentOffset = 0
            fun appendToLast(transform: (MdListItem) -> MdListItem) {
                items[items.lastIndex] = transform(items[items.lastIndex])
            }
            while (i < lines.size) {
                val cur = lines[i]
                if (cur.isBlank()) break
                val bm = bulletRegex.matchEntire(cur)
                val om = if (bm == null) orderedRegex.matchEntire(cur) else null
                val nestedFence = if (bm == null && om == null && items.isNotEmpty()) {
                    matchFenceOpen(cur, maxIndent = contentOffset + 3)?.takeIf { it.indent >= 2 }
                } else null
                if (bm != null) {
                    val level = levelFor(bm.groupValues[1])
                    val raw = bm.groupValues[2]
                    contentOffset = cur.length - raw.length
                    val task = taskRegex.matchEntire(raw)
                    if (task != null) {
                        items.add(MdListItem(task.groupValues[2], level = level, checked = task.groupValues[1].equals("x", ignoreCase = true)))
                    } else {
                        items.add(MdListItem(raw, level = level))
                    }
                    i++
                } else if (om != null) {
                    val level = levelFor(om.groupValues[1])
                    contentOffset = cur.length - om.groupValues[3].length
                    items.add(MdListItem(om.groupValues[3], level = level, number = om.groupValues[2].toIntOrNull()))
                    i++
                } else if (nestedFence != null) {
                    val (code, next) = readFence(lines, i, nestedFence)
                    appendToLast { it.copy(blocks = it.blocks + code) }
                    i = next
                } else if (items.isNotEmpty() && cur.startsWith("  ") && !startsNewBlock(lines, i)) {
                    val text = cur.trim()
                    appendToLast { last ->
                        val tail = last.blocks.lastOrNull()
                        when {
                            last.blocks.isEmpty() -> last.copy(text = last.text + "\n" + text)
                            tail is MdBlock.Paragraph -> last.copy(blocks = last.blocks.dropLast(1) + MdBlock.Paragraph(tail.text + "\n" + text))
                            else -> last.copy(blocks = last.blocks + MdBlock.Paragraph(text))
                        }
                    }
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
    if (matchFenceOpen(line) != null) return true
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

fun parseInlineMarkdown(text: String): List<MdInline> = parseInline(text, 0)

/** Start of the first run of exactly [len] backticks at or after [from], or -1. Linear in the scanned span. */
private fun findTickRun(text: String, from: Int, len: Int): Int {
    var j = from
    while (j < text.length) {
        if (text[j] != '`') {
            j++
            continue
        }
        val start = j
        while (j < text.length && text[j] == '`') j++
        if (j - start == len) return start
    }
    return -1
}

/** Index of the ')' closing a link destination that starts at [from] (balanced parentheses), or -1. */
private fun findLinkClose(text: String, from: Int): Int {
    var depth = 0
    var j = from
    while (j < text.length) {
        when (text[j]) {
            '\\' -> j++
            '\n' -> return -1
            '(' -> depth++
            ')' -> if (depth == 0) return j else depth--
        }
        j++
    }
    return -1
}

/**
 * Inline parser. Every delimiter search that fails to find a closer is remembered for the rest of
 * this call (scanning only moves forward, so a later search would fail too), which keeps long runs
 * of backticks or unclosed "[", "*", "~~" linear instead of O(n²) (审计 A) — streaming replies
 * re-parse the whole message on every delta.
 */
private fun parseInline(text: String, depth: Int): List<MdInline> {
    if (text.isEmpty()) return emptyList()
    if (depth > MAX_INLINE_DEPTH) return listOf(MdInline.Text(text))
    val out = mutableListOf<MdInline>()
    val plain = StringBuilder()
    var i = 0
    // Backtick run lengths with no closing run left in the text.
    val noTickCloser = HashSet<Int>()
    // Emphasis delimiters with no closer left in the text.
    val noCloser = HashSet<String>()
    // First ']' at or after the last lookup position (-1: none left), and the memoised link
    // destination close for that ']' — many '[' share the same ']'.
    var nextBracket = -2
    var parenFor = -1
    var parenAt = -1

    fun flush() {
        if (plain.isNotEmpty()) {
            out.add(MdInline.Text(plain.toString()))
            plain.setLength(0)
        }
    }

    fun closerOf(delim: String, from: Int): Int {
        if (delim in noCloser) return -1
        val end = text.indexOf(delim, from)
        if (end == -1) noCloser.add(delim)
        return end
    }

    while (i < text.length) {
        val ch = text[i]

        // Escaped punctuation
        if (ch == '\\' && i + 1 < text.length && text[i + 1] in "\\`*_{}[]()#+-.!~|") {
            plain.append(text[i + 1])
            i += 2
            continue
        }

        // Inline code: `...` or ``...`` — the closing run must be exactly as long as the opening one.
        if (ch == '`') {
            var tickLen = 1
            while (i + tickLen < text.length && text[i + tickLen] == '`') tickLen++
            val end = if (tickLen in noTickCloser) -1 else findTickRun(text, i + tickLen, tickLen)
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
            noTickCloser.add(tickLen)
            // The whole unmatched run is literal text.
            plain.append(text, i, i + tickLen)
            i += tickLen
            continue
        }

        // Link: [label](url) — the URL may contain balanced parentheses.
        if (ch == '[') {
            if (nextBracket != -1 && nextBracket <= i) nextBracket = text.indexOf(']', i + 1)
            val closeBracket = nextBracket
            if (closeBracket != -1 && closeBracket + 1 < text.length && text[closeBracket + 1] == '(') {
                if (parenFor != closeBracket) {
                    parenFor = closeBracket
                    parenAt = findLinkClose(text, closeBracket + 2)
                }
                val closeParen = parenAt
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
            val end = closerOf("***", i + 3)
            if (end > i + 3) {
                flush()
                out.add(MdInline.Bold(listOf(MdInline.Italic(parseInline(text.substring(i + 3, end), depth + 1)))))
                i = end + 3
                continue
            }
        }

        // Bold: **...** or __...__ (skip __ inside word identifiers like foo__bar__baz)
        if (text.startsWith("**", i) || (text.startsWith("__", i) && (i == 0 || !text[i - 1].isLetterOrDigit()))) {
            val delim = text.substring(i, i + 2)
            val end = closerOf(delim, i + 2)
            val okEnd = end > i + 2 && (delim == "**" || end + 2 >= text.length || !text[end + 2].isLetterOrDigit())
            if (okEnd) {
                flush()
                out.add(MdInline.Bold(parseInline(text.substring(i + 2, end), depth + 1)))
                i = end + 2
                continue
            }
        }

        // Strikethrough: ~~...~~
        if (text.startsWith("~~", i)) {
            val end = closerOf("~~", i + 2)
            if (end > i + 2) {
                flush()
                out.add(MdInline.Strike(parseInline(text.substring(i + 2, end), depth + 1)))
                i = end + 2
                continue
            }
        }

        // Italic: *...* (not followed by space so bullet/multiply signs stay literal)
        if (ch == '*' && i + 1 < text.length && !text[i + 1].isWhitespace()) {
            val end = closerOf("*", i + 1)
            if (end > i + 1 && !text[end - 1].isWhitespace()) {
                flush()
                out.add(MdInline.Italic(parseInline(text.substring(i + 1, end), depth + 1)))
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
