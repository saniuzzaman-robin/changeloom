package dev.changeloom.android.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import dev.changeloom.android.ui.theme.ChangeloomTheme
import dev.changeloom.android.ui.theme.Radius

/**
 * Modal card in the app's style: a tinted icon badge, centred title and message, optional [content], and a row of
 * [actions]. [danger] recolours the badge, border, glow and (via [DialogConfirmButton]) the confirm action red.
 */
@Composable
fun ChangeloomDialog(
    onDismiss: () -> Unit,
    title: String,
    icon: ImageVector,
    modifier: Modifier = Modifier,
    message: String? = null,
    danger: Boolean = false,
    properties: DialogProperties = DialogProperties(),
    actions: @Composable RowScope.() -> Unit,
    content: (@Composable () -> Unit)? = null,
) {
    val c = ChangeloomTheme.colors
    val tone = if (danger) c.red else c.primaryText
    Dialog(onDismissRequest = onDismiss, properties = properties) {
        Column(
            modifier
                .shadow(32.dp, Radius.sheet, ambientColor = Color.Black.copy(alpha = 0.3f), spotColor = if (danger) c.red else c.primary)
                .clip(Radius.sheet)
                .background(c.elevated)
                .background(Brush.verticalGradient(listOf(tone.copy(alpha = if (danger) 0.14f else 0.08f), Color.Transparent)))
                .border(1.dp, if (danger) c.red.copy(alpha = 0.55f) else c.lineStrong, Radius.sheet)
                .padding(start = 24.dp, end = 24.dp, top = 28.dp, bottom = 20.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Box(
                Modifier
                    .size(56.dp)
                    .clip(Radius.pill)
                    .background(tone.copy(alpha = 0.14f))
                    .border(1.dp, tone.copy(alpha = 0.35f), Radius.pill),
                contentAlignment = Alignment.Center,
            ) {
                Icon(icon, contentDescription = null, tint = tone, modifier = Modifier.size(28.dp))
            }
            Spacer(Modifier.height(16.dp))
            Text(title, style = MaterialTheme.typography.titleLarge, color = c.fg, textAlign = TextAlign.Center)
            if (message != null) {
                Spacer(Modifier.height(8.dp))
                Text(message, style = MaterialTheme.typography.bodyMedium, color = c.fgMuted, textAlign = TextAlign.Center)
            }
            if (content != null) {
                Spacer(Modifier.height(16.dp))
                content()
            }
            Spacer(Modifier.height(24.dp))
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(12.dp)) { actions() }
        }
    }
}

/** Filled pill for a dialog's main action: primary gradient, or solid red when [danger]. */
@Composable
fun DialogConfirmButton(
    text: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    danger: Boolean = false,
) {
    val c = ChangeloomTheme.colors
    if (!danger) {
        PrimaryButton(text, onClick, modifier, enabled = enabled, dense = true)
        return
    }
    Box(
        modifier
            .graphicsLayer { alpha = if (enabled) 1f else 0.45f }
            .clip(Radius.pill)
            .background(c.red)
            .clickable(remember { MutableInteractionSource() }, indication = null, enabled = enabled, role = Role.Button, onClick = onClick)
            .defaultMinSize(minHeight = 48.dp)
            .padding(horizontal = 24.dp, vertical = 12.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(text, style = MaterialTheme.typography.labelLarge, color = Color.White, maxLines = 1)
    }
}
