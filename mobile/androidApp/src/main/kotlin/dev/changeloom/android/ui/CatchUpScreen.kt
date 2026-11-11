package dev.changeloom.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.pager.PagerState
import androidx.compose.foundation.pager.VerticalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.ArrowForward
import androidx.compose.material.icons.rounded.Bookmark
import androidx.compose.material.icons.rounded.BookmarkBorder
import androidx.compose.material.icons.rounded.Close
import androidx.compose.material.icons.rounded.ErrorOutline
import androidx.compose.material.icons.rounded.ThumbUp
import androidx.compose.material.icons.rounded.VisibilityOff
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Snackbar
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.saveable.listSaver
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import dev.changeloom.android.R
import dev.changeloom.android.data.FollowSuggester
import dev.changeloom.android.ui.components.ActionPill
import dev.changeloom.android.ui.components.BannerTone
import dev.changeloom.android.ui.components.Eyebrow
import dev.changeloom.android.ui.components.GlassCard
import dev.changeloom.android.ui.components.ImportanceMeter
import dev.changeloom.android.ui.components.KeyPoints
import dev.changeloom.android.ui.components.KindPill
import dev.changeloom.android.ui.components.MAX_IMPORTANCE
import dev.changeloom.android.ui.components.PrimaryButton
import dev.changeloom.android.ui.components.ScreenBackdrop
import dev.changeloom.android.ui.components.SeverityDot
import dev.changeloom.android.ui.components.StatusBanner
import dev.changeloom.android.ui.components.TopicChip
import dev.changeloom.android.ui.theme.ChangeloomTheme
import dev.changeloom.android.ui.theme.Radius
import dev.changeloom.android.ui.theme.Spacing
import dev.changeloom.android.ui.theme.ThemeMode
import dev.changeloom.shared.data.StorySummary
import dev.changeloom.shared.data.TimelineState
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filterNotNull
import org.koin.androidx.compose.koinViewModel
import org.koin.compose.koinInject

/** Ids for catch-up, in feed order: unread stories that weren't on screen before the last refresh, nor since. */
internal fun catchUpQueue(items: List<StorySummary>, seenInSession: Set<Long>): List<Long> =
    items.filter { !it.isRead && !it.seen && it.id !in seenInSession }.map { it.id }

/**
 * Stories catch-up has left: the server's unseen count less those seen in this session, never fewer than the loaded
 * ones queued; without a server count (a cached or offline feed), only the loaded ones.
 */
internal fun catchUpCount(state: TimelineState, seenInSession: Set<Long>): Int {
    val queued = catchUpQueue(state.items, seenInSession).size
    val unseen = state.unseenCount ?: return queued
    val covered = state.items.count { !it.seen && it.id in seenInSession }
    return (unseen - covered).coerceIn(queued.toLong(), Int.MAX_VALUE.toLong()).toInt()
}

/** The queued stories still in the timeline, in queue order: dismissed and muted ones drop out, and an undo brings them back. */
internal fun catchUpPages(queue: List<Long>, items: List<StorySummary>): List<StorySummary> {
    val byId = items.associateBy { it.id }
    return queue.mapNotNull(byId::get)
}

/** [queue] plus the ids in [queued] it doesn't have yet, at the end. */
internal fun extendCatchUp(queue: List<Long>, queued: List<Long>): List<Long> {
    val known = queue.toSet()
    val added = queued.filter { it !in known }
    return if (added.isEmpty()) queue else queue + added
}

/**
 * The unseen stories one per page, ending on the caught-up card. The queue is fixed when it opens, so stories seen on
 * the way stay in it, and stories loaded later join its end. Swiping on from a story counts it as seen.
 */
