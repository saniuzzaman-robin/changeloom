package dev.changeloom.android.ui

import android.graphics.Bitmap
import android.util.Log
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dev.changeloom.android.data.fetchAvatar
import dev.changeloom.android.push.DeviceRegistrar
import dev.changeloom.android.ui.theme.ThemeMode
import dev.changeloom.android.ui.theme.ThemePreferences
import dev.changeloom.shared.auth.AuthRepository
import dev.changeloom.shared.data.ApiException
import dev.changeloom.shared.data.ChangeloomApi
import dev.changeloom.shared.data.CheckState
import dev.changeloom.shared.data.MeStats
import dev.changeloom.shared.data.PagerState
import dev.changeloom.shared.data.Story
import dev.changeloom.shared.data.StoryPager
import dev.changeloom.shared.data.TimelineRepository
import dev.changeloom.shared.data.TimelineState
import dev.changeloom.shared.data.TopicRequest
import dev.changeloom.shared.data.TopicSelection
import dev.changeloom.shared.data.TopicTree
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import java.io.IOException

private const val TAG = "Profile"
internal const val REQUEST_MIN_LENGTH = 2
internal const val REQUEST_MAX_LENGTH = 100

data class SignInState(val busy: Boolean = false, val error: String? = null)

class SignInViewModel(private val auth: AuthRepository) : ViewModel() {
    private val _state = MutableStateFlow(SignInState())
    val state: StateFlow<SignInState> = _state.asStateFlow()

    fun signIn(email: String, password: String) = run { auth.signInWithEmail(email.trim(), password) }
    fun register(email: String, password: String) = run { auth.registerWithEmail(email.trim(), password) }
    fun googleToken(token: String) = run { auth.signInWithGoogleIdToken(token) }
    fun fail(message: String) = _state.update { it.copy(error = message) }

    private fun run(block: suspend () -> Unit) {
        _state.value = SignInState(busy = true)
        viewModelScope.launch {
            _state.value = try {
                block()
                SignInState()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                SignInState(error = e.message ?: "Sign-in failed")
            }
        }
    }
}

/** The topic tree only changes on backend deploys, so it is fetched once per process. */
class TopicCatalog(private val api: ChangeloomApi) {
    private val mutex = Mutex()
    private var cached: TopicTree? = null

    suspend fun tree(): TopicTree = mutex.withLock { cached ?: TopicTree(api.topics()).also { cached = it } }
}

data class TopicPickerState(
    val loading: Boolean = true,
    val saving: Boolean = false,
    val selection: TopicSelection? = null,
    /** What the server has saved for this user, minimised; empty means onboarding is still needed. */
    val followed: List<String> = emptyList(),
    val query: String = "",
    val expanded: Set<String> = emptySet(),
    /** Set after an edit (not onboarding) is saved; the screen closes and calls [TopicPickerViewModel.savedHandled]. */
    val saved: Boolean = false,
    val error: String? = null,
) {
    val dirty: Boolean get() = selection != null && selection.toFollowed() != followed
}

/**
 * Followed topics for both onboarding (it also gates the main UI on [TopicPickerState.followed]) and
 * editing. Reloads whenever the signed-in user changes, so a second account never sees the first's picks.
 */
