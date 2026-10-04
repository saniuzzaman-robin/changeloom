package dev.changeloom.shared.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
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

/** Followed topics first, then the user's professions, then related ones, then the rest; a missing match (older server) counts as the rest. */
internal fun matchTier(match: String?): Int = when (match) {
    "followed" -> 0
    "profession" -> 1
    "related" -> 2
    else -> 3
}

/** Timeline order: unread first, then match tier, then newest first, then highest id — same as the server. */
internal val timelineOrder: Comparator<StorySummary> =
    compareBy<StorySummary> { it.isRead }
        .thenBy { matchTier(it.match) }
        .thenByDescending { Instant.parse(it.publishedAt) }
        .thenByDescending { it.id }

class TimelineRepository(private val api: ChangeloomApi, private val cache: StoryCache) {
    private val _state = MutableStateFlow(TimelineState())
    val state: StateFlow<TimelineState> = _state.asStateFlow()

    private val bookmarkMutex = Mutex()

    /** Bookmark changes not yet synced, story id to wanted state. Always differs from the server's state. */
    private val pendingBookmarks = LinkedHashMap<Long, Boolean>()

    /** Shows the cached timeline immediately (if nothing is loaded yet), then fetches the first page. */
    suspend fun refresh() {
        if (_state.value.items.isEmpty()) {
            _state.update { it.copy(items = cache.loadTimeline()) }
        }
        _state.update { it.copy(refreshing = true, error = null) }
        try {
            val page = api.timeline()
            val items = withPendingBookmarks(page.items)
            cache.saveTimeline(items)
            _state.value = TimelineState(items = items, nextCursor = page.nextCursor)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            _state.update {
                it.copy(refreshing = false, offline = it.items.isNotEmpty(), error = userMessage(e, "Couldn't load your timeline"))
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
                    items = s.items + withPendingBookmarks(page.items).filter { it.id !in known },
                    nextCursor = page.nextCursor,
                    loadingMore = false,
                )
            }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            _state.update { it.copy(loadingMore = false, error = userMessage(e, "Couldn't load more stories")) }
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
                    error = userMessage(e, "Couldn't update the read state"),
                )
            }
        }
    }

    /**
     * Applies the change to the timeline copy (if the story is in it) and queues it; nothing is sent until
     * [flushBookmarks]. Toggling a story back cancels its queued change.
     */
    suspend fun setBookmarked(id: Long, on: Boolean) {
        applyBookmark(id, on)
        bookmarkMutex.withLock {
            if (pendingBookmarks[id] == !on) pendingBookmarks.remove(id) else pendingBookmarks[id] = on
        }
        cache.saveTimeline(_state.value.items)
    }

    /** Sends the queued bookmark changes, if any, in as few calls as possible. Failed changes stay queued for the next flush. */
    suspend fun flushBookmarks() = bookmarkMutex.withLock {
        val sent = LinkedHashMap(pendingBookmarks)
        for (batch in sent.entries.chunked(ChangeloomApi.MAX_VIEW_IDS)) {
            try {
                api.syncBookmarks(batch.filter { it.value }.map { it.key }, batch.filter { !it.value }.map { it.key })
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                return@withLock
            }
            batch.forEach { pendingBookmarks.remove(it.key) }
        }
    }

    private suspend fun withPendingBookmarks(items: List<StorySummary>): List<StorySummary> = bookmarkMutex.withLock {
        if (pendingBookmarks.isEmpty()) items
        else items.map { s -> pendingBookmarks[s.id]?.let { s.copy(isBookmarked = it) } ?: s }
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
        bookmarkMutex.withLock { pendingBookmarks.clear() }
        cache.clear()
    }

    fun clearError() = _state.update { it.copy(error = null) }

    /** Story detail: network first, cached copy when offline. */
    suspend fun story(id: Long): Story = try {
        api.story(id).let { s -> bookmarkMutex.withLock { pendingBookmarks[id] }?.let { s.copy(isBookmarked = it) } ?: s }
            .also { cache.saveStory(it) }
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        cache.loadStory(id) ?: throw e
    }
}
