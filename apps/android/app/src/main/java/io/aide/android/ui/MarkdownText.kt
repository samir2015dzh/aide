package io.aide.android.ui

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.Layout
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.LinkAnnotation
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.TextLinkStyles
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.withLink
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import io.aide.android.model.MdAlign
import io.aide.android.model.MdBlock
import io.aide.android.model.MdInline
import io.aide.android.model.parseInlineMarkdown
import io.aide.android.model.parseMarkdownBlocks
import kotlinx.coroutines.delay

/**
 * Fluent 2 Markdown renderer for Android conversations and long reports (工单 #132 / #106).
 * Renders structured headings, crisp bold/italic/inline-code hierarchy, lists, blockquotes,
 * copyable fenced code blocks, and horizontally scrollable aligned GFM tables.
 */
@Composable
fun MarkdownText(
    text: String,
    modifier: Modifier = Modifier,
    color: Color = MaterialTheme.colorScheme.onSurface,
    selectable: Boolean = true,
) {
    val blocks = remember(text) { parseMarkdownBlocks(text) }
    val content: @Composable () -> Unit = {
        Column(modifier, verticalArrangement = Arrangement.spacedBy(10.dp)) {
            blocks.forEach { block ->
                MarkdownBlockView(block, color)
            }
        }
    }
    if (selectable) SelectionContainer { content() } else content()
}

@Composable
private fun MarkdownBlockView(block: MdBlock, textColor: Color) {
    val inlineCodeBg = aide.fill2
    val linkColor = aide.blue
    when (block) {
        is MdBlock.Heading -> {
            val style = when (block.level) {
                1 -> TextStyle(fontSize = 21.sp, lineHeight = 29.sp, fontWeight = FontWeight.Bold)
                2 -> TextStyle(fontSize = 18.sp, lineHeight = 26.sp, fontWeight = FontWeight.Bold)
                3 -> TextStyle(fontSize = 16.5.sp, lineHeight = 24.sp, fontWeight = FontWeight.SemiBold)
                else -> TextStyle(fontSize = 15.5.sp, lineHeight = 22.sp, fontWeight = FontWeight.SemiBold)
            }
            Column(Modifier.padding(top = if (block.level <= 2) 6.dp else 4.dp)) {
                Text(
                    text = buildInlineAnnotated(block.text, textColor, inlineCodeBg, linkColor, baseBold = true),
                    style = style,
                    color = textColor,
                )
                if (block.level == 1) {
                    Spacer(Modifier.height(6.dp))
                    Hairline()
                }
            }
        }
        is MdBlock.Paragraph -> {
            Text(
                text = buildInlineAnnotated(block.text, textColor, inlineCodeBg, linkColor),
                style = TextStyle(fontSize = 16.sp, lineHeight = 25.sp),
                color = textColor,
            )
        }
        is MdBlock.CodeBlock -> MarkdownCodeBlock(block.language, block.code)
        is MdBlock.BlockQuote -> {
            val shape = RoundedCornerShape(topEnd = Radius.Md, bottomEnd = Radius.Md)
            Row(
                Modifier
                    .fillMaxWidth()
                    .height(IntrinsicSize.Min)
                    .clip(shape)
                    .background(aide.fill),
            ) {
                Box(Modifier.width(3.dp).fillMaxHeight().background(aide.accent))
                Column(
                    Modifier.weight(1f).padding(horizontal = 12.dp, vertical = 8.dp),
                    verticalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    val muted = MaterialTheme.colorScheme.onSurfaceVariant
                    block.blocks.forEach { child -> MarkdownBlockView(child, muted) }
                }
            }
        }
        is MdBlock.ListBlock -> {
            Column(verticalArrangement = Arrangement.spacedBy(5.dp)) {
                block.items.forEachIndexed { idx, item ->
                    val indent = (item.level * 16).dp
                    Row(
                        Modifier.fillMaxWidth().padding(start = indent),
                        verticalAlignment = Alignment.Top,
                    ) {
                        if (item.checked != null) {
                            Box(
                                Modifier
                                    .padding(top = 4.dp, end = 8.dp)
                                    .size(16.dp)
                                    .clip(RoundedCornerShape(4.dp))
                                    .then(
                                        if (item.checked) Modifier.background(aide.accent)
                                        else Modifier.border(1.5.dp, aide.controlStrong, RoundedCornerShape(4.dp)),
                                    ),
                                contentAlignment = Alignment.Center,
                            ) {
                                if (item.checked) Icon(CheckMark, null, Modifier.size(11.dp), tint = Color.White)
                            }
                        } else {
                            val marker = if (item.number != null) "${item.number}." else when (item.level) {
                                0 -> "•"
                                1 -> "◦"
                                else -> "▪"
                            }
                            Text(
                                text = marker,
                                style = TextStyle(fontSize = 15.sp, lineHeight = 25.sp, fontWeight = FontWeight.SemiBold),
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                modifier = Modifier.widthIn(min = 20.dp).padding(end = 6.dp),
                            )
                        }
                        Text(
                            text = buildInlineAnnotated(item.text, textColor, inlineCodeBg, linkColor),
                            style = TextStyle(fontSize = 16.sp, lineHeight = 25.sp),
                            color = textColor,
                            modifier = Modifier.weight(1f),
                        )
                    }
                }
            }
        }
        is MdBlock.Table -> MarkdownTableView(block, textColor, inlineCodeBg, linkColor)
        MdBlock.Rule -> Hairline(Modifier.padding(vertical = 4.dp))
    }
}

