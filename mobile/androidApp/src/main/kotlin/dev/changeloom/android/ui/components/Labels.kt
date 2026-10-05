package dev.changeloom.android.ui.components

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.animation.expandHorizontally
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.shrinkHorizontally
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.Article
import androidx.compose.material.icons.rounded.Bolt
import androidx.compose.material.icons.rounded.Campaign
import androidx.compose.material.icons.rounded.Check
import androidx.compose.material.icons.rounded.Gavel
import androidx.compose.material.icons.rounded.LocalOffer
import androidx.compose.material.icons.rounded.RemoveCircleOutline
import androidx.compose.material.icons.rounded.RocketLaunch
import androidx.compose.material.icons.rounded.Science
import androidx.compose.material.icons.rounded.Shield
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import dev.changeloom.android.R
import dev.changeloom.android.ui.theme.ChangeloomTheme
import dev.changeloom.android.ui.theme.Durations
import dev.changeloom.android.ui.theme.Radius
import dev.changeloom.android.ui.theme.expoTween
import dev.changeloom.android.ui.theme.kindColor
import dev.changeloom.android.ui.theme.severityColor

fun kindIcon(kind: String): ImageVector = when (kind) {
    "release" -> Icons.Rounded.RocketLaunch
    "breaking" -> Icons.Rounded.Bolt
    "security" -> Icons.Rounded.Shield
    "deprecation" -> Icons.Rounded.RemoveCircleOutline
    "announcement" -> Icons.Rounded.Campaign
    "research" -> Icons.Rounded.Science
    "policy" -> Icons.Rounded.Gavel
    "deal" -> Icons.Rounded.LocalOffer
    else -> Icons.AutoMirrored.Rounded.Article
}

/** Tinted pill naming a story's kind (`release`, `security`, ...). */
@Composable
fun KindPill(kind: String, modifier: Modifier = Modifier) {
    val color = kindColor(kind)
    Row(
        modifier
            .clip(Radius.pill)
            .background(color.copy(alpha = 0.12f))
            .border(1.dp, color.copy(alpha = 0.28f), Radius.pill)
            .padding(start = 7.dp, end = 9.dp, top = 3.dp, bottom = 3.dp),
        horizontalArrangement = Arrangement.spacedBy(5.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(kindIcon(kind), contentDescription = null, tint = color, modifier = Modifier.size(12.dp))
        Eyebrow(kind, color = color)
    }
}

/** Severity dot; critical pulses like Tailwind's `animate-ping`. Draws nothing when [severity] is null. */
@Composable
fun SeverityDot(severity: String?, modifier: Modifier = Modifier, dotSize: Dp = 8.dp) {
    if (severity == null) return
    val color = severityColor(severity)
    // Kept as State and read only while drawing, so the pulse redraws the dot without recomposing the card.
    val pulse = if (severity == "critical") {
        val t = rememberInfiniteTransition(label = "ping")
        t.animateFloat(0f, 1f, infiniteRepeatable(tween(1000, easing = LinearEasing), RepeatMode.Restart), label = "pingProgress")
    } else {
        null
    }
    val description = stringResource(R.string.severity_label, severity)
    Canvas(modifier.size(dotSize * 2.4f).semantics { contentDescription = description }) {
        val r = dotSize.toPx() / 2
        pulse?.value?.let { p -> drawCircle(color.copy(alpha = 0.6f * (1 - p)), radius = r * (1 + 1.4f * p)) }
        drawCircle(color, radius = r)
    }
}

/** The top of the api's 1–5 importance scale. */
const val MAX_IMPORTANCE = 5

/** Five rising bars; the first [importance] are lit with the brand gradient. */
@Composable
fun ImportanceMeter(importance: Int, modifier: Modifier = Modifier) {
    val c = ChangeloomTheme.colors
    val brand = ChangeloomTheme.gradients.brandHorizontal
    val description = stringResource(R.string.importance_label, importance, MAX_IMPORTANCE)
    Row(
        modifier.semantics { contentDescription = description },
        horizontalArrangement = Arrangement.spacedBy(2.dp),
        verticalAlignment = Alignment.Bottom,
    ) {
        for (i in 1..MAX_IMPORTANCE) {
            Box(
                Modifier
                    .width(3.dp)
                    .height((4 + i * 2).dp)
                    .clip(Radius.pill)
                    .background(if (i <= importance) brand else SolidColor(c.lineStrong)),
            )
        }
    }
}

/** Topic pill. Selectable chips animate their fill and slide a check mark in. */
@Composable
fun TopicChip(
    label: String,
    modifier: Modifier = Modifier,
    selected: Boolean = false,
    compact: Boolean = false,
    onClick: (() -> Unit)? = null,
) {
    val c = ChangeloomTheme.colors
    val bg by animateColorAsState(if (selected) c.primary.copy(alpha = 0.16f) else c.surface2, expoTween(Durations.MEDIUM), label = "chipBg")
    val border by animateColorAsState(if (selected) c.primary.copy(alpha = 0.55f) else c.line, expoTween(Durations.MEDIUM), label = "chipBorder")
    val fg by animateColorAsState(if (selected) c.primaryText else c.fgMuted, expoTween(Durations.MEDIUM), label = "chipFg")
    var m = modifier.clip(Radius.pill).background(bg).border(1.dp, border, Radius.pill)
    if (onClick != null) m = m.clickable(role = Role.Checkbox, onClick = onClick)
    Row(
        m.padding(horizontal = if (compact) 9.dp else 12.dp, vertical = if (compact) 4.dp else 7.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        AnimatedVisibility(selected, enter = fadeIn() + expandHorizontally(), exit = fadeOut() + shrinkHorizontally()) {
            Icon(Icons.Rounded.Check, contentDescription = null, tint = fg, modifier = Modifier.padding(end = 5.dp).size(14.dp))
        }
        Text(label, style = if (compact) MaterialTheme.typography.labelSmall else MaterialTheme.typography.labelMedium, color = fg, maxLines = 1)
    }
}
