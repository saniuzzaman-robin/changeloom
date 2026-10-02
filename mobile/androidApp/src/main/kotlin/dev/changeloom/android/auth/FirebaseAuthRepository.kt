package dev.changeloom.android.auth

import com.google.firebase.auth.FirebaseAuth
import com.google.firebase.auth.FirebaseUser
import com.google.firebase.auth.GoogleAuthProvider
import dev.changeloom.shared.auth.AuthRepository
import dev.changeloom.shared.auth.AuthUser
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.tasks.await

class FirebaseAuthRepository(
    private val auth: FirebaseAuth = FirebaseAuth.getInstance(),
) : AuthRepository {
    private val user = MutableStateFlow(auth.currentUser?.toAuthUser())
    override val currentUser: StateFlow<AuthUser?> = user

    init {
        auth.addAuthStateListener { user.value = it.currentUser?.toAuthUser() }
    }

    override suspend fun idToken(): String? = auth.currentUser?.getIdToken(false)?.await()?.token

    override suspend fun signInWithEmail(email: String, password: String) {
        auth.signInWithEmailAndPassword(email, password).await()
    }

    override suspend fun registerWithEmail(email: String, password: String) {
        auth.createUserWithEmailAndPassword(email, password).await()
    }

    override suspend fun signInWithGoogleIdToken(googleIdToken: String) {
        auth.signInWithCredential(GoogleAuthProvider.getCredential(googleIdToken, null)).await()
    }

    override fun signOut() = auth.signOut()

    /** The account's own photo if one was set, else the one from a linked Google sign-in. */
    private fun FirebaseUser.toAuthUser() = AuthUser(
        uid,
        email,
        displayName?.takeIf { it.isNotBlank() },
        (photoUrl ?: providerData.firstOrNull { it.providerId == GoogleAuthProvider.PROVIDER_ID }?.photoUrl)?.toString(),
    )
}
