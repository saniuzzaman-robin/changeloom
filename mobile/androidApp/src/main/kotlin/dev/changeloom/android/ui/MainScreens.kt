package dev.changeloom.android.ui

import android.text.format.DateFormat
import androidx.activity.compose.BackHandler
import androidx.annotation.StringRes
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.AnimatedContentTransitionScope
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.AnimatedVisibilityScope
import androidx.compose.animation.ContentTransform
import androidx.compose.animation.SharedTransitionLayout
import androidx.compose.animation.SharedTransitionScope
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.scaleIn
import androidx.compose.animation.scaleOut
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.calculateEndPadding
import androidx.compose.foundation.layout.calculateStartPadding
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBars
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyItemScope
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Home
import androidx.compose.material.icons.rounded.Bookmark
import androidx.compose.material.icons.rounded.BookmarkBorder
import androidx.compose.material.icons.rounded.BookmarkRemove
import androidx.compose.material.icons.rounded.CloudOff
import androidx.compose.material.icons.rounded.DoneAll
import androidx.compose.material.icons.rounded.ErrorOutline
import androidx.compose.material.icons.rounded.Home
import androidx.compose.material.icons.rounded.KeyboardArrowUp
import androidx.compose.material.icons.rounded.MarkEmailUnread
import androidx.compose.material.icons.rounded.MoreVert
import androidx.compose.material.icons.rounded.Person
import androidx.compose.material.icons.rounded.PersonOutline
import androidx.compose.material.icons.rounded.Refresh
import androidx.compose.material.icons.rounded.RemoveCircleOutline
import androidx.compose.material.icons.rounded.Search
import androidx.compose.material.icons.rounded.VisibilityOff
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Snackbar
import androidx.compose.material3.SnackbarDuration
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.SnackbarResult
import androidx.compose.material3.SwipeToDismissBox
import androidx.compose.material3.SwipeToDismissBoxValue
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.material3.pulltorefresh.PullToRefreshDefaults
import androidx.compose.material3.pulltorefresh.rememberPullToRefreshState
import androidx.compose.material3.rememberSwipeToDismissBoxState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.saveable.rememberSaveableStateHolder
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.layout.layout
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.customActions
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.changeloom.android.R
import dev.changeloom.android.ads.FeedAds
import dev.changeloom.android.ads.NativeAdCard
import dev.changeloom.android.ads.NativeAdRepository
import dev.changeloom.android.data.FollowSuggester
import dev.changeloom.android.data.suggestion
import dev.changeloom.android.play.ReviewPromptEffect
import dev.changeloom.android.push.NotificationRationale
import dev.changeloom.android.telemetry.TrackScreen
import dev.changeloom.android.ui.components.BannerTone
import dev.changeloom.android.ui.components.EmptyState
import dev.changeloom.android.ui.components.Eyebrow
import dev.changeloom.android.ui.components.GlassCard
import dev.changeloom.android.ui.components.GradientAvatar
import dev.changeloom.android.ui.components.GradientText
import dev.changeloom.android.ui.components.ImportanceMeter
import dev.changeloom.android.ui.components.KindPill
import dev.changeloom.android.ui.components.LoomMark
import dev.changeloom.android.ui.components.PrimaryButton
import dev.changeloom.android.ui.components.SaveToggle
import dev.changeloom.android.ui.components.ScreenBackdrop
import dev.changeloom.android.ui.components.SecondaryButton
import dev.changeloom.android.ui.components.SeverityDot
import dev.changeloom.android.ui.components.SkeletonBlock
import dev.changeloom.android.ui.components.StatusBanner
import dev.changeloom.android.ui.components.TextAction
import dev.changeloom.android.ui.components.TopicChip
import dev.changeloom.android.ui.components.enter
import dev.changeloom.android.ui.theme.ChangeloomTheme
import dev.changeloom.android.ui.theme.Durations
import dev.changeloom.android.ui.theme.Radius
import dev.changeloom.android.ui.theme.Spacing
import dev.changeloom.android.ui.theme.ThemeMode
import dev.changeloom.android.ui.theme.expoTween
import dev.changeloom.shared.data.ReadFilter
import dev.changeloom.shared.data.STORY_KINDS
import dev.changeloom.shared.data.StorySummary
import dev.changeloom.shared.data.TimelineFilter
import dev.changeloom.shared.data.TimelineState
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import org.koin.androidx.compose.koinViewModel
import org.koin.compose.koinInject
import java.time.Duration
import java.time.Instant
import java.time.LocalDate
import java.time.LocalTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.DateTimeParseException
import java.util.Locale

private enum class Tab(@StringRes val label: Int, val icon: ImageVector, val selectedIcon: ImageVector) {
    Feed(R.string.tab_feed, Icons.Outlined.Home, Icons.Rounded.Home),
    Search(R.string.tab_search, Icons.Rounded.Search, Icons.Rounded.Search),
    Saved(R.string.tab_saved, Icons.Rounded.BookmarkBorder, Icons.Rounded.Bookmark),
    Profile(R.string.tab_profile, Icons.Rounded.PersonOutline, Icons.Rounded.Person),
}

/** What covers the tab shell, if anything. */
private sealed interface Overlay {
    data object None : Overlay
    data object Topics : Overlay
    data class Story(val id: Long) : Overlay
}

/** Display name for a topic slug; falls back to the slug's last segment until the topic tree loads. */
internal val LocalTopicName = staticCompositionLocalOf<(String) -> String> { { it.substringAfterLast('/') } }

/** Set by [MainScreen] so story titles can morph between a card and the detail screen; null in previews. */
internal val LocalSharedTransitionScope = staticCompositionLocalOf<SharedTransitionScope?> { null }
internal val LocalOverlayScope = staticCompositionLocalOf<AnimatedVisibilityScope?> { null }

/**
 * Marks a story title as the shared element between its card and the detail screen. A shared element draws only the
 * destination's title, moving it into place; shared bounds would cross-fade both, and since the two titles differ in
 * size and line breaks, the outgoing one showed as ghost text over the new one.
 */
@Composable
internal fun Modifier.sharedStoryTitle(id: Long): Modifier {
    val shared = LocalSharedTransitionScope.current ?: return this
    val scope = LocalOverlayScope.current ?: return this
    return with(shared) {
        // Laid out at its final size while it moves, so the text doesn't reflow or clip inside the growing bounds.
        sharedElement(rememberSharedContentState("story-title-$id"), scope, boundsTransform = { _, _ -> expoTween(Durations.SLOW) })
            .skipToLookaheadSize()
    }
}

/**
 * No nav library: the tab, open story and topic editor survive rotation via rememberSaveable, and each
 * tab's scroll position is kept by a SaveableStateHolder. [openStoryId] is a story to open right away
 * (a tapped push notification); [onOpenStoryHandled] clears it.
 */
