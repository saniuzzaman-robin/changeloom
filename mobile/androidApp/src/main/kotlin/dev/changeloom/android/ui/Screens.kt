package dev.changeloom.android.ui

import android.os.Bundle
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.unit.dp
import androidx.lifecycle.HasDefaultViewModelProviderFactory
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleRegistry
import androidx.lifecycle.SAVED_STATE_REGISTRY_OWNER_KEY
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.SavedStateViewModelFactory
import androidx.lifecycle.VIEW_MODEL_STORE_OWNER_KEY
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.ViewModelStore
import androidx.lifecycle.ViewModelStoreOwner
import androidx.lifecycle.enableSavedStateHandles
import androidx.lifecycle.viewmodel.CreationExtras
import androidx.lifecycle.viewmodel.MutableCreationExtras
import androidx.savedstate.SavedStateRegistry
import androidx.savedstate.SavedStateRegistryController
import androidx.savedstate.SavedStateRegistryOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.LocalViewModelStoreOwner
import androidx.lifecycle.viewmodel.compose.viewModel
import dev.changeloom.android.telemetry.TrackScreen
import dev.changeloom.android.ui.components.LoomMark
import dev.changeloom.android.ui.theme.expoTween
import org.koin.androidx.compose.koinViewModel

/**
 * [userId] is the signed-in account, or null when signed out. [onContentReady] is called once a real screen (not the
 * loading mark) is showing, so the launch splash can go.
 */
@Composable
fun AppRoot(
    userId: String?,
    onSignOut: () -> Unit,
    openStoryId: Long? = null,
    onOpenStoryHandled: () -> Unit = {},
    onContentReady: () -> Unit = {},
) {
    val sessions: SessionViewModels = viewModel()
    val session = remember(userId) { sessions.ownerFor(userId) }
    val activityOwner = checkNotNull(LocalViewModelStoreOwner.current) { "AppRoot needs a ViewModelStoreOwner" }
    var signInShown by rememberSaveable { mutableStateOf(false) }
    val vm: TopicPickerViewModel? = if (userId != null) koinViewModel(viewModelStoreOwner = session) else null
    val state = vm?.state?.collectAsStateWithLifecycle()?.value
    val screen = when {
        state == null -> RootScreen.SignIn
        state.followed.isNotEmpty() -> RootScreen.Main
        state.loading -> RootScreen.Loading
        else -> RootScreen.Onboarding
    }
    // Each target carries its own store owner, so a screen fading out keeps its ViewModels instead of picking up
    // the next session's (or, for sign-in, leaving the activity's store).
    val target = RootTarget(screen, if (userId == null) activityOwner else session, vm)
    AnimatedContent(
        target,
        transitionSpec = { fadeIn(expoTween()) togetherWith fadeOut(expoTween()) },
        contentKey = { it.screen },
        label = "root",
    ) { t ->
        CompositionLocalProvider(LocalViewModelStoreOwner provides t.owner) {
            when (t.screen) {
                RootScreen.SignIn -> {
                    SideEffect(onContentReady)
                    SideEffect { signInShown = true }
                    TrackScreen("sign_in")
                    SignInScreen()
                }
                RootScreen.Main -> {
                    SideEffect(onContentReady)
                    MainScreen(openStoryId, onOpenStoryHandled)
                }
                RootScreen.Loading -> LoadingMark(weave = signInShown)
                RootScreen.Onboarding -> {
                    SideEffect(onContentReady)
                    TrackScreen("onboarding")
                    TopicPickerScreen(TopicPickerMode.Onboarding, onExit = onSignOut, vm = checkNotNull(t.vm))
                }
            }
        }
    }
}

private enum class RootScreen { SignIn, Loading, Main, Onboarding }

private data class RootTarget(val screen: RootScreen, val owner: ViewModelStoreOwner, val vm: TopicPickerViewModel?)

/**
 * The launch splash's mark at the splash's size, so the handover doesn't jump, breathing slowly while the account
 * loads. It weaves in only when [weave] (after signing in); after a cold start the splash has already woven it.
 */
@Composable
private fun LoadingMark(weave: Boolean) {
    val alpha = rememberInfiniteTransition(label = "loadingBreathe").animateFloat(
        1f,
        BREATHE_MIN_ALPHA,
        infiniteRepeatable(tween(BREATHE_HALF_MS, easing = FastOutSlowInEasing), RepeatMode.Reverse),
        label = "loadingAlpha",
    )
    Box(Modifier.fillMaxSize(), Alignment.Center) {
        LoomMark(Modifier.size(SPLASH_MARK_SIZE).graphicsLayer { this.alpha = alpha.value }, animate = weave)
    }
}

/**
 * Holds the ViewModels of the signed-in account. It lives in the activity's store, so they survive configuration
 * changes, and it starts a fresh store whenever the account changes or signs out, so each sign-in gets new
 * ViewModels (which load on creation) and none of the previous account's state. Their SavedStateHandles are
 * saved with the activity and come back after process death, for the same account only.
 */
class SessionViewModels(private val saved: SavedStateHandle) : ViewModel() {
    private var session: SessionOwner? = null

    fun ownerFor(userId: String?): ViewModelStoreOwner {
        session?.takeIf { it.userId == userId }?.let { return it }
        session?.viewModelStore?.clear()
        val restored = saved.get<Bundle>(KEY_SESSION_STATE)?.takeIf { saved.get<String>(KEY_SESSION_USER) == userId }
        return SessionOwner(userId, restored).also { owner ->
            session = owner
            saved[KEY_SESSION_USER] = userId
            saved.setSavedStateProvider(KEY_SESSION_STATE) { owner.save() }
        }
    }

    override fun onCleared() {
        session?.viewModelStore?.clear()
    }
}

private const val KEY_SESSION_USER = "session_user"
private const val KEY_SESSION_STATE = "session_state"

// The splash icon is 288dp for the 108-unit launcher viewport; LoomMark spans CROP_SIZE = 84 units: 288 × 84 / 108.
private val SPLASH_MARK_SIZE = 224.dp
private const val BREATHE_MIN_ALPHA = 0.55f
private const val BREATHE_HALF_MS = 800

/**
 * One account's ViewModel store with its own saved state, restored from [restored]. Koin reads the creation extras,
 * so `SavedStateHandle` constructor params resolve against this owner rather than the activity.
 */
private class SessionOwner(val userId: String?, restored: Bundle?) :
    ViewModelStoreOwner, SavedStateRegistryOwner, HasDefaultViewModelProviderFactory {
    private val lifecycleRegistry = LifecycleRegistry(this)
    private val savedState = SavedStateRegistryController.create(this)

    override val viewModelStore = ViewModelStore()
    override val lifecycle: Lifecycle get() = lifecycleRegistry
    override val savedStateRegistry: SavedStateRegistry get() = savedState.savedStateRegistry
    override val defaultViewModelProviderFactory: ViewModelProvider.Factory = SavedStateViewModelFactory()
    override val defaultViewModelCreationExtras: CreationExtras
        get() = MutableCreationExtras().apply {
            this[SAVED_STATE_REGISTRY_OWNER_KEY] = this@SessionOwner
            this[VIEW_MODEL_STORE_OWNER_KEY] = this@SessionOwner
        }

    init {
        savedState.performAttach()
        savedState.performRestore(restored)
        enableSavedStateHandles()
        // CREATED hands the restored state to the SavedStateHandles; the store never needs to be STARTED.
        lifecycleRegistry.currentState = Lifecycle.State.CREATED
    }

    fun save(): Bundle = Bundle().also(savedState::performSave)
}