@Composable
private fun MarkdownCodeBlock(language: String, code: String) {
    val context = LocalContext.current
    var copied by remember(code) { mutableStateOf(false) }
    LaunchedEffect(copied) {
        if (copied) {
            delay(1500)
            copied = false
        }
    }
    val shape = RoundedCornerShape(Radius.Md)
    Column(
        Modifier
            .fillMaxWidth()
            .clip(shape)
            .background(aide.inset)
            .border(1.dp, aide.controlBorder, shape),
    ) {
        Row(
            Modifier
                .fillMaxWidth()
                .background(aide.fill)
                .padding(start = 12.dp, end = 6.dp, top = 5.dp, bottom = 5.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = language.ifEmpty { "code" },
                style = TextStyle(fontSize = 12.sp, fontFamily = FontFamily.Monospace, fontWeight = FontWeight.Medium),
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.weight(1f),
            )
            Text(
                text = if (copied) t("Copied", "已复制") else t("Copy", "复制"),
                style = TextStyle(fontSize = 12.sp, fontWeight = FontWeight.SemiBold),
                color = if (copied) aide.greenText else aide.blue,
                modifier = Modifier
                    .clip(RoundedCornerShape(Radius.Sm))
                    .clickable {
                        val cm = context.getSystemService(Context.CLIPBOARD_SERVICE) as? ClipboardManager
                        cm?.setPrimaryClip(ClipData.newPlainText("code", code))
                        copied = true
                    }
                    .padding(horizontal = 8.dp, vertical = 3.dp),
            )
        }
        Hairline()
        Box(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 12.dp, vertical = 10.dp)) {
            Text(
                text = code,
                style = TextStyle(fontSize = 13.5.sp, lineHeight = 20.sp, fontFamily = FontFamily.Monospace),
                color = MaterialTheme.colorScheme.onSurface,
            )
        }
    }
}

/**
 * Horizontally scrollable GFM table with aligned columns, distinct header background,
 * column dividers, and subtle zebra striping (工单 #132 补充).
 */
