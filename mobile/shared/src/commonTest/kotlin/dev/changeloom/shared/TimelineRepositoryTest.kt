package dev.changeloom.shared

import dev.changeloom.shared.auth.AuthRepository
import dev.changeloom.shared.auth.AuthUser
import dev.changeloom.shared.data.ChangeloomApi
import dev.changeloom.shared.data.Story
import dev.changeloom.shared.data.StoryCache
import dev.changeloom.shared.data.ReadFilter
import dev.changeloom.shared.data.StorySummary
import dev.changeloom.shared.data.TimelineFilter
import dev.changeloom.shared.data.TimelineRepository
import io.ktor.client.engine.mock.MockEngine
import io.ktor.client.engine.mock.respond
import io.ktor.client.engine.mock.respondError
import io.ktor.http.HttpHeaders
import io.ktor.http.HttpMethod
import io.ktor.http.HttpStatusCode
import io.ktor.http.content.TextContent
import io.ktor.http.headersOf
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

private object TokenAuth : AuthRepository {
    override val currentUser: StateFlow<AuthUser?> = MutableStateFlow(null)
    override suspend fun idToken(forceRefresh: Boolean) = "tok"
    override suspend fun signInWithEmail(email: String, password: String) = Unit
    override suspend fun registerWithEmail(email: String, password: String) = Unit
    override suspend fun signInWithGoogleIdToken(googleIdToken: String) = false
    override fun signOut() = Unit
    override fun needsReauth() = false
    override suspend fun reauthenticateWithPassword(password: String) = Unit
    override suspend fun reauthenticateWithGoogleIdToken(googleIdToken: String) = Unit
    override suspend fun deleteUser() = Unit
}

private class MemoryCache(var timeline: List<StorySummary> = emptyList()) : StoryCache {
    val stories = mutableMapOf<Long, Story>()
    var followed = emptyList<String>()
    /** When set, [loadTimeline] waits for it, like a slow disk read. */
    var timelineRead: CompletableDeferred<Unit>? = null
    override suspend fun loadTimeline(): List<StorySummary> {
        timelineRead?.await()
        return timeline
    }
    override suspend fun saveTimeline(items: List<StorySummary>) { timeline = items }
    override suspend fun loadStory(id: Long) = stories[id]
    override suspend fun saveStory(story: Story) { stories[story.id] = story }
    override suspend fun loadFollowed() = followed
    override suspend fun saveFollowed(slugs: List<String>) { followed = slugs }
    override suspend fun clear() { timeline = emptyList(); stories.clear(); followed = emptyList() }
}

private val json = headersOf(HttpHeaders.ContentType, "application/json")

