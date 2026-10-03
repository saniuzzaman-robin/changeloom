package dev.changeloom.shared.data

import kotlinx.io.IOException

/**
 * The text to show for a failed call. [failed] says what went wrong ("Couldn't load the timeline");
 * the cause adds what the user can do about it. Raw exception messages never reach the UI.
 */
fun userMessage(error: Throwable, failed: String): String = when {
    error is ApiException && error.status == 401 -> "Your session has expired. Sign in again."
    error is ApiException && error.status == 403 && error.code == APP_CHECK_FAILED ->
        "$failed: this copy of the app couldn't be verified. Install Changeloom from Google Play."
    error is ApiException && error.status == 404 -> "$failed: it isn't available any more."
    error is ApiException && error.status == 429 -> "$failed: too many requests. Wait a minute and try again."
    error is ApiException && error.status >= 500 -> "$failed. Changeloom is having trouble; try again soon."
    error is IOException -> "$failed. Check your connection and try again."
    else -> "$failed. Try again."
}

/** The api's error code when a request has no valid App Check token. */
private const val APP_CHECK_FAILED = "app_check_failed"

/** True for failures worth reporting as non-fatals: not the network, and not a client error the server explained. */
fun isUnexpected(error: Throwable): Boolean = when (error) {
    is ApiException -> error.status >= 500
    is IOException -> false
    else -> true
}
