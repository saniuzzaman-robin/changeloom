package dev.changeloom.android.ads

import com.google.firebase.remoteconfig.FirebaseRemoteConfig
import com.google.firebase.remoteconfig.FirebaseRemoteConfigSettings
import dev.changeloom.android.BuildConfig
import dev.changeloom.android.telemetry.AppLog
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.tasks.await

private const val TAG = "AdsConfig"
private const val KEY_ENABLED = "ads_enabled"
private const val KEY_INTERVAL = "ads_interval"
private const val DEFAULT_ENABLED = true
private const val DEFAULT_INTERVAL = 6L

/** Bounds on the gap Remote Config may set, so a bad value can't fill the feed with ads. */
private const val MIN_INTERVAL = 3L
private const val MAX_INTERVAL = 100L

/** Debug builds fetch often so console changes show up while testing; release keeps the SDK's 12h default. */
private const val DEBUG_FETCH_INTERVAL_S = 60L

/** [enabled] is the kill switch; [interval] is how many stories sit between two feed ads. */
data class AdsFlags(val enabled: Boolean = DEFAULT_ENABLED, val interval: Int = DEFAULT_INTERVAL.toInt())

/** Remote Config's `ads_enabled` and `ads_interval`. Until the first fetch (or offline) the in-app defaults apply. */
class AdsConfig {
    private val config by lazy { FirebaseRemoteConfig.getInstance() }
    private val _flags = MutableStateFlow(AdsFlags())
    val flags: StateFlow<AdsFlags> = _flags.asStateFlow()

    /** Applies the cached values, then fetches newer ones (throttled by the SDK). */
    suspend fun refresh() {
        try {
            if (BuildConfig.DEBUG) {
                config.setConfigSettingsAsync(
                    FirebaseRemoteConfigSettings.Builder().setMinimumFetchIntervalInSeconds(DEBUG_FETCH_INTERVAL_S).build(),
                ).await()
            }
            config.setDefaultsAsync(mapOf(KEY_ENABLED to DEFAULT_ENABLED, KEY_INTERVAL to DEFAULT_INTERVAL)).await()
            _flags.value = read()
            config.fetchAndActivate().await()
            _flags.value = read()
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            AppLog.w(TAG, "Couldn't fetch Remote Config; using cached or default ad settings", e)
        }
    }

    private fun read() = AdsFlags(
        enabled = config.getBoolean(KEY_ENABLED),
        interval = config.getLong(KEY_INTERVAL).coerceIn(MIN_INTERVAL, MAX_INTERVAL).toInt(),
    )
}
