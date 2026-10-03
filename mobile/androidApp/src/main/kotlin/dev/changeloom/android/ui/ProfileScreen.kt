package dev.changeloom.android.ui

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBars
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.Logout
import androidx.compose.material.icons.rounded.Bookmark
import androidx.compose.material.icons.rounded.DarkMode
import androidx.compose.material.icons.rounded.DoneAll
import androidx.compose.material.icons.rounded.Edit
import androidx.compose.material.icons.rounded.ErrorOutline
import androidx.compose.material.icons.rounded.LightMode
import androidx.compose.material.icons.rounded.SettingsBrightness
import androidx.compose.material.icons.rounded.Tag
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalInspectionMode
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.changeloom.android.BuildConfig
import dev.changeloom.android.ui.components.BannerTone
import dev.changeloom.android.ui.components.Eyebrow
import dev.changeloom.android.ui.components.GlassCard
import dev.changeloom.android.ui.components.GradientAvatar
import dev.changeloom.android.ui.components.GridBackground
import dev.changeloom.android.ui.components.SecondaryButton
import dev.changeloom.android.ui.components.SpotlightGlow
import dev.changeloom.android.ui.components.StatusBanner
import dev.changeloom.android.ui.components.StatusBarScrim
import dev.changeloom.android.ui.components.TextAction
import dev.changeloom.android.ui.components.TopicChip
import dev.changeloom.android.ui.components.enter
import dev.changeloom.android.ui.theme.ChangeloomTheme
import dev.changeloom.android.ui.theme.Durations
import dev.changeloom.android.ui.theme.Radius
import dev.changeloom.android.ui.theme.Spacing
import dev.changeloom.android.ui.theme.ThemeMode
import dev.changeloom.android.ui.theme.expoTween
import dev.changeloom.shared.data.MeStats
import org.koin.androidx.compose.koinViewModel
import kotlin.math.roundToLong

