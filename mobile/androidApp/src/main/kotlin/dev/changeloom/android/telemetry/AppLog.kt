package dev.changeloom.android.telemetry

import android.util.Log
import com.google.firebase.crashlytics.FirebaseCrashlytics
import dev.changeloom.android.BuildConfig
import dev.changeloom.shared.data.isUnexpected

/**
 * Debug builds write to logcat. Release builds keep warnings as Crashlytics breadcrumbs (sent with the
 * next crash or non-fatal) and send errors as non-fatals. Never pass PII: no emails, tokens or user ids.
 */
object AppLog {
    fun w(tag: String, message: String, error: Throwable? = null) {
        if (BuildConfig.DEBUG) {
            Log.w(tag, message, error)
        } else {
            FirebaseCrashlytics.getInstance().log("W/$tag: $message${error?.let { " ($it)" }.orEmpty()}")
        }
    }

    fun e(tag: String, message: String, error: Throwable) {
        if (BuildConfig.DEBUG) {
            Log.e(tag, message, error)
        } else {
            FirebaseCrashlytics.getInstance().apply {
                log("E/$tag: $message")
                recordException(error)
            }
        }
    }

    /** A failed operation: an error when it looks like a bug or a server fault, else a warning (offline, 4xx). */
    fun failure(tag: String, message: String, error: Throwable) =
        if (isUnexpected(error)) e(tag, message, error) else w(tag, message, error)
}