private fun item(id: Long, at: String, read: Boolean, match: String? = null, topics: List<String> = listOf("x")) =
    """{"id":$id,"title":"t$id","summary":"s","kind":"release","importance":3,"published_at":"$at","topics":[${topics.joinToString(",") { "\"$it\"" }}],"is_read":$read${match?.let { ""","match":"$it"""" } ?: ""}}"""

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
    fun filterIsSentToTheServerAndKeptAcrossPaging() = runTest {
        val cache = MemoryCache(timeline = listOf(StorySummary(99, "cached", "s", "release", null, 3, "2026-09-01T10:00:00Z", listOf("x"), false, null, false)))
        val queries = mutableListOf<io.ktor.http.Parameters>()
        val repo = repo(cache) { request ->
            queries += request.url.parameters
            respond(page(item(5, "2026-09-03T10:00:00Z", false), next = if (request.url.parameters["cursor"] == null) "c1" else null), headers = json)
        }
        repo.setFilter(TimelineFilter(kinds = setOf("security", "deprecation"), read = ReadFilter.Unread))
        repo.loadMore()
        assertEquals(2, queries.size)
        for (q in queries) {
            assertEquals(listOf("security", "deprecation"), q.getAll("kind"))
            assertEquals("false", q["read"])
        }
        assertEquals("c1", queries[1]["cursor"])
        // The cache keeps the unfiltered timeline.
        assertEquals(listOf(99L), cache.timeline.map { it.id })
    }

    @Test
    fun defaultFilterSendsNoFilterParameters() = runTest {
        var query: io.ktor.http.Parameters? = null
        val repo = repo(MemoryCache()) { request ->
            query = request.url.parameters
            respond(firstPage, headers = json)
        }
        repo.refresh()
        assertNull(query!!["kind"])
        assertNull(query!!["read"])
    }

    @Test
    fun markingReadDropsTheStoryUnderTheUnreadFilter() = runTest {
        val repo = repo(MemoryCache()) { request ->
            if (request.method == HttpMethod.Put) respond("", HttpStatusCode.NoContent)
            else respond(page(item(1, "2026-09-03T10:00:00Z", false), item(2, "2026-09-02T10:00:00Z", false)), headers = json)
        }
        repo.setFilter(TimelineFilter(read = ReadFilter.Unread))
        repo.setRead(1, true)
        assertEquals(listOf(2L), repo.state.value.items.map { it.id })
    }

    @Test
    fun failedReadRestoresAStoryTheFilterDropped() = runTest {
        val repo = repo(MemoryCache()) { request ->
            if (request.method == HttpMethod.Put) respondError(HttpStatusCode.InternalServerError)
            else respond(page(item(1, "2026-09-03T10:00:00Z", false), item(2, "2026-09-02T10:00:00Z", false)), headers = json)
        }
        repo.setFilter(TimelineFilter(read = ReadFilter.Unread))
        repo.setRead(1, true)
        assertEquals(listOf(1L, 2L), repo.state.value.items.map { it.id })
    }

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
    fun refreshIsRefreshingWhileReadingTheCache() = runTest {
        val cache = MemoryCache().apply { timelineRead = CompletableDeferred() }
        val repo = repo(cache) { respond(firstPage, headers = json) }
        val job = launch { repo.refresh() }
        runCurrent()
        // An empty list that isn't refreshing reads as "no stories", so the flag must be set before the cache read.
        assertTrue(repo.state.value.refreshing)
        cache.timelineRead?.complete(Unit)
        job.join()
        assertFalse(repo.state.value.refreshing)
    }

    @Test
    fun localReadChangesKeepServerOrder() = runTest {
        // The server ranks unread stories by a score it doesn't send, so its order isn't by tier or date.
        val body = page(
            item(1, "2026-09-01T10:00:00Z", false, "explore"),
            item(2, "2026-09-06T10:00:00Z", false, "followed"),
            item(3, "2026-09-05T10:00:00Z", false, "related"),
            item(4, "2026-09-08T10:00:00Z", true, "followed"),
            item(5, "2026-09-07T10:00:00Z", true, "explore"),
            next = "c1",
        )
        val repo = repo(MemoryCache()) { req ->
            when {
                req.method != HttpMethod.Get -> respond("", HttpStatusCode.NoContent)
                req.url.parameters["cursor"] == "c1" -> respond(page(item(6, "2026-09-09T10:00:00Z", false, "followed")), headers = json)
                else -> respond(body, headers = json)
            }
        }
        repo.refresh()
        repo.setRead(2, true)
        // 2 joins the read group, newest first.
        assertEquals(listOf(1L, 3L, 4L, 5L, 2L), repo.state.value.items.map { it.id })
        repo.setRead(2, false)
        assertEquals(listOf(1L, 2L, 3L, 4L, 5L), repo.state.value.items.map { it.id })
        // A story the server sent as read has no unread position: it ends the unread group.
        repo.setRead(5, false)
        assertEquals(listOf(1L, 2L, 3L, 5L, 4L), repo.state.value.items.map { it.id })
        // A later page's story keeps its place after every story of the first page.
        repo.loadMore()
        repo.setRead(6, true)
        repo.setRead(6, false)
        assertEquals(listOf(1L, 2L, 3L, 5L, 6L, 4L), repo.state.value.items.map { it.id })
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
        // The server's score for it isn't known, so it ends the unread group.
        assertEquals(listOf(1L, 2L, 3L), repo.state.value.items.map { it.id })
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
    fun bookmarkChangesAreQueuedLocallyAndSyncedInOneCallOnFlush() = runTest {
        var syncs = 0
        var failSync = false
        val cache = MemoryCache()
        val repo = repo(cache) { request ->
            when {
                request.url.encodedPath == "/v1/timeline" -> respond(firstPage, headers = json)
                failSync -> respondError(HttpStatusCode.InternalServerError)
                else -> { syncs++; respond("", HttpStatusCode.NoContent) }
            }
        }
        repo.refresh()
        repo.setBookmarked(1, true)
        repo.setBookmarked(2, true)
        repo.setBookmarked(2, false) // toggled back: no net change
        assertTrue(repo.state.value.items.first { it.id == 1L }.isBookmarked)
        assertTrue(cache.timeline.first { it.id == 1L }.isBookmarked)
        assertEquals(0, syncs)

        failSync = true
        repo.flushBookmarks()
        failSync = false
        assertEquals(0, syncs)

        repo.flushBookmarks()
        assertEquals(1, syncs)
        repo.flushBookmarks() // nothing queued: no call
        assertEquals(1, syncs)
    }

    @Test
    fun refreshKeepsUnsyncedBookmarkChanges() = runTest {
        val repo = repo(MemoryCache()) { respond(firstPage, headers = json) }
        repo.refresh()
        repo.setBookmarked(1, true)
        repo.refresh()
        assertTrue(repo.state.value.items.first { it.id == 1L }.isBookmarked)
    }

    @Test
    fun dismissHidesTheStoryAndUndoPutsItBackInPlace() = runTest {
        val cache = MemoryCache()
        val calls = mutableListOf<String>()
        val repo = repo(cache) { req ->
            if (req.url.encodedPath.endsWith("/dismiss")) {
                calls += "${req.method.value} ${req.url.encodedPath}"
                respond("", HttpStatusCode.NoContent)
            } else respond(firstPage, headers = json)
        }
        repo.refresh()
        val story = repo.state.value.items.first { it.id == 1L }
        assertTrue(repo.dismiss(1))
        assertEquals(listOf(2L, 3L), repo.state.value.items.map { it.id })
        assertEquals(listOf(2L, 3L), cache.timeline.map { it.id })

        repo.undismiss(story)
        assertEquals(listOf(1L, 2L, 3L), repo.state.value.items.map { it.id })
        assertEquals(listOf("PUT /v1/stories/1/dismiss", "DELETE /v1/stories/1/dismiss"), calls)
    }

    @Test
    fun failedDismissPutsTheStoryBack() = runTest {
        val repo = repo(MemoryCache()) { req ->
            if (req.method == HttpMethod.Put) respondError(HttpStatusCode.InternalServerError) else respond(firstPage, headers = json)
        }
        repo.refresh()
        assertFalse(repo.dismiss(1))
        assertEquals(listOf(1L, 2L, 3L), repo.state.value.items.map { it.id })
        assertTrue(repo.state.value.error != null)
    }

    @Test
    fun muteHidesCoveredStoriesSendsTheWholeSetAndUndoes() = runTest {
        val cache = MemoryCache().apply { followed = listOf("web/react") }
        var muted = listOf("cloud")
        val sent = mutableListOf<String>()
        val repo = repo(cache) { req ->
            when (req.url.encodedPath) {
                "/v1/me" -> respond("""{"id":1,"topics":["web/react"],"muted_topics":[${muted.joinToString(",") { "\"$it\"" }}]}""", headers = json)
                "/v1/me/muted-topics" -> {
                    val body = (req.body as TextContent).text
                    sent += body
                    muted = Regex("\"([^\"]+)\"").findAll(body.substringAfter(":")).map { it.groupValues[1] }.toList()
                    respond("""{"id":1,"topics":["web/react"],"muted_topics":[${muted.joinToString(",") { "\"$it\"" }}]}""", headers = json)
                }
                else -> respond(
                    page(
                        item(1, "2026-09-03T10:00:00Z", false, topics = listOf("web/vue")),
                        item(2, "2026-09-02T10:00:00Z", false, topics = listOf("web/react")),
                        item(3, "2026-09-01T10:00:00Z", false, topics = listOf("web/vue", "databases")),
                        item(4, "2026-08-31T10:00:00Z", false, topics = listOf("web")),
                    ),
                    headers = json,
                )
            }
        }
        repo.refresh()
        // web covers web/vue and web itself, but not the followed web/react; story 3 has another topic.
        val hidden = repo.muteTopic("web")
        assertEquals(listOf(1L, 4L), hidden?.map { it.id })
        assertEquals(listOf(2L, 3L), repo.state.value.items.map { it.id })
        assertEquals(listOf("cloud", "web"), muted)

        repo.undoMute("web", hidden!!)
        assertEquals(listOf("cloud"), muted)
        assertEquals(listOf(1L, 2L, 3L, 4L), repo.state.value.items.map { it.id })
        assertEquals(2, sent.size)
    }

    @Test
    fun failedMutePutsTheStoriesBack() = runTest {
        val repo = repo(MemoryCache()) { req ->
            when (req.url.encodedPath) {
                "/v1/me" -> respond("""{"id":1,"topics":[]}""", headers = json)
                "/v1/me/muted-topics" -> respondError(HttpStatusCode.InternalServerError)
                else -> respond(firstPage, headers = json)
            }
        }
        repo.refresh()
        assertNull(repo.muteTopic("x"))
        assertEquals(listOf(1L, 2L, 3L), repo.state.value.items.map { it.id })
        assertTrue(repo.state.value.error != null)
    }

    @Test
    fun unmuteReloadsTheTimeline() = runTest {
        var timelineCalls = 0
        val repo = repo(MemoryCache()) { req ->
            when (req.url.encodedPath) {
                "/v1/me" -> respond("""{"id":1,"topics":[],"muted_topics":["x","y"]}""", headers = json)
                "/v1/me/muted-topics" -> respond("""{"id":1,"topics":[],"muted_topics":["y"]}""", headers = json)
                else -> {
                    timelineCalls++
                    respond(firstPage, headers = json)
                }
            }
        }
        assertEquals(listOf("y"), repo.unmuteTopic("x"))
        assertEquals(1, timelineCalls)
        assertEquals(listOf(1L, 2L, 3L), repo.state.value.items.map { it.id })
    }
}
