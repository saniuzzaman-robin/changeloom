package dev.changeloom.android.ui

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.changeloom.android.ui.components.LoomMark
import org.koin.androidx.compose.koinViewModel

@Composable
fun AppRoot(signedIn: Boolean, onSignOut: () -> Unit, openStoryId: Long? = null, onOpenStoryHandled: () -> Unit = {}) {
    if (!signedIn) {
        SignInScreen()
        return
    }
    val vm: TopicPickerViewModel = koinViewModel()
    val state by vm.state.collectAsStateWithLifecycle()
    when {
        state.followed.isNotEmpty() -> MainScreen(openStoryId, onOpenStoryHandled)
        state.loading -> Box(Modifier.fillMaxSize(), Alignment.Center) { LoomMark(Modifier.size(64.dp)) }
        else -> TopicPickerScreen(TopicPickerMode.Onboarding, onExit = onSignOut, vm = vm)
    }
}
