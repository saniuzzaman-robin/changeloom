package dev.changeloom.android.push

import android.app.Notification
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import androidx.core.app.NotificationManagerCompat
import androidx.work.CoroutineWorker
import androidx.work.ExistingWorkPolicy
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import dev.changeloom.android.MainActivity
import dev.changeloom.android.R
import dev.changeloom.android.telemetry.AppLog
import dev.changeloom.shared.auth.AuthRepository
import dev.changeloom.shared.data.ChangeloomApi
import dev.changeloom.shared.data.StorySummary
import kotlinx.coroutines.CancellationException
import org.koin.core.component.KoinComponent
import org.koin.core.component.get
import java.time.Duration
import java.time.LocalDateTime
import java.time.LocalTime
import java.util.concurrent.TimeUnit

private const val TAG = "DailyDigest"
private const val WORK_NAME = "daily_digest"
private const val NOTIFICATION_ID = 0x0D16E57
private const val PREVIEW_COUNT = 3

/** Local times the digest goes out. */
private val DIGEST_TIMES = listOf(LocalTime.of(9, 0), LocalTime.of(17, 0), LocalTime.of(21, 30))
private const val EVENING_START_HOUR = 12
private const val NIGHT_START_HOUR = 20

/** Time from [now] until the next digest time; strictly in the future, so a run at 9:00 plans 17:00. */
internal fun delayUntilNextDigest(now: LocalDateTime): Duration {
    val today = now.toLocalDate()
    val next = DIGEST_TIMES.map { today.atTime(it) }.firstOrNull { it.isAfter(now) }
        ?: today.plusDays(1).atTime(DIGEST_TIMES.first())
    return Duration.between(now, next)
}

/** Queues the next digest unless one is already queued; call at app start. */
fun scheduleDailyDigest(context: Context) = enqueue(context, ExistingWorkPolicy.KEEP)

private fun enqueue(context: Context, policy: ExistingWorkPolicy) {
    val request = OneTimeWorkRequestBuilder<DailyDigestWorker>()
        .setInitialDelay(delayUntilNextDigest(LocalDateTime.now()).toMillis(), TimeUnit.MILLISECONDS)
        .build()
    WorkManager.getInstance(context).enqueueUniqueWork(WORK_NAME, policy, request)
}

/**
 * Unread stories matching the user's topics and professions, or any unread story when none match; most important
 * first, ties in timeline order, so the previews are the stories that matter most.
 */
internal fun digestStories(items: List<StorySummary>): List<StorySummary> {
    val unread = items.filter { !it.isRead }
    val mine = unread.filter { it.match == "followed" || it.match == "profession" }
    return mine.ifEmpty { unread }.sortedByDescending { it.importance }
}

/**
 * Posts the morning or evening digest: a friendly title, how many unread stories match the user's interests and
 * the first few titles. It queues the next one itself, so the schedule follows the phone's clock and time zone.
 */
class DailyDigestWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params), KoinComponent {
    override suspend fun doWork(): Result {
        try {
            if (get<AuthRepository>().currentUser.value != null && canNotify()) post(get<ChangeloomApi>().timeline().items)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            // Best-effort: skip this slot, the next one still runs.
            AppLog.w(TAG, "Could not build the daily digest", e)
        }
        enqueue(applicationContext, ExistingWorkPolicy.REPLACE)
        return Result.success()
    }

    // Covers both the Android 13+ permission and notifications switched off in settings; the permission alone
    // doesn't exist before 13, where checking it always fails.
    private fun canNotify() = NotificationManagerCompat.from(applicationContext).areNotificationsEnabled()

    private fun post(items: List<StorySummary>) {
        val stories = digestStories(items)
        if (stories.isEmpty()) return
        val context = applicationContext
        val hour = LocalTime.now().hour
        val title = when {
            hour >= NIGHT_START_HOUR -> R.string.digest_title_night
            hour >= EVENING_START_HOUR -> R.string.digest_title_evening
            else -> R.string.digest_title_morning
        }
        val previews = stories.take(PREVIEW_COUNT).joinToString("\n") { "• ${it.title}" }
        val more = stories.size - PREVIEW_COUNT
        val body = if (more > 0) previews + "\n" + context.getString(R.string.digest_more, more) else previews
        val open = Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP)
        val pending = PendingIntent.getActivity(context, NOTIFICATION_ID, open, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
        val summary = context.resources.getQuantityString(R.plurals.digest_summary, stories.size, stories.size)
        val notification = Notification.Builder(context, context.getString(R.string.daily_digest_channel_id))
            .setSmallIcon(R.drawable.ic_stat_changeloom)
            .setContentTitle(context.getString(title))
            .setContentText(summary)
            .setStyle(Notification.BigTextStyle().setSummaryText(summary).bigText(body))
            .setContentIntent(pending)
            .setAutoCancel(true)
            .build()
        context.getSystemService(NotificationManager::class.java).notify(NOTIFICATION_ID, notification)
    }
}
