package dev.changeloom.android.data

import android.content.Context
import androidx.core.content.edit
import dev.changeloom.android.telemetry.AppLog
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.Json

private const val TAG = "FollowSuggester"
private const val FILE = "follow_suggestions"
private const val KEY_OPENS = "opens"
private const val KEY_DECLINED = "declined"

/** Opens of a topic's stories before the feed suggests following it. */
const val FOLLOW_SUGGESTION_OPENS = 3

/** Topics whose opens are kept; the least opened go first, so the file stays small. */
private const val MAX_TRACKED_TOPICS = 100

/** How often the user opened each topic's stories, and the suggestions they turned down. */
data class OpenCounts(val opens: Map<String, Int> = emptyMap(), val declined: Set<String> = emptySet())

/**
 * The topic to suggest: the most opened one with at least [FOLLOW_SUGGESTION_OPENS] opens that isn't declined and
 * isn't covered by [followed] (the topic itself or its root). Ties go to the first slug.
 */
fun OpenCounts.suggestion(followed: Set<String>): String? =
    opens.entries
        .filter { (slug, n) -> n >= FOLLOW_SUGGESTION_OPENS && slug !in declined && slug !in followed && slug.substringBefore('/') !in followed }
        .maxWithOrNull(compareBy<Map.Entry<String, Int>> { it.value }.thenByDescending { it.key })
        ?.key

/**
 * Counts story opens per topic so the feed can suggest following a topic the user keeps reading. The counts belong
 * to the signed-in account: [clear] runs on sign-out. Disk access runs on [Dispatchers.IO].
 */
class FollowSuggester(context: Context) {
    private val prefs by lazy { context.getSharedPreferences(FILE, Context.MODE_PRIVATE) }
    private val mutex = Mutex()
    private var loaded = false

    private val _counts = MutableStateFlow(OpenCounts())
    val counts: StateFlow<OpenCounts> = _counts.asStateFlow()

    /** Reads the saved counts once; later calls return at once. */
    suspend fun load() = mutex.withLock { ensureLoaded() }

    suspend fun recordOpen(topics: List<String>) = change { c ->
        val opens = c.opens.toMutableMap()
        topics.distinct().forEach { opens[it] = (opens[it] ?: 0) + 1 }
        val kept = if (opens.size <= MAX_TRACKED_TOPICS) opens else opens.entries.sortedByDescending { it.value }.take(MAX_TRACKED_TOPICS).associate { it.toPair() }
        c.copy(opens = kept)
    }

    /** Never suggest [slug] again ("Not now", or the user muted it). */
    suspend fun decline(slug: String) = change { it.copy(declined = it.declined + slug) }

    suspend fun clear() = mutex.withLock {
        withContext(Dispatchers.IO) { prefs.edit { clear() } }
        _counts.value = OpenCounts()
        loaded = true
    }

    private suspend fun change(update: (OpenCounts) -> OpenCounts) = mutex.withLock {
        ensureLoaded()
        val next = update(_counts.value)
        _counts.value = next
        withContext(Dispatchers.IO) {
            prefs.edit {
                putString(KEY_OPENS, Json.encodeToString(next.opens))
                putStringSet(KEY_DECLINED, next.declined)
            }
        }
    }

    private suspend fun ensureLoaded() {
        if (loaded) return
        _counts.value = withContext(Dispatchers.IO) {
            val opens = prefs.getString(KEY_OPENS, null)?.let {
                try {
                    Json.decodeFromString<Map<String, Int>>(it)
                } catch (e: SerializationException) {
                    AppLog.w(TAG, "Unreadable topic open counts; starting over", e)
                    emptyMap()
                }
            }.orEmpty()
            OpenCounts(opens, prefs.getStringSet(KEY_DECLINED, null).orEmpty().toSet())
        }
        loaded = true
    }
}
