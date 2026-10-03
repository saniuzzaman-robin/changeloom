package dev.changeloom.android.ui

import androidx.activity.compose.BackHandler
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.Spring
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.spring
import androidx.compose.animation.expandVertically
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.scaleIn
import androidx.compose.animation.scaleOut
import androidx.compose.animation.shrinkVertically
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.selection.triStateToggleable
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.ArrowBack
import androidx.compose.material.icons.automirrored.rounded.ArrowForward
import androidx.compose.material.icons.rounded.AutoAwesome
import androidx.compose.material.icons.rounded.Build
import androidx.compose.material.icons.rounded.Check
import androidx.compose.material.icons.rounded.Close
import androidx.compose.material.icons.rounded.Cloud
import androidx.compose.material.icons.rounded.Code
import androidx.compose.material.icons.rounded.ErrorOutline
import androidx.compose.material.icons.rounded.ExpandMore
import androidx.compose.material.icons.rounded.PhoneAndroid
import androidx.compose.material.icons.rounded.Public
import androidx.compose.material.icons.rounded.Remove
import androidx.compose.material.icons.rounded.Search
import androidx.compose.material.icons.rounded.SearchOff
import androidx.compose.material.icons.rounded.Security
import androidx.compose.material.icons.rounded.Storage
import androidx.compose.material.icons.rounded.Tag
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.state.ToggleableState
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.changeloom.android.R
import dev.changeloom.android.ui.components.BannerTone
import dev.changeloom.android.ui.components.ChangeloomTextField
import dev.changeloom.android.ui.components.EmptyState
import dev.changeloom.android.ui.components.Eyebrow
import dev.changeloom.android.ui.components.GlassCard
import dev.changeloom.android.ui.components.GradientText
import dev.changeloom.android.ui.components.GridBackground
import dev.changeloom.android.ui.components.IconTile
import dev.changeloom.android.ui.components.LoomMark
import dev.changeloom.android.ui.components.PrimaryButton
import dev.changeloom.android.ui.components.SkeletonBlock
import dev.changeloom.android.ui.components.SpotlightGlow
import dev.changeloom.android.ui.components.StatusBanner
import dev.changeloom.android.ui.components.TextAction
import dev.changeloom.android.ui.components.TopicChip
import dev.changeloom.android.ui.components.enter
import dev.changeloom.android.ui.components.shimmer
import dev.changeloom.android.ui.theme.ChangeloomTheme
import dev.changeloom.android.ui.theme.Durations
import dev.changeloom.android.ui.theme.Radius
import dev.changeloom.android.ui.theme.Spacing
import dev.changeloom.android.ui.theme.ThemeMode
import dev.changeloom.android.ui.theme.expoTween
import dev.changeloom.shared.data.CheckState
import dev.changeloom.shared.data.Topic
import dev.changeloom.shared.data.TopicSelection
import dev.changeloom.shared.data.TopicTree
import org.koin.androidx.compose.koinViewModel

enum class TopicPickerMode { Onboarding, Edit }

/**
 * Followed-topic picker. [TopicPickerMode.Onboarding] is shown until the user has topics and [onExit]
 * signs out; [TopicPickerMode.Edit] discards unsaved changes on [onExit] and closes itself after saving.
 */
@Composable
fun TopicPickerScreen(mode: TopicPickerMode, onExit: () -> Unit, vm: TopicPickerViewModel = koinViewModel()) {
    val state by vm.state.collectAsStateWithLifecycle()
    val exit = {
        if (mode == TopicPickerMode.Edit) vm.discard()
        onExit()
    }
    if (mode == TopicPickerMode.Edit) {
        BackHandler(onBack = exit)
        LaunchedEffect(state.saved) {
            if (state.saved) {
                vm.savedHandled()
                onExit()
            }
        }
    }
    TopicPickerContent(
        state = state,
        mode = mode,
        onToggle = vm::toggle,
        onExpand = vm::toggleExpanded,
        onQueryChange = vm::setQuery,
        onSelectAll = vm::selectAll,
        onClear = vm::clear,
        onSave = vm::save,
        onRetry = vm::load,
        onExit = exit,
    )
}

