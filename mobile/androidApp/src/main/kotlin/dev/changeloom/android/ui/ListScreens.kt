package dev.changeloom.android.ui

import androidx.compose.foundation.background
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
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.ArrowForward
import androidx.compose.material.icons.rounded.BookmarkBorder
import androidx.compose.material.icons.rounded.BookmarkRemove
import androidx.compose.material.icons.rounded.Close
import androidx.compose.material.icons.rounded.ErrorOutline
import androidx.compose.material.icons.rounded.Search
import androidx.compose.material.icons.rounded.SearchOff
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.changeloom.android.R
import dev.changeloom.android.ui.components.BannerTone
import dev.changeloom.android.ui.components.ChangeloomTextField
import dev.changeloom.android.ui.components.EmptyState
import dev.changeloom.android.ui.components.Eyebrow
import dev.changeloom.android.ui.components.IconTile
import dev.changeloom.android.ui.components.SecondaryButton
import dev.changeloom.android.ui.components.SpotlightGlow
import dev.changeloom.android.ui.components.StatusBanner
import dev.changeloom.android.ui.components.StatusBarScrim
import dev.changeloom.android.ui.components.TopicChip
import dev.changeloom.android.ui.components.enter
import dev.changeloom.android.ui.theme.ChangeloomTheme
import dev.changeloom.android.ui.theme.Spacing
import dev.changeloom.android.ui.theme.ThemeMode
import dev.changeloom.shared.data.PagerState
import org.koin.androidx.compose.koinViewModel

@Composable
internal fun SearchScreen(onOpen: (Long) -> Unit, contentPadding: PaddingValues, vm: SearchViewModel = koinViewModel()) {
    val state by vm.state.collectAsStateWithLifecycle()
    val query by vm.query.collectAsStateWithLifecycle()
    val picker: TopicPickerViewModel = koinViewModel()
    val pickerState by picker.state.collectAsStateWithLifecycle()
    val tree = pickerState.selection?.tree
    // The user's own tools first, then a few generic ideas.
    val suggestions = remember(tree, pickerState.followed) {
        val followed = tree?.let { t -> pickerState.followed.flatMap { t.leavesUnder(it) }.mapNotNull { t.topic(it)?.name } }.orEmpty()
        (followed + SEARCH_IDEAS).distinct().take(MAX_SUGGESTIONS)
    }
    SearchContent(
        state = state,
        query = query,
        suggestions = suggestions,
        contentPadding = contentPadding,
        onQueryChange = vm::setQuery,
        onSearch = vm::search,
        onSuggestion = { vm.setQuery(it); vm.search() },
        onOpen = onOpen,
        onToggleSave = vm::setBookmarked,
        onLoadMore = vm::loadMore,
        onDismissError = vm::dismissError,
    )
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun SearchContent(
    state: PagerState,
    query: String,
    suggestions: List<String>,
    contentPadding: PaddingValues,
    onQueryChange: (String) -> Unit,
    onSearch: () -> Unit,
    onSuggestion: (String) -> Unit,
    onOpen: (Long) -> Unit,
    onToggleSave: (Long, Boolean) -> Unit,
    onLoadMore: () -> Unit,
    onDismissError: () -> Unit,
) {
    val c = ChangeloomTheme.colors
    val keyboard = LocalSoftwareKeyboardController.current
    val search = { keyboard?.hide(); onSearch() }
    val listState = rememberLazyListState()
    LoadMoreEffect(listState, onLoadMore)

    Box(Modifier.fillMaxSize()) {
        SpotlightGlow(Modifier.fillMaxWidth().height(360.dp))
        // Side insets are applied per child: listPadding() already includes them for the results.
        Column(Modifier.fillMaxSize().windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Top))) {
            Column(Modifier.windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Horizontal)).padding(horizontal = Spacing.gutter).padding(top = 16.dp)) {
                Eyebrow(stringResource(R.string.tab_search), Modifier.enter(delayMillis = 0), color = c.primaryText)
                Spacer(Modifier.height(8.dp))
                Text(stringResource(R.string.search_title), Modifier.enter(delayMillis = 80), style = MaterialTheme.typography.headlineMedium, color = c.fg)
                Spacer(Modifier.height(16.dp))
                SearchField(query, onQueryChange, search, Modifier.enter(delayMillis = 160))
                StatusBanner(
                    state.error,
                    Icons.Rounded.ErrorOutline,
                    Modifier.padding(top = 12.dp),
                    tone = BannerTone.Error,
                    actionLabel = stringResource(R.string.dismiss),
                    onAction = onDismissError,
                )
            }
            when {
                state.loading && state.items.isEmpty() -> Column(
                    Modifier.padding(listPadding(contentPadding, top = false)),
                    verticalArrangement = Arrangement.spacedBy(12.dp),
                ) { repeat(LIST_SKELETON_CARDS) { SkeletonStoryCard() } }
                state.items.isEmpty() && state.loaded -> EmptyState(
                    title = stringResource(R.string.search_empty_title),
                    message = stringResource(R.string.search_empty_body),
                    art = { IconTile(Icons.Rounded.SearchOff) },
                )
                state.items.isEmpty() -> Column(
                    Modifier
                        .windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Horizontal))
                        .padding(horizontal = Spacing.gutter)
                        .padding(top = 24.dp)
                        .enter(delayMillis = 240),
                ) {
                    Eyebrow(stringResource(R.string.search_suggestions))
                    Spacer(Modifier.height(12.dp))
                    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        suggestions.forEach { TopicChip(it, onClick = { keyboard?.hide(); onSuggestion(it) }) }
                    }
                }
                else -> LazyColumn(
                    Modifier.fillMaxSize(),
                    state = listState,
                    contentPadding = listPadding(contentPadding, top = false),
                    verticalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    items(state.items, key = { it.id }) { story ->
                        StoryCard(
                            story,
                            onOpen = { onOpen(story.id) },
                            onToggleSave = { onToggleSave(story.id, !story.isBookmarked) },
                            modifier = Modifier.animateItem(),
                        )
                    }
                    if (state.loadingMore) item(key = "more") { SkeletonStoryCard() }
                }
            }
        }
    }
}

