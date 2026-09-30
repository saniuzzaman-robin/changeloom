package dev.changeloom.android.auth

import android.content.Context
import androidx.credentials.CredentialManager
import androidx.credentials.CustomCredential
import androidx.credentials.GetCredentialRequest
import com.google.android.libraries.identity.googleid.GetGoogleIdOption
import com.google.android.libraries.identity.googleid.GoogleIdTokenCredential

/** Shows the Google account picker and returns a Google ID token for Firebase. */
suspend fun requestGoogleIdToken(context: Context, webClientId: String): String {
    val option = GetGoogleIdOption.Builder()
        .setServerClientId(webClientId)
        .setFilterByAuthorizedAccounts(false)
        .build()
    val result = CredentialManager.create(context)
        .getCredential(context, GetCredentialRequest.Builder().addCredentialOption(option).build())
    val credential = result.credential
    require(credential is CustomCredential && credential.type == GoogleIdTokenCredential.TYPE_GOOGLE_ID_TOKEN_CREDENTIAL) {
        "Unexpected credential type: ${credential.type}"
    }
    return GoogleIdTokenCredential.createFrom(credential.data).idToken
}
