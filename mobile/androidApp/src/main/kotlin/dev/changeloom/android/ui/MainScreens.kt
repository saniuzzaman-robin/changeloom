package dev.changeloom.android.ui

import androidx.activity.compose.BackHandler
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.AnimatedContentTransitionScope
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
import androidx.compose.foundation.lazy.LazyItemScope
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
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
import androidx.compose.material.icons.rounded.MarkEmailUnread
import androidx.compose.material.icons.rounded.Person
import androidx.compose.material.icons.rounded.PersonOutline
import androidx.compose.material.icons.rounded.Refresh
import androidx.compose.material.icons.rounded.Search
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SwipeToDismissBox
import androidx.compose.material3.SwipeToDismissBoxValue
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.material3.pulltorefresh.PullToRefreshDefaults
import androidx.compose.material3.pulltorefresh.rememberPullToRefreshState
import androidx.compose.material3.rememberSwipeToDismissBoxState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
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
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.changeloom.android.ui.components.BannerTone
import dev.changeloom.android.ui.components.EmptyState
import dev.changeloom.android.ui.components.Eyebrow
import dev.changeloom.android.ui.components.GlassCard
import dev.changeloom.android.ui.components.GradientAvatar
import dev.changeloom.android.ui.components.GradientText
import dev.changeloom.android.ui.components.GridBackground
import dev.changeloom.android.ui.components.ImportanceMeter
import dev.changeloom.android.ui.components.KindPill
import dev.changeloom.android.ui.components.SaveToggle
import dev.changeloom.android.ui.components.SecondaryButton
import dev.changeloom.android.ui.components.SeverityDot
import dev.changeloom.android.ui.components.SkeletonBlock
import dev.changeloom.android.ui.components.SpotlightGlow
import dev.changeloom.android.ui.components.StatusBanner
import dev.changeloom.android.ui.components.StatusBarScrim
import dev.changeloom.android.ui.components.TopicChip
import dev.changeloom.android.ui.components.enter
import dev.changeloom.android.ui.theme.ChangeloomTheme
import dev.changeloom.android.ui.theme.Durations
import dev.changeloom.android.ui.theme.Radius
import dev.changeloom.android.ui.theme.ThemeMode
import dev.changeloom.android.ui.theme.expoTween
import dev.changeloom.shared.data.StorySummary
import dev.changeloom.shared.data.TimelineState
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import org.koin.androidx.compose.koinViewModel
import java.time.Duration
import java.time.Instant
import java.time.LocalDate
import java.time.LocalTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.DateTimeParseException
import java.util.Locale

