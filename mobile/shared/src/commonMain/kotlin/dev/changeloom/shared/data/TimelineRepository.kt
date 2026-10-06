package dev.changeloom.shared.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlin.time.Instant

/** Which stories the timeline shows; the server applies it, so paging stays correct. */
enum class ReadFilter { All, Unread, Read }

/** Story kinds the api knows, in the order the filter lists them. */
val STORY_KINDS = listOf("release", "breaking", "security", "deprecation", "announcement", "article", "research", "policy", "deal")

/** [kinds] empty means every kind. */
data class TimelineFilter(val kinds: Set<String> = emptySet(), val read: ReadFilter = ReadFilter.All) {
    val isDefault: Boolean get() = kinds.isEmpty() && read == ReadFilter.All
}

data class TimelineState(
    val filter: TimelineFilter = TimelineFilter(),
    val items: List<StorySummary> = emptyList(),
    val nextCursor: String? = null,
    val refreshing: Boolean = false,
    val loadingMore: Boolean = false,
    /** True while showing cached data that could not be refreshed. */
    val offline: Boolean = false,
    val error: String? = null,
    /** Each story's position in the order the server sent it, which local read changes keep; see [sortedForTimeline]. */
    internal val serverOrder: Map<Long, Int> = emptyMap(),
)

private val newestFirst: Comparator<Pair<StorySummary, Instant>> =
    compareByDescending<Pair<StorySummary, Instant>> { it.second }.thenByDescending { it.first.id }

/**
 * Timeline order, as the server ranks it: unread stories in the server's order (it ranks them by a score it doesn't
 * send, so [serverOrder] holds each one's position as received; a story the server sent as read goes last), then
 * read ones newest first, then highest id. Each timestamp is parsed once rather than on every comparison.
 */
internal fun List<StorySummary>.sortedForTimeline(serverOrder: Map<Long, Int>): List<StorySummary> {
    val (unread, read) = partition { !it.isRead }
    return unread.sortedBy { serverOrder[it.id] ?: Int.MAX_VALUE } +
        read.map { it to Instant.parse(it.publishedAt) }.sortedWith(newestFirst).map { it.first }
}

/** Positions of these stories in server order, counting from [start]. */
private fun List<StorySummary>.positions(start: Int = 0): Map<Long, Int> =
    withIndex().associate { (i, story) -> story.id to start + i }

class TimelineRepository(private val api: ChangeloomApi, private val cache: StoryCache) {
    private val _state = MutableStateFlow(TimelineState())
    val state: StateFlow<TimelineState> = _state.asStateFlow()

    private val bookmarkMutex = Mutex()

    /** Serializes mute changes: each one reads the server's set and writes it back whole. */
    private val muteMutex = Mutex()

    /** Bookmark changes not yet synced, story id to wanted state. Always differs from the server's state. */
    private val pendingBookmarks = LinkedHashMap<Long, Boolean>()

    /** Shows the cached timeline immediately (if nothing is loaded yet), then fetches the first page. */
    suspend fun refresh() {
        val filter = _state.value.filter
        // Refreshing before the cache read: an empty list that isn't refreshing shows the "no stories" state.
        _state.update { it.copy(refreshing = true, error = null) }
        // The cache holds the unfiltered timeline.
        if (_state.value.items.isEmpty() && filter.isDefault) {
            val cached = cache.loadTimeline()
            _state.update { if (it.filter == filter && it.items.isEmpty()) it.copy(items = cached, serverOrder = cached.positions()) else it }
        }
        try {
            val page = api.timeline(filter = filter)
            val items = withPendingBookmarks(page.items)
            // A newer filter owns the state now; this answer is for the old one.
            if (_state.value.filter != filter) return
            if (filter.isDefault) cache.saveTimeline(items)
            _state.value = TimelineState(filter = filter, items = items, nextCursor = page.nextCursor, serverOrder = items.positions())
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            _state.update {
                if (it.filter != filter) it
                else it.copy(refreshing = false, offline = it.items.isNotEmpty(), error = userMessage(e, "Couldn't load your timeline"))
            }
        }
    }

    /** Switches the filter and reloads from the first page; the old filter's stories are dropped at once. */
    suspend fun setFilter(filter: TimelineFilter) {
        if (_state.value.filter == filter) return
        _state.value = TimelineState(filter = filter, refreshing = true)
        refresh()
    }

    suspend fun loadMore() {
        val cursor = _state.value.nextCursor ?: return
        val filter = _state.value.filter
        if (_state.value.loadingMore || _state.value.refreshing) return
        _state.update { it.copy(loadingMore = true, error = null) }
        try {
            val page = api.timeline(cursor, filter = filter)
            _state.update { s ->
                if (s.filter != filter) return@update s
                val known = s.items.mapTo(HashSet()) { it.id }
                val added = withPendingBookmarks(page.items).filter { it.id !in known }
                s.copy(
                    items = s.items + added,
                    nextCursor = page.nextCursor,
                    loadingMore = false,
                    // Ids the read filter dropped keep their position, so new ones start after every known one.
                    serverOrder = s.serverOrder + added.positions(start = s.serverOrder.size),
                )
            }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            _state.update { if (it.filter != filter) it else it.copy(loadingMore = false, error = userMessage(e, "Couldn't load more stories")) }
        }
    }