@Composable
fun MainScreen(openStoryId: Long? = null, onOpenStoryHandled: () -> Unit = {}) {
    var storyId by rememberSaveable { mutableStateOf<Long?>(null) }
    var editingTopics by rememberSaveable { mutableStateOf(false) }
    var tab by rememberSaveable { mutableStateOf(Tab.Feed) }
    val shell = rememberSaveableStateHolder()
    val picker: TopicPickerViewModel = koinViewModel()
    // Derived, so picker edits (each toggle, each search keystroke) don't recompose the whole screen.
    val pickerState = picker.state.collectAsStateWithLifecycle()
    val tree by remember { derivedStateOf { pickerState.value.selection?.tree } }
    val topicName = remember(tree) { { slug: String -> tree?.topic(slug)?.name ?: slug.substringAfterLast('/') } }

    LaunchedEffect(openStoryId) {
        if (openStoryId != null) {
            storyId = openStoryId
            onOpenStoryHandled()
        }
    }

    val overlay = storyId?.let { Overlay.Story(it) } ?: if (editingTopics) Overlay.Topics else Overlay.None
    TrackScreen(
        when (overlay) {
            is Overlay.Story -> "story"
            Overlay.Topics -> "topics_edit"
            Overlay.None -> tab.name.lowercase()
        },
    )
    BackHandler(enabled = storyId != null) { storyId = null }
    BackHandler(enabled = overlay == Overlay.None && tab != Tab.Feed) { tab = Tab.Feed }
    NotificationRationale()
    ReviewPromptEffect(enabled = overlay == Overlay.None)

    CompositionLocalProvider(LocalTopicName provides topicName) {
        SharedTransitionLayout(Modifier.fillMaxSize().background(ChangeloomTheme.colors.bg)) {
            AnimatedContent(overlay, transitionSpec = { overlayTransition() }, label = "overlay") { target ->
                CompositionLocalProvider(LocalSharedTransitionScope provides this@SharedTransitionLayout, LocalOverlayScope provides this) {
                    when (target) {
                        Overlay.None -> shell.SaveableStateProvider("shell") {
                            Shell(tab, onTab = { tab = it }, onOpen = { storyId = it }, onEditTopics = { editingTopics = true })
                        }
                        Overlay.Topics -> TopicPickerScreen(TopicPickerMode.Edit, onExit = { editingTopics = false }, vm = picker)
                        is Overlay.Story -> StoryDetailScreen(target.id, onBack = { storyId = null })
                    }
                }
            }
        }
    }
}

private fun AnimatedContentTransitionScope<Overlay>.overlayTransition(): ContentTransform {
    val opening = targetState != Overlay.None
    val enter = fadeIn(expoTween(Durations.MEDIUM)) + scaleIn(expoTween(Durations.SLOW), initialScale = if (opening) 0.96f else 1.02f)
    val exit = fadeOut(expoTween(Durations.FAST)) + scaleOut(expoTween(Durations.SLOW), targetScale = if (opening) 1.02f else 0.96f)
    return (enter togetherWith exit).apply { targetContentZIndex = if (opening) 1f else 0f }
}

@Composable
private fun Shell(tab: Tab, onTab: (Tab) -> Unit, onOpen: (Long) -> Unit, onEditTopics: () -> Unit) {
    val tabs = rememberSaveableStateHolder()
    val timeline: TimelineViewModel = koinViewModel()
    val profile: ProfileViewModel = koinViewModel()
    // The same photo the Profile tab shows; ProfileViewModel loads it once per sign-in.
    val photo = profile.photo.collectAsStateWithLifecycle().value
    val photoImage = remember(photo) { photo?.asImageBitmap() }
    // Derived, so refresh and paging updates recompose the shell only when the tab bar's badge changes.
    val timelineState = timeline.state.collectAsStateWithLifecycle()
    val hasUnread by remember { derivedStateOf { timelineState.value.items.any { !it.isRead } } }
    // Only the feed keeps its scroll position across tab switches: leaving another tab drops its saved UI state, so
    // it opens at the top next time. Opening a story isn't a switch, so lists keep their place behind it.
    var shownTab by remember { mutableStateOf(tab) }
    LaunchedEffect(tab) {
        if (shownTab != tab && shownTab != Tab.Feed) tabs.removeState(shownTab.name)
        shownTab = tab
    }
    // One backdrop behind every tab, so it spans the whole screen and stays put while tabs slide.
    Box(Modifier.fillMaxSize()) {
        ScreenBackdrop(Modifier.matchParentSize())
        Scaffold(
            containerColor = Color.Transparent,
            contentWindowInsets = WindowInsets(0),
            bottomBar = { TabBar(tab, onTab, hasUnread) },
        ) { padding ->
            AnimatedContent(tab, transitionSpec = { tabTransition() }, label = "tabs") { current ->
                tabs.SaveableStateProvider(current.name) {
                    when (current) {
                        Tab.Feed -> FeedScreen(
                            onOpen,
                            onProfile = { onTab(Tab.Profile) },
                            displayName = profile.displayName,
                            userEmail = profile.email,
                            photo = photoImage,
                            contentPadding = padding,
                        )
                        Tab.Search -> SearchScreen(onOpen, contentPadding = padding)
                        Tab.Saved -> SavedScreen(onOpen, onBrowse = { onTab(Tab.Feed) }, contentPadding = padding)
                        Tab.Profile -> ProfileScreen(onEditTopics, contentPadding = padding)
                    }
                }
            }
        }
    }
}

private fun AnimatedContentTransitionScope<Tab>.tabTransition(): ContentTransform {
    val dir = if (targetState.ordinal > initialState.ordinal) 1 else -1
    return (fadeIn(expoTween(Durations.MEDIUM)) + slideInHorizontally(expoTween(Durations.SLOW)) { dir * it / TAB_SLIDE_DIVISOR }) togetherWith
        (fadeOut(expoTween(Durations.FAST)) + slideOutHorizontally(expoTween(Durations.SLOW)) { -dir * it / TAB_SLIDE_DIVISOR })
}

@Composable
private fun TabBar(current: Tab, onSelect: (Tab) -> Unit, hasUnread: Boolean) {
    val c = ChangeloomTheme.colors
    Row(
        Modifier
            .fillMaxWidth()
            .background(c.navGlass)
            .drawBehind { drawLine(c.line, Offset.Zero, Offset(size.width, 0f), 1.dp.toPx()) }
            .windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Horizontal + WindowInsetsSides.Bottom))
            .padding(horizontal = 8.dp, vertical = 8.dp),
    ) {
        Tab.entries.forEach { tab ->
            TabItem(tab, selected = tab == current, badge = tab == Tab.Feed && hasUnread, onClick = { onSelect(tab) }, Modifier.weight(1f))
        }
    }
}