private enum class Tab(val label: String, val icon: ImageVector, val selectedIcon: ImageVector) {
    Feed("Feed", Icons.Outlined.Home, Icons.Rounded.Home),
    Search("Search", Icons.Rounded.Search, Icons.Rounded.Search),
    Saved("Saved", Icons.Rounded.BookmarkBorder, Icons.Rounded.Bookmark),
    Profile("Profile", Icons.Rounded.PersonOutline, Icons.Rounded.Person),
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

/** Marks a story title as the shared element between its card and the detail screen. */
@Composable
internal fun Modifier.sharedStoryTitle(id: Long): Modifier {
    val shared = LocalSharedTransitionScope.current ?: return this
    val scope = LocalOverlayScope.current ?: return this
    return with(shared) {
        sharedBounds(rememberSharedContentState("story-title-$id"), scope, boundsTransform = { _, _ -> expoTween(Durations.SLOW) })
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
    val tree = picker.state.collectAsStateWithLifecycle().value.selection?.tree
    val topicName = remember(tree) { { slug: String -> tree?.topic(slug)?.name ?: slug.substringAfterLast('/') } }

    LaunchedEffect(openStoryId) {
        if (openStoryId != null) {
            storyId = openStoryId
            onOpenStoryHandled()
        }
    }

    val overlay = storyId?.let { Overlay.Story(it) } ?: if (editingTopics) Overlay.Topics else Overlay.None
    BackHandler(enabled = storyId != null) { storyId = null }
    BackHandler(enabled = overlay == Overlay.None && tab != Tab.Feed) { tab = Tab.Feed }

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
    val hasUnread = timeline.state.collectAsStateWithLifecycle().value.items.any { !it.isRead }
    Scaffold(
        containerColor = Color.Transparent,
        contentWindowInsets = WindowInsets(0),
        bottomBar = { TabBar(tab, onTab, hasUnread) },
    ) { padding ->
        AnimatedContent(tab, transitionSpec = { tabTransition() }, label = "tabs") { current ->
            tabs.SaveableStateProvider(current.name) {
                when (current) {
                    Tab.Feed -> FeedScreen(onOpen, onProfile = { onTab(Tab.Profile) }, displayName = profile.displayName, userEmail = profile.email, contentPadding = padding)
                    Tab.Search -> SearchScreen(onOpen, contentPadding = padding)
                    Tab.Saved -> SavedScreen(onOpen, onBrowse = { onTab(Tab.Feed) }, contentPadding = padding)
                    Tab.Profile -> ProfileScreen(onEditTopics, contentPadding = padding)
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
        Text(tab.label, style = MaterialTheme.typography.labelSmall, color = fg)
    }
}

@Composable
private fun FeedScreen(
    onOpen: (Long) -> Unit,
    onProfile: () -> Unit,
    displayName: String?,
    userEmail: String?,
    contentPadding: PaddingValues,
    vm: TimelineViewModel = koinViewModel(),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    FeedContent(
        state = state,
        displayName = displayName,
        userEmail = userEmail,
        contentPadding = contentPadding,
        onOpen = onOpen,
        onProfile = onProfile,
        onRefresh = vm::refresh,
        onLoadMore = vm::loadMore,
        onSetRead = vm::setRead,
        onSetSaved = vm::setBookmarked,
        onDismissError = vm::dismissError,
    )
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
) {
    val c = ChangeloomTheme.colors
    val listState = rememberLazyListState()
    val pullState = rememberPullToRefreshState()
    val (fresh, earlier) = remember(state.items) { state.items.partition { !it.isRead } }
    val staggered = rememberStaggered(listState)
    LoadMoreEffect(listState, onLoadMore)

    Box(Modifier.fillMaxSize()) {
        GridBackground(Modifier.fillMaxWidth().height(320.dp))
        SpotlightGlow(Modifier.fillMaxWidth().height(380.dp))
        PullToRefreshBox(
            isRefreshing = state.refreshing && state.items.isNotEmpty(),
            onRefresh = onRefresh,
            modifier = Modifier.fillMaxSize(),
            state = pullState,
            indicator = {
                PullToRefreshDefaults.Indicator(
                    state = pullState,
                    isRefreshing = state.refreshing && state.items.isNotEmpty(),
                    modifier = Modifier.align(Alignment.TopCenter).windowInsetsPadding(WindowInsets.statusBars),
                    containerColor = c.elevated,
                    color = c.primaryText,
                )
            },
        ) {
            LazyColumn(
                Modifier.fillMaxSize(),
                state = listState,
                contentPadding = listPadding(contentPadding),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                item(key = "header") { FeedHeader(displayName, userEmail, fresh.size, onProfile) }
                item(key = "banners") {
                    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        StatusBanner(if (state.offline) "Offline — showing saved stories" else null, Icons.Rounded.CloudOff, tone = BannerTone.Warning)
                        StatusBanner(state.error, Icons.Rounded.ErrorOutline, tone = BannerTone.Error, actionLabel = "Dismiss", onAction = onDismissError)
                    }
                }
                when {
                    state.items.isEmpty() && state.refreshing -> items(LIST_SKELETON_CARDS) { SkeletonStoryCard() }
                    state.items.isEmpty() -> item(key = "empty") {
                        EmptyState(
                            title = "No stories yet",
                            message = "New releases and changes for your topics land here. Pull down to check again.",
                            action = { SecondaryButton("Refresh", onRefresh, icon = Icons.Rounded.Refresh) },
                        )
                    }
                    else -> {
                        if (fresh.isNotEmpty()) {
                            item(key = "new") { SectionHeader("New for you", fresh.size, highlight = true, Modifier.animateItem()) }
                            itemsIndexed(fresh, key = { _, s -> s.id }) { i, story ->
                                FeedCard(story, staggered, i, onOpen, onSetRead, onSetSaved)
                            }
                        }
                        if (earlier.isNotEmpty()) {
                            item(key = "earlier") { SectionHeader("Earlier", null, highlight = false, Modifier.animateItem()) }
                            itemsIndexed(earlier, key = { _, s -> s.id }) { i, story ->
                                FeedCard(story, staggered, fresh.size + i, onOpen, onSetRead, onSetSaved)
                            }
                        }
                        if (state.loadingMore) item(key = "more") { SkeletonStoryCard() }
                    }
                }
            }
        }
        StatusBarScrim(listState.canScrollBackward)
    }
}

@Composable
private fun LazyItemScope.FeedCard(
    story: StorySummary,
    animateIn: Boolean,
    index: Int,
    onOpen: (Long) -> Unit,
    onSetRead: (Long, Boolean) -> Unit,
    onSetSaved: (Long, Boolean) -> Unit,
) {
    val entrance = if (animateIn && index < STAGGERED_CARDS) Modifier.enter(delayMillis = STAGGER_START_MILLIS + index * STAGGER_STEP_MILLIS) else Modifier
    SwipeActions(
        startToEnd = SwipeAction(
            label = if (story.isRead) "Mark unread" else "Mark read",
            icon = if (story.isRead) Icons.Rounded.MarkEmailUnread else Icons.Rounded.DoneAll,
            color = ChangeloomTheme.colors.success,
            onSwipe = { onSetRead(story.id, !story.isRead) },
        ),
        endToStart = SwipeAction(
            label = if (story.isBookmarked) "Unsave" else "Save",
            icon = if (story.isBookmarked) Icons.Rounded.BookmarkRemove else Icons.Rounded.Bookmark,
            color = ChangeloomTheme.colors.primary,
            onSwipe = { onSetSaved(story.id, !story.isBookmarked) },
        ),
        // A swipe action toggles in place, so the card springs back afterwards.
        resetAfterSwipe = true,
        modifier = Modifier.animateItem().then(entrance),
    ) {
        StoryCard(story, onOpen = { onOpen(story.id) }, onToggleSave = { onSetSaved(story.id, !story.isBookmarked) })
    }
}

@Composable
private fun FeedHeader(displayName: String?, userEmail: String?, freshCount: Int, onProfile: () -> Unit) {
    val c = ChangeloomTheme.colors
    val name = greetingName(displayName, userEmail)
    val today = remember { LocalDate.now().format(DateTimeFormatter.ofPattern("EEEE, MMM d", Locale.getDefault())) }
    Column(Modifier.padding(top = 8.dp, bottom = 4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Eyebrow(today, Modifier.enter(delayMillis = 0), color = c.primaryText)
                Spacer(Modifier.height(8.dp))
                Column(Modifier.enter(delayMillis = 80)) {
                    Text(greeting(LocalTime.now()) + if (name != null) "," else "", style = MaterialTheme.typography.headlineMedium, color = c.fg)
                    GradientText(name ?: "your changelog", style = MaterialTheme.typography.headlineMedium)
                }
            }
            GradientAvatar(
                displayName ?: userEmail ?: "?",
                Modifier.clip(CircleShape).clickable(onClickLabel = "Open profile", onClick = onProfile).enter(delayMillis = 120),
                size = 48.dp,
            )
        }
        Spacer(Modifier.height(8.dp))
        Text(
            when (freshCount) {
                0 -> "You're all caught up."
                1 -> "1 new update across your topics."
                else -> "$freshCount new updates across your topics."
            },
            Modifier.enter(delayMillis = 160),
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

/** Story card: meta row, title (shared with the detail screen), summary, topics, importance and save. */
@Composable
internal fun StoryCard(story: StorySummary, onOpen: () -> Unit, onToggleSave: () -> Unit, modifier: Modifier = Modifier) {
    val c = ChangeloomTheme.colors
    val topicName = LocalTopicName.current
    val brand = ChangeloomTheme.gradients.brand
    val accent by animateFloatAsState(if (story.isRead) 0f else 1f, expoTween(Durations.SLOW), label = "unreadAccent")
    GlassCard(
        modifier.fillMaxWidth(),
        onClick = onOpen,
        contentPadding = PaddingValues(start = 16.dp, end = 4.dp, top = 14.dp, bottom = 4.dp),
    ) {
        Column(
            Modifier.drawBehind {
                // Unread accent: a gradient rail along the card's left edge.
                if (accent > 0f) {
                    drawRect(brand, topLeft = Offset(-16.dp.toPx(), -14.dp.toPx()), size = Size(3.dp.toPx(), size.height + 18.dp.toPx()), alpha = accent)
                }
            },
        ) {
            Row(Modifier.padding(end = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                KindPill(story.kind)
                SeverityDot(story.severity, Modifier.padding(start = 2.dp))
                Spacer(Modifier.weight(1f))
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
        modifier = modifier,
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
private fun rememberStaggered(listState: LazyListState): Boolean {
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
 * List padding for a tab: clear of the status bar (when [top]), the tab bar ([shellPadding]) and any side
 * insets (a 3-button nav bar or display cutout in landscape), so backgrounds stay full-bleed but content doesn't.
 */
@Composable
internal fun listPadding(shellPadding: PaddingValues, top: Boolean = true): PaddingValues {
    val safe = WindowInsets.safeDrawing.asPaddingValues()
    val direction = LocalLayoutDirection.current
    return PaddingValues(
        start = safe.calculateStartPadding(direction) + 20.dp,
        end = safe.calculateEndPadding(direction) + 20.dp,
        top = if (top) safe.calculateTopPadding() + 8.dp else 8.dp,
        bottom = shellPadding.calculateBottomPadding() + 16.dp,
    )
}

private fun greeting(now: LocalTime): String = when (now.hour) {
    in 5..11 -> "Good morning"
    in 12..17 -> "Good afternoon"
    else -> "Good evening"
}

/** First word of the account's display name ("Ada Lovelace" → "Ada"), else a guess from the email. */
internal fun greetingName(displayName: String?, email: String?): String? =
    displayName?.trim()?.split(Regex("\\s+"))?.firstOrNull { it.isNotBlank() } ?: email?.let(::firstNameOf)

/** "jane.doe@x.dev" → "Jane". */
internal fun firstNameOf(email: String): String? =
    email.substringBefore('@').split('.', '_', '-', '+').firstOrNull { it.isNotBlank() }
        ?.replaceFirstChar { it.titlecase(Locale.getDefault()) }

/** "just now", "5m", "3h", "2d", then a short date. Falls back to the raw date if the timestamp won't parse. */
internal fun relativeTime(publishedAt: String, now: Instant = Instant.now()): String {
    val then = try {
        Instant.parse(publishedAt)
    } catch (e: DateTimeParseException) {
        return publishedAt.take(DATE_LENGTH)
    }
    val age = Duration.between(then, now)
    return when {
        age.toMinutes() < 1 -> "just now"
        age.toHours() < 1 -> "${age.toMinutes()}m ago"
        age.toDays() < 1 -> "${age.toHours()}h ago"
        age.toDays() < WEEK_DAYS -> "${age.toDays()}d ago"
        else -> DateTimeFormatter.ofPattern("MMM d", Locale.getDefault()).format(then.atZone(ZoneId.systemDefault()))
    }
}

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
