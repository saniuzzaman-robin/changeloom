package dev.changeloom.android.telemetry

import android.content.Context
import android.os.Bundle
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import com.google.firebase.analytics.FirebaseAnalytics
import org.koin.compose.koinInject

/** Product analytics. Events carry no user id or PII; story ids and counts only. */
interface Analytics {
    fun screenView(name: String)
    fun login(method: String)
    fun signUp(method: String)
    fun storyOpen(storyId: Long)
    fun bookmarkAdd(storyId: Long)
    fun share(storyId: Long)
    fun search()
    fun topicsUpdate(count: Int, onboarding: Boolean)
    fun topicRequest()
    fun notificationOpen(storyId: Long)

    /** Google consent mode, from the user's ad consent (see ConsentManager). */
    fun setConsent(consent: AnalyticsConsent)

    companion object {
        const val METHOD_PASSWORD = "password"
        const val METHOD_GOOGLE = "google"
    }
}

/** Google consent mode signals. */
data class AnalyticsConsent(
    val analyticsStorage: Boolean,
    val adStorage: Boolean,
    val adUserData: Boolean,
    val adPersonalization: Boolean,
) {
    companion object {
        private val Granted = AnalyticsConsent(analyticsStorage = true, adStorage = true, adUserData = true, adPersonalization = true)

        /**
         * Maps IAB TCF v2 purpose consents ('0'/'1' per purpose, purpose 1 first) to consent mode, following Google's
         * mapping: storage needs purpose 1 (store or access information on a device), ad user data purposes 1 and 7
         * (measure ad performance), ad personalization purposes 3 and 4 (personalised ads profile and selection).
         * Where GDPR doesn't apply, everything is granted.
         */
        fun fromTcf(gdprApplies: Boolean, purposeConsents: String): AnalyticsConsent {
            if (!gdprApplies) return Granted
            fun purpose(n: Int) = purposeConsents.getOrNull(n - 1) == '1'
            return AnalyticsConsent(
                analyticsStorage = purpose(1),
                adStorage = purpose(1),
                adUserData = purpose(1) && purpose(7),
                adPersonalization = purpose(3) && purpose(4),
            )
        }
    }
}

/** GA4 event names: the recommended ones where they exist, so they show up in the standard reports. */
class FirebaseAnalyticsTracker(context: Context) : Analytics {
    private val analytics = FirebaseAnalytics.getInstance(context)

    override fun screenView(name: String) = log(FirebaseAnalytics.Event.SCREEN_VIEW) {
        putString(FirebaseAnalytics.Param.SCREEN_NAME, name)
    }

    override fun login(method: String) = log(FirebaseAnalytics.Event.LOGIN) { putString(FirebaseAnalytics.Param.METHOD, method) }

    override fun signUp(method: String) = log(FirebaseAnalytics.Event.SIGN_UP) { putString(FirebaseAnalytics.Param.METHOD, method) }

    override fun storyOpen(storyId: Long) = log("story_open") { putString(FirebaseAnalytics.Param.ITEM_ID, storyId.toString()) }

    override fun bookmarkAdd(storyId: Long) = log("bookmark_add") { putString(FirebaseAnalytics.Param.ITEM_ID, storyId.toString()) }

    override fun share(storyId: Long) = log(FirebaseAnalytics.Event.SHARE) {
        putString(FirebaseAnalytics.Param.CONTENT_TYPE, "story")
        putString(FirebaseAnalytics.Param.ITEM_ID, storyId.toString())
    }

    // The query itself is never sent: users can type anything into it.
    override fun search() = log(FirebaseAnalytics.Event.SEARCH) {}

    override fun topicsUpdate(count: Int, onboarding: Boolean) = log("topics_update") {
        putLong("topic_count", count.toLong())
        putLong("onboarding", if (onboarding) 1 else 0)
    }

    override fun topicRequest() = log("topic_request") {}

    override fun notificationOpen(storyId: Long) = log("notification_open") {
        putString(FirebaseAnalytics.Param.ITEM_ID, storyId.toString())
    }

    override fun setConsent(consent: AnalyticsConsent) {
        fun status(granted: Boolean) = if (granted) FirebaseAnalytics.ConsentStatus.GRANTED else FirebaseAnalytics.ConsentStatus.DENIED
        analytics.setConsent(
            mapOf(
                FirebaseAnalytics.ConsentType.ANALYTICS_STORAGE to status(consent.analyticsStorage),
                FirebaseAnalytics.ConsentType.AD_STORAGE to status(consent.adStorage),
                FirebaseAnalytics.ConsentType.AD_USER_DATA to status(consent.adUserData),
                FirebaseAnalytics.ConsentType.AD_PERSONALIZATION to status(consent.adPersonalization),
            ),
        )
    }

    private fun log(event: String, params: Bundle.() -> Unit) = analytics.logEvent(event, Bundle().apply(params))
}

/** Logs a screen_view for [name] once per visit; rotation doesn't count as a new visit. */
@Composable
fun TrackScreen(name: String, analytics: Analytics = koinInject()) {
    var logged by rememberSaveable { mutableStateOf<String?>(null) }
    LaunchedEffect(name) {
        if (logged != name) {
            analytics.screenView(name)
            logged = name
        }
    }
}