@Composable
private fun SearchField(query: String, onChange: (String) -> Unit, onSearch: () -> Unit, modifier: Modifier = Modifier) {
    val c = ChangeloomTheme.colors
    ChangeloomTextField(
        value = query,
        onValueChange = onChange,
        modifier = modifier.fillMaxWidth(),
        placeholder = stringResource(R.string.search_placeholder),
        leadingIcon = Icons.Rounded.Search,
        trailing = if (query.isEmpty()) {
            null
        } else {
            {
                Row {
                    IconButton(onClick = { onChange("") }) { Icon(Icons.Rounded.Close, contentDescription = stringResource(R.string.clear_search)) }
                    IconButton(onClick = onSearch, enabled = query.isNotBlank()) {
                        Icon(Icons.AutoMirrored.Rounded.ArrowForward, contentDescription = stringResource(R.string.tab_search), tint = c.primaryText)
                    }
                }
            }
        },
        keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
        keyboardActions = KeyboardActions(onSearch = { onSearch() }),
    )
}

/** Saved stories. Reloaded each time the tab opens so changes made elsewhere show up. */
@Composable
internal fun SavedScreen(onOpen: (Long) -> Unit, onBrowse: () -> Unit, contentPadding: PaddingValues, vm: BookmarksViewModel = koinViewModel()) {
    val state by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { vm.refresh() }
    SavedContent(
        state = state,
        contentPadding = contentPadding,
        onOpen = onOpen,
        onToggleSave = vm::setBookmarked,
        onRemove = vm::remove,
        onBrowse = onBrowse,
        onLoadMore = vm::loadMore,
        onDismissError = vm::dismissError,
    )
}

