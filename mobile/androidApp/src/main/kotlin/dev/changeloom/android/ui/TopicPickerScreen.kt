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
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
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
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.selection.triStateToggleable
import androidx.compose.foundation.shape.CircleShape
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
import androidx.compose.material.icons.rounded.Handyman
import androidx.compose.material.icons.rounded.Home
import androidx.compose.material.icons.rounded.Dashboard
import androidx.compose.material.icons.rounded.Edit
import androidx.compose.material.icons.rounded.Psychology
import androidx.compose.material.icons.rounded.Work
import androidx.compose.material.icons.rounded.AccountBalance
import androidx.compose.material.icons.rounded.SportsSoccer
import androidx.compose.material.icons.rounded.SportsEsports
import androidx.compose.material.icons.rounded.Science
import androidx.compose.material.icons.rounded.School
import androidx.compose.material.icons.rounded.RocketLaunch
import androidx.compose.material.icons.rounded.Restaurant
import androidx.compose.material.icons.rounded.PhotoCamera
import androidx.compose.material.icons.rounded.Paid
import androidx.compose.material.icons.rounded.MusicNote
import androidx.compose.material.icons.rounded.Movie
import androidx.compose.material.icons.rounded.Medication
import androidx.compose.material.icons.rounded.MedicalServices
import androidx.compose.material.icons.rounded.LocalShipping
import androidx.compose.material.icons.rounded.Gavel
import androidx.compose.material.icons.rounded.Flight
import androidx.compose.material.icons.rounded.FitnessCenter
import androidx.compose.material.icons.rounded.Engineering
import androidx.compose.material.icons.rounded.Eco
import androidx.compose.material.icons.rounded.Campaign
import androidx.compose.material.icons.rounded.Brush
import androidx.compose.material.icons.rounded.Analytics
import androidx.compose.material.icons.rounded.Agriculture
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
import dev.changeloom.shared.data.Profession
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
        onToggleProfession = vm::toggleProfession,
        onStep = vm::setStep,
        onExpand = vm::toggleExpanded,
        onQueryChange = vm::setQuery,
        onSelectAll = vm::selectAll,
        onClear = vm::clear,
        onSave = vm::save,
        onRetry = vm::load,
        onExit = exit,
    )
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun TopicPickerContent(
    state: TopicPickerState,
    mode: TopicPickerMode,
    onToggle: (String) -> Unit,
    onToggleProfession: (String) -> Unit,
    onStep: (PickerStep) -> Unit,
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
    val professionStep = state.step == PickerStep.Professions && state.professions.isNotEmpty()
    val suggested = remember(state.professions, state.selectedProfessions) { suggestedRoots(state.professions, state.selectedProfessions) }
    val rows = remember(selection?.tree, query, suggested) { selection?.tree?.let { topicRows(it, query, suggested) }.orEmpty() }
    val professionList = remember(state.professions, query) {
        state.professions.filter { query.isEmpty() || it.name.contains(query, ignoreCase = true) }
    }
    val listState = rememberLazyListState()
    val staggered = rememberStaggered(listState)
    // The two steps share one list, so moving between them starts from the top.
    LaunchedEffect(professionStep) { listState.scrollToItem(0) }

    Box(Modifier.fillMaxSize().background(c.bg)) {
        GridBackground(Modifier.fillMaxWidth().height(360.dp))
        SpotlightGlow(Modifier.fillMaxWidth().height(420.dp))
        Column(Modifier.fillMaxSize().windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Horizontal + WindowInsetsSides.Top))) {
            PickerHeader(mode, professionStep, onExit)
            SearchField(
                state.query,
                onQueryChange,
                placeholder = stringResource(if (professionStep) R.string.search_professions else R.string.search_topics),
                enabled = selection != null,
                modifier = Modifier.padding(horizontal = Spacing.gutter).padding(top = 12.dp).enter(delayMillis = 240),
            )
            StatusBanner(
                state.error,
                Icons.Rounded.ErrorOutline,
                Modifier.padding(horizontal = Spacing.gutter).padding(top = 8.dp),
                tone = BannerTone.Error,
                actionLabel = if (selection == null) stringResource(R.string.retry) else null,
                onAction = onRetry,
            )
            LazyColumn(
                Modifier.weight(1f).fillMaxWidth(),
                state = listState,
                contentPadding = PaddingValues(horizontal = Spacing.gutter, vertical = 12.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                when {
                    selection == null -> if (state.loading) items(SKELETON_CARDS) { SkeletonTopicCard() }
                    professionStep && professionList.isEmpty() -> item {
                        EmptyState(
                            title = stringResource(R.string.professions_empty_title),
                            message = stringResource(R.string.professions_empty_body),
                            art = { IconTile(Icons.Rounded.SearchOff) },
                        )
                    }
                    professionStep -> items(professionList, key = { it.slug }) { profession ->
                        val picked = profession.slug in state.selectedProfessions
                        ProfessionCard(
                            profession,
                            picked = picked,
                            onClick = { onToggleProfession(profession.slug) },
                        )
                    }
                    rows.isEmpty() -> item {
                        EmptyState(
                            title = stringResource(R.string.topics_empty_title),
                            message = stringResource(R.string.topics_empty_body),
                            art = { IconTile(Icons.Rounded.SearchOff) },
                        )
                    }
                    else -> {
                        // Sections only when professions suggest areas and nothing is being searched.
                        val sections = query.isEmpty() && rows.any { it.suggested } && rows.any { !it.suggested }
                        rows.forEachIndexed { index, row ->
                            if (sections && index == 0) item(key = "suggested") { SectionLabel(stringResource(R.string.suggested_areas)) }
                            if (sections && !row.suggested && rows[index - 1].suggested) {
                                item(key = "more") { SectionLabel(stringResource(R.string.more_areas)) }
                            }
                            item(key = row.root.slug) {
                                RootTopicCard(
                                    row = row,
                                    selection = selection,
                                    expanded = row.root.slug in state.expanded || row.matchedChildren,
                                    onToggle = onToggle,
                                    onExpand = { onExpand(row.root.slug) },
                                    // Only the first cards, on first show: cards scrolled into view appear at once.
                                    modifier = if (staggered && index < STAGGERED_CARDS) Modifier.enter(delayMillis = 300 + index * 50) else Modifier,
                                )
                            }
                        }
                    }
                }
            }
            if (professionStep) {
                ProfessionBar(state, onNext = { onStep(PickerStep.Topics) })
            } else {
                PickerBar(mode, state, onSelectAll, onClear, onSave, onEditProfessions = { onStep(PickerStep.Professions) })
            }
        }
    }
}

