package dev.changeloom.android.push

import android.app.Notification
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import androidx.core.app.NotificationManagerCompat
import androidx.work.CoroutineWorker
import androidx.work.Data
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
private const val SLOT_KEY = "slot"

/**
 * WorkManager may run a job later than asked (Doze, battery saver, standby) and, after a time zone or clock change,
 * at the wrong local time. A digest outside this window around its slot is skipped instead of shown at an odd hour.
 */
private val EARLY_TOLERANCE = Duration.ofMinutes(5)
private val LATE_TOLERANCE = Duration.ofMinutes(30)

/** The first digest time strictly after [now], so a run at 9:00 plans 17:00. */
internal fun nextDigestSlot(now: LocalDateTime): LocalDateTime {
    val today = now.toLocalDate()
    return DIGEST_TIMES.map { today.atTime(it) }.firstOrNull { it.isAfter(now) }
        ?: today.plusDays(1).atTime(DIGEST_TIMES.first())
}

/** Time from [now] until the next digest time. */
internal fun delayUntilNextDigest(now: LocalDateTime): Duration = Duration.between(now, nextDigestSlot(now))

/** Whether [now] is close enough to the [slot] the run was queued for to post the digest. */
internal fun isOnTime(slot: LocalDateTime, now: LocalDateTime): Boolean =
    !now.isBefore(slot.minus(EARLY_TOLERANCE)) && !now.isAfter(slot.plus(LATE_TOLERANCE))

internal enum class DigestPeriod { Morning, Evening, Night }

/** The greeting follows the slot the digest is for, not the moment the run happens to start. */
internal fun digestPeriod(slot: LocalDateTime): DigestPeriod = when {
    slot.hour >= NIGHT_START_HOUR -> DigestPeriod.Night
    slot.hour >= EVENING_START_HOUR -> DigestPeriod.Evening
    else -> DigestPeriod.Morning
}

/** Queues the next digest unless one is already queued; call at app start. */
fun scheduleDailyDigest(context: Context) = enqueue(context, ExistingWorkPolicy.KEEP)

/** Replaces the queued digest; call when the time zone or clock changed, since its delay was for the old local time. */
internal fun rescheduleDailyDigest(context: Context) = enqueue(context, ExistingWorkPolicy.REPLACE)

private fun enqueue(context: Context, policy: ExistingWorkPolicy) {
    val now = LocalDateTime.now()
    val slot = nextDigestSlot(now)
    val request = OneTimeWorkRequestBuilder<DailyDigestWorker>()
        .setInitialDelay(Duration.between(now, slot).toMillis(), TimeUnit.MILLISECONDS)
        .setInputData(Data.Builder().putString(SLOT_KEY, slot.toString()).build())
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
            // A job queued before slots were recorded has none: post it once, as before.
            val slot = inputData.getString(SLOT_KEY)?.let(LocalDateTime::parse)
            if (slot != null && !isOnTime(slot, LocalDateTime.now())) {
                AppLog.w(TAG, "Skipping the digest for $slot: it ran at ${LocalDateTime.now()}")
            } else if (get<AuthRepository>().currentUser.value != null && canNotify()) {
                post(get<ChangeloomApi>().timeline().items, slot ?: LocalDateTime.now())
            }
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

    private fun post(items: List<StorySummary>, slot: LocalDateTime) {
        val stories = digestStories(items)
        if (stories.isEmpty()) return
        val context = applicationContext
        val title = when (digestPeriod(slot)) {
            DigestPeriod.Night -> R.string.digest_title_night
            DigestPeriod.Evening -> R.string.digest_title_evening
            DigestPeriod.Morning -> R.string.digest_title_morning
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
