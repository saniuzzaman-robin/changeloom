package dev.changeloom.shared.auth

import kotlinx.coroutines.flow.StateFlow

data class AuthUser(
    val uid: String,
    val email: String?,
    val displayName: String? = null,
    val photoUrl: String? = null,
)

/** Platform-specific sign-in; the API client only needs [idToken]. */
interface AuthRepository {
    val currentUser: StateFlow<AuthUser?>

    /** Fresh Firebase ID token for the signed-in user, or null when signed out. */
    suspend fun idToken(): String?

    suspend fun signInWithEmail(email: String, password: String)
    suspend fun registerWithEmail(email: String, password: String)
    suspend fun signInWithGoogleIdToken(googleIdToken: String)
    fun signOut()
}