@Composable
private fun TabItem(tab: Tab, selected: Boolean, badge: Boolean, onClick: () -> Unit, modifier: Modifier = Modifier) {
    val c = ChangeloomTheme.colors
    val fg by animateColorAsState(if (selected) c.primaryText else c.fgSubtle, expoTween(), label = "tabFg")
    val pill by animateColorAsState(if (selected) c.primary.copy(alpha = 0.16f) else Color.Transparent, expoTween(), label = "tabPill")
    val pillWidth by animateDpAsState(if (selected) 60.dp else 40.dp, expoTween(Durations.SLOW), label = "tabPillWidth")
    Column(
        modifier.clip(Radius.lg).selectable(selected, role = Role.Tab, onClick = onClick).padding(vertical = 4.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Box(Modifier.width(pillWidth).height(30.dp).clip(Radius.pill).background(pill), contentAlignment = Alignment.Center) {
            Icon(if (selected) tab.selectedIcon else tab.icon, contentDescription = null, tint = fg, modifier = Modifier.size(22.dp))
            if (badge) {
                Box(Modifier.align(Alignment.TopEnd).padding(top = 4.dp, end = 8.dp).size(7.dp).clip(CircleShape).background(c.accent))
            }
        }
        Spacer(Modifier.height(4.dp))
        Text(stringResource(tab.label), style = MaterialTheme.typography.labelSmall, color = fg)
    }
}

@Composable
private fun FeedScreen(
    onOpen: (Long) -> Unit,
    onProfile: () -> Unit,
    displayName: String?,
    userEmail: String?,
    photo: ImageBitmap?,
    contentPadding: PaddingValues,
    vm: TimelineViewModel = koinViewModel(),
    picker: TopicPickerViewModel = koinViewModel(),
    adRepository: NativeAdRepository = koinInject(),
    suggester: FollowSuggester = koinInject(),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val undo by vm.undo.collectAsStateWithLifecycle()
    // Derived, so picker edits don't recompose the feed; only a change to the followed set does.
    val pickerState = picker.state.collectAsStateWithLifecycle()
    val followed by remember { derivedStateOf { pickerState.value.followed.toSet() } }
    val menu = remember(followed) { StoryMenu(followed, vm::dismiss, vm::muteTopic) }
    val counts = suggester.counts.collectAsStateWithLifecycle()
    // Until the followed topics load, every topic looks unfollowed, so nothing is suggested yet.
    val suggestion by remember {
        derivedStateOf {
            val p = pickerState.value
            if (p.loading && p.followed.isEmpty()) {
                null
            } else {
                counts.value.suggestion(followed)?.let { FollowSuggestion(it, counts.value.opens[it] ?: 0, p.saving, p.error) }
            }
        }
    }
    val ads by adRepository.feedAds.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { adRepository.refresh() }
    FeedContent(
        state = state,
        displayName = displayName,
        userEmail = userEmail,
        photo = photo,
        contentPadding = contentPadding,
        ads = ads,
        onOpen = onOpen,
        onProfile = onProfile,
        onRefresh = {
            vm.refresh()
            adRepository.refresh()
        },
        onLoadMore = vm::loadMore,
        onFilter = vm::setFilter,
        onSetRead = vm::setRead,
        onSetSaved = vm::setBookmarked,
        onDismissError = vm::dismissError,
        onSeen = vm::storySeen,
        menu = menu,
        suggestion = suggestion,
        onFollow = picker::follow,
        onDeclineSuggestion = vm::declineSuggestion,
        undo = undo,
        onUndo = vm::undo,
        onUndoExpired = vm::undoExpired,
    )
    LifecycleEventEffect(Lifecycle.Event.ON_STOP) { vm.flushViews() }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun FeedContent(
    state: TimelineState,
    displayName: String?,
    userEmail: String?,
    contentPadding: PaddingValues,
    onOpen: (Long) -> Unit,
    onProfile: () -> Unit,
    onRefresh: () -> Unit,
    onLoadMore: () -> Unit,
    onSetRead: (Long, Boolean) -> Unit,
    onSetSaved: (Long, Boolean) -> Unit,
    onDismissError: () -> Unit,
    onFilter: (TimelineFilter) -> Unit = {},
    ads: FeedAds? = null,
    photo: ImageBitmap? = null,
    onSeen: (Long) -> Unit = {},
    menu: StoryMenu? = null,
    suggestion: FollowSuggestion? = null,
    onFollow: (String) -> Unit = {},
    onDeclineSuggestion: (String) -> Unit = {},
    undo: FeedUndo? = null,
    onUndo: (FeedUndo) -> Unit = {},
    onUndoExpired: (FeedUndo) -> Unit = {},
) {
    val c = ChangeloomTheme.colors
    val topicName = LocalTopicName.current
    val snackbar = remember { SnackbarHostState() }
    val undoMessage = when (undo) {
        is FeedUndo.Dismissed -> stringResource(R.string.story_hidden)
        is FeedUndo.Muted -> stringResource(R.string.topic_muted, topicName(undo.topic))
        null -> null
    }
    val undoLabel = stringResource(R.string.undo)
    LaunchedEffect(undo) {
        val action = undo ?: return@LaunchedEffect
        val result = snackbar.showSnackbar(undoMessage.orEmpty(), actionLabel = undoLabel, duration = SnackbarDuration.Short)
        if (result == SnackbarResult.ActionPerformed) onUndo(action) else onUndoExpired(action)
    }
    val listState = rememberLazyListState()
    val pullState = rememberPullToRefreshState()
    val (fresh, earlier) = remember(state.items) { state.items.partition { !it.isRead } }
    val staggered = rememberStaggered(listState)
    LoadMoreEffect(listState, onLoadMore)
    SeenEffect(listState, onSeen)

    Box(Modifier.fillMaxSize().tabContentBounds(contentPadding)) {
        PullToRefreshBox(
            isRefreshing = state.refreshing && state.items.isNotEmpty(),
            onRefresh = onRefresh,
            modifier = Modifier.fillMaxSize(),
            state = pullState,
            indicator = {
                PullToRefreshDefaults.Indicator(
                    state = pullState,
                    isRefreshing = state.refreshing && state.items.isNotEmpty(),
                    modifier = Modifier.align(Alignment.TopCenter),
                    containerColor = c.elevated,
                    color = c.primaryText,
                )
            },
        ) {
            LazyColumn(
                Modifier.fillMaxSize().testTag("feed"),
                state = listState,
                contentPadding = listPadding(),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                item(key = "header") { FeedHeader(displayName, userEmail, photo, fresh.size, onProfile, animateIn = staggered) }
                item(key = "banners") {
                    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        StatusBanner(if (state.offline) stringResource(R.string.offline_banner) else null, Icons.Rounded.CloudOff, tone = BannerTone.Warning)
                        StatusBanner(state.error, Icons.Rounded.ErrorOutline, tone = BannerTone.Error, actionLabel = stringResource(R.string.dismiss), onAction = onDismissError)
                    }
                }
                if (state.items.isNotEmpty() || !state.filter.isDefault) {
                    stickyHeader(key = "filters", contentType = FILTER_CHIP) { FeedFilters(state.filter, onFilter) }
                }
                when {
                    state.items.isEmpty() && state.refreshing -> items(LIST_SKELETON_CARDS) { SkeletonStoryCard() }
                    state.items.isEmpty() && !state.filter.isDefault -> item(key = "empty_filtered") {
                        EmptyState(
                            title = stringResource(R.string.filter_no_match_title),
                            message = stringResource(R.string.filter_no_match_body),
                            action = { SecondaryButton(stringResource(R.string.filter_clear), { onFilter(TimelineFilter()) }) },
                        )
                    }
                    state.items.isEmpty() -> item(key = "empty") {
                        EmptyState(
                            title = stringResource(R.string.feed_empty_title),
                            message = stringResource(R.string.feed_empty_body),
                            action = { SecondaryButton(stringResource(R.string.refresh), onRefresh, icon = Icons.Rounded.Refresh) },
                        )
                    }
                    else -> {
                        // The suggestion follows the first few new stories, or opens the earlier ones when nothing is new.
                        val suggestAfter = minOf(FOLLOW_CARD_AFTER, fresh.size) - 1
                        if (fresh.isNotEmpty()) {
                            item(key = "new") { SectionHeader(stringResource(R.string.section_new), fresh.size, highlight = true, Modifier.animateItem()) }
                            fresh.forEachIndexed { i, story ->
                                item(key = story.id, contentType = STORY_CONTENT) { FeedCard(story, staggered, i, onOpen, onSetRead, onSetSaved, menu) }
                                feedAd(ads, i)
                                if (i == suggestAfter) followSuggestion(suggestion, onFollow, onDeclineSuggestion)
                            }
                        }
                        if (earlier.isNotEmpty()) {
                            item(key = "earlier") { SectionHeader(stringResource(R.string.section_earlier), null, highlight = false, Modifier.animateItem()) }
                            if (fresh.isEmpty()) followSuggestion(suggestion, onFollow, onDeclineSuggestion)
                            earlier.forEachIndexed { i, story ->
                                item(key = story.id, contentType = STORY_CONTENT) {
                                    FeedCard(story, staggered, fresh.size + i, onOpen, onSetRead, onSetSaved, menu)
                                }
                                feedAd(ads, fresh.size + i)
                            }
                        }
                        if (state.loadingMore) item(key = "more") { SkeletonStoryCard() }
                        if (state.reachedEnd) item(key = "end", contentType = END_CONTENT) { FeedEnd(Modifier.animateItem()) }
                    }
                }
            }
        }
        BackToTopButton(listState, Modifier.align(Alignment.BottomEnd))
        SnackbarHost(snackbar, Modifier.align(Alignment.BottomCenter).padding(horizontal = Spacing.gutter, vertical = 12.dp)) { data ->
            Snackbar(data, containerColor = c.elevated, contentColor = c.fg, actionColor = c.primaryText)
        }
    }
}

/** Read state (one of unread/read, or neither for all) and story kinds (any number); the server applies them. */
@Composable
private fun FeedFilters(filter: TimelineFilter, onChange: (TimelineFilter) -> Unit) {
    val gutter = Spacing.gutter
    LazyRow(
        Modifier
            // Pinned to the top of the list: opaque so stories scroll underneath, and out to the screen edges past the list's gutter.
            .layout { measurable, constraints ->
                val bleed = gutter.roundToPx()
                val placeable = measurable.measure(constraints.copy(minWidth = constraints.maxWidth + 2 * bleed, maxWidth = constraints.maxWidth + 2 * bleed))
                layout(constraints.maxWidth, placeable.height) { placeable.place(-bleed, 0) }
            }
            .background(ChangeloomTheme.colors.navGlass)
            .padding(vertical = 6.dp)
            .testTag("feed_filters"),
        contentPadding = PaddingValues(horizontal = gutter),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        item(key = "unread", contentType = FILTER_CHIP) {
            TopicChip(
                stringResource(R.string.filter_unread),
                selected = filter.read == ReadFilter.Unread,
                onClick = { onChange(filter.copy(read = if (filter.read == ReadFilter.Unread) ReadFilter.All else ReadFilter.Unread)) },
            )
        }
        item(key = "read", contentType = FILTER_CHIP) {
            TopicChip(
                stringResource(R.string.filter_read),
                selected = filter.read == ReadFilter.Read,
                onClick = { onChange(filter.copy(read = if (filter.read == ReadFilter.Read) ReadFilter.All else ReadFilter.Read)) },
            )
        }
        items(STORY_KINDS, key = { it }, contentType = { FILTER_CHIP }) { kind ->
            TopicChip(
                kind.replaceFirstChar { it.uppercase() },
                selected = kind in filter.kinds,
                onClick = { onChange(filter.copy(kinds = if (kind in filter.kinds) filter.kinds - kind else filter.kinds + kind)) },
            )
        }
    }
}

private const val FILTER_CHIP = "filter_chip"

/**
 * True only when the server has said there are no more pages: not while refreshing or loading more, and not for
 * cached stories shown offline, where more may exist.
 */
private val TimelineState.reachedEnd: Boolean
    get() = items.isNotEmpty() && nextCursor == null && !refreshing && !loadingMore && !offline

/** The bottom of the feed: the loom mark between two hairlines, then what to expect next. */
@Composable
private fun FeedEnd(modifier: Modifier = Modifier) {
    val c = ChangeloomTheme.colors
    Column(
        modifier.fillMaxWidth().padding(top = 20.dp, bottom = 8.dp).testTag("feed_end"),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            HorizontalDivider(Modifier.weight(1f), color = c.line)
            LoomMark(Modifier.padding(horizontal = 14.dp).size(28.dp))
            HorizontalDivider(Modifier.weight(1f), color = c.line)
        }
        Spacer(Modifier.height(14.dp))
        Text(stringResource(R.string.feed_end_title), style = MaterialTheme.typography.titleSmall, color = c.fg, textAlign = TextAlign.Center)
        Spacer(Modifier.height(4.dp))
        Text(
            stringResource(R.string.feed_end_body),
            Modifier.padding(horizontal = 24.dp),
            style = MaterialTheme.typography.bodySmall,
            color = c.fgMuted,
            textAlign = TextAlign.Center,
        )
    }
}

