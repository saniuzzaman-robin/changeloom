package dev.changeloom.android.ui.components

import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsPressedAsState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import dev.changeloom.android.ui.theme.ChangeloomTheme
import dev.changeloom.android.ui.theme.Durations
import dev.changeloom.android.ui.theme.Radius
import dev.changeloom.android.ui.theme.expoTween

/** Scale-down on press, shared by buttons and tappable cards. */
@Composable
internal fun Modifier.pressScale(interaction: MutableInteractionSource, pressed: Float = 0.97f): Modifier {
    val isPressed by interaction.collectIsPressedAsState()
    val scale by animateFloatAsState(if (isPressed) pressed else 1f, expoTween(Durations.FAST), label = "pressScale")
    return graphicsLayer { scaleX = scale; scaleY = scale }
}

/** Gradient call-to-action (`linear-gradient(135deg, primary-strong, primary)`) with a busy state. */
@Composable
fun PrimaryButton(
    text: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    loading: Boolean = false,
    icon: ImageVector? = null,
) {
    val c = ChangeloomTheme.colors
    val interaction = remember { MutableInteractionSource() }
    val active = enabled && !loading
    Box(
        modifier
            .pressScale(interaction)
            .graphicsLayer { alpha = if (enabled) 1f else 0.45f }
            .clip(Radius.lg)
            .background(ChangeloomTheme.gradients.button)
            .drawWithContent {
                drawContent()
                // Inset top highlight, like the site's `0 1px 0 0 #ffffff0a inset`.
                drawLine(Color.White.copy(alpha = 0.18f), Offset(0f, 0.5f), Offset(size.width, 0.5f), 1.dp.toPx())
            }
            .clickable(interaction, indication = null, enabled = active, role = Role.Button, onClick = onClick)
            .defaultMinSize(minHeight = 52.dp)
            .padding(horizontal = 20.dp, vertical = 14.dp),
        contentAlignment = Alignment.Center,
    ) {
        AnimatedContent(loading, transitionSpec = { fadeIn(expoTween()) togetherWith fadeOut(expoTween()) }, label = "primaryButton") { busy ->
            if (busy) {
                CircularProgressIndicator(Modifier.size(20.dp), color = c.primaryFg, strokeWidth = 2.dp)
            } else {
                ButtonContent(text, icon, c.primaryFg)
            }
        }
    }
}

/** Quiet bordered button on `surface-2`, for secondary actions. */
@Composable
fun SecondaryButton(
    text: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    loading: Boolean = false,
    icon: ImageVector? = null,
    contentColor: Color = ChangeloomTheme.colors.fg,
    /** Custom leading mark (e.g. a brand logo) for when an [icon] tint won't do. */
    leading: (@Composable () -> Unit)? = null,
) {
    val c = ChangeloomTheme.colors
    val interaction = remember { MutableInteractionSource() }
    Box(
        modifier
            .pressScale(interaction)
            .graphicsLayer { alpha = if (enabled) 1f else 0.45f }
            .clip(Radius.lg)
            .background(c.surface2)
            .border(1.dp, c.lineStrong, Radius.lg)
            .clickable(interaction, indication = null, enabled = enabled && !loading, role = Role.Button, onClick = onClick)
            .defaultMinSize(minHeight = 52.dp)
            .padding(horizontal = 20.dp, vertical = 14.dp),
        contentAlignment = Alignment.Center,
    ) {
        if (loading) {
            CircularProgressIndicator(Modifier.size(20.dp), color = contentColor, strokeWidth = 2.dp)
        } else {
            ButtonContent(text, icon, contentColor, leading)
        }
    }
}

@Composable
private fun ButtonContent(text: String, icon: ImageVector?, color: Color, leading: (@Composable () -> Unit)? = null) {
    Row(horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.CenterVertically) {
        if (icon != null) Icon(icon, contentDescription = null, tint = color, modifier = Modifier.size(20.dp)) else leading?.invoke()
        Text(text, style = MaterialTheme.typography.labelLarge, color = color)
    }
}

/** Gradient-filled square tile holding an icon (topic headers, empty states). */
@Composable
fun IconTile(
    icon: ImageVector,
    modifier: Modifier = Modifier,
    brush: Brush = ChangeloomTheme.gradients.brand,
    tint: Color = Color.White,
) {
    Box(modifier.size(44.dp).clip(Radius.lg).background(brush), contentAlignment = Alignment.Center) {
        Icon(icon, contentDescription = null, tint = tint, modifier = Modifier.size(22.dp))
    }
}
