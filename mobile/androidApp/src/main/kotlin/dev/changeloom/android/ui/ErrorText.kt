package dev.changeloom.android.ui

import androidx.annotation.StringRes
import androidx.credentials.exceptions.GetCredentialException
import androidx.credentials.exceptions.NoCredentialException
import com.google.firebase.FirebaseNetworkException
import com.google.firebase.FirebaseTooManyRequestsException
import com.google.firebase.auth.FirebaseAuthInvalidCredentialsException
import com.google.firebase.auth.FirebaseAuthInvalidUserException
import com.google.firebase.auth.FirebaseAuthUserCollisionException
import com.google.firebase.auth.FirebaseAuthWeakPasswordException
import dev.changeloom.android.R
import dev.changeloom.shared.data.userMessage

private const val ERROR_INVALID_EMAIL = "ERROR_INVALID_EMAIL"

/**
 * The one place that turns an exception into text for the UI: sign-in errors from Firebase and Credential
 * Manager here, api and network errors in the shared [userMessage]. [failed] says what went wrong.
 */
fun Strings.errorText(error: Throwable, @StringRes failed: Int): String = when (error) {
    is FirebaseAuthWeakPasswordException -> get(R.string.error_weak_password)
    is FirebaseAuthInvalidCredentialsException ->
        get(if (error.errorCode == ERROR_INVALID_EMAIL) R.string.error_invalid_email else R.string.error_wrong_credentials)
    is FirebaseAuthInvalidUserException -> get(R.string.error_no_account)
    is FirebaseAuthUserCollisionException -> get(R.string.error_email_taken)
    is FirebaseTooManyRequestsException -> get(R.string.error_too_many_attempts)
    is FirebaseNetworkException -> get(R.string.error_offline, get(failed))
    is NoCredentialException -> get(R.string.error_no_google_account)
    is GetCredentialException -> get(R.string.error_google_unavailable, get(failed))
    else -> userMessage(error, get(failed))
}
