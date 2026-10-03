package dev.changeloom.shared

import dev.changeloom.shared.auth.AuthRepository
import dev.changeloom.shared.auth.AuthUser
import dev.changeloom.shared.data.ChangeloomApi
import dev.changeloom.shared.data.Story
import dev.changeloom.shared.data.StoryCache
import dev.changeloom.shared.data.StorySummary
import dev.changeloom.shared.data.TimelineRepository
import io.ktor.client.engine.mock.MockEngine
import io.ktor.client.engine.mock.respond
import io.ktor.client.engine.mock.respondError
import io.ktor.http.HttpHeaders
import io.ktor.http.HttpMethod
import io.ktor.http.HttpStatusCode
import io.ktor.http.headersOf
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

private object TokenAuth : AuthRepository {
    override val currentUser: StateFlow<AuthUser?> = MutableStateFlow(null)
    override suspend fun idToken() = "tok"
    override suspend fun signInWithEmail(email: String, password: String) = Unit
    override suspend fun registerWithEmail(email: String, password: String) = Unit
    override suspend fun signInWithGoogleIdToken(googleIdToken: String) = Unit
    override fun signOut() = Unit
}

private class MemoryCache(var timeline: List<StorySummary> = emptyList()) : StoryCache {
    val stories = mutableMapOf<Long, Story>()
    override suspend fun loadTimeline() = timeline
    override suspend fun saveTimeline(items: List<StorySummary>) { timeline = items }
    override suspend fun loadStory(id: Long) = stories[id]
    override suspend fun saveStory(story: Story) { stories[story.id] = story }
    override suspend fun clear() { timeline = emptyList(); stories.clear() }
}

private val json = headersOf(HttpHeaders.ContentType, "application/json")

private fun item(id: Long, at: String, read: Boolean, match: String? = null) =
    """{"id":$id,"title":"t$id","summary":"s","kind":"release","importance":3,"published_at":"$at","topics":["x"],"is_read":$read${match?.let { ""","match":"$it"""" } ?: ""}}"""

private fun page(vararg items: String, next: String? = null) =
    """{"items":[${items.joinToString(",")}]${next?.let { ""","next_cursor":"$it"""" } ?: ""}}"""

private val firstPage = page(
    item(1, "2026-09-03T10:00:00Z", false),
    item(2, "2026-09-02T10:00:00.5Z", false),
    item(3, "2026-09-04T10:00:00Z", true),
    next = "c1",
)

private fun repo(cache: MemoryCache, handler: io.ktor.client.engine.mock.MockRequestHandler): TimelineRepository =
    TimelineRepository(ChangeloomApi(ChangeloomApi.createClient("http://api.test", MockEngine(handler)), TokenAuth), cache)

class TimelineRepositoryTest {
    @Test
    fun refreshLoadsAndCachesFirstPage() = runTest {
        val cache = MemoryCache()
        val repo = repo(cache) { respond(firstPage, headers = json) }
        repo.refresh()
        val s = repo.state.value
        assertEquals(listOf(1L, 2L, 3L), s.items.map { it.id })
        assertEquals("c1", s.nextCursor)
        assertEquals(3, cache.timeline.size)
    }

    @Test
    fun markReadKeepsMatchTierOrder() = runTest {
        val body = page(
            item(1, "2026-09-01T10:00:00Z", false, "followed"),
            item(2, "2026-09-05T10:00:00Z", false, "related"),
            item(3, "2026-09-06T10:00:00Z", false, "explore"),
            item(4, "2026-09-07T10:00:00Z", true, "followed"),
        )
        val repo = repo(MemoryCache()) { req ->
            if (req.method == HttpMethod.Put) respond("", HttpStatusCode.NoContent) else respond(body, headers = json)
        }
        repo.refresh()
        assertEquals("related", repo.state.value.items[1].match)
        repo.setRead(3, true)
        repo.setRead(3, false)
        // Unread first (followed, related, explore by tier even though explore is newest), then read.
        assertEquals(listOf(1L, 2L, 3L, 4L), repo.state.value.items.map { it.id })
    }