@Composable
internal fun CatchUpScreen(
    onClose: () -> Unit,
    onOpen: (Long) -> Unit,
    onOpenSaved: () -> Unit,
    vm: TimelineViewModel = koinViewModel(),
    picker: TopicPickerViewModel = koinViewModel(),
    profile: ProfileViewModel = koinViewModel(),
    suggester: FollowSuggester = koinInject(),
) {
    val c = ChangeloomTheme.colors
    val state by vm.state.collectAsStateWithLifecycle()
    val undo by vm.undo.collectAsStateWithLifecycle()
    val seenInSession = vm.seenInSession.collectAsStateWithLifecycle()
    val suggestions = rememberFollowSuggestions(vm, picker, suggester)
    var queue by rememberSaveable(saver = listSaver(save = { it.value }, restore = { mutableStateOf(it) })) {
        mutableStateOf(catchUpQueue(vm.state.value.items, seenInSession.value))
    }
    LaunchedEffect(state.items) { queue = extendCatchUp(queue, catchUpQueue(state.items, seenInSession.value)) }
    val pages = remember(queue, state.items) { catchUpPages(queue, state.items) }
    val pageCount by rememberUpdatedState(pages.size + 1)
    val pager = rememberPagerState { pageCount }
    val latestPages by rememberUpdatedState(pages)
    val latestState by rememberUpdatedState(state)
    LaunchedEffect(pager) {
        var previous = pager.settledPage
        snapshotFlow { pager.settledPage }.collect { page ->
            if (page != previous) latestPages.getOrNull(previous)?.let { vm.storySeen(it.id) }
            previous = page
        }
    }
    // Near the end, the next page; again whenever one arrives while still there. A failed load waits for Retry.
    LaunchedEffect(pager) {
        snapshotFlow { latestState.nextCursor.takeIf { pager.currentPage >= pageCount - 1 - CATCH_UP_LOAD_AHEAD } }
            .distinctUntilChanged()
            .filterNotNull()
            .collect { vm.loadMore() }
    }
    val snackbar = remember { SnackbarHostState() }
    UndoSnackbarEffect(undo, snackbar, vm::undo, vm::undoExpired)
    var requestingTopic by rememberSaveable { mutableStateOf(false) }

    Box(Modifier.fillMaxSize().background(c.bg)) {
        ScreenBackdrop(Modifier.matchParentSize())
        // App rule: the pages stay between the status bar and the navigation bar.
        Column(Modifier.fillMaxSize().windowInsetsPadding(WindowInsets.safeDrawing)) {
            CatchUpTopBar(pager, pages.size, onClose)
            VerticalPager(
                pager,
                Modifier.weight(1f).readableWidth().testTag("catch_up"),
                // The next card peeks in below, so the pages read as a stack to swipe through.
                contentPadding = PaddingValues(horizontal = Spacing.gutter, vertical = CATCH_UP_PEEK),
                pageSpacing = 12.dp,
                key = { pages.getOrNull(it)?.id ?: CATCH_UP_END },
            ) { page ->
                val story = pages.getOrNull(page)
                when {
                    story != null -> CatchUpPage(
                        story,
                        boosted = story.id in state.boosted,
                        onOpen = { onOpen(story.id) },
                        onToggleSave = { vm.setBookmarked(story.id, !story.isBookmarked) },
                        onMoreLikeThis = { vm.boost(story.id) },
                        onNotInterested = { vm.dismiss(story.id) },
                    )
                    // A refresh's stories join the queue when they arrive, so it isn't the end yet either.
                    (state.nextCursor != null || state.refreshing) && !state.offline -> CatchUpLoading(state.loadingMore, state.error, onRetry = vm::loadMore)
                    else -> CatchUpEnd(
                        suggestions.caughtUp.value.including(suggestions.local.value?.topic, offeredInFeed = false),
                        savedToRead = remember(state.items) { state.items.count { !it.isRead && it.isBookmarked } },
                        onFollow = picker::follow,
                        onDecline = vm::declineSuggestion,
                        onOpenSaved = onOpenSaved,
                        onRequestTopic = {
                            profile.shown() // loads the earlier requests the sheet lists, unless they are current
                            requestingTopic = true
                        },
                        onDone = onClose,
                    )
                }
            }
        }
        SnackbarHost(
            snackbar,
            Modifier.align(Alignment.BottomCenter).windowInsetsPadding(WindowInsets.safeDrawing).padding(horizontal = Spacing.gutter, vertical = 12.dp),
        ) { data ->
            Snackbar(data, containerColor = c.elevated, contentColor = c.fg, actionColor = c.primaryText)
        }
    }
    if (requestingTopic) {
        val profileState by profile.state.collectAsStateWithLifecycle()
        TopicRequestSheet(profileState, profile::setRequestText, profile::submitRequest, onDismiss = { requestingTopic = false })
    }
    val lifecycleOwner = LocalLifecycleOwner.current
    LaunchedEffect(lifecycleOwner) { lifecycleOwner.repeatOnLifecycle(Lifecycle.State.STARTED) { vm.trackViews() } }
}

