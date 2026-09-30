package dev.changeloom.android.ui

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.changeloom.shared.data.StorySummary
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filter
import org.koin.androidx.compose.koinViewModel
import org.koin.core.parameter.parametersOf

private enum class Screen { Timeline, Search, Bookmarks, Settings }

/**
 * No nav library: the current story id and screen survive rotation via rememberSaveable.
 * [openStoryId] is a story to open right away (a tapped push notification); [onOpenStoryHandled] clears it.
 */
@Composable
fun MainScreen(openStoryId: Long? = null, onOpenStoryHandled: () -> Unit = {}) {
    var storyId by rememberSaveable { mutableStateOf<Long?>(null) }
    var screen by rememberSaveable { mutableStateOf(Screen.Timeline) }

    LaunchedEffect(openStoryId) {
        if (openStoryId != null) {
            storyId = openStoryId
            onOpenStoryHandled()
        }
    }

    val open = storyId
    val backToTimeline = { screen = Screen.Timeline }
    when {
        open != null -> {
            BackHandler { storyId = null }
            StoryDetailScreen(open, onBack = { storyId = null })
        }
        screen == Screen.Settings -> {
            BackHandler(onBack = backToTimeline)
            SettingsScreen(onBack = backToTimeline)
        }
        screen == Screen.Search -> {
            BackHandler(onBack = backToTimeline)
            SearchScreen(onBack = backToTimeline, onOpen = { storyId = it })
        }
        screen == Screen.Bookmarks -> {
            BackHandler(onBack = backToTimeline)
            BookmarksScreen(onBack = backToTimeline, onOpen = { storyId = it })
        }
        else -> TimelineScreen(
            onOpen = { storyId = it },
            onSearch = { screen = Screen.Search },
            onBookmarks = { screen = Screen.Bookmarks },
            onSettings = { screen = Screen.Settings },
        )
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun TimelineScreen(
    onOpen: (Long) -> Unit,
    onSearch: () -> Unit,
    onBookmarks: () -> Unit,
    onSettings: () -> Unit,
    vm: TimelineViewModel = koinViewModel(),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val listState = rememberLazyListState()

    LaunchedEffect(listState, state.items.size) {
        snapshotFlow { listState.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: -1 }
            .distinctUntilChanged()
            .filter { it >= state.items.size - LOAD_MORE_THRESHOLD }
            .collect { vm.loadMore() }
    }

    Column(Modifier.fillMaxSize()) {
        Row(Modifier.fillMaxWidth().padding(start = 16.dp, end = 8.dp), verticalAlignment = Alignment.CenterVertically) {
            Text("Changeloom", style = MaterialTheme.typography.titleLarge, modifier = Modifier.weight(1f))
            TextButton(onClick = onSearch) { Text("Search") }
            TextButton(onClick = onBookmarks) { Text("Saved") }
            TextButton(onClick = onSettings) { Text("Settings") }
        }
        if (state.offline) StatusLine("Offline — showing saved stories", MaterialTheme.colorScheme.onSurfaceVariant)
        state.error?.let { StatusLine(it, MaterialTheme.colorScheme.error, action = "Dismiss", onAction = vm::dismissError) }
        PullToRefreshBox(isRefreshing = state.refreshing, onRefresh = vm::refresh, modifier = Modifier.weight(1f)) {
            if (state.items.isEmpty() && !state.refreshing) {
                Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(24.dp)) {
                    Text("No stories yet. Pull down to refresh.", style = MaterialTheme.typography.bodyLarge)
                }
            } else {
                LazyColumn(Modifier.fillMaxSize(), state = listState) {
                    items(state.items, key = { it.id }) { story ->
                        StoryRow(
                            story,
                            onOpen = { onOpen(story.id) },
                            onToggleBookmark = { vm.setBookmarked(story.id, !story.isBookmarked) },
                            onToggleRead = { vm.setRead(story.id, !story.isRead) },
                        )
                        HorizontalDivider()
                    }
                    if (state.loadingMore) {
                        item { Box(Modifier.fillMaxWidth().padding(16.dp), Alignment.Center) { CircularProgressIndicator() } }
                    }
                }
            }
        }
    }
}

@Composable
internal fun StatusLine(text: String, color: androidx.compose.ui.graphics.Color, action: String? = null, onAction: () -> Unit = {}) {
    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically) {
        Text(text, color = color, style = MaterialTheme.typography.bodySmall, modifier = Modifier.weight(1f))
        if (action != null) TextButton(onClick = onAction) { Text(action) }
    }
}

