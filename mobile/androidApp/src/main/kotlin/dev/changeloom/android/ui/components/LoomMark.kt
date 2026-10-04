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
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.PathMeasure
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.clipRect
import androidx.compose.ui.graphics.drawscope.scale
import androidx.compose.ui.graphics.drawscope.translate
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import dev.changeloom.android.R
import dev.changeloom.android.ui.theme.ChangeloomTheme

// Geometry in the launcher icon's 108-unit viewport. Keep in sync with the launcher, notification and splash
// drawables in res/drawable and the SVGs in mobile/branding.
private const val CROP_LEFT = 12f
private const val CROP_SIZE = 84f
private const val DELTA_W = 8f
private const val WEFT_Y = 52f
private const val WEFT_HALF = 3f
private const val WEFT_LEFT = 24f
private const val WEFT_RIGHT = 84f

/** The delta, starting and ending at the gap where the weft passes over its left side. */
private val deltaPath = Path().apply {
    moveTo(43.33f, 46.5f)
    lineTo(54f, 28f)
    lineTo(76.5f, 67f)
    lineTo(31.5f, 67f)
    lineTo(36.98f, 57.5f)
}

/** The weft, broken where it passes under the delta's right side. */
private val weftPath = Path().apply {
    moveTo(60.35f, WEFT_Y - WEFT_HALF)
    lineTo(WEFT_LEFT + WEFT_HALF, WEFT_Y - WEFT_HALF)
    arcTo(Rect(Offset(WEFT_LEFT + WEFT_HALF, WEFT_Y), WEFT_HALF), -90f, -180f, false)
    lineTo(60.35f, WEFT_Y + WEFT_HALF)
    close()
    moveTo(75.35f, WEFT_Y - WEFT_HALF)
    lineTo(WEFT_RIGHT - WEFT_HALF, WEFT_Y - WEFT_HALF)
    arcTo(Rect(Offset(WEFT_RIGHT - WEFT_HALF, WEFT_Y), WEFT_HALF), -90f, 180f, false)
    lineTo(75.35f, WEFT_Y + WEFT_HALF)
    close()
}

/**
 * The Changeloom mark: a delta (change) with a weft thread woven through it, over one side and under the other.
 * With [animate], the delta draws itself and the weft then shoots through it.
 */
@Composable
fun LoomMark(modifier: Modifier = Modifier, animate: Boolean = false) {
    val c = ChangeloomTheme.colors
    val progress = remember { Animatable(if (animate) 0f else 1f) }
    LaunchedEffect(animate) {
        if (animate) progress.animateTo(1f, tween(1400, easing = FastOutSlowInEasing))
    }
    val description = stringResource(R.string.app_brand)
    Canvas(modifier.semantics { contentDescription = description }) {
        val p = progress.value
        val draw = (p / 0.6f).coerceIn(0f, 1f)
        val shoot = ((p - 0.45f) / 0.55f).coerceIn(0f, 1f)
        scale(size.minDimension / CROP_SIZE, Offset.Zero) {
            translate(-CROP_LEFT, -CROP_LEFT) {
                val measure = PathMeasure().apply { setPath(deltaPath, false) }
                val delta = Path().also { measure.getSegment(0f, measure.length * draw, it, true) }
                drawPath(delta, c.primaryText, style = Stroke(DELTA_W, cap = StrokeCap.Butt, join = StrokeJoin.Round))
                clipRect(right = WEFT_LEFT + (WEFT_RIGHT - WEFT_LEFT) * shoot) { drawPath(weftPath, c.accent) }
            }
        }
    }
}