@Composable
internal fun TopicPickerContent(
    state: TopicPickerState,
    mode: TopicPickerMode,
    onToggle: (String) -> Unit,
    onExpand: (String) -> Unit,
    onQueryChange: (String) -> Unit,
    onSelectAll: () -> Unit,
    onClear: () -> Unit,
    onSave: () -> Unit,
    onRetry: () -> Unit,
    onExit: () -> Unit,
) {
    val c = ChangeloomTheme.colors
    val selection = state.selection
    val query = state.query.trim()
    val rows = remember(selection?.tree, query) { selection?.tree?.let { topicRows(it, query) }.orEmpty() }

    Box(Modifier.fillMaxSize().background(c.bg)) {
        GridBackground(Modifier.fillMaxWidth().height(360.dp))
        SpotlightGlow(Modifier.fillMaxWidth().height(420.dp))
        Column(Modifier.fillMaxSize().windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Horizontal + WindowInsetsSides.Top))) {
            PickerHeader(mode, onExit)
            SearchField(
                state.query,
                onQueryChange,
                enabled = selection != null,
                modifier = Modifier.padding(horizontal = Spacing.gutter).padding(top = 20.dp).enter(delayMillis = 240),
            )
            StatusBanner(
                state.error,
                Icons.Rounded.ErrorOutline,
                Modifier.padding(horizontal = Spacing.gutter).padding(top = 12.dp),
                tone = BannerTone.Error,
                actionLabel = if (selection == null) stringResource(R.string.retry) else null,
                onAction = onRetry,
            )
            LazyColumn(
                Modifier.weight(1f).fillMaxWidth(),
                contentPadding = PaddingValues(horizontal = Spacing.gutter, vertical = 16.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                when {
                    selection == null -> if (state.loading) items(SKELETON_CARDS) { SkeletonTopicCard() }
                    rows.isEmpty() -> item {
                        EmptyState(
                            title = stringResource(R.string.topics_empty_title),
                            message = stringResource(R.string.topics_empty_body),
                            art = { IconTile(Icons.Rounded.SearchOff) },
                        )
                    }
                    else -> itemsIndexed(rows, key = { _, row -> row.root.slug }) { index, row ->
                        RootTopicCard(
                            row = row,
                            selection = selection,
                            expanded = row.root.slug in state.expanded || row.matchedChildren,
                            onToggle = onToggle,
                            onExpand = { onExpand(row.root.slug) },
                            modifier = Modifier.enter(delayMillis = if (index < STAGGERED_CARDS) 300 + index * 50 else 0),
                        )
                    }
                }
            }
            PickerBar(mode, state, onSelectAll, onClear, onSave)
        }
    }
}

@Composable
private fun PickerHeader(mode: TopicPickerMode, onExit: () -> Unit) {
    val c = ChangeloomTheme.colors
    val onboarding = mode == TopicPickerMode.Onboarding
    Column(Modifier.fillMaxWidth().padding(horizontal = Spacing.gutter).padding(top = 8.dp)) {
        Row(Modifier.fillMaxWidth().height(48.dp), verticalAlignment = Alignment.CenterVertically) {
            if (onboarding) {
                LoomMark(Modifier.size(30.dp))
                Spacer(Modifier.weight(1f))
                TextAction(stringResource(R.string.sign_out), onExit, color = c.fgMuted)
            } else {
                IconButton(onClick = onExit) {
                    Icon(Icons.AutoMirrored.Rounded.ArrowBack, contentDescription = stringResource(R.string.back), tint = c.fg)
                }
            }
        }
        Spacer(Modifier.height(12.dp))
        Eyebrow(stringResource(if (onboarding) R.string.topics_onboarding_eyebrow else R.string.topics_edit_eyebrow), Modifier.enter(delayMillis = 0), color = c.primaryText)
        Spacer(Modifier.height(8.dp))
        Column(Modifier.enter(delayMillis = 80)) {
            if (onboarding) {
                Text(stringResource(R.string.topics_onboarding_title_1), style = MaterialTheme.typography.headlineLarge, color = c.fg)
                GradientText(stringResource(R.string.topics_onboarding_title_2), style = MaterialTheme.typography.headlineLarge)
            } else {
                Text(stringResource(R.string.edit_topics), style = MaterialTheme.typography.headlineLarge, color = c.fg)
            }
        }
        Spacer(Modifier.height(8.dp))
        Text(
            if (onboarding) {
                stringResource(R.string.topics_onboarding_body)
            } else {
                stringResource(R.string.topics_edit_body)
            },
            Modifier.enter(delayMillis = 160),
            style = MaterialTheme.typography.bodyMedium,
            color = c.fgMuted,
        )
    }
}

