package dev.changeloom.android.ui

import android.graphics.Bitmap
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dev.changeloom.android.R
import dev.changeloom.android.auth.SessionManager
import dev.changeloom.android.play.ReviewPrompter
import dev.changeloom.android.data.fetchAvatar
import dev.changeloom.android.telemetry.Analytics
import dev.changeloom.android.telemetry.AppLog
import dev.changeloom.android.ui.theme.ThemeMode
import dev.changeloom.android.ui.theme.ThemePreferences
import dev.changeloom.shared.auth.AuthRepository
import dev.changeloom.shared.auth.ReauthRequiredException
import dev.changeloom.shared.auth.SignInMethod
import dev.changeloom.shared.data.ApiException
import dev.changeloom.shared.data.ChangeloomApi
import dev.changeloom.shared.data.CheckState
import dev.changeloom.shared.data.MeStats
import dev.changeloom.shared.data.PagerState
import dev.changeloom.shared.data.Profession
import dev.changeloom.shared.data.Story
import dev.changeloom.shared.data.StoryCache
import dev.changeloom.shared.data.StoryPager
import dev.changeloom.shared.data.TimelineRepository
import dev.changeloom.shared.data.TimelineState
import dev.changeloom.shared.data.TopicList
import dev.changeloom.shared.data.TopicRequest
import dev.changeloom.shared.data.TopicSelection
import dev.changeloom.shared.data.TopicTree
import dev.changeloom.shared.data.ViewTracker
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.Job
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import java.io.IOException
import java.util.Locale

private const val VIEW_FLUSH_INTERVAL_MS = 30_000L
private const val TAG_SIGN_IN = "SignIn"
private const val TAG_TOPICS = "Topics"
private const val TAG_STORY = "Story"
private const val TAG_PROFILE = "Profile"
internal const val REQUEST_MIN_LENGTH = 2
internal const val REQUEST_MAX_LENGTH = 100

private const val KEY_AUTH_MODE = "auth_mode"
private const val KEY_EMAIL = "email"
private const val KEY_QUERY = "query"
private const val KEY_SEARCHED = "searched"

data class SignInState(val busy: Boolean = false, val error: String? = null)

enum class AuthMode {
    SignIn,
    Register,
    ;

    val other: AuthMode get() = if (this == SignIn) Register else SignIn
}

/** The form's mode and email survive process death; the password is kept in memory only, never in saved state. */
class SignInViewModel(
    private val auth: AuthRepository,
    private val analytics: Analytics,
    private val strings: Strings,
    private val saved: SavedStateHandle,
) : ViewModel() {
    private val _state = MutableStateFlow(SignInState())
    val state: StateFlow<SignInState> = _state.asStateFlow()

    val mode: StateFlow<AuthMode> = saved.getStateFlow(KEY_AUTH_MODE, AuthMode.SignIn)
    val email: StateFlow<String> = saved.getStateFlow(KEY_EMAIL, "")
    private val _password = MutableStateFlow("")
    val password: StateFlow<String> = _password.asStateFlow()

    fun setMode(mode: AuthMode) {
        saved[KEY_AUTH_MODE] = mode
    }

    fun setEmail(text: String) {
        saved[KEY_EMAIL] = text
    }

    fun setPassword(text: String) {
        _password.value = text
    }

    fun submit() {
        val email = email.value.trim()
        val password = _password.value
        if (mode.value == AuthMode.SignIn) {
            run {
                auth.signInWithEmail(email, password)
                analytics.login(Analytics.METHOD_PASSWORD)
            }
        } else {
            run {
                auth.registerWithEmail(email, password)
                analytics.signUp(Analytics.METHOD_PASSWORD)
            }
        }
    }

    fun googleToken(token: String) = run {
        if (auth.signInWithGoogleIdToken(token)) analytics.signUp(Analytics.METHOD_GOOGLE) else analytics.login(Analytics.METHOD_GOOGLE)
    }

    /** The Google account picker failed before there was a token to sign in with. */
    fun googleFailed(error: Exception) {
        AppLog.w(TAG_SIGN_IN, "Google sign-in failed", error)
        _state.update { it.copy(error = strings.errorText(error, R.string.google_sign_in_failed)) }
    }

    private fun run(block: suspend () -> Unit) {
        _state.value = SignInState(busy = true)
        viewModelScope.launch {
            _state.value = try {
                block()
                SignInState()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                AppLog.w(TAG_SIGN_IN, "Sign-in failed", e)
                SignInState(error = strings.errorText(e, R.string.sign_in_failed))
            }
        }
    }
}