/** A floating arrow that appears once a few cards are scrolled away and takes the list back to the top. */
@Composable
private fun BackToTopButton(listState: LazyListState, modifier: Modifier = Modifier) {
    val c = ChangeloomTheme.colors
    val scope = rememberCoroutineScope()
    val visible by remember { derivedStateOf { listState.firstVisibleItemIndex >= BACK_TO_TOP_AFTER_ITEMS } }
    AnimatedVisibility(
        visible,
        modifier
            .windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Horizontal))
            .padding(end = Spacing.gutter, bottom = 16.dp),
        enter = fadeIn(expoTween()) + scaleIn(expoTween(), initialScale = 0.8f),
        exit = fadeOut(expoTween(Durations.FAST)) + scaleOut(expoTween(Durations.FAST), targetScale = 0.8f),
    ) {
        Box(
            Modifier
                .size(48.dp)
                .shadow(12.dp, CircleShape, ambientColor = Color.Black.copy(alpha = 0.25f), spotColor = Color.Black.copy(alpha = 0.35f))
                .clip(CircleShape)
                .background(c.elevated)
                .border(1.dp, c.lineStrong, CircleShape)
                .clickable(role = Role.Button, onClickLabel = stringResource(R.string.back_to_top)) {
                    scope.launch {
                        // A long jump first, so the animated part stays short however far down the list is.
                        if (listState.firstVisibleItemIndex > BACK_TO_TOP_JUMP_ITEMS) listState.scrollToItem(BACK_TO_TOP_JUMP_ITEMS)
                        listState.animateScrollToItem(0)
                    }
                },
            contentAlignment = Alignment.Center,
        ) {
            Icon(Icons.Rounded.KeyboardArrowUp, contentDescription = stringResource(R.string.back_to_top), tint = c.fg, modifier = Modifier.size(26.dp))
        }
    }
}

