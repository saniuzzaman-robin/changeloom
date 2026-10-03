package dev.changeloom.shared.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

/**
 * Collects the ids of stories the user has seen in the feed and reports them in batches. An id is reported at most
 * once per session; ids whose [send] failed stay queued for the next [flush].
 */
class ViewTracker(
    private val maxBatch: Int = ChangeloomApi.MAX_VIEW_IDS,
    private val send: suspend (List<Long>) -> Unit,
) {
    private val mutex = Mutex()
    private val known = HashSet<Long>()
    private val pending = LinkedHashSet<Long>()

    suspend fun seen(id: Long) = mutex.withLock {
        if (known.add(id)) pending += id
    }

    /** Sends everything queued, [maxBatch] ids per call. Stops at the first failure and keeps the unsent ids. */
    suspend fun flush() = mutex.withLock {
        while (pending.isNotEmpty()) {
            val batch = pending.take(maxBatch)
            try {
                send(batch)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                return@withLock
            }
            pending.removeAll(batch.toSet())
        }
    }

    /** Flushes every [intervalMillis] until cancelled. */
    suspend fun run(intervalMillis: Long): Nothing {
        while (true) {
            delay(intervalMillis)
            flush()
        }
    }
}
