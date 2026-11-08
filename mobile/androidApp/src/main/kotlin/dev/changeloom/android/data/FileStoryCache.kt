package dev.changeloom.android.data

import dev.changeloom.android.telemetry.AppLog
import dev.changeloom.shared.data.Story
import dev.changeloom.shared.data.StoryCache
import dev.changeloom.shared.data.StorySummary
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.builtins.serializer
import kotlinx.serialization.json.Json
import java.io.File
import java.io.IOException
import java.util.concurrent.atomic.AtomicReference

/**
 * JSON files under [dir]: `timeline.json`, `followed.json` and one file per opened story (oldest pruned beyond
 * [MAX_STORIES]). Timeline saves are conflated: [scope] writes only the latest list, so a burst of taps is one write.
 */
class FileStoryCache(
    private val dir: File,
    private val scope: CoroutineScope = CoroutineScope(SupervisorJob() + Dispatchers.IO),
) : StoryCache {
    private val json = Json { ignoreUnknownKeys = true }
    private val timelineFile get() = File(dir, "timeline.json")
    private val storiesDir get() = File(dir, "stories")
    private val followedFile get() = File(dir, "followed.json")
    private val slugsSerializer = ListSerializer(String.serializer())

    /** The newest timeline not yet written; null when the file is current. */
    private val pendingTimeline = AtomicReference<List<StorySummary>?>(null)

    /** Serializes timeline writes with [clear], so a write never lands after a sign-out's clear. */
    private val timelineLock = Mutex()

    private val storiesLock = Mutex()

    /** Story files on disk, counted once and then kept up to date; null until the first save. */
    private var storyCount: Int? = null

    override suspend fun loadTimeline(): List<StorySummary> = pendingTimeline.get() ?: withContext(Dispatchers.IO) {
        read(timelineFile) { json.decodeFromString(ListSerializer(StorySummary.serializer()), it) } ?: emptyList()
    }

    override suspend fun saveTimeline(items: List<StorySummary>) {
        // Only the save that finds nothing pending starts a writer; later ones just replace what it will write.
        if (pendingTimeline.getAndSet(items) != null) return
        scope.launch {
            timelineLock.withLock {
                val latest = pendingTimeline.getAndSet(null) ?: return@withLock
                write(timelineFile, json.encodeToString(ListSerializer(StorySummary.serializer()), latest))
            }
        }
    }

    override suspend fun loadStory(id: Long): Story? = withContext(Dispatchers.IO) {
        read(File(storiesDir, "$id.json")) { json.decodeFromString(Story.serializer(), it) }
    }

    /** Lists the directory only when the count passes [MAX_STORIES] + [PRUNE_SLACK], then prunes to [MAX_STORIES]. */
    override suspend fun saveStory(story: Story) = withContext(Dispatchers.IO) {
        storiesLock.withLock {
            val file = File(storiesDir, "${story.id}.json")
            val added = !file.exists()
            val before = storyCount ?: (storiesDir.list()?.size ?: 0)
            write(file, json.encodeToString(Story.serializer(), story))
            val count = if (added) before + 1 else before
            storyCount = if (count > MAX_STORIES + PRUNE_SLACK) {
                storiesDir.listFiles()?.sortedByDescending { it.lastModified() }?.drop(MAX_STORIES)?.forEach { it.delete() }
                storiesDir.list()?.size ?: 0
            } else {
                count
            }
        }
    }

    override suspend fun loadFollowed(): List<String> = withContext(Dispatchers.IO) {
        read(followedFile) { json.decodeFromString(slugsSerializer, it) } ?: emptyList()
    }

    override suspend fun saveFollowed(slugs: List<String>) = withContext(Dispatchers.IO) {
        write(followedFile, json.encodeToString(slugsSerializer, slugs))
    }

    override suspend fun clear() {
        pendingTimeline.set(null)
        withContext(Dispatchers.IO) {
            timelineLock.withLock {
                storiesLock.withLock {
                    dir.deleteRecursively()
                    storyCount = 0
                }
            }
        }
    }

    private fun <T> read(file: File, decode: (String) -> T): T? {
        if (!file.exists()) return null
        return try {
            decode(file.readText())
        } catch (e: IOException) {
            AppLog.w(TAG, "Cache read failed: ${file.name}", e)
            null
        } catch (e: IllegalArgumentException) { // SerializationException: corrupt or outdated file
            AppLog.w(TAG, "Cache entry unreadable, ignoring: ${file.name}", e)
            null
        }
    }

    private fun write(file: File, text: String) {
        try {
            file.parentFile?.mkdirs()
            val tmp = File(file.parentFile, file.name + ".tmp")
            tmp.writeText(text)
            if (!tmp.renameTo(file)) throw IOException("rename failed")
        } catch (e: IOException) {
            AppLog.w(TAG, "Cache write failed: ${file.name}", e)
        }
    }

    private companion object {
        const val TAG = "FileStoryCache"
        const val MAX_STORIES = 100

        /** Story files allowed beyond [MAX_STORIES] before a prune, so most saves skip listing the directory. */
        const val PRUNE_SLACK = 20
    }
}
