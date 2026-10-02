package dev.changeloom.android.ui.theme

import androidx.compose.material3.ColorScheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Immutable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.compositeOver

/** Design tokens mirrored from saniuzzaman.dev (`--bg`, `--surface`, `--primary`, ...). */
@Immutable
data class ChangeloomColors(
    val isDark: Boolean,
    val bg: Color,
    val surface: Color,
    val surface2: Color,
    val elevated: Color,
    val fg: Color,
    val fgMuted: Color,
    val fgSubtle: Color,
    val line: Color,
    val lineStrong: Color,
    val primary: Color,
    val primaryStrong: Color,
    val primaryFg: Color,
    val primaryText: Color,
    val accent: Color,
    val success: Color,
    val glow: Color,
    /**
     * Bars over scrolling content (tab bar, top bar, system-bar scrims). The site's nav is 82% glass with a backdrop
     * blur; Compose can't blur what's behind a view, so this is opaque instead, or text would show through.
     */
    val navGlass: Color,
    val blue: Color,
    val rose: Color,
    val amber: Color,
    val red: Color,
    val slate: Color,
)

val DarkColors = ChangeloomColors(
    isDark = true,
    bg = Color(0xFF070709),
    surface = Color(0xFF0E0E13),
    surface2 = Color(0xFF15151C),
    elevated = Color(0xFF121218),
    fg = Color(0xFFF8FAFC),
    fgMuted = Color(0xFF94A3B8),
    fgSubtle = Color(0xFF7C8799),
    line = Color(0x14FFFFFF),
    lineStrong = Color(0x29FFFFFF),
    primary = Color(0xFF6366F1),
    primaryStrong = Color(0xFF4F46E5),
    primaryFg = Color(0xFFFFFFFF),
    primaryText = Color(0xFF818CF8),
    accent = Color(0xFF06B6D4),
    success = Color(0xFF10B981),
    glow = Color(0x2E6366F1),
    navGlass = Color(0xFF070709),
    blue = Color(0xFF3B82F6),
    rose = Color(0xFFF43F5E),
    amber = Color(0xFFF59E0B),
    red = Color(0xFFEF4444),
    slate = Color(0xFF64748B),
)

val LightColors = ChangeloomColors(
    isDark = false,
    bg = Color(0xFFF8FAFC),
    surface = Color(0xFFFFFFFF),
    surface2 = Color(0xFFF1F5F9),
    elevated = Color(0xFFFFFFFF),
    fg = Color(0xFF0F172A),
    fgMuted = Color(0xFF475569),
    fgSubtle = Color(0xFF5B6779),
    line = Color(0xFFE2E8F0),
    lineStrong = Color(0xFFCBD5E1),
    primary = Color(0xFF4F46E5),
    primaryStrong = Color(0xFF4338CA),
    primaryFg = Color(0xFFFFFFFF),
    primaryText = Color(0xFF4F46E5),
    accent = Color(0xFF0284C7),
    success = Color(0xFF047857),
    glow = Color(0x1A4F46E5),
    navGlass = Color(0xFFFFFFFF),
    blue = Color(0xFF2563EB),
    rose = Color(0xFFE11D48),
    amber = Color(0xFFD97706),
    red = Color(0xFFDC2626),
    slate = Color(0xFF64748B),
)

/** Maps the tokens onto Material 3 so stock components (fields, dialogs, switches) match. */
fun ChangeloomColors.toColorScheme(): ColorScheme {
    val base = if (isDark) darkColorScheme() else lightColorScheme()
    return base.copy(
        primary = primary,
        onPrimary = primaryFg,
        primaryContainer = glow.compositeOver(surface2),
        onPrimaryContainer = primaryText,
        inversePrimary = primaryText,
        secondary = accent,
        onSecondary = primaryFg,
        secondaryContainer = surface2,
        onSecondaryContainer = fg,
        tertiary = blue,
        onTertiary = primaryFg,
        background = bg,
        onBackground = fg,
        surface = bg,
        onSurface = fg,
        surfaceVariant = surface2,
        onSurfaceVariant = fgMuted,
        surfaceTint = primary,
        surfaceBright = elevated,
        surfaceDim = bg,
        surfaceContainerLowest = bg,
        surfaceContainerLow = surface,
        surfaceContainer = surface,
        surfaceContainerHigh = surface2,
        surfaceContainerHighest = surface2,
        inverseSurface = fg,
        inverseOnSurface = bg,
        error = rose,
        onError = primaryFg,
        outline = lineStrong,
        outlineVariant = line,
        scrim = Color.Black,
    )
}
