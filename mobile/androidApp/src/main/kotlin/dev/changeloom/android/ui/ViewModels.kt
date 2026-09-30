package dev.changeloom.android.ui

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dev.changeloom.android.push.DeviceRegistrar
import dev.changeloom.shared.auth.AuthRepository
import dev.changeloom.shared.data.ChangeloomApi
import dev.changeloom.shared.data.PagerState
import dev.changeloom.shared.data.Story
import dev.changeloom.shared.data.StoryPager
import dev.changeloom.shared.data.TimelineRepository
import dev.changeloom.shared.data.TimelineState
import dev.changeloom.shared.data.Topic
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

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

data class OnboardingState(
    val loading: Boolean = true,
    val saving: Boolean = false,
    val topics: List<Topic> = emptyList(),
    val selected: Set<String> = emptySet(),
    val done: Boolean = false,
    val error: String? = null,
)

class OnboardingViewModel(private val api: ChangeloomApi) : ViewModel() {
    private val _state = MutableStateFlow(OnboardingState())
    val state: StateFlow<OnboardingState> = _state.asStateFlow()

    init {
        load()
    }

    fun load() {
        _state.update { it.copy(loading = true, error = null) }
        viewModelScope.launch {
            try {
                val topics = api.topics()
                val me = api.me()
                _state.update {
                    it.copy(loading = false, topics = topics, selected = me.topics.toSet(), done = me.topics.isNotEmpty())
                }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                _state.update { it.copy(loading = false, error = e.message ?: "Could not load topics") }
            }
        }
    }

    fun toggle(slug: String) = _state.update {
        it.copy(selected = if (slug in it.selected) it.selected - slug else it.selected + slug)
    }

    fun save() {
        _state.update { it.copy(saving = true, error = null) }
        viewModelScope.launch {
            try {
                api.putMyTopics(_state.value.selected.sorted())
                _state.update { it.copy(saving = false, done = true) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                _state.update { it.copy(saving = false, error = e.message ?: "Could not save topics") }
            }
        }
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

data class SettingsState(
    val loading: Boolean = true,
    val saving: Boolean = false,
    val topics: List<Topic> = emptyList(),
    val selected: Set<String> = emptySet(),
    val saved: Boolean = false,
    val error: String? = null,
)

class SettingsViewModel(
    private val api: ChangeloomApi,
    private val repo: TimelineRepository,
    private val auth: AuthRepository,
    private val registrar: DeviceRegistrar,
) : ViewModel() {
    val email: String? = auth.currentUser.value?.email
    private val _state = MutableStateFlow(SettingsState())
    val state: StateFlow<SettingsState> = _state.asStateFlow()

    init {
        load()
    }

    fun load() {
        _state.update { it.copy(loading = true, error = null) }
        viewModelScope.launch {
            try {
                val topics = api.topics()
                val me = api.me()
                _state.update { it.copy(loading = false, topics = topics, selected = me.topics.toSet()) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                _state.update { it.copy(loading = false, error = e.message ?: "Could not load topics") }
            }
        }
    }

    fun toggle(slug: String) = _state.update {
        it.copy(saved = false, selected = if (slug in it.selected) it.selected - slug else it.selected + slug)
    }

    fun save() {
        _state.update { it.copy(saving = true, saved = false, error = null) }
        viewModelScope.launch {
            try {
                api.putMyTopics(_state.value.selected.sorted())
                repo.refresh()
                _state.update { it.copy(saving = false, saved = true) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                _state.update { it.copy(saving = false, error = e.message ?: "Could not save topics") }
            }
        }
    }

    /** Stops pushes for this device and clears the local cache first, so a different account never sees this user's data. */
    fun signOut() {
        viewModelScope.launch {
            registrar.unregister()
            repo.clear()
            auth.signOut()
        }
    }
}
