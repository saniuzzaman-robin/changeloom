package dev.changeloom.android.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.unit.dp
import dev.changeloom.android.ui.theme.ChangeloomTheme
import dev.changeloom.android.ui.theme.Radius

/**
 * The site's card: `surface` fill, 1px `line` border, an inset top highlight in dark mode and a soft
 * drop shadow in light mode (`--shadow-card`). Tappable cards scale down slightly when pressed.
 */
@Composable
fun GlassCard(
    modifier: Modifier = Modifier,
    onClick: (() -> Unit)? = null,
    shape: Shape = Radius.xl,
    border: Brush? = null,
    background: Color = ChangeloomTheme.colors.surface,
    contentPadding: PaddingValues = PaddingValues(16.dp),
    content: @Composable ColumnScope.() -> Unit,
) {
    val c = ChangeloomTheme.colors
    val interaction = remember { MutableInteractionSource() }
    var m = modifier
    if (onClick != null) m = m.pressScale(interaction, pressed = 0.98f)
    if (!c.isDark) m = m.shadow(12.dp, shape, ambientColor = c.fg.copy(alpha = 0.06f), spotColor = c.fg.copy(alpha = 0.16f))
    m = m
        .clip(shape)
        .background(background)
        .border(1.dp, border ?: SolidColor(c.line), shape)
    if (c.isDark) {
        m = m.drawWithContent {
            drawContent()
            drawLine(Color.White.copy(alpha = 0.04f), Offset(0f, 1f), Offset(size.width, 1f), 1.dp.toPx())
        }
    }
    if (onClick != null) m = m.clickable(interaction, indication = null, onClick = onClick)
    Column(m.padding(contentPadding), content = content)
}
