package dev.changeloom.android.ui.theme

import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Shapes
import androidx.compose.ui.unit.dp

/** Site radii: md 6, lg 8, xl 12, 2xl 16, 3xl 24. */
object Radius {
    val sm = RoundedCornerShape(6.dp)
    val md = RoundedCornerShape(8.dp)
    val lg = RoundedCornerShape(12.dp)
    val xl = RoundedCornerShape(16.dp)
    val xxl = RoundedCornerShape(24.dp)
    val pill = RoundedCornerShape(percent = 50)
}

val ChangeloomShapes = Shapes(
    extraSmall = Radius.sm,
    small = Radius.md,
    medium = Radius.lg,
    large = Radius.xl,
    extraLarge = Radius.xxl,
)
