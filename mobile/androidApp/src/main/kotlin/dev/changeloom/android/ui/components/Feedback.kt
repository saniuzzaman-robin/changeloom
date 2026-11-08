package dev.changeloom.android.ui.components

import androidx.compose.animation.AnimatedVisibility
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
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawWithCache
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.compositeOver
import androidx.compose.ui.graphics.drawscope.translate
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import dev.changeloom.android.ui.theme.ChangeloomTheme
import dev.changeloom.android.ui.theme.LocalShimmerClock
import dev.changeloom.android.ui.theme.Radius

/** Moving highlight for loading placeholders, on the theme's shared clock ([LocalShimmerClock]). */
@Composable
fun Modifier.shimmer(): Modifier {
    val c = ChangeloomTheme.colors
    val base = c.surface2
    val highlight = if (c.isDark) Color.White.copy(alpha = 0.06f).compositeOver(base) else Color.White
    val clock = LocalShimmerClock.current
    DisposableEffect(clock) {
        clock.attach()
        onDispose { clock.detach() }
    }
    return drawWithCache {
        val w = size.width
        val brush = Brush.linearGradient(listOf(base, highlight, base), start = Offset(-w / 2, 0f), end = Offset(w / 2, size.height))
        onDrawBehind {
            // Slide the gradient, not the rect: the shader moves with the canvas while the rect stays in place.
            val dx = w * clock.progress
            translate(left = dx) { drawRect(brush, topLeft = Offset(-dx, 0f), size = size) }
        }
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
                .clip(Radius.xl)
                .background(color.copy(alpha = 0.10f))
                .border(1.dp, color.copy(alpha = 0.25f), Radius.xl)
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
            if (actionLabel != null) TextAction(actionLabel, onAction, color = color)
        }
    }
}

private class LastMessage(var text: String = "")
