package dev.changeloom.android.ui.components

import androidx.compose.animation.core.Animatable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.material3.LocalTextStyle
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalInspectionMode
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import dev.changeloom.android.ui.theme.ChangeloomTextStyles
import dev.changeloom.android.ui.theme.ChangeloomTheme
import dev.changeloom.android.ui.theme.Durations
import dev.changeloom.android.ui.theme.expoTween
import java.util.Locale

/** Text painted with a gradient, e.g. the wordmark. */
@Composable
fun GradientText(
    text: String,
    modifier: Modifier = Modifier,
    style: TextStyle = LocalTextStyle.current,
    brush: Brush = ChangeloomTheme.gradients.brandHorizontal,
    textAlign: TextAlign? = null,
) {
    Text(text, modifier, style = style.merge(TextStyle(brush = brush)), textAlign = textAlign)
}

/** Fades and lifts content in after [delayMillis]; already settled in previews. */
@Composable
fun Modifier.enter(delayMillis: Int): Modifier {
    val inspection = LocalInspectionMode.current
    val progress = remember { Animatable(if (inspection) 1f else 0f) }
    LaunchedEffect(Unit) { progress.animateTo(1f, expoTween(Durations.SLOW + 300, delayMillis)) }
    val rise = with(LocalDensity.current) { 24.dp.toPx() }
    return graphicsLayer {
        alpha = progress.value
        translationY = (1f - progress.value) * rise
    }
}

/**
 * The site's `word-rise`: each word fades and slides up in turn.
 * [startDelayMillis] lets several pieces of text on a screen enter one after another.
 */
@Composable
fun WordRiseText(
    text: String,
    modifier: Modifier = Modifier,
    style: TextStyle = LocalTextStyle.current,
    color: Color = Color.Unspecified,
    startDelayMillis: Int = 0,
    staggerMillis: Int = 70,
    horizontalArrangement: Arrangement.Horizontal = Arrangement.Start,
) {
    val words = remember(text) { text.split(' ').filter { it.isNotEmpty() } }
    val rise = with(LocalDensity.current) { 14.dp.toPx() }
    // Previews don't advance animations, so start settled there.
    val initial = if (LocalInspectionMode.current) 1f else 0f
    FlowRow(modifier, horizontalArrangement = horizontalArrangement) {
        words.forEachIndexed { i, word ->
            val progress = remember(text) { Animatable(initial) }
            LaunchedEffect(text) {
                progress.animateTo(1f, expoTween(Durations.SLOW + 200, startDelayMillis + i * staggerMillis))
            }
            Text(
                text = if (i < words.lastIndex) "$word " else word,
                style = style,
                color = color,
                modifier = Modifier.graphicsLayer {
                    alpha = progress.value
                    translationY = (1f - progress.value) * rise
                },
            )
        }
    }
}

/** Small uppercase mono meta label: `RELEASE · 2H AGO`. */
@Composable
fun Eyebrow(
    text: String,
    modifier: Modifier = Modifier,
    color: Color = ChangeloomTheme.colors.fgSubtle,
    maxLines: Int = 1,
) {
    Text(
        text = text.uppercase(Locale.ROOT),
        modifier = modifier,
        style = ChangeloomTextStyles.eyebrow,
        color = color,
        maxLines = maxLines,
        overflow = TextOverflow.Ellipsis,
    )
}