@Composable
private fun MarkdownTableView(
    table: MdBlock.Table,
    textColor: Color,
    inlineCodeBg: Color,
    linkColor: Color,
) {
    val cols = table.headers.size
    if (cols == 0) return
    val shape = RoundedCornerShape(Radius.Md)
    val allRows = remember(table) { listOf(table.headers) + table.rows }
    val headerBg = aide.fill2
    val stripeBg = aide.fill.copy(alpha = 0.45f)
    val sepColor = aide.separator
    val borderColor = aide.controlBorder

    Box(
        Modifier
            .fillMaxWidth()
            .clip(shape)
            .background(aide.card)
            .border(1.dp, borderColor, shape)
            .horizontalScroll(rememberScrollState()),
    ) {
        val totalRows = allRows.size
        Layout(
            content = {
                allRows.forEachIndexed { r, row ->
                    val isHeader = r == 0
                    val rowBg = when {
                        isHeader -> headerBg
                        r % 2 == 0 -> stripeBg
                        else -> Color.Transparent
                    }
                    row.forEachIndexed { c, cell ->
                        val align = when (table.alignments.getOrElse(c) { MdAlign.Left }) {
                            MdAlign.Center -> TextAlign.Center
                            MdAlign.Right -> TextAlign.End
                            MdAlign.Left -> TextAlign.Start
                        }
                        val boxAlign = when (table.alignments.getOrElse(c) { MdAlign.Left }) {
                            MdAlign.Center -> Alignment.TopCenter
                            MdAlign.Right -> Alignment.TopEnd
                            MdAlign.Left -> Alignment.TopStart
                        }
                        Box(
                            Modifier
                                .background(rowBg)
                                .padding(horizontal = 12.dp, vertical = 8.dp),
                            contentAlignment = boxAlign,
                        ) {
                            Text(
                                text = buildInlineAnnotated(cell, textColor, inlineCodeBg, linkColor, baseBold = isHeader),
                                style = TextStyle(
                                    fontSize = 13.5.sp,
                                    lineHeight = 20.sp,
                                    fontWeight = if (isHeader) FontWeight.Bold else FontWeight.Normal,
                                    textAlign = align,
                                ),
                                color = textColor,
                            )
                        }
                    }
                }
                // Horizontal row dividers (totalRows - 1)
                repeat((totalRows - 1).coerceAtLeast(0)) { r ->
                    Box(Modifier.background(if (r == 0) borderColor else sepColor))
                }
                // Vertical column dividers (cols - 1)
                repeat((cols - 1).coerceAtLeast(0)) {
                    Box(Modifier.background(sepColor))
                }
            },
        ) { measurables, constraints ->
            val cellCount = totalRows * cols
            val hDivCount = (totalRows - 1).coerceAtLeast(0)
            val minColPx = 72.dp.roundToPx()
            val maxColPx = 220.dp.roundToPx()
            val strokePx = 1.dp.roundToPx().coerceAtLeast(1)
            val headerStrokePx = 1.5.dp.roundToPx().coerceAtLeast(1)

            // Pass 1: measure natural width of every cell up to maxColPx to determine each column's width.
            val colWidths = IntArray(cols) { minColPx }
            for (r in 0 until totalRows) {
                for (c in 0 until cols) {
                    val m = measurables[r * cols + c]
                    val natural = m.maxIntrinsicWidth(Constraints.Infinity).coerceIn(minColPx, maxColPx)
                    if (natural > colWidths[c]) colWidths[c] = natural
                }
            }
            val totalWidth = colWidths.sum()

            // Pass 2: measure every cell constrained to its exact column width, and compute each row height.
            val rowHeights = IntArray(totalRows)
            val cellPlaceables = Array(cellCount) { idx ->
                val c = idx % cols
                val p = measurables[idx].measure(Constraints(minWidth = colWidths[c], maxWidth = colWidths[c]))
                val r = idx / cols
                if (p.height > rowHeights[r]) rowHeights[r] = p.height
                p
            }
            // Pass 3: re-measure cells shorter than their row so backgrounds fill full row height cleanly.
            for (r in 0 until totalRows) {
                val rh = rowHeights[r]
                for (c in 0 until cols) {
                    val idx = r * cols + c
                    if (cellPlaceables[idx].height < rh) {
                        cellPlaceables[idx] = measurables[idx].measure(Constraints.fixed(colWidths[c], rh))
                    }
                }
            }
            val totalHeight = rowHeights.sum()

            val hDivs = List(hDivCount) { r ->
                val h = if (r == 0) headerStrokePx else strokePx
                measurables[cellCount + r].measure(Constraints.fixed(totalWidth, h))
            }
            val vDivs = List((cols - 1).coerceAtLeast(0)) { c ->
                measurables[cellCount + hDivCount + c].measure(Constraints.fixed(strokePx, totalHeight))
            }

            layout(totalWidth, totalHeight) {
                var y = 0
                for (r in 0 until totalRows) {
                    var x = 0
                    for (c in 0 until cols) {
                        cellPlaceables[r * cols + c].placeRelative(x, y)
                        x += colWidths[c]
                    }
                    y += rowHeights[r]
                    if (r < hDivs.size) {
                        hDivs[r].placeRelative(0, y - hDivs[r].height)
                    }
                }
                var vx = 0
                for (c in 0 until vDivs.size) {
                    vx += colWidths[c]
                    vDivs[c].placeRelative(vx, 0)
                }
            }
        }
    }
}

private fun buildInlineAnnotated(
    text: String,
    textColor: Color,
    inlineCodeBg: Color,
    linkColor: Color,
    baseBold: Boolean = false,
): AnnotatedString = buildAnnotatedString {
    appendInlineNodes(parseInlineMarkdown(text), textColor, inlineCodeBg, linkColor, baseBold)
}

private fun AnnotatedString.Builder.appendInlineNodes(
    nodes: List<MdInline>,
    textColor: Color,
    inlineCodeBg: Color,
    linkColor: Color,
    baseBold: Boolean,
) {
    for (node in nodes) {
        when (node) {
            is MdInline.Text -> append(node.text)
            is MdInline.Bold -> withStyle(SpanStyle(fontWeight = FontWeight.Bold, color = textColor)) {
                appendInlineNodes(node.children, textColor, inlineCodeBg, linkColor, true)
            }
            is MdInline.Italic -> withStyle(SpanStyle(fontStyle = FontStyle.Italic)) {
                appendInlineNodes(node.children, textColor, inlineCodeBg, linkColor, baseBold)
            }
            is MdInline.Strike -> withStyle(SpanStyle(textDecoration = TextDecoration.LineThrough)) {
                appendInlineNodes(node.children, textColor, inlineCodeBg, linkColor, baseBold)
            }
            is MdInline.Code -> withStyle(
                SpanStyle(
                    fontFamily = FontFamily.Monospace,
                    fontSize = 14.sp,
                    background = inlineCodeBg,
                    fontWeight = if (baseBold) FontWeight.SemiBold else FontWeight.Normal,
                    color = textColor,
                ),
            ) {
                append("\u2009${node.code}\u2009")
            }
            is MdInline.Link -> withLink(
                LinkAnnotation.Url(
                    url = node.url,
                    styles = TextLinkStyles(
                        style = SpanStyle(color = linkColor, textDecoration = TextDecoration.Underline),
                    ),
                ),
            ) {
                append(node.label)
            }
        }
    }
}