/** The topic tree only changes on backend deploys, so it is fetched once per process. */
class TopicCatalog(private val api: ChangeloomApi) {
    private val mutex = Mutex()
    private var cached: TopicList? = null

    private suspend fun list(): TopicList = mutex.withLock { cached ?: api.topicList().also { cached = it } }

    suspend fun tree(): TopicTree = TopicTree(list().items)

    suspend fun professions(): List<Profession> = list().professions
}

/** The picker's two steps: pick professions, then topics (the professions' areas come first). */
enum class PickerStep { Professions, Topics }

data class TopicPickerState(
    val loading: Boolean = true,
    val saving: Boolean = false,
    val selection: TopicSelection? = null,
    /** What the server has saved for this user, minimised; empty means onboarding is still needed. */
    val followed: List<String> = emptyList(),
    val query: String = "",
    val expanded: Set<String> = emptySet(),
    val step: PickerStep = PickerStep.Topics,
    val professions: List<Profession> = emptyList(),
    /** Chosen profession slugs, in the order they were picked. */
    val selectedProfessions: List<String> = emptyList(),
    /** The professions the server has saved. */
    val savedProfessions: List<String> = emptyList(),
    /** Set after an edit (not onboarding) is saved; the screen closes and calls [TopicPickerViewModel.savedHandled]. */
    val saved: Boolean = false,
    val error: String? = null,
) {
    val dirty: Boolean
        get() = selection != null && (selection.toFollowed() != followed || selectedProfessions != savedProfessions)
}

/**
 * Followed topics for both onboarding (it also gates the main UI on [TopicPickerState.followed]) and
 * editing. Scoped to the signed-in account (see [SessionViewModels]), so it loads once per sign-in.
 * The cached topics open the main UI at once, so a cold or offline start never waits on (or falls back to)
 * onboarding; the server copy replaces them when it arrives.
 */