@Composable
internal fun ProfileScreen(
    onEditTopics: () -> Unit,
    contentPadding: PaddingValues,
    vm: ProfileViewModel = koinViewModel(),
    picker: TopicPickerViewModel = koinViewModel(),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val mode by vm.themeMode.collectAsStateWithLifecycle()
    val followed = picker.state.collectAsStateWithLifecycle().value.followed
    val photo = vm.photo.collectAsStateWithLifecycle().value
    val photoImage = remember(photo) { photo?.asImageBitmap() }
    LaunchedEffect(Unit) { vm.refresh() }
    ProfileContent(
        displayName = vm.displayName,
        email = vm.email,
        photo = photoImage,
        state = state,
        followed = followed,
        themeMode = mode,
        version = BuildConfig.VERSION_NAME,
        contentPadding = contentPadding,
        onEditTopics = onEditTopics,
        onThemeMode = vm::setThemeMode,
        onRetry = vm::refresh,
        onRequestText = vm::setRequestText,
        onSubmitRequest = vm::submitRequest,
        onSignOut = vm::signOut,
    )
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun ProfileContent(
    displayName: String?,
    email: String?,
    photo: ImageBitmap?,
    state: ProfileState,
    followed: List<String>,
    themeMode: ThemeMode,
    version: String,
    contentPadding: PaddingValues,
    onEditTopics: () -> Unit,
    onThemeMode: (ThemeMode) -> Unit,
    onRetry: () -> Unit,
    onRequestText: (String) -> Unit,
    onSubmitRequest: () -> Unit,
    onSignOut: () -> Unit,
) {
    val c = ChangeloomTheme.colors
    val topicName = LocalTopicName.current
    var confirmSignOut by rememberSaveable { mutableStateOf(false) }
    val scroll = rememberScrollState()

    Box(Modifier.fillMaxSize()) {
        Column(
            Modifier
                .fillMaxSize()
                .verticalScroll(scroll)
                .padding(bottom = contentPadding.calculateBottomPadding() + 24.dp),
        ) {
            ProfileBanner(displayName, email, photo)
            Column(
                Modifier.windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Horizontal)).padding(horizontal = Spacing.gutter),
                verticalArrangement = Arrangement.spacedBy(28.dp),
            ) {
                Column(Modifier.enter(160)) {
                    Eyebrow("Your activity")
                    Spacer(Modifier.height(12.dp))
                    StatusBanner(state.error, Icons.Rounded.ErrorOutline, tone = BannerTone.Error, actionLabel = "Retry", onAction = onRetry)
                    if (state.error != null) Spacer(Modifier.height(12.dp))
                    Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        StatTile("Saved", state.stats?.saved, Icons.Rounded.Bookmark, c.primaryText, Modifier.weight(1f))
                        StatTile("Read", state.stats?.read, Icons.Rounded.DoneAll, c.success, Modifier.weight(1f))
                        StatTile("Topics", followed.size.toLong(), Icons.Rounded.Tag, c.accent, Modifier.weight(1f))
                    }
                }

                Column(Modifier.enter(240)) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Eyebrow("Following", Modifier.weight(1f))
                        TextAction("Edit topics", onEditTopics, icon = Icons.Rounded.Edit)
                    }
                    Spacer(Modifier.height(4.dp))
                    if (followed.isEmpty()) {
                        Text("You're not following any topics yet.", style = MaterialTheme.typography.bodyMedium, color = c.fgMuted)
                    } else {
                        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                            followed.forEach { TopicChip(topicName(it)) }
                        }
                    }
                }

                Column(Modifier.enter(280)) {
                    TopicRequestSection(state, onRequestText, onSubmitRequest)
                }

                Column(Modifier.enter(320)) {
                    Eyebrow("Appearance")
                    Spacer(Modifier.height(12.dp))
                    ThemeSwitch(themeMode, onThemeMode)
                }

                Column(Modifier.enter(400), horizontalAlignment = Alignment.CenterHorizontally) {
                    SecondaryButton(
                        "Sign out",
                        onClick = { confirmSignOut = true },
                        modifier = Modifier.fillMaxWidth(),
                        icon = Icons.AutoMirrored.Rounded.Logout,
                        contentColor = c.rose,
                    )
                    Spacer(Modifier.height(16.dp))
                    Eyebrow("Changeloom v$version")
                }
            }
        }
        StatusBarScrim(scroll.canScrollBackward)
    }

    if (confirmSignOut) {
        AlertDialog(
            onDismissRequest = { confirmSignOut = false },
            confirmButton = {
                TextAction("Sign out", onClick = {
                    confirmSignOut = false
                    onSignOut()
                }, color = c.rose)
            },
            dismissButton = { TextAction("Cancel", onClick = { confirmSignOut = false }, color = c.fgMuted) },
            title = { Text("Sign out?") },
            text = { Text("This device stops getting notifications until you sign back in.") },
            shape = Radius.xxl,
            containerColor = c.elevated,
            titleContentColor = c.fg,
            textContentColor = c.fgMuted,
        )
    }
}