@Composable
private fun PickerHeader(
    mode: TopicPickerMode,
    professionStep: Boolean,
    onExit: () -> Unit,
) {
    val c = ChangeloomTheme.colors
    val onboarding = mode == TopicPickerMode.Onboarding
    Column(Modifier.fillMaxWidth().padding(horizontal = Spacing.gutter).padding(top = 0.dp)) {
        Row(Modifier.fillMaxWidth().height(44.dp), verticalAlignment = Alignment.CenterVertically) {
            if (onboarding) {
                LoomMark(Modifier.size(30.dp))
                Spacer(Modifier.weight(1f))
                TextAction(stringResource(R.string.sign_out), onExit, color = c.fgMuted)
            } else {
                Box(
                    Modifier
                        .size(40.dp)
                        .clip(CircleShape)
                        .background(c.navGlass)
                        .border(1.dp, c.line, CircleShape)
                        .clickable(role = Role.Button, onClickLabel = stringResource(R.string.back), onClick = onExit),
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(Icons.AutoMirrored.Rounded.ArrowBack, contentDescription = stringResource(R.string.back), tint = c.fg, modifier = Modifier.size(20.dp))
                }
            }
        }
        Spacer(Modifier.height(4.dp))
        val eyebrow = when {
            professionStep -> R.string.professions_eyebrow
            onboarding -> R.string.topics_onboarding_eyebrow
            else -> R.string.topics_edit_eyebrow
        }
        Eyebrow(stringResource(eyebrow), Modifier.enter(delayMillis = 0), color = c.primaryText)
        Spacer(Modifier.height(4.dp))
        Column(Modifier.enter(delayMillis = 80)) {
            if (professionStep) {
                Text(stringResource(R.string.professions_title_1), style = MaterialTheme.typography.headlineMedium, color = c.fg)
                GradientText(stringResource(R.string.professions_title_2), style = MaterialTheme.typography.headlineMedium)
            } else if (onboarding) {
                Text(stringResource(R.string.topics_onboarding_title_1), style = MaterialTheme.typography.headlineMedium, color = c.fg)
                GradientText(stringResource(R.string.topics_onboarding_title_2), style = MaterialTheme.typography.headlineMedium)
            } else {
                Text(stringResource(R.string.edit_topics), style = MaterialTheme.typography.headlineMedium, color = c.fg)
            }
        }
        Spacer(Modifier.height(4.dp))
        Text(
            when {
                professionStep -> stringResource(R.string.professions_body)
                onboarding -> stringResource(R.string.topics_onboarding_body)
                else -> stringResource(R.string.topics_edit_body)
            },
            Modifier.enter(delayMillis = 160),
            style = MaterialTheme.typography.bodyMedium,
            color = c.fgMuted,
        )
    }
}

