package dev.changeloom.android.ui.theme

import androidx.compose.animation.core.CubicBezierEasing
import androidx.compose.animation.core.FiniteAnimationSpec
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.Stable
import androidx.compose.runtime.State
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.staticCompositionLocalOf

/** The site's `--ease-out-expo`. */
val EaseOutExpo = CubicBezierEasing(0.16f, 1f, 0.3f, 1f)

object Durations {
    const val FAST = 150
    const val MEDIUM = 300
    const val SLOW = 600
}

fun <T> expoTween(durationMillis: Int = Durations.MEDIUM, delayMillis: Int = 0): FiniteAnimationSpec<T> =
    tween(durationMillis, delayMillis, EaseOutExpo)

/**
 * One shimmer sweep shared by every loading placeholder, so they move in step and the frame clock drives a single
 * animation. [ChangeloomTheme] runs it only while a placeholder is attached.
 */
@Stable
class ShimmerClock {
    private var users by mutableIntStateOf(0)
    private var sweep by mutableStateOf<State<Float>?>(null)

    /** Highlight position in widths, [SHIMMER_FROM] to [SHIMMER_TO]. Read it in the draw phase only. */
    val progress: Float get() = sweep?.value ?: SHIMMER_FROM

    fun attach() {
        users++
    }

    fun detach() {
        users--
    }

    @Composable
    internal fun Run() {
        if (users == 0) return
        val progress = rememberInfiniteTransition(label = "shimmer")
            .animateFloat(SHIMMER_FROM, SHIMMER_TO, infiniteRepeatable(tween(SHIMMER_PERIOD_MS, easing = LinearEasing)), label = "shimmerX")
        DisposableEffect(progress) {
            sweep = progress
            onDispose { sweep = null }
        }
    }
}

val LocalShimmerClock = staticCompositionLocalOf { ShimmerClock() }

private const val SHIMMER_FROM = -1f
private const val SHIMMER_TO = 2f
private const val SHIMMER_PERIOD_MS = 1400
