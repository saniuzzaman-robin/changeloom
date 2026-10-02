package dev.changeloom.android.ui.theme

import androidx.compose.animation.core.CubicBezierEasing
import androidx.compose.animation.core.FiniteAnimationSpec
import androidx.compose.animation.core.tween

/** The site's `--ease-out-expo`. */
val EaseOutExpo = CubicBezierEasing(0.16f, 1f, 0.3f, 1f)

object Durations {
    const val FAST = 150
    const val MEDIUM = 300
    const val SLOW = 600
}

fun <T> expoTween(durationMillis: Int = Durations.MEDIUM, delayMillis: Int = 0): FiniteAnimationSpec<T> =
    tween(durationMillis, delayMillis, EaseOutExpo)