@Composable
internal fun StoryRow(
    story: StorySummary,
    onOpen: () -> Unit,
    onToggleBookmark: () -> Unit,
    onToggleRead: (() -> Unit)? = null,
) {
    Column(Modifier.fillMaxWidth().clickable(onClick = onOpen).padding(horizontal = 16.dp, vertical = 12.dp)) {
        Text(
            listOfNotNull(story.kind.uppercase(), story.severity?.uppercase(), story.publishedAt.take(DATE_LENGTH)).joinToString(" · "),
            style = MaterialTheme.typography.labelSmall,
            color = if (story.severity == "critical" || story.severity == "high") MaterialTheme.colorScheme.error
            else MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Text(
            story.title,
            style = MaterialTheme.typography.titleMedium,
            fontWeight = if (story.isRead) FontWeight.Normal else FontWeight.Bold,
            color = if (story.isRead) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onSurface,
        )
        Text(story.summary, style = MaterialTheme.typography.bodyMedium, maxLines = SUMMARY_LINES)
        Row {
            TextButton(onClick = onToggleBookmark) { Text(if (story.isBookmarked) "Saved ✓" else "Save") }
            if (onToggleRead != null) {
                TextButton(onClick = onToggleRead) { Text(if (story.isRead) "Mark unread" else "Mark read") }
            }
        }
    }
}

@Composable
private fun StoryDetailScreen(id: Long, onBack: () -> Unit) {
    val vm: StoryDetailViewModel = koinViewModel(key = "story-$id") { parametersOf(id) }
    val timeline: TimelineViewModel = koinViewModel()
    val state by vm.state.collectAsStateWithLifecycle()
    val timelineState by timeline.state.collectAsStateWithLifecycle()
    val uriHandler = LocalUriHandler.current
    val story = state.story
    val isRead = timelineState.items.firstOrNull { it.id == id }?.isRead ?: story?.isRead ?: false

    Column(Modifier.fillMaxSize()) {
        TextButton(onClick = onBack, modifier = Modifier.padding(start = 8.dp)) { Text("← Back") }
        when {
            state.loading -> Box(Modifier.fillMaxSize(), Alignment.Center) { CircularProgressIndicator() }
            story == null -> Column(Modifier.padding(24.dp)) {
                Text(state.error ?: "Could not load story", color = MaterialTheme.colorScheme.error)
                TextButton(onClick = vm::load) { Text("Retry") }
            }
            else -> Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(horizontal = 16.dp)) {
                Text(
                    listOfNotNull(story.kind.uppercase(), story.severity?.uppercase(), story.publishedAt.take(DATE_LENGTH))
                        .joinToString(" · "),
                    style = MaterialTheme.typography.labelSmall,
                )
                Text(story.title, style = MaterialTheme.typography.headlineSmall)
                Spacer(Modifier.height(12.dp))
                MarkdownText(story.bodyMd)
                if (story.sources.isNotEmpty()) {
                    Spacer(Modifier.height(20.dp))
                    Text("Sources", style = MaterialTheme.typography.titleMedium)
                    story.sources.forEach { source ->
                        TextButton(onClick = { uriHandler.openUri(source.url) }) { Text(source.name) }
                    }
                }
                OutlinedButton(
                    onClick = { vm.setBookmarked(!story.isBookmarked) },
                    modifier = Modifier.fillMaxWidth().padding(top = 16.dp),
                ) { Text(if (story.isBookmarked) "Remove bookmark" else "Bookmark") }
                OutlinedButton(
                    onClick = { vm.setRead(!isRead) },
                    modifier = Modifier.fillMaxWidth().padding(vertical = 16.dp),
                ) { Text(if (isRead) "Mark unread" else "Mark read") }
            }
        }
    }
}

@Composable
private fun SettingsScreen(onBack: () -> Unit, vm: SettingsViewModel = koinViewModel()) {
    val state by vm.state.collectAsStateWithLifecycle()
    Column(Modifier.fillMaxSize().padding(16.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            TextButton(onClick = onBack) { Text("← Back") }
            Text("Settings", style = MaterialTheme.typography.titleLarge)
        }
        vm.email?.let { Text("Signed in as $it", style = MaterialTheme.typography.bodyMedium) }
        Spacer(Modifier.height(12.dp))
        Text("Followed topics", style = MaterialTheme.typography.titleMedium)
        state.error?.let {
            Text(it, color = MaterialTheme.colorScheme.error)
            TextButton(onClick = vm::load) { Text("Retry") }
        }
        if (state.loading) {
            Box(Modifier.weight(1f).fillMaxWidth(), Alignment.Center) { CircularProgressIndicator() }
        } else {
            LazyColumn(Modifier.weight(1f)) {
                items(state.topics, key = { it.slug }) { topic ->
                    Row(
                        Modifier.fillMaxWidth().clickable { vm.toggle(topic.slug) }
                            .padding(start = if (topic.parent == null) 0.dp else 24.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Checkbox(topic.slug in state.selected, { vm.toggle(topic.slug) })
                        Text(topic.name, style = MaterialTheme.typography.bodyLarge)
                    }
                }
            }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
            Button(vm::save, enabled = state.selected.isNotEmpty() && !state.saving && !state.loading) { Text("Save topics") }
            if (state.saved) Text("Saved", style = MaterialTheme.typography.bodySmall)
            Spacer(Modifier.weight(1f))
            OutlinedButton(vm::signOut) { Text("Sign out") }
        }
    }
}

internal const val LOAD_MORE_THRESHOLD = 3
private const val DATE_LENGTH = 10
private const val SUMMARY_LINES = 3
