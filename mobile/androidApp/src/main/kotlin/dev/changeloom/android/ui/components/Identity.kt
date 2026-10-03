package dev.changeloom.android.ui.components

import androidx.compose.animation.Crossfade
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.Spring
import androidx.compose.animation.core.spring
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.Bookmark
import androidx.compose.material.icons.rounded.BookmarkBorder
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.changeloom.android.R
import dev.changeloom.android.ui.theme.ChangeloomTheme
import dev.changeloom.android.ui.theme.Durations
import dev.changeloom.android.ui.theme.expoTween
import java.util.Locale

/** Up to two initials from a display name or an email address. */
internal fun initialsOf(name: String): String {
    val parts = name.substringBefore('@').split(' ', '.', '_', '-').filter { it.isNotBlank() }
    return parts.take(2).joinToString("") { it.first().toString() }.uppercase(Locale.ROOT).ifEmpty { "?" }
}

/** A photo, or initials until one is available, inside a brand-gradient ring. */
@Composable
fun GradientAvatar(name: String, modifier: Modifier = Modifier, size: Dp = 64.dp, photo: ImageBitmap? = null) {
    val c = ChangeloomTheme.colors
    Box(
        modifier
            .size(size)
            .border(2.dp, ChangeloomTheme.gradients.brand, CircleShape)
            .padding(4.dp)
            .clip(CircleShape)
            .background(c.surface2),
        contentAlignment = Alignment.Center,
    ) {
        Crossfade(photo, Modifier.fillMaxSize(), animationSpec = expoTween(Durations.SLOW), label = "avatar") { image ->
            if (image != null) {
                Image(image, contentDescription = null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
            } else {
                Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                    GradientText(
                        initialsOf(name),
                        style = MaterialTheme.typography.titleLarge.copy(fontSize = (size.value * 0.34f).sp),
                        brush = ChangeloomTheme.gradients.brand,
                    )
                }
            }
        }
    }
}

/** Bookmark toggle that pops and ticks when a story is saved. */
@Composable
fun SaveToggle(saved: Boolean, onToggle: () -> Unit, modifier: Modifier = Modifier) {
    val c = ChangeloomTheme.colors
    val haptics = LocalHapticFeedback.current
    val tint by animateColorAsState(if (saved) c.primaryText else c.fgSubtle, expoTween(Durations.MEDIUM), label = "saveTint")
    val scale = remember { Animatable(1f) }
    val previous = remember { booleanArrayOf(saved) }
    LaunchedEffect(saved) {
        if (saved && !previous[0]) {
            scale.animateTo(1.35f, spring(stiffness = Spring.StiffnessHigh))
            scale.animateTo(1f, spring(dampingRatio = Spring.DampingRatioMediumBouncy, stiffness = Spring.StiffnessMediumLow))
        }
        previous[0] = saved
    }
    IconButton(
        onClick = {
            haptics.performHapticFeedback(if (saved) HapticFeedbackType.ToggleOff else HapticFeedbackType.ToggleOn)
            onToggle()
        },
        modifier = modifier,
    ) {
        Crossfade(saved, label = "saveIcon") { on ->
            Icon(
                if (on) Icons.Rounded.Bookmark else Icons.Rounded.BookmarkBorder,
                contentDescription = stringResource(if (on) R.string.remove_from_saved else R.string.save),
                tint = tint,
                modifier = Modifier.graphicsLayer { scaleX = scale.value; scaleY = scale.value },
            )
        }
    }
}
