package dev.changeloom.android.play

import android.app.Activity
import androidx.activity.compose.LocalActivity
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.google.android.play.core.review.ReviewManagerFactory
import dev.changeloom.android.telemetry.AppLog
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.tasks.await
import org.koin.compose.koinInject

private const val TAG = "Review"

/** Stories read before the app asks for a review. */
internal const val READS_BEFORE_REVIEW = 5

/** At most one review prompt per 90 days. */
internal const val REVIEW_INTERVAL_MS = 90L * 24 * 60 * 60 * 1000

/** Where [ReviewPrompter] keeps its counts. */
interface ReviewStore {
    var storyReads: Int
    var lastPromptAt: Long
}

/**
 * Decides when to ask for a Play review: after [READS_BEFORE_REVIEW] story reads, and at most once per
 * [REVIEW_INTERVAL_MS]. Play itself may still decide not to show the dialog.
 */
class ReviewPrompter(private val store: ReviewStore, private val now: () -> Long = System::currentTimeMillis) {
    private val _due = MutableStateFlow(false)
    val due: StateFlow<Boolean> = _due.asStateFlow()

    fun storyRead() {
        store.storyReads += 1
        if (store.storyReads >= READS_BEFORE_REVIEW && now() - store.lastPromptAt >= REVIEW_INTERVAL_MS) _due.value = true
    }

    /** Call when the prompt is about to show; counting starts over. */
    fun prompted() {
        store.storyReads = 0
        store.lastPromptAt = now()
        _due.value = false
    }
}

/** Shows Play's review dialog when one is due and [enabled] (back on the feed, not mid-story). */
@Composable
fun ReviewPromptEffect(enabled: Boolean, prompter: ReviewPrompter = koinInject()) {
    val due by prompter.due.collectAsStateWithLifecycle()
    val activity = LocalActivity.current
    LaunchedEffect(due && enabled, activity) {
        if (due && enabled && activity != null) {
            // Recorded first, so a failing flow is never retried on every visit.
            prompter.prompted()
            launchReview(activity)
        }
    }
}

private suspend fun launchReview(activity: Activity) {
    try {
        val manager = ReviewManagerFactory.create(activity)
        manager.launchReviewFlow(activity, manager.requestReviewFlow().await()).await()
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        AppLog.w(TAG, "Couldn't show the review dialog", e)
    }
}