/** A topic the user keeps opening but doesn't follow, with how often they opened it and the follow call's state. */
internal data class FollowSuggestion(val topic: String, val opens: Int, val saving: Boolean, val error: String?)

private fun LazyListScope.followSuggestion(suggestion: FollowSuggestion?, onFollow: (String) -> Unit, onDecline: (String) -> Unit) {
    if (suggestion == null) return
    item(key = "follow_suggestion", contentType = FOLLOW_CONTENT) { FollowSuggestionCard(suggestion, onFollow, onDecline, Modifier.animateItem()) }
}

@Composable
private fun FollowSuggestionCard(suggestion: FollowSuggestion, onFollow: (String) -> Unit, onDecline: (String) -> Unit, modifier: Modifier = Modifier) {
    val c = ChangeloomTheme.colors
    val name = LocalTopicName.current(suggestion.topic)
    GlassCard(modifier.fillMaxWidth().testTag("follow_suggestion")) {
        Eyebrow(stringResource(R.string.suggested), color = c.accent)
        Spacer(Modifier.height(8.dp))
        Text(stringResource(R.string.follow_suggestion_title, name), style = MaterialTheme.typography.titleMedium, color = c.fg)
        Spacer(Modifier.height(4.dp))
        Text(
            pluralStringResource(R.plurals.follow_suggestion_body, suggestion.opens, suggestion.opens),
            style = MaterialTheme.typography.bodyMedium,
            color = c.fgMuted,
        )
        if (suggestion.error != null) {
            Spacer(Modifier.height(8.dp))
            Text(suggestion.error, style = MaterialTheme.typography.bodySmall, color = c.red)
        }
        Spacer(Modifier.height(12.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
            PrimaryButton(stringResource(R.string.follow), { onFollow(suggestion.topic) }, loading = suggestion.saving)
            TextAction(stringResource(R.string.not_now), { onDecline(suggestion.topic) }, enabled = !suggestion.saving)
        }
    }
}

/** The ad after the story at [index] in the feed, if one goes there. Ads never show outside the feed. */
private fun LazyListScope.feedAd(ads: FeedAds?, index: Int) {
    val slot = ads?.slotAfter(index) ?: return
    val ad = ads.adFor(slot) ?: return
    item(key = "ad-$slot", contentType = AD_CONTENT) { NativeAdCard(ad, Modifier.animateItem()) }
}

@Composable
private fun LazyItemScope.FeedCard(
    story: StorySummary,
    animateIn: Boolean,
    index: Int,
    onOpen: (Long) -> Unit,
    onSetRead: (Long, Boolean) -> Unit,
    onSetSaved: (Long, Boolean) -> Unit,
    menu: StoryMenu?,
) {
    val entrance = if (animateIn && index < STAGGERED_CARDS) Modifier.enter(delayMillis = STAGGER_START_MILLIS + index * STAGGER_STEP_MILLIS) else Modifier
    SwipeActions(
        startToEnd = SwipeAction(
            label = stringResource(if (story.isRead) R.string.mark_unread else R.string.mark_read),
            icon = if (story.isRead) Icons.Rounded.MarkEmailUnread else Icons.Rounded.DoneAll,
            color = ChangeloomTheme.colors.success,
            onSwipe = { onSetRead(story.id, !story.isRead) },
        ),
        endToStart = SwipeAction(
            label = stringResource(if (story.isBookmarked) R.string.unsave else R.string.save),
            icon = if (story.isBookmarked) Icons.Rounded.BookmarkRemove else Icons.Rounded.Bookmark,
            color = ChangeloomTheme.colors.primary,
            onSwipe = { onSetSaved(story.id, !story.isBookmarked) },
        ),
        // A swipe action toggles in place, so the card springs back afterwards.
        resetAfterSwipe = true,
        modifier = Modifier.animateItem().then(entrance),
    ) {
        StoryCard(story, onOpen = { onOpen(story.id) }, onToggleSave = { onSetSaved(story.id, !story.isBookmarked) }, menu = menu)
    }
}

@Composable
private fun FeedHeader(
    displayName: String?,
    userEmail: String?,
    photo: ImageBitmap?,
    freshCount: Int,
    onProfile: () -> Unit,
    animateIn: Boolean,
) {
    val c = ChangeloomTheme.colors
    val name = greetingName(displayName, userEmail)
    val today = remember { LocalDate.now().format(localizedPattern("EEEEMMMd")) }
    Column(Modifier.padding(top = 8.dp, bottom = 4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Eyebrow(today, Modifier.enter(delayMillis = 0, enabled = animateIn), color = c.primaryText)
                Spacer(Modifier.height(8.dp))
                Column(Modifier.enter(delayMillis = 80, enabled = animateIn)) {
                    val greeting = stringResource(greeting(LocalTime.now()))
                    Text(
                        if (name != null) stringResource(R.string.greeting_before_name, greeting) else greeting,
                        style = MaterialTheme.typography.headlineMedium,
                        color = c.fg,
                    )
                    GradientText(name ?: stringResource(R.string.greeting_fallback_name), style = MaterialTheme.typography.headlineMedium)
                }
            }
            GradientAvatar(
                displayName ?: userEmail ?: "?",
                Modifier.clip(CircleShape).clickable(onClickLabel = stringResource(R.string.open_profile), onClick = onProfile).enter(delayMillis = 120, enabled = animateIn),
                size = 48.dp,
                photo = photo,
            )
        }
        Spacer(Modifier.height(8.dp))
        Text(
            if (freshCount == 0) {
                stringResource(R.string.feed_caught_up)
            } else {
                pluralStringResource(R.plurals.feed_new_updates, freshCount, freshCount)
            },
            Modifier.enter(delayMillis = 160, enabled = animateIn),
            style = MaterialTheme.typography.bodyMedium,
            color = c.fgMuted,
        )
    }
}