class TopicPickerViewModel(
    private val api: ChangeloomApi,
    private val catalog: TopicCatalog,
    private val repo: TimelineRepository,
    private val cache: StoryCache,
    private val analytics: Analytics,
    private val strings: Strings,
) : ViewModel() {
    private val _state = MutableStateFlow(TopicPickerState())
    val state: StateFlow<TopicPickerState> = _state.asStateFlow()
    private var loadJob: Job? = null

    init {
        load()
    }

    fun load() {
        loadJob?.cancel()
        _state.update { it.copy(loading = true, error = null) }
        loadJob = viewModelScope.launch {
            if (_state.value.followed.isEmpty()) {
                val cached = cache.loadFollowed()
                _state.update { if (it.followed.isEmpty()) it.copy(followed = cached) else it }
            }
            try {
                val (loaded, me) = coroutineScope {
                    val loaded = async { catalog.tree() to catalog.professions() }
                    val me = async { api.me() }
                    loaded.await() to me.await()
                }
                val (tree, professions) = loaded
                val known = professions.mapTo(mutableSetOf()) { it.slug }
                val mine = me.professions.filter { it in known }
                val selection = TopicSelection.fromFollowed(tree, me.topics)
                val partial = tree.roots.map { it.slug }.filter { selection.stateOf(it) == CheckState.Partial }
                val followed = selection.toFollowed()
                cache.saveFollowed(followed)
                _state.update {
                    it.copy(
                        loading = false,
                        selection = selection,
                        followed = followed,
                        expanded = partial.toSet(),
                        professions = professions,
                        selectedProfessions = mine,
                        savedProfessions = mine,
                        // Onboarding starts with the professions; an edit goes straight to the topics.
                        step = if (followed.isEmpty() && professions.isNotEmpty()) PickerStep.Professions else PickerStep.Topics,
                    )
                }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                AppLog.failure(TAG_TOPICS, "Couldn't load topics", e)
                _state.update { it.copy(loading = false, error = strings.errorText(e, R.string.topics_load_failed)) }
            }
        }
    }

    fun toggleProfession(slug: String) = _state.update { s ->
        val chosen = s.selectedProfessions
        s.copy(
            selectedProfessions = when {
                slug in chosen -> chosen - slug
                else -> chosen + slug
            },
            saved = false,
        )
    }

    fun setStep(step: PickerStep) = _state.update { it.copy(step = step, query = "") }

    fun toggle(slug: String) = edit { it.toggle(slug) }
    fun selectAll() = edit { it.selectAll() }
    fun clear() = edit { it.clear() }

    fun toggleExpanded(slug: String) = _state.update {
        it.copy(expanded = if (slug in it.expanded) it.expanded - slug else it.expanded + slug)
    }

    fun setQuery(text: String) = _state.update { it.copy(query = text) }

    /** Drops unsaved edits, e.g. when leaving the edit screen without saving. */
    fun discard() = _state.update { s ->
        s.copy(
            selection = s.selection?.let { TopicSelection.fromFollowed(it.tree, s.followed) },
            selectedProfessions = s.savedProfessions,
            step = if (s.followed.isEmpty() && s.professions.isNotEmpty()) PickerStep.Professions else PickerStep.Topics,
            query = "",
            error = null,
        )
    }

    fun savedHandled() = _state.update { it.copy(saved = false) }

    fun save() {
        val selection = _state.value.selection ?: return
        val slugs = selection.toFollowed()
        val onboarding = _state.value.followed.isEmpty()
        val professions = _state.value.selectedProfessions
        _state.update { it.copy(saving = true, error = null) }
        viewModelScope.launch {
            try {
                // Professions first: if the topics call then fails, a retry only repeats the idempotent writes.
                if (professions != _state.value.savedProfessions) {
                    api.putMyProfessions(professions)
                    _state.update { it.copy(savedProfessions = professions) }
                }
                api.putMyTopics(slugs)
                cache.saveFollowed(slugs)
                analytics.topicsUpdate(slugs.size, onboarding)
                // A new timeline view model refreshes on its own after onboarding; an edit must refresh the existing one.
                if (!onboarding) viewModelScope.launch { repo.refresh() }
                _state.update { it.copy(saving = false, followed = slugs, query = "", saved = !onboarding) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                AppLog.failure(TAG_TOPICS, "Couldn't save topics", e)
                _state.update { it.copy(saving = false, error = strings.errorText(e, R.string.topics_save_failed)) }
            }
        }
    }

    private fun edit(change: (TopicSelection) -> TopicSelection) = _state.update { s ->
        s.selection?.let { s.copy(selection = change(it), saved = false) } ?: s
    }
}

class TimelineViewModel(
    private val repo: TimelineRepository,
    private val analytics: Analytics,
    private val views: ViewTracker,
) : ViewModel() {
    val state: StateFlow<TimelineState> = repo.state

    init {
        refresh()
        viewModelScope.launch { views.run(VIEW_FLUSH_INTERVAL_MS) }
    }

    /** A story was on screen long enough to count as seen. */
    fun storySeen(id: Long) {
        viewModelScope.launch { views.seen(id) }
    }

    fun flushViews() {
        viewModelScope.launch { views.flush() }
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
        if (on) analytics.bookmarkAdd(id)
        viewModelScope.launch { repo.setBookmarked(id, on) }
    }

    fun dismissError() = repo.clearError()
}

data class StoryDetailState(val loading: Boolean = true, val story: Story? = null, val error: String? = null)

class StoryDetailViewModel(
    private val id: Long,
    private val repo: TimelineRepository,
    private val analytics: Analytics,
    private val strings: Strings,
    private val review: ReviewPrompter,
) : ViewModel() {
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
                analytics.storyOpen(id)
                review.storyRead()
                repo.setRead(id, true) // opening a story marks it read
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                AppLog.failure(TAG_STORY, "Couldn't load a story", e)
                _state.value = StoryDetailState(loading = false, error = strings.errorText(e, R.string.story_load_failed))
            }
        }
    }

    fun setRead(read: Boolean) {
        viewModelScope.launch { repo.setRead(id, read) }
    }

    fun setBookmarked(on: Boolean) {
        if (_state.value.story == null) return
        if (on) analytics.bookmarkAdd(id)
        _state.update { s -> s.copy(story = s.story?.copy(isBookmarked = on)) }
        viewModelScope.launch { repo.setBookmarked(id, on) }
    }

    fun shared() = analytics.share(id)
}

