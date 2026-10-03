package dev.changeloom.android.ads

import android.content.Context
import android.os.SystemClock
import androidx.compose.runtime.Immutable
import com.google.android.gms.ads.AdListener
import com.google.android.gms.ads.AdLoader
import com.google.android.gms.ads.AdRequest
import com.google.android.gms.ads.LoadAdError
import com.google.android.gms.ads.MobileAds
import com.google.android.gms.ads.nativead.NativeAd
import com.google.android.gms.ads.nativead.NativeAdOptions
import dev.changeloom.android.telemetry.AppLog
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

private const val TAG = "Ads"

/** Ads kept loaded for the feed. Two or more means two ad slots on screen never share one. */
private const val POOL_SIZE = 3

/** AdMob native ads expire an hour after loading. */
private const val AD_MAX_AGE_MS = 60 * 60 * 1000L

/** The first feed ad goes after this many stories. */
internal const val FIRST_AD_AFTER = 3

/** The ad slot after the story at [index] (0-based): after story [FIRST_AD_AFTER], then every [interval] stories. */
internal fun adSlotAfter(index: Int, interval: Int): Int? {
    val sinceFirst = index + 1 - FIRST_AD_AFTER
    return if (sinceFirst >= 0 && sinceFirst % interval == 0) sinceFirst / interval else null
}

/** The ads the feed shows and where. */
@Immutable
class FeedAds(private val ads: List<NativeAd>, private val interval: Int) {
    fun slotAfter(index: Int): Int? = adSlotAfter(index, interval)

    /** The ad for [slot]; with a single ad loaded only the first slot shows it, so it never shows twice at once. */
    fun adFor(slot: Int): NativeAd? = if (ads.size == 1 && slot > 0) null else ads.getOrNull(slot % ads.size)
}

/**
 * A small pool of native ads for the feed. Nothing loads until consent allows ads and Remote Config has them on;
 * MobileAds is initialised then, once and off the main thread. [refresh] reloads the pool once it is an hour old.
 */
class NativeAdRepository(
    private val context: Context,
    private val adUnitId: String,
    private val consent: ConsentManager,
    private val config: AdsConfig,
    private val scope: CoroutineScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate),
) {
    private val pool = MutableStateFlow<List<NativeAd>>(emptyList())
    private var loadedAt = 0L
    private var loader: AdLoader? = null
    private var started = false
    private var initialized = false

    private val allowed: StateFlow<Boolean> = combine(consent.canRequestAds, config.flags) { canRequest, flags -> canRequest && flags.enabled }
        .stateIn(scope, SharingStarted.Eagerly, false)

    /** What the feed shows; null while ads are off or none are loaded. */
    val feedAds: StateFlow<FeedAds?> = combine(pool, config.flags, allowed) { ads, flags, on ->
        if (on && ads.isNotEmpty()) FeedAds(ads, flags.interval) else null
    }.stateIn(scope, SharingStarted.Eagerly, null)

    /** Call when the feed shows and when it refreshes. */
    fun refresh() {
        if (!started) {
            started = true
            scope.launch { config.refresh() }
            scope.launch {
                allowed.collect { on -> if (on) load() else clear() }
            }
        } else if (allowed.value) {
            scope.launch { load() }
        }
    }

    private suspend fun load() {
        if (loader?.isLoading == true) return
        if (pool.value.isNotEmpty() && SystemClock.elapsedRealtime() - loadedAt < AD_MAX_AGE_MS) return
        if (!initialized) {
            initialized = true
            withContext(Dispatchers.IO) { MobileAds.initialize(context) }
        }
        var fresh = 0
        loader = AdLoader.Builder(context, adUnitId)
            .forNativeAd { ad ->
                // The first ad of a load replaces the expired pool; the rest join it.
                if (!allowed.value) {
                    ad.destroy()
                } else if (fresh++ == 0) {
                    replace(listOf(ad))
                    loadedAt = SystemClock.elapsedRealtime()
                } else {
                    pool.value += ad
                }
            }
            .withAdListener(object : AdListener() {
                override fun onAdFailedToLoad(error: LoadAdError) {
                    AppLog.w(TAG, "Native ad failed to load: ${error.code} ${error.message}")
                }
            })
            .withNativeAdOptions(
                NativeAdOptions.Builder()
                    .setAdChoicesPlacement(NativeAdOptions.ADCHOICES_TOP_RIGHT)
                    .setMediaAspectRatio(NativeAdOptions.NATIVE_MEDIA_ASPECT_RATIO_LANDSCAPE)
                    .build(),
            )
            .build()
            .also { it.loadAds(AdRequest.Builder().build(), POOL_SIZE) }
    }

    /** Consent withdrawn or ads switched off: drop what is loaded. */
    private fun clear() = replace(emptyList())

    private fun replace(ads: List<NativeAd>) {
        val old = pool.value
        pool.value = ads
        old.forEach(NativeAd::destroy)
    }
}