@Composable
private fun SectionHeader(title: String, count: Int?, highlight: Boolean, modifier: Modifier = Modifier) {
    val c = ChangeloomTheme.colors
    val color = if (highlight) c.primaryText else c.fgSubtle
    Row(modifier.fillMaxWidth().padding(top = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        Eyebrow(title, color = color)
        Spacer(Modifier.width(10.dp))
        Box(Modifier.weight(1f).height(1.dp).background(c.line))
        if (count != null) {
            Spacer(Modifier.width(10.dp))
            Eyebrow("$count", color = color)
        }
    }
}

/**
 * The feed card's "Not interested" and "Less about" actions. Followed topics aren't offered: muting one would unfollow
 * it, which belongs on the topics screen.
 */
@Immutable
internal class StoryMenu(val followed: Set<String>, val onNotInterested: (Long) -> Unit, val onLessAbout: (String) -> Unit)

/** Story card: meta row, title (shared with the detail screen), summary, topics, importance, save and the feed's [menu]. */
@Composable
internal fun StoryCard(
    story: StorySummary,
    onOpen: () -> Unit,
    onToggleSave: () -> Unit,
    modifier: Modifier = Modifier,
    menu: StoryMenu? = null,
) {
    val c = ChangeloomTheme.colors
    val topicName = LocalTopicName.current
    val brand = ChangeloomTheme.gradients.brand
    val accent by animateFloatAsState(if (story.isRead) 0f else 1f, expoTween(Durations.SLOW), label = "unreadAccent")
    GlassCard(
        modifier.fillMaxWidth().testTag("story_card"),
        onClick = onOpen,
        contentPadding = PaddingValues(start = Spacing.cardPadding, end = 4.dp, top = Spacing.cardPadding, bottom = 4.dp),
    ) {
        Column(
            Modifier.drawBehind {
                // Unread accent: a gradient rail along the card's left edge.
                if (accent > 0f) {
                    val inset = Spacing.cardPadding.toPx()
                    drawRect(brand, topLeft = Offset(-inset, -inset), size = Size(3.dp.toPx(), size.height + inset + 4.dp.toPx()), alpha = accent)
                }
            },
        ) {
            Row(Modifier.padding(end = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                KindPill(story.kind)
                SeverityDot(story.severity, Modifier.padding(start = 2.dp))
                Spacer(Modifier.weight(1f))
                // Only the timeline sets `match`; followed stories need no label.
                if (story.match != null && story.match != "followed") {
                    Eyebrow(
                        stringResource(if (story.match == "headline") R.string.headline else R.string.suggested),
                        Modifier.padding(end = 8.dp),
                        color = c.accent,
                    )
                }
                Eyebrow(relativeTime(story.publishedAt))
            }
            Spacer(Modifier.height(10.dp))
            Text(
                story.title,
                Modifier.padding(end = 12.dp).sharedStoryTitle(story.id),
                style = MaterialTheme.typography.titleMedium,
                fontWeight = if (story.isRead) FontWeight.Medium else FontWeight.Bold,
                color = if (story.isRead) c.fgMuted else c.fg,
                maxLines = TITLE_LINES,
                overflow = TextOverflow.Ellipsis,
            )
            Spacer(Modifier.height(6.dp))
            Text(
                story.summary,
                Modifier.padding(end = 12.dp),
                style = MaterialTheme.typography.bodyMedium,
                color = c.fgMuted,
                maxLines = SUMMARY_LINES,
                overflow = TextOverflow.Ellipsis,
            )
            Spacer(Modifier.height(10.dp))
            Row(verticalAlignment = Alignment.CenterVertically) {
                Row(Modifier.weight(1f), horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                    story.topics.take(MAX_CARD_TOPICS).forEach { TopicChip(topicName(it)) }
                    if (story.topics.size > MAX_CARD_TOPICS) Eyebrow("+${story.topics.size - MAX_CARD_TOPICS}")
                }
                ImportanceMeter(story.importance, Modifier.padding(start = 8.dp))
                SaveToggle(story.isBookmarked, onToggleSave)
                if (menu != null) StoryMenuButton(story, menu)
            }
        }
    }
}

@Composable
private fun StoryMenuButton(story: StorySummary, menu: StoryMenu) {
    val c = ChangeloomTheme.colors
    val topicName = LocalTopicName.current
    var open by remember { mutableStateOf(false) }
    Box {
        IconButton(onClick = { open = true }) {
            Icon(Icons.Rounded.MoreVert, contentDescription = stringResource(R.string.story_actions), tint = c.fgSubtle)
        }
        // The menu's content is composed only while it is open, so the topic filter runs once per opening.
        DropdownMenu(expanded = open, onDismissRequest = { open = false }, containerColor = c.elevated) {
            DropdownMenuItem(
                text = { Text(stringResource(R.string.not_interested), color = c.fg) },
                onClick = {
                    open = false
                    menu.onNotInterested(story.id)
                },
                leadingIcon = { Icon(Icons.Rounded.VisibilityOff, contentDescription = null, tint = c.fgMuted) },
            )
            val topics = remember(story.topics, menu.followed) { story.topics.filter { it !in menu.followed } }
            topics.forEach { slug ->
                DropdownMenuItem(
                    text = { Text(stringResource(R.string.less_about, topicName(slug)), color = c.fg) },
                    onClick = {
                        open = false
                        menu.onLessAbout(slug)
                    },
                    leadingIcon = { Icon(Icons.Rounded.RemoveCircleOutline, contentDescription = null, tint = c.fgMuted) },
                )
            }
        }
    }
}

@Composable
internal fun SkeletonStoryCard(modifier: Modifier = Modifier) {
    GlassCard(modifier.fillMaxWidth()) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            SkeletonBlock(Modifier.width(84.dp), height = 20.dp)
            Spacer(Modifier.weight(1f))
            SkeletonBlock(Modifier.width(48.dp), height = 10.dp)
        }
        Spacer(Modifier.height(14.dp))
        SkeletonBlock(Modifier.fillMaxWidth(0.92f), height = 18.dp)
        Spacer(Modifier.height(6.dp))
        SkeletonBlock(Modifier.fillMaxWidth(0.6f), height = 18.dp)
        Spacer(Modifier.height(12.dp))
        SkeletonBlock(Modifier.fillMaxWidth())
        Spacer(Modifier.height(6.dp))
        SkeletonBlock(Modifier.fillMaxWidth(0.8f))
        Spacer(Modifier.height(14.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            SkeletonBlock(Modifier.width(64.dp).clip(Radius.pill), height = 26.dp)
            SkeletonBlock(Modifier.width(52.dp).clip(Radius.pill), height = 26.dp)
        }
    }
}

internal data class SwipeAction(val label: String, val icon: ImageVector, val color: Color, val onSwipe: () -> Unit)

/**
 * Swipe-to-act wrapper. With [resetAfterSwipe] the content slides back once the action runs; without
 * it the caller is expected to remove the item (e.g. unsaving from the Saved list).
 */
@Composable
internal fun SwipeActions(
    startToEnd: SwipeAction?,
    endToStart: SwipeAction?,
    resetAfterSwipe: Boolean,
    modifier: Modifier = Modifier,
    content: @Composable () -> Unit,
) {
    val state = rememberSwipeToDismissBoxState()
    val scope = rememberCoroutineScope()
    val start by rememberUpdatedState(startToEnd)
    val end by rememberUpdatedState(endToStart)
    // SwipeToDismissBox re-runs onDismiss whenever the lambda changes while dismissed, so keep it stable.
    val onDismiss: (SwipeToDismissBoxValue) -> Unit = remember(state, scope, resetAfterSwipe) {
        { direction ->
            when (direction) {
                SwipeToDismissBoxValue.StartToEnd -> start?.onSwipe?.invoke()
                SwipeToDismissBoxValue.EndToStart -> end?.onSwipe?.invoke()
                SwipeToDismissBoxValue.Settled -> Unit
            }
            if (resetAfterSwipe) scope.launch { state.reset() }
        }
    }
    SwipeToDismissBox(
        state = state,
        modifier = modifier.semantics {
            customActions = listOfNotNull(startToEnd, endToStart).map { action ->
                CustomAccessibilityAction(action.label) {
                    action.onSwipe()
                    true
                }
            }
        },
        enableDismissFromStartToEnd = startToEnd != null,
        enableDismissFromEndToStart = endToStart != null,
        onDismiss = onDismiss,
        backgroundContent = {
            val action = when (state.dismissDirection) {
                SwipeToDismissBoxValue.StartToEnd -> startToEnd
                SwipeToDismissBoxValue.EndToStart -> endToStart
                SwipeToDismissBoxValue.Settled -> null
            }
            if (action != null) {
                val alignEnd = state.dismissDirection == SwipeToDismissBoxValue.EndToStart
                Row(
                    Modifier
                        .fillMaxSize()
                        .clip(Radius.xl)
                        .background(Brush.horizontalGradient(if (alignEnd) listOf(Color.Transparent, action.color.copy(alpha = 0.28f)) else listOf(action.color.copy(alpha = 0.28f), Color.Transparent)))
                        .padding(horizontal = 24.dp),
                    horizontalArrangement = if (alignEnd) Arrangement.End else Arrangement.Start,
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Icon(action.icon, contentDescription = null, tint = action.color, modifier = Modifier.size(22.dp))
                    Spacer(Modifier.width(8.dp))
                    Eyebrow(action.label, color = action.color)
                }
            }
        },
    ) { content() }
}

/** First-load entrance for the cards; off once the user scrolls, so cards recycled into view don't re-animate. */
@Composable
internal fun rememberStaggered(listState: LazyListState): Boolean {
    var staggered by rememberSaveable { mutableStateOf(true) }
    LaunchedEffect(listState) {
        snapshotFlow { listState.isScrollInProgress }.first { it }
        staggered = false
    }
    return staggered
}

/** Calls [onLoadMore] when the last items come into view, and again whenever the list grows while still there. */
@Composable
internal fun LoadMoreEffect(listState: LazyListState, onLoadMore: () -> Unit) {
    val latest by rememberUpdatedState(onLoadMore)
    LaunchedEffect(listState) {
        snapshotFlow {
            val info = listState.layoutInfo
            val last = info.visibleItemsInfo.lastOrNull()?.index ?: -1
            if (info.totalItemsCount > 0 && last >= info.totalItemsCount - LOAD_MORE_THRESHOLD) info.totalItemsCount else -1
        }
            .distinctUntilChanged()
            .filter { it >= 0 }
            .collect { latest() }
    }
}

/**
 * Calls [onSeen] with the id of each story (a `Long` item key) once at least half of it has stayed in view for
 * [SEEN_DWELL_MS]; scrolling past it sooner doesn't count.
 */
@Composable
internal fun SeenEffect(listState: LazyListState, onSeen: (Long) -> Unit) {
    val latest by rememberUpdatedState(onSeen)
    LaunchedEffect(listState) {
        val timers = mutableMapOf<Long, Job>()
        snapshotFlow {
            val info = listState.layoutInfo
            info.visibleItemsInfo.mapNotNullTo(mutableSetOf()) { item ->
                val id = item.key as? Long ?: return@mapNotNullTo null
                val visible = minOf(item.offset + item.size, info.viewportEndOffset) - maxOf(item.offset, info.viewportStartOffset)
                id.takeIf { item.size > 0 && visible * 2 >= item.size }
            }
        }.distinctUntilChanged().collect { inView ->
            timers.keys.filter { it !in inView }.forEach { timers.remove(it)?.cancel() }
            inView.filter { it !in timers }.forEach { id -> timers[id] = launch { delay(SEEN_DWELL_MS); latest(id) } }
        }
    }
}

private const val SEEN_DWELL_MS = 1_000L

/**
 * App rule: content never scrolls under the status bar or the navigation bar. A tab screen's container stops below
 * the status bar and above the tab bar ([shellPadding], which itself sits above the navigation bar); only
 * backgrounds draw behind the bars.
 */
@Composable
internal fun Modifier.tabContentBounds(shellPadding: PaddingValues): Modifier =
    windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Top))
        .padding(bottom = shellPadding.calculateBottomPadding())