/** Close, and "k of n" while on a story; reads [pager] itself so a swipe recomposes only the bar. */
@Composable
private fun CatchUpTopBar(pager: PagerState, total: Int, onClose: () -> Unit) {
    val c = ChangeloomTheme.colors
    Row(Modifier.fillMaxWidth().height(CATCH_UP_BAR_HEIGHT).padding(horizontal = 12.dp), verticalAlignment = Alignment.CenterVertically) {
        Box(
            Modifier
                .size(40.dp)
                .clip(CircleShape)
                .background(c.navGlass)
                .border(1.dp, c.line, CircleShape)
                .clickable(role = Role.Button, onClickLabel = stringResource(R.string.close), onClick = onClose),
            contentAlignment = Alignment.Center,
        ) {
            Icon(Icons.Rounded.Close, contentDescription = stringResource(R.string.close), tint = c.fg, modifier = Modifier.size(20.dp))
        }
        Spacer(Modifier.width(14.dp))
        Eyebrow(stringResource(R.string.catch_up), color = c.primaryText)
        Spacer(Modifier.weight(1f))
        val page = pager.currentPage
        if (page < total) Eyebrow(stringResource(R.string.catch_up_progress, page + 1, total), Modifier.padding(end = 8.dp))
    }
}

/** One story: badges, title (shared with the detail screen), summary, key points, importance and topics, then its actions. */
@Composable
private fun CatchUpPage(
    story: StorySummary,
    boosted: Boolean,
    onOpen: () -> Unit,
    onToggleSave: () -> Unit,
    onMoreLikeThis: () -> Unit,
    onNotInterested: () -> Unit,
) {
    val c = ChangeloomTheme.colors
    val topicName = LocalTopicName.current
    GlassCard(Modifier.fillMaxSize().testTag("catch_up_page")) {
        // Long stories scroll inside the card; once at its end, the swipe moves the pager on.
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState())) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                KindPill(story.kind)
                SeverityDot(story.severity, Modifier.padding(start = 4.dp))
                if (story.actionRequired) ActionPill(Modifier.padding(start = 8.dp))
                Spacer(Modifier.weight(1f))
                Eyebrow(relativeTime(story.publishedAt))
            }
            Spacer(Modifier.height(16.dp))
            Text(story.title, Modifier.sharedStoryTitle(story.id), style = MaterialTheme.typography.headlineSmall, color = c.fg)
            Spacer(Modifier.height(10.dp))
            Text(story.summary, style = MaterialTheme.typography.bodyLarge, color = c.fgMuted)
            if (story.keyPoints.isNotEmpty()) {
                Spacer(Modifier.height(16.dp))
                Eyebrow(stringResource(R.string.quick_take), color = c.primaryText)
                Spacer(Modifier.height(8.dp))
                KeyPoints(story.keyPoints)
            }
            Spacer(Modifier.height(20.dp))
            Row(verticalAlignment = Alignment.CenterVertically) {
                ImportanceMeter(story.importance)
                Spacer(Modifier.width(8.dp))
                Eyebrow(stringResource(R.string.importance_label, story.importance, MAX_IMPORTANCE))
            }
            if (story.topics.isNotEmpty()) {
                Spacer(Modifier.height(14.dp))
                FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    story.topics.forEach { TopicChip(topicName(it)) }
                }
            }
        }
        Spacer(Modifier.height(12.dp))
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(4.dp)) {
            CatchUpAction(
                if (story.isBookmarked) Icons.Rounded.Bookmark else Icons.Rounded.BookmarkBorder,
                stringResource(if (story.isBookmarked) R.string.saved else R.string.save),
                actionLabel = stringResource(if (story.isBookmarked) R.string.remove_from_saved else R.string.save),
                onClick = onToggleSave,
                Modifier.weight(1f),
                active = story.isBookmarked,
            )
            // Once boosted it stays lit; the snackbar's Undo takes it back.
            CatchUpAction(
                Icons.Rounded.ThumbUp,
                stringResource(R.string.more_like_this),
                actionLabel = stringResource(R.string.more_like_this),
                onClick = onMoreLikeThis,
                Modifier.weight(1f),
                active = boosted,
                enabled = !boosted,
            )
            CatchUpAction(
                Icons.Rounded.VisibilityOff,
                stringResource(R.string.not_interested),
                actionLabel = stringResource(R.string.not_interested),
                onClick = onNotInterested,
                Modifier.weight(1f),
            )
            CatchUpAction(
                Icons.AutoMirrored.Rounded.ArrowForward,
                stringResource(R.string.open),
                actionLabel = stringResource(R.string.open_story),
                onClick = onOpen,
                Modifier.weight(1f),
            )
        }
    }
}

