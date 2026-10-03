package dev.changeloom.android.ui.components

import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.animateDpAsState
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
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import dev.changeloom.android.ui.theme.ChangeloomTheme
import dev.changeloom.android.ui.theme.Durations
import dev.changeloom.android.ui.theme.Radius
import dev.changeloom.android.ui.theme.expoTween

private val BUTTON_HEIGHT = 56.dp
private val DENSE_BUTTON_HEIGHT = 48.dp
private val PRIMARY_GLOW = 14.dp

/** Scale-down on press, shared by buttons and tappable cards. */
@Composable
internal fun Modifier.pressScale(interaction: MutableInteractionSource, pressed: Float = 0.97f): Modifier {
    val isPressed by interaction.collectIsPressedAsState()
    val scale by animateFloatAsState(if (isPressed) pressed else 1f, expoTween(Durations.FAST), label = "pressScale")
    return graphicsLayer { scaleX = scale; scaleY = scale }
}

/**
 * Gradient pill call-to-action with a soft glow in the primary colour and a busy state. Disabled, it settles
 * into a flat `surface-2` pill so it reads as unavailable rather than faded.
 */
@Composable
fun PrimaryButton(
    text: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    loading: Boolean = false,
    icon: ImageVector? = null,
    trailingIcon: ImageVector? = null,
    /** 48dp tall instead of 56dp, for screens that must fit without scrolling (still a full touch target). */
    dense: Boolean = false,
) {
    val c = ChangeloomTheme.colors
    val interaction = remember { MutableInteractionSource() }
    val active = enabled && !loading
    val fill by animateFloatAsState(if (enabled) 1f else 0f, expoTween(), label = "primaryFill")
    val glow by animateDpAsState(if (enabled) PRIMARY_GLOW else 0.dp, expoTween(), label = "primaryGlow")
    val fg by animateColorAsState(if (enabled) c.primaryFg else c.fgSubtle, expoTween(), label = "primaryFg")
    Box(
        modifier
            .pressScale(interaction)
            .shadow(glow, Radius.pill, ambientColor = c.primary, spotColor = c.primary)
            .clip(Radius.pill)
            .background(c.surface2)
            .background(ChangeloomTheme.gradients.button, alpha = fill)
            .drawWithContent {
                drawContent()
                // Inset top highlight, like the site's `0 1px 0 0 #ffffff0a inset`.
                drawLine(Color.White.copy(alpha = 0.18f * fill), Offset(0f, 0.5f), Offset(size.width, 0.5f), 1.dp.toPx())
            }
            .clickable(interaction, indication = null, enabled = active, role = Role.Button, onClick = onClick)
            .defaultMinSize(minHeight = if (dense) DENSE_BUTTON_HEIGHT else BUTTON_HEIGHT)
            .padding(horizontal = 24.dp, vertical = if (dense) 12.dp else 16.dp),
        contentAlignment = Alignment.Center,
    ) {
        AnimatedContent(loading, transitionSpec = { fadeIn(expoTween()) togetherWith fadeOut(expoTween()) }, label = "primaryButton") { busy ->
            if (busy) {
                CircularProgressIndicator(Modifier.size(20.dp), color = c.primaryFg, strokeWidth = 2.dp)
            } else {
                ButtonContent(text, icon, fg, trailingIcon = trailingIcon)
            }
        }
    }
}

/** Outlined pill for secondary actions; fills with `surface-2` while pressed. */
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
    /** 48dp tall instead of 56dp, for screens that must fit without scrolling (still a full touch target). */
    dense: Boolean = false,
) {
    val c = ChangeloomTheme.colors
    val interaction = remember { MutableInteractionSource() }
    val pressed by interaction.collectIsPressedAsState()
    val bg by animateColorAsState(if (pressed) c.surface2 else Color.Transparent, expoTween(Durations.FAST), label = "secondaryBg")
    Box(
        modifier
            .pressScale(interaction)
            .graphicsLayer { alpha = if (enabled) 1f else 0.45f }
            .clip(Radius.pill)
            .background(bg)
            .border(1.dp, c.lineStrong, Radius.pill)
            .clickable(interaction, indication = null, enabled = enabled && !loading, role = Role.Button, onClick = onClick)
            .defaultMinSize(minHeight = if (dense) DENSE_BUTTON_HEIGHT else BUTTON_HEIGHT)
            .padding(horizontal = 24.dp, vertical = if (dense) 12.dp else 16.dp),
        contentAlignment = Alignment.Center,
    ) {
        if (loading) {
            CircularProgressIndicator(Modifier.size(20.dp), color = contentColor, strokeWidth = 2.dp)
        } else {
            ButtonContent(text, icon, contentColor, leading)
        }
    }
}

/** Quiet text action (links, toolbar and dialog actions) with a tinted pill behind it while pressed. */
@Composable
fun TextAction(
    text: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    icon: ImageVector? = null,
    color: Color = ChangeloomTheme.colors.primaryText,
) {
    val interaction = remember { MutableInteractionSource() }
    val pressed by interaction.collectIsPressedAsState()
    val bg by animateColorAsState(if (pressed) color.copy(alpha = 0.12f) else Color.Transparent, expoTween(Durations.FAST), label = "textActionBg")
    Row(
        modifier
            .minimumInteractiveComponentSize()
            .graphicsLayer { alpha = if (enabled) 1f else 0.4f }
            .clip(Radius.pill)
            .background(bg)
            .clickable(interaction, indication = null, enabled = enabled, role = Role.Button, onClick = onClick)
            .padding(horizontal = 12.dp, vertical = 8.dp),
        horizontalArrangement = Arrangement.spacedBy(6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (icon != null) Icon(icon, contentDescription = null, tint = color, modifier = Modifier.size(16.dp))
        Text(text, style = MaterialTheme.typography.labelLarge, color = color)
    }
}

@Composable
private fun ButtonContent(
    text: String,
    icon: ImageVector?,
    color: Color,
    leading: (@Composable () -> Unit)? = null,
    trailingIcon: ImageVector? = null,
) {
    Row(horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.CenterVertically) {
        if (icon != null) Icon(icon, contentDescription = null, tint = color, modifier = Modifier.size(20.dp)) else leading?.invoke()
        Text(text, style = MaterialTheme.typography.labelLarge, color = color)
        if (trailingIcon != null) Icon(trailingIcon, contentDescription = null, tint = color, modifier = Modifier.size(18.dp))
    }
}

/** Gradient-filled square tile holding an icon (topic headers, empty states). */
@Composable
fun IconTile(
    icon: ImageVector,
    modifier: Modifier = Modifier,
    brush: Brush = ChangeloomTheme.gradients.brand,
    tint: Color = Color.White,
    size: Dp = 44.dp,
) {
    Box(modifier.size(size).clip(Radius.lg).background(brush), contentAlignment = Alignment.Center) {
        Icon(icon, contentDescription = null, tint = tint, modifier = Modifier.size(size / 2))
    }
}