/** Bookmarked stories. Reloaded each time the screen opens so changes made elsewhere show up. */
class BookmarksViewModel(
    api: ChangeloomApi,
    private val repo: TimelineRepository,
    private val analytics: Analytics,
) : ViewModel() {
    private val pager = StoryPager { cursor -> api.bookmarks(cursor) }
    val state: StateFlow<PagerState> = pager.state

    /** Syncs queued changes first (only if there are any), since the list is read from the server. */
    fun refresh() {
        viewModelScope.launch {
            repo.flushBookmarks()
            pager.refresh()
        }
    }

    fun loadMore() {
        viewModelScope.launch { pager.loadMore() }
    }

    fun setBookmarked(id: Long, on: Boolean) {
        if (on) analytics.bookmarkAdd(id)
        pager.setBookmarked(id, on)
        viewModelScope.launch { repo.setBookmarked(id, on) }
    }

    /** Unsaves and drops the story from the list. */
    fun remove(id: Long) {
        pager.remove(id)
        viewModelScope.launch { repo.setBookmarked(id, false) }
    }

    fun dismissError() = pager.clearError()
}

/** The query, and the last search run, survive process death; the search runs again on restore. */
class SearchViewModel(
    private val api: ChangeloomApi,
    private val repo: TimelineRepository,
    private val analytics: Analytics,
    private val saved: SavedStateHandle,
) : ViewModel() {
    val query: StateFlow<String> = saved.getStateFlow(KEY_QUERY, "")

    private val pager = MutableStateFlow<StoryPager?>(null)

    @OptIn(ExperimentalCoroutinesApi::class)
    val state: StateFlow<PagerState> = pager
        .flatMapLatest { it?.state ?: flowOf(PagerState()) }
        .stateIn(viewModelScope, SharingStarted.Eagerly, PagerState())

    init {
        saved.get<String>(KEY_SEARCHED)?.let(::startSearch)
    }

    fun setQuery(text: String) {
        saved[KEY_QUERY] = text
        // Clearing the field drops the results, which brings the suggestions back.
        if (text.isEmpty()) {
            saved[KEY_SEARCHED] = null
            pager.value = null
        }
    }

    fun search() {
        val text = query.value.trim()
        saved[KEY_SEARCHED] = text.ifEmpty { null }
        if (text.isEmpty()) {
            pager.value = null
            return
        }
        analytics.search()
        startSearch(text)
    }

    private fun startSearch(text: String) {
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
        if (on) analytics.bookmarkAdd(id)
        current.setBookmarked(id, on)
        viewModelScope.launch { repo.setBookmarked(id, on) }
    }

    fun dismissError() {
        pager.value?.clearError()
    }
}

enum class DeleteStep { Confirm, Password, Google, Deleting }

/** Account deletion in progress; [step] is what the dialog asks for, [error] why the last attempt failed. */
data class DeleteState(val step: DeleteStep, val error: String? = null)

data class ProfileState(
    /** Null until the first `/v1/me` load succeeds. */
    val stats: MeStats? = null,
    /** ISO 3166-1 alpha-2 code the server has saved; null until set. */
    val country: String? = null,
    val error: String? = null,
    /** The user's topic requests, newest first; null until the first load succeeds. */
    val requests: List<TopicRequest>? = null,
    val requestText: String = "",
    val submittingRequest: Boolean = false,
    val requestError: String? = null,
    /** Null unless the user is deleting their account. */
    val delete: DeleteState? = null,
)