@Composable
private fun CatchUpAction(
    icon: ImageVector,
    label: String,
    actionLabel: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    active: Boolean = false,
    enabled: Boolean = true,
) {
    val c = ChangeloomTheme.colors
    val fg = when {
        active -> c.primaryText
        enabled -> c.fg
        else -> c.fgSubtle
    }
    Column(
        modifier
            .clip(Radius.lg)
            .background(if (active) c.primary.copy(alpha = 0.16f) else Color.Transparent)
            .clickable(enabled = enabled, role = Role.Button, onClickLabel = actionLabel, onClick = onClick)
            .padding(horizontal = 4.dp, vertical = 10.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Icon(icon, contentDescription = null, tint = fg, modifier = Modifier.size(22.dp))
        Spacer(Modifier.height(6.dp))
        Text(label, style = MaterialTheme.typography.labelMedium, color = fg, textAlign = TextAlign.Center, maxLines = 2, overflow = TextOverflow.Ellipsis)
    }
}

/** The page after the loaded stories while more may come: a placeholder, or the failed load with Retry. */
@Composable
private fun CatchUpLoading(loading: Boolean, error: String?, onRetry: () -> Unit) {
    Column(Modifier.fillMaxSize(), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        if (error != null && !loading) {
            StatusBanner(error, Icons.Rounded.ErrorOutline, tone = BannerTone.Error, actionLabel = stringResource(R.string.retry), onAction = onRetry)
        }
        SkeletonStoryCard()
    }
}

/** The last page: the feed's caught-up card, then back to the feed. */
@Composable
private fun CatchUpEnd(
    topics: CaughtUpTopics,
    savedToRead: Int,
    onFollow: (String) -> Unit,
    onDecline: (String) -> Unit,
    onOpenSaved: () -> Unit,
    onRequestTopic: () -> Unit,
    onDone: () -> Unit,
) {
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).testTag("catch_up_end")) {
        CaughtUpCard(topics, savedToRead, onFollow, onDecline, onOpenSaved, onRequestTopic)
        Spacer(Modifier.height(16.dp))
        PrimaryButton(stringResource(R.string.catch_up_done), onDone, Modifier.fillMaxWidth())
    }
}

private const val CATCH_UP_END = "end"

/** Pages from the end at which the next timeline page loads. */
private const val CATCH_UP_LOAD_AHEAD = 2
private val CATCH_UP_BAR_HEIGHT = 56.dp
private val CATCH_UP_PEEK = 20.dp

@Preview
@Composable
private fun CatchUpPageDark() = ChangeloomTheme(ThemeMode.Dark) {
    Box(Modifier.background(ChangeloomTheme.colors.bg).padding(16.dp).height(560.dp)) {
        CatchUpPage(previewStories()[0], boosted = false, onOpen = {}, onToggleSave = {}, onMoreLikeThis = {}, onNotInterested = {})
    }
}
