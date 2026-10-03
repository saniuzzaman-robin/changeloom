package dev.changeloom.android.ui

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelStore
import androidx.lifecycle.ViewModelStoreOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.LocalViewModelStoreOwner
import androidx.lifecycle.viewmodel.compose.viewModel
import dev.changeloom.android.ui.components.LoomMark
import org.koin.androidx.compose.koinViewModel

/** [userId] is the signed-in account, or null when signed out. */
@Composable
fun AppRoot(userId: String?, onSignOut: () -> Unit, openStoryId: Long? = null, onOpenStoryHandled: () -> Unit = {}) {
    val sessions: SessionViewModels = viewModel()
    val session = remember(userId) { sessions.ownerFor(userId) }
    if (userId == null) {
        SignInScreen()
        return
    }
    CompositionLocalProvider(LocalViewModelStoreOwner provides session) {
        val vm: TopicPickerViewModel = koinViewModel()
        val state by vm.state.collectAsStateWithLifecycle()
        when {
            state.followed.isNotEmpty() -> MainScreen(openStoryId, onOpenStoryHandled)
            state.loading -> Box(Modifier.fillMaxSize(), Alignment.Center) { LoomMark(Modifier.size(64.dp)) }
            else -> TopicPickerScreen(TopicPickerMode.Onboarding, onExit = onSignOut, vm = vm)
        }
    }
}

/**
 * Holds the ViewModels of the signed-in account. It lives in the activity's store, so they survive configuration
 * changes, and it starts a fresh store whenever the account changes or signs out, so each sign-in gets new
 * ViewModels (which load on creation) and none of the previous account's state.
 */
class SessionViewModels : ViewModel() {
    private var userId: String? = null
    private var store = ViewModelStore()

    fun ownerFor(userId: String?): ViewModelStoreOwner {
        if (userId != this.userId) {
            store.clear()
            store = ViewModelStore()
            this.userId = userId
        }
        val current = store
        return object : ViewModelStoreOwner {
            override val viewModelStore = current
        }
    }

    override fun onCleared() = store.clear()
}