class ProfileViewModel(
    private val api: ChangeloomApi,
    private val auth: AuthRepository,
    private val session: SessionManager,
    private val theme: ThemePreferences,
    private val analytics: Analytics,
    private val strings: Strings,
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
                    AppLog.w(TAG_PROFILE, "Couldn't load the profile photo; showing initials", e)
                }
            }
        }
    }

    /** Counts change whenever the user saves or reads a story, so the screen refreshes them each time it is shown. */
    fun refresh() {
        viewModelScope.launch {
            try {
                val me = api.me()
                _state.update { it.copy(stats = me.stats, country = me.country, error = null) }
                if (me.country == null) deviceCountry()?.let(::setCountry)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                AppLog.failure(TAG_PROFILE, "Couldn't load stats", e)
                _state.update { it.copy(error = strings.errorText(e, R.string.stats_load_failed)) }
            }
            try {
                val requests = api.topicRequests()
                _state.update { it.copy(requests = requests) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                AppLog.failure(TAG_PROFILE, "Couldn't load topic requests", e)
            }
        }
    }

    /** Saves the user's country; it scopes deal stories. */
    fun setCountry(code: String) {
        val previous = _state.value.country
        _state.update { it.copy(country = code) }
        viewModelScope.launch {
            try {
                api.putMyCountry(code)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                AppLog.failure(TAG_PROFILE, "Couldn't save the country", e)
                _state.update { it.copy(country = previous, error = strings.errorText(e, R.string.country_save_failed)) }
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
                analytics.topicRequest()
                _state.update { it.copy(submittingRequest = false, requestText = "", requests = listOf(created) + it.requests.orEmpty()) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                val message = when ((e as? ApiException)?.status) {
                    400 -> strings.get(R.string.request_length, REQUEST_MIN_LENGTH, REQUEST_MAX_LENGTH)
                    409 -> strings.get(R.string.request_duplicate)
                    429 -> strings.get(R.string.request_too_many)
                    else -> {
                        AppLog.failure(TAG_PROFILE, "Couldn't send a topic request", e)
                        strings.errorText(e, R.string.request_send_failed)
                    }
                }
                _state.update { it.copy(submittingRequest = false, requestError = message) }
            }
        }
    }

    fun setThemeMode(mode: ThemeMode) = theme.setMode(mode)

    fun signOut() = session.signOut()

    fun startDelete() = _state.update { it.copy(delete = DeleteState(DeleteStep.Confirm)) }

    fun cancelDelete() = _state.update { if (it.delete?.step == DeleteStep.Deleting) it else it.copy(delete = null) }

    /** Deletes right away when the sign-in is recent enough; otherwise asks the user to confirm it's them first. */
    fun confirmDelete() = if (auth.needsReauth()) askToReauth() else delete {}

    fun deleteWithPassword(password: String) = delete { auth.reauthenticateWithPassword(password) }

    fun deleteWithGoogle(token: String) = delete { auth.reauthenticateWithGoogleIdToken(token) }

    /** The Google account picker failed before there was a token. */
    fun googleReauthFailed(error: Exception) {
        AppLog.w(TAG_PROFILE, "Google re-authentication failed", error)
        askToReauth(strings.errorText(error, R.string.reauth_failed))
    }

    private fun askToReauth(error: String? = null) {
        val step = if (auth.currentUser.value?.signInMethod == SignInMethod.Google) DeleteStep.Google else DeleteStep.Password
        _state.update { it.copy(delete = DeleteState(step, error)) }
    }

    /** Re-authenticates with [reauth], then deletes everything; success signs out, which clears this view model. */
    private fun delete(reauth: suspend () -> Unit) {
        _state.update { it.copy(delete = DeleteState(DeleteStep.Deleting)) }
        viewModelScope.launch {
            try {
                reauth()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                AppLog.w(TAG_PROFILE, "Re-authentication failed", e)
                askToReauth(strings.errorText(e, R.string.reauth_failed))
                return@launch
            }
            try {
                session.deleteAccount()
            } catch (e: ReauthRequiredException) {
                askToReauth()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                AppLog.failure(TAG_PROFILE, "Couldn't delete the account", e)
                _state.update { it.copy(delete = DeleteState(DeleteStep.Confirm, strings.errorText(e, R.string.delete_failed))) }
            }
        }
    }
}

/** The device locale's region when it is a known country code; the default until the user picks one. */
private fun deviceCountry(): String? =
    Locale.getDefault().country.takeIf { it in Locale.getISOCountries() }