@Composable
private fun SearchField(query: String, onChange: (String) -> Unit, enabled: Boolean, modifier: Modifier = Modifier) {
    ChangeloomTextField(
        value = query,
        onValueChange = onChange,
        modifier = modifier.fillMaxWidth(),
        enabled = enabled,
        placeholder = stringResource(R.string.search_topics),
        leadingIcon = Icons.Rounded.Search,
        trailing = if (query.isEmpty()) {
            null
        } else {
            { IconButton(onClick = { onChange("") }) { Icon(Icons.Rounded.Close, contentDescription = stringResource(R.string.clear_search)) } }
        },
        keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
    )
}

/** A root topic and the children to show under it; [matchedChildren] means the search matched only children. */
private data class TopicRow(val root: Topic, val children: List<Topic>, val matchedChildren: Boolean)

private fun topicRows(tree: TopicTree, query: String): List<TopicRow> = tree.roots.mapNotNull { root ->
    val children = tree.childrenOf(root.slug)
    when {
        query.isEmpty() || root.name.contains(query, ignoreCase = true) -> TopicRow(root, children, matchedChildren = false)
        else -> children.filter { it.name.contains(query, ignoreCase = true) }
            .takeIf { it.isNotEmpty() }
            ?.let { TopicRow(root, it, matchedChildren = true) }
    }
}

@Composable
private fun RootTopicCard(
    row: TopicRow,
    selection: TopicSelection,
    expanded: Boolean,
    onToggle: (String) -> Unit,
    onExpand: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val c = ChangeloomTheme.colors
    val root = row.root
    val state = selection.stateOf(root.slug)
    val hasChildren = row.children.isNotEmpty()
    val open = hasChildren && expanded
    val border by animateColorAsState(
        if (state == CheckState.Unchecked) c.line else c.primary.copy(alpha = 0.5f),
        expoTween(Durations.MEDIUM),
        label = "topicBorder",
    )
    val chevron by animateFloatAsState(if (open) 180f else 0f, expoTween(Durations.SLOW), label = "chevron")
    val leaves = selection.tree.leavesUnder(root.slug)
    val picked = leaves.count { selection.stateOf(it) == CheckState.Checked }

    GlassCard(
        modifier.fillMaxWidth(),
        onClick = if (hasChildren) onExpand else { { onToggle(root.slug) } },
        border = SolidColor(border),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            IconTile(topicIcon(root.slug))
            Spacer(Modifier.width(14.dp))
            Column(Modifier.weight(1f)) {
                Text(root.name, style = MaterialTheme.typography.titleMedium, color = c.fg)
                Spacer(Modifier.height(2.dp))
                Eyebrow(
                    when {
                        !hasChildren -> stringResource(if (state == CheckState.Checked) R.string.following else R.string.single_topic)
                        picked == leaves.size -> stringResource(R.string.all_topics, leaves.size)
                        picked > 0 -> stringResource(R.string.topics_selected, picked, leaves.size)
                        else -> pluralStringResource(R.plurals.topic_count, leaves.size, leaves.size)
                    },
                    color = if (picked > 0) c.primaryText else c.fgSubtle,
                )
            }
            TriStateCheck(state, label = stringResource(R.string.follow_topic, root.name), onClick = { onToggle(root.slug) })
            if (hasChildren) {
                Icon(
                    Icons.Rounded.ExpandMore,
                    contentDescription = stringResource(if (open) R.string.collapse_topic else R.string.expand_topic, root.name),
                    tint = c.fgSubtle,
                    modifier = Modifier.rotate(chevron),
                )
            }
        }
        if (root.description.isNotBlank()) {
            Text(
                root.description,
                Modifier.padding(top = 10.dp),
                style = MaterialTheme.typography.bodySmall,
                color = c.fgMuted,
                maxLines = 2,
            )
        }
        AnimatedVisibility(
            open,
            enter = expandVertically(expoTween(Durations.SLOW)) + fadeIn(expoTween(Durations.SLOW)),
            exit = shrinkVertically(expoTween(Durations.MEDIUM)) + fadeOut(expoTween(Durations.FAST)),
        ) {
            FlowRow(
                Modifier.padding(top = 14.dp),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                row.children.forEach { child ->
                    TopicChip(
                        child.name,
                        selected = selection.stateOf(child.slug) == CheckState.Checked,
                        onClick = { onToggle(child.slug) },
                    )
                }
            }
        }
    }
}