/** List padding inside [tabContentBounds]: the gutter plus any side insets (a 3-button nav bar or cutout in landscape). */
@Composable
internal fun listPadding(): PaddingValues {
    val safe = WindowInsets.safeDrawing.asPaddingValues()
    val direction = LocalLayoutDirection.current
    return PaddingValues(
        start = safe.calculateStartPadding(direction) + Spacing.gutter,
        end = safe.calculateEndPadding(direction) + Spacing.gutter,
        top = 8.dp,
        bottom = 16.dp,
    )
}

@StringRes
private fun greeting(now: LocalTime): Int = when (now.hour) {
    in 5..11 -> R.string.greeting_morning
    in 12..17 -> R.string.greeting_afternoon
    else -> R.string.greeting_evening
}

/** A date format with [skeleton]'s fields in the user's locale's order, e.g. "MMMd" → "Oct 3" or "3 Oct". */
internal fun localizedPattern(skeleton: String): DateTimeFormatter =
    DateTimeFormatter.ofPattern(DateFormat.getBestDateTimePattern(Locale.getDefault(), skeleton), Locale.getDefault())

/** First word of the account's display name ("Ada Lovelace" → "Ada"), else a guess from the email. */
internal fun greetingName(displayName: String?, email: String?): String? =
    displayName?.trim()?.split(Regex("\\s+"))?.firstOrNull { it.isNotBlank() } ?: email?.let(::firstNameOf)

