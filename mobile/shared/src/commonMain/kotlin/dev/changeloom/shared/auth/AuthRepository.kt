package dev.changeloom.shared.auth

import kotlinx.coroutines.flow.StateFlow

data class AuthUser(
    val uid: String,
    val email: String?,
    val displayName: String? = null,
    val photoUrl: String? = null,
    /** How the user proves who they are again before a sensitive change (Google when linked, else password). */
    val signInMethod: SignInMethod = SignInMethod.Password,
)

enum class SignInMethod { Password, Google }

/** The account was signed in too long ago for this change: re-authenticate, then retry. */
class ReauthRequiredException(cause: Throwable? = null) : Exception("Sign in again to continue", cause)

/** Platform-specific sign-in; the API client only needs [idToken]. */
interface AuthRepository {
    val currentUser: StateFlow<AuthUser?>

    /** Firebase ID token for the signed-in user, or null when signed out. [forceRefresh] skips the cached token. */
    suspend fun idToken(forceRefresh: Boolean = false): String?

    suspend fun signInWithEmail(email: String, password: String)
    suspend fun registerWithEmail(email: String, password: String)
    /** Returns true when this sign-in created the account. */
    suspend fun signInWithGoogleIdToken(googleIdToken: String): Boolean
    fun signOut()

    /** True when the last sign-in is too old for [deleteUser]; re-authenticate first. */
    fun needsReauth(): Boolean
    suspend fun reauthenticateWithPassword(password: String)
    suspend fun reauthenticateWithGoogleIdToken(googleIdToken: String)

    /** Deletes the sign-in account, which also signs out. Throws [ReauthRequiredException] when it needs a fresh sign-in. */
    suspend fun deleteUser()
}