class TopicPickerViewModel(
    private val api: ChangeloomApi,
    private val catalog: TopicCatalog,
    private val repo: TimelineRepository,
    auth: AuthRepository,
) : ViewModel() {
    private val _state = MutableStateFlow(TopicPickerState())
    val state: StateFlow<TopicPickerState> = _state.asStateFlow()
    private var loadJob: Job? = null

    init {
        viewModelScope.launch {
            auth.currentUser.map { it?.uid }.distinctUntilChanged().collect { uid ->
                loadJob?.cancel()
                // Stays "loading" while signed out so the next sign-in never flashes the previous user's state.
                _state.value = TopicPickerState()
                if (uid != null) load()
            }
        }
    }

    fun load() {
        loadJob?.cancel()
        _state.update { it.copy(loading = true, error = null) }
        loadJob = viewModelScope.launch {
            try {
                val tree = catalog.tree()
                val selection = TopicSelection.fromFollowed(tree, api.me().topics)
                val partial = tree.roots.map { it.slug }.filter { selection.stateOf(it) == CheckState.Partial }
                _state.update {
                    it.copy(loading = false, selection = selection, followed = selection.toFollowed(), expanded = partial.toSet())
                }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                _state.update { it.copy(loading = false, error = e.message ?: "Could not load topics") }
            }
        }
    }

    fun toggle(slug: String) = edit { it.toggle(slug) }
    fun selectAll() = edit { it.selectAll() }
    fun clear() = edit { it.clear() }

    fun toggleExpanded(slug: String) = _state.update {
        it.copy(expanded = if (slug in it.expanded) it.expanded - slug else it.expanded + slug)
    }

    fun setQuery(text: String) = _state.update { it.copy(query = text) }

    /** Drops unsaved edits, e.g. when leaving the edit screen without saving. */
    fun discard() = _state.update { s ->
        s.copy(selection = s.selection?.let { TopicSelection.fromFollowed(it.tree, s.followed) }, query = "", error = null)
    }

    fun savedHandled() = _state.update { it.copy(saved = false) }

    fun save() {
        val selection = _state.value.selection ?: return
        val slugs = selection.toFollowed()
        val onboarding = _state.value.followed.isEmpty()
        _state.update { it.copy(saving = true, error = null) }
        viewModelScope.launch {
            try {
                api.putMyTopics(slugs)
                // A new timeline view model refreshes on its own after onboarding; an edit must refresh the existing one.
                if (!onboarding) viewModelScope.launch { repo.refresh() }
                _state.update { it.copy(saving = false, followed = slugs, query = "", saved = !onboarding) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                _state.update { it.copy(saving = false, error = e.message ?: "Could not save topics") }
            }
        }
    }

    private fun edit(change: (TopicSelection) -> TopicSelection) = _state.update { s ->
        s.selection?.let { s.copy(selection = change(it), saved = false) } ?: s
    }
}

class TimelineViewModel(private val repo: TimelineRepository) : ViewModel() {
    val state: StateFlow<TimelineState> = repo.state

    init {
        refresh()
    }

    fun refresh() {
        viewModelScope.launch { repo.refresh() }
    }

    fun loadMore() {
        viewModelScope.launch { repo.loadMore() }
    }

    fun setRead(id: Long, read: Boolean) {
        viewModelScope.launch { repo.setRead(id, read) }
    }

    fun setBookmarked(id: Long, on: Boolean) {
        viewModelScope.launch { repo.setBookmarked(id, on) }
    }

    fun dismissError() = repo.clearError()
}

data class StoryDetailState(val loading: Boolean = true, val story: Story? = null, val error: String? = null)

class StoryDetailViewModel(private val id: Long, private val repo: TimelineRepository) : ViewModel() {
    private val _state = MutableStateFlow(StoryDetailState())
    val state: StateFlow<StoryDetailState> = _state.asStateFlow()

    init {
        load()
    }

    fun load() {
        _state.value = StoryDetailState()
        viewModelScope.launch {
            try {
                val story = repo.story(id)
                _state.value = StoryDetailState(loading = false, story = story)
                repo.setRead(id, true) // opening a story marks it read
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                _state.value = StoryDetailState(loading = false, error = e.message ?: "Could not load story")
            }
        }
    }

    fun setRead(read: Boolean) {
        viewModelScope.launch { repo.setRead(id, read) }
    }

    fun setBookmarked(on: Boolean) {
        if (_state.value.story == null) return
        _state.update { s -> s.copy(story = s.story?.copy(isBookmarked = on)) }
        viewModelScope.launch {
            if (!repo.setBookmarked(id, on)) _state.update { s -> s.copy(story = s.story?.copy(isBookmarked = !on)) }
        }
    }
}

/** Bookmarked stories. Reloaded each time the screen opens so changes made elsewhere show up. */
class BookmarksViewModel(api: ChangeloomApi, private val repo: TimelineRepository) : ViewModel() {
    private val pager = StoryPager { cursor -> api.bookmarks(cursor) }
    val state: StateFlow<PagerState> = pager.state

    fun refresh() {
        viewModelScope.launch { pager.refresh() }
    }

    fun loadMore() {
        viewModelScope.launch { pager.loadMore() }
    }

    fun setBookmarked(id: Long, on: Boolean) {
        pager.setBookmarked(id, on)
        viewModelScope.launch { if (!repo.setBookmarked(id, on)) pager.setBookmarked(id, !on) }
    }

    /** Unsaves and drops the story from the list; a failed sync reloads the list to bring it back. */
    fun remove(id: Long) {
        pager.remove(id)
        viewModelScope.launch { if (!repo.setBookmarked(id, false)) pager.refresh() }
    }

    fun dismissError() = pager.clearError()
}

class SearchViewModel(private val api: ChangeloomApi, private val repo: TimelineRepository) : ViewModel() {
    private val _query = MutableStateFlow("")
    val query: StateFlow<String> = _query.asStateFlow()

    private val pager = MutableStateFlow<StoryPager?>(null)

    @OptIn(ExperimentalCoroutinesApi::class)
    val state: StateFlow<PagerState> = pager
        .flatMapLatest { it?.state ?: flowOf(PagerState()) }
        .stateIn(viewModelScope, SharingStarted.Eagerly, PagerState())

    fun setQuery(text: String) {
        _query.value = text
    }

    fun search() {
        val text = _query.value.trim()
        if (text.isEmpty()) {
            pager.value = null
            return
        }
        val next = StoryPager { cursor -> api.search(text, cursor) }
        pager.value = next
        viewModelScope.launch { next.refresh() }
    }

    fun loadMore() {
        val current = pager.value ?: return
        viewModelScope.launch { current.loadMore() }
    }

    fun setBookmarked(id: Long, on: Boolean) {
        val current = pager.value ?: return
        current.setBookmarked(id, on)
        viewModelScope.launch { if (!repo.setBookmarked(id, on)) current.setBookmarked(id, !on) }
    }

    fun dismissError() {
        pager.value?.clearError()
    }
}

data class ProfileState(
    /** Null until the first `/v1/me` load succeeds. */
    val stats: MeStats? = null,
    val error: String? = null,
    /** The user's topic requests, newest first; null until the first load succeeds. */
    val requests: List<TopicRequest>? = null,
    val requestText: String = "",
    val submittingRequest: Boolean = false,
    val requestError: String? = null,
)

class ProfileViewModel(
    private val api: ChangeloomApi,
    private val repo: TimelineRepository,
    private val auth: AuthRepository,
    private val registrar: DeviceRegistrar,
    private val theme: ThemePreferences,
) : ViewModel() {
    val email: String? = auth.currentUser.value?.email
    val displayName: String? = auth.currentUser.value?.displayName
    val themeMode: StateFlow<ThemeMode> = theme.mode

    private val _state = MutableStateFlow(ProfileState())
    val state: StateFlow<ProfileState> = _state.asStateFlow()

    /** The profile photo, or null while loading or when there isn't one (the avatar shows initials). */
    private val _photo = MutableStateFlow<Bitmap?>(null)
    val photo: StateFlow<Bitmap?> = _photo.asStateFlow()

    init {
        auth.currentUser.value?.photoUrl?.let { url ->
            viewModelScope.launch {
                try {
                    _photo.value = fetchAvatar(url)
                } catch (e: IOException) {
                    Log.w(TAG, "Couldn't load the profile photo; showing initials", e)
                }
            }
        }
    }

    /** Counts change whenever the user saves or reads a story, so the screen refreshes them each time it is shown. */
    fun refresh() {
        viewModelScope.launch {
            try {
                val stats = api.me().stats
                _state.update { it.copy(stats = stats, error = null) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                _state.update { it.copy(error = e.message ?: "Couldn't load your stats") }
            }
            try {
                val requests = api.topicRequests()
                _state.update { it.copy(requests = requests) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                Log.w(TAG, "Couldn't load topic requests", e)
            }
        }
    }

    fun setRequestText(text: String) = _state.update { it.copy(requestText = text, requestError = null) }

    fun submitRequest() {
        val text = _state.value.requestText.trim()
        if (text.length < REQUEST_MIN_LENGTH || _state.value.submittingRequest) return
        _state.update { it.copy(submittingRequest = true, requestError = null) }
        viewModelScope.launch {
            try {
                val created = api.requestTopic(text)
                _state.update { it.copy(submittingRequest = false, requestText = "", requests = listOf(created) + it.requests.orEmpty()) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                val message = when ((e as? ApiException)?.status) {
                    400 -> "Use $REQUEST_MIN_LENGTH to $REQUEST_MAX_LENGTH characters."
                    409 -> "You already asked for that."
                    429 -> "You have too many pending requests. Wait for some to be reviewed."
                    else -> e.message ?: "Couldn't send your request"
                }
                _state.update { it.copy(submittingRequest = false, requestError = message) }
            }
        }
    }

    fun setThemeMode(mode: ThemeMode) = theme.setMode(mode)

    /** Stops pushes for this device and clears the local cache first, so a different account never sees this user's data. */
    fun signOut() {
        viewModelScope.launch {
            registrar.unregister()
            repo.clear()
            auth.signOut()
        }
    }
}
