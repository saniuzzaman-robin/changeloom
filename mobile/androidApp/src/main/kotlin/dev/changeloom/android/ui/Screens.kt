package dev.changeloom.android.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.credentials.exceptions.GetCredentialCancellationException
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.changeloom.android.auth.requestGoogleIdToken
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import org.koin.androidx.compose.koinViewModel

@Composable
fun AppRoot(signedIn: Boolean, onSignOut: () -> Unit, openStoryId: Long? = null, onOpenStoryHandled: () -> Unit = {}) {
    if (!signedIn) {
        SignInScreen()
        return
    }
    val vm: OnboardingViewModel = koinViewModel()
    val state by vm.state.collectAsStateWithLifecycle()
    when {
        state.loading -> Centered { CircularProgressIndicator() }
        state.done -> MainScreen(openStoryId, onOpenStoryHandled)
        else -> OnboardingScreen(state, vm::toggle, vm::save, vm::load, onSignOut)
    }
}

@Composable
private fun Centered(content: @Composable () -> Unit) {
    Column(
        Modifier.fillMaxSize().padding(24.dp),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) { content() }
}

@Composable
fun SignInScreen(vm: SignInViewModel = koinViewModel()) {
    val state by vm.state.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var email by rememberSaveable { mutableStateOf("") }
    var password by rememberSaveable { mutableStateOf("") }

    Column(
        Modifier.fillMaxSize().padding(24.dp),
        verticalArrangement = Arrangement.Center,
    ) {
        Text("Changeloom", style = MaterialTheme.typography.headlineLarge)
        Spacer(Modifier.height(24.dp))
        OutlinedTextField(email, { email = it }, Modifier.fillMaxWidth(), label = { Text("Email") }, singleLine = true)
        OutlinedTextField(
            password, { password = it }, Modifier.fillMaxWidth(),
            label = { Text("Password") }, singleLine = true, visualTransformation = PasswordVisualTransformation(),
        )
        state.error?.let { Text(it, color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(top = 8.dp)) }
        Spacer(Modifier.height(16.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Button({ vm.signIn(email, password) }, enabled = !state.busy) { Text("Sign in") }
            OutlinedButton({ vm.register(email, password) }, enabled = !state.busy) { Text("Create account") }
        }
        OutlinedButton(
            onClick = {
                scope.launch {
                    try {
                        vm.googleToken(requestGoogleIdToken(context, webClientId(context)))
                    } catch (e: GetCredentialCancellationException) {
                        // User dismissed the account picker.
                    } catch (e: CancellationException) {
                        throw e
                    } catch (e: Exception) {
                        vm.fail(e.message ?: "Google sign-in failed")
                    }
                }
            },
            enabled = !state.busy,
            modifier = Modifier.fillMaxWidth().padding(top = 8.dp),
        ) { Text("Continue with Google") }
    }
}

@Composable
fun OnboardingScreen(
    state: OnboardingState,
    onToggle: (String) -> Unit,
    onSave: () -> Unit,
    onRetry: () -> Unit,
    onSignOut: () -> Unit,
) {
    Column(Modifier.fillMaxSize().padding(16.dp)) {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text("Pick topics to follow", style = MaterialTheme.typography.titleLarge, modifier = Modifier.weight(1f))
            TextButton(onClick = onSignOut) { Text("Sign out") }
        }
        state.error?.let {
            Text(it, color = MaterialTheme.colorScheme.error)
            TextButton(onClick = onRetry) { Text("Retry") }
        }
        LazyColumn(Modifier.weight(1f)) {
            items(state.topics, key = { it.slug }) { topic ->
                Row(
                    Modifier.fillMaxWidth().clickable { onToggle(topic.slug) }
                        .padding(start = if (topic.parent == null) 0.dp else 24.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Checkbox(topic.slug in state.selected, { onToggle(topic.slug) })
                    Column {
                        Text(topic.name, style = MaterialTheme.typography.bodyLarge)
                        Text(topic.description, style = MaterialTheme.typography.bodySmall)
                    }
                }
            }
        }
        Button(onSave, Modifier.fillMaxWidth(), enabled = state.selected.isNotEmpty() && !state.saving) {
            Text("Continue")
        }
    }
}

/** Generated by the google-services plugin only when the Firebase config has a Google OAuth client. */
private fun webClientId(context: android.content.Context): String {
    val id = context.resources.getIdentifier("default_web_client_id", "string", context.packageName)
    check(id != 0) {
        "Google sign-in is not configured: enable the Google provider and add your SHA-1 in Firebase, then re-download google-services.json"
    }
    return context.getString(id)
}
