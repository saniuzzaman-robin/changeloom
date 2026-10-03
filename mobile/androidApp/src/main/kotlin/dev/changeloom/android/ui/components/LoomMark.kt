package dev.changeloom.android.ui.components

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.graphics.BlendMode
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.CompositingStrategy
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.PathMeasure
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.scale
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import dev.changeloom.android.R
import dev.changeloom.android.ui.theme.ChangeloomTheme

// Geometry in a unit square. Keep in sync with the launcher icon and splash drawables in res/drawable.
private val WARP_X = floatArrayOf(0.34f, 0.5f, 0.66f)
private const val WARP_TOP = 0.12f
private const val WARP_BOTTOM = 0.88f
private val ROW_Y = floatArrayOf(0.3f, 0.5f, 0.7f)
private const val RUN_LEFT = 0.22f
private const val RUN_RIGHT = 0.78f
private const val START_X = 0.16f
private const val END_X = 0.84f
private const val TURN_R = 0.1f
private const val THREAD_W = 0.1f
private const val WARP_W = 0.07f
private const val CLEAR = 0.03f

/** Checkerboard weave: the thread passes over a warp when row + col is even. */
private fun threadOver(row: Int, col: Int) = (row + col) % 2 == 0

/** One thread snaking right, left, right through the warps, turning at alternate ends. */
private fun threadPath(w: Float) = Path().apply {
    moveTo(START_X * w, ROW_Y[0] * w)
    lineTo(RUN_RIGHT * w, ROW_Y[0] * w)
    arcTo(turn(RUN_RIGHT, (ROW_Y[0] + ROW_Y[1]) / 2, w), -90f, 180f, false)
    lineTo(RUN_LEFT * w, ROW_Y[1] * w)
    arcTo(turn(RUN_LEFT, (ROW_Y[1] + ROW_Y[2]) / 2, w), -90f, -180f, false)
    lineTo(END_X * w, ROW_Y[2] * w)
}

private fun turn(cx: Float, cy: Float, w: Float) = Rect(Offset(cx * w, cy * w), TURN_R * w)

/**
 * The Changeloom mark: one brand-gradient thread woven over and under three warp threads.
 * With [animate], the warps grow from the centre and the thread then weaves itself through them.
 */
@Composable
fun LoomMark(modifier: Modifier = Modifier, animate: Boolean = false) {
    val c = ChangeloomTheme.colors
    val brand = ChangeloomTheme.gradients.brand
    val progress = remember { Animatable(if (animate) 0f else 1f) }
    LaunchedEffect(animate) {
        if (animate) progress.animateTo(1f, tween(1400, easing = FastOutSlowInEasing))
    }
    // Offscreen so the BlendMode.Clear strokes cut the weave's gaps out of the mark, not the screen behind it.
    val description = stringResource(R.string.app_brand)
    Canvas(modifier.graphicsLayer { compositingStrategy = CompositingStrategy.Offscreen }.semantics { contentDescription = description }) {
        val p = progress.value
        val w = size.minDimension
        val warpGrow = (p / 0.45f).coerceIn(0f, 1f)
        val weave = ((p - 0.15f) / 0.85f).coerceIn(0f, 1f)
        val warp = c.fgMuted

        scale(1f, warpGrow, Offset(w / 2, w / 2)) {
            WARP_X.forEach { x -> drawLine(warp, Offset(x * w, WARP_TOP * w), Offset(x * w, WARP_BOTTOM * w), WARP_W * w, StrokeCap.Round) }
        }
        if (weave > 0f) {
            val full = threadPath(w)
            val measure = PathMeasure().apply { setPath(full, false) }
            val drawn = Path().also { measure.getSegment(0f, measure.length * weave, it, true) }
            // A clear halo under the thread opens the gap where it passes over a warp.
            drawPath(drawn, Color.Black, style = threadStroke(w, THREAD_W + 2 * CLEAR), blendMode = BlendMode.Clear)
            drawPath(drawn, brand, style = threadStroke(w, THREAD_W))
        }
        scale(1f, warpGrow, Offset(w / 2, w / 2)) { drawWarpOvers(w, warp) }
    }
}

private fun threadStroke(w: Float, width: Float) = Stroke(width * w, cap = StrokeCap.Round, join = StrokeJoin.Round)

/** Re-draws each warp where it passes over the thread, clearing a gap either side first. */
private fun DrawScope.drawWarpOvers(w: Float, color: Color) {
    val half = THREAD_W / 2 + CLEAR + 0.01f
    ROW_Y.forEachIndexed { row, y ->
        WARP_X.forEachIndexed { col, x ->
            if (threadOver(row, col)) return@forEachIndexed
            val top = Offset(x * w, (y - half) * w)
            val bottom = Offset(x * w, (y + half) * w)
            drawLine(Color.Black, top, bottom, (WARP_W + 2 * CLEAR) * w, blendMode = BlendMode.Clear)
            drawLine(color, top, bottom, WARP_W * w)
        }
    }
}
