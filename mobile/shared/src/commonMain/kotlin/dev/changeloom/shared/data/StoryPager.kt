package dev.changeloom.shared.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update

data class PagerState(
    val items: List<StorySummary> = emptyList(),
    val nextCursor: String? = null,
    val loading: Boolean = false,
    val loadingMore: Boolean = false,
    /** True once the first page has been requested, so "no results" is not shown before a load. */
    val loaded: Boolean = false,
    val error: String? = null,
)

/** Cursor-paginated story list (search results, bookmarks). Not cached: these lists need the network. */
class StoryPager(private val fetch: suspend (cursor: String?) -> TimelinePage) {
    private val _state = MutableStateFlow(PagerState())
    val state: StateFlow<PagerState> = _state.asStateFlow()

    /** Replaces the list with the first page. */
    suspend fun refresh() {
        _state.update { it.copy(loading = true, error = null) }
        try {
            val page = fetch(null)
            _state.value = PagerState(items = page.items, nextCursor = page.nextCursor, loaded = true)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            _state.update { it.copy(loading = false, loaded = true, error = e.message ?: "Could not load stories") }
        }
    }

    suspend fun loadMore() {
        val cursor = _state.value.nextCursor ?: return
        if (_state.value.loading || _state.value.loadingMore) return
        _state.update { it.copy(loadingMore = true, error = null) }
        try {
            val page = fetch(cursor)
            _state.update { s ->
                val known = s.items.mapTo(HashSet()) { it.id }
                s.copy(items = s.items + page.items.filter { it.id !in known }, nextCursor = page.nextCursor, loadingMore = false)
            }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            _state.update { it.copy(loadingMore = false, error = e.message ?: "Could not load more") }
        }
    }

    fun setBookmarked(id: Long, on: Boolean) = _state.update { s ->
        s.copy(items = s.items.map { if (it.id == id) it.copy(isBookmarked = on) else it })
    }

    /** Drops a story from the loaded list, e.g. one unsaved from the bookmarks list. */
    fun remove(id: Long) = _state.update { s -> s.copy(items = s.items.filter { it.id != id }) }

    fun clearError() = _state.update { it.copy(error = null) }
}
