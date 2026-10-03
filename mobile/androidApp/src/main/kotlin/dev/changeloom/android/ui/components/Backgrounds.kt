package dev.changeloom.android.ui.components

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.height
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.draw.drawWithCache
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.BlendMode
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.CompositingStrategy
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import dev.changeloom.android.ui.theme.ChangeloomTheme

/**
 * The site's hairline grid (`linear-gradient(to right, var(--line) 1px, transparent 1px)` both ways),
 * faded out with a radial mask centred at [fadeCenter] (fractions of the size). Place it behind content in a Box.
 */
@Composable
fun GridBackground(
    modifier: Modifier = Modifier,
    cell: Dp = 32.dp,
    lineColor: Color = ChangeloomTheme.colors.line,
    fadeCenter: Offset = Offset(0.5f, 0f),
    fadeRadius: Float = 0.9f,
) {
    Spacer(
        modifier
            .graphicsLayer { compositingStrategy = CompositingStrategy.Offscreen }
            .drawWithCache {
                val step = cell.toPx()
                val stroke = 1.dp.toPx()
                val mask = Brush.radialGradient(
                    colors = listOf(Color.Black, Color.Black.copy(alpha = 0.35f), Color.Transparent),
                    center = Offset(size.width * fadeCenter.x, size.height * fadeCenter.y),
                    radius = size.maxDimension * fadeRadius,
                )
                onDrawBehind {
                    var x = 0f
                    while (x <= size.width) {
                        drawLine(lineColor, Offset(x, 0f), Offset(x, size.height), stroke)
                        x += step
                    }
                    var y = 0f
                    while (y <= size.height) {
                        drawLine(lineColor, Offset(0f, y), Offset(size.width, y), stroke)
                        y += step
                    }
                    drawRect(mask, blendMode = BlendMode.DstIn)
                }
            },
    )
}

/** The site's spotlight: a soft radial wash of [color] centred at [center] (fractions of the size). */
@Composable
fun SpotlightGlow(
    modifier: Modifier = Modifier,
    color: Color = ChangeloomTheme.colors.primary.copy(alpha = 0.18f),
    center: Offset = Offset(0.5f, 0f),
    radius: Dp = 420.dp,
) {
    Spacer(
        modifier.drawBehind {
            drawRect(
                Brush.radialGradient(
                    colors = listOf(color, Color.Transparent),
                    center = Offset(size.width * center.x, size.height * center.y),
                    radius = radius.toPx(),
                ),
            )
        },
    )
}

/**
 * The tab screens' backdrop, fixed behind them: the brand spotlight and the grid span the whole screen and fade out
 * gradually, and a faint accent wash rises from the bottom corner. Each screen used to draw its own in a strip at
 * the top, which cut the glow off partway down and left the rest of the screen flat.
 */
@Composable
fun ScreenBackdrop(
    modifier: Modifier = Modifier,
    glow: Color = ChangeloomTheme.colors.primary,
    /** How far down the grid reaches, as a fraction of the screen's longer side; reading screens keep it short. */
    gridFade: Float = BACKDROP_GRID_FADE,
) {
    val c = ChangeloomTheme.colors
    val accentAlpha = if (c.isDark) BACKDROP_ACCENT_ALPHA_DARK else BACKDROP_ACCENT_ALPHA_LIGHT
    Box(modifier) {
        GridBackground(Modifier.matchParentSize(), fadeCenter = Offset(0.5f, 0f), fadeRadius = gridFade)
        Spacer(
            Modifier.matchParentSize().drawBehind {
                // Down to the bottom edge, so there is no line where the wash stops.
                drawRect(
                    Brush.radialGradient(
                        0f to glow.copy(alpha = BACKDROP_GLOW_ALPHA),
                        BACKDROP_GLOW_MID_STOP to glow.copy(alpha = BACKDROP_GLOW_ALPHA * BACKDROP_GLOW_MID_FACTOR),
                        1f to Color.Transparent,
                        center = Offset(size.width / 2, 0f),
                        radius = size.height,
                    ),
                )
                drawRect(
                    Brush.radialGradient(
                        listOf(c.accent.copy(alpha = accentAlpha), Color.Transparent),
                        center = Offset(size.width, size.height),
                        radius = size.maxDimension * BACKDROP_ACCENT_RADIUS,
                    ),
                )
            },
        )
    }
}

private const val BACKDROP_GLOW_ALPHA = 0.18f
private const val BACKDROP_GLOW_MID_STOP = 0.55f
private const val BACKDROP_GLOW_MID_FACTOR = 0.4f
private const val BACKDROP_GRID_FADE = 0.85f
private const val BACKDROP_ACCENT_ALPHA_DARK = 0.08f
private const val BACKDROP_ACCENT_ALPHA_LIGHT = 0.06f
private const val BACKDROP_ACCENT_RADIUS = 0.75f