/** Gradient checkbox that pops in, with a dash for a partly selected topic. */
@Composable
private fun TriStateCheck(state: CheckState, label: String, onClick: () -> Unit) {
    val c = ChangeloomTheme.colors
    val fill by animateFloatAsState(
        if (state == CheckState.Unchecked) 0f else 1f,
        spring(dampingRatio = 0.55f, stiffness = Spring.StiffnessMedium),
        label = "checkFill",
    )
    Box(
        Modifier
            .size(44.dp)
            .clip(Radius.md)
            .triStateToggleable(state = state.toToggleableState(), role = Role.Checkbox, onClick = onClick)
            .semantics { contentDescription = label },
        contentAlignment = Alignment.Center,
    ) {
        Box(Modifier.size(22.dp).border(1.5.dp, c.lineStrong, Radius.sm))
        Box(
            Modifier
                .size(22.dp)
                .graphicsLayer {
                    scaleX = fill
                    scaleY = fill
                    alpha = fill.coerceIn(0f, 1f)
                }
                .clip(Radius.sm)
                .background(ChangeloomTheme.gradients.button),
        )
        AnimatedContent(
            state,
            transitionSpec = {
                (scaleIn(expoTween(), initialScale = 0.4f) + fadeIn(expoTween())) togetherWith
                    (scaleOut(expoTween(Durations.FAST), targetScale = 0.4f) + fadeOut(expoTween(Durations.FAST)))
            },
            label = "checkIcon",
        ) { s ->
            when (s) {
                CheckState.Checked -> Icon(Icons.Rounded.Check, contentDescription = null, tint = c.primaryFg, modifier = Modifier.size(16.dp))
                CheckState.Partial -> Icon(Icons.Rounded.Remove, contentDescription = null, tint = c.primaryFg, modifier = Modifier.size(16.dp))
                CheckState.Unchecked -> Spacer(Modifier.size(16.dp))
            }
        }
    }
}

private fun CheckState.toToggleableState() = when (this) {
    CheckState.Checked -> ToggleableState.On
    CheckState.Partial -> ToggleableState.Indeterminate
    CheckState.Unchecked -> ToggleableState.Off
}

@Composable
private fun SkeletonTopicCard() {
    GlassCard(Modifier.fillMaxWidth()) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.size(44.dp).clip(Radius.lg).shimmer())
            Spacer(Modifier.width(14.dp))
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                SkeletonBlock(Modifier.fillMaxWidth(0.5f), height = 16.dp)
                SkeletonBlock(Modifier.fillMaxWidth(0.3f), height = 10.dp)
            }
        }
    }
}

