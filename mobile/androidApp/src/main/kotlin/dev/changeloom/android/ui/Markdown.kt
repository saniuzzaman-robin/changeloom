package dev.changeloom.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.horizontalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.LinkAnnotation
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.TextLinkStyles
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.withLink
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp

internal sealed interface MdBlock {
    data class Heading(val level: Int, val text: String) : MdBlock
    data class Paragraph(val text: String) : MdBlock
    data class Bullet(val text: String, val marker: String) : MdBlock
    data class Code(val text: String) : MdBlock
    data class Quote(val text: String) : MdBlock
}

private val headingRe = Regex("^(#{1,6})\\s+(.*)$")
private val bulletRe = Regex("^\\s*([-*+]|\\d+\\.)\\s+(.*)$")

/** Small Markdown subset: headings, paragraphs, lists, block quotes and fenced code. Enough for AI-written summaries. */
internal fun parseMarkdown(md: String): List<MdBlock> {
    val blocks = mutableListOf<MdBlock>()
    val para = StringBuilder()
    fun flush() {
        if (para.isNotEmpty()) blocks += MdBlock.Paragraph(para.toString().trim()).also { para.clear() }
    }
    val lines = md.lines()
    var i = 0
    while (i < lines.size) {
        val line = lines[i]
        when {
            line.trimStart().startsWith("```") -> {
                flush()
                val code = mutableListOf<String>()
                i++
                while (i < lines.size && !lines[i].trimStart().startsWith("```")) code += lines[i++]
                blocks += MdBlock.Code(code.joinToString("\n"))
            }
            line.isBlank() -> flush()
            headingRe.matches(line) -> {
                flush()
                val m = headingRe.matchEntire(line)!!
                blocks += MdBlock.Heading(m.groupValues[1].length, m.groupValues[2].trim())
            }
            bulletRe.matches(line) -> {
                flush()
                val m = bulletRe.matchEntire(line)!!
                val marker = m.groupValues[1].let { if (it[0].isDigit()) it else "•" }
                blocks += MdBlock.Bullet(m.groupValues[2].trim(), marker)
            }
            line.trimStart().startsWith(">") -> {
                flush()
                blocks += MdBlock.Quote(line.trimStart().removePrefix(">").trim())
            }
            else -> {
                if (para.isNotEmpty()) para.append(' ')
                para.append(line.trim())
            }
        }
        i++
    }
    flush()
    return blocks
}

private val inlineRe = Regex("""\*\*(.+?)\*\*|`([^`]+)`|\[([^\]]+)]\(([^)\s]+)\)|(?<![*\w])\*([^*\n]+?)\*(?!\w)""")

/** Bold, italic, inline code and links. Only http(s) links become tappable. */
internal fun inlineMarkdown(text: String, linkColor: androidx.compose.ui.graphics.Color): AnnotatedString = buildAnnotatedString {
    var pos = 0
    for (m in inlineRe.findAll(text)) {
        append(text.substring(pos, m.range.first))
        val (bold, code, label, url, italic) = m.destructured
        when {
            bold.isNotEmpty() -> withStyle(SpanStyle(fontWeight = FontWeight.Bold)) { append(bold) }
            code.isNotEmpty() -> withStyle(SpanStyle(fontFamily = FontFamily.Monospace)) { append(code) }
            label.isNotEmpty() && url.startsWith("http") -> withLink(
                LinkAnnotation.Url(url, TextLinkStyles(SpanStyle(color = linkColor, textDecoration = TextDecoration.Underline))),
            ) { append(label) }
            label.isNotEmpty() -> append(label)
            else -> withStyle(SpanStyle(fontStyle = FontStyle.Italic)) { append(italic) }
        }
        pos = m.range.last + 1
    }
    append(text.substring(pos))
}

@Composable
fun MarkdownText(markdown: String, modifier: Modifier = Modifier) {
    val link = MaterialTheme.colorScheme.primary
    val type = MaterialTheme.typography
    Column(modifier, verticalArrangement = androidx.compose.foundation.layout.Arrangement.spacedBy(10.dp)) {
        for (block in parseMarkdown(markdown)) {
            when (block) {
                is MdBlock.Heading -> Text(
                    inlineMarkdown(block.text, link),
                    style = when (block.level) {
                        1 -> type.headlineSmall
                        2 -> type.titleLarge
                        else -> type.titleMedium
                    },
                )
                is MdBlock.Paragraph -> Text(inlineMarkdown(block.text, link), style = type.bodyLarge)
                is MdBlock.Bullet -> Row(Modifier.padding(start = 8.dp)) {
                    Text(block.marker + " ", style = type.bodyLarge)
                    Text(inlineMarkdown(block.text, link), style = type.bodyLarge)
                }
                is MdBlock.Quote -> Text(
                    inlineMarkdown(block.text, link),
                    style = type.bodyLarge.copy(fontStyle = FontStyle.Italic),
                    modifier = Modifier.padding(start = 12.dp),
                )
                is MdBlock.Code -> Text(
                    block.text,
                    style = type.bodySmall.copy(fontFamily = FontFamily.Monospace),
                    modifier = Modifier.fillMaxWidth()
                        .background(MaterialTheme.colorScheme.surfaceVariant)
                        .horizontalScroll(rememberScrollState())
                        .padding(8.dp),
                )
            }
        }
    }
}