@Composable
internal fun SavedContent(
    state: PagerState,
    contentPadding: PaddingValues,
    onOpen: (Long) -> Unit,
    onToggleSave: (Long, Boolean) -> Unit,
    onRemove: (Long) -> Unit,
    onBrowse: () -> Unit,
    onLoadMore: () -> Unit,
    onDismissError: () -> Unit,
) {
    val c = ChangeloomTheme.colors
    val listState = rememberLazyListState()
    LoadMoreEffect(listState, onLoadMore)

    Box(Modifier.fillMaxSize()) {
        SpotlightGlow(Modifier.fillMaxWidth().height(360.dp))
        LazyColumn(
            Modifier.fillMaxSize(),
            state = listState,
            contentPadding = listPadding(contentPadding),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            item(key = "header") {
                Column(Modifier.padding(top = 8.dp, bottom = 4.dp)) {
                    Eyebrow(stringResource(R.string.tab_saved), Modifier.enter(delayMillis = 0), color = c.primaryText)
                    Spacer(Modifier.height(8.dp))
                    Text(stringResource(R.string.saved_title), Modifier.enter(delayMillis = 80), style = MaterialTheme.typography.headlineMedium, color = c.fg)
                    if (state.items.isNotEmpty()) {
                        Spacer(Modifier.height(8.dp))
                        Text(
                            pluralStringResource(R.plurals.saved_count, state.items.size, state.items.size),
                            Modifier.enter(delayMillis = 160),
                            style = MaterialTheme.typography.bodyMedium,
                            color = c.fgMuted,
                        )
                    }
                    StatusBanner(
                        state.error,
                        Icons.Rounded.ErrorOutline,
                        Modifier.padding(top = 12.dp),
                        tone = BannerTone.Error,
                        actionLabel = stringResource(R.string.dismiss),
                        onAction = onDismissError,
                    )
                }
            }
            when {
                state.items.isEmpty() && (state.loading || !state.loaded) -> items(LIST_SKELETON_CARDS) { SkeletonStoryCard() }
                state.items.isEmpty() -> item(key = "empty") {
                    EmptyState(
                        title = stringResource(R.string.saved_empty_title),
                        message = stringResource(R.string.saved_empty_body),
                        art = { IconTile(Icons.Rounded.BookmarkBorder) },
                        action = { SecondaryButton(stringResource(R.string.browse_feed), onBrowse) },
                    )
                }
                else -> {
                    items(state.items, key = { it.id }) { story ->
                        SwipeActions(
                            startToEnd = null,
                            endToStart = SwipeAction(stringResource(R.string.remove), Icons.Rounded.BookmarkRemove, c.rose, onSwipe = { onRemove(story.id) }),
                            resetAfterSwipe = false,
                            modifier = Modifier.animateItem(),
                        ) {
                            StoryCard(story, onOpen = { onOpen(story.id) }, onToggleSave = { onToggleSave(story.id, !story.isBookmarked) })
                        }
                    }
                    if (state.loadingMore) item(key = "more") { SkeletonStoryCard() }
                }
            }
        }
        StatusBarScrim(listState.canScrollBackward)
    }
}

@Composable
private fun SearchPreview(state: PagerState, query: String) = SearchContent(
    state = state,
    query = query,
    suggestions = listOf("Kotlin", "Android", "Go", "AWS") + SEARCH_IDEAS,
    contentPadding = PaddingValues(bottom = 80.dp),
    onQueryChange = {}, onSearch = {}, onSuggestion = {}, onOpen = {},
    onToggleSave = { _, _ -> }, onLoadMore = {}, onDismissError = {},
)

@Preview(name = "Search, suggestions, dark", heightDp = 800)
@Composable
private fun SearchSuggestionsDark() = ChangeloomTheme(ThemeMode.Dark) {
    Box(Modifier.background(ChangeloomTheme.colors.bg)) { SearchPreview(PagerState(), "") }
}

@Preview(name = "Search, results, light", heightDp = 1000)
@Composable
private fun SearchResultsLight() = ChangeloomTheme(ThemeMode.Light) {
    Box(Modifier.background(ChangeloomTheme.colors.bg)) { SearchPreview(PagerState(items = previewStories(), loaded = true), "kotlin") }
}

@Composable
private fun SavedPreview(state: PagerState) = SavedContent(
    state = state,
    contentPadding = PaddingValues(bottom = 80.dp),
    onOpen = {}, onToggleSave = { _, _ -> }, onRemove = {}, onBrowse = {}, onLoadMore = {}, onDismissError = {},
)

@Preview(name = "Saved, light", heightDp = 1000)
@Composable
private fun SavedLight() = ChangeloomTheme(ThemeMode.Light) {
    Box(Modifier.background(ChangeloomTheme.colors.bg)) {
        SavedPreview(PagerState(items = previewStories().map { it.copy(isBookmarked = true) }, loaded = true))
    }
}

@Preview(name = "Saved, empty, dark", heightDp = 800)
@Composable
private fun SavedEmptyDark() = ChangeloomTheme(ThemeMode.Dark) {
    Box(Modifier.background(ChangeloomTheme.colors.bg)) { SavedPreview(PagerState(loaded = true)) }
}

private val SEARCH_IDEAS = listOf("security", "breaking change", "deprecated")
private const val MAX_SUGGESTIONS = 10