@Composable
private fun SearchField(query: String, onChange: (String) -> Unit, placeholder: String, enabled: Boolean, modifier: Modifier = Modifier) {
    ChangeloomTextField(
        value = query,
        onValueChange = onChange,
        modifier = modifier.fillMaxWidth(),
        enabled = enabled,
        placeholder = placeholder,
        leadingIcon = Icons.Rounded.Search,
        trailing = if (query.isEmpty()) {
            null
        } else {
            { IconButton(onClick = { onChange("") }) { Icon(Icons.Rounded.Close, contentDescription = stringResource(R.string.clear_search)) } }
        },
        keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
    )
}

/**
 * A root topic and the children to show under it; [matchedChildren] means the search matched only children and
 * [suggested] that one of the chosen professions lists this area.
 */
private data class TopicRow(val root: Topic, val children: List<Topic>, val matchedChildren: Boolean, val suggested: Boolean = false)

/** Root slugs of the chosen professions, in the order the professions were picked, then each profession's display order. */
private fun suggestedRoots(professions: List<Profession>, chosen: List<String>): List<String> {
    val bySlug = professions.associateBy { it.slug }
    return chosen.flatMap { bySlug[it]?.topics.orEmpty() }.distinct()
}

/** Matching roots with the [suggested] areas first (in that order), then the rest in catalog order. */
private fun topicRows(tree: TopicTree, query: String, suggested: List<String>): List<TopicRow> {
    val rank = suggested.withIndex().associate { it.value to it.index }
    val rows = tree.roots.mapNotNull { root ->
        val children = tree.childrenOf(root.slug)
        val isSuggested = root.slug in rank
        when {
            query.isEmpty() || root.name.contains(query, ignoreCase = true) -> TopicRow(root, children, matchedChildren = false, suggested = isSuggested)
            else -> children.filter { it.name.contains(query, ignoreCase = true) }
                .takeIf { it.isNotEmpty() }
                ?.let { TopicRow(root, it, matchedChildren = true, suggested = isSuggested) }
        }
    }
    return rows.sortedBy { rank[it.root.slug] ?: Int.MAX_VALUE } // stable: unsuggested keep catalog order
}

@Composable
private fun SectionLabel(text: String) {
    Eyebrow(text, Modifier.padding(top = 4.dp), color = ChangeloomTheme.colors.fgSubtle)
}

private val PICKER_CARD_PADDING = PaddingValues(horizontal = 12.dp, vertical = 8.dp)
private val PICKER_TILE_SIZE = 36.dp

@Composable
private fun ProfessionCard(profession: Profession, picked: Boolean, onClick: () -> Unit, modifier: Modifier = Modifier) {
    val c = ChangeloomTheme.colors
    val border by animateColorAsState(if (picked) c.primary.copy(alpha = 0.5f) else c.line, expoTween(Durations.MEDIUM), label = "professionBorder")
    GlassCard(
        modifier.fillMaxWidth(),
        onClick = onClick,
        border = SolidColor(border),
        contentPadding = PICKER_CARD_PADDING,
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            IconTile(professionIcon(profession.slug), size = PICKER_TILE_SIZE)
            Spacer(Modifier.width(12.dp))
            Column(Modifier.weight(1f)) {
                Text(profession.name, style = MaterialTheme.typography.titleSmall, color = c.fg)
                if (profession.description.isNotBlank()) {
                    Text(profession.description, style = MaterialTheme.typography.bodySmall, color = c.fgMuted, maxLines = 1)
                }
            }
            TriStateCheck(
                if (picked) CheckState.Checked else CheckState.Unchecked,
                label = stringResource(R.string.follow_topic, profession.name),
                onClick = onClick,
            )
        }
    }
}

