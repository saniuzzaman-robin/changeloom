package dev.changeloom.android.ui.components

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.animation.expandVertically
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.shrinkVertically
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.compositeOver
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import dev.changeloom.android.ui.theme.ChangeloomTheme
import dev.changeloom.android.ui.theme.Radius

/** Moving highlight for loading placeholders. */
@Composable
fun Modifier.shimmer(): Modifier {
    val c = ChangeloomTheme.colors
    val base = c.surface2
    val highlight = if (c.isDark) Color.White.copy(alpha = 0.06f).compositeOver(base) else Color.White
    val progress by rememberInfiniteTransition(label = "shimmer")
        .animateFloat(-1f, 2f, infiniteRepeatable(tween(1400, easing = LinearEasing)), label = "shimmerX")
    return drawBehind {
        val w = size.width
        drawRect(
            Brush.linearGradient(
                listOf(base, highlight, base),
                start = Offset(w * progress - w / 2, 0f),
                end = Offset(w * progress + w / 2, size.height),
            ),
        )
    }
}

/** A rounded shimmering placeholder bar. */
@Composable
fun SkeletonBlock(modifier: Modifier = Modifier, height: Dp = 14.dp) {
    Box(modifier.height(height).clip(Radius.sm).shimmer())
}

@Composable
fun EmptyState(
    title: String,
    message: String,
    modifier: Modifier = Modifier,
    art: @Composable () -> Unit = { LoomMark(Modifier.size(72.dp)) },
    action: (@Composable () -> Unit)? = null,
) {
    val c = ChangeloomTheme.colors
    Column(
        modifier.fillMaxWidth().padding(horizontal = 32.dp, vertical = 48.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        art()
        Spacer(Modifier.height(20.dp))
        Text(title, style = MaterialTheme.typography.titleLarge, color = c.fg, textAlign = TextAlign.Center)
        Spacer(Modifier.height(8.dp))
        Text(message, style = MaterialTheme.typography.bodyMedium, color = c.fgMuted, textAlign = TextAlign.Center)
        if (action != null) {
            Spacer(Modifier.height(24.dp))
            action()
        }
    }
}

enum class BannerTone { Info, Warning, Error }

/** Inline status strip (offline, errors) that expands in and out as [message] appears and clears. */
@Composable
fun StatusBanner(
    message: String?,
    icon: ImageVector,
    modifier: Modifier = Modifier,
    tone: BannerTone = BannerTone.Info,
    actionLabel: String? = null,
    onAction: () -> Unit = {},
) {
    val c = ChangeloomTheme.colors
    val color = when (tone) {
        BannerTone.Info -> c.primaryText
        BannerTone.Warning -> c.amber
        BannerTone.Error -> c.rose
    }
    // Keep the last text visible while the banner animates out.
    val shown = remember { LastMessage() }
    if (message != null) shown.text = message
    AnimatedVisibility(message != null, modifier, enter = fadeIn() + expandVertically(), exit = fadeOut() + shrinkVertically()) {
        Row(
            Modifier
                .fillMaxWidth()
                .clip(Radius.lg)
                .background(color.copy(alpha = 0.10f))
                .border(1.dp, color.copy(alpha = 0.25f), Radius.lg)
                .padding(start = 12.dp, end = 4.dp, top = 4.dp, bottom = 4.dp),
            horizontalArrangement = Arrangement.spacedBy(10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Icon(icon, contentDescription = null, tint = color, modifier = Modifier.size(18.dp))
            Text(
                shown.text,
                style = MaterialTheme.typography.bodySmall,
                color = c.fg,
                modifier = Modifier.weight(1f).padding(vertical = 8.dp),
            )
            if (actionLabel != null) {
                TextButton(onClick = onAction) { Text(actionLabel, color = color, style = MaterialTheme.typography.labelMedium) }
            }
        }
    }
}

private class LastMessage(var text: String = "")
