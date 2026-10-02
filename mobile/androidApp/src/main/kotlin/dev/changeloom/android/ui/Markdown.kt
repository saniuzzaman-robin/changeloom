package dev.changeloom.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
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
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.LinkAnnotation
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.TextLinkStyles
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.withLink
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import dev.changeloom.android.ui.theme.ChangeloomTextStyles
import dev.changeloom.android.ui.theme.ChangeloomTheme
import dev.changeloom.android.ui.theme.MonoFamily
import dev.changeloom.android.ui.theme.Radius
import dev.changeloom.android.ui.theme.ThemeMode

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

/** Span colours for [inlineMarkdown]. */
internal data class InlineStyles(val strong: SpanStyle, val code: SpanStyle, val link: SpanStyle)

/** Bold, italic, inline code and links. Only http(s) links become tappable. */
internal fun inlineMarkdown(text: String, styles: InlineStyles): AnnotatedString = buildAnnotatedString {
    var pos = 0
    for (m in inlineRe.findAll(text)) {
        append(text.substring(pos, m.range.first))
        val (bold, code, label, url, italic) = m.destructured
        when {
            bold.isNotEmpty() -> withStyle(styles.strong) { append(bold) }
            code.isNotEmpty() -> withStyle(styles.code) { append(code) }
            label.isNotEmpty() && url.startsWith("http") -> withLink(LinkAnnotation.Url(url, TextLinkStyles(styles.link))) { append(label) }
            label.isNotEmpty() -> append(label)
            else -> withStyle(SpanStyle(fontStyle = FontStyle.Italic)) { append(italic) }
        }
        pos = m.range.last + 1
    }
    append(text.substring(pos))
}

/** Story body in the site's prose style: muted copy, bright headings and emphasis, mono code on `surface-2`. */
@Composable
fun MarkdownText(markdown: String, modifier: Modifier = Modifier) {
    val c = ChangeloomTheme.colors
    val type = MaterialTheme.typography
    val blocks = remember(markdown) { parseMarkdown(markdown) }
    val styles = InlineStyles(
        strong = SpanStyle(fontWeight = FontWeight.SemiBold, color = c.fg),
        code = SpanStyle(fontFamily = MonoFamily, color = c.primaryText, background = c.primary.copy(alpha = 0.10f)),
        link = SpanStyle(color = c.primaryText, textDecoration = TextDecoration.Underline),
    )
    Column(modifier, verticalArrangement = Arrangement.spacedBy(12.dp)) {
        blocks.forEachIndexed { i, block ->
            when (block) {
                is MdBlock.Heading -> Text(
                    inlineMarkdown(block.text, styles),
                    // Headings get extra air above, except at the very top.
                    Modifier.padding(top = if (i == 0) 0.dp else 10.dp),
                    style = when (block.level) {
                        1 -> type.headlineSmall
                        2 -> type.titleLarge
                        else -> type.titleMedium
                    },
                    color = c.fg,
                )
                is MdBlock.Paragraph -> Text(inlineMarkdown(block.text, styles), style = type.bodyLarge, color = c.fgMuted)
                is MdBlock.Bullet -> Row(Modifier.padding(start = 4.dp)) {
                    Box(Modifier.widthIn(min = 20.dp).height(26.dp), contentAlignment = Alignment.CenterStart) {
                        if (block.marker == "•") {
                            Box(Modifier.size(6.dp).clip(Radius.pill).background(ChangeloomTheme.gradients.brand))
                        } else {
                            Text(block.marker, style = ChangeloomTextStyles.code, color = c.primaryText)
                        }
                    }
                    Spacer(Modifier.width(6.dp))
                    Text(inlineMarkdown(block.text, styles), style = type.bodyLarge, color = c.fgMuted)
                }
                is MdBlock.Quote -> Row(Modifier.height(IntrinsicSize.Min)) {
                    Box(Modifier.width(3.dp).padding(vertical = 2.dp).fillMaxHeight().clip(Radius.pill).background(ChangeloomTheme.gradients.brand))
                    Text(
                        inlineMarkdown(block.text, styles),
                        Modifier.padding(start = 14.dp),
                        style = type.bodyLarge.copy(fontStyle = FontStyle.Italic),
                        color = c.fg,
                    )
                }
                is MdBlock.Code -> Text(
                    block.text,
                    style = ChangeloomTextStyles.code,
                    color = c.fg,
                    softWrap = false,
                    modifier = Modifier
                        .fillMaxWidth()
                        .clip(Radius.lg)
                        .background(c.surface2)
                        .border(1.dp, c.line, Radius.lg)
                        .horizontalScroll(rememberScrollState())
                        .padding(horizontal = 14.dp, vertical = 12.dp),
                )
            }
        }
    }
}

internal const val PREVIEW_MARKDOWN = """## What changed
Context parameters are **stable**, and `-Xcontext-parameters` is no longer needed. See the [release notes](https://kotlinlang.org).

- Faster incremental builds with K2
- New *experimental* name-based destructuring
1. Bump the Kotlin plugin
2. Remove the compiler flag

> Context receivers are removed; migrate before upgrading.

```
kotlin("jvm") version "2.4.0"
```"""

@Preview(name = "Markdown, dark")
@Composable
private fun MarkdownDark() = ChangeloomTheme(ThemeMode.Dark) {
    MarkdownText(PREVIEW_MARKDOWN, Modifier.background(ChangeloomTheme.colors.bg).padding(20.dp))
}

@Preview(name = "Markdown, light")
@Composable
private fun MarkdownLight() = ChangeloomTheme(ThemeMode.Light) {
    MarkdownText(PREVIEW_MARKDOWN, Modifier.background(ChangeloomTheme.colors.bg).padding(20.dp))
}