/** Sticky footer: bulk actions and the save button, kept above the keyboard and navigation bar. */
@Composable
private fun PickerBar(
    mode: TopicPickerMode,
    state: TopicPickerState,
    onSelectAll: () -> Unit,
    onClear: () -> Unit,
    onSave: () -> Unit,
) {
    val c = ChangeloomTheme.colors
    val selection = state.selection
    val count = selection?.count ?: 0
    val editable = selection != null && !state.saving
    val canSave = selection != null && !selection.isEmpty && (mode == TopicPickerMode.Onboarding || state.dirty)
    Column(
        Modifier
            .fillMaxWidth()
            .background(c.navGlass)
            .drawBehind { drawLine(c.line, Offset.Zero, Offset(size.width, 0f), 1.dp.toPx()) }
            .windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Horizontal + WindowInsetsSides.Bottom))
            .padding(horizontal = Spacing.gutter, vertical = 12.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            TextAction(stringResource(R.string.select_all), onSelectAll, enabled = editable)
            TextAction(stringResource(R.string.clear), onClear, enabled = editable && selection?.isEmpty == false)
            Spacer(Modifier.weight(1f))
            AnimatedContent(count, label = "selectedCount") { n ->
                Eyebrow(pluralStringResource(R.plurals.topic_count, n, n), color = if (n > 0) c.primaryText else c.fgSubtle)
            }
        }
        Spacer(Modifier.height(8.dp))
        PrimaryButton(
            text = stringResource(if (mode == TopicPickerMode.Onboarding) R.string.continue_count else R.string.save_count, count),
            onClick = onSave,
            modifier = Modifier.fillMaxWidth(),
            enabled = canSave,
            loading = state.saving,
            icon = if (mode == TopicPickerMode.Onboarding) Icons.AutoMirrored.Rounded.ArrowForward else Icons.Rounded.Check,
        )
    }
}

private fun topicIcon(slug: String): ImageVector = when (slug.substringBefore('/')) {
    "languages" -> Icons.Rounded.Code
    "web" -> Icons.Rounded.Public
    "mobile" -> Icons.Rounded.PhoneAndroid
    "cloud" -> Icons.Rounded.Cloud
    "databases" -> Icons.Rounded.Storage
    "ai" -> Icons.Rounded.AutoAwesome
    "security" -> Icons.Rounded.Security
    "devtools" -> Icons.Rounded.Build
    else -> Icons.Rounded.Tag
}

private const val SKELETON_CARDS = 5
private const val STAGGERED_CARDS = 6

private val previewTree = TopicTree(
    listOf(
        Topic("languages", "Languages", null, "Programming language releases, proposals and toolchains."),
        Topic("languages/go", "Go", "languages", ""),
        Topic("languages/kotlin", "Kotlin", "languages", ""),
        Topic("languages/rust", "Rust", "languages", ""),
        Topic("languages/swift", "Swift", "languages", ""),
        Topic("mobile", "Mobile", null, "Mobile platforms, SDKs and cross-platform frameworks."),
        Topic("mobile/android", "Android", "mobile", ""),
        Topic("mobile/ios", "iOS", "mobile", ""),
        Topic("cloud", "Cloud & Infrastructure", null, "Cloud providers, containers and infrastructure tooling."),
        Topic("cloud/aws", "AWS", "cloud", ""),
        Topic("cloud/gcp", "Google Cloud", "cloud", ""),
        Topic("ai", "AI & ML", null, "Model releases, SDKs and ML tooling."),
    ),
)

private fun previewState(followed: List<String>, saved: List<String> = emptyList()): TopicPickerState {
    val selection = TopicSelection.fromFollowed(previewTree, followed)
    return TopicPickerState(loading = false, selection = selection, followed = saved, expanded = setOf("languages"))
}

@Composable
private fun PickerPreview(state: TopicPickerState, mode: TopicPickerMode) = TopicPickerContent(
    state, mode, onToggle = {}, onExpand = {}, onQueryChange = {}, onSelectAll = {}, onClear = {}, onSave = {}, onRetry = {}, onExit = {},
)

@Preview(name = "Onboarding, dark", heightDp = 900)
@Composable
private fun TopicPickerOnboardingDark() = ChangeloomTheme(ThemeMode.Dark) {
    PickerPreview(previewState(listOf("languages/kotlin", "languages/go", "mobile")), TopicPickerMode.Onboarding)
}

@Preview(name = "Edit, light", heightDp = 900)
@Composable
private fun TopicPickerEditLight() = ChangeloomTheme(ThemeMode.Light) {
    PickerPreview(previewState(listOf("languages", "cloud/aws"), saved = listOf("languages")), TopicPickerMode.Edit)
}

@Preview(name = "Loading, dark", heightDp = 900)
@Composable
private fun TopicPickerLoadingDark() = ChangeloomTheme(ThemeMode.Dark) {
    PickerPreview(TopicPickerState(), TopicPickerMode.Onboarding)
}
