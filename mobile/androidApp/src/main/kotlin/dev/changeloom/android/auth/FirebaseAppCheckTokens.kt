package dev.changeloom.android.auth

import android.os.SystemClock
import com.google.firebase.appcheck.FirebaseAppCheck
import dev.changeloom.android.telemetry.AppLog
import dev.changeloom.shared.data.AppCheckTokens
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.TimeoutCancellationException
import kotlinx.coroutines.tasks.await
import kotlinx.coroutines.withTimeout

private const val TAG = "AppCheck"

/** A fresh attestation can take a while; requests don't wait longer than this for it. */
private const val TOKEN_TIMEOUT_MS = 3_000L

/** After a failure, requests skip App Check for this long instead of each paying for another attempt. */
private const val RETRY_AFTER_FAILURE_MS = 5 * 60 * 1000L

/**
 * App Check tokens for the api. The provider (Play Integrity, or the debug one in debug builds) is installed on
 * the first request rather than at app start. The SDK caches and refreshes the token.
 */
class FirebaseAppCheckTokens : AppCheckTokens {
    private val appCheck by lazy {
        FirebaseAppCheck.getInstance().apply { installAppCheckProviderFactory(appCheckProviderFactory()) }
    }

    @Volatile private var failedAt: Long? = null

    /** Null when attestation fails: the request still goes out, and the api decides (APPCHECK_ENFORCE). */
    override suspend fun token(): String? {
        failedAt?.let { if (SystemClock.elapsedRealtime() - it < RETRY_AFTER_FAILURE_MS) return null }
        return try {
            withTimeout(TOKEN_TIMEOUT_MS) { appCheck.getAppCheckToken(false).await().token }.also { failedAt = null }
        } catch (e: CancellationException) {
            // withTimeout's own cancellation is a failure; the caller's is passed on.
            if (e !is TimeoutCancellationException) throw e
            failed(e)
        } catch (e: Exception) {
            failed(e)
        }
    }

    private fun failed(e: Exception): String? {
        AppLog.w(TAG, "Couldn't get an App Check token; skipping it for a while", e)
        failedAt = SystemClock.elapsedRealtime()
        return null
    }
}
