package dev.changeloom.android.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color

/** The site's gradients; `Offset.Infinite` ends resolve to the drawn size. */
@Immutable
data class ChangeloomGradients(
    /** `linear-gradient(120deg, primary, accent 60%, #3b82f6)`: hero text, active borders. */
    val brand: Brush,
    /** Same stops, left to right: gradient text on one line. */
    val brandHorizontal: Brush,
    /** `linear-gradient(135deg, primary-strong, primary)`: primary buttons. */
    val button: Brush,
    /** Subtle `primary/10 → accent/5` wash for selected cards and banners. */
    val wash: Brush,
)

private fun gradientsFor(c: ChangeloomColors): ChangeloomGradients {
    val stops = arrayOf(0f to c.primary, 0.6f to c.accent, 1f to c.blue)
    return ChangeloomGradients(
        brand = Brush.linearGradient(*stops, start = Offset.Zero, end = Offset.Infinite),
        brandHorizontal = Brush.horizontalGradient(*stops),
        button = Brush.linearGradient(listOf(c.primaryStrong, c.primary), start = Offset.Zero, end = Offset.Infinite),
        wash = Brush.linearGradient(
            listOf(c.primary.copy(alpha = 0.10f), c.accent.copy(alpha = 0.05f)),
            start = Offset.Zero,
            end = Offset.Infinite,
        ),
    )
}

private val DarkGradients = gradientsFor(DarkColors)
private val LightGradients = gradientsFor(LightColors)

val LocalChangeloomColors = staticCompositionLocalOf { DarkColors }
val LocalChangeloomGradients = staticCompositionLocalOf { DarkGradients }

enum class ThemeMode { System, Light, Dark }

@Composable
fun ThemeMode.isDark(): Boolean = when (this) {
    ThemeMode.System -> isSystemInDarkTheme()
    ThemeMode.Light -> false
    ThemeMode.Dark -> true
}

@Composable
fun ChangeloomTheme(mode: ThemeMode = ThemeMode.Dark, content: @Composable () -> Unit) {
    val dark = mode.isDark()
    val colors = if (dark) DarkColors else LightColors
    CompositionLocalProvider(
        LocalChangeloomColors provides colors,
        LocalChangeloomGradients provides if (dark) DarkGradients else LightGradients,
    ) {
        MaterialTheme(
            colorScheme = colors.toColorScheme(),
            typography = ChangeloomTypography,
            shapes = ChangeloomShapes,
            content = content,
        )
    }
}

object ChangeloomTheme {
    val colors: ChangeloomColors
        @Composable @ReadOnlyComposable get() = LocalChangeloomColors.current

    val gradients: ChangeloomGradients
        @Composable @ReadOnlyComposable get() = LocalChangeloomGradients.current
}

/** Severity accent colour; null severity reads as muted. */
@Composable
@ReadOnlyComposable
fun severityColor(severity: String?): Color {
    val c = ChangeloomTheme.colors
    return when (severity) {
        "critical" -> c.rose
        "high" -> c.amber
        "medium" -> c.accent
        "low" -> c.slate
        else -> c.fgSubtle
    }
}

/** Story kind accent colour (`StorySummary.kind`). */
@Composable
@ReadOnlyComposable
fun kindColor(kind: String): Color {
    val c = ChangeloomTheme.colors
    return when (kind) {
        "release" -> c.primaryText
        "breaking" -> c.rose
        "security" -> c.red
        "deprecation" -> c.amber
        "announcement" -> c.accent
        "research" -> c.blue
        "policy" -> c.slate
        "deal" -> c.accent
        else -> c.fgMuted
    }
}