@Composable
private fun ProfileBanner(displayName: String?, email: String?, photo: ImageBitmap?) {
    val c = ChangeloomTheme.colors
    val name = displayName ?: email?.let(::firstNameOf) ?: "Reader"
    val status = WindowInsets.statusBars.asPaddingValues().calculateTopPadding()
    Box(Modifier.fillMaxWidth()) {
        SpotlightGlow(Modifier.matchParentSize(), center = Offset(0.5f, 0f), radius = 320.dp)
        GridBackground(Modifier.matchParentSize(), fadeCenter = Offset(0.5f, 0f), fadeRadius = 0.8f)
        Column(
            Modifier
                .fillMaxWidth()
                .windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Horizontal))
                .padding(start = Spacing.gutter, end = Spacing.gutter, top = status + 32.dp, bottom = 32.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            GradientAvatar(displayName ?: email ?: name, Modifier.enter(0), size = 88.dp, photo = photo)
            Spacer(Modifier.height(16.dp))
            Column(Modifier.enter(80), horizontalAlignment = Alignment.CenterHorizontally) {
                Text(
                    name,
                    style = MaterialTheme.typography.headlineMedium,
                    color = c.fg,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    textAlign = TextAlign.Center,
                )
                if (email != null) {
                    Spacer(Modifier.height(4.dp))
                    Text(email, style = MaterialTheme.typography.bodyMedium, color = c.fgMuted, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
            }
        }
    }
}

/** A count that ticks up from zero the first time it is known; "–" while it loads. */
@Composable
private fun StatTile(label: String, value: Long?, icon: ImageVector, tint: Color, modifier: Modifier = Modifier) {
    val c = ChangeloomTheme.colors
    val inspection = LocalInspectionMode.current
    val shown = remember { Animatable(if (inspection) (value ?: 0).toFloat() else 0f) }
    LaunchedEffect(value) { if (value != null) shown.animateTo(value.toFloat(), expoTween(Durations.SLOW + 600)) }
    GlassCard(modifier, contentPadding = PaddingValues(14.dp)) {
        Box(
            Modifier
                .size(32.dp)
                .clip(Radius.md)
                .background(tint.copy(alpha = 0.14f)),
            contentAlignment = Alignment.Center,
        ) {
            Icon(icon, contentDescription = null, Modifier.size(18.dp), tint = tint)
        }
        Spacer(Modifier.height(12.dp))
        Text(
            if (value == null) "–" else shown.value.roundToLong().toString(),
            style = MaterialTheme.typography.headlineSmall.copy(fontWeight = FontWeight.Bold),
            color = c.fg,
            maxLines = 1,
        )
        Eyebrow(label)
    }
}

/** Segmented System / Light / Dark switch; a washed pill slides under the selected option. */
@Composable
private fun ThemeSwitch(mode: ThemeMode, onChange: (ThemeMode) -> Unit) {
    val c = ChangeloomTheme.colors
    val options = ThemeMode.entries
    BoxWithConstraints(
        Modifier
            .fillMaxWidth()
            .clip(Radius.pill)
            .background(c.surface2)
            .border(1.dp, c.line, Radius.pill)
            .padding(4.dp),
    ) {
        val segment = maxWidth / options.size
        val indicator by animateDpAsState(segment * mode.ordinal, expoTween(Durations.SLOW), label = "themeIndicator")
        Box(
            Modifier
                .offset(x = indicator)
                .width(segment)
                .height(SEGMENT_HEIGHT)
                .clip(Radius.pill)
                .background(c.surface)
                .background(ChangeloomTheme.gradients.wash)
                .border(1.dp, c.primary.copy(alpha = 0.4f), Radius.pill),
        )
        Row(Modifier.selectableGroup()) {
            options.forEach { option ->
                val selected = option == mode
                val fg by animateColorAsState(if (selected) c.primaryText else c.fgMuted, expoTween(), label = "themeFg")
                Row(
                    Modifier
                        .weight(1f)
                        .height(SEGMENT_HEIGHT)
                        .clip(Radius.pill)
                        .selectable(selected, role = Role.RadioButton) { onChange(option) },
                    horizontalArrangement = Arrangement.Center,
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Icon(option.icon(), contentDescription = null, Modifier.size(18.dp), tint = fg)
                    Spacer(Modifier.width(8.dp))
                    Text(option.name, style = MaterialTheme.typography.labelLarge, color = fg)
                }
            }
        }
    }
}

private fun ThemeMode.icon(): ImageVector = when (this) {
    ThemeMode.System -> Icons.Rounded.SettingsBrightness
    ThemeMode.Light -> Icons.Rounded.LightMode
    ThemeMode.Dark -> Icons.Rounded.DarkMode
}

@Composable
private fun ProfilePreview(state: ProfileState, mode: ThemeMode) {
    ProfileContent(
        displayName = "Ada Lovelace",
        email = "ada@example.com",
        photo = null,
        state = state,
        followed = listOf("languages/kotlin", "mobile/android", "security", "cloud/aws"),
        themeMode = mode,
        version = "0.1.0",
        contentPadding = PaddingValues(0.dp),
        onEditTopics = {},
        onThemeMode = {},
        onRetry = {},
        onRequestText = {},
        onSubmitRequest = {},
        onSignOut = {},
    )
}

@Preview(name = "Profile, dark", heightDp = 1000)
@Composable
private fun ProfileDark() = ChangeloomTheme(ThemeMode.Dark) {
    Box(Modifier.background(ChangeloomTheme.colors.bg)) { ProfilePreview(ProfileState(stats = MeStats(saved = 12, read = 148)), ThemeMode.Dark) }
}

@Preview(name = "Profile, error, light", heightDp = 1000)
@Composable
private fun ProfileErrorLight() = ChangeloomTheme(ThemeMode.Light) {
    Box(Modifier.background(ChangeloomTheme.colors.bg)) { ProfilePreview(ProfileState(error = "Couldn't reach the server"), ThemeMode.Light) }
}

private val SEGMENT_HEIGHT = 40.dp