/** "jane.doe@x.dev" → "Jane". */
internal fun firstNameOf(email: String): String? =
    email.substringBefore('@').split('.', '_', '-', '+').firstOrNull { it.isNotBlank() }
        ?.replaceFirstChar { it.titlecase(Locale.getDefault()) }

/** "just now", "5m ago", "3h ago", "2d ago", then a short date. Falls back to the raw date if the timestamp won't parse. */
@Composable
internal fun relativeTime(publishedAt: String, now: Instant = Instant.now()): String {
    val then = remember(publishedAt) {
        try {
            Instant.parse(publishedAt)
        } catch (e: DateTimeParseException) {
            null
        }
    } ?: return publishedAt.take(DATE_LENGTH)
    val age = Duration.between(then, now)
    return when {
        age.toMinutes() < 1 -> stringResource(R.string.time_just_now)
        age.toHours() < 1 -> stringResource(R.string.time_minutes_ago, age.toMinutes())
        age.toDays() < 1 -> stringResource(R.string.time_hours_ago, age.toHours())
        age.toDays() < WEEK_DAYS -> stringResource(R.string.time_days_ago, age.toDays())
        else -> shortDate().format(then.atZone(ZoneId.systemDefault()))
    }
}

/** The "MMMd" format for the current locale, built once per locale: every older card needs it, and the lookup is slow. */
private fun shortDate(): DateTimeFormatter {
    val locale = Locale.getDefault()
    shortDateCache?.let { (cachedLocale, format) -> if (cachedLocale == locale) return format }
    return localizedPattern("MMMd").also { shortDateCache = locale to it }
}

/** Only touched from composition, on the main thread. */
private var shortDateCache: Pair<Locale, DateTimeFormatter>? = null

internal fun previewStories(now: Instant = Instant.now()): List<StorySummary> = listOf(
    StorySummary(
        1, "Kotlin 2.4 stabilises context parameters", "Context parameters leave preview, and the K2 compiler gets faster incremental builds.",
        "release", "medium", 4, now.minus(Duration.ofMinutes(42)).toString(), listOf("languages/kotlin", "mobile/android"), isRead = false,
    ),
    StorySummary(
        2, "OpenSSL 3.6.1 fixes a critical heap overflow", "Upgrade now: a crafted certificate can trigger remote code execution in TLS clients.",
        "security", "critical", 5, now.minus(Duration.ofHours(3)).toString(), listOf("security", "languages/go", "cloud/aws"),
        isRead = false, isBookmarked = true,
    ),
    StorySummary(
        3, "React Native drops the legacy bridge", "The new architecture is now the only option; old native modules need migrating.",
        "breaking", "high", 4, now.minus(Duration.ofDays(1)).toString(), listOf("mobile/react-native"), isRead = true,
    ),
    StorySummary(
        4, "Gradle deprecates the old dependency configurations", "compile and runtime are going away in Gradle 10.",
        "deprecation", "low", 2, now.minus(Duration.ofDays(9)).toString(), listOf("tools/gradle"), isRead = true,
    ),
)

@Composable
private fun FeedPreview(state: TimelineState) = FeedContent(
    state = state,
    displayName = "Jane Doe",
    userEmail = "jane.doe@example.com",
    contentPadding = PaddingValues(bottom = 80.dp),
    onOpen = {}, onProfile = {}, onRefresh = {}, onLoadMore = {},
    onSetRead = { _, _ -> }, onSetSaved = { _, _ -> }, onDismissError = {},
)

@Preview(name = "Feed, dark", heightDp = 1100)
@Composable
private fun FeedDark() = ChangeloomTheme(ThemeMode.Dark) {
    Box(Modifier.background(ChangeloomTheme.colors.bg)) { FeedPreview(TimelineState(items = previewStories())) }
}

@Preview(name = "Feed, light, offline", heightDp = 1100)
@Composable
private fun FeedLight() = ChangeloomTheme(ThemeMode.Light) {
    Box(Modifier.background(ChangeloomTheme.colors.bg)) { FeedPreview(TimelineState(items = previewStories(), offline = true)) }
}

@Preview(name = "Feed, loading, dark", heightDp = 900)
@Composable
private fun FeedLoadingDark() = ChangeloomTheme(ThemeMode.Dark) {
    Box(Modifier.background(ChangeloomTheme.colors.bg)) { FeedPreview(TimelineState(refreshing = true)) }
}

@Preview(name = "Tab bar, light")
@Composable
private fun TabBarLight() = ChangeloomTheme(ThemeMode.Light) { TabBar(Tab.Saved, {}, hasUnread = true) }

private const val LOAD_MORE_THRESHOLD = 3
internal const val LIST_SKELETON_CARDS = 4
private const val STAGGERED_CARDS = 6
private const val STAGGER_START_MILLIS = 200
private const val STAGGER_STEP_MILLIS = 60
private const val TAB_SLIDE_DIVISOR = 12
private const val DATE_LENGTH = 10
private const val WEEK_DAYS = 7
private const val TITLE_LINES = 3
private const val SUMMARY_LINES = 3
private const val MAX_CARD_TOPICS = 2
private const val STORY_CONTENT = "story"
private const val END_CONTENT = "end"

/** The back-to-top button shows once the list is scrolled past this item, and jumps to [BACK_TO_TOP_JUMP_ITEMS] before animating. */
private const val BACK_TO_TOP_AFTER_ITEMS = 4
private const val BACK_TO_TOP_JUMP_ITEMS = 8
private const val AD_CONTENT = "ad"
private const val FOLLOW_CONTENT = "follow"

/** New stories shown before the follow suggestion. */
private const val FOLLOW_CARD_AFTER = 2
