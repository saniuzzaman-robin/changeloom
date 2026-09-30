package dev.changeloom.shared.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlin.time.Instant

data class TimelineState(
    val items: List<StorySummary> = emptyList(),
    val nextCursor: String? = null,
    val refreshing: Boolean = false,
    val loadingMore: Boolean = false,
    /** True while showing cached data that could not be refreshed. */
    val offline: Boolean = false,
    val error: String? = null,
)

/** Timeline order: unread first, then newest first, then highest id — same as the server. */
internal val timelineOrder: Comparator<StorySummary> =
    compareBy<StorySummary> { it.isRead }
        .thenByDescending { Instant.parse(it.publishedAt) }
        .thenByDescending { it.id }

class TimelineRepository(private val api: ChangeloomApi, private val cache: StoryCache) {
    private val _state = MutableStateFlow(TimelineState())
    val state: StateFlow<TimelineState> = _state.asStateFlow()

    /** Shows the cached timeline immediately (if nothing is loaded yet), then fetches the first page. */
    suspend fun refresh() {
        if (_state.value.items.isEmpty()) {
            _state.update { it.copy(items = cache.loadTimeline()) }
        }
        _state.update { it.copy(refreshing = true, error = null) }
        try {
            val page = api.timeline()
            cache.saveTimeline(page.items)
            _state.value = TimelineState(items = page.items, nextCursor = page.nextCursor)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            _state.update {
                it.copy(refreshing = false, offline = it.items.isNotEmpty(), error = e.message ?: "Could not load timeline")
            }
        }
    }

    suspend fun loadMore() {
        val cursor = _state.value.nextCursor ?: return
        if (_state.value.loadingMore || _state.value.refreshing) return
        _state.update { it.copy(loadingMore = true, error = null) }
        try {
            val page = api.timeline(cursor)
            _state.update { s ->
                val known = s.items.mapTo(HashSet()) { it.id }
                s.copy(
                    items = s.items + page.items.filter { it.id !in known },
                    nextCursor = page.nextCursor,
                    loadingMore = false,
                )
            }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            _state.update { it.copy(loadingMore = false, error = e.message ?: "Could not load more") }
        }
    }

    /** Applies the change locally (re-sorting the list), then syncs; a failed sync rolls the story back. */
    suspend fun setRead(id: Long, read: Boolean) {
        val before = _state.value.items.firstOrNull { it.id == id } ?: return
        if (before.isRead == read) return
        applyRead(id, read)
        try {
            if (read) api.markRead(id) else api.markUnread(id)
            cache.saveTimeline(_state.value.items)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            _state.update { s ->
                s.copy(
                    items = s.items.map { if (it.id == id) before else it }.sortedWith(timelineOrder),
                    error = e.message ?: "Could not update read state",
                )
            }
        }
    }

    /**
     * Applies the change to the timeline copy (if the story is in it), then syncs. Returns false and rolls
     * the timeline back when the sync fails, so callers holding their own copy can roll back too.
     */
    suspend fun setBookmarked(id: Long, on: Boolean): Boolean {
        applyBookmark(id, on)
        try {
            if (on) api.addBookmark(id) else api.removeBookmark(id)
            cache.saveTimeline(_state.value.items)
            return true
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            applyBookmark(id, !on)
            _state.update { it.copy(error = e.message ?: "Could not update bookmark") }
            return false
        }
    }

    private fun applyBookmark(id: Long, on: Boolean) = _state.update { s ->
        s.copy(items = s.items.map { if (it.id == id) it.copy(isBookmarked = on) else it })
    }

    private fun applyRead(id: Long, read: Boolean) = _state.update { s ->
        s.copy(items = s.items.map { if (it.id == id) it.copy(isRead = read, readAt = if (read) it.readAt else null) else it }
            .sortedWith(timelineOrder))
    }

    /** Drops in-memory and cached data (on sign-out, so the next user never sees it). */
    suspend fun clear() {
        _state.value = TimelineState()
        cache.clear()
    }

    fun clearError() = _state.update { it.copy(error = null) }

    /** Story detail: network first, cached copy when offline. */
    suspend fun story(id: Long): Story = try {
        api.story(id).also { cache.saveStory(it) }
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        cache.loadStory(id) ?: throw e
    }
}
