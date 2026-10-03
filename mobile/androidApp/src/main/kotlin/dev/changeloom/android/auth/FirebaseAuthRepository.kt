package dev.changeloom.android.auth

import com.google.firebase.auth.AuthCredential
import com.google.firebase.auth.EmailAuthProvider
import com.google.firebase.auth.FirebaseAuth
import com.google.firebase.auth.FirebaseAuthRecentLoginRequiredException
import com.google.firebase.auth.FirebaseUser
import com.google.firebase.auth.GoogleAuthProvider
import dev.changeloom.shared.auth.AuthRepository
import dev.changeloom.shared.auth.AuthUser
import dev.changeloom.shared.auth.ReauthRequiredException
import dev.changeloom.shared.auth.SignInMethod
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.tasks.await

/** Firebase wants a sign-in within about 5 minutes before deleting an account; this leaves a margin. */
private const val RECENT_SIGN_IN_MS = 4 * 60 * 1000L

class FirebaseAuthRepository(
    private val auth: FirebaseAuth = FirebaseAuth.getInstance(),
) : AuthRepository {
    private val user = MutableStateFlow(auth.currentUser?.toAuthUser())
    override val currentUser: StateFlow<AuthUser?> = user

    /** When this process last re-authenticated; the user's metadata keeps the sign-in time it loaded with. */
    private var reauthenticatedAt = 0L

    init {
        auth.addAuthStateListener { user.value = it.currentUser?.toAuthUser() }
    }

    override suspend fun idToken(forceRefresh: Boolean): String? = auth.currentUser?.getIdToken(forceRefresh)?.await()?.token

    override suspend fun signInWithEmail(email: String, password: String) {
        auth.signInWithEmailAndPassword(email, password).await()
    }

    override suspend fun registerWithEmail(email: String, password: String) {
        auth.createUserWithEmailAndPassword(email, password).await()
    }

    override suspend fun signInWithGoogleIdToken(googleIdToken: String): Boolean =
        auth.signInWithCredential(GoogleAuthProvider.getCredential(googleIdToken, null)).await()
            .additionalUserInfo?.isNewUser == true

    override fun signOut() = auth.signOut()

    override fun needsReauth(): Boolean {
        val signedInAt = maxOf(auth.currentUser?.metadata?.lastSignInTimestamp ?: 0L, reauthenticatedAt)
        return System.currentTimeMillis() - signedInAt > RECENT_SIGN_IN_MS
    }

    override suspend fun reauthenticateWithPassword(password: String) {
        val email = checkNotNull(signedInUser().email) { "The account has no email to re-authenticate with" }
        reauthenticate(EmailAuthProvider.getCredential(email, password))
    }

    override suspend fun reauthenticateWithGoogleIdToken(googleIdToken: String) =
        reauthenticate(GoogleAuthProvider.getCredential(googleIdToken, null))

    override suspend fun deleteUser() {
        try {
            signedInUser().delete().await()
        } catch (e: FirebaseAuthRecentLoginRequiredException) {
            throw ReauthRequiredException(e)
        }
    }

    private suspend fun reauthenticate(credential: AuthCredential) {
        signedInUser().reauthenticate(credential).await()
        reauthenticatedAt = System.currentTimeMillis()
    }

    private fun signedInUser(): FirebaseUser = checkNotNull(auth.currentUser) { "Not signed in" }

    /** The account's own photo if one was set, else the one from a linked Google sign-in. */
    private fun FirebaseUser.toAuthUser(): AuthUser {
        val google = providerData.firstOrNull { it.providerId == GoogleAuthProvider.PROVIDER_ID }
        return AuthUser(
            uid,
            email,
            displayName?.takeIf { it.isNotBlank() },
            (photoUrl ?: google?.photoUrl)?.toString(),
            if (google != null) SignInMethod.Google else SignInMethod.Password,
        )
    }
}