    /** Applies the change locally (re-sorting the list), then syncs; a failed sync rolls the story back. */
    suspend fun setRead(id: Long, read: Boolean) {
        val before = _state.value.items.firstOrNull { it.id == id } ?: return
        if (before.isRead == read) return
        applyRead(id, read)
        try {
            if (read) api.markRead(id) else api.markUnread(id)
            persist()
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            _state.update { s ->
                s.copy(
                    // Back in place, also when the read filter had already dropped it.
                    items = (s.items.filter { it.id != id } + before).sortedForTimeline(s.serverOrder),
                    error = userMessage(e, "Couldn't update the read state"),
                )
            }
        }
    }

    /** "Not interested": hides the story at once, then tells the server; a failed call puts it back. True on success. */
    suspend fun dismiss(id: Long): Boolean {
        val story = _state.value.items.firstOrNull { it.id == id } ?: return false
        hide(setOf(id))
        return try {
            api.dismiss(id)
            persist()
            true
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            restore(listOf(story), userMessage(e, "Couldn't hide the story"))
            false
        }
    }

    /** Undoes [dismiss]: the story is back in its place at once; a failed call hides it again. */
    suspend fun undismiss(story: StorySummary) {
        restore(listOf(story))
        try {
            api.undismiss(story.id)
            persist()
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            hide(setOf(story.id))
            _state.update { it.copy(error = userMessage(e, "Couldn't show the story again")) }
        }
    }

    /**
     * "Less about [slug]": hides the stories whose every topic it covers, then adds it to the server's muted set. A
     * mute covers the topic and its descendants, except followed ones, as on the server. Returns the hidden stories
     * for an undo, or null when the call failed (they are back then).
     */
    suspend fun muteTopic(slug: String): List<StorySummary>? {
        val followed = cache.loadFollowed().toSet()
        val hidden = _state.value.items.filter { s -> s.topics.isNotEmpty() && s.topics.all { it.mutedBy(slug, followed) } }
        hide(hidden.mapTo(HashSet()) { it.id })
        return try {
            setMuted(slug, true)
            persist()
            hidden
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            restore(hidden, userMessage(e, "Couldn't mute the topic"))
            null
        }
    }

    /** Undoes [muteTopic]: the [hidden] stories are back at once; a failed call hides them again. */
    suspend fun undoMute(slug: String, hidden: List<StorySummary>) {
        restore(hidden)
        try {
            setMuted(slug, false)
            persist()
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            hide(hidden.mapTo(HashSet()) { it.id })
            _state.update { it.copy(error = userMessage(e, "Couldn't unmute the topic")) }
        }
    }

    /** Unmutes [slug] and reloads the timeline to bring its stories back; returns the saved muted set. Throws when the call fails. */
    suspend fun unmuteTopic(slug: String): List<String> = setMuted(slug, false).also { refresh() }

    /** Reads the server's muted set and writes it back with [slug] in or out; returns the saved set. */
    private suspend fun setMuted(slug: String, muted: Boolean): List<String> = muteMutex.withLock {
        val current = api.me()
        val wanted = if (muted) (current.mutedTopics + slug).distinct() else current.mutedTopics - slug
        val me = if (wanted == current.mutedTopics) current else api.putMyMutedTopics(wanted)
        // Muting a followed topic unfollows it on the server; keep the local copy in step.
        cache.saveFollowed(me.topics)
        me.mutedTopics
    }

    private fun String.mutedBy(slug: String, followed: Set<String>) =
        this == slug || (startsWith("$slug/") && this !in followed)

    private fun hide(ids: Set<Long>) = _state.update { s -> s.copy(items = s.items.filter { it.id !in ids }) }

    /** Puts stories back in their places, unless the list already has them. */
    private fun restore(stories: List<StorySummary>, error: String? = null) = _state.update { s ->
        val known = s.items.mapTo(HashSet()) { it.id }
        s.copy(items = (s.items + stories.filter { it.id !in known }).sortedForTimeline(s.serverOrder), error = error ?: s.error)
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
        persist()
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

    /** Caches the timeline, unless a filter is on: the cache holds the unfiltered one. */
    private suspend fun persist() {
        val s = _state.value
        if (s.filter.isDefault) cache.saveTimeline(s.items)
    }

    /** A story that no longer fits the read filter leaves the list. */
    private fun applyRead(id: Long, read: Boolean) = _state.update { s ->
        val items = s.items.map { if (it.id == id) it.copy(isRead = read, readAt = if (read) it.readAt else null) else it }
        s.copy(items = items.filter { s.filter.read.allows(it.isRead) }.sortedForTimeline(s.serverOrder))
    }

    private fun ReadFilter.allows(isRead: Boolean) = when (this) {
        ReadFilter.All -> true
        ReadFilter.Unread -> !isRead
        ReadFilter.Read -> isRead
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
