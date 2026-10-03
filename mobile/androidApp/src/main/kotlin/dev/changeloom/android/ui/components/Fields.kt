package dev.changeloom.android.ui.components

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsFocusedAsState
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Icon
import androidx.compose.material3.LocalContentColor
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.unit.dp
import dev.changeloom.android.ui.theme.ChangeloomTheme
import dev.changeloom.android.ui.theme.Durations
import dev.changeloom.android.ui.theme.Radius
import dev.changeloom.android.ui.theme.expoTween

private val FIELD_HEIGHT = 56.dp
private val DENSE_FIELD_HEIGHT = 48.dp

/** Matches [Radius.xl], which clips the field; the focus ring and halo are drawn with it. */
private val FIELD_CORNER = 16.dp
private val RING_WIDTH = 1.5.dp

/** Halo drawn around a focused field: outward spread in dp and alpha, outermost first. */
private val GLOW_LAYERS = listOf(6f to 0.06f, 3f to 0.12f)

/**
 * Single-line input: a `surface-2` well with an optional mono label above it. On focus, a brand-gradient ring
 * and a soft halo fade in; [isError] turns both the ring and [supportingText] rose.
 */
@Composable
fun ChangeloomTextField(
    value: String,
    onValueChange: (String) -> Unit,
    modifier: Modifier = Modifier,
    label: String? = null,
    placeholder: String? = null,
    leadingIcon: ImageVector? = null,
    /** Trailing actions, typically IconButtons; tinted `fg-subtle` unless they set their own colour. */
    trailing: (@Composable () -> Unit)? = null,
    supportingText: String? = null,
    isError: Boolean = false,
    enabled: Boolean = true,
    keyboardOptions: KeyboardOptions = KeyboardOptions.Default,
    keyboardActions: KeyboardActions = KeyboardActions.Default,
    visualTransformation: VisualTransformation = VisualTransformation.None,
    /** 48dp tall instead of 56dp, for screens that must fit without scrolling. */
    dense: Boolean = false,
) {
    val c = ChangeloomTheme.colors
    val brand = ChangeloomTheme.gradients.brandHorizontal
    val interaction = remember { MutableInteractionSource() }
    val focused by interaction.collectIsFocusedAsState()
    val focus by animateFloatAsState(if (focused) 1f else 0f, expoTween(Durations.SLOW), label = "fieldFocus")
    val accent = if (isError) c.rose else c.primaryText
    val iconTint by animateColorAsState(if (isError || focused) accent else c.fgSubtle, expoTween(), label = "fieldIcon")
    val halo = if (isError) c.rose else c.primary

    Column(modifier) {
        if (label != null) {
            Eyebrow(label, Modifier.padding(start = 4.dp, bottom = 8.dp), color = if (isError || focused) accent else c.fgSubtle)
        }
        BasicTextField(
            value = value,
            onValueChange = onValueChange,
            modifier = Modifier
                .fillMaxWidth()
                .semantics { (label ?: placeholder)?.let { contentDescription = it } }
                .drawBehind {
                    if (focus == 0f) return@drawBehind
                    for ((spread, alpha) in GLOW_LAYERS) {
                        val s = spread.dp.toPx()
                        drawRoundRect(
                            halo.copy(alpha = alpha * focus),
                            topLeft = Offset(-s, -s),
                            size = Size(size.width + 2 * s, size.height + 2 * s),
                            cornerRadius = CornerRadius(FIELD_CORNER.toPx() + s),
                        )
                    }
                },
            enabled = enabled,
            singleLine = true,
            textStyle = MaterialTheme.typography.bodyLarge.copy(color = c.fg),
            cursorBrush = SolidColor(if (isError) c.rose else c.primary),
            keyboardOptions = keyboardOptions,
            keyboardActions = keyboardActions,
            visualTransformation = visualTransformation,
            interactionSource = interaction,
            decorationBox = { input ->
                Row(
                    Modifier
                        .fillMaxWidth()
                        .heightIn(min = if (dense) DENSE_FIELD_HEIGHT else FIELD_HEIGHT)
                        .graphicsLayer { alpha = if (enabled) 1f else 0.6f }
                        .clip(Radius.xl)
                        .background(c.surface2)
                        .border(1.dp, if (isError) c.rose.copy(alpha = 0.7f) else c.line, Radius.xl)
                        .drawWithContent {
                            drawContent()
                            if (isError || focus == 0f) return@drawWithContent
                            val w = RING_WIDTH.toPx()
                            drawRoundRect(
                                brand,
                                topLeft = Offset(w / 2, w / 2),
                                size = Size(size.width - w, size.height - w),
                                cornerRadius = CornerRadius(FIELD_CORNER.toPx() - w / 2),
                                alpha = focus,
                                style = Stroke(w),
                            )
                        }
                        .padding(start = 16.dp, end = if (trailing != null) 4.dp else 16.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    if (leadingIcon != null) {
                        Icon(leadingIcon, contentDescription = null, tint = iconTint, modifier = Modifier.size(20.dp))
                        Spacer(Modifier.width(12.dp))
                    }
                    Box(Modifier.weight(1f).padding(vertical = if (dense) 12.dp else 16.dp)) {
                        if (value.isEmpty() && placeholder != null) {
                            Text(placeholder, style = MaterialTheme.typography.bodyLarge, color = c.fgSubtle, maxLines = 1)
                        }
                        input()
                    }
                    if (trailing != null) CompositionLocalProvider(LocalContentColor provides c.fgSubtle, content = trailing)
                }
            },
        )
        if (supportingText != null) {
            Text(
                supportingText,
                Modifier.padding(start = 4.dp, top = 6.dp),
                style = MaterialTheme.typography.bodySmall,
                color = if (isError) c.rose else c.fgSubtle,
            )
        }
    }
}
