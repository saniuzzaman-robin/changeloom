package dev.changeloom.android.data

import dev.changeloom.android.telemetry.AppLog
import dev.changeloom.shared.data.Story
import dev.changeloom.shared.data.StoryCache
import dev.changeloom.shared.data.StorySummary
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.builtins.serializer
import kotlinx.serialization.json.Json
import java.io.File
import java.io.IOException

/** JSON files under [dir]: `timeline.json`, `followed.json` and one file per opened story (oldest pruned beyond [MAX_STORIES]). */
class FileStoryCache(private val dir: File) : StoryCache {
    private val json = Json { ignoreUnknownKeys = true }
    private val timelineFile get() = File(dir, "timeline.json")
    private val storiesDir get() = File(dir, "stories")
    private val followedFile get() = File(dir, "followed.json")
    private val slugsSerializer = ListSerializer(String.serializer())

    override suspend fun loadTimeline(): List<StorySummary> = withContext(Dispatchers.IO) {
        read(timelineFile) { json.decodeFromString(ListSerializer(StorySummary.serializer()), it) } ?: emptyList()
    }

    override suspend fun saveTimeline(items: List<StorySummary>) = withContext(Dispatchers.IO) {
        write(timelineFile, json.encodeToString(ListSerializer(StorySummary.serializer()), items))
    }

    override suspend fun loadStory(id: Long): Story? = withContext(Dispatchers.IO) {
        read(File(storiesDir, "$id.json")) { json.decodeFromString(Story.serializer(), it) }
    }

    override suspend fun saveStory(story: Story) = withContext(Dispatchers.IO) {
        write(File(storiesDir, "${story.id}.json"), json.encodeToString(Story.serializer(), story))
        storiesDir.listFiles()?.sortedByDescending { it.lastModified() }?.drop(MAX_STORIES)?.forEach { it.delete() }
        Unit
    }

    override suspend fun loadFollowed(): List<String> = withContext(Dispatchers.IO) {
        read(followedFile) { json.decodeFromString(slugsSerializer, it) } ?: emptyList()
    }

    override suspend fun saveFollowed(slugs: List<String>) = withContext(Dispatchers.IO) {
        write(followedFile, json.encodeToString(slugsSerializer, slugs))
    }

    override suspend fun clear() {
        withContext(Dispatchers.IO) { dir.deleteRecursively() }
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
    }
}