/** Footer of the profession step: how many are chosen and the way on (professions are optional). */
@Composable
private fun ProfessionBar(state: TopicPickerState, onNext: () -> Unit) {
    val c = ChangeloomTheme.colors
    val count = state.selectedProfessions.size
    Column(
        Modifier
            .fillMaxWidth()
            .background(c.navGlass)
            .drawBehind { drawLine(c.line, Offset.Zero, Offset(size.width, 0f), 1.dp.toPx()) }
            .windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Horizontal + WindowInsetsSides.Bottom))
            .padding(horizontal = Spacing.gutter, vertical = 8.dp),
    ) {
        Eyebrow(
            stringResource(R.string.professions_selected, count),
            color = if (count > 0) c.primaryText else c.fgSubtle,
        )
        Spacer(Modifier.height(8.dp))
        PrimaryButton(
            text = stringResource(if (count > 0) R.string.professions_next else R.string.professions_skip),
            onClick = onNext,
            modifier = Modifier.fillMaxWidth(),
            enabled = !state.saving,
            icon = Icons.AutoMirrored.Rounded.ArrowForward,
        )
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
        contentPadding = PICKER_CARD_PADDING,
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            IconTile(topicIcon(root.slug), size = PICKER_TILE_SIZE)
            Spacer(Modifier.width(12.dp))
            Column(Modifier.weight(1f)) {
                Text(root.name, style = MaterialTheme.typography.titleSmall, color = c.fg)
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
                Modifier.padding(top = 6.dp),
                style = MaterialTheme.typography.bodySmall,
                color = c.fgMuted,
                maxLines = 1,
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
    onEditProfessions: () -> Unit,
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
            .padding(horizontal = Spacing.gutter, vertical = 8.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            TextAction(stringResource(R.string.select_all), onSelectAll, enabled = editable)
            TextAction(stringResource(R.string.clear), onClear, enabled = editable && selection?.isEmpty == false)
            if (state.professions.isNotEmpty()) {
                TextAction(stringResource(R.string.back_to_professions), onEditProfessions, enabled = editable, icon = Icons.Rounded.Edit)
            }
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

private fun professionIcon(slug: String): ImageVector = when (slug) {
    "software-engineer" -> Icons.Rounded.Code
    "data-scientist" -> Icons.Rounded.Analytics
    "ml-engineer" -> Icons.Rounded.Psychology
    "cybersecurity" -> Icons.Rounded.Security
    "it-devops" -> Icons.Rounded.Cloud
    "product-manager" -> Icons.Rounded.Dashboard
    "ux-designer", "visual-artist" -> Icons.Rounded.Brush
    "game-developer", "gamer" -> Icons.Rounded.SportsEsports
    "doctor", "nurse", "dentist", "veterinarian", "physiotherapist", "psychologist" -> Icons.Rounded.MedicalServices
    "pharmacist" -> Icons.Rounded.Medication
    "biologist", "chemist", "physicist", "science-enthusiast" -> Icons.Rounded.Science
    "environmental-scientist" -> Icons.Rounded.Eco
    "teacher", "professor-researcher", "student" -> Icons.Rounded.School
    "founder" -> Icons.Rounded.RocketLaunch
    "marketer", "content-creator" -> Icons.Rounded.Campaign
    "lawyer" -> Icons.Rounded.Gavel
    "public-servant", "finance-professional", "accountant" -> Icons.Rounded.AccountBalance
    "investor" -> Icons.Rounded.Paid
    "civil-engineer", "mechanical-engineer", "electrical-engineer", "aerospace-engineer", "architect" -> Icons.Rounded.Engineering
    "construction-worker", "electrician-plumber", "automotive-technician", "manufacturing-worker" -> Icons.Rounded.Handyman
    "farmer" -> Icons.Rounded.Agriculture
    "logistics-pro" -> Icons.Rounded.LocalShipping
    "pilot-aviation", "traveler" -> Icons.Rounded.Flight
    "hospitality-worker", "foodie" -> Icons.Rounded.Restaurant
    "real-estate-agent", "parent" -> Icons.Rounded.Home
    "writer-journalist" -> Icons.Rounded.Edit
    "photographer" -> Icons.Rounded.PhotoCamera
    "musician", "music-fan" -> Icons.Rounded.MusicNote
    "filmmaker", "entertainment-fan" -> Icons.Rounded.Movie
    "fitness-wellness" -> Icons.Rounded.FitnessCenter
    "sports-fan" -> Icons.Rounded.SportsSoccer
    "world-affairs" -> Icons.Rounded.Public
    "tech-enthusiast" -> Icons.Rounded.AutoAwesome
    else -> Icons.Rounded.Work
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
    state, mode, onToggle = {}, onToggleProfession = {}, onStep = {}, onExpand = {}, onQueryChange = {}, onSelectAll = {}, onClear = {}, onSave = {}, onRetry = {}, onExit = {},
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
