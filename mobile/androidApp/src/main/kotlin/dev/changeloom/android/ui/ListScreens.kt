package dev.changeloom.android.ui

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.changeloom.shared.data.PagerState
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filter
import org.koin.androidx.compose.koinViewModel

@Composable
internal fun SearchScreen(onBack: () -> Unit, onOpen: (Long) -> Unit, vm: SearchViewModel = koinViewModel()) {
    val state by vm.state.collectAsStateWithLifecycle()
    val query by vm.query.collectAsStateWithLifecycle()

    Column(Modifier.fillMaxSize()) {
        Row(Modifier.fillMaxWidth().padding(start = 8.dp, end = 16.dp), verticalAlignment = Alignment.CenterVertically) {
            TextButton(onClick = onBack) { Text("← Back") }
            Text("Search", style = MaterialTheme.typography.titleLarge)
        }
        Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically) {
            OutlinedTextField(
                value = query,
                onValueChange = vm::setQuery,
                singleLine = true,
                placeholder = { Text("Search all stories") },
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
                keyboardActions = KeyboardActions(onSearch = { vm.search() }),
                modifier = Modifier.weight(1f),
            )
            Button(onClick = vm::search, enabled = query.isNotBlank(), modifier = Modifier.padding(start = 8.dp)) { Text("Go") }
        }
        StoryResults(
            state = state,
            emptyText = if (state.loaded) "No stories match." else "Search titles, summaries and articles.",
            onOpen = onOpen,
            onToggleBookmark = vm::setBookmarked,
            onLoadMore = vm::loadMore,
            onDismissError = vm::dismissError,
        )
    }
}

@Composable
internal fun BookmarksScreen(onBack: () -> Unit, onOpen: (Long) -> Unit, vm: BookmarksViewModel = koinViewModel()) {
    val state by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { vm.refresh() }

    Column(Modifier.fillMaxSize()) {
        Row(Modifier.fillMaxWidth().padding(start = 8.dp, end = 16.dp), verticalAlignment = Alignment.CenterVertically) {
            TextButton(onClick = onBack) { Text("← Back") }
            Text("Saved stories", style = MaterialTheme.typography.titleLarge)
        }
        StoryResults(
            state = state,
            emptyText = "Nothing saved yet.",
            onOpen = onOpen,
            onToggleBookmark = vm::setBookmarked,
            onLoadMore = vm::loadMore,
            onDismissError = vm::dismissError,
        )
    }
}

@Composable
private fun StoryResults(
    state: PagerState,
    emptyText: String,
    onOpen: (Long) -> Unit,
    onToggleBookmark: (Long, Boolean) -> Unit,
    onLoadMore: () -> Unit,
    onDismissError: () -> Unit,
) {
    val listState = rememberLazyListState()
    LaunchedEffect(listState, state.items.size) {
        snapshotFlow { listState.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: -1 }
            .distinctUntilChanged()
            .filter { it >= state.items.size - LOAD_MORE_THRESHOLD }
            .collect { onLoadMore() }
    }

    state.error?.let { StatusLine(it, MaterialTheme.colorScheme.error, action = "Dismiss", onAction = onDismissError) }
    when {
        state.loading && state.items.isEmpty() -> Box(Modifier.fillMaxSize(), Alignment.Center) { CircularProgressIndicator() }
        state.items.isEmpty() -> Text(emptyText, style = MaterialTheme.typography.bodyLarge, modifier = Modifier.padding(24.dp))
        else -> LazyColumn(Modifier.fillMaxSize(), state = listState) {
            items(state.items, key = { it.id }) { story ->
                StoryRow(story, onOpen = { onOpen(story.id) }, onToggleBookmark = { onToggleBookmark(story.id, !story.isBookmarked) })
                HorizontalDivider()
            }
            if (state.loadingMore) {
                item { Box(Modifier.fillMaxWidth().padding(16.dp), Alignment.Center) { CircularProgressIndicator() } }
            }
        }
    }
}