    @Test
    fun offlineRefreshKeepsCachedItems() = runTest {
        val cache = MemoryCache(listOf(StorySummary(9, "t", "s", "release", null, 3, "2026-09-01T00:00:00Z", listOf("x"), false)))
        val repo = repo(cache) { respondError(HttpStatusCode.ServiceUnavailable) }
        repo.refresh()
        val s = repo.state.value
        assertEquals(listOf(9L), s.items.map { it.id })
        assertTrue(s.offline)
        assertFalse(s.refreshing)
    }

    @Test
    fun markReadReordersOptimisticallyAndSyncs() = runTest {
        var putPath: String? = null
        val repo = repo(MemoryCache()) { req ->
            if (req.method == HttpMethod.Put) { putPath = req.url.encodedPath; respond("", HttpStatusCode.NoContent) }
            else respond(firstPage, headers = json)
        }
        repo.refresh()
        repo.setRead(1, true)
        // 1 (2026-09-03) joins the read group, ordered after 3 (2026-09-04).
        assertEquals(listOf(2L, 3L, 1L), repo.state.value.items.map { it.id })
        assertEquals("/v1/stories/1/read", putPath)
    }

    @Test
    fun markUnreadMovesStoryBackToUnreadGroup() = runTest {
        val repo = repo(MemoryCache()) { req ->
            if (req.method == HttpMethod.Delete) respond("", HttpStatusCode.NoContent) else respond(firstPage, headers = json)
        }
        repo.refresh()
        repo.setRead(3, false)
        assertEquals(listOf(3L, 1L, 2L), repo.state.value.items.map { it.id })
        assertNull(repo.state.value.items.first().readAt)
    }

    @Test
    fun failedSyncRollsBack() = runTest {
        val repo = repo(MemoryCache()) { req ->
            if (req.method == HttpMethod.Put) respondError(HttpStatusCode.InternalServerError) else respond(firstPage, headers = json)
        }
        repo.refresh()
        repo.setRead(1, true)
        val s = repo.state.value
        assertEquals(listOf(1L, 2L, 3L), s.items.map { it.id })
        assertFalse(s.items.first().isRead)
        assertTrue(s.error != null)
    }

    @Test
    fun loadMoreAppendsWithoutDuplicates() = runTest {
        val repo = repo(MemoryCache()) { req ->
            if (req.url.parameters["cursor"] == "c1") respond(page(item(3, "2026-09-04T10:00:00Z", true), item(4, "2026-08-01T00:00:00Z", true)), headers = json)
            else respond(firstPage, headers = json)
        }
        repo.refresh()
        repo.loadMore()
        assertEquals(listOf(1L, 2L, 3L, 4L), repo.state.value.items.map { it.id })
        assertNull(repo.state.value.nextCursor)
    }

    @Test
    fun storyFallsBackToCacheOffline() = runTest {
        val cache = MemoryCache()
        var online = true
        val body = """{"id":5,"title":"t","summary":"s","kind":"release","importance":3,"published_at":"2026-09-01T00:00:00Z","topics":[],"is_read":false,"body_md":"# hi","sources":[{"url":"http://a","name":"A"}]}"""
        val repo = repo(cache) { if (online) respond(body, headers = json) else respondError(HttpStatusCode.BadGateway) }
        assertEquals("# hi", repo.story(5).bodyMd)
        online = false
        assertEquals("A", repo.story(5).sources.single().name)
    }

    @Test
    fun setBookmarkedUpdatesTimelineAndRollsBackOnFailure() = runTest {
        var failBookmark = false
        val cache = MemoryCache()
        val repo = repo(cache) { request ->
            when {
                request.url.encodedPath == "/v1/timeline" -> respond(firstPage, headers = json)
                failBookmark -> respondError(HttpStatusCode.InternalServerError)
                else -> respond("", HttpStatusCode.NoContent)
            }
        }
        repo.refresh()
        assertTrue(repo.setBookmarked(1, true))
        assertTrue(repo.state.value.items.first { it.id == 1L }.isBookmarked)
        assertTrue(cache.timeline.first { it.id == 1L }.isBookmarked)

        failBookmark = true
        assertFalse(repo.setBookmarked(2, true))
        assertFalse(repo.state.value.items.first { it.id == 2L }.isBookmarked)
        assertTrue(repo.state.value.error != null)
    }
}
