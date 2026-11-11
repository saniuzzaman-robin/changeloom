package dev.changeloom.android.ui

import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.wrapContentWidth
import androidx.compose.runtime.Composable
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalWindowInfo
import androidx.compose.ui.unit.Dp
import dev.changeloom.android.ui.theme.Spacing

/** Width class of the app window (not the device, so split-screen and freeform windows get the right one). */
enum class WindowWidth { Compact, Medium, Expanded }

private const val MEDIUM_MIN_DP = 600
private const val EXPANDED_MIN_DP = 840

fun windowWidthOf(widthDp: Float): WindowWidth = when {
    widthDp >= EXPANDED_MIN_DP -> WindowWidth.Expanded
    widthDp >= MEDIUM_MIN_DP -> WindowWidth.Medium
    else -> WindowWidth.Compact
}

/** Provided once at [AppRoot]; previews and tests that don't provide it get the phone layout. */
val LocalWindowWidth = compositionLocalOf { WindowWidth.Compact }

/** The width class of the current window. */
@Composable
fun rememberWindowWidth(): WindowWidth {
    val widthPx = LocalWindowInfo.current.containerSize.width
    val density = LocalDensity.current.density
    return windowWidthOf(widthPx / density)
}

/** Fills the width up to [Spacing.readableMaxWidth] and centres the content when the window is wider. */
fun Modifier.readableWidth(max: Dp = Spacing.readableMaxWidth): Modifier =
    fillMaxWidth().wrapContentWidth(Alignment.CenterHorizontally).widthIn(max = max).fillMaxWidth()
